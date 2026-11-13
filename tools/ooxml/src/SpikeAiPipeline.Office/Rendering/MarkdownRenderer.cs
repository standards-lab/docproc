using System.Globalization;
using System.Text;
using SpikeAiPipeline.Office.Model;

namespace SpikeAiPipeline.Office.Rendering;

/// <summary>The model as GitHub-flavored Markdown: a rendering of the model, never a second
/// reading of the document.</summary>
public sealed class MarkdownRenderer
{
    // The labels of the notes and comments referenced so far, numbered in the order the
    // rendering reaches their references: footnotes and endnotes share one count, [^1], [^2], ...,
    // so their ids never collide, and comments have their own, [^c1], [^c2], ....
    private readonly Dictionary<(bool Endnote, int Id), int> notes = [];
    private readonly Dictionary<int, int> comments = [];

    // How many levels a heading is rendered below its own: one inside a slide, whose own heading
    // is the outermost.
    private int headingShift;

    private MarkdownRenderer()
    {
    }

    /// <summary>The document as Markdown, ending in a newline, or empty when the document has no
    /// text: each distinct header first, then the body, each distinct footer, the notes as GFM
    /// footnote definitions, and the comments as footnote definitions too, every chunk separated
    /// by a blank line. A header or footer is distinct by its rendering, so one that sections
    /// share, or that repeats another's content, is rendered once.</summary>
    public static string Render(Document document)
    {
        var renderer = new MarkdownRenderer();
        var stories = document.Sections ?? [];
        var chunks = new List<string>();
        chunks.AddRange(renderer.Stories(stories.SelectMany(s => s.Headers ?? [])));
        chunks.AddRange(renderer.RenderBlocks(document.Blocks).Select(c => c.Text));
        chunks.AddRange(renderer.Stories(stories.SelectMany(s => s.Footers ?? [])));
        chunks.AddRange(renderer.NoteDefinitions(document));
        chunks.AddRange(renderer.CommentDefinitions(document));
        return chunks.Count == 0 ? "" : string.Join("\n\n", chunks) + "\n";
    }

    private List<string> Stories(IEnumerable<HeaderFooter> stories)
    {
        var distinct = new List<string>();
        foreach (var story in stories)
        {
            var text = string.Join("\n\n", RenderBlocks(story.Blocks).Select(c => c.Text));
            if (text.Length > 0 && !distinct.Contains(text))
            {
                distinct.Add(text);
            }
        }
        return distinct;
    }

    // The notes in label order. A note no reference reached is labeled after the rest, so its
    // definition is still there; a note's own references are labeled as its definition is made.
    private List<string> NoteDefinitions(Document document)
    {
        var byKey = new Dictionary<(bool Endnote, int Id), Note>();
        foreach (var note in document.Footnotes ?? [])
        {
            byKey.TryAdd((false, note.Id), note);
        }
        foreach (var note in document.Endnotes ?? [])
        {
            byKey.TryAdd((true, note.Id), note);
        }
        foreach (var key in byKey.Keys)
        {
            NoteLabel(key);
        }
        var definitions = new List<string>();
        for (var label = 1; label <= notes.Count; label++)
        {
            var key = notes.First(n => n.Value == label).Key;
            if (byKey.TryGetValue(key, out var note))
            {
                definitions.Add(Definition($"[^{label}]: ", RenderBlocks(note.Blocks)));
            }
        }
        return definitions;
    }

    // Each comment: its author, its thread's state, the text it is anchored to, its text, and its
    // replies, each a paragraph of the definition.
    private List<string> CommentDefinitions(Document document)
    {
        var all = document.Comments ?? [];
        foreach (var comment in all)
        {
            CommentLabel(comment.Id);
        }
        var definitions = new List<string>();
        foreach (var comment in all.OrderBy(c => comments[c.Id]))
        {
            var anchor = string.Join(" ", Lines(comment.Anchor));
            var head = $"Comment by {Author(comment.Author)} ({(comment.Resolved ? "resolved" : "open")})"
                + (anchor.Length > 0 ? $" on \"{anchor}\"" : "") + ": ";
            var chunks = RenderBlocks(comment.Blocks);
            foreach (var reply in comment.Replies)
            {
                var replyChunks = RenderBlocks(reply.Blocks);
                var prefix = $"Reply by {Author(reply.Author)}: ";
                if (replyChunks.Count > 0 && replyChunks[0].IsParagraph)
                {
                    replyChunks[0] = replyChunks[0] with { Text = prefix + replyChunks[0].Text };
                }
                else
                {
                    replyChunks.Insert(0, new Chunk(prefix.TrimEnd(), false, true));
                }
                chunks.AddRange(replyChunks);
            }
            definitions.Add(Definition($"[^c{comments[comment.Id]}]: " + head, chunks));
        }
        return definitions;
    }

