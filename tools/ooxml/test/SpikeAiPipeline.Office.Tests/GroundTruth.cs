using System.Globalization;
using System.Text.Json.Nodes;
using SpikeAiPipeline.Office.Model;

namespace SpikeAiPipeline.Office.Tests;

/// <summary>A model in the ground truth's shape, element by element, so the two compare
/// directly: headings, paragraphs, list items with their nesting level and whether each starts its
/// list (and the first number of an ordered list that doesn't start at 1, on its first item),
/// tables with each cell's elements, in this same shape, whatever blocks it holds (and the grid
/// columns a row that starts late skips before it), pictures, and charts. A paragraph's or list
/// item's references are its marks: each
/// one's offset in the text, its kind, and its note's or comment's place, from 1, in the
/// document's list of them, so ids, which writers renumber, never enter the comparison. The
/// document's sections, notes, and comments are compared in the same shape. A sheet is its name,
/// whether it is hidden, and its elements, its table's rows each its number and its cells, each
/// cell its reference, its value's type and value, its formula or shared formula's anchor, and
/// its spans when merged. A slide is its number, whether it is hidden, its elements, and its
/// notes' elements when it has notes.</summary>
internal static class GroundTruth
{
    /// <summary>The ground truth's parts beside the body's elements.</summary>
    public static readonly string[] Parts = ["sections", "footnotes", "endnotes", "comments"];

    public static List<JsonObject> Elements(Document document)
    {
        var elements = new List<JsonObject>();
        AddBlocks(document, document.Blocks, elements);
        return elements;
    }

    /// <summary>The truth of one of the document's other parts, null when the document has none.</summary>
    public static JsonNode? Part(Document document, string name) => name switch
    {
        "sections" => document.Sections is { } sections
            ? new JsonArray([.. sections.Select(s => new JsonObject
            {
                ["headers"] = Stories(document, s.Headers),
                ["footers"] = Stories(document, s.Footers),
            })])
            : null,
        "footnotes" => Notes(document, document.Footnotes),
        "endnotes" => Notes(document, document.Endnotes),
        "comments" => document.Comments is { } comments
            ? new JsonArray([.. comments.Select(c => new JsonObject
            {
                ["author"] = c.Author,
                ["anchor"] = c.Anchor,
                ["resolved"] = c.Resolved,
                ["elements"] = Array(document, c.Blocks),
                ["replies"] = new JsonArray([.. c.Replies.Select(r => new JsonObject
                {
                    ["author"] = r.Author,
                    ["elements"] = Array(document, r.Blocks),
                })]),
            })])
            : null,
        _ => throw new ArgumentOutOfRangeException(nameof(name), name, "not a part of the ground truth"),
    };

    private static JsonArray Stories(Document document, IReadOnlyList<HeaderFooter>? stories) =>
        new([.. (stories ?? []).Select(s => new JsonObject
        {
            ["kind"] = s.Kind.ToString().ToLowerInvariant(),
            ["elements"] = Array(document, s.Blocks),
        })]);

    private static JsonArray? Notes(Document document, IReadOnlyList<Note>? notes) =>
        notes is null ? null : new([.. notes.Select(n => new JsonObject { ["elements"] = Array(document, n.Blocks) })]);

    private static JsonArray Array(Document document, IReadOnlyList<Block> blocks)
    {
        var elements = new List<JsonObject>();
        AddBlocks(document, blocks, elements);
        return new JsonArray([.. elements]);
    }

    private static void AddBlocks(Document document, IReadOnlyList<Block> blocks, List<JsonObject> elements, int listLevel = 0)
    {
        foreach (var block in blocks)
        {
            switch (block)
            {
                case Heading heading:
                    elements.Add(new JsonObject { ["kind"] = "heading", ["level"] = heading.Level, ["text"] = InlineText.Of(heading.Inlines) });
                    break;
                case Paragraph paragraph:
                    elements.Add(Marked(document, new JsonObject { ["kind"] = "paragraph", ["text"] = InlineText.Of(paragraph.Inlines) }, paragraph.Inlines));
                    break;
                case ListBlock list:
                    for (var i = 0; i < list.Items.Count; i++)
                    {
                        var item = list.Items[i];
                        var rest = item.Blocks;
                        if (item.Blocks is [Paragraph first, ..])
                        {
                            var listItem = new JsonObject
                            {
                                ["kind"] = "listItem",
                                ["level"] = listLevel,
                                ["ordered"] = list.Ordered,
                                ["start"] = i == 0,
                                ["text"] = InlineText.Of(first.Inlines),
                            };
                            if (i == 0 && list.Start is int startAt)
                            {
                                listItem["startAt"] = startAt;
                            }
                            elements.Add(Marked(document, listItem, first.Inlines));
                            rest = [.. item.Blocks.Skip(1)];
                        }
                        AddBlocks(document, rest, elements, listLevel + 1);
                    }
                    break;
                case Table table when table.Rows.Any(row => row.Row is not null):
                    elements.Add(new JsonObject { ["kind"] = "table", ["rows"] = SheetRows(table) });
                    break;
                case Table table:
                    elements.Add(new JsonObject { ["kind"] = "table", ["rows"] = Rows(document, table) });
                    break;
                case Sheet sheet:
                    elements.Add(new JsonObject
                    {
                        ["kind"] = "sheet",
                        ["name"] = sheet.Name,
                        ["hidden"] = sheet.Hidden,
                        ["elements"] = Array(document, sheet.Blocks),
                    });
                    break;
                case Slide slide:
                    var element = new JsonObject
                    {
                        ["kind"] = "slide",
                        ["number"] = slide.Number,
                        ["hidden"] = slide.Hidden,
                        ["elements"] = Array(document, slide.Blocks),
                    };
                    if (slide.Notes is { } notes)
                    {
                        element["notes"] = Array(document, notes);
                    }
                    elements.Add(element);
                    break;
                case Media media:
                    elements.Add(Picture(media));
                    break;
                case Chart chart:
                    elements.Add(ChartTruth(chart));
                    break;
                default:
                    elements.Add(new JsonObject { ["kind"] = block.GetType().Name });
                    break;
            }
        }
    }

