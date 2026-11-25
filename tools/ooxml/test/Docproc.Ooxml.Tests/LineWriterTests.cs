using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Rendering;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Tests;

public sealed class LineWriterTests
{
    [Fact]
    public void AnXlsxIsItsDocumentLineThenEachSheetsLines()
    {
        var picture = new Media { Name = "abc.png", ContentType = "image/png", Alt = "Logo", Source = "/xl/media/image1.png", Sheet = 1 };
        var document = new Document(SourceFormat.Xlsx,
        [
            new Sheet("Data", false,
            [
                new Table(
                [
                    new TableRow(
                    [
                        Cell("A1", CellValue.OfNumber(1.5)) with { ColSpan = 2, RowSpan = 3 },
                        Cell("C1", CellValue.OfBoolean(true)),
                        Cell("D1", CellValue.OfDate("2026-01-15")),
                    ]) { Row = 1 },
                    new TableRow(
                    [
                        Cell("C2", CellValue.OfText("x")) with { Formula = "\"x\"" },
                        Cell("D2", null) with { SharedFrom = "D1" },
                        Cell("E2", CellValue.OfError("#N/A")),
                    ]) { Row = 2 },
                ]),
            ]),
            new Sheet("Hidden", true, [picture]),
        ]);

        Assert.Equal(
            """
            {"line":"document","schema":"2","format":"xlsx","media":[{"kind":"picture","file":"abc.png","content_type":"image/png","part":"/xl/media/image1.png","sheet":2}]}
            {"line":"sheet","name":"Data","markdown":"# Data\n\n|  | A | C | D | E |\n| --- | --- | --- | --- | --- |\n"}
            {"line":"rows","markdown":"| 1 | 1.5 | TRUE | 2026-01-15 |  |\n| 2 |  | x | (formula shared from D1) | \\#N/A |\n","rows":[{"row":1,"cells":[{"ref":"A1","value_type":"number","value":1.5,"col_span":2,"row_span":3},{"ref":"C1","value_type":"boolean","value":true},{"ref":"D1","value_type":"date","value":"2026-01-15"}]},{"row":2,"cells":[{"ref":"C2","value_type":"text","value":"x","formula":"\"x\""},{"ref":"D2","shared_from":"D1"},{"ref":"E2","value_type":"error","value":"#N/A"}]}]}
            {"line":"sheet","name":"Hidden","hidden":true,"markdown":"\n# Hidden (hidden)\n"}
            {"line":"rows","markdown":"\n![Logo](abc.png)\n"}

            """,
            Write(document));
    }

    [Fact]
    public void ADocxOrPptxIsItsDocumentLineAlone()
    {
        var linked = new Media { Target = "https://example.com/a.png", Title = "Linked", Slide = 0 };

        Assert.Equal(
            """
            {"line":"document","schema":"2","format":"pptx","markdown":"# Slide 1\n\n![Linked](https://example.com/a.png)\n","media":[{"kind":"picture","target":"https://example.com/a.png","slide":0}]}

            """,
            Write(new Document(SourceFormat.Pptx, [new Slide(1, false, [linked])])));
        Assert.Equal(
            """
            {"line":"document","schema":"2","format":"docx"}

            """,
            Write(new Document(SourceFormat.Docx, [])));
    }

