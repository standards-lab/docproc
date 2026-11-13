using System.Globalization;
using System.Text;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Writing;
using C = DocumentFormat.OpenXml.Drawing.Charts;
using S = DocumentFormat.OpenXml.Spreadsheet;
using Xdr = DocumentFormat.OpenXml.Drawing.Spreadsheet;

namespace Docproc.Ooxml.Reading;

/// <summary>Reads an xlsx package: its sheets in workbook order, each a table of the cells that
/// have a value or a formula, with their stored values typed and nothing evaluated, then its
/// charts and pictures, then its embedded packages and OLE objects; and the package's
/// properties.</summary>
/// <remarks>Each worksheet part is read twice, forward-only, and never held whole
/// (context/design.md, OX-2). <see cref="Validate"/> reads every sheet once and writes nothing:
/// it finds whatever would refuse the workbook, and collects what the sheet's lines need before
/// its rows, its used columns, shared-formula anchors, merges, and hyperlinks, and its drawings,
/// objects, and their media. <see cref="XlsxWorkbook.Rows"/> reads a sheet a second time, giving its rows as they
/// are read.</remarks>
internal static partial class XlsxReader
{
    /// <summary>Reads the package whole into the model, each sheet's rows collected; the CLI
    /// streams them instead (<see cref="XlsxWorkbook.Write"/>).</summary>
    public static Conversion Read(SpreadsheetDocument package)
    {
        var workbook = Validate(package);
        var blocks = new List<Block>();
        foreach (var sheet in workbook.Sheets)
        {
            var rows = workbook.Rows(sheet).ToList();
            blocks.Add(new Model.Sheet(sheet.Name, sheet.Hidden, [.. rows.Count > 0 ? [new Table(rows)] : Array.Empty<Block>(), .. sheet.Drawings]));
        }
        return new Conversion(new Document(SourceFormat.Xlsx, blocks) { Properties = workbook.Properties }, workbook.Media) { Warnings = workbook.Warnings };
    }

    /// <summary>The validate pass: reads every sheet of the package forward once, writing nothing,
    /// so whatever refuses the workbook is found before any output. The workbook-level parts,
    /// shared strings and styles, are read whole up front, as the cells' values need them.</summary>
    public static XlsxWorkbook Validate(SpreadsheetDocument package)
    {
        var workbookPart = package.WorkbookPart ?? throw new OpenXmlPackageException("the package has no workbook part");
        var workbook = workbookPart.Workbook ?? throw new OpenXmlPackageException("the workbook part has no workbook");
        var warnings = new List<Warning>();
        var properties = PropertiesReader.Read(package, warnings);
        var context = new XlsxContext(
            SharedStrings(workbookPart.SharedStringTablePart?.SharedStringTable),
            new NumberFormats(workbookPart.WorkbookStylesPart?.Stylesheet),
            workbook.WorkbookProperties?.Date1904?.Value ?? false,
            new UnreadKinds(warnings));
        var sheets = new List<XlsxSheet>();
        var ordinal = 0;
        foreach (var sheet in workbook.Sheets?.Elements<S.Sheet>() ?? [])
        {
            // A sheet whose reference names no part (an empty or unknown relationship id) is one
            // invalid reference: it is skipped, with a warning, and the other sheets are read. It
            // has no sheet line to count, so its place is its ordinal in the workbook's order.
            ordinal++;
            var id = sheet.Id?.Value;
            if (string.IsNullOrEmpty(id) || !workbookPart.TryGetPartById(id, out var part))
            {
                warnings.Add(new Warning($"sheet {ordinal}", $"\"{sheet.Name?.Value}\" was skipped: its relationship id \"{id}\" names no part"));
                continue;
            }
            var state = sheet.State?.Value;
            var hidden = state == S.SheetStateValues.Hidden || state == S.SheetStateValues.VeryHidden;
            sheets.Add(ValidateSheet(context, sheet.Name?.Value ?? "", hidden, part, sheets.Count));
        }
        return new XlsxWorkbook(context, sheets, warnings) { Properties = properties };
    }

    internal sealed class XlsxContext(IReadOnlyList<string> strings, NumberFormats formats, bool date1904, UnreadKinds unread)
    {
        public UnreadKinds Unread => unread;