    private static string Author(string? author) => author is null ? "an unknown author" : string.Join(" ", Lines(author));

    // A footnote definition: its first paragraph on the label's line, the rest of its chunks
    // indented four spaces after a blank line, the continuation GFM reads as the same footnote.
    private static string Definition(string label, List<Chunk> chunks)
    {
        const string indent = "    ";
        var text = new StringBuilder(label.TrimEnd());
        var start = 0;
        if (chunks.Count > 0 && chunks[0].IsParagraph)
        {
            text.Append(' ').Append(Indent(chunks[0].Text, indent, firstLine: false));
            start = 1;
        }
        for (var c = start; c < chunks.Count; c++)
        {
            text.Append(c == 0 ? "\n" : "\n\n").Append(Indent(chunks[c].Text, indent, firstLine: true));
        }
        return text.ToString();
    }

    private int NoteLabel((bool Endnote, int Id) key)
    {
        if (!notes.TryGetValue(key, out var label))
        {
            notes[key] = label = notes.Count + 1;
        }
        return label;
    }

    private int CommentLabel(int id)
    {
        if (!comments.TryGetValue(id, out var label))
        {
            comments[id] = label = comments.Count + 1;
        }
        return label;
    }

    // A chunk of rendered blocks: whether it is a list, which joins a list item tightly, and
    // whether it is a paragraph, which can follow a list marker or a definition's label.
    private readonly record struct Chunk(string Text, bool IsList, bool IsParagraph);

    // Markdown joins two adjacent lists of one kind into one, so a list that follows another of
    // its kind switches markers, '-' to '*' or '.' to ')', which starts a new list.
    private List<Chunk> RenderBlocks(IReadOnlyList<Block> blocks)
    {
        var chunks = new List<Chunk>();
        bool? previousOrdered = null;
        var alternate = false;
        foreach (var block in blocks)
        {
            if (block is ListBlock list)
            {
                alternate = previousOrdered == list.Ordered && !alternate;
                previousOrdered = list.Ordered;
                chunks.Add(new Chunk(RenderList(list, alternate), true, false));
                continue;
            }
            previousOrdered = null;
            alternate = false;
            var text = block switch
            {
                Heading heading => RenderHeading(heading),
                Paragraph paragraph => RenderParagraph(paragraph),
                Table table => RenderTable(table),
                Chart chart => RenderChart(chart),
                Media media => RenderMedia(media),
                Sheet sheet => RenderSheet(sheet),
                Slide slide => RenderSlide(slide),
                _ => "",
            };
            if (text.Length > 0)
            {
                chunks.Add(new Chunk(text, false, block is Paragraph));
            }
        }
        return chunks;
    }

    private string RenderHeading(Heading heading)
    {
        var text = string.Join(" ", Lines(heading.Inlines));
        return text.Length == 0 ? "" : new string('#', Math.Clamp(heading.Level + headingShift, 1, 6)) + " " + text;
    }

    // A line break inside a paragraph is a hard break: a backslash at the end of the line.
    private string RenderParagraph(Paragraph paragraph) =>
        string.Join("\\\n", Lines(paragraph.Inlines));

    // An item's first paragraph follows its marker; its other blocks are indented to the
    // marker's width, a nested list on the next line, keeping the list tight, anything else after
    // a blank line. An ordered list counts from its start, which Markdown takes from its first
    // item's number.
    private string RenderList(ListBlock list, bool alternate)
    {
        var text = new StringBuilder();
        var first = (long)(list.Start ?? 1);
        for (var i = 0; i < list.Items.Count; i++)
        {
            var marker = list.Ordered ? $"{(first + i).ToString(CultureInfo.InvariantCulture)}{(alternate ? ')' : '.')}" : alternate ? "*" : "-";
            var indent = new string(' ', marker.Length + 1);
            var chunks = RenderBlocks(list.Items[i].Blocks);
            if (i > 0)
            {
                text.Append('\n');
            }
            text.Append(marker);
            var start = 0;
            if (chunks.Count > 0 && !chunks[0].IsList)
            {
                text.Append(' ').Append(Indent(chunks[0].Text, indent, firstLine: false));
                start = 1;
            }
            for (var c = start; c < chunks.Count; c++)
            {
                text.Append(chunks[c].IsList || c == 0 ? "\n" : "\n\n").Append(Indent(chunks[c].Text, indent, firstLine: true));
            }
        }
        return text.ToString();
    }

