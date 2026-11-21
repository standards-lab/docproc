using System.Text;

namespace Docproc.Ooxml.Writing;

internal sealed record Warning(string Where, string What);

// See docs/tool-contract.md, "Common envelope".
internal sealed class Stderr(Stream stream)
{
    /// <summary>The tool's name, which begins each line.</summary>
    public const string Tool = "docproc-ooxml";

    /// <summary>Writes a warning line.</summary>
    public void Warning(Warning warning) => Write(WarningLine(warning.Where, warning.What));

    /// <summary>Writes an error line; the reason never leads with the input's path, which the
    /// caller already knows.</summary>
    public void Error(string reason) => Write(ErrorLine(reason));

    internal static string WarningLine(string where, string what) =>
        $"{Tool}: warning: {Escape(where, inWhere: true)}: {Escape(what, inWhere: false)}\n";

    internal static string ErrorLine(string reason) => $"{Tool}: error: {Escape(reason, inWhere: false)}\n";

    private static string Escape(string text, bool inWhere)
    {
        var escaped = new StringBuilder(text.Length);
        for (var i = 0; i < text.Length; i++)
        {
            switch (text[i])
            {
                case '\\':
                    escaped.Append(@"\\");
                    break;
                case '\n':
                    escaped.Append(@"\n");
                    break;
                case '\r':
                    escaped.Append(@"\r");
                    break;
                case ':' when inWhere && i + 1 < text.Length && text[i + 1] == ' ':
                    escaped.Append(@":\");
                    break;
                default:
                    escaped.Append(text[i]);
                    break;
            }
        }
        return escaped.ToString();
    }

    private void Write(string line)
    {
        try
        {
            stream.Write(Encoding.UTF8.GetBytes(line));
        }
        catch (IOException)
        {
        }
    }
}
