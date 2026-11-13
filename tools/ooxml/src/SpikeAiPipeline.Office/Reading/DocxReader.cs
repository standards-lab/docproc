using System.Text;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using SpikeAiPipeline.Office.Model;
using A = DocumentFormat.OpenXml.Drawing;
using C = DocumentFormat.OpenXml.Drawing.Charts;
using DW = DocumentFormat.OpenXml.Drawing.Wordprocessing;
using V = DocumentFormat.OpenXml.Vml;
using W = DocumentFormat.OpenXml.Wordprocessing;

namespace SpikeAiPipeline.Office.Reading;

/// <summary>Reads a docx package into the model, its tracked changes accepted: the body, each
/// section's headers and footers, the notes the text references, and the comments.</summary>
internal static partial class DocxReader
{
    public static Conversion Read(WordprocessingDocument package)
    {
        var main = package.MainDocumentPart ?? throw new OpenXmlPackageException("the package has no main document part");
        var body = main.Document?.Body ?? throw new OpenXmlPackageException("the main document part has no body");
        var context = new DocxContext(
            main,
            new Styles(main.StyleDefinitionsPart?.Styles),
            new Numbering(main.NumberingDefinitionsPart?.Numbering));
        var blocks = new BodyReader(context, main, isBody: true).ReadBlocks(body);
        var sections = ReadSections(context, body);
        var comments = context.Comments.Build(context);
        return new Conversion(
            new Document(Document.CurrentSchema, SourceFormat.Docx, blocks)
            {
                Sections = sections.Any(s => s.Headers is not null || s.Footers is not null) ? sections : null,
                Footnotes = context.Footnotes.Read is { Count: > 0 } footnotes ? footnotes : null,
                Endnotes = context.Endnotes.Read is { Count: > 0 } endnotes ? endnotes : null,
                Comments = comments.Count > 0 ? comments : null,
            },
            context.Media.Files)
        {
            Warnings = context.Warnings,
        };
    }

    // Each section's headers and footers, in body order: a section is a paragraph's w:sectPr, and
    // the body's own w:sectPr is the last. A section without a reference of a kind inherits the
    // previous section's; the first-page kind counts only with a title page, the even-page kind
    // only when the settings turn even and odd headers on. A header or footer is read for each
    // section that shows it, so its media are placed in that section. A reference of a type no
    // header or footer has (Word's are default, first, and even) is skipped with a warning.
    private static List<Section> ReadSections(DocxContext context, W.Body body)
    {
        var evenAndOdd = context.Main.DocumentSettingsPart?.Settings?.GetFirstChild<W.EvenAndOddHeaders>() is { } setting && OnOff(setting.Val);
        var properties = body.Descendants<W.Paragraph>()
            .Select(p => p.ParagraphProperties?.SectionProperties)
            .OfType<W.SectionProperties>()
            .ToList();
        if (body.Elements<W.SectionProperties>().LastOrDefault() is { } last)
        {
            properties.Add(last);
        }
        var headers = new Dictionary<HeaderFooterKind, string>();
        var footers = new Dictionary<HeaderFooterKind, string>();
        var sections = new List<Section>();
        for (var i = 0; i < properties.Count; i++)
        {
            var sectPr = properties[i];
            foreach (var reference in sectPr.Elements<W.HeaderReference>())
            {
                if (Valid(context, reference.Type, "header", i))
                {
                    Inherit(headers, KindOf(reference.Type?.Value), reference.Id?.Value);
                }
            }
            foreach (var reference in sectPr.Elements<W.FooterReference>())
            {
                if (Valid(context, reference.Type, "footer", i))
                {
                    Inherit(footers, KindOf(reference.Type?.Value), reference.Id?.Value);
                }
            }
            List<HeaderFooterKind> kinds = [HeaderFooterKind.Default];
            if (sectPr.GetFirstChild<W.TitlePage>() is { } titlePage && OnOff(titlePage.Val))
            {
                kinds.Add(HeaderFooterKind.First);
            }
            if (evenAndOdd)
            {
                kinds.Add(HeaderFooterKind.Even);
            }
            context.Section = i;
            sections.Add(new Section(Stories(context, headers, kinds), Stories(context, footers, kinds)));
        }
        context.Section = null;
        return sections;
    }