        public IReadOnlyList<string> Strings => strings;

        public NumberFormats Formats => formats;

        public bool Date1904 => date1904;

        public MediaStore Media { get; } = new();
    }

    // A worksheet's cells, then its drawing, then its objects; a chartsheet's drawing alone; any
    // other kind of sheet (a dialog or macro sheet) holds nothing docproc-ooxml reads. A
    // worksheet's notes, legacy or threaded, are not read. The index is the sheet's line's, from 0.
    private static XlsxSheet ValidateSheet(XlsxContext context, string name, bool hidden, OpenXmlPart part, int index)
    {
        SheetCells? cells = null;
        DrawingsPart? drawings = null;
        IReadOnlyList<OpenXmlElement> objects = [];
        switch (part)
        {
            case WorksheetPart worksheet:
                (cells, objects) = ValidateCells(context, worksheet);
                drawings = worksheet.DrawingsPart;
                if (worksheet.WorksheetCommentsPart is not null || worksheet.WorksheetThreadedCommentsParts.Any())
                {
                    context.Unread.Found(Place(index), UnreadKinds.Notes);
                }
                break;
            case ChartsheetPart chartsheet:
                drawings = chartsheet.DrawingsPart;
                break;
            default:
                break;
        }
        var blocks = new List<Block>();
        if (drawings is not null)
        {
            ReadDrawing(context, drawings, index, blocks);
        }
        foreach (var element in objects)
        {
            ReadObject(context, (WorksheetPart)part, element, index, blocks);
        }
        return new XlsxSheet(name, hidden, cells, blocks);
    }

    // Where a warning about a sheet is: its sheet line, by ordinal.
    private static string Place(int index) => $"sheet {index + 1}";

    // A worksheet's OLE object: its preview picture (its objectPr's), then its embedded package or
    // OLE object as stored, with its program id. Excel writes the object twice in alternate
    // content, the preview only in the choice's copy; both name the same relationship. A
    // streaming reader loads alternate content's children as unknown elements, so they are found
    // by name.
    private static void ReadObject(XlsxContext context, WorksheetPart part, OpenXmlElement element, int sheet, List<Block> blocks)
    {
        List<OpenXmlElement> objects = [.. new[] { element }.Concat(element.Descendants()).Where(e => e.LocalName == "oleObject")];
        // Transitional and Strict name the relationships' namespace apart, both ending so.
        string? Relationship(OpenXmlElement e) => e.GetAttributes().FirstOrDefault(a => a.LocalName == "id" && a.NamespaceUri.EndsWith("relationships", StringComparison.Ordinal)).Value;
        if (objects.SelectMany(o => o.ChildElements.Where(c => c.LocalName == "objectPr")).Select(Relationship).FirstOrDefault(id => id is not null) is { } previewId
            && context.Media.Picture(part, previewId, null, null) is { } preview)
        {
            blocks.Add(preview with { Sheet = sheet });
        }
        var progId = objects.Select(o => o.GetAttributes().FirstOrDefault(a => a.LocalName == "progId" && a.NamespaceUri.Length == 0).Value).FirstOrDefault(p => p is not null);
        if (objects.Select(Relationship).FirstOrDefault(id => id is not null) is { } id
            && context.Media.Payload(part, id, progId) is { } payload)
        {
            blocks.Add(payload with { Sheet = sheet });
        }
    }

