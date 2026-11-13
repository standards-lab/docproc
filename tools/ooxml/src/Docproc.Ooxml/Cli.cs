using System.Reflection;
using Docproc.Ooxml.Model;
using Docproc.Ooxml.Reading;
using Docproc.Ooxml.Rendering;

namespace Docproc.Ooxml;

/// <summary>The CLI: one document in, its JSON model or Markdown on stdout.</summary>
public static class Cli
{
    /// <summary>The document was converted and written to stdout.</summary>
    public const int Converted = 0;

    /// <summary>The document was converted but its media could not be written to the media
    /// directory; the reason went to stderr and nothing to stdout.</summary>
    public const int MediaNotWritten = 1;

    /// <summary>The arguments were wrong; usage went to stderr.</summary>
    public const int UsageError = 2;

    /// <summary>The document was refused (missing, unreadable, not docx, xlsx, or pptx,
    /// encrypted or legacy, malformed, or past a limit of docproc-ooxml's); the reason went to stderr
    /// and nothing to stdout.</summary>
    public const int Refused = 3;

    /// <summary>The conversion failed in a way docproc-ooxml doesn't expect of any document: a fault
    /// of its own, never the document's; the error went to stderr and nothing to stdout.</summary>
    public const int Failed = 4;

    /// <summary>The CLI's version, from its assembly.</summary>
    public static string Version =>
        typeof(Cli).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion ?? "unknown";

    /// <summary>The usage text, written to stdout for <c>--help</c> and to stderr after a usage
    /// error.</summary>
    public const string Usage = """
        Usage:
          docproc-ooxml <input> [--format json|markdown] [--media <dir>]
          docproc-ooxml --version
          docproc-ooxml --help

        Converts a docx, xlsx, or pptx document into docproc-ooxml's JSON document model, or its
        Markdown rendering, on stdout.

        Arguments:
          <input>          The docx, xlsx, or pptx file to convert.

        Options:
          --format <json|markdown>
                           The rendering written to stdout (default: json).
          --media <dir>    The directory embedded media are written to, each once, named by the
                           SHA-256 of its bytes.
          --version        Print the version and exit.
          --help           Print this usage and exit.

        """;

    /// <summary>Runs the CLI on the given arguments and returns its exit code.</summary>
    public static int Run(string[] args, TextWriter stdout, TextWriter stderr)
    {
        if (!Parse(args, out var parsed, out var error))
        {
            stderr.WriteLine(error);
            stderr.WriteLine();
            stderr.Write(Usage);
            return UsageError;
        }
        switch (parsed.Command)
        {
            case Command.Help:
                stdout.Write(Usage);
                stdout.Flush();
                return Converted;
            case Command.Version:
                stdout.WriteLine(Version);
                stdout.Flush();
                return Converted;
            default:
                return Convert(parsed.Input!, parsed.Format, parsed.Media, stdout, stderr);
        }
    }

    private enum Command
    {
        Convert,
        Help,
        Version,
    }

    private sealed record Arguments(Command Command, string? Input, string Format, string? Media);

    // The command line is parsed by hand: one input, --format and --media each taking a value,
    // given as the next argument or after `=`, at most once, and --version and --help, each
    // alone. `--` ends the options, so an input may begin with `-`; `-` alone is an input.
    private static bool Parse(string[] args, out Arguments parsed, out string error)
    {
        parsed = new Arguments(Command.Convert, null, "json", null);
        error = "";
        string? input = null, format = null, media = null;
        var action = Command.Convert;
        var options = true;
        for (var i = 0; i < args.Length; i++)
        {
            var arg = args[i];
            if (!options || arg == "-" || !arg.StartsWith('-'))
            {
                if (input is not null)
                {
                    error = $"Unexpected argument '{arg}': only one input is converted.";
                    return false;
                }
                input = arg;
                continue;
            }
            if (arg == "--")
            {
                options = false;
                continue;
            }
            var equals = arg.IndexOf('=', StringComparison.Ordinal);
            var name = equals < 0 ? arg : arg[..equals];
            switch (name)
            {
                case "--help" or "--version" when equals < 0:
                    if (args.Length != 1)
                    {
                        error = $"Option '{name}' is given alone.";
                        return false;
                    }
                    action = name == "--help" ? Command.Help : Command.Version;
                    break;
                case "--format" or "--media":
                    string value;
                    if (equals >= 0)
                    {
                        value = arg[(equals + 1)..];
                    }
                    else if (i + 1 < args.Length)
                    {
                        value = args[++i];
                    }
                    else
                    {
                        error = $"Option '{name}' needs a value.";
                        return false;
                    }
                    if ((name == "--format" ? format : media) is not null)
                    {
                        error = $"Option '{name}' is given more than once.";
                        return false;
                    }
                    if (name == "--format")
                    {
                        if (value is not ("json" or "markdown"))
                        {
                            error = $"Option '--format' is json or markdown, not '{value}'.";
                            return false;
                        }
                        format = value;
                    }
                    else
                    {
                        media = value;
                    }
                    break;
                default:
                    error = $"Unrecognized option '{arg}'.";
                    return false;
            }
        }
        if (action == Command.Convert && input is null)
        {
            error = "Required argument missing: the input.";
            return false;
        }
        parsed = new Arguments(action, input, format ?? "json", media);
        return true;
    }

    // Any exception the conversion doesn't expect is a failure of its own, exit code 4, so it can
    // never pass for another outcome, such as exit code 1, which says the media could not be
    // written.
    private static int Convert(string path, string format, string? mediaDirectory, TextWriter stdout, TextWriter stderr)
    {
        try
        {
            return ConvertDocument(path, format, mediaDirectory, stdout, stderr);
        }
        catch (Exception e)
        {
            stderr.WriteLine($"docproc-ooxml: failed: {path}: an unexpected {e.GetType().FullName}: {e.Message}");
            return Failed;
        }
    }

    // The whole conversion and rendering are made before anything is written, so a refusal leaves
    // stdout empty and the media directory untouched. What the conversion skipped goes to stderr
    // as warnings. Media are written next, the directory created if missing, each distinct
    // content once (a file of the same name holds the same bytes, so it is rewritten); stdout
    // comes last, so it is written only when they all were.
    private static int ConvertDocument(string path, string format, string? mediaDirectory, TextWriter stdout, TextWriter stderr)
    {
        Conversion conversion;
        try
        {
            conversion = OfficeReader.Convert(path);
        }
        catch (RefusalException e)
        {
            stderr.WriteLine($"docproc-ooxml: refused: {e.Message}");
            return Refused;
        }
        foreach (var warning in conversion.Warnings)
        {
            stderr.WriteLine($"docproc-ooxml: warning: {path}: {warning}");
        }
        var output = format == "markdown" ? MarkdownRenderer.Render(conversion.Document) : DocumentJson.Serialize(conversion.Document);
        if (mediaDirectory is not null)
        {
            try
            {
                Directory.CreateDirectory(mediaDirectory);
                foreach (var (name, bytes) in conversion.Media)
                {
                    File.WriteAllBytes(Path.Combine(mediaDirectory, name), bytes);
                }
            }
            catch (Exception e) when (e is IOException or UnauthorizedAccessException)
            {
                stderr.WriteLine($"docproc-ooxml: media not written: {e.Message}");
                return MediaNotWritten;
            }
        }
        stdout.Write(output);
        stdout.Flush();
        return Converted;
    }
}
