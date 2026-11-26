using System.Security.Cryptography;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Serialization;
using MimeKit;
using MimeKit.Tnef;

namespace Docproc.Mail.Generator;

/// <summary>
/// The TNEF group, tests/fixtures/tnef: each TNEF stream layout.json names, from Apache POI's
/// HMEF test data under poi/, wrapped as the TNEF part of an .eml under eml/, and truth.json, what
/// MimeKit's TnefPart reads of each stream. The .eml is written byte-stable: its date, its
/// Message-Id and its boundary are fixed, its line breaks CRLF, and the stream base64-encoded as
/// an attachment named winmail.dat, under the content type layout.json gives, beside a text/plain
/// part when it gives one. The truth holds ConvertToMessage's HTML, text and RTF bodies, the RTF
/// decompressed, and its attachments, less those bodies, each with its filename, media type, Content-Id, size and
/// SHA-256, and what TnefReader finds wrong with the stream; a stream TnefPart can't read holds
/// its exception's message.
/// </summary>
public static class Tnef
{
    const string Note =
        "Written by the generator (MISE_ENV=fixtures mise run tnef); edit layout.json, never this "
        + "file or the .eml under eml/. Each entry is what MimeKit's TnefPart reads of its TNEF "
        + "stream: ConvertToMessage's HTML, text and RTF bodies, the RTF decompressed, and its "
        + "attachments, with compliance, TnefReader's status for the stream read through in its "
        + "loose mode, or, for a stream TnefPart can't read, its exception's message.";

    static readonly JsonSerializerOptions Options = new()
    {
        WriteIndented = true,
        Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
    };

    public static int Run(string dir)
    {
        var layout = JsonSerializer.Deserialize<TnefLayout>(File.ReadAllBytes(Path.Combine(dir, "layout.json")))
            ?? throw new InvalidDataException("layout.json: no layout");
        Directory.CreateDirectory(Path.Combine(dir, "eml"));
        var messages = new JsonObject();
        foreach (var entry in layout.Messages)
        {
            var stream = File.ReadAllBytes(Path.Combine(dir, entry.Source));
            var eml = Path.Combine(dir, "eml", entry.Eml);
            using (var output = File.Create(eml))
            {
                Wrap(entry, stream).WriteTo(new FormatOptions { NewLineFormat = NewLineFormat.Dos }, output);
            }
            var truth = Read(stream);
            truth["source"] = entry.Source;
            if (entry.Text is not null)
            {
                truth["mime_text"] = entry.Text;
            }
            messages[entry.Eml] = truth;
        }
        var all = new JsonObject
        {
            ["note"] = Note,
            ["tools"] = new JsonObject { ["MimeKit"] = Fixtures.Tools()["MimeKit"] },
            ["messages"] = messages,
        };
        File.WriteAllText(Path.Combine(dir, "truth.json"), all.ToJsonString(Options) + "\n");
        return 0;
    }

    static MimeMessage Wrap(TnefEntry entry, byte[] stream)
    {
        var name = Path.GetFileNameWithoutExtension(entry.Eml);
        var message = new MimeMessage();
        message.From.Add(new MailboxAddress("Fixture Sender", "sender@example.org"));
        message.To.Add(new MailboxAddress("Fixture Recipient", "recipient@example.org"));
        message.Subject = entry.Subject;
        message.Date = new DateTimeOffset(2026, 1, 1, 0, 0, 0, TimeSpan.Zero);
        message.MessageId = $"{name}@example.org";
        var tnef = new MimePart(ContentType.Parse(entry.ContentType))
        {
            Content = new MimeContent(new MemoryStream(stream)),
            ContentTransferEncoding = ContentEncoding.Base64,
            FileName = "winmail.dat",
        };
        var mixed = new Multipart("mixed") { Boundary = $"=_{name}" };
        if (entry.Text is not null)
        {
            mixed.Add(new TextPart("plain") { Text = entry.Text.ReplaceLineEndings("\r\n") });
        }
        mixed.Add(tnef);
        message.Body = mixed;
        return message;
    }

    static JsonObject Read(byte[] stream)
    {
        var part = new TnefPart { Content = new MimeContent(new MemoryStream(stream)) };
        try
        {
            var converted = part.ConvertToMessage();
            var truth = new JsonObject();
            if (converted.HtmlBody is { } html)
            {
                truth["html"] = html;
            }
            if (converted.TextBody is { } text)
            {
                truth["text"] = text;
            }
            var rtf = converted.BodyParts.OfType<TextPart>().FirstOrDefault(p => p.IsRichText);
            if (rtf is not null)
            {
                truth["rtf"] = rtf.Text;
            }
            var attachments = new JsonArray();
            var bodies = converted.BodyParts.OfType<TextPart>().Where(p => p.FileName is null).ToList();
            foreach (var entity in converted.Attachments.Where(e => !bodies.Contains(e)))
            {
                attachments.Add(Attachment(entity));
            }
            truth["attachments"] = attachments;
            truth["compliance"] = Compliance(stream);
            return truth;
        }
        catch (Exception e) when (e is TnefException or InvalidDataException or ArgumentException or IOException)
        {
            return new JsonObject { ["error"] = e.Message };
        }
    }

    static string Compliance(byte[] stream)
    {
        using var reader = new TnefReader(new MemoryStream(stream), 0, TnefComplianceMode.Loose);
        try
        {
            while (reader.ReadNextAttribute())
            {
                var properties = reader.TnefPropertyReader;
                if (reader.AttributeTag is TnefAttributeTag.MapiProperties or TnefAttributeTag.Attachment)
                {
                    while (properties.ReadNextProperty())
                    {
                        properties.ReadValue();
                    }
                }
                else if (reader.AttributeTag is TnefAttributeTag.RecipientTable)
                {
                    while (properties.ReadNextRow())
                    {
                        while (properties.ReadNextProperty())
                        {
                            properties.ReadValue();
                        }
                    }
                }
            }
        }
        catch (TnefException e)
        {
            return $"{reader.ComplianceStatus}: {e.Message}";
        }
        return reader.ComplianceStatus.ToString();
    }

    static JsonObject Attachment(MimeEntity entity)
    {
        var record = new JsonObject { ["mime_type"] = entity.ContentType.MimeType };
        if (entity is MimePart { Content: { } stored } part)
        {
            using var content = new MemoryStream();
            stored.DecodeTo(content);
            var bytes = content.ToArray();
            record["filename"] = part.FileName;
            record["size"] = bytes.Length;
            record["sha256"] = Convert.ToHexStringLower(SHA256.HashData(bytes));
        }
        if (entity.ContentId is { } id)
        {
            record["content_id"] = id;
        }
        return record;
    }
}

/// <summary>layout.json: the TNEF group's messages, in order.</summary>
public sealed record TnefLayout(
    [property: JsonPropertyName("messages")] IReadOnlyList<TnefEntry> Messages);

/// <summary>
/// One message: its .eml's name under eml/, its TNEF stream's path under the group, its subject,
/// the content type its TNEF part is written under, and the text of the text/plain part beside it,
/// when it has one.
/// </summary>
public sealed record TnefEntry(
    [property: JsonPropertyName("eml")] string Eml,
    [property: JsonPropertyName("source")] string Source,
    [property: JsonPropertyName("subject")] string Subject,
    [property: JsonPropertyName("content_type")] string ContentType)
{
    [JsonPropertyName("text")]
    public string? Text { get; init; }
}