    private static bool Valid(DocxContext context, EnumValue<W.HeaderFooterValues>? type, string story, int section)
    {
        // An attribute outside its type's values has text but no value.
        if (type is null || type.HasValue)
        {
            return true;
        }
        context.Warnings.Add($"a {story} reference of type \"{type.InnerText}\" in section {section + 1} was skipped: no {story} has that type");
        return false;
    }

    private static void Inherit(Dictionary<HeaderFooterKind, string> stories, HeaderFooterKind kind, string? relationshipId)
    {
        if (relationshipId is not null)
        {
            stories[kind] = relationshipId;
        }
    }

    private static HeaderFooterKind KindOf(W.HeaderFooterValues? type) =>
        type == W.HeaderFooterValues.First ? HeaderFooterKind.First
        : type == W.HeaderFooterValues.Even ? HeaderFooterKind.Even
        : HeaderFooterKind.Default;

    private static List<HeaderFooter>? Stories(DocxContext context, Dictionary<HeaderFooterKind, string> stories, List<HeaderFooterKind> kinds)
    {
        var result = new List<HeaderFooter>();
        foreach (var kind in kinds)
        {
            if (stories.TryGetValue(kind, out var id)
                && context.Main.TryGetPartById(id, out var part)
                && part.RootElement is W.Header or W.Footer)
            {
                var blocks = new BodyReader(context, part, isBody: false).ReadBlocks(part.RootElement);
                if (blocks.Count > 0)
                {
                    result.Add(new HeaderFooter(kind, blocks));
                }
            }
        }
        return result.Count > 0 ? result : null;
    }

    // An on/off property's value: present with no value is on.
    private static bool OnOff(OnOffValue? value) => value?.Value ?? true;

    // The blocks of one story part: the body, a header or footer, a note, or a comment. Only the
    // body counts sections and collects comment anchors.
    private sealed class BodyReader(DocxContext context, OpenXmlPart part, bool isBody)
    {
        /// <summary>The deepest a table may be nested, the outermost table being 1. A table deeper
        /// than this refuses the document as it is reached, before any output.</summary>
        public const int MaxTableDepth = 8;

        // How deep the table being read is nested, 0 outside any.
        private int tableDepth;

        // The story's list counts, as Word numbers its paragraphs: by abstract numbering
        // definition, each level's last number, and the numbering instances and levels already
        // counted, whose start overrides have taken effect.
        private readonly Dictionary<int, Dictionary<int, int>> counts = [];
        private readonly HashSet<(int NumId, int Level)> counted = [];

        // The blocks of a container. Tracked changes are read in their accepted state: a paragraph
        // whose mark was deleted (or moved away) runs on into the next paragraph, which keeps its
        // own properties, as the mark that survives is the next one's. A table ends that: the text
        // carried to it stays a paragraph of its own before the table.
        public List<Block> ReadBlocks(OpenXmlElement container)
        {
            var blocks = new BlockList();
            InlineBuilder? carried = null;
            foreach (var element in BlockElements(container))
            {
                // A comment's range can start or end between paragraphs; its anchor's text is
                // the paragraphs between, and its mark is where its reference is.
                if (element is W.CommentRangeStart rangeStart)
                {
                    if (isBody)
                    {
                        context.Comments.Start(rangeStart.Id?.Value);
                    }
                    continue;
                }
                if (element is W.CommentRangeEnd rangeEnd)
                {
                    if (isBody)
                    {
                        context.Comments.Close(rangeEnd.Id?.Value);
                    }
                    continue;
                }
                if (element is W.Table table)
                {
                    if (carried is not null)
                    {
                        AddCarried(blocks, carried);
                        carried = null;
                    }
                    blocks.Add(ReadTable(table));
                    continue;
                }
                var paragraph = (W.Paragraph)element;
                var inlines = carried ?? new InlineBuilder();
                carried = null;
                ReadInlines(paragraph, inlines);
                if (MarkRemoved(paragraph))
                {
                    carried = inlines;
                    continue;
                }
                AddParagraph(blocks, paragraph, inlines);
                if (isBody)
                {
                    context.Comments.EndParagraph();
                    if (paragraph.ParagraphProperties?.SectionProperties is not null)
                    {
                        context.Section++;
                    }
                }
            }
            if (carried is not null)
            {
                AddCarried(blocks, carried);
            }
            return blocks.Build();
        }

