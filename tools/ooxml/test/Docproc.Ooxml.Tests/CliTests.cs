using System.Diagnostics;
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;

namespace Docproc.Ooxml.Tests;

// The CLI's process contract, on the NativeAOT binary the check publishes into .state/aot/ before
// it runs the tests: arguments, stdout, stderr, exit code, and the media directory.
public sealed class CliTests : IDisposable
{
    private static readonly string Binary = Path.Combine(Fixtures.Root, ".state", "aot", "docproc-ooxml");

    private readonly DirectoryInfo scratch = Directory.CreateTempSubdirectory("docproc-ooxml-cli-tests-");

    public void Dispose() => scratch.Delete(recursive: true);

    [Fact]
    public void VersionIsTheAssemblysVersion()
    {
        var result = Run("--version");

        Assert.Equal(0, result.ExitCode);
        Assert.Equal(Cli.Version + "\n", result.Stdout);
        Assert.Empty(result.Stderr);
    }

    public static TheoryData<string, string> AllFixtures => Fixtures.All;

    // Each fixture's stdout and stderr, byte for byte, are its goldens, which `mise run golden`
    // recorded from an earlier run of the published binary; the comparison is a second run, so it
    // shows a run's bytes are the same every time as well as what they are. Every fixture
    // converts.
    [Theory]
    [MemberData(nameof(AllFixtures))]
    public void AFixturesOutputIsItsGoldens(string name, string variant)
    {
        var result = RunBytes(Fixtures.DocumentPath(name, variant));

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(File.ReadAllBytes(Fixtures.GoldenPath(name, variant, "jsonl")), result.Stdout);
        Assert.Equal(File.ReadAllBytes(Fixtures.GoldenPath(name, variant, "stderr")), result.Stderr);
    }

    // The goldens are each fixture's two and nothing else, so none outlives its fixture.
    [Fact]
    public void EveryGoldenIsAFixtures()
    {
        var expected = Fixtures.All.SelectMany(row => new[]
        {
            Fixtures.GoldenPath(row.Data.Item1, row.Data.Item2, "jsonl"),
            Fixtures.GoldenPath(row.Data.Item1, row.Data.Item2, "stderr"),
        });

        Assert.Equal(
            expected.Order(StringComparer.Ordinal),
            Directory.GetFiles(Fixtures.GoldenRoot, "*", SearchOption.AllDirectories).Order(StringComparer.Ordinal));
    }

