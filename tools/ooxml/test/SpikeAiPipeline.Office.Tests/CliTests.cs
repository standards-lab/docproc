using System.Diagnostics;
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;
using SpikeAiPipeline.Office.Model;
using SpikeAiPipeline.Office.Reading;
using SpikeAiPipeline.Office.Rendering;

namespace SpikeAiPipeline.Office.Tests;

// The CLI's process contract, on the NativeAOT binary the check publishes into .state/aot/ before
// it runs the tests: arguments, stdout, stderr, exit code, and the media directory.
public sealed class CliTests : IDisposable
{
    private static readonly string Binary = Path.Combine(Fixtures.Root, ".state", "aot", "spike-ai-pipeline-office");

    private readonly DirectoryInfo scratch = Directory.CreateTempSubdirectory("office-cli-tests-");

    public void Dispose() => scratch.Delete(recursive: true);

    [Fact]
    public void VersionIsTheAssemblysVersion()
    {
        var result = Run("--version");

        Assert.Equal(0, result.ExitCode);
        Assert.Equal(Cli.Version + "\n", result.Stdout);
        Assert.Empty(result.Stderr);
    }

    [Theory]
    [InlineData("json")]
    [InlineData(null)]
    public void JsonIsTheDefaultAndGoesToStdoutAlone(string? format)
    {
        var path = Fixtures.DocumentPath("tracked-changes", "libreoffice");
        var result = format is null ? Run(path) : Run(path, "--format", format);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("tracked-changes", "libreoffice", "json"), result.Stdout);
        Assert.Empty(result.Stderr);
    }

    [Fact]
    public void MarkdownGoesToStdoutAlone()
    {
        var result = Run(Fixtures.DocumentPath("tables", "libreoffice"), "--format", "markdown");

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("tables", "libreoffice", "md"), result.Stdout);
        Assert.Empty(result.Stderr);
    }

    // Each distinct picture once, named by its bytes, however many parts and blocks hold it: the
    // media fixture's python-docx variant stores the red square in two parts and shows it three
    // times, and links one picture, which is never written. The directory and its missing parent
    // are created.
    [Theory]
    [InlineData("json")]
    [InlineData("markdown")]
    public void MediaAreWrittenOnceEachNamedByTheirContent(string format)
    {
        var media = Path.Combine(scratch.FullName, "out", "media");
        var path = Fixtures.DocumentPath("media", "python-docx");

        var result = Run(path, "--format", format, "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("media", "python-docx", format == "json" ? "json" : "md"), result.Stdout);
        var files = Directory.GetFiles(media).Select(Path.GetFileName).Order(StringComparer.Ordinal).ToList();
        Assert.Equal(
            [
                "0fcca33bb0baf1a0669bd2fcdcaeb34e33d27196f7ffb5294b27aea8a9e46446.png",
                "10a86bd779a80e60e5d3314ec1edeab643cb454a4c7dfee671132090878d7aea.png",
                "670be6349ed84b27d6ca43d66ba6e9454c632f68de8d9b8fea78a480e90afb31.emf",
                "df026100fd6fe38e22923c5152a33ea6f6879e38073fb47f022ed9d90dec65a8.png",
            ],
            files);
        AssertEachStoredPartIsWritten(path, "word/media/", 5, media); // The red square's copy is a fifth part, written as the same file.
    }

    [Theory]
    [InlineData("json")]
    [InlineData("markdown")]
    public void AnXlsxConvertsToStdoutAlone(string format)
    {
        var result = Run(Fixtures.DocumentPath("formulas", "libreoffice"), "--format", format);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("formulas", "libreoffice", format == "json" ? "json" : "md"), result.Stdout);
        Assert.Empty(result.Stderr);
    }

    // The drawings fixture's openpyxl variant stores the red square twice, as two parts of one
    // sheet's drawing, and the blue square on a second sheet: two files.
    [Fact]
    public void SheetPicturesAreWrittenOnceEachNamedByTheirContent()
    {
        var media = Path.Combine(scratch.FullName, "media");
        var path = Fixtures.DocumentPath("drawings", "openpyxl");

        var result = Run(path, "--format", "markdown", "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("drawings", "openpyxl", "md"), result.Stdout);
        var files = Directory.GetFiles(media).Select(Path.GetFileName).Order(StringComparer.Ordinal).ToList();
        Assert.Equal(
            [
                "0fcca33bb0baf1a0669bd2fcdcaeb34e33d27196f7ffb5294b27aea8a9e46446.png",
                "10a86bd779a80e60e5d3314ec1edeab643cb454a4c7dfee671132090878d7aea.png",
            ],
            files);
        AssertEachStoredPartIsWritten(path, "xl/media/", 3, media);
    }

    // The slides fixture's python-pptx variant presents its slides in an order that isn't their
    // parts', with a hidden slide and notes.
    [Theory]
    [InlineData("json")]
    [InlineData("markdown")]
    public void APptxConvertsToStdoutAlone(string format)
    {
        var result = Run(Fixtures.DocumentPath("slides", "python-pptx"), "--format", format);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("slides", "python-pptx", format == "json" ? "json" : "md"), result.Stdout);
        Assert.Empty(result.Stderr);
    }

    // The slide-objects fixture shows the red square on two slides, one part, and the blue
    // square on one: two files, each the bytes of its part.
    [Theory]
    [InlineData("json")]
    [InlineData("markdown")]
    public void SlidePicturesAreWrittenOnceEachNamedByTheirContent(string format)
    {
        var media = Path.Combine(scratch.FullName, "media");
        var path = Fixtures.DocumentPath("slide-objects", "python-pptx");

        var result = Run(path, "--format", format, "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("slide-objects", "python-pptx", format == "json" ? "json" : "md"), result.Stdout);
        var files = Directory.GetFiles(media).Select(Path.GetFileName).Order(StringComparer.Ordinal).ToList();
        Assert.Equal(
            [
                "0fcca33bb0baf1a0669bd2fcdcaeb34e33d27196f7ffb5294b27aea8a9e46446.png",
                "10a86bd779a80e60e5d3314ec1edeab643cb454a4c7dfee671132090878d7aea.png",
            ],
            files);
        AssertEachStoredPartIsWritten(path, "ppt/media/", 2, media);
    }

    public static TheoryData<string, string> RealWorldFiles => Fixtures.RealWorldFiles;

    // Each real-world file, written by Word, Excel, or PowerPoint, converts on the published binary
    // to what the converter's seam gives, whose model the file's ground truth checks.
    [Theory]
    [MemberData(nameof(RealWorldFiles))]
    public void ARealWorldFileConvertsToStdoutAlone(string name, string variant)
    {
        var path = Fixtures.DocumentPath(name, variant);
        var document = OfficeReader.Read(path);

        foreach (var (format, expected) in new[] { ("json", DocumentJson.Serialize(document)), ("markdown", MarkdownRenderer.Render(document)) })
        {
            var result = Run(path, "--format", format);

            Assert.Equal(Cli.Converted, result.ExitCode);
            Assert.Equal(expected, result.Stdout);
            Assert.Empty(result.Stderr);
        }
    }

    [Fact]
    public void NothingIsWrittenWithoutTheMediaOption()
    {
        var before = Directory.GetFileSystemEntries(Path.GetDirectoryName(Fixtures.DocumentPath("media", "python-docx"))!);

        var result = RunIn(scratch.FullName, Fixtures.DocumentPath("media", "python-docx"));

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("media", "python-docx", "json"), result.Stdout);
        Assert.Empty(Directory.GetFileSystemEntries(scratch.FullName));
        Assert.Equal(before, Directory.GetFileSystemEntries(Path.GetDirectoryName(Fixtures.DocumentPath("media", "python-docx"))!));
    }

    // The media directory is created on every conversion it is given for, so it existing says the
    // conversion succeeded, even when the document has no media.
    [Fact]
    public void TheMediaDirectoryIsCreatedEvenWithoutMedia()
    {
        var media = Path.Combine(scratch.FullName, "media");

        var result = Run(Fixtures.DocumentPath("headings", "python-docx"), "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Fixtures.Expected("headings", "python-docx", "json"), result.Stdout);
        Assert.Empty(Directory.GetFileSystemEntries(media));
    }

    [Fact]
    public void MediaThatCannotBeWrittenLeaveStdoutEmptyAndExitOne()
    {
        var media = Write("media", [1, 2, 3]); // A file where the directory would be.

        var result = Run(Fixtures.DocumentPath("media", "python-docx"), "--media", media);

        Assert.Equal(Cli.MediaNotWritten, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.StartsWith("spike-ai-pipeline-office: media not written: ", result.Stderr, StringComparison.Ordinal);
    }

    [Theory]
    [InlineData]
    [InlineData("--format", "xml", "in.docx")]
    [InlineData("--bogus", "in.docx")]
    [InlineData("one.docx", "two.docx")]
    [InlineData("in.docx", "--media")]
    public void UsageErrorsPrintUsageToStderrAndExitTwo(params string[] args)
    {
        var result = Run(args);

        Assert.Equal(Cli.UsageError, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.Contains("Usage:", result.Stderr, StringComparison.Ordinal);
    }

    [Fact]
    public void AMissingFileIsRefused() =>
        AssertRefused(Path.Combine(scratch.FullName, "absent.docx"), "no such file");

    [Fact]
    public void AFileThatIsNoPackageIsRefused() =>
        AssertRefused(Write("notes.docx", Encoding.UTF8.GetBytes("plain text, not a package")), "not a docx, xlsx, or pptx package");

    [Fact]
    public void AZipThatIsNoOfficePackageIsRefused() =>
        AssertRefused(Zip("archive.docx", ("readme.txt", "hello")), "not a docx, xlsx, or pptx package");

    // An encrypted OOXML package is an OLE compound file, as are the legacy binary formats; the
    // signature alone identifies it.
    [Fact]
    public void AnEncryptedOrLegacyFileIsRefused() =>
        AssertRefused(
            Write("secret.docx", [0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0, 0, 0, 0]),
            "OLE compound file, which is an encrypted package or a legacy binary format");

    [Fact]
    public void ATruncatedPackageIsRefusedAsMalformed()
    {
        var bytes = File.ReadAllBytes(Fixtures.DocumentPath("headings", "python-docx"));
        AssertRefused(Write("truncated.docx", bytes[..(bytes.Length / 2)]), "a malformed package");
    }

    [Fact]
    public void APackageWithBrokenXmlIsRefusedAsMalformed() =>
        AssertRefused(
            Package("broken.docx", "word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "<w:document"),
            "a malformed package");

    // The sections fixture's third section reads its default header, which holds a picture,
    // before its first-page header, broken here: the picture was read, and still nothing is
    // written, as media are written only once the whole conversion has succeeded.
    [Fact]
    public void APackageMalformedAfterItsMediaIsRefusedAndWritesNoMedia()
    {
        var path = Path.Combine(scratch.FullName, "late.docx");
        File.Copy(Fixtures.DocumentPath("sections", "python-docx"), path);
        using (var zip = ZipFile.Open(path, ZipArchiveMode.Update))
        {
            zip.GetEntry("word/header5.xml")!.Delete();
            using var writer = new StreamWriter(zip.CreateEntry("word/header5.xml").Open());
            writer.Write("<w:hdr");
        }

        AssertRefused(path, "a malformed package");
    }

    [Fact]
    public void AnXlsxWithBrokenXmlIsRefusedAsMalformed() =>
        AssertRefused(
            Package("broken.xlsx", "xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml", "<workbook"),
            "a malformed package");

    // The drawings fixture's first sheet holds both red squares; its second sheet's part, read
    // after them, is broken here, and still no media are written.
    [Fact]
    public void AnXlsxMalformedAfterItsMediaIsRefusedAndWritesNoMedia()
    {
        var path = Path.Combine(scratch.FullName, "late.xlsx");
        File.Copy(Fixtures.DocumentPath("drawings", "openpyxl"), path);
        using (var zip = ZipFile.Open(path, ZipArchiveMode.Update))
        {
            zip.GetEntry("xl/worksheets/sheet2.xml")!.Delete();
            using var writer = new StreamWriter(zip.CreateEntry("xl/worksheets/sheet2.xml").Open());
            writer.Write("<worksheet");
        }

        AssertRefused(path, "a malformed package");
    }

    [Fact]
    public void APptxWithBrokenXmlIsRefusedAsMalformed() =>
        AssertRefused(
            Package("broken.pptx", "ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", "<p:presentation"),
            "a malformed package");

    // A single invalid reference is skipped with a warning and the rest converts, as in poi's test
    // file for its bug 60293: a header reference of type "odd", which no header kind is.
    [Fact]
    public void AHeaderReferenceOfAnInvalidTypeIsSkippedWithAWarning()
    {
        var path = Package(
            "odd.docx",
            "word/document.xml",
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
            Document(
                "<w:p><w:r><w:t>kept</w:t></w:r></w:p>" +
                "<w:sectPr><w:headerReference w:type=\"odd\" r:id=\"rId9\"/><w:footerReference w:type=\"default\" r:id=\"rId8\"/></w:sectPr>"));

        var result = Run(path, "--format", "markdown");

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal("kept\n", result.Stdout);
        Assert.Equal(
            $"spike-ai-pipeline-office: warning: {path}: a header reference of type \"odd\" in section 1 was skipped: no header has that type\n",
            result.Stderr);
    }

    // As in poi's test file for its bug 60825: a sheet whose relationship id is empty, so it
    // names no part, is skipped with a warning, and the sheet beside it converts.
    [Fact]
    public void ASheetThatNamesNoPartIsSkippedWithAWarning()
    {
        var path = Zip(
            "unnamed.xlsx",
            ("[Content_Types].xml",
                "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">" +
                "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>" +
                "<Override PartName=\"/xl/workbook.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml\"/>" +
                "<Override PartName=\"/xl/worksheets/sheet1.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml\"/></Types>"),
            ("_rels/.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"xl/workbook.xml\"/>" +
                "</Relationships>"),
            ("xl/workbook.xml",
                "<workbook xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\" " +
                "xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\"><sheets>" +
                "<sheet name=\"Module\" sheetId=\"1\" r:id=\"\"/><sheet name=\"Data\" sheetId=\"2\" r:id=\"rId1\"/></sheets></workbook>"),
            ("xl/_rels/workbook.xml.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet\" Target=\"worksheets/sheet1.xml\"/>" +
                "</Relationships>"),
            ("xl/worksheets/sheet1.xml",
                "<worksheet xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"><sheetData>" +
                "<row r=\"1\"><c r=\"A1\"><v>42</v></c></row></sheetData></worksheet>"));

        var result = Run(path, "--format", "markdown");

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal("# Data\n\n|  | A |\n| --- | --- |\n| 1 | 42 |\n", result.Stdout);
        Assert.Equal(
            $"spike-ai-pipeline-office: warning: {path}: sheet \"Module\" was skipped: its relationship id \"\" names no part\n",
            result.Stderr);
    }

    // Tables nested past eight deep are refused as they are reached, before any output, with
    // where: the ninth table's first paragraph, the body's second, after the one before them.
    [Fact]
    public void TablesNestedPastTheLimitAreRefused() =>
        AssertRefused(
            Package(
                "deep.docx",
                "word/document.xml",
                "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
                Document("<w:p><w:r><w:t>before</w:t></w:r></w:p>" + NestedTables(9, "") + "<w:sectPr/>")),
            "deep.docx: tables nested 9 deep (limit 8), at body paragraph 2");

    // Eight tables deep converts, and so does the deepest model that allows: a list nested Word's
    // nine levels in the innermost cell, past the serializer's default depth of 64.
    [Theory]
    [InlineData("json")]
    [InlineData("markdown")]
    public void TablesNestedToTheLimitConvertWithTheDeepestList(string format)
    {
        var levels = string.Concat(Enumerable.Range(0, 9).Select(l =>
            $"<w:lvl w:ilvl=\"{l}\"><w:numFmt w:val=\"bullet\"/><w:lvlText w:val=\"-\"/></w:lvl>"));
        var items = string.Concat(Enumerable.Range(0, 9).Select(l =>
            $"<w:p><w:pPr><w:numPr><w:ilvl w:val=\"{l}\"/><w:numId w:val=\"1\"/></w:numPr></w:pPr><w:r><w:t>item {l}</w:t></w:r></w:p>"));
        var path = Zip(
            "eight.docx",
            ("[Content_Types].xml",
                "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">" +
                "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>" +
                "<Override PartName=\"/word/document.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml\"/>" +
                "<Override PartName=\"/word/numbering.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml\"/></Types>"),
            ("_rels/.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"word/document.xml\"/>" +
                "</Relationships>"),
            ("word/document.xml", Document(NestedTables(8, items) + "<w:sectPr/>")),
            ("word/_rels/document.xml.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering\" Target=\"numbering.xml\"/>" +
                "</Relationships>"),
            ("word/numbering.xml",
                "<w:numbering xmlns:w=\"http://schemas.openxmlformats.org/wordprocessingml/2006/main\">" +
                $"<w:abstractNum w:abstractNumId=\"0\">{levels}</w:abstractNum><w:num w:numId=\"1\"><w:abstractNumId w:val=\"0\"/></w:num></w:numbering>"));

        var result = Run(path, "--format", format);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Empty(result.Stderr);
        Assert.Contains("item 8", result.Stdout, StringComparison.Ordinal);
    }

    // An exception office doesn't expect, here from a stdout that can't be written, is exit code
    // 4 and never 1, which says the media were not written.
    [Fact]
    public void AnUnexpectedErrorFailsWithItsOwnExitCode()
    {
        var stderr = new StringWriter();

        var code = Cli.Run([Fixtures.DocumentPath("headings", "python-docx")], new UnwritableWriter(), stderr);

        Assert.Equal(Cli.Failed, code);
        Assert.StartsWith(
            "spike-ai-pipeline-office: failed: " + Fixtures.DocumentPath("headings", "python-docx") + ": an unexpected System.InvalidOperationException: ",
            stderr.ToString(),
            StringComparison.Ordinal);
    }

    private sealed class UnwritableWriter : TextWriter
    {
        public override Encoding Encoding => Encoding.UTF8;

        public override void Write(char value) => throw new InvalidOperationException("stdout is closed");

        public override void Write(string? value) => throw new InvalidOperationException("stdout is closed");
    }

    // Tables nested the given number deep, each one cell, the innermost holding the given
    // paragraphs; each cell's own paragraph comes after the table it holds.
    private static string NestedTables(int depth, string innermost) =>
        depth == 0 ? innermost : $"<w:tbl><w:tr><w:tc>{NestedTables(depth - 1, innermost)}<w:p><w:r><w:t>depth {depth}</w:t></w:r></w:p></w:tc></w:tr></w:tbl>";

    private static string Document(string body) =>
        "<w:document xmlns:w=\"http://schemas.openxmlformats.org/wordprocessingml/2006/main\" " +
        $"xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships\"><w:body>{body}</w:body></w:document>";

    // A relationship to a part the package lacks, as poi's picture.xlsx has (its connections
    // part), fails the SDK when it first loads the package's parts.
    [Fact]
    public void ARelationshipToAMissingPartIsRefusedAsMalformed() =>
        AssertRefused(
            Zip(
                "missing.xlsx",
                ("[Content_Types].xml",
                    "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">" +
                    "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>" +
                    "<Override PartName=\"/xl/workbook.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml\"/></Types>"),
                ("_rels/.rels",
                    "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                    "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"xl/workbook.xml\"/>" +
                    "</Relationships>"),
                ("xl/workbook.xml", "<workbook xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"><sheets/></workbook>"),
                ("xl/_rels/workbook.xml.rels",
                    "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                    "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/connections\" Target=\"connections.xml\"/>" +
                    "</Relationships>")),
            "doesn't exist in the package");

    // The package stores the given number of parts under the media prefix, and each one's bytes
    // are in the media directory under its content's name.
    private static void AssertEachStoredPartIsWritten(string path, string prefix, int parts, string media)
    {
        using var package = ZipFile.OpenRead(path);
        var stored = package.Entries.Where(e => e.FullName.StartsWith(prefix, StringComparison.Ordinal)).ToList();
        Assert.Equal(parts, stored.Count);
        foreach (var entry in stored)
        {
            using var stream = entry.Open();
            using var bytes = new MemoryStream();
            stream.CopyTo(bytes);
            var name = Convert.ToHexStringLower(SHA256.HashData(bytes.ToArray())) + Path.GetExtension(entry.Name);
            Assert.Equal(bytes.ToArray(), File.ReadAllBytes(Path.Combine(media, name)));
        }
    }

    private void AssertRefused(string path, string reason)
    {
        var media = Path.Combine(scratch.FullName, "media");
        var result = Run(path, "--format", "markdown", "--media", media);

        Assert.Equal(Cli.Refused, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.False(Directory.Exists(media));
        Assert.StartsWith("spike-ai-pipeline-office: refused: ", result.Stderr, StringComparison.Ordinal);
        Assert.Contains(reason, result.Stderr, StringComparison.Ordinal);
    }

    private string Write(string name, byte[] bytes)
    {
        var path = Path.Combine(scratch.FullName, name);
        File.WriteAllBytes(path, bytes);
        return path;
    }

    private string Zip(string name, params (string Entry, string Content)[] entries)
    {
        var path = Path.Combine(scratch.FullName, name);
        using var zip = ZipFile.Open(path, ZipArchiveMode.Create);
        foreach (var (entry, content) in entries)
        {
            using var writer = new StreamWriter(zip.CreateEntry(entry).Open());
            writer.Write(content);
        }
        return path;
    }

    // A package of one main part, with the content types and relationship that name it.
    private string Package(string name, string part, string contentType, string content) =>
        Zip(
            name,
            ("[Content_Types].xml",
                "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">" +
                "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>" +
                $"<Override PartName=\"/{part}\" ContentType=\"{contentType}\"/></Types>"),
            ("_rels/.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                $"<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"{part}\"/>" +
                "</Relationships>"),
            (part, content));

    private sealed record Result(int ExitCode, string Stdout, string Stderr);

    private static Result Run(params string[] args) => RunIn(Environment.CurrentDirectory, args);

    private static Result RunIn(string directory, params string[] args)
    {
        Assert.True(File.Exists(Binary), $"{Binary} is missing: `mise run check` publishes it before the tests run");
        var start = new ProcessStartInfo(Binary)
        {
            WorkingDirectory = directory,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            StandardOutputEncoding = Encoding.UTF8,
            StandardErrorEncoding = Encoding.UTF8,
        };
        foreach (var arg in args)
        {
            start.ArgumentList.Add(arg);
        }
        using var process = Process.Start(start)!;
        var stderr = process.StandardError.ReadToEndAsync(TestContext.Current.CancellationToken);
        var stdout = process.StandardOutput.ReadToEnd();
        process.WaitForExit();
        return new Result(process.ExitCode, stdout, stderr.Result);
    }
}