    private static string Indent(string text, string indent, bool firstLine)
    {
        var lines = text.Split('\n');
        for (var i = firstLine ? 0 : 1; i < lines.Length; i++)
        {
            if (lines[i].Length > 0)
            {
                lines[i] = indent + lines[i];
            }
        }
        return string.Join("\n", lines);
    }

    // A sheet under a heading of its name, marked when it is hidden, then its blocks: its table,
    // charts, and pictures.
    private string RenderSheet(Sheet sheet)
    {
        var name = string.Join(" ", Lines(sheet.Name));
        var heading = "# " + (name.Length > 0 ? name : "Sheet") + (sheet.Hidden ? " (hidden)" : "");
        return string.Join("\n\n", [heading, .. RenderBlocks(sheet.Blocks).Select(c => c.Text)]);
    }

    // A slide under a heading of its number, marked when it is hidden, then its blocks, its
    // title's heading a level below the slide's, and then its notes after a bold "Notes" label,
    // which no slide text can make, as text's asterisks are escaped.
    private string RenderSlide(Slide slide)
    {
        var heading = "# Slide " + slide.Number.ToString(CultureInfo.InvariantCulture) + (slide.Hidden ? " (hidden)" : "");
        headingShift++;
        List<string> chunks = [heading, .. RenderBlocks(slide.Blocks).Select(c => c.Text)];
        if (slide.Notes is { } notes && RenderBlocks(notes) is { Count: > 0 } notesChunks)
        {
            chunks.Add("**Notes**");
            chunks.AddRange(notesChunks.Select(c => c.Text));
        }
        headingShift--;
        return string.Join("\n\n", chunks);
    }

    // A GFM table, its first row the header. GFM has no merged cells, so a merged cell's text sits
    // in its top-left grid position and the positions it covers are empty, as are the positions a
    // row that starts late skips. A sheet's table has a grid of its own.
    private string RenderTable(Table table)
    {
        if (table.Rows.Count == 0)
        {
            return "";
        }
        if (table.Rows.Any(row => row.Row is not null))
        {
            return RenderSheetTable(table);
        }
        var grid = new List<string?[]>();
        for (var r = 0; r < table.Rows.Count; r++)
        {
            var column = 0;
            foreach (var cell in table.Rows[r].Cells)
            {
                column += cell.SkipBefore ?? 0;
                while (At(grid, r, column) is not null)
                {
                    column++;
                }
                var colSpan = cell.ColSpan ?? 1;
                var rowSpan = Math.Min(cell.RowSpan ?? 1, table.Rows.Count - r);
                for (var dr = 0; dr < rowSpan; dr++)
                {
                    for (var dc = 0; dc < colSpan; dc++)
                    {
                        Set(grid, r + dr, column + dc, dr == 0 && dc == 0 ? CellText(cell.Blocks ?? []) : "");
                    }
                }
                column += colSpan;
            }
        }
        while (grid.Count < table.Rows.Count)
        {
            grid.Add([]);
        }
        return GridTable(grid);
    }

    // A sheet's table as the sheet shows it: a header row of the column letters the cells use, in
    // order, and each row that has cells under its number, the columns and rows between left
    // out, so the grid stays as small as the sheet's used cells. A merged cell's value sits in its
    // anchor's position. A cell shows its value, or else its formula, or for a shared formula's
    // cell its anchor.
    private static string RenderSheetTable(Table table)
    {
        var columns = table.Rows.SelectMany(row => row.Cells).Select(cell => cell.Ref).OfType<string>()
            .Select(ColumnOf).Distinct().OrderBy(c => c.Length).ThenBy(c => c, StringComparer.Ordinal).ToList();
        if (columns.Count == 0)
        {
            return "";
        }
        List<string?[]> grid = [["", .. columns]];
        foreach (var row in table.Rows)
        {
            var line = new string?[columns.Count + 1];
            line[0] = row.Row?.ToString(CultureInfo.InvariantCulture) ?? "";
            foreach (var cell in row.Cells)
            {
                if (cell.Ref is { } reference)
                {
                    line[columns.IndexOf(ColumnOf(reference)) + 1] = SheetCellText(cell);
                }
            }
            grid.Add(line);
        }
        return GridTable(grid);
    }

