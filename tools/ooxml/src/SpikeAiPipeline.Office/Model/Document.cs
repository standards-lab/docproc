using System.Text.Json.Serialization;

namespace SpikeAiPipeline.Office.Model;

// The document model: one tree for docx, xlsx, and pptx, and office's versioned output contract.
// A document is an ordered list of blocks; a block's "type" names its kind in the JSON. Every
// block but a media block is the document's text, complete without inference.
//
// The kinds this schema version holds are heading, paragraph, list, table, chart, media, sheet,
// and slide; a docx document also holds its sections' headers and footers, its footnotes and
// endnotes, and its comments, referred to from the text by the footnote, endnote, and comment
// inlines. An xlsx document's blocks are its sheets, each holding a table of its cells, whose
// values are typed, then its charts and pictures. A pptx document's blocks are its slides in
// presentation order, each holding its own blocks and its notes' blocks beside them. The model
// grows by adding kinds and optional properties, never by reshaping these.

/// <summary>The formats office reads; the JSON names them by their file extension.</summary>
[JsonConverter(typeof(JsonStringEnumConverter<SourceFormat>))]
public enum SourceFormat
{
    /// <summary>A WordprocessingML package.</summary>
    [JsonStringEnumMemberName("docx")]
    Docx,

    /// <summary>A SpreadsheetML package.</summary>
    [JsonStringEnumMemberName("xlsx")]
    Xlsx,

    /// <summary>A PresentationML package.</summary>
    [JsonStringEnumMemberName("pptx")]
    Pptx,
}

/// <summary>A converted document: its schema version, its source format, and its blocks in
/// document order. The other parts of a document are left out when it has none.</summary>
public sealed record Document(string Schema, SourceFormat Format, IReadOnlyList<Block> Blocks)
{
    /// <summary>The schema version this build writes. It changes whenever the JSON's shape does,
    /// so a reader can tell which contract a document follows.</summary>
    public const string CurrentSchema = "1";

    /// <summary>A docx document's sections in body order, each with the headers and footers it
    /// shows, or null when no section has any. A media block's section is an index into the
    /// document's sections, which it counts even when this is null.</summary>
    public IReadOnlyList<Section>? Sections { get; init; }

    /// <summary>The footnotes the text references, in the order it first references them.</summary>
    public IReadOnlyList<Note>? Footnotes { get; init; }

    /// <summary>The endnotes the text references, in the order it first references them.</summary>
    public IReadOnlyList<Note>? Endnotes { get; init; }

    /// <summary>The comments, in the order the document stores them, each with its replies.</summary>
    public IReadOnlyList<Comment>? Comments { get; init; }
}

/// <summary>A docx section's headers and footers, as its pages show them: its own, or the
/// previous section's where it has none of a kind. A first-page one is kept only when the
/// section has a title page, an even-page one only when the document turns even-page headers on,
/// and one with no blocks is left out.</summary>
public sealed record Section(IReadOnlyList<HeaderFooter>? Headers, IReadOnlyList<HeaderFooter>? Footers);

/// <summary>A header or footer and its blocks.</summary>
public sealed record HeaderFooter(HeaderFooterKind Kind, IReadOnlyList<Block> Blocks);

/// <summary>The pages a header or footer is shown on.</summary>
[JsonConverter(typeof(JsonStringEnumConverter<HeaderFooterKind>))]
public enum HeaderFooterKind
{
    /// <summary>Every page the other kinds don't cover; with even-page headers, the odd pages.</summary>
    [JsonStringEnumMemberName("default")]
    Default,

    /// <summary>The section's first page.</summary>
    [JsonStringEnumMemberName("first")]
    First,

    /// <summary>The even pages.</summary>
    [JsonStringEnumMemberName("even")]
    Even,
}

/// <summary>A footnote or endnote: its id in the document, which its references give, and its
/// blocks.</summary>
public sealed record Note(int Id, IReadOnlyList<Block> Blocks);

/// <summary>A comment: its id, which its reference in the text gives, its author, the text it is
/// anchored to (the accepted text of its range, paragraphs joined by a newline), whether its
/// thread is resolved, its blocks, and its replies in order.</summary>
public sealed record Comment(int Id, string? Author, string Anchor, bool Resolved, IReadOnlyList<Block> Blocks, IReadOnlyList<CommentReply> Replies);

/// <summary>A reply in a comment's thread.</summary>
public sealed record CommentReply(int Id, string? Author, IReadOnlyList<Block> Blocks);

