using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Reading;
using Docproc.Ooxml.Rendering;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Tests;

public sealed class ConverterTests : IDisposable
{
    private readonly DirectoryInfo scratch = Directory.CreateTempSubdirectory("docproc-ooxml-converter-tests-");

    public void Dispose() => scratch.Delete(recursive: true);

    public static TheoryData<string, string> Cases => Fixtures.All;

    [Theory]
    [MemberData(nameof(Cases))]
    public void ModelMatchesTheGroundTruth(string name, string variant)
    {
        var document = OfficeReader.Read(Fixtures.DocumentPath(name, variant));
        var truth = JsonNode.Parse(File.ReadAllText(Fixtures.TruthPath(name, variant)))!;

        Assert.Equal(Fixtures.Format(name), document.Format.ToString().ToLowerInvariant());
        Assert.Equal(Fixtures.Format(name), (string?)truth["format"]);
        var expected = truth["elements"]!.AsArray();
        var actual = GroundTruth.Elements(document);
        for (var i = 0; i < Math.Min(expected.Count, actual.Count); i++)
        {
            Assert.True(
                JsonNode.DeepEquals(expected[i], actual[i]),
                $"element {i}: expected {expected[i]!.ToJsonString()}, got {actual[i].ToJsonString()}");
        }
        Assert.Equal(expected.Count, actual.Count);
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void OtherPartsMatchTheGroundTruth(string name, string variant)
    {
        var document = OfficeReader.Read(Fixtures.DocumentPath(name, variant));
        var truth = JsonNode.Parse(File.ReadAllText(Fixtures.TruthPath(name, variant)))!;

        foreach (var part in GroundTruth.Parts)
        {
            var expected = truth[part];
            var actual = GroundTruth.Part(document, part);
            Assert.True(
                JsonNode.DeepEquals(expected, actual),
                $"{part}: expected {expected?.ToJsonString() ?? "none"}, got {actual?.ToJsonString() ?? "none"}");
        }
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void MediaFilesAreNamedByTheirContent(string name, string variant)
    {
        var conversion = OfficeReader.Convert(Fixtures.DocumentPath(name, variant));
        var truth = JsonNode.Parse(File.ReadAllText(Fixtures.TruthPath(name, variant)))!;

        foreach (var (file, bytes) in conversion.Media)
        {
            Assert.StartsWith(Convert.ToHexStringLower(SHA256.HashData(bytes)) + ".", file, StringComparison.Ordinal);
        }
        Assert.Equal(Names(truth).Order(StringComparer.Ordinal), conversion.Media.Keys.Order(StringComparer.Ordinal));
    }

    private static HashSet<string> Names(JsonNode? node)
    {
        var names = new HashSet<string>(StringComparer.Ordinal);
        void Walk(JsonNode? n)
        {
            switch (n)
            {
                case JsonObject o:
                    if ((string?)o["kind"] is "media" or "embedded" or "ole" && o["name"] is { } fileName)
                    {
                        names.Add((string)fileName!);
                    }
                    foreach (var (_, value) in o)
                    {
                        Walk(value);
                    }
                    break;
                case JsonArray a:
                    foreach (var value in a)
                    {
                        Walk(value);
                    }
                    break;
                default:
                    break;
            }
        }
        Walk(node);
        return names;
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void DeletedTextIsGoneAndMovedTextAppearsOnce(string name, string variant)
    {
        var document = OfficeReader.Read(Fixtures.DocumentPath(name, variant));
        var truth = JsonNode.Parse(File.ReadAllText(Fixtures.TruthPath(name, variant)))!;
        string[] outputs = [Write(document), MarkdownRenderer.Render(document)];

        foreach (var output in outputs)
        {
            foreach (var absent in truth["absent"]!.AsArray().Select(n => (string)n!))
            {
                Assert.DoesNotContain(absent, output, StringComparison.Ordinal);
            }
            foreach (var once in truth["once"]!.AsArray().Select(n => (string)n!))
            {
                Assert.Equal(2, output.Split(once).Length);
            }
        }
    }

    [Theory]
    [InlineData("headings", "python-docx", "application/vnd.ms-word.document.macroEnabled.main+xml")]
    [InlineData("headings", "python-docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml")]
    [InlineData("headings", "python-docx", "application/vnd.ms-word.template.macroEnabledTemplate.main+xml")]
    [InlineData("cells", "openpyxl", "application/vnd.ms-excel.sheet.macroEnabled.main+xml")]
    [InlineData("cells", "openpyxl", "application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml")]
    [InlineData("cells", "openpyxl", "application/vnd.ms-excel.template.macroEnabled.main+xml")]
    [InlineData("slides", "python-pptx", "application/vnd.ms-powerpoint.presentation.macroEnabled.main+xml")]
    [InlineData("slides", "python-pptx", "application/vnd.openxmlformats-officedocument.presentationml.template.main+xml")]
    [InlineData("slides", "python-pptx", "application/vnd.ms-powerpoint.template.macroEnabled.main+xml")]
    [InlineData("slides", "python-pptx", "application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml")]
    public void AMacroEnabledFileOrTemplateIsReadAsItsBaseFormat(string name, string variant, string contentType)
    {
        var source = Fixtures.DocumentPath(name, variant);
        var path = Path.Combine(scratch.FullName, Path.GetFileName(source));
        File.Copy(source, path);
        using (var zip = ZipFile.Open(path, ZipArchiveMode.Update))
        {
            var baseType = $"application/vnd.openxmlformats-officedocument.{Fixtures.Format(name) switch
            {
                "docx" => "wordprocessingml.document",
                "xlsx" => "spreadsheetml.sheet",
                _ => "presentationml.presentation",
            }}.main+xml";
            var types = Read(zip, "[Content_Types].xml");
            Assert.Contains(baseType, types, StringComparison.Ordinal);
            types = types.Replace(baseType, contentType, StringComparison.Ordinal);
            if (contentType.Contains("ms-word.document.macroEnabled", StringComparison.Ordinal))
            {
                types = types.Replace("</Types>", "<Override PartName=\"/word/vbaProject.bin\" ContentType=\"application/vnd.ms-office.vbaProject\"/></Types>", StringComparison.Ordinal);
                Write(zip, "word/vbaProject.bin", "not a macro");
                var relationships = Read(zip, "word/_rels/document.xml.rels").Replace(
                    "</Relationships>",
                    "<Relationship Id=\"rIdVba\" Type=\"http://schemas.microsoft.com/office/2006/relationships/vbaProject\" Target=\"vbaProject.bin\"/></Relationships>",
                    StringComparison.Ordinal);
                Write(zip, "word/_rels/document.xml.rels", relationships);
            }
            Write(zip, "[Content_Types].xml", types);
        }

        Assert.Equal(Write(OfficeReader.Read(source)), Write(OfficeReader.Read(path)));
    }

    [Fact]
    public void ANoteIsMarkedOnTheCellItAnchorsAMergesAnchorForACoveredCell()
    {
        var path = Path.Combine(scratch.FullName, "sheet-notes.xlsx");
        File.Copy(Fixtures.DocumentPath("sheet-notes", "openpyxl"), path);
        using (var zip = ZipFile.Open(path, ZipArchiveMode.Update))
        {
            Write(zip, "xl/worksheets/sheet1.xml", Read(zip, "xl/worksheets/sheet1.xml")
                .Replace("</row></sheetData>", "</row><row r=\"7\"><c r=\"A7\" t=\"inlineStr\"><is><t>Last</t></is></c></row></sheetData>", StringComparison.Ordinal)
                .Replace("</sheetData>", "</sheetData><mergeCells count=\"1\"><mergeCell ref=\"A2:B2\"/></mergeCells>", StringComparison.Ordinal));
            Write(zip, "xl/comments/comment1.xml", Read(zip, "xl/comments/comment1.xml").Replace(
                "</commentList>",
                "<comment ref=\"A5\" authorId=\"0\"><text><t>Between rows.</t></text></comment>"
                + "<comment ref=\"C9\" authorId=\"1\"><text><t>Past the last row.</t></text></comment></commentList>",
                StringComparison.Ordinal));
        }

        var document = OfficeReader.Read(path);

        var table = Assert.IsType<Table>(Assert.IsType<Sheet>(document.Blocks[0]).Blocks[0]);
        Assert.Equal(
            ["1: A1 B1", "2: A2(2)[1]", "3: A3[2] B3 D3[3]", "5: A5[4]", "7: A7", "9: C9[5]"],
            table.Rows.Select(row => $"{row.Row}: " + string.Join(" ", row.Cells.Select(cell =>
                cell.Ref + (cell.ColSpan is { } span ? $"({span})" : "") + (cell.Comments is { } ids ? $"[{string.Join(",", ids)}]" : "")))));
        Assert.Equal([1, 2, 3, 4, 5, 6], document.Comments!.Select(c => c.Id));
        Assert.Equal(MarkdownRenderer.Render(document), string.Concat(Write(document).Split('\n')[..^1].Select(line => (string?)JsonNode.Parse(line)!["markdown"])));
        Assert.Contains("| 2 | Rent[^c1] |  |  |  |\n", MarkdownRenderer.Render(document), StringComparison.Ordinal);
    }

    [Fact]
    public void APptxAutoNumberStartsItsListAtItsStartAt()
    {
        var path = Path.Combine(scratch.FullName, "bullets.pptx");
        File.Copy(Fixtures.DocumentPath("bullets", "python-pptx"), path);
        using (var zip = ZipFile.Open(path, ZipArchiveMode.Update))
        {
            foreach (var (slide, startAt) in new[] { ("ppt/slides/slide1.xml", 3), ("ppt/slides/slide2.xml", 1) })
            {
                var xml = Read(zip, slide);
                var at = xml.IndexOf("<a:buAutoNum type=\"arabicPeriod\"/>", StringComparison.Ordinal);
                Write(zip, slide, xml[..at] + $"<a:buAutoNum type=\"arabicPeriod\" startAt=\"{startAt}\"/>" + xml[(at + "<a:buAutoNum type=\"arabicPeriod\"/>".Length)..]);
            }
        }

        var lists = OfficeReader.Read(path).Blocks.OfType<Slide>()
            .Select(slide => slide.Blocks.OfType<ListBlock>().First(list => list.Ordered))
            .ToList();
        Assert.Equal([3, null], lists.Select(list => list.Start));
    }

    [Fact]
    public void AChartExChartWithNoCacheIsItsFallbackPictureInADocx()
    {
        var path = Uncached("chartex", "word/charts/chart4.xml");

        var blocks = OfficeReader.Read(path).Blocks;
        Assert.Equal(
            ["ListBlock", "Paragraph", "Chart", "Chart", "Media", "Chart", "Chart"],
            blocks.Select(b => b.GetType().Name));
        var media = Assert.IsType<Media>(blocks[4]);
        Assert.Equal(Named(path, "word/media/image1.png"), media.Name);
        Assert.Equal(0, media.Section);
        Assert.Equal("sunburst", Assert.IsType<Chart>(blocks[^1]).ChartType);
    }

    [Fact]
    public void AChartExChartWithNoCacheIsItsFallbackPictureInAPptx()
    {
        var path = Uncached("chartex-slide", "ppt/charts/chartEx1.xml");

        var slide = Assert.IsType<Slide>(Assert.Single(OfficeReader.Read(path).Blocks));
        var media = Assert.IsType<Media>(Assert.Single(slide.Blocks));
        Assert.Equal(Named(path, "ppt/media/image1.png"), media.Name);
        Assert.Equal(0, media.Slide);
    }

    [Theory]
    [InlineData(true)]
    [InlineData(false)]
    public void AnXlsxChartExChartIsItsCacheOrElseItsFallbackPicture(bool cached)
    {
        var path = Path.Combine(scratch.FullName, "with-chartex.xlsx");
        File.Copy(Fixtures.DocumentPath("with-drawing", Fixtures.RealWorld), path);
        string chartEx;
        using (var source = ZipFile.OpenRead(Fixtures.DocumentPath("chartex-slide", Fixtures.RealWorld)))
        {
            chartEx = Read(source, "ppt/charts/chartEx1.xml");
        }
        using (var zip = ZipFile.Open(path, ZipArchiveMode.Update))
        {
            Write(zip, "xl/charts/chartEx1.xml", cached ? chartEx : WithoutPoints(chartEx));
            Write(zip, "[Content_Types].xml", Read(zip, "[Content_Types].xml").Replace(
                "</Types>",
                "<Override PartName=\"/xl/charts/chartEx1.xml\" ContentType=\"application/vnd.ms-office.chartex+xml\"/></Types>",
                StringComparison.Ordinal));
            Write(zip, "xl/drawings/_rels/drawing1.xml.rels", Read(zip, "xl/drawings/_rels/drawing1.xml.rels").Replace(
                "</Relationships>",
                "<Relationship Id=\"rIdCx\" Type=\"http://schemas.microsoft.com/office/2014/relationships/chartEx\" Target=\"../charts/chartEx1.xml\"/></Relationships>",
                StringComparison.Ordinal));
            var drawing = Read(zip, "xl/drawings/drawing1.xml");
            var at = drawing.IndexOf("</xdr:twoCellAnchor>", StringComparison.Ordinal) + "</xdr:twoCellAnchor>".Length;
            Write(zip, "xl/drawings/drawing1.xml", drawing[..at] + ChartExAnchor + drawing[at..]);
        }

        var blocks = Assert.IsType<Sheet>(OfficeReader.Read(path).Blocks[0]).Blocks;
        var original = Assert.IsType<Sheet>(OfficeReader.Read(Fixtures.DocumentPath("with-drawing", Fixtures.RealWorld)).Blocks[0]).Blocks;
        Assert.Equal(Truth(original), Truth([blocks[0], .. blocks.Skip(2)]));
        if (cached)
        {
            var chart = Assert.IsType<Chart>(blocks[1]);
            Assert.Equal("boxWhisker", chart.ChartType);
            Assert.Equal(["Series1", "Series2", "Series3"], chart.Series.Select(s => s.Name));
        }
        else
        {
            var media = Assert.IsType<Media>(blocks[1]);
            Assert.Equal(Assert.IsType<Media>(original[0]).Name, media.Name);
            Assert.Equal("the chart's picture", media.Alt);
            Assert.Equal(0, media.Sheet);
        }
    }

    private const string ChartExAnchor =
        "<xdr:twoCellAnchor xmlns:mc=\"http://schemas.openxmlformats.org/markup-compatibility/2006\" xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\">"
        + "<xdr:from><xdr:col>0</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>0</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from>"
        + "<xdr:to><xdr:col>4</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>8</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to>"
        + "<mc:AlternateContent><mc:Choice xmlns:cx1=\"http://schemas.microsoft.com/office/drawing/2015/9/8/chartex\" Requires=\"cx1\">"
        + "<xdr:graphicFrame macro=\"\"><xdr:nvGraphicFramePr><xdr:cNvPr id=\"20\" name=\"Chart 19\"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr>"
        + "<xdr:xfrm><a:off x=\"0\" y=\"0\"/><a:ext cx=\"0\" cy=\"0\"/></xdr:xfrm><a:graphic><a:graphicData uri=\"http://schemas.microsoft.com/office/drawing/2014/chartex\">"
        + "<cx:chart xmlns:cx=\"http://schemas.microsoft.com/office/drawing/2014/chartex\" r:id=\"rIdCx\"/></a:graphicData></a:graphic></xdr:graphicFrame></mc:Choice>"
        + "<mc:Fallback><xdr:pic><xdr:nvPicPr><xdr:cNvPr id=\"20\" name=\"Chart 19\" descr=\"the chart's picture\"/><xdr:cNvPicPr/></xdr:nvPicPr>"
        + "<xdr:blipFill><a:blip r:embed=\"rId1\"/></xdr:blipFill><xdr:spPr/></xdr:pic></mc:Fallback></mc:AlternateContent><xdr:clientData/></xdr:twoCellAnchor>";

    private string Uncached(string name, string part)
    {
        var source = Fixtures.DocumentPath(name, Fixtures.RealWorld);
        var path = Path.Combine(scratch.FullName, Path.GetFileName(source));
        File.Copy(source, path);
        using var zip = ZipFile.Open(path, ZipArchiveMode.Update);
        var xml = Read(zip, part);
        Assert.Contains("<cx:pt ", xml, StringComparison.Ordinal);
        Write(zip, part, WithoutPoints(xml));
        return path;
    }

    private static string Truth(IReadOnlyList<Block> blocks) =>
        new JsonArray([.. GroundTruth.Elements(new Document(SourceFormat.Xlsx, blocks))]).ToJsonString();

    private static string WithoutPoints(string chartEx) =>
        Regex.Replace(chartEx, "<cx:pt [^>]*>[^<]*</cx:pt>", "");

    private static string Named(string package, string entry)
    {
        using var zip = ZipFile.OpenRead(package);
        using var stream = zip.GetEntry(entry)!.Open();
        return Convert.ToHexStringLower(SHA256.HashData(stream)) + Path.GetExtension(entry);
    }

    private static string Read(ZipArchive zip, string entry)
    {
        using var reader = new StreamReader(zip.GetEntry(entry)!.Open());
        return reader.ReadToEnd();
    }

    private static void Write(ZipArchive zip, string entry, string content)
    {
        zip.GetEntry(entry)?.Delete();
        using var writer = new StreamWriter(zip.CreateEntry(entry).Open());
        writer.Write(content);
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void TheLinesMarkdownJoinsIntoTheWholeRendering(string name, string variant)
    {
        var document = OfficeReader.Read(Fixtures.DocumentPath(name, variant));

        var lines = Write(document).Split('\n')[..^1].Select(line => JsonNode.Parse(line)!.AsObject()).ToList();

        Assert.Equal("document", (string?)lines[0]["line"]);
        Assert.Equal("2", (string?)lines[0]["schema"]);
        Assert.Equal(Fixtures.Format(name), (string?)lines[0]["format"]);
        Assert.All(lines.Skip(1), line => Assert.False(line.ContainsKey("schema")));
        var kinds = lines.Skip(1).Select(line => (string?)line["line"]).ToList();
        Assert.All(kinds, kind => Assert.True(kind is "sheet" or "rows", kind));
        Assert.Equal(document.Blocks.OfType<Sheet>().Count(), kinds.Count(kind => kind == "sheet"));
        if (Fixtures.Format(name) == "xlsx")
        {
            Assert.False(lines[0].ContainsKey("markdown"));
        }
        Assert.Equal(MarkdownRenderer.Render(document), string.Concat(lines.Select(line => (string?)line["markdown"])));
    }

    [Theory]
    [MemberData(nameof(Cases))]
    public void TheDocumentLineNamesEveryMediaFile(string name, string variant)
    {
        var conversion = OfficeReader.Convert(Fixtures.DocumentPath(name, variant));

        var document = JsonNode.Parse(Write(conversion.Document).Split('\n')[0])!;
        var files = (document["media"]?.AsArray() ?? []).Select(medium => (string?)medium!["file"]).OfType<string>();
        Assert.Equal(conversion.Media.Keys.Order(StringComparer.Ordinal), files.Distinct().Order(StringComparer.Ordinal));
    }

    private static string Write(Document document)
    {
        using var output = new MemoryStream();
        Lines.Write(document, output);
        return Encoding.UTF8.GetString(output.ToArray());
    }
}
