using System.IO.Compression;
using System.Xml;
using System.Xml.Linq;
using DocumentFormat.OpenXml.Packaging;
using SpikeAiPipeline.Office.Model;

namespace SpikeAiPipeline.Office.Reading;

/// <summary>A document office won't convert, and why. It is raised before any output is
/// written.</summary>
public sealed class RefusalException(string message) : Exception(message);

/// <summary>A document office reads but won't convert, as its readers find it: what in it goes
/// past a limit, and where. The converter refuses the document with this reason.</summary>
internal sealed class ContentRefusedException(string reason) : Exception(reason);

/// <summary>A conversion's result: the model, and the embedded media its media blocks name, by
/// file name.</summary>
public sealed record Conversion(Document Document, IReadOnlyDictionary<string, byte[]> Media)
{
    /// <summary>What the conversion skipped and converted the rest without: a single invalid
    /// reference, such as a header reference of a type no header has, or a sheet that names no
    /// part, each one line, its path left to the caller.</summary>
    public IReadOnlyList<string> Warnings { get; init; } = [];
}

/// <summary>Office's converter: a document in, the model and its media out.</summary>
public static class OfficeReader
{
    private static readonly byte[] CompoundFileSignature = [0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1];
    private static readonly byte[] ZipSignature = [0x50, 0x4B, 0x03, 0x04];

    /// <summary>Converts the document at path into the model.</summary>
    /// <exception cref="RefusalException">The file is missing or unreadable, isn't a docx, xlsx,
    /// or pptx package, is encrypted or a legacy binary format, is malformed, or goes past a limit
    /// of office's (tables nested more than eight deep).</exception>
    public static Document Read(string path) => Convert(path).Document;

    /// <summary>Converts the document at path into the model and the media it embeds, read whole
    /// before anything is written.</summary>
    /// <exception cref="RefusalException">As <see cref="Read"/>.</exception>
    public static Conversion Convert(string path)
    {
        if (!File.Exists(path))
        {
            throw new RefusalException($"{path}: no such file");
        }
        try
        {
            return ConvertPackage(path, Sniff(path));
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            // A ZIP or XML failure inside the package is malformed; this is the file itself.
            throw new RefusalException($"{path}: cannot be read: {e.Message}");
        }
    }

    private static Conversion ConvertPackage(string path, SourceFormat format)
    {
        try
        {
            if (format == SourceFormat.Xlsx)
            {
                using var workbook = SpreadsheetDocument.Open(path, false);
                return XlsxReader.Read(Loaded(workbook));
            }
            if (format == SourceFormat.Pptx)
            {
                using var presentation = PresentationDocument.Open(path, false);
                return PptxReader.Read(Loaded(presentation));
            }
            using var package = WordprocessingDocument.Open(path, false);
            return DocxReader.Read(Loaded(package));
        }
        catch (Exception e) when (IsMalformed(e))
        {
            throw Malformed(path, e);
        }
        catch (ContentRefusedException e)
        {
            throw new RefusalException($"{path}: {e.Message}");
        }
    }

    // The SDK loads a package's parts the first time any is asked for, and throws an
    // InvalidOperationException then for a relationship to a part the package lacks. They are
    // asked for here, once, so that failure is the package's and nothing else's.
    private static T Loaded<T>(T package)
        where T : OpenXmlPackage
    {
        try
        {
            _ = package.GetAllParts().Count();
        }
        catch (InvalidOperationException e)
        {
            // Its message goes on to repeat itself on further lines.
            throw new InvalidDataException(e.Message.Trim().Split('\n')[0].Trim(), e);
        }
        return package;
    }