    // Each distinct picture once, named by its bytes, however many parts and blocks hold it: the
    // media fixture's python-docx variant stores the red square in two parts and shows it three
    // times, and links one picture, which is never written. The directory and its missing parent
    // are created.
    [Fact]
    public void MediaAreWrittenOnceEachNamedByTheirContent()
    {
        var media = Path.Combine(scratch.FullName, "out", "media");
        var path = Fixtures.DocumentPath("media", "python-docx");

        var result = Run(path, "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("media", "python-docx"), result.Stdout);
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

    // The drawings fixture's openpyxl variant stores the red square twice, as two parts of one
    // sheet's drawing, and the blue square on a second sheet: two files.
    [Fact]
    public void SheetPicturesAreWrittenOnceEachNamedByTheirContent()
    {
        var media = Path.Combine(scratch.FullName, "media");
        var path = Fixtures.DocumentPath("drawings", "openpyxl");

        var result = Run(path, "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("drawings", "openpyxl"), result.Stdout);
        var files = Directory.GetFiles(media).Select(Path.GetFileName).Order(StringComparer.Ordinal).ToList();
        Assert.Equal(
            [
                "0fcca33bb0baf1a0669bd2fcdcaeb34e33d27196f7ffb5294b27aea8a9e46446.png",
                "10a86bd779a80e60e5d3314ec1edeab643cb454a4c7dfee671132090878d7aea.png",
            ],
            files);
        AssertEachStoredPartIsWritten(path, "xl/media/", 3, media);
    }

    // The slide-objects fixture shows the red square on two slides, one part, and the blue
    // square on one: two files, each the bytes of its part.
    [Fact]
    public void SlidePicturesAreWrittenOnceEachNamedByTheirContent()
    {
        var media = Path.Combine(scratch.FullName, "media");
        var path = Fixtures.DocumentPath("slide-objects", "python-pptx");

        var result = Run(path, "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("slide-objects", "python-pptx"), result.Stdout);
        var files = Directory.GetFiles(media).Select(Path.GetFileName).Order(StringComparer.Ordinal).ToList();
        Assert.Equal(
            [
                "0fcca33bb0baf1a0669bd2fcdcaeb34e33d27196f7ffb5294b27aea8a9e46446.png",
                "10a86bd779a80e60e5d3314ec1edeab643cb454a4c7dfee671132090878d7aea.png",
            ],
            files);
        AssertEachStoredPartIsWritten(path, "ppt/media/", 2, media);
    }

    [Fact]
    public void NothingIsWrittenWithoutTheMediaOption()
    {
        var before = Directory.GetFileSystemEntries(Path.GetDirectoryName(Fixtures.DocumentPath("media", "python-docx"))!);

        var result = RunIn(scratch.FullName, Fixtures.DocumentPath("media", "python-docx"));

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("media", "python-docx"), result.Stdout);
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
        Assert.Equal(Golden("headings", "python-docx"), result.Stdout);
        Assert.Empty(Directory.GetFileSystemEntries(media));
    }

    [Fact]
    public void MediaThatCannotBeWrittenLeaveStdoutEmptyAndExitOne()
    {
        var media = Write("media", [1, 2, 3]); // A file where the directory would be.

        var result = Run(Fixtures.DocumentPath("media", "python-docx"), "--media", media);

        Assert.Equal(Cli.MediaNotWritten, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.StartsWith("docproc-ooxml: media not written: ", result.Stderr, StringComparison.Ordinal);
    }

    [Fact]
    public void HelpPrintsUsageToStdout()
    {
        var result = Run("--help");

        Assert.Equal(0, result.ExitCode);
        Assert.Equal(Cli.Usage, result.Stdout);
        Assert.Empty(result.Stderr);
    }

    // An option's value may follow `=`, and `--` ends the options, so an input may begin with `-`.
    [Fact]
    public void OptionValuesFollowEqualsAndDoubleDashEndsTheOptions()
    {
        var media = Path.Combine(scratch.FullName, "media");
        var input = Path.Combine(scratch.FullName, "-tables.docx");
        File.Copy(Fixtures.DocumentPath("tables", "libreoffice"), input);

        var result = Run($"--media={media}", "--", input);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("tables", "libreoffice"), result.Stdout);
        Assert.Empty(result.Stderr);
        Assert.True(Directory.Exists(media));
    }

    // --format is gone: one run writes the one contract.
    [Theory]
    [InlineData]
    [InlineData("--format", "json", "in.docx")]
    [InlineData("--format=markdown", "in.docx")]
    [InlineData("--bogus", "in.docx")]
    [InlineData("-x", "in.docx")]
    [InlineData("one.docx", "two.docx")]
    [InlineData("in.docx", "--media")]
    [InlineData("--media", "a", "--media", "b", "in.docx")]
    [InlineData("--media", "a", "--media=b", "in.docx")]
    [InlineData("--version", "in.docx")]
    [InlineData("--help", "--version")]
    [InlineData("--help=yes")]
    [InlineData("--")]
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

        var result = Run(path);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal("{\"line\":\"document\",\"schema\":\"2\",\"format\":\"docx\",\"markdown\":\"kept\\n\"}\n", result.Stdout);
        Assert.Equal(
            $"docproc-ooxml: warning: {path}: a header reference of type \"odd\" in section 1 was skipped: no header has that type\n",
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

        var result = Run(path);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(
            """
            {"line":"document","schema":"2","format":"xlsx"}
            {"line":"sheet","name":"Data","markdown":"# Data\n\n|  | A |\n| --- | --- |\n"}
            {"line":"rows","markdown":"| 1 | 42 |\n","rows":[{"row":1,"cells":[{"ref":"A1","value_type":"number","value":42}]}]}

            """,
            result.Stdout);
        Assert.Equal(
            $"docproc-ooxml: warning: {path}: sheet \"Module\" was skipped: its relationship id \"\" names no part\n",
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
    // nine levels in the innermost cell.
    [Fact]
    public void TablesNestedToTheLimitConvertWithTheDeepestList()
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

        var result = Run(path);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Empty(result.Stderr);
        Assert.Contains("item 8", result.Stdout, StringComparison.Ordinal);
    }

    // An exception docproc-ooxml doesn't expect, here from a stdout that can't be written, is exit code
    // 4 and never 1, which says the media were not written.
    [Fact]
    public void AnUnexpectedErrorFailsWithItsOwnExitCode()
    {
        var stderr = new StringWriter();

        var code = Cli.Run([Fixtures.DocumentPath("headings", "python-docx")], new UnwritableWriter(), stderr);

        Assert.Equal(Cli.Failed, code);
        Assert.StartsWith(
            "docproc-ooxml: failed: " + Fixtures.DocumentPath("headings", "python-docx") + ": an unexpected System.InvalidOperationException: ",
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
        var result = Run(path, "--media", media);

        Assert.Equal(Cli.Refused, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.False(Directory.Exists(media));
        Assert.StartsWith("docproc-ooxml: refused: ", result.Stderr, StringComparison.Ordinal);
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

    // A fixture variant's stdout golden, as text.
    private static string Golden(string name, string variant) =>
        File.ReadAllText(Fixtures.GoldenPath(name, variant, "jsonl"));

    private sealed record Result(int ExitCode, string Stdout, string Stderr);

    private sealed record BytesResult(int ExitCode, byte[] Stdout, byte[] Stderr);

    private static Result Run(params string[] args) => RunIn(Environment.CurrentDirectory, args);

    private static Result RunIn(string directory, params string[] args)
    {
        var result = RunBytesIn(directory, args);
        return new Result(result.ExitCode, Encoding.UTF8.GetString(result.Stdout), Encoding.UTF8.GetString(result.Stderr));
    }

    private static BytesResult RunBytes(params string[] args) => RunBytesIn(Environment.CurrentDirectory, args);

    // The binary's exit code and its stdout's and stderr's bytes, as written.
    private static BytesResult RunBytesIn(string directory, params string[] args)
    {
        Assert.True(File.Exists(Binary), $"{Binary} is missing: `mise run check` publishes it before the tests run");
        var start = new ProcessStartInfo(Binary)
        {
            WorkingDirectory = directory,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
        };
        foreach (var arg in args)
        {
            start.ArgumentList.Add(arg);
        }
        using var process = Process.Start(start)!;
        using var stderr = new MemoryStream();
        var copying = process.StandardError.BaseStream.CopyToAsync(stderr, TestContext.Current.CancellationToken);
        using var stdout = new MemoryStream();
        process.StandardOutput.BaseStream.CopyTo(stdout);
        copying.Wait(TestContext.Current.CancellationToken);
        process.WaitForExit();
        return new BytesResult(process.ExitCode, stdout.ToArray(), stderr.ToArray());
    }
}