    private static JsonArray Rows(Document document, Table table)
    {
        var rows = new JsonArray();
        foreach (var row in table.Rows)
        {
            var cells = new JsonArray();
            foreach (var cell in row.Cells)
            {
                var json = new JsonObject
                {
                    ["elements"] = Array(document, cell.Blocks ?? []),
                    ["colSpan"] = cell.ColSpan ?? 1,
                    ["rowSpan"] = cell.RowSpan ?? 1,
                };
                if (cell.SkipBefore is int skipBefore)
                {
                    json["skipBefore"] = skipBefore;
                }
                cells.Add(json);
            }
            rows.Add(cells);
        }
        return rows;
    }

    private static JsonArray SheetRows(Table table) =>
        new([.. table.Rows.Select(row => new JsonObject
        {
            ["row"] = row.Row,
            ["cells"] = new JsonArray([.. row.Cells.Select(SheetCell)]),
        })]);

    private static JsonObject SheetCell(TableCell cell)
    {
        var json = new JsonObject { ["ref"] = cell.Ref };
        if (cell.Value is { } value)
        {
            json["type"] = value.Type.ToString().ToLowerInvariant();
            json["value"] = value.Type switch
            {
                // Parsed from their shortest text, as the truth's numbers are, so 14 and 14.0 compare equal.
                CellValueType.Number => JsonNode.Parse(value.Number.ToString("R", CultureInfo.InvariantCulture)),
                CellValueType.Boolean => value.Boolean,
                _ => value.Text,
            };
        }
        if (cell.Formula is not null)
        {
            json["formula"] = cell.Formula;
        }
        if (cell.SharedFrom is not null)
        {
            json["sharedFrom"] = cell.SharedFrom;
        }
        if (cell.ColSpan is not null || cell.RowSpan is not null)
        {
            json["colSpan"] = cell.ColSpan ?? 1;
            json["rowSpan"] = cell.RowSpan ?? 1;
        }
        return json;
    }

    private static JsonObject Picture(Media media)
    {
        var picture = new JsonObject { ["kind"] = "media" };
        if (media.Name is not null)
        {
            picture["name"] = media.Name;
            picture["contentType"] = media.ContentType;
        }
        else
        {
            picture["target"] = media.Target;
        }
        if (media.Alt is not null)
        {
            picture["alt"] = media.Alt;
        }
        if (media.Title is not null)
        {
            picture["title"] = media.Title;
        }
        if (media.Section is not null)
        {
            picture["section"] = media.Section;
        }
        if (media.Sheet is not null)
        {
            picture["sheet"] = media.Sheet;
        }
        if (media.Slide is not null)
        {
            picture["slide"] = media.Slide;
        }
        return picture;
    }

    private static JsonObject ChartTruth(Chart chart)
    {
        var json = new JsonObject { ["kind"] = "chart", ["chartType"] = chart.ChartType };
        if (chart.Title is not null)
        {
            json["title"] = chart.Title;
        }
        json["series"] = new JsonArray([.. chart.Series.Select(s => new JsonObject
        {
            ["name"] = s.Name,
            ["categories"] = new JsonArray([.. s.Categories.Select(c => (JsonNode?)c)]),
            // Parsed from their shortest text, as the truth's numbers are, so 14 and 14.0 compare equal.
            ["values"] = new JsonArray([.. s.Values.Select(v => v is double d ? JsonNode.Parse(d.ToString("R", CultureInfo.InvariantCulture)) : null)]),
        })]);
        return json;
    }

    private static JsonObject Marked(Document document, JsonObject element, IReadOnlyList<Inline> inlines)
    {
        var marks = new JsonArray();
        var offset = 0;
        foreach (var inline in inlines)
        {
            (string Kind, int N)? mark = inline switch
            {
                FootnoteReference f => ("footnote", Place(document.Footnotes, n => n.Id == f.Id)),
                EndnoteReference e => ("endnote", Place(document.Endnotes, n => n.Id == e.Id)),
                CommentReference c => ("comment", Place(document.Comments, n => n.Id == c.Id)),
                _ => null,
            };
            if (inline is TextRun run)
            {
                offset += run.Text.Length;
            }
            else if (mark is var (kind, n))
            {
                marks.Add(new JsonObject { ["at"] = offset, ["kind"] = kind, ["n"] = n });
            }
        }
        if (marks.Count > 0)
        {
            element["marks"] = marks;
        }
        return element;
    }

    // A note's or comment's place in its list, from 1, or 0 when the list lacks it.
    private static int Place<T>(IReadOnlyList<T>? list, Func<T, bool> match)
    {
        for (var i = 0; i < (list?.Count ?? 0); i++)
        {
            if (match(list![i]))
            {
                return i + 1;
            }
        }
        return 0;
    }
}
