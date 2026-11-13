using System.IO.Compression;
using System.Xml;
using System.Xml.Linq;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Reading;

/// <summary>A document docproc-ooxml won't convert, and why. It is raised before any output is
/// written. Its message is the reason, which never holds the input's path, and begins
/// <c>encrypted</c> only for an encrypted input (context/layers.md, "Refusal reasons").</summary>
internal sealed class RefusalException(string reason) : Exception(reason);

/// <summary>A document docproc-ooxml reads but won't convert, as its readers find it: what in it goes
/// past a limit, and where. The converter refuses the document with this reason.</summary>
internal sealed class ContentRefusedException(string reason) : Exception(reason);

/// <summary>A conversion's result: the model, and the embedded media its media blocks name, by
/// file name.</summary>
internal sealed record Conversion(Document Document, IReadOnlyDictionary<string, byte[]> Media)
{
    /// <summary>What the conversion skipped and converted the rest without: a single invalid
    /// reference, such as a header reference of a type no header has, or a sheet that names no
    /// part, each where it is and what was skipped.</summary>
    public IReadOnlyList<Warning> Warnings { get; init; } = [];
}

/// <summary>Office's converter: a document in, the model and its media out.</summary>
internal static class OfficeReader
{
    private static readonly byte[] ZipSignature = [0x50, 0x4B, 0x03, 0x04];

    /// <summary>The refusal's reason for a path that isn't a regular file, absent included.</summary>
    internal const string NotAFile = "not a file";

    /// <summary>The refusal's reason for a file that isn't a docx, xlsx, or pptx package.</summary>
    internal const string NotAPackage = "not a docx, xlsx, or pptx package";

    /// <summary>The refusal's reason for an encrypted OOXML package: an OLE compound file whose
    /// root holds an <c>EncryptedPackage</c> stream. It begins <c>encrypted</c>, which docproc
    /// reads as the input being encrypted.</summary>
    internal const string Encrypted = "encrypted package (an OLE compound file holding an EncryptedPackage stream); decrypt it first";

    /// <summary>The refusal's reason for any other OLE compound file, such as a legacy binary
    /// document; it never holds the word the encrypted reason begins with.</summary>
    internal const string Legacy = "an OLE compound file, such as a legacy binary document (doc, xls, ppt); convert it to docx, xlsx, or pptx first";

    /// <summary>Converts the document at path into the model.</summary>
    /// <exception cref="RefusalException">The path isn't a regular file or can't be read, or the
    /// file isn't a docx, xlsx, or pptx package, is encrypted or a legacy binary format, is
    /// malformed, or goes past a limit of docproc-ooxml's (tables nested more than eight
    /// deep).</exception>
    public static Document Read(string path) => Convert(path).Document;

    /// <summary>Converts the document at path into the model and the media it embeds, read whole
    /// before anything is written.</summary>
    /// <exception cref="RefusalException">As <see cref="Read"/>.</exception>
    public static Conversion Convert(string path)
    {
        try
        {
            if (Native.KindOf(path) != Native.PathKind.RegularFile)
            {
                throw new RefusalException(NotAFile);
            }
            return ConvertPackage(path, Sniff(path));
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            // A ZIP or XML failure inside the package is malformed; this is the file itself.
            throw new RefusalException($"cannot be read: {WithoutPath(e, path)}");
        }
    }

    // .NET's messages for a file name it, as given or in full; a reason never holds the path.
    private static string WithoutPath(Exception e, string path) =>
        e is UnauthorizedAccessException
            ? "permission denied"
            : e.Message.Replace(Path.GetFullPath(path), "the input", StringComparison.Ordinal).Replace(path, "the input", StringComparison.Ordinal);

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
            throw new RefusalException(e.Message);
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
    // names the main part, and that part's exact content type names the format. An encrypted
    // OOXML package is an OLE compound file holding the encrypted ZIP in an EncryptedPackage
    // stream ([MS-OFFCRYPTO] 2.3.4.4), which the compound file's own directory tells from doc,
    // xls, and ppt, compound files too.
    private static SourceFormat Sniff(string path)
    {
        using (var stream = File.OpenRead(path))
        {
            var head = new byte[CompoundFile.Signature.Length];
            var read = stream.ReadAtLeast(head, head.Length, throwOnEndOfStream: false);
            if (head.AsSpan(0, read).SequenceEqual(CompoundFile.Signature))
            {
                IReadOnlyList<string> children;
                try
                {
                    children = CompoundFile.RootChildren(stream);
                }
                catch (MalformedCompoundFileException e)
                {
                    throw new RefusalException($"a malformed OLE compound file: {e.Message}");
                }
                // [MS-CFB] compares names without regard to case.
                throw new RefusalException(
                    children.Contains("EncryptedPackage", StringComparer.OrdinalIgnoreCase) ? Encrypted : Legacy);
            }
            if (!head.AsSpan(0, read).StartsWith(ZipSignature))
            {
                throw new RefusalException(NotAPackage);
            }
        }
        try
        {
            using var zip = ZipFile.OpenRead(path);
            return FormatOf(zip) ?? throw new RefusalException(NotAPackage);
        }
        catch (Exception e) when (IsMalformed(e))
        {
            throw Malformed(path, e);
        }
    }

    private const string RelationshipsNamespace = "http://schemas.openxmlformats.org/package/2006/relationships";
    private const string ContentTypesNamespace = "http://schemas.openxmlformats.org/package/2006/content-types";

    // The main parts' exact content types the Open XML SDK opens, each format's document,
    // template, macro-enabled, slide show, and add-in kinds; any other, such as an xlsb's binary
    // workbook (application/vnd.ms-excel.sheet.binary.macroEnabled.main), is no package of the
    // three.
    private static readonly Dictionary<string, SourceFormat> MainContentTypes = new(StringComparer.Ordinal)
    {
        ["application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"] = SourceFormat.Docx,
        ["application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml"] = SourceFormat.Docx,
        ["application/vnd.ms-word.document.macroEnabled.main+xml"] = SourceFormat.Docx,
        ["application/vnd.ms-word.template.macroEnabledTemplate.main+xml"] = SourceFormat.Docx,
        ["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"] = SourceFormat.Xlsx,
        ["application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml"] = SourceFormat.Xlsx,
        ["application/vnd.ms-excel.sheet.macroEnabled.main+xml"] = SourceFormat.Xlsx,
        ["application/vnd.ms-excel.template.macroEnabled.main+xml"] = SourceFormat.Xlsx,
        ["application/vnd.ms-excel.addin.macroEnabled.main+xml"] = SourceFormat.Xlsx,
        ["application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"] = SourceFormat.Pptx,
        ["application/vnd.openxmlformats-officedocument.presentationml.template.main+xml"] = SourceFormat.Pptx,
        ["application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml"] = SourceFormat.Pptx,
        ["application/vnd.ms-powerpoint.presentation.macroEnabled.main+xml"] = SourceFormat.Pptx,
        ["application/vnd.ms-powerpoint.template.macroEnabled.main+xml"] = SourceFormat.Pptx,
        ["application/vnd.ms-powerpoint.slideshow.macroEnabled.main+xml"] = SourceFormat.Pptx,
        ["application/vnd.ms-powerpoint.addin.macroEnabled.main+xml"] = SourceFormat.Pptx,
    };

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
        return contentType is not null && MainContentTypes.TryGetValue(contentType, out var format) ? format : null;
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
        new($"a malformed package: {WithoutPath(e, path)}");
}