    // A cell's stored value by its type: a shared or inline string, a formula's string, a
    // boolean, an error, an ISO date, or a number, a date when its format is a date or time
    // format. An empty value is none, as openpyxl writes a formula it has not computed, except
    // a formula's string, which can be empty.
    private static CellValue? Value(XlsxContext context, S.Cell cell)
    {
        var type = cell.DataType?.Value ?? S.CellValues.Number;
        if (type == S.CellValues.InlineString)
        {
            return cell.InlineString is { } inline ? CellValue.OfText(Text(inline)) : null;
        }
        var raw = cell.CellValue?.Text;
        if (type == S.CellValues.String)
        {
            return raw is null ? null : CellValue.OfText(raw);
        }
        if (string.IsNullOrEmpty(raw))
        {
            return null;
        }
        if (type == S.CellValues.SharedString)
        {
            // Excel Online writes an index with a trailing space (<v>2 </v>), which Excel reads.
            return int.TryParse(raw, NumberStyles.AllowLeadingWhite | NumberStyles.AllowTrailingWhite, CultureInfo.InvariantCulture, out var index) && index < context.Strings.Count
                ? CellValue.OfText(context.Strings[index])
                : throw new InvalidDataException($"cell {cell.CellReference}'s shared string {raw} is not in the shared strings");
        }
        if (type == S.CellValues.Boolean)
        {
            return raw switch
            {
                "1" or "true" => CellValue.OfBoolean(true),
                "0" or "false" => CellValue.OfBoolean(false),
                _ => throw new InvalidDataException($"cell {cell.CellReference}'s boolean {raw} is not 0 or 1"),
            };
        }
        if (type == S.CellValues.Error)
        {
            return CellValue.OfError(raw);
        }
        if (type == S.CellValues.Date)
        {
            return CellValue.OfDate(raw);
        }
        if (!double.TryParse(raw, NumberStyles.Float, CultureInfo.InvariantCulture, out var number) || !double.IsFinite(number))
        {
            throw new InvalidDataException($"cell {cell.CellReference}'s number {raw} is not a number");
        }
        var parts = context.Formats.DateParts(cell.StyleIndex?.Value ?? 0);
        return parts is { } date && SerialDate.Iso(number, context.Date1904, date.Date, date.Time) is { } iso
            ? CellValue.OfDate(iso)
            : CellValue.OfNumber(number);
    }

    // A shared or inline string's text: its text, or its rich text's runs, flattened, its escapes
    // decoded. Phonetic runs (rPh) are a reading aid, not the text, and are left out.
    private static string Text(OpenXmlElement item)
    {
        var text = new StringBuilder();
        foreach (var child in item.ChildElements)
        {
            switch (child)
            {
                case S.Text t:
                    text.Append(t.Text);
                    break;
                case S.Run run:
                    text.Append(run.Text?.Text);
                    break;
                default:
                    break;
            }
        }
        return Unescape(text.ToString());
    }

    // SpreadsheetML's escapes (ECMA-376 Part 1, 22.9.2.19, ST_Xstring): _xHHHH_ is the UTF-16
    // code unit of four hexadecimal digits, which is how a character XML can't hold, such as a
    // control character, is stored. _x005F_ is the escaped underscore, so _x005F_x0041_ is the
    // text _x0041_: decoded left to right, an escape's own underscore never starts another.
    internal static string Unescape(string text)
    {
        if (!text.Contains("_x", StringComparison.Ordinal))
        {
            return text;
        }
        var decoded = new StringBuilder(text.Length);
        for (var i = 0; i < text.Length; i++)
        {
            if (text[i] == '_' && i + 6 < text.Length && text[i + 1] == 'x' && text[i + 6] == '_'
                && ushort.TryParse(text.AsSpan(i + 2, 4), NumberStyles.AllowHexSpecifier, CultureInfo.InvariantCulture, out var unit))
            {
                decoded.Append((char)unit);
                i += 6;
                continue;
            }
            decoded.Append(text[i]);
        }
        return decoded.ToString();
    }

    private static List<string> SharedStrings(S.SharedStringTable? table) =>
        [.. (table?.Elements<S.SharedStringItem>() ?? []).Select(Text)];

    // A drawing's pictures and charts in the order it anchors them, through groups. A picture's
    // alternative text and title are its own drawing properties'. A chartEx chart, in an
    // mc:AlternateContent, is its cached data, or with none its fallback's picture, never both;
    // any other alternate content is read through, as the rest of the drawing is. A shape's text,
    // a text box's, and a SmartArt diagram are not read.
    private static void ReadDrawing(XlsxContext context, DrawingsPart drawings, int sheet, List<Block> blocks) =>
        ReadDrawing(context, drawings, drawings.WorksheetDrawing, sheet, blocks);

