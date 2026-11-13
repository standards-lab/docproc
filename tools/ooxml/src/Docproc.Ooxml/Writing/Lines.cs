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
        var writer = new LineWriter(output);
        if (document.Format != SourceFormat.Xlsx)
        {
            writer.WriteDocument(document.Format, MarkdownRenderer.Render(document), Pictures(document));
            return;
        }
        // An xlsx document's Markdown is its sheets', so its document line holds none. A sheet's
        // lines are written in the order a forward reading of it gives them: its header, its rows,
        // and then its drawings, which its drawing part anchors apart from its cells.
        writer.WriteDocument(document.Format, "", Pictures(document));
        var follows = false;
        foreach (var sheet in document.Blocks.OfType<Sheet>())
        {
            var table = sheet.Blocks.OfType<Table>().FirstOrDefault();
            var columns = table is null ? [] : MarkdownRenderer.SheetColumns(table);
            writer.WriteSheet(sheet.Name, sheet.Hidden, MarkdownRenderer.SheetFragment(sheet.Name, sheet.Hidden, columns, follows));
            foreach (var row in table?.Rows ?? [])
            {
                writer.WriteRow(row, MarkdownRenderer.RowFragment(row, columns));
            }
            writer.EndSheet(MarkdownRenderer.DrawingFragment([.. sheet.Blocks.Where(block => block is not Table)]));
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
