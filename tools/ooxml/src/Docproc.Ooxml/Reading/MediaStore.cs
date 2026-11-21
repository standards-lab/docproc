using System.Buffers;
using System.Security.Cryptography;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;

namespace Docproc.Ooxml.Reading;

internal sealed class MediaStore
{
    private readonly Dictionary<string, byte[]> files = new(StringComparer.Ordinal);
    private readonly Dictionary<Uri, (string Name, string ContentType)> parts = [];

    /// <summary>The files, by name.</summary>
    public IReadOnlyDictionary<string, byte[]> Files => files;

    /// <summary>Adds a part's bytes; returns its file name and content type.</summary>
    public (string Name, string ContentType) Add(OpenXmlPart part)
    {
        if (parts.TryGetValue(part.Uri, out var known))
        {
            return known;
        }
        byte[] bytes;
        using (var stream = part.GetStream(FileMode.Open, FileAccess.Read))
        using (var buffer = new MemoryStream())
        {
            stream.CopyTo(buffer);
            bytes = buffer.ToArray();
        }
        var name = NameOf(bytes, part.Uri.OriginalString);
        files.TryAdd(name, bytes);
        return parts[part.Uri] = (name, part.ContentType);
    }

    // See docs/tool-contract.md, "Common envelope".
    internal static string NameOf(ReadOnlySpan<byte> bytes, string storedName)
    {
        var hash = Convert.ToHexStringLower(SHA256.HashData(bytes));
        var extension = Path.GetExtension(storedName.AsSpan());
        return extension.Length is >= 2 and <= 9 && !extension[1..].ContainsAnyExcept(ExtensionCharacters)
            ? $"{hash}{extension}"
            : hash;
    }

    private static readonly SearchValues<char> ExtensionCharacters =
        SearchValues.Create("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz");

    /// <summary>The media block of a picture the given part relates to by id, its bytes added,
    /// with no location, which the caller gives: an embedded picture's file, or a linked
    /// picture's target, never fetched; null when the relationship is neither.</summary>
    public Media? Picture(OpenXmlPart part, string relationshipId, string? alt, string? title)
    {
        if (part.TryGetPartById(relationshipId, out var target))
        {
            var (name, contentType) = Add(target);
            return new Media
            {
                Name = name,
                ContentType = contentType,
                Alt = alt,
                Title = title,
                Source = target.Uri.OriginalString,
            };
        }
        if (part.ExternalRelationships.FirstOrDefault(r => r.Id == relationshipId) is { } link)
        {
            return new Media { Target = link.Uri.OriginalString, Alt = alt, Title = title };
        }
        return null;
    }

    /// <summary>The media block of an embedded package or OLE object the given part relates to
    /// by id, its bytes added as stored, with no location, which the caller gives; null when the
    /// relationship is neither, a linked object's among them, which has no part to write. Only the
    /// part's own embedded-package and embedded-object relationships are followed, so a chart's
    /// source workbook, its chart part's, is never one (docs/design-review.md, OX-5).</summary>
    public Media? Payload(OpenXmlPart part, string relationshipId, string? progId)
    {
        if (!part.TryGetPartById(relationshipId, out var target))
        {
            return null;
        }
        var kind = target switch
        {
            EmbeddedPackagePart => MediaKind.Embedded,
            EmbeddedObjectPart => MediaKind.Ole,
            _ => (MediaKind?)null,
        };
        if (kind is null)
        {
            return null;
        }
        var (name, contentType) = Add(target);
        return new Media
        {
            Kind = kind.Value,
            ProgId = string.IsNullOrWhiteSpace(progId) ? null : progId,
            Name = name,
            ContentType = contentType,
            Source = target.Uri.OriginalString,
        };
    }

    /// <summary>The target of an external hyperlink the given part relates to by id, or null when
    /// the id names none.</summary>
    public static string? Hyperlink(OpenXmlPart part, string? relationshipId) =>
        relationshipId is null
            ? null
            : part.HyperlinkRelationships.FirstOrDefault(r => r.Id == relationshipId && r.IsExternal)?.Uri.OriginalString;
}