        private static void AddCarried(BlockList blocks, InlineBuilder carried)
        {
            if (!carried.IsBlank)
            {
                blocks.Add(new Paragraph(carried.Build()));
            }
            foreach (var block in carried.Blocks)
            {
                blocks.Add(block);
            }
        }

        private void AddParagraph(BlockList blocks, W.Paragraph paragraph, InlineBuilder inlines)
        {
            var properties = paragraph.ParagraphProperties;
            var styleId = properties?.ParagraphStyleId?.Val?.Value;
            if (ListLevel(properties, styleId) is var (numId, level) && context.Numbering.Resolve(numId, level, context.Styles) is { } numberingLevel)
            {
                // A list item is kept even when empty: it is still an item of its list. Its
                // pictures and charts are blocks of the item, after its paragraph.
                blocks.AddItem(level, numId, numberingLevel, Number(numId, level, numberingLevel), [new Paragraph(inlines.Build()), .. inlines.Blocks]);
                return;
            }
            // Empty paragraphs and headings are spacing, not text. A paragraph's pictures and
            // charts follow it as blocks of their own, in the order they occur.
            if (!inlines.IsBlank)
            {
                blocks.Add(HeadingLevel(properties, styleId) is int headingLevel
                    ? new Heading(headingLevel, inlines.Build())
                    : new Paragraph(inlines.Build()));
            }
            foreach (var block in inlines.Blocks)
            {
                blocks.Add(block);
            }
        }

        // The level a paragraph's own outline level gives it, or else its style's: an outline level
        // in the style chain, or a style named "heading N". Outline level 9 is body text.
        private int? HeadingLevel(W.ParagraphProperties? properties, string? styleId)
        {
            if (properties?.OutlineLevel?.Val?.Value is int own)
            {
                return own < 9 ? own + 1 : null;
            }
            foreach (var style in context.Styles.Chain(styleId))
            {
                if (style.StyleParagraphProperties?.OutlineLevel?.Val?.Value is int outline)
                {
                    return outline < 9 ? outline + 1 : null;
                }
                var name = style.StyleName?.Val?.Value;
                if (name is not null
                    && name.StartsWith("heading ", StringComparison.OrdinalIgnoreCase)
                    && int.TryParse(name.AsSpan("heading ".Length), out var level)
                    && level is >= 1 and <= 9)
                {
                    return level;
                }
            }
            return null;
        }

        // A list paragraph's number, as Word counts it: one more than the last of its level in its
        // abstract definition, which every instance of the definition shares, or the level's
        // start when the level has none yet. An instance that overrides the level's start sets
        // it the first time the instance numbers that level. Numbering a level restarts the
        // levels below it.
        private int Number(int numId, int level, NumberingLevel numbering)
        {
            if (!counts.TryGetValue(numbering.AbstractId, out var levels))
            {
                counts[numbering.AbstractId] = levels = [];
            }
            if (counted.Add((numId, level)) && numbering.Restarts)
            {
                levels.Remove(level);
            }
            var number = levels.TryGetValue(level, out var last) ? last + 1 : numbering.Start;
            levels[level] = number;
            foreach (var deeper in levels.Keys.Where(l => l > level).ToList())
            {
                levels.Remove(deeper);
            }
            return number;
        }

