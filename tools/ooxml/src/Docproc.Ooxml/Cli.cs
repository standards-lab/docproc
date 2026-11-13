using System.Reflection;
using System.Text;
using Docproc.Ooxml.Reading;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml;

/// <summary>The CLI: one document in, its lines on stdout. Its exit codes are the envelope's
/// (context/layers.md, "Common envelope"; tools/envelope/cases.json), and stdout and stderr take
/// bytes, never text, so the locale can't change them.</summary>
internal static class Cli
{
    /// <summary>The document was read and its lines written to stdout, skipped items
    /// included.</summary>
    public const int Converted = 0;

    /// <summary>stdout or the media could not be written: an I/O error, a broken pipe included.
    /// The reason went to stderr.</summary>
    public const int NotWritten = 1;

    /// <summary>The arguments were wrong; the reason went to stderr.</summary>
    public const int UsageError = 2;

    /// <summary>The document was refused before any output (not a file, unreadable, not docx,
    /// xlsx, or pptx, encrypted or legacy, malformed, or past a limit of docproc-ooxml's); the
    /// reason went to stderr and nothing to stdout.</summary>
    public const int Refused = 3;

    /// <summary>A fault of docproc-ooxml's own, never the document's; the reason went to
    /// stderr.</summary>
    public const int Failed = 4;

    /// <summary>The schema of the lines docproc-ooxml writes, which <c>--version</c> states on its
    /// second line.</summary>
    public const string Schema = "2";

    /// <summary>The CLI's version, from its assembly.</summary>
    internal static string Version =>
        typeof(Cli).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion ?? "unknown";

    /// <summary>The usage text, written to stdout for <c>--help</c>.</summary>
    public const string Usage = """
        Usage:
          docproc-ooxml <input> [--media <dir>]
          docproc-ooxml --version
          docproc-ooxml --help

        Converts a docx, xlsx, or pptx document into JSON Lines on stdout, schema "2": a document
        line holding its properties, its Markdown rendering, and its media, then each xlsx sheet's
        sheet line and rows lines.

        Arguments:
          <input>          The docx, xlsx, or pptx file to convert.

        Options:
          --media <dir>    The directory embedded media (pictures, embedded packages, and OLE
                           objects) are written to as stored, each once, named by the SHA-256 of
                           its bytes.
          --version        Print the version, then the schema, and exit.
          --help           Print this usage and exit.

        Exit codes: 0 converted, 1 stdout or media not written, 2 usage, 3 refused before any
        output, 4 a fault of docproc-ooxml's own. stderr holds one line each:
        `docproc-ooxml: warning: <where>: <what>` or `docproc-ooxml: error: <reason>`.

        """;

    /// <summary>Runs the CLI on the given arguments and returns its exit code. Nothing leaves it
    /// as an exception: a fault of its own is exit code 4, so none leaves the process as an
    /// abort.</summary>
    public static int Run(string[] args, Stream stdout, Stream stderr)
    {
        var errors = new Stderr(stderr);
        try
        {
            if (!Parse(args, out var parsed, out var error))
            {
                errors.Error(error);
                return UsageError;
            }
            return parsed.Command switch
            {
                Command.Help => Write(stdout, Usage, errors),
                Command.Version => Write(stdout, $"{Version}\nschema {Schema}\n", errors),
                _ => Convert(parsed.Input!, parsed.Media, stdout, errors),
            };
        }
        catch (Exception e)
        {
            errors.Error($"an unexpected {e.GetType().FullName}: {e.Message}");
            return Failed;
        }
    }

    // stdout's one write: an I/O error, a broken pipe included, is exit code 1.
    private static int Write(Stream stdout, string text, Stderr errors)
    {
        try
        {
            stdout.Write(Encoding.UTF8.GetBytes(text));
            stdout.Flush();
            return Converted;
        }
        catch (IOException e)
        {
            errors.Error($"stdout not written: {e.Message}");
            return NotWritten;
        }
    }

    private enum Command
    {
        Convert,
        Help,
        Version,
    }

