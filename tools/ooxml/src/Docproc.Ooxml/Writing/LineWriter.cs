using System.Buffers;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using Docproc.Ooxml.Model;

namespace Docproc.Ooxml.Writing;

/// <summary>docproc-ooxml's output contract, schema "2" (context/layers.md, "docproc-ooxml"): JSON
/// Lines, one compact object per line, each line's fields spelled out in order, in snake_case, a
/// field with no value left out. The lines come in the order they are written: the
/// <c>document</c> line first, then for each xlsx sheet its <c>sheet</c> line and its
/// <c>rows</c> lines.</summary>
/// <remarks>Each line is written to the output whole as it is finished, so a caller that reads a
/// sheet forward writes its lines as it goes, and a sheet's rows never wait in memory past the
/// bound of one <c>rows</c> line.</remarks>
internal sealed class LineWriter(Stream output)
{
    /// <summary>The schema version this build writes, on the <c>document</c> line. It changes only
    /// when a field is cut or reshaped; the lines grow by addition within it.</summary>
    public const string Schema = "2";

    /// <summary>The bytes a <c>rows</c> line holds at most, its newline included, unless one row
    /// alone needs more: a line holds as many of the sheet's next rows as fit, and at least one.</summary>
    public const int RowsLineBound = 256 * 1024;

    // Non-ASCII text is written as itself rather than as \u escapes: the lines are data for
    // docproc, never embedded in HTML, so the relaxed encoder's one risk doesn't apply.
    private static readonly JsonWriterOptions Options = new() { Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping };

    // What every rows line costs before its rows and their Markdown: its fields' names and
    // punctuation, and its newline.
    private static readonly int RowsLineOverhead = """{"line":"rows","markdown":"","rows":[]}"""u8.Length + 1;

    private readonly ArrayBufferWriter<byte> buffer = new();

    // The rows of the sheet's next rows line: each row's JSON and its Markdown, and the line's
    // size so far.
    private readonly List<byte[]> rows = [];
    private readonly StringBuilder rowsMarkdown = new();
    private long rowsSize = RowsLineOverhead;

    /// <summary>Writes the <c>document</c> line: the schema, the format, the package's properties,
    /// the Markdown of no sheet (an xlsx document's is empty), and every medium in document
    /// order.</summary>
    public void WriteDocument(SourceFormat format, Properties? properties, string markdown, IReadOnlyList<Media> media)
    {
        using var json = new Utf8JsonWriter(buffer, Options);
        json.WriteStartObject();
        json.WriteString("line", "document");
        json.WriteString("schema", Schema);
        json.WriteString("format", format switch
        {
            SourceFormat.Docx => "docx",
            SourceFormat.Xlsx => "xlsx",
            _ => "pptx",
        });
        if (properties is not null)
        {
            json.WriteStartObject("properties");
            WriteProperties(json, "core", properties.Core);
            WriteProperties(json, "app", properties.App);
            WriteProperties(json, "custom", properties.Custom);
            json.WriteEndObject();
        }
        WriteOptional(json, "markdown", markdown);
        if (media.Count > 0)
        {
            json.WriteStartArray("media");
            foreach (var medium in media)
            {
                WriteMedium(json, medium);
            }
            json.WriteEndArray();
        }
        json.WriteEndObject();
        json.Flush();
        WriteLine();
    }

    /// <summary>Writes a <c>sheet</c> line: the sheet's name, whether it is hidden, and its first
    /// Markdown fragment, its heading and its table's header. The previous sheet's rows must be
    /// finished first.</summary>
    public void WriteSheet(string name, bool hidden, string markdown)
    {
        if (rows.Count > 0)
        {
            throw new InvalidOperationException("the previous sheet's rows were not finished");
        }
        using var json = new Utf8JsonWriter(buffer, Options);
        json.WriteStartObject();
        json.WriteString("line", "sheet");
        json.WriteString("name", name);
        if (hidden)
        {
            json.WriteBoolean("hidden", true);
        }
        WriteOptional(json, "markdown", markdown);
        json.WriteEndObject();
        json.Flush();
        WriteLine();
    }

    /// <summary>Adds the sheet's next row and its Markdown table row to the sheet's next
    /// <c>rows</c> line, writing the line first when the row would take it past the bound.</summary>
    public void WriteRow(TableRow row, string markdown)
    {
        var bytes = RowJson(row);
        // A row adds its JSON, its Markdown as escaped, and a comma after the line's first row.
        var size = bytes.Length + JsonEncodedText.Encode(markdown, Options.Encoder).EncodedUtf8Bytes.Length + (rows.Count > 0 ? 1 : 0);
        if (rows.Count > 0 && rowsSize + size > RowsLineBound)
        {
            WriteRows();
            size--;
        }
        rows.Add(bytes);
        rowsMarkdown.Append(markdown);
        rowsSize += size;
    }

    /// <summary>Finishes a sheet: writes its last <c>rows</c> line, and then its closing
    /// <c>rows</c> line, holding its drawings' Markdown and no rows, when it has any.</summary>
    public void EndSheet(string drawingMarkdown)
    {
        if (rows.Count > 0)
        {
            WriteRows();
        }
        if (drawingMarkdown.Length > 0)
        {
            output.Write(RowsLine(drawingMarkdown, []));
        }
    }

