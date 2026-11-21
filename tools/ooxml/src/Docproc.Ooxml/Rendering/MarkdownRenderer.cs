using System.Globalization;
using System.Text;
using Docproc.Ooxml.Model;

namespace Docproc.Ooxml.Rendering;

// See docs/tool-contract.md, "docproc-ooxml".
internal sealed class MarkdownRenderer
{
    private readonly Dictionary<(bool Endnote, int Id), int> notes = [];
    private readonly Dictionary<int, int> comments = [];

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

    private readonly record struct Chunk(string Text, bool IsList, bool IsParagraph);

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
            if (block is Table docxTable && docxTable.Rows.All(row => row.Row is null))
            {
                foreach (var chart in CellCharts(docxTable.Rows.SelectMany(row => row.Cells).SelectMany(cell => cell.Blocks ?? [])))
                {
                    if (RenderChart(chart) is { Length: > 0 } chartText)
                    {
                        chunks.Add(new Chunk(chartText, false, false));
                    }
                }
            }
        }
        return chunks;
    }

    private static IEnumerable<Chart> CellCharts(IEnumerable<Block> blocks)
    {
        foreach (var block in blocks)
        {
            var charts = block switch
            {
                Chart chart => [chart],
                ListBlock list => CellCharts(list.Items.SelectMany(item => item.Blocks)),
                Table table => CellCharts(table.Rows.SelectMany(row => row.Cells).SelectMany(cell => cell.Blocks ?? [])),
                _ => [],
            };
            foreach (var chart in charts)
            {
                yield return chart;
            }
        }
    }

    private string RenderHeading(Heading heading)
    {
        var text = string.Join(" ", Lines(heading.Inlines));
        return text.Length == 0 ? "" : new string('#', Math.Clamp(heading.Level + headingShift, 1, 6)) + " " + text;
    }

    private string RenderParagraph(Paragraph paragraph) =>
        string.Join("\\\n", Lines(paragraph.Inlines));

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

    private string RenderSheet(Sheet sheet) =>
        string.Join("\n\n", [SheetHeading(sheet.Name, sheet.Hidden), .. RenderBlocks(sheet.Blocks).Select(c => c.Text)]);

    private static string SheetHeading(string name, bool hidden)
    {
        var text = string.Join(" ", Lines(name));
        return "# " + (text.Length > 0 ? text : "Sheet") + (hidden ? " (hidden)" : "");
    }

    internal static string SheetFragment(string name, bool hidden, IReadOnlyList<string> columns, bool follows)
    {
        var text = new StringBuilder();
        if (follows)
        {
            text.Append('\n');
        }
        text.Append(SheetHeading(name, hidden)).Append('\n');
        if (columns.Count > 0)
        {
            text.Append('\n').Append(Row(["", .. columns], columns.Count + 1)).Append('\n')
                .Append(Delimiter(columns.Count + 1)).Append('\n');
        }
        return text.ToString();
    }

    internal static string RowFragment(TableRow row, IReadOnlyList<string> columns) =>
        Row(SheetRowCells(row, columns), columns.Count + 1) + "\n";

    internal static string DrawingFragment(IReadOnlyList<Block> drawings) =>
        string.Concat(new MarkdownRenderer().RenderBlocks(drawings).Select(c => "\n" + c.Text + "\n"));

    internal static List<string> SheetColumns(Table table) =>
        [.. table.Rows.SelectMany(row => row.Cells).Select(cell => cell.Ref).OfType<string>()
            .Select(ColumnOf).Distinct().OrderBy(c => c.Length).ThenBy(c => c, StringComparer.Ordinal)];

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

    private static string RenderSheetTable(Table table)
    {
        var columns = SheetColumns(table);
        if (columns.Count == 0)
        {
            return "";
        }
        List<string?[]> grid = [["", .. columns]];
        grid.AddRange(table.Rows.Select(row => SheetRowCells(row, columns)));
        return GridTable(grid);
    }

    private static string?[] SheetRowCells(TableRow row, IReadOnlyList<string> columns)
    {
        var line = new string?[columns.Count + 1];
        line[0] = row.Row?.ToString(CultureInfo.InvariantCulture) ?? "";
        foreach (var cell in row.Cells)
        {
            if (cell.Ref is { } reference)
            {
                line[IndexOf(columns, ColumnOf(reference)) + 1] = SheetCellText(cell);
            }
        }
        return line;
    }

    private static int IndexOf(IReadOnlyList<string> columns, string column)
    {
        for (var i = 0; i < columns.Count; i++)
        {
            if (columns[i] == column)
            {
                return i;
            }
        }
        throw new ArgumentException($"column {column} is not one of the sheet's columns", nameof(column));
    }

    private static string ColumnOf(string reference) => new([.. reference.TakeWhile(char.IsAsciiLetter)]);

    private static string SheetCellText(TableCell cell)
    {
        var text = SheetCellValue(cell);
        return cell.Target is { } target && text.Length > 0 ? $"[{text}]({Target(target)})" : text;
    }

    private static string SheetCellValue(TableCell cell)
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
        var lines = new List<string> { Row(grid[0], columns), Delimiter(columns) };
        lines.AddRange(grid.Skip(1).Select(row => Row(row, columns)));
        return string.Join("\n", lines);
    }

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

    private static string RenderMedia(Media media)
    {
        var destination = Target(media.Name ?? media.Target ?? "");
        if (media.Kind != MediaKind.Picture)
        {
            var label = media.ProgId ?? (media.Source is { } part ? part[(part.LastIndexOf('/') + 1)..] : media.Name ?? "");
            return $"[{string.Join(" ", Lines(label))}]({destination})";
        }
        var text = media.Alt ?? media.Title;
        var alt = string.Join(" ", Lines(text ?? ""));
        var title = media.Title is { } own && own != text ? string.Join(" ", TrimmedLines(own)) : "";
        return title.Length > 0
            ? $"![{alt}]({destination} \"{Title(title).Replace("|", "\\|", StringComparison.Ordinal)}\")"
            : $"![{alt}]({destination})";
    }

    // See docs/tool-contract.md, "Common envelope".
    internal static string Title(string title)
    {
        var text = new StringBuilder(title.Length + 8);
        for (var i = 0; i < title.Length; i++)
        {
            var c = title[i];
            switch (c)
            {
                case '\\' or '"':
                    text.Append('\\').Append(c);
                    break;
                case '&' when ReferenceLike(title.AsSpan(i + 1)):
                    text.Append(@"\&");
                    break;
                case '\n':
                    text.Append("&#10;");
                    break;
                case '\r':
                    text.Append("&#13;");
                    break;
                case '\0':
                    text.Append('\uFFFD');
                    break;
                default:
                    text.Append(c);
                    break;
            }
        }
        return text.ToString();
    }

    private static string Target(string target) => Destination(target).Replace("|", "\\|", StringComparison.Ordinal);

    // See docs/tool-contract.md, "Common envelope".
    internal static string Destination(string target)
    {
        var angled = target.Any(c => char.IsWhiteSpace(c) || char.IsControl(c) || c is '<' or '>');
        var text = new StringBuilder(target.Length + 8);
        if (angled)
        {
            text.Append('<');
        }
        for (var i = 0; i < target.Length; i++)
        {
            var c = target[i];
            switch (c)
            {
                case '\\':
                    text.Append(@"\\");
                    break;
                case '&' when ReferenceLike(target.AsSpan(i + 1)):
                    text.Append(@"\&");
                    break;
                case '<' or '>' when angled:
                case '(' or ')' when !angled:
                    text.Append('\\').Append(c);
                    break;
                case '\n':
                    text.Append("%0A");
                    break;
                case '\r':
                    text.Append("%0D");
                    break;
                case '\0':
                    text.Append("%00");
                    break;
                default:
                    text.Append(c);
                    break;
            }
        }
        if (angled)
        {
            text.Append('>');
        }
        return text.ToString();
    }

    private static bool ReferenceLike(ReadOnlySpan<char> rest)
    {
        if (rest.StartsWith("#", StringComparison.Ordinal))
        {
            rest = rest[1..];
        }
        var name = 0;
        while (name < rest.Length && char.IsAsciiLetterOrDigit(rest[name]))
        {
            name++;
        }
        return name > 0 && name < rest.Length && rest[name] == ';';
    }

    private static string Delimiter(int columns) => "|" + string.Concat(Enumerable.Repeat(" --- |", columns));

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

    private List<string> Lines(IReadOnlyList<Inline> inlines)
    {
        var lines = new List<List<Segment>> { new() };
        string? link = null;
        foreach (var inline in inlines)
        {
            switch (inline)
            {
                case TextRun run:
                    var text = run.Text.Replace('\t', ' ');
                    var parts = (link is null ? text : text.Replace('\n', ' ')).Split('\n');
                    for (var i = 0; i < parts.Length; i++)
                    {
                        if (i > 0)
                        {
                            lines.Add([]);
                        }
                        if (parts[i].Length > 0)
                        {
                            lines[^1].Add(new Segment(SegmentKind.Text, parts[i]));
                        }
                    }
                    break;
                case FootnoteReference footnote:
                    lines[^1].Add(new Segment(SegmentKind.Label, $"[^{NoteLabel((false, footnote.Id))}]"));
                    break;
                case EndnoteReference endnote:
                    lines[^1].Add(new Segment(SegmentKind.Label, $"[^{NoteLabel((true, endnote.Id))}]"));
                    break;
                case CommentReference comment:
                    lines[^1].Add(new Segment(SegmentKind.Label, $"[^c{CommentLabel(comment.Id)}]"));
                    break;
                case HyperlinkStart start when link is null:
                    link = start.Target;
                    lines[^1].Add(new Segment(SegmentKind.LinkStart, ""));
                    break;
                case HyperlinkEnd when link is not null:
                    lines[^1].Add(new Segment(SegmentKind.LinkEnd, link));
                    link = null;
                    break;
                default:
                    break;
            }
        }
        if (link is not null)
        {
            lines[^1].Add(new Segment(SegmentKind.LinkEnd, link));
        }
        return [.. lines.Select(RenderLine).Where(line => line.Length > 0)];
    }

    private enum SegmentKind
    {
        Text,
        Label,
        LinkStart,
        LinkEnd,
    }

    private readonly record struct Segment(SegmentKind Kind, string Text);

    private static string RenderLine(List<Segment> line)
    {
        for (var i = 0; i < line.Count && line[i].Kind != SegmentKind.Label; i++)
        {
            if (line[i].Kind == SegmentKind.Text)
            {
                line[i] = line[i] with { Text = line[i].Text.TrimStart() };
                if (line[i].Text.Length > 0)
                {
                    break;
                }
            }
        }
        for (var i = line.Count - 1; i >= 0 && line[i].Kind != SegmentKind.Label; i--)
        {
            if (line[i].Kind == SegmentKind.Text)
            {
                line[i] = line[i] with { Text = line[i].Text.TrimEnd() };
                if (line[i].Text.Length > 0)
                {
                    break;
                }
            }
        }
        var text = new StringBuilder();
        for (var i = 0; i < line.Count; i++)
        {
            var segment = line[i];
            switch (segment.Kind)
            {
                case SegmentKind.Text:
                    text.Append(Escape(segment.Text, lineStart: text.Length == 0));
                    break;
                case SegmentKind.Label:
                    text.Append(segment.Text);
                    break;
                case SegmentKind.LinkStart:
                    var end = line.FindIndex(i, s => s.Kind == SegmentKind.LinkEnd);
                    if (line.Skip(i).Take(end - i).Any(s => s.Kind == SegmentKind.Label || (s.Kind == SegmentKind.Text && !string.IsNullOrWhiteSpace(s.Text))))
                    {
                        text.Append('[');
                    }
                    else
                    {
                        i = end;
                    }
                    break;
                default:
                    text.Append("](").Append(Target(segment.Text)).Append(')');
                    break;
            }
        }
        return text.ToString();
    }

    private static IEnumerable<string> Lines(string text) => TrimmedLines(text).Select(line => Escape(line));

    private static IEnumerable<string> TrimmedLines(string text) =>
        text.Replace('\t', ' ').Split('\n').Select(line => line.Trim()).Where(line => line.Length > 0);

    // See docs/tool-contract.md, "Common envelope".
    internal static string Escape(string line, bool lineStart = true)
    {
        var text = new StringBuilder(line.Length + 8);
        for (var i = 0; i < line.Length; i++)
        {
            var c = line[i];
            if (c is '\\' or '`' or '*' or '_' or '[' or ']' or '~' or '|' or '&' or '$' || (c == '<' && OpensHtml(line, i + 1)))
            {
                text.Append('\\');
            }
            text.Append(c);
        }
        if (!lineStart)
        {
            return text.ToString();
        }
        if (text.Length > 0 && text[0] is '#' or '+' or '-' or '=' or '>')
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

    private static bool OpensHtml(string line, int next) =>
        next >= line.Length
        || line[next] is '!' or '?'
        || char.IsAsciiLetter(line[next])
        || (line[next] == '/' && (next + 1 >= line.Length || char.IsAsciiLetter(line[next + 1])));
}
