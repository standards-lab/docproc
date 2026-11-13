using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace SpikeAiPipeline.Office.Model;

/// <summary>The model's JSON, written through source-generated serializers, so it needs no
/// reflection under NativeAOT.</summary>
public static class DocumentJson
{
    // Non-ASCII text is written as itself rather than as \u escapes: the JSON is a data file for
    // the pipeline, never embedded in HTML, so the relaxed encoder's one risk doesn't apply.
    // The model is deeper than the serializer's default limit of 64 allows: a docx reply's table
    // nested eight deep (the readers refuse more), six levels each, holding a list nested nine
    // deep (Word's levels, four each), is 93 deep, so the limit is set above that.
    private static readonly DocumentJsonContext Context = new(
        new JsonSerializerOptions(DocumentJsonContext.Default.Options)
        {
            Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
            MaxDepth = MaxDepth,
        });

    /// <summary>The deepest JSON the model can make, with room to spare.</summary>
    internal const int MaxDepth = 128;

    /// <summary>The document as JSON: camelCase, indented two spaces, null properties left out,
    /// and ending in a newline. The same model always gives the same bytes.</summary>
    public static string Serialize(Document document) =>
        JsonSerializer.Serialize(document, Context.Document) + "\n";
}

[JsonSourceGenerationOptions(
    PropertyNamingPolicy = JsonKnownNamingPolicy.CamelCase,
    DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
    WriteIndented = true,
    IndentSize = 2,
    NewLine = "\n")]
[JsonSerializable(typeof(Document))]
internal sealed partial class DocumentJsonContext : JsonSerializerContext;

/// <summary>A cell value as its JSON primitive. Office only writes the model, so reading is not
/// supported.</summary>
internal sealed class CellValueJsonConverter : JsonConverter<CellValue>
{
    public override CellValue Read(ref Utf8JsonReader reader, Type typeToConvert, JsonSerializerOptions options) =>
        throw new NotSupportedException("office writes the model and never reads it");

    public override void Write(Utf8JsonWriter writer, CellValue value, JsonSerializerOptions options)
    {
        switch (value.Type)
        {
            case CellValueType.Number:
                writer.WriteNumberValue(value.Number);
                break;
            case CellValueType.Boolean:
                writer.WriteBooleanValue(value.Boolean);
                break;
            default:
                writer.WriteStringValue(value.Text);
                break;
        }
    }
}
