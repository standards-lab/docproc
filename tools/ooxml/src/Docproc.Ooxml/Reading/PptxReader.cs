using System.Text;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Writing;
using A = DocumentFormat.OpenXml.Drawing;
using C = DocumentFormat.OpenXml.Drawing.Charts;
using P = DocumentFormat.OpenXml.Presentation;

namespace Docproc.Ooxml.Reading;

// See docs/tool-contract.md, "docproc-ooxml".
internal static class PptxReader
{
    public static Conversion Read(PresentationDocument package)
    {
        var presentationPart = package.PresentationPart ?? throw new OpenXmlPackageException("the package has no presentation part");
        var presentation = presentationPart.Presentation ?? throw new OpenXmlPackageException("the presentation part has no presentation");
        var media = new MediaStore();
        var warnings = new List<Warning>();
        var properties = PropertiesReader.Read(package, warnings);
        var unread = new UnreadKinds(warnings);
        var blocks = new List<Block>();
        foreach (var slideId in presentation.SlideIdList?.Elements<P.SlideId>() ?? [])
        {
            var id = slideId.RelationshipId?.Value ?? throw new OpenXmlPackageException($"slide {slideId.Id} has no relationship");
            if (!presentationPart.TryGetPartById(id, out var part) || part is not SlidePart slidePart)
            {
                throw new OpenXmlPackageException($"slide {slideId.Id}'s part is missing");
            }
            var slide = slidePart.Slide ?? throw new OpenXmlPackageException($"slide {slideId.Id}'s part has no slide");
            var place = $"slide {blocks.Count + 1}";
            var reader = new SlideReader(media, slidePart, presentation.DefaultTextStyle, blocks.Count, new TextScope(slidePart, unread, place));
            blocks.Add(new Model.Slide(blocks.Count + 1, slide.Show?.Value == false, reader.ReadShapes(slide.CommonSlideData?.ShapeTree))
            {
                Notes = ReadNotes(slidePart.NotesSlidePart, unread, place) is { Count: > 0 } notes ? notes : null,
            });
            if (slidePart.SlideCommentsPart?.CommentList?.HasChildren == true
                || slidePart.GetPartsOfType<PowerPointCommentPart>().Any(comments => comments.RootElement?.HasChildren == true))
            {
                unread.Found(place, UnreadKinds.Comments);
            }
        }
        return new Conversion(new Document(SourceFormat.Pptx, blocks) { Properties = properties }, media.Files) { Warnings = warnings };
    }

    private sealed record TextScope(OpenXmlPart Part, UnreadKinds Unread, string Place);

    private static List<Block> ReadNotes(NotesSlidePart? part, UnreadKinds unread, string place)
    {
        var blocks = new List<Block>();
        var master = part?.NotesMasterPart?.NotesMaster;
        var masterBody = master?.CommonSlideData?.ShapeTree?.Descendants<P.Shape>()
            .FirstOrDefault(s => Placeholder(s)?.Type?.Value == P.PlaceholderValues.Body);
        foreach (var shape in part?.NotesSlide?.CommonSlideData?.ShapeTree?.Descendants<P.Shape>() ?? [])
        {
            if (Placeholder(shape)?.Type?.Value == P.PlaceholderValues.Body && shape.TextBody is { } body)
            {
                blocks.AddRange(ReadText(body, [body.ListStyle, masterBody?.TextBody?.ListStyle, master?.NotesStyle], new TextScope(part!, unread, place)));
            }
        }
        return blocks;
    }

