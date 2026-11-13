using System.Text;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using SpikeAiPipeline.Office.Model;
using A = DocumentFormat.OpenXml.Drawing;
using C = DocumentFormat.OpenXml.Drawing.Charts;
using P = DocumentFormat.OpenXml.Presentation;

namespace SpikeAiPipeline.Office.Reading;

/// <summary>Reads a pptx package into the model: its slides in presentation order, hidden ones
/// included and marked, each with its shapes' text, tables, charts, and pictures in shape-tree
/// order, and its notes.</summary>
/// <remarks>Shapes are read in the order the slide's shape tree holds them, which is their
/// z-order, back to front; nothing is re-sorted by position. A group is read in place, its own
/// shapes in its tree's order. A placeholder holds only the slide's own text, so one left empty
/// is left out, and the prompt text its layout shows is never read. Content inside
/// mc:AlternateContent is not read, but for a chartEx chart's: its cached data, or with none
/// its fallback's picture.</remarks>
internal static class PptxReader
{
    public static Conversion Read(PresentationDocument package)
    {
        var presentationPart = package.PresentationPart ?? throw new OpenXmlPackageException("the package has no presentation part");
        var presentation = presentationPart.Presentation ?? throw new OpenXmlPackageException("the presentation part has no presentation");
        var media = new MediaStore();
        var blocks = new List<Block>();
        foreach (var slideId in presentation.SlideIdList?.Elements<P.SlideId>() ?? [])
        {
            var id = slideId.RelationshipId?.Value ?? throw new OpenXmlPackageException($"slide {slideId.Id} has no relationship");
            if (!presentationPart.TryGetPartById(id, out var part) || part is not SlidePart slidePart)
            {
                throw new OpenXmlPackageException($"slide {slideId.Id}'s part is missing");
            }
            var slide = slidePart.Slide ?? throw new OpenXmlPackageException($"slide {slideId.Id}'s part has no slide");
            var reader = new SlideReader(media, slidePart, presentation.DefaultTextStyle, blocks.Count);
            blocks.Add(new Model.Slide(blocks.Count + 1, slide.Show?.Value == false, reader.ReadShapes(slide.CommonSlideData?.ShapeTree))
            {
                Notes = ReadNotes(slidePart.NotesSlidePart) is { Count: > 0 } notes ? notes : null,
            });
        }
        return new Conversion(new Document(Document.CurrentSchema, SourceFormat.Pptx, blocks), media.Files);
    }

    // A notes page's text: its body placeholders' paragraphs, never the slide image, the slide
    // number, or a header, footer, or date placeholder. Their bullets come from their own list
    // style and the notes master's body placeholder and notes style.
    private static List<Block> ReadNotes(NotesSlidePart? part)
    {
        var blocks = new List<Block>();
        var master = part?.NotesMasterPart?.NotesMaster;
        var masterBody = master?.CommonSlideData?.ShapeTree?.Descendants<P.Shape>()
            .FirstOrDefault(s => Placeholder(s)?.Type?.Value == P.PlaceholderValues.Body);
        foreach (var shape in part?.NotesSlide?.CommonSlideData?.ShapeTree?.Descendants<P.Shape>() ?? [])
        {
            if (Placeholder(shape)?.Type?.Value == P.PlaceholderValues.Body && shape.TextBody is { } body)
            {
                blocks.AddRange(ReadText(body, [body.ListStyle, masterBody?.TextBody?.ListStyle, master?.NotesStyle]));
            }
        }
        return blocks;
    }

    private sealed class SlideReader(MediaStore media, SlidePart part, P.DefaultTextStyle? defaultTextStyle, int index)
    {
        private readonly SlideLayoutPart? layout = part.SlideLayoutPart;
        private readonly P.SlideMaster? master = part.SlideLayoutPart?.SlideMasterPart?.SlideMaster;