        // The numbering instance and level of a list paragraph, from its own w:numPr or its
        // style's, the level within Word's nine. Numbering instance 0 removes numbering.
        private (int NumId, int Level)? ListLevel(W.ParagraphProperties? properties, string? styleId)
        {
            var own = properties?.NumberingProperties;
            int? numId = own?.NumberingId?.Val?.Value;
            int? level = own?.NumberingLevelReference?.Val?.Value;
            if (numId is null)
            {
                foreach (var style in context.Styles.Chain(styleId))
                {
                    if (style.StyleParagraphProperties?.NumberingProperties is { NumberingId.Val.Value: var styleNumId } styleNumbering)
                    {
                        numId = styleNumId;
                        level ??= styleNumbering.NumberingLevelReference?.Val?.Value;
                        break;
                    }
                }
            }
            return numId is null or 0 ? null : (numId.Value, Math.Clamp(level ?? 0, 0, 8));
        }

        // A table's rows and cells, through the content controls and custom XML that can wrap
        // either. A horizontal merge is a cell's w:gridSpan; a vertical merge is a w:vMerge
        // restart cell and the continue cells below it, which fold into its row span. A row that
        // starts late (w:gridBefore) gives its first cell the columns it skips; one that ends
        // early (w:gridAfter) needs nothing, its last columns simply empty. A table nested past
        // the limit refuses the document.
        private Table ReadTable(W.Table table)
        {
            if (++tableDepth > MaxTableDepth)
            {
                throw new ContentRefusedException($"tables nested {tableDepth} deep (limit {MaxTableDepth}), at {Location(table)}");
            }
            try
            {
                return ReadRows(table);
            }
            finally
            {
                tableDepth--;
            }
        }

        // Where an element is, for a person to find it: the story and the number, from 1 in
        // document order, of the first paragraph at or after it.
        private string Location(OpenXmlElement element)
        {
            var root = part.RootElement ?? element;
            var paragraphs = root.Descendants<W.Paragraph>().ToList();
            var first = element.Descendants<W.Paragraph>().FirstOrDefault();
            var number = (first is null ? paragraphs.Count : paragraphs.IndexOf(first)) + 1;
            return isBody ? $"body paragraph {number}" : $"paragraph {number} of {part.Uri.OriginalString}";
        }

        private Table ReadRows(W.Table table)
        {
            var rows = new List<List<CellBuilder>>();
            var mergeAnchors = new Dictionary<int, CellBuilder>();
            foreach (var row in RowElements(table))
            {
                var cells = new List<CellBuilder>();
                var column = row.TableRowProperties?.GetFirstChild<W.GridBefore>()?.Val?.Value ?? 0;
                var skip = column;
                foreach (var cell in CellElements(row))
                {
                    var cellProperties = cell.TableCellProperties;
                    var span = cellProperties?.GridSpan?.Val?.Value ?? 1;
                    var merge = cellProperties?.VerticalMerge;
                    var continues = merge is not null && (merge.Val is null || merge.Val.Value == W.MergedCellValues.Continue);
                    if (continues && mergeAnchors.TryGetValue(column, out var anchor))
                    {
                        anchor.RowSpan++;
                    }
                    else
                    {
                        var builder = new CellBuilder(ReadBlocks(cell), span, skip);
                        skip = 0;
                        cells.Add(builder);
                        if (merge?.Val?.Value == W.MergedCellValues.Restart)
                        {
                            mergeAnchors[column] = builder;
                        }
                        else
                        {
                            mergeAnchors.Remove(column);
                        }
                    }
                    column += span;
                }
                rows.Add(cells);
            }
            return new Table([.. rows.Select(cells => new TableRow([.. cells.Select(c => c.Build())]))]);
        }

