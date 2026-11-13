using Docproc.Ooxml.Model;
using Docproc.Ooxml.Rendering;

namespace Docproc.Ooxml.Writing;

/// <summary>A converted document as its lines: the <c>document</c> line, then each xlsx sheet's
/// <c>sheet</c> line and <c>rows</c> lines. Joined in order, the lines' Markdown fragments are the
/// renderer's whole rendering.</summary>
internal static class Lines
{
    /// <summary>Writes the document's lines to the output.</summary>
    public static void Write(Document document, Stream output)
    {
        if (document.Format != SourceFormat.Xlsx)
        {
            new LineWriter(output).WriteDocument(document.Format, MarkdownRenderer.Render(document), Pictures(document));
            return;
        }
        WriteXlsx(Pictures(document), document.Blocks.OfType<Sheet>().Select(sheet =>
        {
            var table = sheet.Blocks.OfType<Table>().FirstOrDefault();
            return new SheetSource(
                sheet.Name,
                sheet.Hidden,
                table is null ? [] : MarkdownRenderer.SheetColumns(table),
                table?.Rows ?? [],
                [.. sheet.Blocks.Where(block => block is not Table)]);
        }), output);
    }

    /// <summary>Writes an xlsx document's lines to the output: its document line, naming the given
    /// pictures, then each sheet's lines, each line written as its rows are enumerated, so a sheet
    /// read forward never waits in memory past one <c>rows</c> line.</summary>
    public static void WriteXlsx(IReadOnlyList<Media> pictures, IEnumerable<SheetSource> sheets, Stream output)
    {
        // An xlsx document's Markdown is its sheets', so its document line holds none. A sheet's
        // lines are written in the order a forward reading of it gives them: its header, its rows,
        // and then its drawings, which its drawing part anchors apart from its cells.
        var writer = new LineWriter(output);
        writer.WriteDocument(SourceFormat.Xlsx, "", pictures);
        var follows = false;
        foreach (var sheet in sheets)
        {
            writer.WriteSheet(sheet.Name, sheet.Hidden, MarkdownRenderer.SheetFragment(sheet.Name, sheet.Hidden, sheet.Columns, follows));
            foreach (var row in sheet.Rows)
            {
                writer.WriteRow(row, MarkdownRenderer.RowFragment(row, sheet.Columns));
            }
            writer.EndSheet(MarkdownRenderer.DrawingFragment(sheet.Drawings));
            follows = true;
        }
    }

    /// <summary>Every picture of the document, in the order of the Markdown's chunks: each
    /// section's headers, the body, each section's footers, the footnotes, the endnotes, and the
    /// comments with their replies; within each, in block order, nested blocks in place. A
    /// picture shown more than once is listed each time, with its own placement.</summary>
    internal static List<Media> Pictures(Document document)
    {
        var pictures = new List<Media>();
        var sections = document.Sections ?? [];
        foreach (var story in sections.SelectMany(section => section.Headers ?? []))
        {
            Collect(story.Blocks, pictures);
        }
        Collect(document.Blocks, pictures);
        foreach (var story in sections.SelectMany(section => section.Footers ?? []))
        {
            Collect(story.Blocks, pictures);
        }
        foreach (var note in (document.Footnotes ?? []).Concat(document.Endnotes ?? []))
        {
            Collect(note.Blocks, pictures);
        }
        foreach (var comment in document.Comments ?? [])
        {
            Collect(comment.Blocks, pictures);
            foreach (var reply in comment.Replies)
            {
                Collect(reply.Blocks, pictures);
            }
        }
        return pictures;
    }

    private static void Collect(IReadOnlyList<Block> blocks, List<Media> pictures)
    {
        foreach (var block in blocks)
        {
            switch (block)
            {
                case Media media:
                    pictures.Add(media);
                    break;
                case ListBlock list:
                    foreach (var item in list.Items)
                    {
                        Collect(item.Blocks, pictures);
                    }
                    break;
                case Table table:
                    foreach (var cell in table.Rows.SelectMany(row => row.Cells))
                    {
                        Collect(cell.Blocks ?? [], pictures);
                    }
                    break;
                case Sheet sheet:
                    Collect(sheet.Blocks, pictures);
                    break;
                case Slide slide:
                    Collect(slide.Blocks, pictures);
                    Collect(slide.Notes ?? [], pictures);
                    break;
                default:
                    break;
            }
        }
    }
}

/// <summary>An xlsx sheet as its lines take it: its name, whether it is hidden, the column letters
/// its table uses in the sheet's order (none when it has no table), its table's rows, which may be
/// read only as they are enumerated, and its charts and pictures in the order its drawing anchors
/// them.</summary>
internal sealed record SheetSource(string Name, bool Hidden, IReadOnlyList<string> Columns, IEnumerable<TableRow> Rows, IReadOnlyList<Block> Drawings);
