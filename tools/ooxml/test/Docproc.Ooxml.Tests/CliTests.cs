using System.Diagnostics;
using System.Globalization;
using System.Runtime.Versioning;
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json.Nodes;
using Docproc.Ooxml.Reading;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Tests;

// The CLI's process contract, on the NativeAOT binary the check publishes into .state/aot/ before
// it runs the tests: arguments, stdout, stderr, exit code, and the media directory.
public sealed class CliTests : IDisposable
{
    private static readonly string Binary = Path.Combine(Fixtures.Root, ".state", "aot", "docproc-ooxml");

    private readonly DirectoryInfo scratch = Directory.CreateTempSubdirectory("docproc-ooxml-cli-tests-");

    public void Dispose() => scratch.Delete(recursive: true);

    // The version alone on the first line, which tools/pack.sh compares with a tag's, and the
    // schema on the second, so a reader's spec refuses a tool of another schema at startup.
    [Fact]
    public void VersionIsTheAssemblysVersionThenTheSchema()
    {
        var result = Run("--version");

        Assert.Equal(0, result.ExitCode);
        Assert.Equal($"{Cli.Version}\nschema 2\n", result.Stdout);
        Assert.Empty(result.Stderr);
    }

    public static TheoryData<string, string> FixturesWithGoldens => Fixtures.WithGoldens;

    // Each fixture's stdout and stderr, byte for byte, are its goldens, which `mise run golden`
    // recorded from an earlier run of the published binary; the comparison is a second run, so it
    // shows a run's bytes are the same every time as well as what they are. Its exit code is its
    // .exit golden's, one of the envelope's, or 0 when it has none: every fixture but the refused
    // ones converts.
    [Theory]
    [MemberData(nameof(FixturesWithGoldens))]
    public void AFixturesOutputIsItsGoldens(string name, string variant)
    {
        var exit = Fixtures.GoldenPath(name, variant, "exit");
        var expected = File.Exists(exit) ? int.Parse(File.ReadAllText(exit).TrimEnd('\n'), CultureInfo.InvariantCulture) : Cli.Converted;

        var result = RunBytes(Fixtures.DocumentPath(name, variant));

        Assert.Contains(expected, EnvelopeCases.Load().ExitCodes.Select(c => c.Code));
        Assert.Equal(Fixtures.Refused.ContainsKey(name) ? Cli.Refused : Cli.Converted, expected);
        Assert.Equal(expected, result.ExitCode);
        Assert.Equal(File.ReadAllBytes(Fixtures.GoldenPath(name, variant, "jsonl")), result.Stdout);
        Assert.Equal(File.ReadAllBytes(Fixtures.GoldenPath(name, variant, "stderr")), result.Stderr);
    }

    // The goldens are each fixture's two, and a refused fixture's .exit, and nothing else, so none
    // outlives its fixture.
    [Fact]
    public void EveryGoldenIsAFixtures()
    {
        var expected = Fixtures.WithGoldens.SelectMany(row => new[]
        {
            Fixtures.GoldenPath(row.Data.Item1, row.Data.Item2, "jsonl"),
            Fixtures.GoldenPath(row.Data.Item1, row.Data.Item2, "stderr"),
        }.Concat(Fixtures.Refused.ContainsKey(row.Data.Item1) ? [Fixtures.GoldenPath(row.Data.Item1, row.Data.Item2, "exit")] : []));

        Assert.Equal(
            expected.Order(StringComparer.Ordinal),
            Directory.GetFiles(Fixtures.GoldenRoot, "*", SearchOption.AllDirectories).Order(StringComparer.Ordinal));
    }

    // The encrypted fixture's reason begins `encrypted`, the envelope's one word for an encrypted
    // input, found by the compound file's own directory; its golden pins the whole line.
    [Fact]
    public void AnEncryptedPackageIsRefusedAsEncrypted()
    {
        var path = Fixtures.DocumentPath("encrypted", Fixtures.RefusedVariant);

        var reason = AssertRefused(path);

        Assert.StartsWith("encrypted", reason, StringComparison.Ordinal);
        Assert.Equal(OfficeReader.Encrypted, reason);
    }

    // A legacy binary document is an OLE compound file too, with no EncryptedPackage stream: its
    // reason never holds the word an encrypted input's begins with.
    [Fact]
    public void ALegacyBinaryDocumentIsRefusedWithoutTheWordEncrypted()
    {
        var reason = AssertRefused(Fixtures.DocumentPath("legacy", Fixtures.RefusedVariant));

        Assert.DoesNotContain("encrypted", reason, StringComparison.OrdinalIgnoreCase);
        Assert.Equal(OfficeReader.Legacy, reason);
    }

