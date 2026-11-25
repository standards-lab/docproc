namespace Docproc.Ooxml.Model;

internal enum SourceFormat
{
    Docx,

    Xlsx,

    Pptx,
}

internal sealed record Document(SourceFormat Format, IReadOnlyList<Block> Blocks)
{
    /// <summary>A docx document's sections in body order, each with the headers and footers it
    /// shows, or null when no section has any. A media block's section is an index into the
    /// document's sections, which it counts even when this is null.</summary>
    public IReadOnlyList<Section>? Sections { get; init; }

    /// <summary>The footnotes the text references, in the order it first references them.</summary>
    public IReadOnlyList<Note>? Footnotes { get; init; }

    /// <summary>The endnotes the text references, in the order it first references them.</summary>
    public IReadOnlyList<Note>? Endnotes { get; init; }

    /// <summary>The comments, in the order the document stores them, a pptx's slide by slide,
    /// each with its replies.</summary>
    public IReadOnlyList<Comment>? Comments { get; init; }

    /// <summary>The package's core, extended, and custom properties, or null when it has none.</summary>
    public Properties? Properties { get; init; }
}

internal sealed record Properties(IReadOnlyList<Property>? Core, IReadOnlyList<Property>? App, IReadOnlyList<Property>? Custom);

internal sealed record Property(string Key, PropertyValue Value);

internal enum PropertyValueType
{
    Text,

    Number,

    Boolean,
}

internal sealed record PropertyValue(PropertyValueType Type, string Text);

internal sealed record Section(IReadOnlyList<HeaderFooter>? Headers, IReadOnlyList<HeaderFooter>? Footers);

internal sealed record HeaderFooter(HeaderFooterKind Kind, IReadOnlyList<Block> Blocks);

internal enum HeaderFooterKind
{
    Default,

    First,

    Even,
}

internal sealed record Note(int Id, IReadOnlyList<Block> Blocks);

internal sealed record Comment(int Id, string? Author, string Anchor, bool Resolved, IReadOnlyList<Block> Blocks, IReadOnlyList<CommentReply> Replies);

internal sealed record CommentReply(int Id, string? Author, IReadOnlyList<Block> Blocks);

internal abstract record Block;

internal sealed record Heading(int Level, IReadOnlyList<Inline> Inlines) : Block;

internal sealed record Paragraph(IReadOnlyList<Inline> Inlines) : Block;

internal sealed record ListBlock(bool Ordered, IReadOnlyList<ListItem> Items) : Block
{
    /// <summary>An ordered list's first number, or null when it is 1 or the list is unordered: the
    /// numbering's start value, or where a list resumes the count of one an unnumbered paragraph
    /// interrupted, the number it resumes at, so its items count on from there.</summary>
    public int? Start { get; init; }
}

internal sealed record ListItem(IReadOnlyList<Block> Blocks);

internal sealed record Table(IReadOnlyList<TableRow> Rows) : Block;

internal sealed record TableRow(IReadOnlyList<TableCell> Cells)
{
    /// <summary>A sheet row's number, from 1; null in a docx table. A sheet's table holds only
    /// the rows that have cells, so its rows are numbered.</summary>
    public int? Row { get; init; }
}

internal sealed record TableCell(IReadOnlyList<Block>? Blocks)
{
    /// <summary>A sheet cell's reference, such as B2; null in a docx table.</summary>
    public string? Ref { get; init; }

    /// <summary>The type of a sheet cell's value, or null when it has none.</summary>
    public CellValueType? ValueType => Value?.Type;

    /// <summary>A sheet cell's value: a constant's, or a formula's cached result; null for a
    /// formula the file caches no result for. Nothing is ever evaluated.</summary>
    public CellValue? Value { get; init; }

    /// <summary>A sheet cell's formula as stored, without the leading <c>=</c>: its own, or as
    /// the anchor of a shared or array formula.</summary>
    public string? Formula { get; init; }

    /// <summary>For a cell of a shared formula other than its anchor, the anchor's reference,
    /// which holds the formula's text; the cell's own text is never derived from it.</summary>
    public string? SharedFrom { get; init; }

    /// <summary>The grid columns before a docx cell that no cell fills, as a row that starts late
    /// leaves them (its <c>w:gridBefore</c>), on its row's first cell; null for none. The columns
    /// a merged cell from above covers are not counted: they are skipped as they always are.</summary>
    public int? SkipBefore { get; init; }