        // A paragraph's text in its accepted state. The runs are walked explicitly: the SDK's
        // InnerText includes w:delText, and every w:t would include a move's source, so insertions
        // and a move's destination are read through, deletions and a move's source skipped. A
        // comment's anchored range ends in a reference to it.
        private void ReadInlines(OpenXmlElement container, InlineBuilder inlines)
        {
            foreach (var child in container.ChildElements)
            {
                switch (child)
                {
                    case W.Run run:
                        if (!ShowsPlaceholder(run))
                        {
                            ReadRun(run, inlines);
                        }
                        break;
                    case W.DeletedRun or W.MoveFromRun:
                        break;
                    case W.InsertedRun or W.MoveToRun or W.Hyperlink or W.SimpleField or W.CustomXmlRun
                        or W.BidirectionalOverride or W.BidirectionalEmbedding:
                        ReadInlines(child, inlines);
                        break;
                    // A smart tag wraps runs as custom XML does, but the SDK has no type for it, so
                    // its content is unknown elements: it is read as the custom XML it is like.
                    case OpenXmlUnknownElement { LocalName: "smartTag", NamespaceUri: WordprocessingNamespace } smartTag:
                        if (!ShowsPlaceholder(smartTag))
                        {
                            ReadInlines(new W.CustomXmlRun { InnerXml = smartTag.InnerXml }, inlines);
                        }
                        break;
                    case W.SdtRun { SdtContentRun: { } content }:
                        ReadInlines(content, inlines);
                        break;
                    case W.CommentRangeStart start when isBody:
                        context.Comments.Start(start.Id?.Value);
                        break;
                    case W.CommentRangeEnd end when isBody:
                        context.Comments.Close(end.Id?.Value);
                        if (context.Comments.Point(end.Id?.Value) is int ended)
                        {
                            inlines.Add(new CommentReference(ended));
                        }
                        break;
                    default:
                        break;
                }
            }
        }

        // A content control showing its placeholder (Word's "Choose an item.") holds a prompt, not
        // the document's text, so its runs are left out wherever the control is: around runs,
        // paragraphs, cells, or rows.
        private static bool ShowsPlaceholder(OpenXmlElement run) =>
            run.Ancestors<W.SdtElement>().Any(sdt => sdt.SdtProperties?.GetFirstChild<W.ShowingPlaceholder>() is { } showing && OnOff(showing.Val));

        // A run's text, references, pictures, and charts. Field codes are instructions, not text;
        // a field's result is ordinary runs. A ruby's base text is the text; its annotation, a
        // reading aid above it, is left out. An embedded OLE object is never extracted, but the
        // picture Word shows in its place is a picture of the document. A chartEx chart, in an
        // mc:AlternateContent, is its cached data, or with none its fallback's picture; any
        // other alternate content is not read.
        private void ReadRun(OpenXmlElement run, InlineBuilder inlines)
        {
            foreach (var child in run.ChildElements)
            {
                switch (child)
                {
                    case W.Text text:
                        Append(inlines, text.Text);
                        break;
                    case W.TabChar or W.PositionalTab:
                        Append(inlines, "\t");
                        break;
                    case W.Break or W.CarriageReturn:
                        Append(inlines, "\n");
                        break;
                    case W.NoBreakHyphen:
                        Append(inlines, "-");
                        break;
                    case W.FootnoteReference reference when reference.Id?.Value is long id:
                        if (context.Footnotes.Reference(context, (int)id))
                        {
                            inlines.Add(new FootnoteReference((int)id));
                        }
                        break;
                    case W.EndnoteReference reference when reference.Id?.Value is long id:
                        if (context.Endnotes.Reference(context, (int)id))
                        {
                            inlines.Add(new EndnoteReference((int)id));
                        }
                        break;
                    // A comment with no range is marked where its reference is.
                    case W.CommentReference reference when isBody:
                        if (context.Comments.Point(reference.Id?.Value) is int pointed)
                        {
                            inlines.Add(new CommentReference(pointed));
                        }
                        break;
                    case W.Drawing drawing:
                        ReadDrawing(drawing, inlines);
                        break;
                    case W.Picture or W.EmbeddedObject:
                        ReadVmlPicture(child, inlines);
                        break;
                    case W.Ruby { RubyBase: { } rubyBase }:
                        ReadInlines(rubyBase, inlines);
                        break;
                    case AlternateContent content when ChartReader.ChartEx(content, part) is { } chartEx:
                        if (chartEx.Chart is { } chart)
                        {
                            inlines.Blocks.Add(chart);
                        }
                        else if (chartEx.Fallback is { } fallback)
                        {
                            ReadRun(fallback, inlines);
                        }
                        break;
                    default:
                        break;
                }
            }
        }