/// <summary>A block of the document, in document order.</summary>
[JsonPolymorphic(TypeDiscriminatorPropertyName = "type")]
[JsonDerivedType(typeof(Heading), "heading")]
[JsonDerivedType(typeof(Paragraph), "paragraph")]
[JsonDerivedType(typeof(ListBlock), "list")]
[JsonDerivedType(typeof(Table), "table")]
[JsonDerivedType(typeof(Chart), "chart")]
[JsonDerivedType(typeof(Media), "media")]
[JsonDerivedType(typeof(Sheet), "sheet")]
[JsonDerivedType(typeof(Slide), "slide")]
public abstract record Block;

/// <summary>A heading, level 1 the outermost.</summary>
public sealed record Heading(int Level, IReadOnlyList<Inline> Inlines) : Block;

/// <summary>A paragraph of body text.</summary>
public sealed record Paragraph(IReadOnlyList<Inline> Inlines) : Block;

/// <summary>A list, ordered or not. A nested list is a block of the item it belongs to.</summary>
public sealed record ListBlock([property: JsonPropertyOrder(-2)] bool Ordered, IReadOnlyList<ListItem> Items) : Block
{
    /// <summary>An ordered list's first number, or null when it is 1 or the list is unordered: the
    /// numbering's start value, or where a list resumes the count of one an unnumbered paragraph
    /// interrupted, the number it resumes at, so its items count on from there.</summary>
    [JsonPropertyOrder(-1)]
    public int? Start { get; init; }
}

/// <summary>A list item: its own blocks, usually a paragraph, then any nested list.</summary>
public sealed record ListItem(IReadOnlyList<Block> Blocks);

/// <summary>A table: rows of cells, the first row first.</summary>
public sealed record Table(IReadOnlyList<TableRow> Rows) : Block;

/// <summary>A table row. A grid position covered by a merged cell from the left or above has
/// no cell of its own, so a row can hold fewer cells than the table has columns.</summary>
public sealed record TableRow(IReadOnlyList<TableCell> Cells)
{
    /// <summary>A sheet row's number, from 1; null in a docx table. A sheet's table holds only
    /// the rows that have cells, so its rows are numbered.</summary>
    [JsonPropertyOrder(-1)]
    public int? Row { get; init; }
}

/// <summary>A table cell and the blocks it holds, or a sheet's cell and its value. A merged cell
/// spans more than one column or row; a span of one is left out of the JSON.</summary>
/// <remarks>A sheet's table holds only the cells that have a value or a formula, each with its
/// reference, so it is as sparse as the sheet and keeps every position; a sheet cell has no
/// blocks.</remarks>
public sealed record TableCell(IReadOnlyList<Block>? Blocks)
{
    /// <summary>A sheet cell's reference, such as B2; null in a docx table.</summary>
    [JsonPropertyOrder(-1)]
    public string? Ref { get; init; }

    /// <summary>The type of a sheet cell's value, or null when it has none.</summary>
    [JsonPropertyOrder(-1)]
    public CellValueType? ValueType => Value?.Type;

    /// <summary>A sheet cell's value: a constant's, or a formula's cached result; null for a
    /// formula the file caches no result for. Nothing is ever evaluated.</summary>
    [JsonPropertyOrder(-1)]
    public CellValue? Value { get; init; }

    /// <summary>A sheet cell's formula as stored, without the leading <c>=</c>: its own, or as
    /// the anchor of a shared or array formula.</summary>
    [JsonPropertyOrder(-1)]
    public string? Formula { get; init; }

    /// <summary>For a cell of a shared formula other than its anchor, the anchor's reference,
    /// which holds the formula's text; the cell's own text is never derived from it.</summary>
    [JsonPropertyOrder(-1)]
    public string? SharedFrom { get; init; }

    /// <summary>The grid columns before a docx cell that no cell fills, as a row that starts late
    /// leaves them (its <c>w:gridBefore</c>), on its row's first cell; null for none. The columns
    /// a merged cell from above covers are not counted: they are skipped as they always are.</summary>
    [JsonPropertyOrder(-1)]
    public int? SkipBefore { get; init; }

    /// <summary>The columns the cell spans, or null for one.</summary>
    public int? ColSpan { get; init; }

    /// <summary>The rows the cell spans, or null for one.</summary>
    public int? RowSpan { get; init; }
}