    private static string ColumnOf(string reference) => new([.. reference.TakeWhile(char.IsAsciiLetter)]);

    // A value plainly: a number in its shortest round-trip form, a boolean as TRUE or FALSE, the
    // rest as their text. A formula with no cached value is shown as a code span, = and its text.
    private static string SheetCellText(TableCell cell)
    {
        if (cell.Value is { } value)
        {
            return value.Type switch
            {
                CellValueType.Number => value.Number.ToString("R", CultureInfo.InvariantCulture),
                CellValueType.Boolean => value.Boolean ? "TRUE" : "FALSE",
                _ => Cell(value.Text),
            };
        }
        if (cell.Formula is { } formula)
        {
            return Code("=" + formula);
        }
        return cell.SharedFrom is { } anchor ? $"(formula shared from {anchor})" : "";
    }

    // A code span on one line, its fence longer than any run of backticks inside, and its pipes
    // escaped, as a GFM table cell requires even in code.
    private static string Code(string text)
    {
        text = text.Replace('\n', ' ').Replace('\r', ' ').Replace("|", "\\|", StringComparison.Ordinal);
        var longest = 0;
        var run = 0;
        foreach (var c in text)
        {
            run = c == '`' ? run + 1 : 0;
            longest = Math.Max(longest, run);
        }
        var fence = new string('`', longest + 1);
        var pad = text.StartsWith('`') || text.EndsWith('`') ? " " : "";
        return fence + pad + text + pad + fence;
    }

    private static string GridTable(List<string?[]> grid)
    {
        var columns = grid.Max(row => row.Length);
        if (columns == 0)
        {
            return "";
        }
        var lines = new List<string> { Row(grid[0], columns), "|" + string.Concat(Enumerable.Repeat(" --- |", columns)) };
        lines.AddRange(grid.Skip(1).Select(row => Row(row, columns)));
        return string.Join("\n", lines);
    }

    // A chart: its title, then a GFM table of its series, a column each, and a row for each
    // category, the first series' categories naming the rows (another's where it lacks one).
    // A point a series lacks is an empty cell.
    private static string RenderChart(Chart chart)
    {
        var parts = new List<string>();
        if (chart.Title is { } title && string.Join(" ", Lines(title)) is { Length: > 0 } heading)
        {
            parts.Add(heading);
        }
        var rows = chart.Series.Count == 0 ? 0 : chart.Series.Max(s => Math.Max(s.Categories.Count, s.Values.Count));
        if (rows > 0)
        {
            List<string?[]> grid = [["", .. chart.Series.Select(s => Cell(s.Name))]];
            for (var r = 0; r < rows; r++)
            {
                var category = chart.Series.Select(s => r < s.Categories.Count ? s.Categories[r] : null).FirstOrDefault(c => c is not null);
                grid.Add([Cell(category), .. chart.Series.Select(s => r < s.Values.Count && s.Values[r] is double v ? v.ToString("R", CultureInfo.InvariantCulture) : "")]);
            }
            parts.Add(GridTable(grid));
        }
        return string.Join("\n\n", parts);
    }

    private static string Cell(string? text) => text is null ? "" : string.Join("<br>", Lines(text));

    // A picture as an image link to its file, relative to the media directory, or to a linked
    // picture's target; its alternative text, or else its title, is the link's text.
    private static string RenderMedia(Media media)
    {
        var alt = string.Join(" ", Lines(media.Alt ?? media.Title ?? ""));
        var destination = media.Name ?? media.Target ?? "";
        if (destination.Any(c => char.IsWhiteSpace(c) || c is '(' or ')' or '<' or '>'))
        {
            destination = "<" + destination.Replace("<", "%3C", StringComparison.Ordinal).Replace(">", "%3E", StringComparison.Ordinal) + ">";
        }
        return $"![{alt}]({destination})";
    }

    private static string Row(string?[] cells, int columns) =>
        "|" + string.Concat(Enumerable.Range(0, columns).Select(c => " " + (c < cells.Length ? cells[c] ?? "" : "") + " |"));

    private static string? At(List<string?[]> grid, int row, int column) =>
        row < grid.Count && column < grid[row].Length ? grid[row][column] : null;

