using System.Runtime.ExceptionServices;
using System.Runtime.InteropServices;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Writing;
using S = DocumentFormat.OpenXml.Spreadsheet;

namespace Docproc.Ooxml.Reading;

/// <summary>A workbook the validate pass has read whole, nothing in it refused: its sheets, the
/// media its drawings hold, and what was skipped. Its sheets' rows are read again, forward, as
/// they are written.</summary>
internal sealed class XlsxWorkbook
{
    private readonly XlsxReader.XlsxContext context;

    internal XlsxWorkbook(XlsxReader.XlsxContext context, IReadOnlyList<XlsxSheet> sheets, IReadOnlyList<Warning> warnings)
    {
        this.context = context;
        Sheets = sheets;
        Warnings = warnings;
    }

    /// <summary>The sheets that are read, in workbook order, each the ordinal of its sheet line.</summary>
    public IReadOnlyList<XlsxSheet> Sheets { get; }

    /// <summary>The media the sheets' drawings hold, by file name.</summary>
    public IReadOnlyDictionary<string, byte[]> Media => context.Media.Files;

    /// <summary>What the validate pass skipped: a sheet that names no part, a properties part
    /// that can't be parsed or repeats a key, and the unread kinds the workbook holds.</summary>
    public IReadOnlyList<Warning> Warnings { get; }

    /// <summary>The package's properties, or null when it has none.</summary>
    public Properties? Properties { get; init; }

    /// <summary>The write pass of a sheet: its table's rows, each read from its part as it is
    /// enumerated. The validate pass already read the part, so reading it again finds nothing it
    /// didn't.</summary>
    public IEnumerable<TableRow> Rows(XlsxSheet sheet) =>
        sheet.Cells is { Columns.Count: > 0 } cells ? XlsxReader.Rows(context, cells) : [];

    /// <summary>Writes the workbook's lines to the output, each sheet's rows read as they are
    /// written, so no more of a sheet is held than its next <c>rows</c> line.</summary>
    public void Write(Stream output) =>
        Lines.WriteXlsx(
            Properties,
            Lines.MediaOf(new Document(SourceFormat.Xlsx, [.. Sheets.Select(sheet => new Model.Sheet(sheet.Name, sheet.Hidden, sheet.Drawings))])),
            Sheets.Select(sheet => new SheetSource(sheet.Name, sheet.Hidden, sheet.Cells?.Columns ?? [], Rows(sheet), sheet.Drawings)),
            output);
}

/// <summary>A sheet as the validate pass leaves it: its name, whether it is hidden, its cells'
/// part and what the write pass needs of it (null for a sheet with no cells, a chartsheet's), and
/// its charts and pictures in the order its drawing anchors them.</summary>
internal sealed record XlsxSheet(string Name, bool Hidden, XlsxReader.SheetCells? Cells, IReadOnlyList<Block> Drawings);

internal static partial class XlsxReader
{
    /// <summary>What the validate pass collects of a worksheet for its write pass: the column
    /// letters its table uses, in the sheet's order (none when it has no table), its merged
    /// ranges, its shared formulas' anchors by shared index, and its external hyperlinks' ranges
    /// with their targets. A forward reading meets the merges and hyperlinks after the cells and
    /// can meet an anchor after a cell that names it, so the rows can't be written until a first
    /// reading has them.</summary>
    internal sealed record SheetCells(WorksheetPart Part, IReadOnlyList<string> Columns, IReadOnlyList<Merge> Merges, IReadOnlyDictionary<uint, string> Anchors, IReadOnlyList<(Merge Range, string Target)> Links);