/// <summary>A chart, from the data its chart part caches, never an image of it: its type (the
/// plot's kind, such as bar, line, or pie; the first plot's when it has several; for an Office
/// 2016 chartEx chart its first series' layout, such as boxWhisker or sunburst), its title, and
/// its series.</summary>
public sealed record Chart(string ChartType, string? Title, IReadOnlyList<ChartSeries> Series) : Block;

/// <summary>A chart series: its name, its categories (or x values), and its values, a point the
/// cache lacks null. The two lists are as long as the cache says.</summary>
public sealed record ChartSeries(string? Name, IReadOnlyList<string?> Categories, IReadOnlyList<double?> Values);

/// <summary>A picture. An embedded one has its file's name in the media directory (the
/// lowercase hex SHA-256 of its bytes and its stored extension, the same for every block that
/// shows those bytes), its content type, and the package part it is stored in; a linked one has
/// its target instead, and no file. Its alternative text and title are its drawing's.</summary>
public sealed record Media : Block
{
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

    /// <summary>A linked picture's target, which office never fetches.</summary>
    public string? Target { get; init; }

    /// <summary>The index of the docx section the picture is shown in: the section whose body,
    /// header, or footer holds it, or for a note the section of its first reference; null in a
    /// comment.</summary>
    public int? Section { get; init; }

    /// <summary>The index of the xlsx sheet the picture is on, in the document's blocks.</summary>
    public int? Sheet { get; init; }

    /// <summary>The index of the pptx slide the picture is on, in the document's blocks: its
    /// number less one.</summary>
    public int? Slide { get; init; }
}

/// <summary>An xlsx sheet, in workbook order: its name, whether it is hidden (hidden or very
/// hidden, both read the same), and its blocks: a table of its cells when it has any, then its
/// charts and pictures in the order its drawing anchors them.</summary>
public sealed record Sheet(string Name, bool Hidden, IReadOnlyList<Block> Blocks) : Block;

/// <summary>A pptx slide, in presentation order: its number from 1 (hidden slides counted),
/// whether it is hidden from the slide show, its blocks in the order of its shape tree, and its
/// notes' blocks, null when it has no notes text.</summary>
public sealed record Slide(int Number, bool Hidden, IReadOnlyList<Block> Blocks) : Block
{
    /// <summary>The blocks of the slide's notes page: the text of its notes placeholder.</summary>
    public IReadOnlyList<Block>? Notes { get; init; }
}

/// <summary>The type of a sheet cell's value.</summary>
[JsonConverter(typeof(JsonStringEnumConverter<CellValueType>))]
public enum CellValueType
{
    /// <summary>A number, as stored, in no number format.</summary>
    [JsonStringEnumMemberName("number")]
    Number,

    /// <summary>Text: a shared or inline string, rich text flattened, or a formula's string.</summary>
    [JsonStringEnumMemberName("text")]
    Text,

    /// <summary>TRUE or FALSE.</summary>
    [JsonStringEnumMemberName("boolean")]
    Boolean,

    /// <summary>An error, by its code, such as #DIV/0!.</summary>
    [JsonStringEnumMemberName("error")]
    Error,

    /// <summary>A number in a date or time format, or a cell stored as a date, in ISO 8601.</summary>
    [JsonStringEnumMemberName("date")]
    Date,
}

/// <summary>A sheet cell's value. The JSON writes a number as a JSON number, a boolean as true or
/// false, and the rest as strings, the cell's value type telling them apart.</summary>
[JsonConverter(typeof(CellValueJsonConverter))]
public sealed record CellValue
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

/// <summary>A piece of a block's text: text, or a reference to a note or comment at its position
/// in the text.</summary>
[JsonPolymorphic(TypeDiscriminatorPropertyName = "type")]
[JsonDerivedType(typeof(TextRun), "text")]
[JsonDerivedType(typeof(FootnoteReference), "footnote")]
[JsonDerivedType(typeof(EndnoteReference), "endnote")]
[JsonDerivedType(typeof(CommentReference), "comment")]
public abstract record Inline;

/// <summary>A run of text, with tabs as <c>\t</c> and line breaks as <c>\n</c>. Adjacent runs
/// are merged, so a block's text is one run between other inlines.</summary>
public sealed record TextRun(string Text) : Inline;

/// <summary>A footnote's reference, by the note's id.</summary>
public sealed record FootnoteReference(int Id) : Inline;

/// <summary>An endnote's reference, by the note's id.</summary>
public sealed record EndnoteReference(int Id) : Inline;

/// <summary>The end of a comment's anchored range, by the comment's id.</summary>
public sealed record CommentReference(int Id) : Inline;
