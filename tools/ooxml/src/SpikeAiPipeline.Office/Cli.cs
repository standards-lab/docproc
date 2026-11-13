using System.CommandLine;
using System.Reflection;
using SpikeAiPipeline.Office.Model;
using SpikeAiPipeline.Office.Reading;
using SpikeAiPipeline.Office.Rendering;

namespace SpikeAiPipeline.Office;

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
    /// encrypted or legacy, malformed, or past a limit of office's); the reason went to stderr
    /// and nothing to stdout.</summary>
    public const int Refused = 3;

    /// <summary>The conversion failed in a way office doesn't expect of any document: a fault of
    /// office's, never the document's; the error went to stderr and nothing to stdout.</summary>
    public const int Failed = 4;

    /// <summary>The CLI's version, from its assembly.</summary>
    public static string Version =>
        typeof(Cli).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion ?? "unknown";

    /// <summary>Runs the CLI on the given arguments and returns its exit code.</summary>
    public static int Run(string[] args, TextWriter stdout, TextWriter stderr)
    {
        var input = new Argument<string>("input") { Description = "The docx, xlsx, or pptx file to convert." };
        var format = new Option<string>("--format")
        {
            Description = "The rendering written to stdout.",
            DefaultValueFactory = _ => "json",
            HelpName = "json|markdown",
        };
        format.AcceptOnlyFromAmong("json", "markdown");
        var media = new Option<string?>("--media")
        {
            Description = "The directory embedded media are written to, each once, named by the SHA-256 of its bytes.",
            HelpName = "dir",
        };
        var root = new RootCommand("Converts a docx, xlsx, or pptx document into office's JSON document model, or its Markdown rendering.")
        {
            input,
            format,
            media,
        };
        root.SetAction(parsed => Convert(parsed.GetValue(input)!, parsed.GetValue(format)!, parsed.GetValue(media), stdout, stderr));

        var result = root.Parse(args);
        if (result.Errors.Count > 0)
        {
            foreach (var error in result.Errors)
            {
                stderr.WriteLine(error.Message);
            }
            stderr.WriteLine();
            root.Parse(["--help"]).Invoke(new InvocationConfiguration { Output = stderr, Error = stderr });
            return UsageError;
        }
        return result.Invoke(new InvocationConfiguration { Output = stdout, Error = stderr });
    }

    // Any exception the conversion doesn't expect is a failure of its own, exit code 4, so it can
    // never pass for another outcome: System.CommandLine would otherwise report it with exit
    // code 1, which says the media could not be written.
    private static int Convert(string path, string format, string? mediaDirectory, TextWriter stdout, TextWriter stderr)
    {
        try
        {
            return ConvertDocument(path, format, mediaDirectory, stdout, stderr);
        }
        catch (Exception e)
        {
            stderr.WriteLine($"spike-ai-pipeline-office: failed: {path}: an unexpected {e.GetType().FullName}: {e.Message}");
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
            stderr.WriteLine($"spike-ai-pipeline-office: refused: {e.Message}");
            return Refused;
        }
        foreach (var warning in conversion.Warnings)
        {
            stderr.WriteLine($"spike-ai-pipeline-office: warning: {path}: {warning}");
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
                stderr.WriteLine($"spike-ai-pipeline-office: media not written: {e.Message}");
                return MediaNotWritten;
            }
        }
        stdout.Write(output);
        stdout.Flush();
        return Converted;
    }
}