    // The format, from the content: an OOXML package is a ZIP whose officeDocument relationship
    // names the main part, and that part's content type names the format. An encrypted OOXML
    // package is an OLE compound file holding the encrypted ZIP, as are doc, xls, and ppt.
    private static SourceFormat Sniff(string path)
    {
        var head = new byte[CompoundFileSignature.Length];
        int read;
        using (var stream = File.OpenRead(path))
        {
            read = stream.ReadAtLeast(head, head.Length, throwOnEndOfStream: false);
        }
        if (head.AsSpan(0, read).SequenceEqual(CompoundFileSignature))
        {
            throw new RefusalException(
                $"{path}: an OLE compound file, which is an encrypted package or a legacy binary format (doc, xls, ppt); " +
                "decrypt it or convert it to docx, xlsx, or pptx first");
        }
        if (!head.AsSpan(0, read).StartsWith(ZipSignature))
        {
            throw Unsupported(path);
        }
        try
        {
            using var zip = ZipFile.OpenRead(path);
            return FormatOf(zip) ?? throw Unsupported(path);
        }
        catch (Exception e) when (IsMalformed(e))
        {
            throw Malformed(path, e);
        }
    }

    private const string RelationshipsNamespace = "http://schemas.openxmlformats.org/package/2006/relationships";
    private const string ContentTypesNamespace = "http://schemas.openxmlformats.org/package/2006/content-types";

    private static SourceFormat? FormatOf(ZipArchive zip)
    {
        var relationships = Load(zip, "_rels/.rels");
        var contentTypes = Load(zip, "[Content_Types].xml");
        if (relationships is null || contentTypes is null)
        {
            return null;
        }
        // Transitional and Strict name the relationship in different namespaces, both ending so.
        var target = relationships.Root?.Elements(XName.Get("Relationship", RelationshipsNamespace))
            .FirstOrDefault(r => ((string?)r.Attribute("Type"))?.EndsWith("/officeDocument", StringComparison.Ordinal) == true)
            ?.Attribute("Target")?.Value;
        if (target is null)
        {
            return null;
        }
        var part = "/" + target.TrimStart('/');
        var types = contentTypes.Root;
        var contentType =
            types?.Elements(XName.Get("Override", ContentTypesNamespace))
                .FirstOrDefault(o => string.Equals((string?)o.Attribute("PartName"), part, StringComparison.OrdinalIgnoreCase))
                ?.Attribute("ContentType")?.Value
            ?? types?.Elements(XName.Get("Default", ContentTypesNamespace))
                .FirstOrDefault(d => string.Equals((string?)d.Attribute("Extension"), Path.GetExtension(part).TrimStart('.'), StringComparison.OrdinalIgnoreCase))
                ?.Attribute("ContentType")?.Value;
        return contentType switch
        {
            null => null,
            _ when contentType.Contains("wordprocessingml", StringComparison.Ordinal) || contentType.Contains("ms-word", StringComparison.Ordinal) => SourceFormat.Docx,
            _ when contentType.Contains("spreadsheetml", StringComparison.Ordinal) || contentType.Contains("ms-excel", StringComparison.Ordinal) => SourceFormat.Xlsx,
            _ when contentType.Contains("presentationml", StringComparison.Ordinal) || contentType.Contains("ms-powerpoint", StringComparison.Ordinal) => SourceFormat.Pptx,
            _ => null,
        };
    }

    private static XDocument? Load(ZipArchive zip, string name)
    {
        var entry = zip.GetEntry(name);
        if (entry is null)
        {
            return null;
        }
        using var stream = entry.Open();
        return XDocument.Load(stream);
    }

    // A FormatException is the SDK's for an attribute that isn't one of its type's values, where
    // a reader doesn't skip the one element that holds it.
    private static bool IsMalformed(Exception e) =>
        e is InvalidDataException or XmlException or OpenXmlPackageException or FileFormatException or FormatException;

    private static RefusalException Malformed(string path, Exception e) =>
        new($"{path}: a malformed package: {e.Message}");

    private static RefusalException Unsupported(string path) =>
        new($"{path}: not a docx, xlsx, or pptx package");
}