        private void Append(InlineBuilder inlines, string text)
        {
            inlines.Append(text);
            if (isBody)
            {
                context.Comments.Append(text);
            }
        }

        // A DrawingML drawing's pictures and charts, in document order, with the drawing's
        // description and title. A picture's blip embeds a part or links an external target.
        private void ReadDrawing(W.Drawing drawing, InlineBuilder inlines)
        {
            var properties = drawing.Descendants<DW.DocProperties>().FirstOrDefault();
            var alt = NonEmpty(properties?.Description?.Value);
            var title = NonEmpty(properties?.Title?.Value);
            foreach (var element in drawing.Descendants())
            {
                switch (element)
                {
                    case A.Blip blip when (blip.Embed?.Value ?? blip.Link?.Value) is { } id:
                        if (ReadPicture(id, alt, title) is { } media)
                        {
                            inlines.Blocks.Add(media);
                        }
                        break;
                    case C.ChartReference chart when chart.Id?.Value is { } id:
                        if (part.TryGetPartById(id, out var chartPart) && chartPart is ChartPart charts && ChartReader.Read(charts) is { } read)
                        {
                            inlines.Blocks.Add(read);
                        }
                        break;
                    default:
                        break;
                }
            }
        }

        // A VML picture's image, with its title as the alternative text, VML having no other.
        private void ReadVmlPicture(OpenXmlElement picture, InlineBuilder inlines)
        {
            foreach (var image in picture.Descendants<V.ImageData>())
            {
                if (image.RelationshipId?.Value is { } id && ReadPicture(id, NonEmpty(image.Title?.Value), null) is { } media)
                {
                    inlines.Blocks.Add(media);
                }
            }
        }

        private Media? ReadPicture(string relationshipId, string? alt, string? title) =>
            context.Media.Picture(part, relationshipId, alt, title) is { } media ? media with { Section = context.Section } : null;
    }

    // The paragraphs and tables of a container in document order, with the comment ranges that
    // start or end between them, through the content controls and custom XML that can wrap them.
    private static IEnumerable<OpenXmlElement> BlockElements(OpenXmlElement container)
    {
        foreach (var child in container.ChildElements)
        {
            switch (child)
            {
                case W.Paragraph or W.Table or W.CommentRangeStart or W.CommentRangeEnd:
                    yield return child;
                    break;
                case W.SdtBlock { SdtContentBlock: { } content }:
                    foreach (var element in BlockElements(content))
                    {
                        yield return element;
                    }
                    break;
                case W.CustomXmlBlock custom:
                    foreach (var element in BlockElements(custom))
                    {
                        yield return element;
                    }
                    break;
                default:
                    break;
            }
        }
    }

    // A table's rows, through the content controls and custom XML that can wrap them.
    private static IEnumerable<W.TableRow> RowElements(OpenXmlElement container)
    {
        foreach (var child in container.ChildElements)
        {
            var rows = child switch
            {
                W.TableRow row => [row],
                W.SdtRow { SdtContentRow: { } content } => RowElements(content),
                W.CustomXmlRow custom => RowElements(custom),
                _ => [],
            };
            foreach (var row in rows)
            {
                yield return row;
            }
        }
    }

    // A row's cells, through the content controls and custom XML that can wrap them.
    private static IEnumerable<W.TableCell> CellElements(OpenXmlElement container)
    {
        foreach (var child in container.ChildElements)
        {
            var cells = child switch
            {
                W.TableCell cell => [cell],
                W.SdtCell { SdtContentCell: { } content } => CellElements(content),
                W.CustomXmlCell custom => CellElements(custom),
                _ => [],
            };
            foreach (var cell in cells)
            {
                yield return cell;
            }
        }
    }

    private const string WordprocessingNamespace = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";