    // The validate pass of a worksheet: one forward reading of its part, holding one row at a
    // time, that finds what would refuse it and collects its merges, its shared-formula anchors,
    // its external hyperlinks, the columns its kept cells use, and its OLE objects, as elements
    // (each an oleObject or the alternate content holding one). It refuses with the failure a
    // reading of the whole part would have met first: the part's XML, wherever it fails, then a
    // shared formula's attributes, then the first failure among the cells in order, a covered
    // cell's left out. A cell's failure counts only when no merge covers it, and the merges come
    // after the cells, so the failing cells are kept until then as the columns' cells are, by
    // column as runs of row numbers, never one by one; a sheet with one that counts is read again
    // to find the first (FirstKeptFailure).
    private static (SheetCells Cells, List<OpenXmlElement> Objects) ValidateCells(XlsxContext context, WorksheetPart part)
    {
        var merges = new List<Merge>();
        var links = new List<(Merge, string)>();
        var objects = new List<OpenXmlElement>();
        var anchors = new Dictionary<uint, string>();
        var candidates = new ColumnCandidates();
        ExceptionDispatchInfo? anchorError = null;
        var failing = new Dictionary<int, List<RowRun>>();
        // A row's number or a cell's reference that fails: no later failure can come first, so
        // the cells after it are not read.
        ExceptionDispatchInfo? stopped = null;
        var rowNumber = 0;
        foreach (var element in Elements(part, rowsOnly: false))
        {
            if (element is S.MergeCell merge)
            {
                if (merge.Reference?.Value is { } range && Range(range) is { } found)
                {
                    merges.Add(found);
                }
                continue;
            }
            // A hyperlink to a place in the workbook (its location alone) stays text.
            if (element is S.Hyperlink hyperlink)
            {
                if (hyperlink.Reference?.Value is { } range && Range(range) is { } linked
                    && MediaStore.Hyperlink(part, hyperlink.Id?.Value) is { } target)
                {
                    links.Add((linked, target));
                }
                continue;
            }
            if (element is not S.Row)
            {
                objects.Add(element);
                continue;
            }
            var row = (S.Row)element;
            if (anchorError is null)
            {
                try
                {
                    foreach (var cell in row.Elements<S.Cell>())
                    {
                        AddAnchor(anchors, cell);
                    }
                }
                catch (Exception e)
                {
                    anchorError = ExceptionDispatchInfo.Capture(e);
                }
            }
            if (stopped is not null)
            {
                continue;
            }
            try
            {
                rowNumber = RowNumber(row, rowNumber);
                var column = 0;
                foreach (var cell in row.Elements<S.Cell>())
                {
                    var (cellRow, cellColumn) = CellPosition(cell, rowNumber, column);
                    column = cellColumn;
                    try
                    {
                        var (held, shared) = ReadCell(context, cell);
                        if (held)
                        {
                            candidates.Add(cellColumn, cellRow);
                        }
                        else if (shared is { } si)
                        {
                            candidates.AddShared(cellColumn, si, cellRow);
                        }
                    }
                    catch (Exception)
                    {
                        AddRun(failing, cellColumn, cellRow);
                    }
                }
            }
            catch (Exception e)
            {
                stopped = ExceptionDispatchInfo.Capture(e);
            }
        }
        anchorError?.Throw();
        var index = new MergeIndex(merges);
        if (failing.Any(entry => entry.Value.Any(run => AnyKept(index, entry.Key, run))))
        {
            FirstKeptFailure(context, part, index).Throw();
        }
        stopped?.Throw();
        return (new SheetCells(part, candidates.Columns(index, anchors), merges, anchors, links), objects);
    }

    // What the validate pass reads of a cell, as the write pass reads it: whether it holds a value
    // or its own formula, and when it holds neither, the index of the shared formula it names, if
    // any. A cell that can't be read throws.
    private static (bool Held, uint? Shared) ReadCell(XlsxContext context, S.Cell cell)
    {
        var formula = cell.CellFormula;
        var text = OwnFormula(formula);
        uint? shared = text is null && formula?.FormulaType?.Value == S.CellFormulaValues.Shared ? formula.SharedIndex?.Value : null;
        return (Value(context, cell) is not null || text is not null, shared);
    }

    // The failure of the first cell in document order that fails and that no merge covers, in a
    // sheet whose validate pass found one: a second reading of its rows, the merges now known,
    // that stops at that cell. The validate pass read the part whole without a fault in its XML
    // and stopped no earlier than that cell, so the reading meets it before any other failure.
    // Only a refused sheet is read so, before anything is written.
    private static ExceptionDispatchInfo FirstKeptFailure(XlsxContext context, WorksheetPart part, MergeIndex merges)
    {
        var rowNumber = 0;
        foreach (var element in Elements(part, rowsOnly: true))
        {
            var row = (S.Row)element;
            rowNumber = RowNumber(row, rowNumber);
            var column = 0;
            foreach (var cell in row.Elements<S.Cell>())
            {
                var (cellRow, cellColumn) = CellPosition(cell, rowNumber, column);
                column = cellColumn;
                if (!Kept(merges, cellRow, cellColumn))
                {
                    continue;
                }
                try
                {
                    ReadCell(context, cell);
                }
                catch (Exception e)
                {
                    return ExceptionDispatchInfo.Capture(e);
                }
            }
        }
        throw new InvalidOperationException("the validate pass found a failing cell no merge covers that a second reading did not");
    }