    private sealed record Arguments(Command Command, string? Input, string? Media);

    // The command line is parsed by hand: one input, --media taking a value, given as the next
    // argument or after `=`, at most once, and --version and --help, each alone. `--` ends the options, so an input may begin with `-`; `-` alone is an input.
    private static bool Parse(string[] args, out Arguments parsed, out string error)
    {
        parsed = new Arguments(Command.Convert, null, null);
        error = "";
        string? input = null, media = null;
        var action = Command.Convert;
        var options = true;
        for (var i = 0; i < args.Length; i++)
        {
            var arg = args[i];
            if (!options || arg == "-" || !arg.StartsWith('-'))
            {
                if (input is not null)
                {
                    error = "more than one input: only one is converted";
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
                        error = $"option '{name}' is given alone";
                        return false;
                    }
                    action = name == "--help" ? Command.Help : Command.Version;
                    break;
                case "--media":
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
                        error = $"option '{name}' needs a value";
                        return false;
                    }
                    if (media is not null)
                    {
                        error = $"option '{name}' is given more than once";
                        return false;
                    }
                    media = value;
                    break;
                default:
                    error = $"unrecognized option '{arg}'";
                    return false;
            }
        }
        if (action == Command.Convert && input is null)
        {
            error = "no input";
            return false;
        }
        parsed = new Arguments(action, input, media);
        return true;
    }

    // Every refusal is found before anything is written (OfficeReader.Open), so a refusal leaves
    // stdout empty and the media directory untouched. What the conversion skipped goes to stderr
    // as warnings. Media are written next, the directory created if missing, each distinct
    // content once (a file of the same name holds the same bytes, so it is rewritten); stdout
    // comes last, so it is written only when they all were. Its lines are written as they are
    // made, an xlsx sheet's as its rows are read.
    private static int Convert(string path, string? mediaDirectory, Stream stdout, Stderr errors)
    {
        PendingConversion conversion;
        try
        {
            conversion = OfficeReader.Open(path);
        }
        catch (RefusalException e)
        {
            errors.Error(e.Message);
            return Refused;
        }
        using (conversion)
        {
            foreach (var warning in conversion.Warnings)
            {
                errors.Warning(warning);
            }
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
                    errors.Error($"media not written: {e.Message}");
                    return NotWritten;
                }
            }
            // Not disposed: that would close stdout, and flush it again after a failed write.
            var output = new BufferedStream(new StdoutStream(stdout), 64 * 1024);
            try
            {
                conversion.WriteLines(output);
                output.Flush();
            }
            catch (StdoutException e)
            {
                errors.Error($"stdout not written: {e.Message}");
                return NotWritten;
            }
        }
        return Converted;
    }

    // An I/O error writing stdout, told apart from one reading the document while its lines are
    // written.
    private sealed class StdoutException(IOException error) : Exception(error.Message, error);

    // stdout, its I/O errors raised as StdoutException.
    private sealed class StdoutStream(Stream stdout) : Stream
    {
        public override bool CanRead => false;

        public override bool CanSeek => false;

        public override bool CanWrite => true;

        public override long Length => throw new NotSupportedException();

        public override long Position
        {
            get => throw new NotSupportedException();
            set => throw new NotSupportedException();
        }

        public override void Write(ReadOnlySpan<byte> buffer)
        {
            try
            {
                stdout.Write(buffer);
            }
            catch (IOException e)
            {
                throw new StdoutException(e);
            }
        }

        public override void Write(byte[] buffer, int offset, int count) => Write(buffer.AsSpan(offset, count));

        public override void Flush()
        {
            try
            {
                stdout.Flush();
            }
            catch (IOException e)
            {
                throw new StdoutException(e);
            }
        }

        public override int Read(byte[] buffer, int offset, int count) => throw new NotSupportedException();

        public override long Seek(long offset, SeekOrigin origin) => throw new NotSupportedException();

        public override void SetLength(long value) => throw new NotSupportedException();
    }
}