    private static bool MarkRemoved(W.Paragraph paragraph) =>
        paragraph.ParagraphProperties?.ParagraphMarkRunProperties is { } mark
        && (mark.GetFirstChild<W.Deleted>() is not null || mark.GetFirstChild<W.MoveFrom>() is not null);

    /// <summary>A block's inlines as they are read, adjacent text merged into one run.</summary>
    private sealed class InlineBuilder
    {
        private readonly List<Inline> inlines = [];
        private readonly StringBuilder text = new();

        /// <summary>The pictures and charts the paragraph holds, the blocks that follow it.</summary>
        public List<Block> Blocks { get; } = [];

        /// <summary>Whether the paragraph has neither text nor a reference.</summary>
        public bool IsBlank => inlines.Count == 0 && string.IsNullOrWhiteSpace(text.ToString());

        public void Append(string value) => text.Append(value);

        public void Add(Inline inline)
        {
            Flush();
            inlines.Add(inline);
        }

        public List<Inline> Build()
        {
            Flush();
            return [.. inlines];
        }

        private void Flush()
        {
            if (text.Length > 0)
            {
                inlines.Add(new TextRun(text.ToString()));
                text.Clear();
            }
        }
    }

    private static string? NonEmpty(string? value) => string.IsNullOrWhiteSpace(value) ? null : value;

    private sealed class CellBuilder(IReadOnlyList<Block> blocks, int colSpan, int skipBefore)
    {
        public int RowSpan { get; set; } = 1;

        public TableCell Build() => new(blocks)
        {
            SkipBefore = skipBefore > 0 ? skipBefore : null,
            ColSpan = colSpan > 1 ? colSpan : null,
            RowSpan = RowSpan > 1 ? RowSpan : null,
        };
    }

    // A container's blocks as they are read. Consecutive list paragraphs make up lists: a deeper
    // level opens a list inside the last item, and at the same level an item continues the list
    // unless its level is ordered where the list's isn't (or the reverse), it belongs to another
    // abstract numbering definition, or it restarts the count, which is a change to a numbering
    // instance that overrides the level's start. Instances of one abstract definition otherwise
    // share its count, so they continue one list, as Word numbers them. A list starts at its
    // first item's number, which for a list a paragraph interrupted continues the count.
    private sealed class BlockList
    {
        private readonly List<Block> blocks = [];
        private readonly List<OpenList> open = [];

        public void Add(Block block)
        {
            while (open.Count > 0)
            {
                Close();
            }
            blocks.Add(block);
        }

        public void AddItem(int level, int numId, NumberingLevel numbering, int number, List<Block> item)
        {
            while (open.Count > 0 && open[^1].Level > level)
            {
                Close();
            }
            if (open.Count > 0 && open[^1] is var top && top.Level == level
                && (top.Ordered != numbering.Ordered
                    || top.AbstractId != numbering.AbstractId
                    || (numbering.Restarts && top.NumId != numId)))
            {
                Close();
            }
            if (open.Count == 0 || open[^1].Level < level)
            {
                open.Add(new OpenList(level, numbering.Ordered, numbering.AbstractId, number));
            }
            open[^1].NumId = numId;
            open[^1].Items.Add(item);
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

        private sealed record OpenList(int Level, bool Ordered, int AbstractId, int Start)
        {
            // The numbering instance of the list's last item.
            public int NumId { get; set; }

            public List<List<Block>> Items { get; } = [];
        }
    }

    /// <summary>What a list paragraph's numbering level says about its list.</summary>
    /// <param name="Ordered">Whether the level counts.</param>
    /// <param name="AbstractId">The abstract numbering definition the level belongs to.</param>
    /// <param name="Restarts">Whether the numbering instance overrides the level's start.</param>
    /// <param name="Start">The level's first number: the instance's override, or else the
    /// level's own start, 0 when it has none, as WordprocessingML defaults it.</param>
    private readonly record struct NumberingLevel(bool Ordered, int AbstractId, bool Restarts, int Start);

    /// <summary>The paragraph styles, by id, with their basedOn chains.</summary>
    private sealed class Styles
    {
        private readonly Dictionary<string, W.Style> byId = [];
        private readonly Dictionary<string, W.Style> anyType = [];
        private readonly string? defaultParagraph;