    // The write pass of a worksheet: its rows, read forward one at a time, each holding the cells
    // that have a value or a formula, row by row as stored. A merged range's anchor carries its
    // spans; the cells it covers are left out, as a docx table's are, and a range whose anchor
    // holds nothing leaves nothing. A shared formula's text is on its anchor, which the other
    // cells name.
    internal static IEnumerable<TableRow> Rows(XlsxContext context, SheetCells sheet)
    {
        var merges = new MergeIndex(sheet.Merges);
        var rowNumber = 0;
        foreach (var element in Elements(sheet.Part, rowsOnly: true))
        {
            var row = (S.Row)element;
            rowNumber = RowNumber(row, rowNumber);
            var column = 0;
            var cells = new List<TableCell>();
            foreach (var cell in row.Elements<S.Cell>())
            {
                var (cellRow, cellColumn) = CellPosition(cell, rowNumber, column);
                column = cellColumn;
                var anchored = merges.Find(cellRow, cellColumn);
                if (anchored is not null && (anchored.Top != cellRow || anchored.Left != cellColumn))
                {
                    continue;
                }
                var formula = cell.CellFormula;
                var text = OwnFormula(formula);
                string? sharedFrom = null;
                if (text is null && formula?.FormulaType?.Value == S.CellFormulaValues.Shared && formula.SharedIndex?.Value is { } si)
                {
                    sharedFrom = sheet.Anchors.GetValueOrDefault(si);
                }
                var value = Value(context, cell);
                if (value is null && text is null && sharedFrom is null)
                {
                    continue;
                }
                cells.Add(new TableCell(null)
                {
                    Ref = Reference(cellRow, cellColumn),
                    Target = LinkOf(sheet.Links, cellRow, cellColumn),
                    Value = value,
                    Formula = text,
                    SharedFrom = sharedFrom,
                    ColSpan = anchored is { Columns: > 1 } ? anchored.Columns : null,
                    RowSpan = anchored is { Rows: > 1 } ? anchored.Rows : null,
                });
            }
            if (cells.Count > 0)
            {
                yield return new TableRow(cells) { Row = rowNumber };
            }
        }
    }

    // The target of the first of a sheet's external hyperlinks whose range holds the cell, or
    // null. A sheet's hyperlinks are few, held as ranges, never cell by cell.
    private static string? LinkOf(IReadOnlyList<(Merge Range, string Target)> links, int row, int column)
    {
        foreach (var (range, target) in links)
        {
            if (row >= range.Top && row <= range.Bottom && column >= range.Left && column <= range.Right)
            {
                return target;
            }
        }
        return null;
    }

    // A row's number: as stored, or with none the one after the row before it.
    private static int RowNumber(S.Row row, int previous) =>
        row.RowIndex?.Value is { } stored
            ? stored is >= 1 and <= MaxRow ? (int)stored : throw new InvalidDataException($"a row's number {stored} is out of range")
            : previous + 1;

    // A cell's position. Its reference is optional; one without follows the cell before it.
    private static (int Row, int Column) CellPosition(S.Cell cell, int rowNumber, int previousColumn) =>
        cell.CellReference?.Value is { } reference
            ? Position(reference) ?? throw new InvalidDataException($"a cell's reference {reference} is not a cell reference")
            : (rowNumber, previousColumn + 1);

    // A formula's own text, or null when it has none, as a shared formula's cells other than its
    // anchor have.
    private static string? OwnFormula(S.CellFormula? formula) => formula?.Text is { Length: > 0 } own ? own : null;

    // A shared formula's anchor, by its shared index: the first cell whose formula holds the text.
    private static void AddAnchor(Dictionary<uint, string> anchors, S.Cell cell)
    {
        if (cell.CellFormula is { Text.Length: > 0 } formula
            && formula.FormulaType?.Value == S.CellFormulaValues.Shared
            && formula.SharedIndex?.Value is { } si
            && cell.CellReference?.Value is { } reference)
        {
            anchors.TryAdd(si, reference);
        }
    }

    // Whether a cell is read: a cell a merged range covers is, only as the range's anchor.
    private static bool Kept(MergeIndex merges, int row, int column) =>
        merges.Find(row, column) is not { } merge || (merge.Top == row && merge.Left == column);