    private sealed class SlideReader(MediaStore media, SlidePart part, P.DefaultTextStyle? defaultTextStyle, int index, TextScope scope)
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
                        if (UnreadKinds.HoldsSmartArt(frame))
                        {
                            scope.Unread.Found(scope.Place, UnreadKinds.SmartArt);
                        }
                        if (ReadGraphic(frame.Graphic?.GraphicData) is { } graphic)
                        {
                            blocks.Add(graphic);
                        }
                        else
                        {
                            blocks.AddRange(ReadObject(frame.Graphic?.GraphicData));
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
                        if (ReadPicture(picture) is { } picked)
                        {
                            blocks.Add(picked);
                        }
                        break;
                    default:
                        break;
                }
            }
            return blocks;
        }

        private Media? ReadPicture(P.Picture picture)
        {
            var properties = picture.NonVisualPictureProperties?.NonVisualDrawingProperties;
            var blip = picture.BlipFill?.Blip;
            return (blip?.Embed?.Value ?? blip?.Link?.Value) is { } id
                && media.Picture(part, id, NonEmpty(properties?.Description?.Value), NonEmpty(properties?.Title?.Value)) is { } picked
                ? picked with { Slide = index }
                : null;
        }

        private List<Block> ReadObject(A.GraphicData? data)
        {
            var objects = data?.Descendants<P.OleObject>().ToList() ?? [];
            var blocks = new List<Block>();
            if (objects.SelectMany(o => o.Descendants<P.Picture>()).FirstOrDefault() is { } preview && ReadPicture(preview) is { } picture)
            {
                blocks.Add(picture);
            }
            if (objects.FirstOrDefault(o => o.Id?.Value is not null) is { } named
                && media.Payload(part, named.Id!.Value!, objects.Select(o => o.ProgId?.Value).FirstOrDefault(p => p is not null)) is { } payload)
            {
                blocks.Add(payload with { Slide = index });
            }
            return blocks;
        }

        private List<Block> ReadShapeText(P.Shape shape, P.TextBody body)
        {
            var placeholder = Placeholder(shape);
            var type = placeholder is null ? (P.PlaceholderValues?)null : TypeOf(placeholder);
            if (type == P.PlaceholderValues.Title || type == P.PlaceholderValues.CenteredTitle)
            {
                var lines = body.Elements<A.Paragraph>().Select(p => InlinesOf(p, scope)).Where(i => !string.IsNullOrWhiteSpace(InlineText.Of(i))).ToList();
                return lines.Count == 0 ? [] : [new Heading(1, Joined(lines))];
            }
            return ReadText(body, Styles(body.ListStyle, placeholder, type), scope);
        }

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
                return ReadTable(table, scope);
            }
            return data?.GetFirstChild<C.ChartReference>()?.Id?.Value is { } chartId
                && part.TryGetPartById(chartId, out var chartPart) && chartPart is ChartPart chart
                ? ChartReader.Read(chart)
                : null;
        }

        private static Table ReadTable(A.Table table, TextScope scope)
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
                    cells.Add(new TableCell(cell.TextBody is { } body ? ReadText(body, [body.ListStyle], scope) : [])
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

    private static List<Block> ReadText(OpenXmlElement body, List<OpenXmlElement?> styles, TextScope scope)
    {
        var blocks = new TextBlocks();
        foreach (var paragraph in body.Elements<A.Paragraph>())
        {
            var inlines = InlinesOf(paragraph, scope);
            if (string.IsNullOrWhiteSpace(InlineText.Of(inlines)))
            {
                continue;
            }
            var properties = paragraph.ParagraphProperties;
            var level = Math.Clamp(properties?.Level?.Value ?? 0, 0, 8);
            var bullet = BulletOf(properties) ?? styles.Select(style => BulletOf(LevelProperties(style, level))).FirstOrDefault(b => b is not null);
            var block = new Paragraph(inlines);
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

    private static A.AutoNumberedBullet? AutoNumberOf(OpenXmlElement? properties) =>
        properties?.ChildElements.FirstOrDefault(c => c is A.NoBullet or A.CharacterBullet or A.PictureBullet or A.AutoNumberedBullet) as A.AutoNumberedBullet;

    private static OpenXmlElement? LevelProperties(OpenXmlElement? style, int level)
    {
        var name = $"lvl{level + 1}pPr";
        return style?.ChildElements.FirstOrDefault(e => e.LocalName == name && e.NamespaceUri == DrawingNamespace);
    }

    private const string DrawingNamespace = "http://schemas.openxmlformats.org/drawingml/2006/main";

    private static List<Inline> InlinesOf(A.Paragraph paragraph, TextScope scope)
    {
        var inlines = new List<Inline>();
        var text = new StringBuilder();
        string? open = null;
        void Flush()
        {
            if (text.Length > 0)
            {
                inlines.Add(new TextRun(text.ToString()));
                text.Clear();
            }
        }
        void Link(OpenXmlElement? properties)
        {
            var target = MediaStore.Hyperlink(scope.Part, properties?.GetFirstChild<A.HyperlinkOnClick>()?.Id?.Value);
            if (target == open)
            {
                return;
            }
            Flush();
            if (open is not null)
            {
                inlines.Add(new HyperlinkEnd());
            }
            if (target is not null)
            {
                inlines.Add(new HyperlinkStart(target));
            }
            open = target;
        }
        foreach (var child in paragraph.ChildElements)
        {
            switch (child)
            {
                case A.Run run:
                    Link(run.RunProperties);
                    text.Append(run.Text?.Text);
                    break;
                case A.Field field:
                    Link(field.GetFirstChild<A.RunProperties>());
                    text.Append(field.Text?.Text);
                    break;
                case A.Break:
                    text.Append('\n');
                    break;
                case AlternateContent content when UnreadKinds.HoldsEquation(content):
                    scope.Unread.Found(scope.Place, UnreadKinds.Equations);
                    break;
                default:
                    break;
            }
        }
        Link(null);
        Flush();
        return inlines;
    }

    private static List<Inline> Joined(List<List<Inline>> paragraphs)
    {
        var inlines = new List<Inline>();
        foreach (var paragraph in paragraphs)
        {
            foreach (var inline in inlines.Count > 0 ? [new TextRun("\n"), .. paragraph] : paragraph)
            {
                if (inline is TextRun run && inlines.Count > 0 && inlines[^1] is TextRun last)
                {
                    inlines[^1] = new TextRun(last.Text + run.Text);
                }
                else
                {
                    inlines.Add(inline);
                }
            }
        }
        return inlines;
    }

    private static P.PlaceholderShape? Placeholder(P.Shape shape) =>
        shape.NonVisualShapeProperties?.ApplicationNonVisualDrawingProperties?.PlaceholderShape;

    private static P.PlaceholderValues TypeOf(P.PlaceholderShape placeholder) =>
        placeholder.Type?.Value ?? P.PlaceholderValues.Object;

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

    private static P.PlaceholderValues MasterKind(P.PlaceholderValues type) =>
        IsTitle(type) ? P.PlaceholderValues.Title
        : type == P.PlaceholderValues.DateAndTime || type == P.PlaceholderValues.Footer
            || type == P.PlaceholderValues.SlideNumber || type == P.PlaceholderValues.Header ? type
        : P.PlaceholderValues.Body;

    private static string? NonEmpty(string? value) => string.IsNullOrWhiteSpace(value) ? null : value;

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