        public Styles(W.Styles? styles)
        {
            foreach (var style in styles?.Elements<W.Style>() ?? [])
            {
                if (style.StyleId?.Value is not { } id)
                {
                    continue;
                }
                anyType.TryAdd(id, style);
                if (style.Type?.Value == W.StyleValues.Paragraph)
                {
                    byId.TryAdd(id, style);
                    if (style.Default?.Value == true)
                    {
                        defaultParagraph ??= id;
                    }
                }
            }
        }

        // A style of any type by id, numbering styles among them.
        public W.Style? Find(string id) => anyType.GetValueOrDefault(id);

        // The style and the styles it is based on, nearest first; a paragraph with no style has
        // the default paragraph style. A cycle ends the chain.
        public IEnumerable<W.Style> Chain(string? styleId)
        {
            var seen = new HashSet<string>(StringComparer.Ordinal);
            var id = styleId ?? defaultParagraph;
            while (id is not null && seen.Add(id) && byId.TryGetValue(id, out var style))
            {
                yield return style;
                id = style.BasedOn?.Val?.Value;
            }
        }
    }

    /// <summary>The numbering part's instances and abstract definitions.</summary>
    private sealed class Numbering
    {
        private readonly Dictionary<int, W.NumberingInstance> instances = [];
        private readonly Dictionary<int, W.AbstractNum> abstracts = [];

        public Numbering(W.Numbering? numbering)
        {
            foreach (var instance in numbering?.Elements<W.NumberingInstance>() ?? [])
            {
                if (instance.NumberID?.Value is int id)
                {
                    instances.TryAdd(id, instance);
                }
            }
            foreach (var definition in numbering?.Elements<W.AbstractNum>() ?? [])
            {
                if (definition.AbstractNumberId?.Value is int id)
                {
                    abstracts.TryAdd(id, definition);
                }
            }
        }

        // The level of a numbering instance, or null when the instance or its definition doesn't
        // exist, which leaves the paragraph out of any list. A definition with only a numStyleLink
        // takes its levels from the numbering its style names.
        public NumberingLevel? Resolve(int numId, int level, Styles styles) => Resolve(numId, level, styles, 0, null);

        private NumberingLevel? Resolve(int numId, int level, Styles styles, int depth, int? startOverride)
        {
            if (depth > 8 || !instances.TryGetValue(numId, out var instance))
            {
                return null;
            }
            var levelOverride = instance.Elements<W.LevelOverride>().FirstOrDefault(o => o.LevelIndex?.Value == level);
            startOverride ??= levelOverride?.StartOverrideNumberingValue?.Val?.Value;
            if (instance.AbstractNumId?.Val?.Value is not int abstractId || !abstracts.TryGetValue(abstractId, out var definition))
            {
                return null;
            }
            var levels = definition.Elements<W.Level>().ToList();
            if (levels.Count == 0
                && definition.NumberingStyleLink?.Val?.Value is { } link
                && styles.Find(link)?.StyleParagraphProperties?.NumberingProperties?.NumberingId?.Val?.Value is int linked)
            {
                return Resolve(linked, level, styles, depth + 1, startOverride);
            }
            var definitionLevel = levelOverride?.GetFirstChild<W.Level>() ?? levels.FirstOrDefault(l => l.LevelIndex?.Value == level);
            return new NumberingLevel(
                IsOrdered(definitionLevel),
                abstractId,
                startOverride is not null,
                startOverride ?? definitionLevel?.StartNumberingValue?.Val?.Value ?? 0);
        }

        // A level formatted as a bullet or as nothing is unordered, as is a level the definition
        // lacks; a level without a format is decimal, so ordered.
        private static bool IsOrdered(W.Level? level)
        {
            if (level is null)
            {
                return false;
            }
            var format = level.NumberingFormat?.Val?.Value;
            return format is null || (format != W.NumberFormatValues.Bullet && format != W.NumberFormatValues.None);
        }
    }
}
