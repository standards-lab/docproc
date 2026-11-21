using System.IO.Compression;
using System.Xml;
using System.Xml.Linq;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Reading;

internal sealed class RefusalException(string reason) : Exception(reason);

internal sealed class ContentRefusedException(string reason) : Exception(reason);

internal sealed record Conversion(Document Document, IReadOnlyDictionary<string, byte[]> Media)
{
    /// <summary>What the conversion skipped and converted the rest without: a single invalid
    /// reference, such as a header reference of a type no header has, or a sheet that names no
    /// part, each where it is and what was skipped.</summary>
    public IReadOnlyList<Warning> Warnings { get; init; } = [];
}

internal sealed class PendingConversion(IReadOnlyDictionary<string, byte[]> media, IReadOnlyList<Warning> warnings, Action<Stream> write, IDisposable? package) : IDisposable
{
    /// <summary>The embedded media the lines name, by file name.</summary>
    public IReadOnlyDictionary<string, byte[]> Media => media;

    /// <summary>What the conversion skipped and converted the rest without.</summary>
    public IReadOnlyList<Warning> Warnings => warnings;

    /// <summary>Writes the document's lines to the output, each as it is made.</summary>
    public void WriteLines(Stream output) => write(output);

    public void Dispose() => package?.Dispose();
}

// See docs/tool-contract.md, "docproc-ooxml".
internal static class OfficeReader
{
    private static readonly byte[] ZipSignature = [0x50, 0x4B, 0x03, 0x04];

    internal const string NoSuchFile = "no such file";

    internal const string IsADirectory = "is a directory";

    internal const string PermissionDenied = "permission denied";

    internal const string NotAFile = "not a file";

    internal const string NotAPackage = "not a docx, xlsx, or pptx package";

    internal const string Encrypted = "encrypted package (an OLE compound file holding an EncryptedPackage stream); decrypt it first";

    internal const string Legacy = "an OLE compound file, such as a legacy binary document (doc, xls, ppt); convert it to docx, xlsx, or pptx first";

    /// <summary>Converts the document at path into the model.</summary>
    /// <exception cref="RefusalException">The path is missing, a directory, or not a regular file,
    /// or can't be read, or the
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
            RefuseUnlessAFile(path);
            return ConvertPackage(path, Sniff(path));
        }
        catch (UnauthorizedAccessException)
        {
            throw new RefusalException(Named(PermissionDenied, path));
        }
        catch (IOException e)
        {
            throw new RefusalException($"cannot be read: {WithoutPath(e, path)}");
        }
    }

    /// <summary>Opens the document at path for the CLI to write: every refusal is found here, before
    /// anything is written. An xlsx workbook's sheets are read once to validate them and are read
    /// again only as their lines are written; a docx or pptx is converted whole.</summary>
    /// <exception cref="RefusalException">As <see cref="Read"/>.</exception>
    public static PendingConversion Open(string path)
    {
        try
        {
            RefuseUnlessAFile(path);
            var format = Sniff(path);
            if (format != SourceFormat.Xlsx)
            {
                var conversion = ConvertPackage(path, format);
                return new PendingConversion(conversion.Media, conversion.Warnings, output => Lines.Write(conversion.Document, output), null);
            }
            var package = Guarded(path, () => SpreadsheetDocument.Open(path, false));
            try
            {
                var workbook = Guarded(path, () => XlsxReader.Validate(Loaded(package)));
                return new PendingConversion(workbook.Media, workbook.Warnings, workbook.Write, package);
            }
            catch
            {
                package.Dispose();
                throw;
            }
        }
        catch (UnauthorizedAccessException)
        {
            throw new RefusalException(Named(PermissionDenied, path));
        }
        catch (IOException e)
        {
            throw new RefusalException($"cannot be read: {WithoutPath(e, path)}");
        }
    }

    private static void RefuseUnlessAFile(string path)
    {
        switch (Native.KindOf(path))
        {
            case Native.PathKind.RegularFile:
                return;
            case Native.PathKind.Missing:
                throw new RefusalException(Named(NoSuchFile, path));
            case Native.PathKind.Directory:
                throw new RefusalException(Named(IsADirectory, path));
            default:
                throw new RefusalException(NotAFile);
        }
    }

    private static string Named(string reason, string path) => $"{reason}: {path}";

    private static string WithoutPath(Exception e, string path) =>
        e.Message.Replace(Path.GetFullPath(path), "the input", StringComparison.Ordinal).Replace(path, "the input", StringComparison.Ordinal);

    private static Conversion ConvertPackage(string path, SourceFormat format) =>
        Guarded(path, () =>
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
        });

    private static T Guarded<T>(string path, Func<T> read)
    {
        try
        {
            return read();
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

    private static T Loaded<T>(T package)
        where T : OpenXmlPackage
    {
        try
        {
            _ = package.GetAllParts().Count();
        }
        catch (InvalidOperationException e)
        {
            throw new InvalidDataException(e.Message.Trim().Split('\n')[0].Trim(), e);
        }
        return package;
    }

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

    private static bool IsMalformed(Exception e) =>
        e is InvalidDataException or XmlException or OpenXmlPackageException or FileFormatException or FormatException;

    private static RefusalException Malformed(string path, Exception e) =>
        new($"a malformed package: {WithoutPath(e, path)}");
}