    // The elements of a worksheet part docproc-ooxml reads, in one forward reading of it: the rows
    // of its first sheetData and, unless rowsOnly, the ranges of its first mergeCells, the
    // hyperlinks of its first hyperlinks, and the children of its first oleObjects, each loaded
    // alone, so no more than one row is held at a time. Only the worksheet's own children count:
    // a row inside alternate content is passed over. As the SDK reads a part whole, one of under
    // four bytes holds no worksheet, and one whose root is no worksheet is invalid data.
    private static IEnumerable<OpenXmlElement> Elements(WorksheetPart part, bool rowsOnly)
    {
        if (!HasWorksheet(part))
        {
            yield break;
        }
        using var reader = OpenXmlReader.Create(part);
        if (!reader.Read() || reader.ElementType != typeof(S.Worksheet))
        {
            throw new InvalidDataException("Cannot load the root element from the part. The part contains invalid data.");
        }
        var sheetData = 0;
        var mergeCells = 0;
        var hyperlinks = 0;
        var oleObjects = 0;
        var inSheetData = false;
        var inMergeCells = false;
        var inHyperlinks = false;
        var inOleObjects = false;
        while (reader.Read())
        {
            if (reader.Depth == 1 && reader.IsStartElement)
            {
                if (reader.ElementType == typeof(S.SheetData))
                {
                    inSheetData = sheetData++ == 0;
                }
                else if (reader.ElementType == typeof(S.MergeCells))
                {
                    inMergeCells = mergeCells++ == 0;
                }
                else if (reader.ElementType == typeof(S.Hyperlinks))
                {
                    inHyperlinks = hyperlinks++ == 0;
                }
                else if (reader.ElementType == typeof(S.OleObjects))
                {
                    inOleObjects = oleObjects++ == 0;
                }
            }
            else if (reader.Depth == 1 && reader.IsEndElement)
            {
                if (inSheetData && rowsOnly)
                {
                    yield break;
                }
                inSheetData = false;
                inMergeCells = false;
                inHyperlinks = false;
                inOleObjects = false;
            }
            else if (reader.Depth == 2 && reader.IsStartElement
                && ((inSheetData && reader.ElementType == typeof(S.Row))
                    || (inMergeCells && reader.ElementType == typeof(S.MergeCell))
                    || (inHyperlinks && reader.ElementType == typeof(S.Hyperlink))
                    || inOleObjects))
            {
                // Loading leaves the reader on the element's end.
                yield return reader.LoadCurrentElement()!;
            }
        }
    }

    private static bool HasWorksheet(WorksheetPart part)
    {
        using var stream = part.GetStream(FileMode.Open, FileAccess.Read);
        return stream.Length >= 4;
    }

    // The cells of a sheet that hold something, by column, as runs of row numbers, until the
    // sheet's merges and shared-formula anchors tell which are kept. A cell that holds only a
    // shared formula's index is kept only when the index has an anchor. A column is used when a
    // kept cell is in it. Runs keep a sheet's filled columns to a few numbers each.
    private sealed class ColumnCandidates
    {
        private readonly Dictionary<int, List<RowRun>> cells = [];
        private readonly Dictionary<(int Column, uint Index), List<RowRun>> shared = [];

        public void Add(int column, int row) => AddRun(cells, column, row);

        public void AddShared(int column, uint index, int row) => AddRun(shared, (column, index), row);

        // The used columns' letters, in the sheet's order.
        public List<string> Columns(MergeIndex merges, Dictionary<uint, string> anchors)
        {
            var byColumn = cells.ToDictionary(entry => entry.Key, entry => (IEnumerable<RowRun>)entry.Value);
            foreach (var ((column, index), runs) in shared)
            {
                if (anchors.ContainsKey(index))
                {
                    byColumn[column] = byColumn.TryGetValue(column, out var other) ? other.Concat(runs) : runs;
                }
            }
            return [.. byColumn.Where(entry => entry.Value.Any(run => AnyKept(merges, entry.Key, run))).Select(entry => entry.Key).Order().Select(ColumnName)];
        }
    }

    // Adds a row to a key's runs: to its last run when the row extends it, else as a run of its
    // own, so cells down a column in order keep to one run.
    private static void AddRun<TKey>(Dictionary<TKey, List<RowRun>> runs, TKey key, int row)
        where TKey : notnull
    {
        ref var list = ref CollectionsMarshal.GetValueRefOrAddDefault(runs, key, out _);
        list ??= [];
        if (list.Count > 0 && list[^1] is var last && row >= last.First && row <= last.Last + 1)
        {
            list[^1] = last with { Last = Math.Max(last.Last, row) };
        }
        else
        {
            list.Add(new RowRun(row, row));
        }
    }

    // Whether any of a run's cells in a column is read, no merge covering it.
    private static bool AnyKept(MergeIndex merges, int column, RowRun run)
    {
        for (var row = run.First; row <= run.Last; row++)
        {
            if (Kept(merges, row, column))
            {
                return true;
            }
        }
        return false;
    }

    private readonly record struct RowRun(int First, int Last);
}