    // The same holds whatever the file's name: the content decides.
    [Fact]
    public void AnEncryptedPackageIsFoundByItsContentNotItsName()
    {
        var path = Path.Combine(scratch.FullName, "report.doc");
        File.Copy(Fixtures.DocumentPath("encrypted", Fixtures.RefusedVariant), path);

        Assert.Equal(OfficeReader.Encrypted, AssertRefused(path));
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

    // A sheet's embedded package and OLE object are written as stored, each after its preview,
    // and named in the media list with the second sheet line's ordinal.
    [Fact]
    public void SheetPayloadsAreWrittenAsStored()
    {
        var media = Path.Combine(scratch.FullName, "media");
        var path = Fixtures.DocumentPath("sheet-embeddings", "openpyxl");

        var result = Run(path, "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("sheet-embeddings", "openpyxl"), result.Stdout);
        Assert.Equal(4, Directory.GetFiles(media).Length);
        AssertEachStoredPartIsWritten(path, "xl/embeddings/", 2, media);
        AssertEachStoredPartIsWritten(path, "xl/media/", 2, media);
        var listed = JsonNode.Parse(result.Stdout.Split('\n')[0])!["media"]!.AsArray();
        Assert.Equal(["picture", "ole", "picture", "embedded"], listed.Select(m => (string?)m!["kind"]));
        Assert.All(listed, m => Assert.Equal(2, (int?)m!["sheet"]));
    }

    // A slide's embedded package and OLE object are written as stored; the chart beside them
    // embeds its source workbook, which is never media (context/design.md, OX-5).
    [Fact]
    public void SlidePayloadsAreWrittenAndAChartsWorkbookIsNot()
    {
        var media = Path.Combine(scratch.FullName, "media");
        var path = Fixtures.DocumentPath("slide-embeddings", "python-pptx");

        var result = Run(path, "--media", media);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("slide-embeddings", "python-pptx"), result.Stdout);
        using var package = ZipFile.OpenRead(path);
        var workbook = package.Entries.Single(e => e.FullName.StartsWith("ppt/embeddings/", StringComparison.Ordinal) && e.Name.EndsWith(".xlsx", StringComparison.Ordinal));
        using (var stream = workbook.Open())
        using (var bytes = new MemoryStream())
        {
            stream.CopyTo(bytes);
            Assert.False(File.Exists(Path.Combine(media, Convert.ToHexStringLower(SHA256.HashData(bytes.ToArray())) + ".xlsx")));
        }
        Assert.Equal(4, Directory.GetFiles(media).Length);
        Assert.DoesNotContain(".xlsx", result.Stdout, StringComparison.Ordinal);
    }

    // The properties by the reading rule (context/layers.md, "docproc-ooxml"): core values strings
    // as stored, an extended value all digits a number, a custom value its variant's type, a file
    // time its stored text, and one that isn't its type's the string it is stored as; keys in
    // stored order, a repeated key's first value kept with a warning, and an empty value or a
    // vector left out.
    [Fact]
    public void PropertiesAreReadByTheReadingRule()
    {
        var path = PropertiesPackage(
            "properties.docx",
            core: "<cp:coreProperties xmlns:cp=\"http://schemas.openxmlformats.org/package/2006/metadata/core-properties\" " +
                "xmlns:dc=\"http://purl.org/dc/elements/1.1/\" xmlns:dcterms=\"http://purl.org/dc/terms/\" xmlns:xsi=\"http://www.w3.org/2001/XMLSchema-instance\">" +
                "<dc:title>Plan</dc:title><dc:subject/><dc:creator>First</dc:creator><dc:creator>Second</dc:creator>" +
                "<dcterms:created xsi:type=\"dcterms:W3CDTF\">2026-01-15T10:00:00Z</dcterms:created><cp:revision>007</cp:revision></cp:coreProperties>",
            app: "<Properties xmlns=\"http://schemas.openxmlformats.org/officeDocument/2006/extended-properties\" " +
                "xmlns:vt=\"http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes\">" +
                "<Pages>0012</Pages><AppVersion>16.0300</AppVersion><ScaleCrop>false</ScaleCrop>" +
                "<TitlesOfParts><vt:vector size=\"1\" baseType=\"lpstr\"><vt:lpstr>Plan</vt:lpstr></vt:vector></TitlesOfParts></Properties>",
            custom: "<Properties xmlns=\"http://schemas.openxmlformats.org/officeDocument/2006/custom-properties\" " +
                "xmlns:vt=\"http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes\">" +
                CustomProperty(2, "Owner", "<vt:lpwstr>Ops: \"East\"</vt:lpwstr>") +
                CustomProperty(3, "Count", "<vt:i4>-5</vt:i4>") +
                CustomProperty(4, "Big", "<vt:ui8>18446744073709551615</vt:ui8>") +
                CustomProperty(5, "Ratio", "<vt:r8>1.5E3</vt:r8>") +
                CustomProperty(6, "Done", "<vt:bool>1</vt:bool>") +
                CustomProperty(7, "Due", "<vt:filetime>2026-02-01T00:00:00Z</vt:filetime>") +
                CustomProperty(8, "Odd", "<vt:i4>many</vt:i4>") +
                CustomProperty(9, "Empty", "<vt:lpwstr></vt:lpwstr>") +
                CustomProperty(10, "List", "<vt:vector size=\"1\" baseType=\"lpstr\"><vt:lpstr>a</vt:lpstr></vt:vector>") +
                CustomProperty(11, "Owner", "<vt:lpwstr>West</vt:lpwstr>") +
                "</Properties>");

        var result = Run(path);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(
            "{\"line\":\"document\",\"schema\":\"2\",\"format\":\"docx\",\"properties\":{" +
            "\"core\":{\"title\":\"Plan\",\"creator\":\"First\",\"created\":\"2026-01-15T10:00:00Z\",\"revision\":\"007\"}," +
            "\"app\":{\"Pages\":12,\"AppVersion\":\"16.0300\",\"ScaleCrop\":\"false\"}," +
            "\"custom\":{\"Owner\":\"Ops: \\\"East\\\"\",\"Count\":-5,\"Big\":18446744073709551615,\"Ratio\":1500,\"Done\":true," +
            "\"Due\":\"2026-02-01T00:00:00Z\",\"Odd\":\"many\"}},\"markdown\":\"kept\\n\"}\n",
            result.Stdout);
        Assert.Equal(
            "docproc-ooxml: warning: core properties: \"creator\" is repeated: its first value is kept\n" +
            "docproc-ooxml: warning: custom properties: \"Owner\" is repeated: its first value is kept\n",
            result.Stderr);
    }

    // A properties part that can't be parsed is a warning, never a refusal: the document and the
    // other parts' properties convert.
    [Theory]
    [InlineData("core")]
    [InlineData("app")]
    [InlineData("custom")]
    public void APropertiesPartThatCannotBeParsedIsAWarning(string broken)
    {
        const string Core = "<cp:coreProperties xmlns:cp=\"http://schemas.openxmlformats.org/package/2006/metadata/core-properties\" " +
            "xmlns:dc=\"http://purl.org/dc/elements/1.1/\"><dc:title>Plan</dc:title></cp:coreProperties>";
        const string App = "<Properties xmlns=\"http://schemas.openxmlformats.org/officeDocument/2006/extended-properties\"><Pages>2</Pages></Properties>";
        const string Custom = "<Properties xmlns=\"http://schemas.openxmlformats.org/officeDocument/2006/custom-properties\" " +
            "xmlns:vt=\"http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes\">" +
            "<property fmtid=\"{D5CDD505-2E9C-101B-9397-08002B2CF9AE}\" pid=\"2\" name=\"Owner\"><vt:lpwstr>Ops</vt:lpwstr></property></Properties>";
        var path = PropertiesPackage(
            $"{broken}.docx",
            core: broken == "core" ? "<cp:coreProperties" : Core,
            app: broken == "app" ? "<Properties><Pages>" : App,
            custom: broken == "custom" ? "not xml" : Custom);

        var result = Run(path);

        Assert.Equal(Cli.Converted, result.ExitCode);
        var line = JsonNode.Parse(result.Stdout)!;
        Assert.Equal("kept\n", (string?)line["markdown"]);
        var properties = line["properties"]!.AsObject();
        Assert.Equal(PropertyMaps.Where(map => map != broken), properties.Select(map => map.Key));
        Assert.StartsWith($"docproc-ooxml: warning: {broken} properties: not read: ", result.Stderr, StringComparison.Ordinal);
        AssertOneLine(result.Stderr);
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

        Assert.Equal(Cli.NotWritten, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.StartsWith("docproc-ooxml: error: media not written: ", result.Stderr, StringComparison.Ordinal);
        AssertOneLine(result.Stderr);
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
    public void UsageErrorsWriteOneErrorLineAndExitTwo(params string[] args)
    {
        var result = Run(args);

        Assert.Equal(Cli.UsageError, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.StartsWith("docproc-ooxml: error: ", result.Stderr, StringComparison.Ordinal);
        AssertOneLine(result.Stderr);
    }

    // A usage error's reason names what was wrong; an argument it echoes is escaped, so the error
    // stays one line.
    [Theory]
    [InlineData(new[] { "--bogus", "in.docx" }, "docproc-ooxml: error: unrecognized option '--bogus'\n")]
    [InlineData(new[] { "--bo\ngus" }, "docproc-ooxml: error: unrecognized option '--bo\\ngus'\n")]
    [InlineData(new[] { "one.docx", "two.docx" }, "docproc-ooxml: error: more than one input: only one is converted\n")]
    [InlineData(new string[0], "docproc-ooxml: error: no input\n")]
    [InlineData(new[] { "in.docx", "--media" }, "docproc-ooxml: error: option '--media' needs a value\n")]
    public void AUsageErrorsLineNamesTheError(string[] args, string stderr)
    {
        var result = Run(args);

        Assert.Equal(Cli.UsageError, result.ExitCode);
        Assert.Equal(stderr, result.Stderr);
    }

    [Fact]
    public void AMissingFileIsNotAFile() =>
        Assert.Equal(OfficeReader.NotAFile, AssertRefused(Path.Combine(scratch.FullName, "absent.docx")));

    [Fact]
    public void ADirectoryIsNotAFile()
    {
        var directory = Directory.CreateDirectory(Path.Combine(scratch.FullName, "folder.docx")).FullName;

        Assert.Equal(OfficeReader.NotAFile, AssertRefused(directory));
    }

    // A device is no regular file, though it can be opened and read.
    [Fact]
    public void ADeviceIsNotAFile() =>
        Assert.Equal(OfficeReader.NotAFile, AssertRefused("/dev/null"));

    // A symbolic link is followed to what it names.
    [Fact]
    public void ALinkIsReadAsWhatItNames()
    {
        var link = Path.Combine(scratch.FullName, "link.docx");
        File.CreateSymbolicLink(link, Fixtures.DocumentPath("headings", "python-docx"));

        var result = Run(link);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(Golden("headings", "python-docx"), result.Stdout);
        Assert.Equal(OfficeReader.NotAFile, AssertRefused(Path.Combine(scratch.FullName, "dangling.docx"), linkTo: Path.Combine(scratch.FullName, "gone")));
    }

    // A file that can't be opened is refused, its reason without the path .NET's message names.
    [Fact]
    [UnsupportedOSPlatform("windows")]
    public void AnUnreadableFileIsRefused()
    {
        var path = Path.Combine(scratch.FullName, "locked.docx");
        File.Copy(Fixtures.DocumentPath("headings", "python-docx"), path);
        File.SetUnixFileMode(path, UnixFileMode.None);

        Assert.Equal("cannot be read: permission denied", AssertRefused(path));
    }

    [Fact]
    public void AFileThatIsNoPackageIsRefused() =>
        Assert.Equal(OfficeReader.NotAPackage, AssertRefused(Write("notes.docx", Encoding.UTF8.GetBytes("plain text, not a package"))));

    [Fact]
    public void AZipThatIsNoOfficePackageIsRefused() =>
        Assert.Equal(OfficeReader.NotAPackage, AssertRefused(Zip("archive.docx", ("readme.txt", "hello"))));

    // The main part's exact content type decides the format: an xlsb's binary workbook is no
    // xlsx, though its content type names Excel.
    [Fact]
    public void AnXlsbIsNoPackageOfTheThree() =>
        Assert.Equal(
            OfficeReader.NotAPackage,
            AssertRefused(Package("book.xlsb", "xl/workbook.bin", "application/vnd.ms-excel.sheet.binary.macroEnabled.main", "binary")));

    // A macro-enabled workbook and a template are packages of the three, each by its own exact
    // content type.
    [Theory]
    [InlineData("book.xlsm", "xl/workbook.xml", "application/vnd.ms-excel.sheet.macroEnabled.main+xml", "<workbook xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"><sheets/></workbook>", "{\"line\":\"document\",\"schema\":\"2\",\"format\":\"xlsx\"}\n")]
    [InlineData("letter.dotx", "word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml", "<w:document xmlns:w=\"http://schemas.openxmlformats.org/wordprocessingml/2006/main\"><w:body><w:p><w:r><w:t>kept</w:t></w:r></w:p></w:body></w:document>", "{\"line\":\"document\",\"schema\":\"2\",\"format\":\"docx\",\"markdown\":\"kept\\n\"}\n")]
    public void OtherKindsOfTheThreeConvert(string name, string part, string contentType, string content, string stdout)
    {
        var result = Run(Package(name, part, contentType, content));

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Equal(stdout, result.Stdout);
        Assert.Empty(result.Stderr);
    }

    // A compound file whose directory can't be read is refused as malformed, never as encrypted.
    [Fact]
    public void AMalformedCompoundFileIsRefusedWithoutTheWordEncrypted()
    {
        var reason = AssertRefused(Write("secret.docx", [0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0, 0, 0, 0]));

        Assert.Equal("a malformed OLE compound file: shorter than its header", reason);
    }

    [Fact]
    public void ATruncatedPackageIsRefusedAsMalformed()
    {
        var bytes = File.ReadAllBytes(Fixtures.DocumentPath("headings", "python-docx"));
        Assert.StartsWith("a malformed package", AssertRefused(Write("truncated.docx", bytes[..(bytes.Length / 2)])), StringComparison.Ordinal);
    }

    [Fact]
    public void APackageWithBrokenXmlIsRefusedAsMalformed() =>
        Assert.StartsWith(
            "a malformed package",
            AssertRefused(Package("broken.docx", "word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "<w:document")),
            StringComparison.Ordinal);

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

        Assert.StartsWith("a malformed package", AssertRefused(path), StringComparison.Ordinal);
    }

    [Fact]
    public void AnXlsxWithBrokenXmlIsRefusedAsMalformed() =>
        Assert.StartsWith(
            "a malformed package",
            AssertRefused(Package("broken.xlsx", "xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml", "<workbook")),
            StringComparison.Ordinal);

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

        Assert.StartsWith("a malformed package", AssertRefused(path), StringComparison.Ordinal);
    }

    [Fact]
    public void APptxWithBrokenXmlIsRefusedAsMalformed() =>
        Assert.StartsWith(
            "a malformed package",
            AssertRefused(Package("broken.pptx", "ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", "<p:presentation")),
            StringComparison.Ordinal);

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
            "docproc-ooxml: warning: section 1: a header reference of type \"odd\" was skipped: no header has that type\n",
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
            "docproc-ooxml: warning: sheet 1: \"Module\" was skipped: its relationship id \"\" names no part\n",
            result.Stderr);
    }

    // Tables nested past eight deep are refused as they are reached, before any output, with
    // where: the ninth table's first paragraph, the body's second, after the one before them.
    [Fact]
    public void TablesNestedPastTheLimitAreRefused() =>
        Assert.Equal(
            "tables nested 9 deep (limit 8), at body paragraph 2",
            AssertRefused(
                Package(
                    "deep.docx",
                    "word/document.xml",
                    "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
                    Document("<w:p><w:r><w:t>before</w:t></w:r></w:p>" + NestedTables(9, "") + "<w:sectPr/>"))));

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

    // An exception docproc-ooxml doesn't expect, here from a stdout that fails other than by I/O,
    // is exit code 4, written as an error line, and never leaves Run, so the process never aborts.
    [Fact]
    public void AnUnexpectedErrorFailsWithItsOwnExitCode()
    {
        using var stderr = new MemoryStream();

        var code = Cli.Run([Fixtures.DocumentPath("headings", "python-docx")], new FailingStream(new InvalidOperationException("stdout\nis odd")), stderr);

        Assert.Equal(Cli.Failed, code);
        Assert.Equal(
            "docproc-ooxml: error: an unexpected System.InvalidOperationException: stdout\\nis odd\n",
            Encoding.UTF8.GetString(stderr.ToArray()));
    }

    // An I/O error on stdout is exit code 1, stdout not written.
    [Fact]
    public void AnIOErrorOnStdoutIsNotWritten()
    {
        using var stderr = new MemoryStream();

        var code = Cli.Run(["--version"], new FailingStream(new IOException("Input/output error")), stderr);

        Assert.Equal(Cli.NotWritten, code);
        Assert.Equal("docproc-ooxml: error: stdout not written: Input/output error\n", Encoding.UTF8.GetString(stderr.ToArray()));
    }

    // stderr that can't be written loses the line, never the exit code.
    [Fact]
    public void AnUnwritableStderrKeepsTheExitCode() =>
        Assert.Equal(Cli.UsageError, Cli.Run(["--bogus"], new MemoryStream(), new FailingStream(new IOException("Broken pipe"))));

    private sealed class FailingStream(Exception error) : MemoryStream
    {
        public override void Write(ReadOnlySpan<byte> buffer) => throw error;

        public override void Write(byte[] buffer, int offset, int count) => throw error;
    }

    // A full device on stdout is exit code 1, where the spike exited 4.
    [Fact]
    public void AFullStdoutIsNotWritten()
    {
        var result = Shell("exec \"$0\" \"$1\" >/dev/full", Fixtures.DocumentPath("headings", "python-docx"));

        Assert.Equal(Cli.NotWritten, result.ExitCode);
        Assert.Equal("docproc-ooxml: error: stdout not written: No space left on device\n", result.Stderr);
    }

    // A closed stdout is exit code 1, where the spike aborted with 134; with stderr closed too,
    // nothing can be told, and the exit code still is.
    [Theory]
    [InlineData("exec \"$0\" \"$1\" >&-", "docproc-ooxml: error: stdout not written: Bad file descriptor\n")]
    [InlineData("exec \"$0\" \"$1\" >&- 2>&-", "")]
    public void AClosedStdoutIsNotWritten(string script, string stderr)
    {
        var result = Shell(script, Fixtures.DocumentPath("headings", "python-docx"));

        Assert.Equal(Cli.NotWritten, result.ExitCode);
        Assert.Equal(stderr, result.Stderr);
    }

    // A reader that closes its end is a broken pipe: exit code 1, where the spike exited 0 over
    // truncated output. The lines pass the pipe's buffer, so the write fails whenever the reader
    // closes, before the tool writes or while it waits for room.
    [Fact]
    public void ABrokenPipeIsNotWritten()
    {
        var path = Zip(
            "large.xlsx",
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
                "<sheet name=\"Data\" sheetId=\"1\" r:id=\"rId1\"/></sheets></workbook>"),
            ("xl/_rels/workbook.xml.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet\" Target=\"worksheets/sheet1.xml\"/>" +
                "</Relationships>"),
            ("xl/worksheets/sheet1.xml",
                "<worksheet xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\"><sheetData>" +
                string.Concat(Enumerable.Range(1, 5000).Select(r => $"<row r=\"{r}\"><c r=\"A{r}\"><v>{r}</v></c></row>")) +
                "</sheetData></worksheet>"));
        var start = new ProcessStartInfo(Binary) { RedirectStandardOutput = true, RedirectStandardError = true };
        start.ArgumentList.Add(path);
        using var process = Process.Start(start)!;
        process.StandardOutput.Close();

        var stderr = process.StandardError.ReadToEnd();
        process.WaitForExit();

        Assert.Equal(Cli.NotWritten, process.ExitCode);
        Assert.Equal("docproc-ooxml: error: stdout not written: Broken pipe\n", stderr);
    }

    // A sheet's rows are read forward twice, never held whole (context/design.md, OX-2). Its
    // output is 50,000 rows in 33 MB of lines, run with the managed heap capped at 16 MiB, less
    // than the output and far less than the sheet's DOM, which the spike held: it peaked at
    // 513 MiB on this sheet, and failed under this cap with exit code 4. Each rows line is within 256 KiB, and
    // together they hold every row in order. It takes about two seconds.
    [Fact]
    public void ALargeSheetIsStreamedInBoundedMemory()
    {
        const int rows = 50_000;
        const int heapLimit = 16 * 1024 * 1024;
        var path = LargeSheet("large.xlsx", rows, "");

        var result = RunBytesIn(Environment.CurrentDirectory, new() { ["DOTNET_GCHeapHardLimit"] = $"0x{heapLimit:x}" }, path);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Empty(result.Stderr);
        Assert.True(result.Stdout.Length > heapLimit, $"{result.Stdout.Length} bytes of lines");
        var lines = Encoding.UTF8.GetString(result.Stdout).Split('\n')[..^1];
        Assert.Equal("""{"line":"document","schema":"2","format":"xlsx"}""", lines[0]);
        Assert.StartsWith("""{"line":"sheet","name":"Data",""", lines[1], StringComparison.Ordinal);
        var numbers = new List<int>();
        foreach (var line in lines[2..])
        {
            Assert.True(Encoding.UTF8.GetByteCount(line) + 1 <= LineWriter.RowsLineBound, $"a rows line of {Encoding.UTF8.GetByteCount(line) + 1} bytes");
            var held = JsonNode.Parse(line)!["rows"]!.AsArray();
            Assert.All(held, row => Assert.Equal(10, row!["cells"]!.AsArray().Count));
            numbers.AddRange(held.Select(row => (int)row!["row"]!));
        }
        Assert.True(lines.Length > 100, $"{lines.Length} lines");
        Assert.Equal(Enumerable.Range(1, rows), numbers);
    }

    // A failure in a sheet's last row is found by the validate pass, so the refusal still comes
    // before any output, though the rows before it fill many rows lines.
    [Fact]
    public void ASheetFailingInItsLastRowIsRefusedBeforeAnyOutput() =>
        Assert.Equal(
            "a malformed package: cell A5001's boolean 2 is not 0 or 1",
            AssertRefused(LargeSheet("late.xlsx", 5000, "<row r=\"5001\"><c r=\"A5001\" t=\"b\"><v>2</v></c></row>")));

    // A forward reading meets a sheet's merges after its cells, and can meet a shared formula's
    // anchor after a cell that names it; the columns, on the sheet line before the rows, are still
    // those of the kept cells. B1 is covered by A1:B1, so its value is left out and its column
    // too, and B2's boolean, which isn't one, is covered by A2:B2, so it is never read; C1 names
    // the anchor A3 comes later.
    [Fact]
    public void TheColumnsAreThoseOfTheKeptCellsWhereverTheMergesAndAnchorsAre()
    {
        var path = OneSheet(
            "merged.xlsx",
            "<sheetData>" +
            "<row r=\"1\"><c r=\"A1\"><v>1</v></c><c r=\"B1\"><v>2</v></c><c r=\"C1\"><f t=\"shared\" si=\"0\"/></c></row>" +
            "<row r=\"2\"><c r=\"A2\"><v>3</v></c><c r=\"B2\" t=\"b\"><v>9</v></c></row>" +
            "<row r=\"3\"><c r=\"A3\"><f t=\"shared\" ref=\"A3:C3\" si=\"0\">D1</f><v>4</v></c></row>" +
            "</sheetData><mergeCells count=\"2\"><mergeCell ref=\"A1:B1\"/><mergeCell ref=\"A2:B2\"/></mergeCells>");

        var result = Run(path);

        Assert.Equal(Cli.Converted, result.ExitCode);
        Assert.Empty(result.Stderr);
        Assert.Equal(
            """
            {"line":"document","schema":"2","format":"xlsx"}
            {"line":"sheet","name":"Data","markdown":"# Data\n\n|  | A | C |\n| --- | --- | --- |\n"}
            {"line":"rows","markdown":"| 1 | 1 | (formula shared from A3) |\n| 2 | 3 |  |\n| 3 | 4 |  |\n","rows":[{"row":1,"cells":[{"ref":"A1","value_type":"number","value":1,"col_span":2},{"ref":"C1","shared_from":"A3"}]},{"row":2,"cells":[{"ref":"A2","value_type":"number","value":3,"col_span":2}]},{"row":3,"cells":[{"ref":"A3","value_type":"number","value":4,"formula":"D1"}]}]}

            """,
            result.Stdout);
    }

    // A one-sheet workbook, its sheet "Data" the given worksheet content.
    private string OneSheet(string name, string worksheet) =>
        Zip(
            name,
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
                "<sheet name=\"Data\" sheetId=\"1\" r:id=\"rId1\"/></sheets></workbook>"),
            ("xl/_rels/workbook.xml.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet\" Target=\"worksheets/sheet1.xml\"/>" +
                "</Relationships>"),
            ("xl/worksheets/sheet1.xml",
                $"<worksheet xmlns=\"http://schemas.openxmlformats.org/spreadsheetml/2006/main\">{worksheet}</worksheet>"));

    // A one-sheet workbook of the given number of rows, each ten numbers in A to J, then the
    // given rows' XML.
    private string LargeSheet(string name, int rows, string after)
    {
        var sheet = new StringBuilder("<sheetData>");
        for (var r = 1; r <= rows; r++)
        {
            sheet.Append(CultureInfo.InvariantCulture, $"<row r=\"{r}\">");
            for (var c = 0; c < 10; c++)
            {
                sheet.Append(CultureInfo.InvariantCulture, $"<c r=\"{(char)('A' + c)}{r}\"><v>{(r * 10) + c}</v></c>");
            }
            sheet.Append("</row>");
        }
        return OneSheet(name, sheet.Append(after).Append("</sheetData>").ToString());
    }

    // stdout and stderr are UTF-8 bytes whatever the locale says: the ruby fixture's Japanese
    // text, and an error line echoing a non-ASCII option, under locales whose charset is ASCII or
    // Latin-1, where the spike wrote `??`.
    [Theory]
    [InlineData("LANG", "C")]
    [InlineData("LC_ALL", "POSIX")]
    [InlineData("LC_ALL", "C")]
    [InlineData("LC_ALL", "en_US.ISO-8859-1")]
    public void BytesAreUtf8UnderAnyLocale(string variable, string locale)
    {
        var environment = new Dictionary<string, string> { [variable] = locale };

        var fixture = RunBytesIn(Environment.CurrentDirectory, environment, Fixtures.DocumentPath("ruby", Fixtures.RealWorld));
        var usage = RunBytesIn(Environment.CurrentDirectory, environment, "--東京");

        Assert.Equal(Cli.Converted, fixture.ExitCode);
        Assert.Equal(File.ReadAllBytes(Fixtures.GoldenPath("ruby", Fixtures.RealWorld, "jsonl")), fixture.Stdout);
        Assert.True(fixture.Stdout.AsSpan().IndexOf("東京"u8) >= 0);
        Assert.Equal(Cli.UsageError, usage.ExitCode);
        Assert.Equal(Encoding.UTF8.GetBytes("docproc-ooxml: error: unrecognized option '--東京'\n"), usage.Stderr);
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
        Assert.Contains(
            "doesn't exist in the package",
            AssertRefused(Zip(
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
                    "</Relationships>"))),
            StringComparison.Ordinal);

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

    // The path is refused, exit code 3, before any output: stdout empty and no media directory.
    // stderr is one error line, whose reason never holds the path, given or in full, or the
    // file's name; returns the reason. With linkTo, the path is first made a link to it.
    private string AssertRefused(string path, string? linkTo = null)
    {
        if (linkTo is not null)
        {
            File.CreateSymbolicLink(path, linkTo);
        }
        var media = Path.Combine(scratch.FullName, "media");
        var result = Run(path, "--media", media);

        Assert.Equal(Cli.Refused, result.ExitCode);
        Assert.Empty(result.Stdout);
        Assert.False(Directory.Exists(media));
        Assert.StartsWith("docproc-ooxml: error: ", result.Stderr, StringComparison.Ordinal);
        AssertOneLine(result.Stderr);
        var reason = result.Stderr["docproc-ooxml: error: ".Length..^1];
        Assert.DoesNotContain(Path.GetFileName(path), reason, StringComparison.Ordinal);
        Assert.DoesNotContain(scratch.FullName, reason, StringComparison.Ordinal);
        return reason;
    }

    private static void AssertOneLine(string stderr)
    {
        Assert.EndsWith("\n", stderr, StringComparison.Ordinal);
        Assert.DoesNotContain('\n', stderr[..^1]);
        Assert.DoesNotContain('\r', stderr);
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

    // A docx package of one paragraph, "kept", with the given core, extended, and custom
    // properties parts, each related from the package as Office relates them.
    private string PropertiesPackage(string name, string core, string app, string custom) =>
        Zip(
            name,
            ("[Content_Types].xml",
                "<Types xmlns=\"http://schemas.openxmlformats.org/package/2006/content-types\">" +
                "<Default Extension=\"rels\" ContentType=\"application/vnd.openxmlformats-package.relationships+xml\"/>" +
                "<Override PartName=\"/word/document.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml\"/>" +
                "<Override PartName=\"/docProps/core.xml\" ContentType=\"application/vnd.openxmlformats-package.core-properties+xml\"/>" +
                "<Override PartName=\"/docProps/app.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.extended-properties+xml\"/>" +
                "<Override PartName=\"/docProps/custom.xml\" ContentType=\"application/vnd.openxmlformats-officedocument.custom-properties+xml\"/></Types>"),
            ("_rels/.rels",
                "<Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">" +
                "<Relationship Id=\"rId1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"word/document.xml\"/>" +
                "<Relationship Id=\"rId2\" Type=\"http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties\" Target=\"docProps/core.xml\"/>" +
                "<Relationship Id=\"rId3\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties\" Target=\"docProps/app.xml\"/>" +
                "<Relationship Id=\"rId4\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/custom-properties\" Target=\"docProps/custom.xml\"/>" +
                "</Relationships>"),
            ("word/document.xml", Document("<w:p><w:r><w:t>kept</w:t></w:r></w:p>")),
            ("docProps/core.xml", core),
            ("docProps/app.xml", app),
            ("docProps/custom.xml", custom));

    // The properties' maps, in the order the document line holds them.
    private static readonly string[] PropertyMaps = ["core", "app", "custom"];

    private static string CustomProperty(int pid, string name, string variant) =>
        $"<property fmtid=\"{{D5CDD505-2E9C-101B-9397-08002B2CF9AE}}\" pid=\"{pid}\" name=\"{name}\">{variant}</property>";

    // A fixture variant's stdout golden, as text.
    private static string Golden(string name, string variant) =>
        File.ReadAllText(Fixtures.GoldenPath(name, variant, "jsonl"));

    private sealed record Result(int ExitCode, string Stdout, string Stderr);

    private sealed record BytesResult(int ExitCode, byte[] Stdout, byte[] Stderr);

    private static Result Run(params string[] args) => RunIn(Environment.CurrentDirectory, args);

    private static Result RunIn(string directory, params string[] args)
    {
        var result = RunBytesIn(directory, null, args);
        return new Result(result.ExitCode, Encoding.UTF8.GetString(result.Stdout), Encoding.UTF8.GetString(result.Stderr));
    }

    private static BytesResult RunBytes(params string[] args) => RunBytesIn(Environment.CurrentDirectory, null, args);

    // The binary run by sh with the script given, its $0 the binary and $1 the argument, so the
    // script can redirect or close the binary's descriptors.
    private static Result Shell(string script, string argument)
    {
        var start = new ProcessStartInfo("/bin/sh") { RedirectStandardOutput = true, RedirectStandardError = true };
        foreach (var arg in new[] { "-c", script, Binary, argument })
        {
            start.ArgumentList.Add(arg);
        }
        using var process = Process.Start(start)!;
        var stdout = process.StandardOutput.ReadToEndAsync(TestContext.Current.CancellationToken);
        var stderr = process.StandardError.ReadToEnd();
        process.WaitForExit();
        return new Result(process.ExitCode, stdout.Result, stderr);
    }

    // The binary's exit code and its stdout's and stderr's bytes, as written, with the
    // environment's variables given set, the rest inherited.
    private static BytesResult RunBytesIn(string directory, Dictionary<string, string>? environment, params string[] args)
    {
        Assert.True(File.Exists(Binary), $"{Binary} is missing: `mise run check` publishes it before the tests run");
        var start = new ProcessStartInfo(Binary)
        {
            WorkingDirectory = directory,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
        };
        foreach (var (name, value) in environment ?? [])
        {
            start.Environment[name] = value;
        }
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