    [Fact]
    public void PropertiesAndPayloadsAreOnTheDocumentLine()
    {
        var properties = new Properties(
            [new Property("title", new PropertyValue(PropertyValueType.Text, "Plan")), new Property("created", new PropertyValue(PropertyValueType.Text, "2026-01-15T10:00:00Z"))],
            null,
            [new Property("Reviewed", new PropertyValue(PropertyValueType.Boolean, "true")), new Property("Ratio", new PropertyValue(PropertyValueType.Number, "0.25"))]);
        var preview = new Media { Name = "p.emf", ContentType = "image/x-emf", Source = "/word/media/image1.emf", Section = 0 };
        var ole = new Media { Kind = MediaKind.Ole, Name = "o.bin", ContentType = "application/vnd.openxmlformats-officedocument.oleObject", Source = "/word/embeddings/oleObject1.bin", ProgId = "Package", Section = 0 };
        var embedded = new Media { Kind = MediaKind.Embedded, Name = "e.xlsx", Source = "/word/embeddings/Book.xlsx", Section = 1 };

        Assert.Equal(
            """
            {"line":"document","schema":"2","format":"docx","properties":{"core":{"title":"Plan","created":"2026-01-15T10:00:00Z"},"custom":{"Reviewed":true,"Ratio":0.25}},"markdown":"![](p.emf)\n\n[Package](o.bin)\n\n[Book.xlsx](e.xlsx)\n","media":[{"kind":"picture","file":"p.emf","content_type":"image/x-emf","part":"/word/media/image1.emf","section":0},{"kind":"ole","file":"o.bin","content_type":"application/vnd.openxmlformats-officedocument.oleObject","part":"/word/embeddings/oleObject1.bin","prog_id":"Package","section":0},{"kind":"embedded","file":"e.xlsx","part":"/word/embeddings/Book.xlsx","section":1}]}

            """,
            Write(new Document(SourceFormat.Docx, [preview, ole, embedded]) { Properties = properties }));
    }

    [Fact]
    public void PicturesAreListedInTheMarkdownsOrder()
    {
        static Media Picture(string name, int? section) => new() { Name = name, Section = section };
        var document = new Document(SourceFormat.Docx,
        [
            new Paragraph([new TextRun("Body"), new CommentReference(1)]),
            new Table([new TableRow([new TableCell([Picture("cell.png", 0)])])]),
            new ListBlock(false, [new ListItem([Picture("item.png", 0)])]),
        ])
        {
            Sections = [new Section([new HeaderFooter(HeaderFooterKind.Default, [Picture("top.png", 0)])], [new HeaderFooter(HeaderFooterKind.Default, [Picture("top.png", 0)])])],
            Comments = [new Comment(1, null, "Body", false, [Picture("comment.png", null)], [])],
        };

        Assert.Equal(
            ["top.png", "cell.png", "item.png", "top.png", "comment.png"],
            Lines.MediaOf(document).Select(m => m.Name));
    }

    [Fact]
    public void ASheetPastTheBoundIsSplitIntoBoundedRowsLines()
    {
        var rows = new List<TableRow>();
        for (var r = 1; r <= 3000; r++)
        {
            var text = r == 1500 ? new string('w', LineWriter.RowsLineBound) : $"row {r} " + new string('x', 200);
            rows.Add(new TableRow([Cell($"A{r}", CellValue.OfText(text)), Cell($"B{r}", CellValue.OfNumber(r))]) { Row = r });
        }
        var document = new Document(SourceFormat.Xlsx, [new Sheet("Big", false, [new Table(rows)])]);

        var output = WriteBytes(document);
        var lines = SplitLines(output);

        Assert.Equal(["document", "sheet"], lines.Take(2).Select(line => (string?)JsonNode.Parse(line)!["line"]));
        var rowsLines = lines.Skip(2).ToList();
        Assert.True(rowsLines.Count > 3, $"{rowsLines.Count} rows lines");
        var numbers = new List<int>();
        var markdown = new StringBuilder();
        for (var i = 0; i < rowsLines.Count; i++)
        {
            var line = rowsLines[i];
            var node = JsonNode.Parse(line)!;
            var held = node["rows"]!.AsArray().Select(row => (int)row!["row"]!).ToList();
            Assert.Equal("rows", (string?)node["line"]);
            Assert.NotEmpty(held);
            var size = Encoding.UTF8.GetByteCount(line) + 1;
            if (held.Count > 1 || held[0] != 1500)
            {
                Assert.True(size <= LineWriter.RowsLineBound, $"line {i}: {size} bytes");
            }
            else
            {
                Assert.True(size > LineWriter.RowsLineBound, "the large row is past the bound");
            }
            if (i + 1 < rowsLines.Count)
            {
                var first = rows[(int)JsonNode.Parse(rowsLines[i + 1])!["rows"]![0]!["row"]! - 1];
                var json = JsonNode.Parse(rowsLines[i + 1])!["rows"]![0]!.ToJsonString(Relaxed);
                var escaped = JsonEncodedText.Encode(MarkdownRenderer.ForFragments().RowFragment(first, ["A", "B"]), JavaScriptEncoder.UnsafeRelaxedJsonEscaping);
                var added = Encoding.UTF8.GetByteCount(json) + 1 + escaped.EncodedUtf8Bytes.Length;
                Assert.True(size + added > LineWriter.RowsLineBound, $"line {i} could have held the next row");
            }
            numbers.AddRange(held);
            markdown.Append((string?)node["markdown"]);
        }
        Assert.Equal(Enumerable.Range(1, 3000), numbers);
        Assert.Equal(string.Concat(rows.Select(row => MarkdownRenderer.ForFragments().RowFragment(row, ["A", "B"]))), markdown.ToString());
    }