    private static void Set(List<string?[]> grid, int row, int column, string value)
    {
        while (grid.Count <= row)
        {
            grid.Add([]);
        }
        if (grid[row].Length <= column)
        {
            var wider = new string?[column + 1];
            grid[row].CopyTo(wider, 0);
            grid[row] = wider;
        }
        grid[row][column] = value;
    }

    // A cell's text on one line, its lines and blocks joined by <br>, the one line break a GFM
    // cell has.
    private string CellText(IReadOnlyList<Block> blocks)
    {
        var lines = new List<string>();
        CollectLines(blocks, lines);
        return string.Join("<br>", lines);
    }

    private void CollectLines(IReadOnlyList<Block> blocks, List<string> lines)
    {
        foreach (var block in blocks)
        {
            switch (block)
            {
                case Heading heading:
                    lines.AddRange(Lines(heading.Inlines));
                    break;
                case Paragraph paragraph:
                    lines.AddRange(Lines(paragraph.Inlines));
                    break;
                case Chart { Title: { } title }:
                    lines.AddRange(Lines(title));
                    break;
                case Media media:
                    lines.Add(RenderMedia(media));
                    break;
                case ListBlock list:
                    foreach (var item in list.Items)
                    {
                        CollectLines(item.Blocks, lines);
                    }
                    break;
                case Table table:
                    foreach (var cell in table.Rows.SelectMany(row => row.Cells))
                    {
                        CollectLines(cell.Blocks ?? [], lines);
                    }
                    break;
                default:
                    break;
            }
        }
    }

    // The lines of inlines, as the lines of their text, each reference a footnote reference in
    // its place. A reference stands in the text as a private-use character while the text is
    // split, trimmed, and escaped, none of which touch it, and then becomes its label.
    private List<string> Lines(IReadOnlyList<Inline> inlines)
    {
        var text = new StringBuilder();
        var labels = new List<string>();
        foreach (var inline in inlines)
        {
            var label = inline switch
            {
                TextRun run => null,
                FootnoteReference footnote => $"[^{NoteLabel((false, footnote.Id))}]",
                EndnoteReference endnote => $"[^{NoteLabel((true, endnote.Id))}]",
                CommentReference comment => $"[^c{CommentLabel(comment.Id)}]",
                _ => "",
            };
            if (inline is TextRun textRun)
            {
                text.Append(textRun.Text.Replace(Placeholder, ' '));
            }
            else if (label!.Length > 0 && labels.Count < PlaceholderCount)
            {
                text.Append((char)(Placeholder + labels.Count));
                labels.Add(label);
            }
        }
        return [.. Lines(text.ToString()).Select(line => Unplace(line, labels))];
    }

    // The private-use characters references stand in as; text that holds one has it as a space.
    private const char Placeholder = '\uE000';
    private const int PlaceholderCount = 0x1000;

    private static string Unplace(string line, List<string> labels)
    {
        if (labels.Count == 0)
        {
            return line;
        }
        var text = new StringBuilder(line.Length);
        foreach (var c in line)
        {
            var index = c - Placeholder;
            text.Append(index >= 0 && index < labels.Count ? labels[index] : c.ToString());
        }
        return text.ToString();
    }

    // The non-empty lines of a text, trimmed (leading spaces would make an indented code block)
    // and escaped, tabs as spaces.
    private static IEnumerable<string> Lines(string text) =>
        text.Replace('\t', ' ').Split('\n').Select(line => line.Trim()).Where(line => line.Length > 0).Select(Escape);

    // Backslash-escapes what Markdown would read as markup: emphasis, code, links, HTML,
    // entities, strikethrough, and table pipes anywhere, and at a line's start what would open
    // a heading, a block quote, a list, or a setext underline.
    private static string Escape(string line)
    {
        var text = new StringBuilder(line.Length + 8);
        foreach (var c in line)
        {
            if (c is '\\' or '`' or '*' or '_' or '[' or ']' or '<' or '>' or '~' or '|' or '&')
            {
                text.Append('\\');
            }
            text.Append(c);
        }
        if (text.Length > 0 && text[0] is '#' or '+' or '-' or '=')
        {
            text.Insert(0, '\\');
        }
        var digits = 0;
        while (digits < text.Length && char.IsAsciiDigit(text[digits]))
        {
            digits++;
        }
        if (digits is > 0 and <= 9 && digits < text.Length && text[digits] is '.' or ')')
        {
            text.Insert(digits, '\\');
        }
        return text.ToString();
    }
}
