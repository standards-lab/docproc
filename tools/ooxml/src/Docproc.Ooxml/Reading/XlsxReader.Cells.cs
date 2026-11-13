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

    /// <summary>What the validate pass skipped: a sheet that names no part.</summary>
    public IReadOnlyList<Warning> Warnings { get; }

    /// <summary>The write pass of a sheet: its table's rows, each read from its part as it is
    /// enumerated. The validate pass already read the part, so reading it again finds nothing it
    /// didn't.</summary>
    public IEnumerable<TableRow> Rows(XlsxSheet sheet) =>
        sheet.Cells is { Columns.Count: > 0 } cells ? XlsxReader.Rows(context, cells) : [];

    /// <summary>Writes the workbook's lines to the output, each sheet's rows read as they are
    /// written, so no more of a sheet is held than its next <c>rows</c> line.</summary>
    public void Write(Stream output) =>
        Lines.WriteXlsx(
            Lines.Pictures(new Document(SourceFormat.Xlsx, [.. Sheets.Select(sheet => new Model.Sheet(sheet.Name, sheet.Hidden, sheet.Drawings))])),
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
    /// ranges, and its shared formulas' anchors by shared index. A forward reading meets the
    /// merges after the cells and can meet an anchor after a cell that names it, so the rows
    /// can't be written until a first reading has them.</summary>
    internal sealed record SheetCells(WorksheetPart Part, IReadOnlyList<string> Columns, IReadOnlyList<Merge> Merges, IReadOnlyDictionary<uint, string> Anchors);

    // A failure found in a sheet's cells, kept until the sheet's merges are known: one at a cell
    // (Row and Column from 1) counts only when no merge covers the cell, as a covered cell is
    // never read; one at row 0 counts always.
    private sealed record CellError(int Row, int Column, ExceptionDispatchInfo Error);

    // The validate pass of a worksheet: one forward reading of its part, holding one row at a
    // time, that finds what would refuse it and collects its merges, its shared-formula anchors,
    // and the columns its kept cells use. It refuses with the failure a reading of the whole part
    // would have met first: the part's XML, wherever it fails, then a shared formula's attributes,
    // then the first failure among the cells in order, a covered cell's left out.
    private static SheetCells ValidateCells(XlsxContext context, WorksheetPart part)
    {
        var merges = new List<Merge>();
        var anchors = new Dictionary<uint, string>();
        var candidates = new ColumnCandidates();
        ExceptionDispatchInfo? anchorError = null;
        var errors = new List<CellError>();
        var stopped = false;
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
            if (stopped)
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
                        var formula = cell.CellFormula;
                        var text = OwnFormula(formula);
                        uint? shared = text is null && formula?.FormulaType?.Value == S.CellFormulaValues.Shared ? formula.SharedIndex?.Value : null;
                        if (Value(context, cell) is not null || text is not null)
                        {
                            candidates.Add(cellColumn, cellRow);
                        }
                        else if (shared is { } si)
                        {
                            candidates.AddShared(cellColumn, si, cellRow);
                        }
                    }
                    catch (Exception e)
                    {
                        errors.Add(new CellError(cellRow, cellColumn, ExceptionDispatchInfo.Capture(e)));
                    }
                }
            }
            catch (Exception e)
            {
                // A row's number or a cell's reference fails wherever the merges are: no later
                // failure can come first.
                errors.Add(new CellError(0, 0, ExceptionDispatchInfo.Capture(e)));
                stopped = true;
            }
        }
        anchorError?.Throw();
        var index = new MergeIndex(merges);
        foreach (var error in errors)
        {
            if (error.Row == 0 || Kept(index, error.Row, error.Column))
            {
                error.Error.Throw();
            }
        }
        return new SheetCells(part, candidates.Columns(index, anchors), merges, anchors);
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
    // of its first sheetData and, unless rowsOnly, the ranges of its first mergeCells, each loaded
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
        var inSheetData = false;
        var inMergeCells = false;
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
            }
            else if (reader.Depth == 1 && reader.IsEndElement)
            {
                if (inSheetData && rowsOnly)
                {
                    yield break;
                }
                inSheetData = false;
                inMergeCells = false;
            }
            else if (reader.Depth == 2 && reader.IsStartElement
                && ((inSheetData && reader.ElementType == typeof(S.Row)) || (inMergeCells && reader.ElementType == typeof(S.MergeCell))))
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

        public void Add(int column, int row) => Add(cells, column, row);

        public void AddShared(int column, uint index, int row) => Add(shared, (column, index), row);

        private static void Add<TKey>(Dictionary<TKey, List<RowRun>> runs, TKey key, int row)
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
            return [.. byColumn.Where(entry => entry.Value.Any(run => Used(merges, entry.Key, run))).Select(entry => entry.Key).Order().Select(ColumnName)];
        }

        private static bool Used(MergeIndex merges, int column, RowRun run)
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
    }

    private readonly record struct RowRun(int First, int Last);
}