    /// <summary>The columns the cell spans, or null for one.</summary>
    public int? ColSpan { get; init; }

    /// <summary>The rows the cell spans, or null for one.</summary>
    public int? RowSpan { get; init; }

    /// <summary>The target of a sheet cell's external hyperlink, which its Markdown links its
    /// value to; null in a docx table, and for a cell with no hyperlink or an internal one.</summary>
    public string? Target { get; init; }
}

internal sealed record Chart(string ChartType, string? Title, IReadOnlyList<ChartSeries> Series) : Block;

internal sealed record ChartSeries(string? Name, IReadOnlyList<string?> Categories, IReadOnlyList<double?> Values);

internal enum MediaKind
{
    Picture,

    Embedded,

    Ole,
}

internal sealed record Media : Block
{
    /// <summary>What the medium is; a picture unless it says otherwise.</summary>
    public MediaKind Kind { get; init; }

    /// <summary>An embedded package's or OLE object's program id, such as <c>Excel.Sheet.12</c>,
    /// from the element that names its relationship; null for a picture.</summary>
    public string? ProgId { get; init; }

    /// <summary>The file's name relative to the media directory, or null for a linked picture.</summary>
    public string? Name { get; init; }

    /// <summary>The stored content type, or null for a linked picture.</summary>
    public string? ContentType { get; init; }

    /// <summary>The alternative text, the drawing's description.</summary>
    public string? Alt { get; init; }

    /// <summary>The drawing's title.</summary>
    public string? Title { get; init; }

    /// <summary>The part the picture is stored in, or null for a linked picture.</summary>
    public string? Source { get; init; }

    /// <summary>A linked picture's target, which docproc-ooxml never fetches.</summary>
    public string? Target { get; init; }

    /// <summary>The index of the docx section the picture is shown in: the section whose body,
    /// header, or footer holds it, or for a note the section of its first reference; null in a
    /// comment.</summary>
    public int? Section { get; init; }

    /// <summary>The index of the xlsx sheet the picture is on, in the document's blocks; the
    /// line writer writes it as the ordinal of the sheet's <c>sheet</c> line, this plus one.</summary>
    public int? Sheet { get; init; }

    /// <summary>The index of the pptx slide the picture is on, in the document's blocks: its
    /// number less one.</summary>
    public int? Slide { get; init; }
}

internal sealed record Sheet(string Name, bool Hidden, IReadOnlyList<Block> Blocks) : Block;

internal sealed record Slide(int Number, bool Hidden, IReadOnlyList<Block> Blocks) : Block
{
    /// <summary>The blocks of the slide's notes page: the text of its notes placeholder.</summary>
    public IReadOnlyList<Block>? Notes { get; init; }

    /// <summary>The ids of the slide's comment threads in the document's comments, in the order
    /// they are read, or null when it has none.</summary>
    public IReadOnlyList<int>? Comments { get; init; }
}

internal enum CellValueType
{
    Number,

    Text,

    Boolean,

    Error,

    Date,
}

internal sealed record CellValue
{
    private CellValue(CellValueType type, double number, bool boolean, string? text)
    {
        Type = type;
        Number = number;
        Boolean = boolean;
        Text = text;
    }

    /// <summary>The value's type.</summary>
    public CellValueType Type { get; }

    /// <summary>A number's value.</summary>
    public double Number { get; }

    /// <summary>A boolean's value.</summary>
    public bool Boolean { get; }

    /// <summary>The text of text, an error's code, or a date's ISO 8601 form.</summary>
    public string? Text { get; }

    public static CellValue OfNumber(double number) => new(CellValueType.Number, number, false, null);

    public static CellValue OfText(string text) => new(CellValueType.Text, 0, false, text);

    public static CellValue OfBoolean(bool boolean) => new(CellValueType.Boolean, 0, boolean, null);

    public static CellValue OfError(string code) => new(CellValueType.Error, 0, false, code);

    public static CellValue OfDate(string iso) => new(CellValueType.Date, 0, false, iso);
}

internal abstract record Inline;

internal sealed record TextRun(string Text) : Inline;

internal sealed record FootnoteReference(int Id) : Inline;

internal sealed record EndnoteReference(int Id) : Inline;

internal sealed record CommentReference(int Id) : Inline;

internal sealed record HyperlinkStart(string Target) : Inline;

internal sealed record HyperlinkEnd : Inline;
