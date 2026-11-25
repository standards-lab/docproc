using Docproc.Ooxml.Model;
using Docproc.Ooxml.Rendering;

namespace Docproc.Ooxml.Writing;

internal static class Lines
{
    /// <summary>Writes the document's lines to the output.</summary>
    public static void Write(Document document, Stream output)
    {
        if (document.Format != SourceFormat.Xlsx)
        {
            new LineWriter(output).WriteDocument(document.Format, document.Properties, MarkdownRenderer.Render(document), MediaOf(document));
            return;
        }
        WriteXlsx(document.Properties, MediaOf(document), document.Blocks.OfType<Sheet>().Select(sheet =>
        {
            var table = sheet.Blocks.OfType<Table>().FirstOrDefault();
            return new SheetSource(
                sheet.Name,
                sheet.Hidden,
                table is null ? [] : MarkdownRenderer.SheetColumns(table),
                table?.Rows ?? [],
                [.. sheet.Blocks.Where(block => block is not Table)]);
        }), document.Comments ?? [], output);
    }

    /// <summary>Writes an xlsx document's lines to the output: its document line, holding the given
    /// properties and naming the given media, then each sheet's lines, each line written as its
    /// rows are enumerated, so a sheet read forward never waits in memory past one <c>rows</c>
    /// line, then the comments' definitions on lines of their own.</summary>
    public static void WriteXlsx(Properties? properties, IReadOnlyList<Media> media, IEnumerable<SheetSource> sheets, IReadOnlyList<Comment> comments, Stream output)
    {
        var writer = new LineWriter(output);
        var renderer = MarkdownRenderer.ForFragments();
        writer.WriteDocument(SourceFormat.Xlsx, properties, "", media);
        var follows = false;
        foreach (var sheet in sheets)
        {
            writer.WriteSheet(sheet.Name, sheet.Hidden, MarkdownRenderer.SheetFragment(sheet.Name, sheet.Hidden, sheet.Columns, follows));
            foreach (var row in sheet.Rows)
            {
                writer.WriteRow(row, renderer.RowFragment(row, sheet.Columns));
            }
            writer.EndSheet(MarkdownRenderer.DrawingFragment(sheet.Drawings));
            follows = true;
        }
        writer.WriteDefinitions(renderer.CommentFragments(comments));
    }

    internal static List<Media> MediaOf(Document document)
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

internal sealed record SheetSource(string Name, bool Hidden, IReadOnlyList<string> Columns, IEnumerable<TableRow> Rows, IReadOnlyList<Block> Drawings);