    [Fact]
    public void CommentDefinitionsFollowTheLastSheetOnBoundedLinesOfTheirOwn()
    {
        var comments = new List<Comment>();
        var cells = new List<TableCell>();
        for (var c = 1; c <= 400; c++)
        {
            var text = c == 200 ? new string('w', LineWriter.RowsLineBound) : $"note {c} " + new string('x', 2000);
            comments.Add(new Comment(c, "Ana", "", false, [new Paragraph([new TextRun(text)])], []));
            cells.Add(Cell($"A{c}", CellValue.OfNumber(c)) with { Comments = [c] });
        }
        var rows = cells.Select((cell, i) => new TableRow([cell]) { Row = i + 1 }).ToList();
        var document = new Document(SourceFormat.Xlsx, [new Sheet("Notes", false, [new Table(rows)])]) { Comments = comments };

        var lines = SplitLines(WriteBytes(document));

        var definitions = lines.SkipWhile(line => JsonNode.Parse(line)!["rows"] is not null || (string?)JsonNode.Parse(line)!["line"] != "rows").ToList();
        Assert.True(definitions.Count > 3, $"{definitions.Count} definition lines");
        foreach (var line in definitions)
        {
            var node = JsonNode.Parse(line)!;
            Assert.Equal("rows", (string?)node["line"]);
            Assert.Null(node["rows"]);
            var markdown = (string)node["markdown"]!;
            Assert.StartsWith("\n[^c", markdown, StringComparison.Ordinal);
            if (!markdown.Contains(new string('w', 100), StringComparison.Ordinal))
            {
                Assert.True(Encoding.UTF8.GetByteCount(line) + 1 <= LineWriter.RowsLineBound, $"{Encoding.UTF8.GetByteCount(line)} bytes");
            }
            else
            {
                Assert.Equal(1, markdown.Split("\n[^c").Length - 1);
            }
        }
        Assert.Equal(MarkdownRenderer.Render(document), string.Concat(lines.Select(line => (string?)JsonNode.Parse(line)!["markdown"])));
    }

    private static readonly JsonSerializerOptions Relaxed = new() { Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping };

    private static TableCell Cell(string reference, CellValue? value) => new(null) { Ref = reference, Value = value };

    private static byte[] WriteBytes(Document document)
    {
        using var output = new MemoryStream();
        Lines.Write(document, output);
        return output.ToArray();
    }

    private static string Write(Document document) => Encoding.UTF8.GetString(WriteBytes(document));

    private static List<string> SplitLines(byte[] output)
    {
        Assert.Equal((byte)'\n', output[^1]);
        var text = Encoding.UTF8.GetString(output);
        return [.. text[..^1].Split('\n')];
    }
}