    private static void ReadDrawing(XlsxContext context, DrawingsPart drawings, OpenXmlElement? container, int sheet, List<Block> blocks)
    {
        foreach (var element in container?.ChildElements ?? [])
        {
            switch (element)
            {
                case Xdr.Picture picture:
                    var properties = picture.NonVisualPictureProperties?.NonVisualDrawingProperties;
                    var blip = picture.BlipFill?.Blip;
                    if ((blip?.Embed?.Value ?? blip?.Link?.Value) is { } id
                        && context.Media.Picture(drawings, id, NonEmpty(properties?.Description?.Value), NonEmpty(properties?.Title?.Value)) is { } media)
                    {
                        blocks.Add(media with { Sheet = sheet });
                    }
                    break;
                case C.ChartReference chart when chart.Id?.Value is { } chartId:
                    if (drawings.TryGetPartById(chartId, out var part) && part is ChartPart charts && ChartReader.Read(charts) is { } read)
                    {
                        blocks.Add(read);
                    }
                    break;
                case AlternateContent content when ChartReader.ChartEx(content, drawings) is { } chartEx:
                    if (chartEx.Chart is { } chartExChart)
                    {
                        blocks.Add(chartExChart);
                    }
                    else
                    {
                        ReadDrawing(context, drawings, chartEx.Fallback, sheet, blocks);
                    }
                    break;
                case Xdr.Shape shape:
                    if (!string.IsNullOrWhiteSpace(shape.TextBody?.InnerText))
                    {
                        context.Unread.Found(Place(sheet), UnreadKinds.TextBoxes);
                    }
                    if (shape.TextBody is { } body && UnreadKinds.HoldsEquation(body))
                    {
                        context.Unread.Found(Place(sheet), UnreadKinds.Equations);
                    }
                    break;
                case Xdr.GraphicFrame frame when UnreadKinds.HoldsSmartArt(frame):
                    context.Unread.Found(Place(sheet), UnreadKinds.SmartArt);
                    break;
                default:
                    ReadDrawing(context, drawings, element, sheet, blocks);
                    break;
            }
        }
    }

    private static string? NonEmpty(string? value) => string.IsNullOrWhiteSpace(value) ? null : value;

    // A1-style references: a column's letters, A to XFD, then the row's number.
    private const int MaxColumn = 16384;
    private const int MaxRow = 1048576;

    private static (int Row, int Column)? Position(string reference)
    {
        var text = reference.Replace("$", "", StringComparison.Ordinal);
        var letters = 0;
        var column = 0;
        while (letters < text.Length && char.IsAsciiLetter(text[letters]))
        {
            column = (column * 26) + (char.ToUpperInvariant(text[letters]) - 'A' + 1);
            letters++;
            if (column > MaxColumn)
            {
                return null;
            }
        }
        if (letters == 0 || !int.TryParse(text.AsSpan(letters), NumberStyles.None, CultureInfo.InvariantCulture, out var row) || row is < 1 or > MaxRow)
        {
            return null;
        }
        return (row, column);
    }

    private static Merge? Range(string range)
    {
        var ends = range.Split(':');
        if (ends.Length > 2 || Position(ends[0]) is not { } first || Position(ends[^1]) is not { } last)
        {
            return null;
        }
        return new Merge(Math.Min(first.Row, last.Row), Math.Min(first.Column, last.Column), Math.Max(first.Row, last.Row), Math.Max(first.Column, last.Column));
    }

    // A merged range by its corners; its anchor is the top-left cell.
    internal sealed record Merge(int Top, int Left, int Bottom, int Right)
    {
        public int Rows => Bottom - Top + 1;

        public int Columns => Right - Left + 1;
    }

    // A sheet's merged ranges, looked up by cell. A range is kept as its corners, never cell by
    // cell, as one can cover the whole sheet. The lookup sweeps down the rows: the ranges that
    // cover the row asked for are kept by their left column, so a cell's range is found by a
    // binary search; asking for a row above the last starts the sweep again. Excel refuses a
    // file whose ranges overlap, so a cell is in one range at most; in a file whose ranges do
    // overlap, a cell is looked for only in the covering range that starts nearest its left.
    private sealed class MergeIndex
    {
        private readonly Merge[] byTop;
        private readonly List<Merge> covering = [];
        private int next;
        private int row;