        public List<Block> ReadShapes(OpenXmlElement? tree)
        {
            var blocks = new List<Block>();
            foreach (var element in tree?.ChildElements ?? [])
            {
                switch (element)
                {
                    case P.Shape shape when shape.TextBody is { } body:
                        blocks.AddRange(ReadShapeText(shape, body));
                        break;
                    case P.GroupShape group:
                        blocks.AddRange(ReadShapes(group));
                        break;
                    case P.GraphicFrame frame:
                        if (ReadGraphic(frame.Graphic?.GraphicData) is { } graphic)
                        {
                            blocks.Add(graphic);
                        }
                        break;
                    case AlternateContent content when ChartReader.ChartEx(content, part) is { } chartEx:
                        if (chartEx.Chart is { } chart)
                        {
                            blocks.Add(chart);
                        }
                        else if (chartEx.Fallback is { } fallback)
                        {
                            blocks.AddRange(ReadShapes(fallback));
                        }
                        break;
                    case P.Picture picture:
                        var properties = picture.NonVisualPictureProperties?.NonVisualDrawingProperties;
                        var blip = picture.BlipFill?.Blip;
                        if ((blip?.Embed?.Value ?? blip?.Link?.Value) is { } id
                            && media.Picture(part, id, NonEmpty(properties?.Description?.Value), NonEmpty(properties?.Title?.Value)) is { } picked)
                        {
                            blocks.Add(picked with { Slide = index });
                        }
                        break;
                    default:
                        break;
                }
            }
            return blocks;
        }

        // A title placeholder's text is the slide's heading, its paragraphs its lines; any other
        // shape's paragraphs are paragraphs and lists.
        private List<Block> ReadShapeText(P.Shape shape, P.TextBody body)
        {
            var placeholder = Placeholder(shape);
            var type = placeholder is null ? (P.PlaceholderValues?)null : TypeOf(placeholder);
            if (type == P.PlaceholderValues.Title || type == P.PlaceholderValues.CenteredTitle)
            {
                var lines = body.Elements<A.Paragraph>().Select(TextOf).Where(t => !string.IsNullOrWhiteSpace(t)).ToList();
                return lines.Count == 0 ? [] : [new Heading(1, [new TextRun(string.Join("\n", lines))])];
            }
            return ReadText(body, Styles(body.ListStyle, placeholder, type));
        }

        // The list styles a shape's paragraphs inherit their bullets from, nearest first: the
        // shape's own; for a placeholder, its layout placeholder's, that one's master
        // placeholder's, and the master's title, body, or other text style by its type; for any
        // other shape, the master's other text style; and last the presentation's default.
        private List<OpenXmlElement?> Styles(A.ListStyle? own, P.PlaceholderShape? placeholder, P.PlaceholderValues? type)
        {
            List<OpenXmlElement?> styles = [own];
            var textStyles = master?.TextStyles;
            if (placeholder is not null && type is { } kind)
            {
                var layoutShape = Match(layout?.SlideLayout?.CommonSlideData?.ShapeTree, kind, placeholder.Index?.Value);
                var masterKind = MasterKind(layoutShape is not null && Placeholder(layoutShape) is { } layoutPlaceholder ? TypeOf(layoutPlaceholder) : kind);
                var masterShape = Match(master?.CommonSlideData?.ShapeTree, masterKind, null);
                styles.Add(layoutShape?.TextBody?.ListStyle);
                styles.Add(masterShape?.TextBody?.ListStyle);
                styles.Add(masterKind == P.PlaceholderValues.Title ? textStyles?.TitleStyle
                    : masterKind == P.PlaceholderValues.Body ? textStyles?.BodyStyle
                    : textStyles?.OtherStyle);
            }
            else
            {
                styles.Add(textStyles?.OtherStyle);
            }
            styles.Add(defaultTextStyle);
            return styles;
        }

        private Block? ReadGraphic(A.GraphicData? data)
        {
            if (data?.GetFirstChild<A.Table>() is { } table)
            {
                return ReadTable(table);
            }
            return data?.GetFirstChild<C.ChartReference>()?.Id?.Value is { } chartId
                && part.TryGetPartById(chartId, out var chartPart) && chartPart is ChartPart chart
                ? ChartReader.Read(chart)
                : null;
        }

