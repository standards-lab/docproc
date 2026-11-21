using System.Text;
using System.Text.Json.Nodes;
using Docproc.Ooxml.Reading;
using Docproc.Ooxml.Rendering;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Tests;

internal sealed record EnvelopeCases(
    IReadOnlyList<(int Code, string Meaning)> ExitCodes,
    IReadOnlyList<(string Where, string What, string Line)> Warnings,
    IReadOnlyList<(string Reason, string Line)> Errors,
    IReadOnlyList<(byte[] Bytes, string StoredName, string Name)> MediaNames,
    IReadOnlyList<(string Text, string Markdown)> LiteralEscape,
    IReadOnlyList<(string Target, string Markdown)> Destinations,
    IReadOnlyList<(string Title, string Markdown)> Titles)
{
    public static string Path { get; } = System.IO.Path.GetFullPath(System.IO.Path.Combine(Fixtures.Root, "..", "envelope", "cases.json"));

    public const string Tool = "tool";

    public static EnvelopeCases Load()
    {
        var root = JsonNode.Parse(File.ReadAllText(Path))!;
        IEnumerable<JsonNode> Rows(string name) => root[name]!.AsArray().Select(row => row!);
        static string Text(JsonNode row, string field) => (string)row[field]!;
        return new EnvelopeCases(
            [.. Rows("exit_codes").Select(r => ((int)r["code"]!, Text(r, "meaning")))],
            [.. Rows("warnings").Select(r => (Text(r, "where"), Text(r, "what"), Text(r, "line")))],
            [.. Rows("errors").Select(r => (Text(r, "reason"), Text(r, "line")))],
            [.. Rows("media_names").Select(r => (Convert.FromHexString(Text(r, "bytes_hex")), Text(r, "stored_name"), Text(r, "name")))],
            [.. Rows("literal_escape").Select(r => (Text(r, "text"), Text(r, "markdown")))],
            [.. Rows("destinations").Select(r => (Text(r, "target"), Text(r, "markdown")))],
            [.. Rows("titles").Select(r => (Text(r, "title"), Text(r, "markdown")))]);
    }

    public static string As(string tool, string line)
    {
        Assert.StartsWith(Tool + ": ", line, StringComparison.Ordinal);
        return tool + line[Tool.Length..];
    }
}

public sealed class EnvelopeTests
{
    private static readonly EnvelopeCases Cases = EnvelopeCases.Load();

    [Fact]
    public void TheExitCodesAreTheEnvelopes() =>
        Assert.Equal(
            Cases.ExitCodes.Select(c => c.Code),
            [Cli.Converted, Cli.NotWritten, Cli.UsageError, Cli.Refused, Cli.Failed]);

    [Fact]
    public void AWarningsLineIsTheEnvelopes()
    {
        Assert.NotEmpty(Cases.Warnings);
        foreach (var (where, what, line) in Cases.Warnings)
        {
            Assert.Equal(EnvelopeCases.As(Stderr.Tool, line) + "\n", Stderr.WarningLine(where, what));
        }
    }

    [Fact]
    public void AWarningsLineSplitsBackToItsWhereAndWhat()
    {
        foreach (var (where, what, line) in Cases.Warnings)
        {
            var parts = line.Split(": ", 4);
            Assert.Equal([EnvelopeCases.Tool, "warning"], parts[..2]);
            Assert.Equal(where, Unescape(parts[2]));
            Assert.Equal(what, Unescape(parts[3]));
        }
    }

    [Fact]
    public void AnErrorsLineIsTheEnvelopes()
    {
        Assert.NotEmpty(Cases.Errors);
        foreach (var (reason, line) in Cases.Errors)
        {
            Assert.Equal(EnvelopeCases.As(Stderr.Tool, line) + "\n", Stderr.ErrorLine(reason));
        }
    }

    [Fact]
    public void AReasonsLineBreakIsEscaped() =>
        Assert.Equal("docproc-ooxml: error: a\\nb\\rc\\\\d: e\n", Stderr.ErrorLine("a\nb\rc\\d: e"));

    [Fact]
    public void TheStderrWriterWritesUtf8()
    {
        using var stream = new MemoryStream();

        new Stderr(stream).Warning(new Warning("東京 1", "x"));

        Assert.Equal(Encoding.UTF8.GetBytes("docproc-ooxml: warning: 東京 1: x\n"), stream.ToArray());
    }

    [Fact]
    public void AMediaNameIsTheEnvelopes()
    {
        Assert.NotEmpty(Cases.MediaNames);
        foreach (var (bytes, storedName, name) in Cases.MediaNames)
        {
            Assert.Equal(name, MediaStore.NameOf(bytes, storedName));
        }
    }

    [Fact]
    public void LiteralTextIsEscapedAsTheEnvelopeSays()
    {
        Assert.NotEmpty(Cases.LiteralEscape);
        foreach (var (text, markdown) in Cases.LiteralEscape)
        {
            Assert.Equal(markdown, MarkdownRenderer.Escape(text));
        }
    }

    [Fact]
    public void ADestinationIsTheEnvelopes()
    {
        Assert.NotEmpty(Cases.Destinations);
        foreach (var (target, markdown) in Cases.Destinations)
        {
            Assert.Equal(markdown, MarkdownRenderer.Destination(target));
        }
    }

    [Fact]
    public void ATitleIsTheEnvelopes()
    {
        Assert.NotEmpty(Cases.Titles);
        foreach (var (title, markdown) in Cases.Titles)
        {
            Assert.Equal(markdown, MarkdownRenderer.Title(title));
        }
    }

    private static string Unescape(string text)
    {
        var plain = new StringBuilder(text.Length);
        for (var i = 0; i < text.Length; i++)
        {
            if (text[i] != '\\' || i + 1 == text.Length)
            {
                plain.Append(text[i]);
                continue;
            }
            plain.Append(text[++i] switch
            {
                'n' => '\n',
                'r' => '\r',
                var c => c,
            });
        }
        return plain.ToString();
    }
}