        public MergeIndex(IEnumerable<Merge> merges)
        {
            // OrderBy is stable, so ranges starting on one row keep the file's order.
            byTop = [.. merges.OrderBy(m => m.Top)];
        }

        public Merge? Find(int cellRow, int cellColumn)
        {
            if (byTop.Length == 0)
            {
                return null;
            }
            if (cellRow != row)
            {
                Sweep(cellRow);
            }
            // The last covering range whose left column is at or before the cell's.
            int low = 0, high = covering.Count - 1, found = -1;
            while (low <= high)
            {
                var middle = low + ((high - low) / 2);
                if (covering[middle].Left <= cellColumn)
                {
                    found = middle;
                    low = middle + 1;
                }
                else
                {
                    high = middle - 1;
                }
            }
            return found >= 0 && covering[found].Right >= cellColumn ? covering[found] : null;
        }

        private void Sweep(int cellRow)
        {
            if (cellRow < row)
            {
                covering.Clear();
                next = 0;
            }
            row = cellRow;
            covering.RemoveAll(m => m.Bottom < cellRow);
            for (; next < byTop.Length && byTop[next].Top <= cellRow; next++)
            {
                var merge = byTop[next];
                if (merge.Bottom >= cellRow)
                {
                    var at = covering.FindLastIndex(m => m.Left <= merge.Left) + 1;
                    covering.Insert(at, merge);
                }
            }
        }
    }

    private static string Reference(int row, int column) => ColumnName(column) + row.ToString(CultureInfo.InvariantCulture);

    // A column's letters, A for 1.
    private static string ColumnName(int column)
    {
        var letters = new StringBuilder();
        for (var c = column; c > 0; c = (c - 1) / 26)
        {
            letters.Insert(0, (char)('A' + ((c - 1) % 26)));
        }
        return letters.ToString();
    }

    // The number formats of the workbook's cell styles, by style index: whether each shows a
    // date, a time, or both, or neither.
    internal sealed class NumberFormats
    {
        private readonly List<uint> styles = [];
        private readonly Dictionary<uint, string> codes = [];

        public NumberFormats(S.Stylesheet? stylesheet)
        {
            foreach (var format in stylesheet?.NumberingFormats?.Elements<S.NumberingFormat>() ?? [])
            {
                if (format.NumberFormatId?.Value is { } id && format.FormatCode?.Value is { } code)
                {
                    codes[id] = code;
                }
            }
            foreach (var style in stylesheet?.CellFormats?.Elements<S.CellFormat>() ?? [])
            {
                styles.Add(style.NumberFormatId?.Value ?? 0);
            }
        }

        public (bool Date, bool Time)? DateParts(uint style)
        {
            var id = style < styles.Count ? styles[(int)style] : 0;
            var parts = codes.TryGetValue(id, out var code) ? DateFormat.Parts(code) : BuiltIn(id);
            return parts.Date || parts.Time ? parts : null;
        }

        // The built-in date and time formats: 14 to 17 dates, 18 to 21 and 45 to 47 times, 22
        // both. The others docproc-ooxml leaves raw, the locale-dependent ones among them.
        private static (bool Date, bool Time) BuiltIn(uint id) => id switch
        {
            >= 14 and <= 17 => (true, false),
            >= 18 and <= 21 or >= 45 and <= 47 => (false, true),
            22 => (true, true),
            _ => (false, false),
        };
    }
}