        // A table's rows of cells. A cell that a merge covers, from the left (hMerge) or from
        // above (vMerge), is left out; the merge's first cell carries its spans.
        private static Table ReadTable(A.Table table)
        {
            var rows = new List<TableRow>();
            foreach (var row in table.Elements<A.TableRow>())
            {
                var cells = new List<TableCell>();
                foreach (var cell in row.Elements<A.TableCell>())
                {
                    if (cell.HorizontalMerge?.Value == true || cell.VerticalMerge?.Value == true)
                    {
                        continue;
                    }
                    var colSpan = cell.GridSpan?.Value ?? 1;
                    var rowSpan = cell.RowSpan?.Value ?? 1;
                    cells.Add(new TableCell(cell.TextBody is { } body ? ReadText(body, [body.ListStyle]) : [])
                    {
                        ColSpan = colSpan > 1 ? colSpan : null,
                        RowSpan = rowSpan > 1 ? rowSpan : null,
                    });
                }
                rows.Add(new TableRow(cells));
            }
            return new Table(rows);
        }
    }

    // A text body's paragraphs as paragraphs and lists. A paragraph is a list item when its
    // bullet is a character, a picture, or an auto-number (ordered), its own or else the first
    // the list styles give for its level, nearest first; with none, or buNone, it is a paragraph.
    // An auto-number counts from its startAt, 1 by default. An empty paragraph is spacing and is
    // left out.
    private static List<Block> ReadText(OpenXmlElement body, List<OpenXmlElement?> styles)
    {
        var blocks = new TextBlocks();
        foreach (var paragraph in body.Elements<A.Paragraph>())
        {
            var text = TextOf(paragraph);
            if (string.IsNullOrWhiteSpace(text))
            {
                continue;
            }
            var properties = paragraph.ParagraphProperties;
            var level = Math.Clamp(properties?.Level?.Value ?? 0, 0, 8);
            var bullet = BulletOf(properties) ?? styles.Select(style => BulletOf(LevelProperties(style, level))).FirstOrDefault(b => b is not null);
            var block = new Paragraph([new TextRun(text)]);
            if (bullet is { } kind && kind != Bullet.None)
            {
                var start = AutoNumberOf(properties) ?? styles.Select(style => AutoNumberOf(LevelProperties(style, level))).FirstOrDefault(n => n is not null);
                blocks.AddItem(level, kind == Bullet.Ordered, start?.StartAt?.Value ?? 1, block);
            }
            else
            {
                blocks.Add(block);
            }
        }
        return blocks.Build();
    }

    private enum Bullet
    {
        None,
        Unordered,
        Ordered,
    }

    // The bullet a paragraph's or a list style level's properties set, or null when they set
    // none, so it is inherited.
    private static Bullet? BulletOf(OpenXmlElement? properties)
    {
        foreach (var child in properties?.ChildElements ?? [])
        {
            switch (child)
            {
                case A.NoBullet:
                    return Bullet.None;
                case A.CharacterBullet or A.PictureBullet:
                    return Bullet.Unordered;
                case A.AutoNumberedBullet:
                    return Bullet.Ordered;
                default:
                    break;
            }
        }
        return null;
    }

    // The auto-number a paragraph's or a list style level's properties set, which the bullet
    // that applies has when it is ordered.
    private static A.AutoNumberedBullet? AutoNumberOf(OpenXmlElement? properties) =>
        properties?.ChildElements.FirstOrDefault(c => c is A.NoBullet or A.CharacterBullet or A.PictureBullet or A.AutoNumberedBullet) as A.AutoNumberedBullet;

    // A list style's properties for a level from 0: its lvl1pPr to lvl9pPr. The presentation's
    // default style and the masters' text styles hold the same elements as a shape's list style.
    private static OpenXmlElement? LevelProperties(OpenXmlElement? style, int level)
    {
        var name = $"lvl{level + 1}pPr";
        return style?.ChildElements.FirstOrDefault(e => e.LocalName == name && e.NamespaceUri == DrawingNamespace);
    }

    private const string DrawingNamespace = "http://schemas.openxmlformats.org/drawingml/2006/main";

