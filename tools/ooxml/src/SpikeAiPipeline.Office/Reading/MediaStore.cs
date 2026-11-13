using System.Security.Cryptography;
using DocumentFormat.OpenXml.Packaging;
using SpikeAiPipeline.Office.Model;

namespace SpikeAiPipeline.Office.Reading;

/// <summary>The embedded media a conversion found, by file name: the lowercase hex SHA-256 of
/// the bytes and the part's stored extension, so each distinct content is one file however many
/// parts or blocks hold it. The bytes are kept as stored, never converted.</summary>
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
        var name = Convert.ToHexStringLower(SHA256.HashData(bytes)) + Path.GetExtension(part.Uri.OriginalString);
        files.TryAdd(name, bytes);
        return parts[part.Uri] = (name, part.ContentType);
    }

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
}