/// <summary>A number format code's date and time parts, from its tokens outside quoted text,
/// escaped and skipped characters, and brackets (colors, conditions, locales). A format with an
/// elapsed-time bracket, [h], [m], or [s], has neither: its value is a duration, which can pass a
/// day, so it stays a number, as stored (context/design.md, OX-17).</summary>
internal static class DateFormat
{
    public static (bool Date, bool Time) Parts(string code)
    {
        var tokens = new List<char>();
        for (var i = 0; i < code.Length; i++)
        {
            var c = char.ToLowerInvariant(code[i]);
            switch (c)
            {
                case '"':
                    i = code.IndexOf('"', i + 1) is var end and >= 0 ? end : code.Length;
                    break;
                case '\\' or '_' or '*':
                    i++;
                    break;
                case '[':
                    var close = code.IndexOf(']', i + 1);
                    var inside = close < 0 ? "" : code[(i + 1)..close].ToLowerInvariant();
                    if (inside.Length > 0 && inside.All(ch => ch == inside[0]) && inside[0] is 'h' or 'm' or 's')
                    {
                        return (false, false);
                    }
                    i = close < 0 ? code.Length : close;
                    break;
                case 'y' or 'd' or 'h' or 'm' or 's':
                    tokens.Add(c);
                    break;
                case 'a' when Matches(code, i, "am/pm") || Matches(code, i, "a/p"):
                    tokens.Add('a');
                    i += Matches(code, i, "am/pm") ? 4 : 2;
                    break;
                default:
                    break;
            }
        }
        var date = false;
        var time = false;
        for (var t = 0; t < tokens.Count; t++)
        {
            switch (tokens[t])
            {
                case 'y' or 'd':
                    date = true;
                    break;
                case 'h' or 's' or 'a':
                    time = true;
                    break;
                default:
                    // An m is minutes after an hour or before a second, and months otherwise.
                    var previous = Previous(tokens, t);
                    var next = Next(tokens, t);
                    if (previous == 'h' || next == 's')
                    {
                        time = true;
                    }
                    else
                    {
                        date = true;
                    }
                    break;
            }
        }
        return (date, time);
    }

    private static bool Matches(string code, int at, string token) =>
        at + token.Length <= code.Length && string.Compare(code, at, token, 0, token.Length, StringComparison.OrdinalIgnoreCase) == 0;

    // The nearest token before or after t that isn't another m of the same run.
    private static char? Previous(List<char> tokens, int t)
    {
        for (var i = t - 1; i >= 0; i--)
        {
            if (tokens[i] != 'm')
            {
                return tokens[i];
            }
        }
        return null;
    }

    private static char? Next(List<char> tokens, int t)
    {
        for (var i = t + 1; i < tokens.Count; i++)
        {
            if (tokens[i] != 'm')
            {
                return tokens[i];
            }
        }
        return null;
    }
}

/// <summary>A spreadsheet serial date in ISO 8601. In the 1900 date system serial 1 is
/// 1 January 1900 and serial 60 is 29 February 1900, a day that never was, kept from Lotus 1-2-3;
/// in the 1904 system serial 0 is 1 January 1904. Times are rounded to the millisecond.</summary>
internal static class SerialDate
{
    private const long MillisecondsPerDay = 86_400_000;

    /// <summary>The serial as a date when the format shows no time, as a time when it shows no
    /// date and the serial is under a day, and as both otherwise; null when it is no date: a
    /// negative serial, the 1900 system's 29 February, or past 9999.</summary>
    public static string? Iso(double serial, bool date1904, bool hasDate, bool hasTime)
    {
        if (serial < 0 || serial >= 3_000_000)
        {
            return null;
        }
        var milliseconds = (long)Math.Round(serial * MillisecondsPerDay, MidpointRounding.AwayFromZero);
        if (!hasDate && milliseconds < MillisecondsPerDay)
        {
            return Time(TimeSpan.FromMilliseconds(milliseconds));
        }
        DateTime epoch;
        if (date1904)
        {
            epoch = new DateTime(1904, 1, 1, 0, 0, 0, DateTimeKind.Unspecified);
        }
        else if (serial < 60)
        {
            epoch = new DateTime(1899, 12, 31, 0, 0, 0, DateTimeKind.Unspecified);
        }
        else if (serial >= 61)
        {
            epoch = new DateTime(1899, 12, 30, 0, 0, 0, DateTimeKind.Unspecified);
        }
        else
        {
            return null;
        }
        if (milliseconds > (DateTime.MaxValue - epoch).TotalMilliseconds)
        {
            return null;
        }
        var value = epoch.AddMilliseconds(milliseconds);
        var day = value.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture);
        return hasTime ? day + "T" + Time(value.TimeOfDay) : day;
    }

    private static string Time(TimeSpan time) =>
        time.ToString(time.Milliseconds == 0 ? @"hh\:mm\:ss" : @"hh\:mm\:ss\.fff", CultureInfo.InvariantCulture);
}