    // A paragraph's text: its runs and fields, line breaks as \n.
    private static string TextOf(A.Paragraph paragraph)
    {
        var text = new StringBuilder();
        foreach (var child in paragraph.ChildElements)
        {
            switch (child)
            {
                case A.Run run:
                    text.Append(run.Text?.Text);
                    break;
                case A.Field field:
                    text.Append(field.Text?.Text);
                    break;
                case A.Break:
                    text.Append('\n');
                    break;
                default:
                    break;
            }
        }
        return text.ToString();
    }

    private static P.PlaceholderShape? Placeholder(P.Shape shape) =>
        shape.NonVisualShapeProperties?.ApplicationNonVisualDrawingProperties?.PlaceholderShape;

    // A placeholder's type; one without is a content (object) placeholder.
    private static P.PlaceholderValues TypeOf(P.PlaceholderShape placeholder) =>
        placeholder.Type?.Value ?? P.PlaceholderValues.Object;

    // The placeholder of a layout or master that a slide's or layout's placeholder takes after:
    // the one of the same index, or else the first of the same type, a centered title matching
    // a title.
    private static P.Shape? Match(OpenXmlElement? tree, P.PlaceholderValues type, uint? index)
    {
        var shapes = tree?.Descendants<P.Shape>().Where(s => Placeholder(s) is not null).ToList() ?? [];
        if (index is { } idx && shapes.FirstOrDefault(s => Placeholder(s)!.Index?.Value == idx) is { } byIndex)
        {
            return byIndex;
        }
        return shapes.FirstOrDefault(s => TypeOf(Placeholder(s)!) == type)
            ?? shapes.FirstOrDefault(s => IsTitle(TypeOf(Placeholder(s)!)) && IsTitle(type));
    }

    private static bool IsTitle(P.PlaceholderValues type) => type == P.PlaceholderValues.Title || type == P.PlaceholderValues.CenteredTitle;

    // The kind of master placeholder a layout placeholder takes after: the title, a date,
    // footer, slide number, or header placeholder, or else the body.
    private static P.PlaceholderValues MasterKind(P.PlaceholderValues type) =>
        IsTitle(type) ? P.PlaceholderValues.Title
        : type == P.PlaceholderValues.DateAndTime || type == P.PlaceholderValues.Footer
            || type == P.PlaceholderValues.SlideNumber || type == P.PlaceholderValues.Header ? type
        : P.PlaceholderValues.Body;

    private static string? NonEmpty(string? value) => string.IsNullOrWhiteSpace(value) ? null : value;

    // A text body's blocks as they are read. Consecutive list paragraphs make up lists: a deeper
    // level opens a list inside the last item, and at the same level an item continues the list
    // unless it is ordered where the list isn't, or the reverse. A paragraph ends every list. A
    // list starts at its first item's number.
    private sealed class TextBlocks
    {
        private readonly List<Block> blocks = [];
        private readonly List<(int Level, bool Ordered, int Start, List<List<Block>> Items)> open = [];

        public void Add(Block block)
        {
            while (open.Count > 0)
            {
                Close();
            }
            blocks.Add(block);
        }

        public void AddItem(int level, bool ordered, int start, Block paragraph)
        {
            while (open.Count > 0 && open[^1].Level > level)
            {
                Close();
            }
            if (open.Count > 0 && open[^1].Level == level && open[^1].Ordered != ordered)
            {
                Close();
            }
            if (open.Count == 0 || open[^1].Level < level)
            {
                open.Add((level, ordered, start, []));
            }
            open[^1].Items.Add([paragraph]);
        }

        public List<Block> Build()
        {
            while (open.Count > 0)
            {
                Close();
            }
            return blocks;
        }

        private void Close()
        {
            var list = open[^1];
            open.RemoveAt(open.Count - 1);
            var block = new ListBlock(list.Ordered, [.. list.Items.Select(item => new ListItem(item))])
            {
                Start = list.Ordered && list.Start != 1 ? list.Start : null,
            };
            if (open.Count > 0)
            {
                open[^1].Items[^1].Add(block);
            }
            else
            {
                blocks.Add(block);
            }
        }
    }
}