    private void WriteRows()
    {
        output.Write(RowsLine(rowsMarkdown.ToString(), rows));
        rows.Clear();
        rowsMarkdown.Clear();
        rowsSize = RowsLineOverhead;
    }

    // A rows line, its newline included: its Markdown, and its rows, left out when there are none,
    // as on a sheet's closing line.
    private static byte[] RowsLine(string markdown, List<byte[]> rows)
    {
        var line = new ArrayBufferWriter<byte>();
        using (var json = new Utf8JsonWriter(line, Options))
        {
            json.WriteStartObject();
            json.WriteString("line", "rows");
            WriteOptional(json, "markdown", markdown);
            if (rows.Count > 0)
            {
                json.WriteStartArray("rows");
                foreach (var row in rows)
                {
                    json.WriteRawValue(row, skipInputValidation: true);
                }
                json.WriteEndArray();
            }
            json.WriteEndObject();
        }
        line.Write("\n"u8);
        return line.WrittenSpan.ToArray();
    }

    // A row and its cells: the model's TableCell less its docx-only fields.
    private static byte[] RowJson(TableRow row)
    {
        var bytes = new ArrayBufferWriter<byte>();
        using (var json = new Utf8JsonWriter(bytes, Options))
        {
            json.WriteStartObject();
            json.WriteNumber("row", row.Row ?? 0);
            json.WriteStartArray("cells");
            foreach (var cell in row.Cells)
            {
                json.WriteStartObject();
                json.WriteString("ref", cell.Ref);
                if (cell.Value is { } value)
                {
                    json.WriteString("value_type", value.Type switch
                    {
                        CellValueType.Number => "number",
                        CellValueType.Text => "text",
                        CellValueType.Boolean => "boolean",
                        CellValueType.Error => "error",
                        _ => "date",
                    });
                    switch (value.Type)
                    {
                        case CellValueType.Number:
                            json.WriteNumber("value", value.Number);
                            break;
                        case CellValueType.Boolean:
                            json.WriteBoolean("value", value.Boolean);
                            break;
                        default:
                            json.WriteString("value", value.Text);
                            break;
                    }
                }
                WriteOptional(json, "formula", cell.Formula);
                WriteOptional(json, "shared_from", cell.SharedFrom);
                WriteOptional(json, "col_span", cell.ColSpan);
                WriteOptional(json, "row_span", cell.RowSpan);
                json.WriteEndObject();
            }
            json.WriteEndArray();
            json.WriteEndObject();
        }
        return bytes.WrittenSpan.ToArray();
    }

    // A map of properties, in stored order, each value as its JSON type; left out when empty.
    private static void WriteProperties(Utf8JsonWriter json, string name, IReadOnlyList<Property>? properties)
    {
        if (properties is not { Count: > 0 })
        {
            return;
        }
        json.WriteStartObject(name);
        foreach (var property in properties)
        {
            switch (property.Value.Type)
            {
                case PropertyValueType.Number:
                    json.WritePropertyName(property.Key);
                    json.WriteRawValue(property.Value.Text, skipInputValidation: true);
                    break;
                case PropertyValueType.Boolean:
                    json.WriteBoolean(property.Key, property.Value.Text == "true");
                    break;
                default:
                    json.WriteString(property.Key, property.Value.Text);
                    break;
            }
        }
        json.WriteEndObject();
    }

    // A medium: its kind, its file or a linked picture's target, its content type, part, and
    // program id, and its placement, the one of section, sheet, or slide its format has: the docx
    // section's index, the ordinal of the xlsx sheet's sheet line, or the pptx slide's index
    // (context/layers.md, "docproc-ooxml").
    private static void WriteMedium(Utf8JsonWriter json, Media medium)
    {
        json.WriteStartObject();
        json.WriteString("kind", medium.Kind switch
        {
            MediaKind.Embedded => "embedded",
            MediaKind.Ole => "ole",
            _ => "picture",
        });
        WriteOptional(json, "file", medium.Name);
        WriteOptional(json, "content_type", medium.ContentType);
        WriteOptional(json, "part", medium.Source);
        WriteOptional(json, "target", medium.Target);
        WriteOptional(json, "prog_id", medium.ProgId);
        WriteOptional(json, "section", medium.Section);
        WriteOptional(json, "sheet", medium.Sheet + 1);
        WriteOptional(json, "slide", medium.Slide);
        json.WriteEndObject();
    }

    private static void WriteOptional(Utf8JsonWriter json, string name, string? value)
    {
        if (!string.IsNullOrEmpty(value))
        {
            json.WriteString(name, value);
        }
    }

    private static void WriteOptional(Utf8JsonWriter json, string name, int? value)
    {
        if (value is { } number)
        {
            json.WriteNumber(name, number);
        }
    }

    private void WriteLine()
    {
        buffer.Write("\n"u8);
        output.Write(buffer.WrittenSpan);
        buffer.ResetWrittenCount();
    }
}
