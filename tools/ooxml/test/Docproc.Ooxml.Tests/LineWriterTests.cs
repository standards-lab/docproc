using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Rendering;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Tests;

// The line writer's seam, independent of the reader: hand-built models against lines written by
// hand from the contract (context/layers.md, "docproc-ooxml"), never a snapshot of
// docproc-ooxml's own output, and a sheet past the rows line's bound.
public sealed class LineWriterTests
{
    // Every field of the document, sheet, and rows lines, in order and in snake_case: a value
    // typed as its JSON primitive, a field with no value left out, a sheet hidden only when it is,
    // and a sheet's drawings on a closing rows line with no rows.
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
            {"line":"document","schema":"2","format":"xlsx","media":[{"kind":"picture","file":"abc.png","content_type":"image/png","part":"/xl/media/image1.png","sheet":1}]}
            {"line":"sheet","name":"Data","markdown":"# Data\n\n|  | A | C | D | E |\n| --- | --- | --- | --- | --- |\n"}
            {"line":"rows","markdown":"| 1 | 1.5 | TRUE | 2026-01-15 |  |\n| 2 |  | x | (formula shared from D1) | \\#N/A |\n","rows":[{"row":1,"cells":[{"ref":"A1","value_type":"number","value":1.5,"col_span":2,"row_span":3},{"ref":"C1","value_type":"boolean","value":true},{"ref":"D1","value_type":"date","value":"2026-01-15"}]},{"row":2,"cells":[{"ref":"C2","value_type":"text","value":"x","formula":"\"x\""},{"ref":"D2","shared_from":"D1"},{"ref":"E2","value_type":"error","value":"#N/A"}]}]}
            {"line":"sheet","name":"Hidden","hidden":true,"markdown":"\n# Hidden (hidden)\n"}
            {"line":"rows","markdown":"\n![Logo](abc.png)\n"}

            """,
            Write(document));
    }

    // A docx or pptx is its document line alone, its Markdown whole; a document with no text and
    // no pictures has neither field.
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

    // A docx's pictures are listed in the order of the Markdown's chunks, headers first, each
    // time one is shown, nested ones in place.
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
            Lines.Pictures(document).Select(m => m.Name));
    }

    // A sheet past the bound: rows lines each within 256 KiB, its newline included, holding as
    // many of the next rows as fit, and a row larger than the bound alone on its line; together
    // they hold every row in order, and their Markdown is the sheet's table rows in order.
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
                // The next line's first row, its JSON, a comma, and its Markdown as escaped, would
                // not have fit on this one.
                var first = rows[(int)JsonNode.Parse(rowsLines[i + 1])!["rows"]![0]!["row"]! - 1];
                var json = JsonNode.Parse(rowsLines[i + 1])!["rows"]![0]!.ToJsonString(Relaxed);
                var escaped = JsonEncodedText.Encode(MarkdownRenderer.RowFragment(first, ["A", "B"]), JavaScriptEncoder.UnsafeRelaxedJsonEscaping);
                var added = Encoding.UTF8.GetByteCount(json) + 1 + escaped.EncodedUtf8Bytes.Length;
                Assert.True(size + added > LineWriter.RowsLineBound, $"line {i} could have held the next row");
            }
            numbers.AddRange(held);
            markdown.Append((string?)node["markdown"]);
        }
        Assert.Equal(Enumerable.Range(1, 3000), numbers);
        Assert.Equal(string.Concat(rows.Select(row => MarkdownRenderer.RowFragment(row, ["A", "B"]))), markdown.ToString());
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

    // The lines of an output, as UTF-8 bytes measured as written: every line ends in a newline.
    private static List<string> SplitLines(byte[] output)
    {
        Assert.Equal((byte)'\n', output[^1]);
        var text = Encoding.UTF8.GetString(output);
        return [.. text[..^1].Split('\n')];
    }
}
