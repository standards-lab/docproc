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

// See docs/tool-contract.md, "docproc-ooxml".
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

    private static string Place(int index) => $"sheet {index + 1}";

    private static void ReadObject(XlsxContext context, WorksheetPart part, OpenXmlElement element, int sheet, List<Block> blocks)
    {
        List<OpenXmlElement> objects = [.. new[] { element }.Concat(element.Descendants()).Where(e => e.LocalName == "oleObject")];
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

    internal sealed record Merge(int Top, int Left, int Bottom, int Right)
    {
        public int Rows => Bottom - Top + 1;

        public int Columns => Right - Left + 1;
    }

    private sealed class MergeIndex
    {
        private readonly Merge[] byTop;
        private readonly List<Merge> covering = [];
        private int next;
        private int row;

        public MergeIndex(IEnumerable<Merge> merges)
        {
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

    private static string ColumnName(int column)
    {
        var letters = new StringBuilder();
        for (var c = column; c > 0; c = (c - 1) / 26)
        {
            letters.Insert(0, (char)('A' + ((c - 1) % 26)));
        }
        return letters.ToString();
    }

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

        private static (bool Date, bool Time) BuiltIn(uint id) => id switch
        {
            >= 14 and <= 17 => (true, false),
            >= 18 and <= 21 or >= 45 and <= 47 => (false, true),
            22 => (true, true),
            _ => (false, false),
        };
    }
}

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
