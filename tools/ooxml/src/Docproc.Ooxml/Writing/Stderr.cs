using System.Text;

namespace Docproc.Ooxml.Writing;

/// <summary>A place in the document and what was skipped there: a warning for stderr. The place
/// is a position by ordinal, such as <c>section 3</c> or <c>sheet 2</c>, never the input's
/// path.</summary>
internal sealed record Warning(string Where, string What);

/// <summary>The one stderr writer, by the envelope (context/layers.md, "Common envelope"): one
/// line each, <c>docproc-ooxml: warning: &lt;where&gt;: &lt;what&gt;</c> and, for exit codes 1 to
/// 4, <c>docproc-ooxml: error: &lt;reason&gt;</c>, written as UTF-8 whatever the locale. Its cases
/// are tools/envelope/cases.json's.</summary>
internal sealed class Stderr(Stream stream)
{
    /// <summary>The tool's name, which begins each line.</summary>
    public const string Tool = "docproc-ooxml";

    /// <summary>Writes a warning line.</summary>
    public void Warning(Warning warning) => Write(WarningLine(warning.Where, warning.What));

    /// <summary>Writes an error line; the reason never leads with the input's path, which the
    /// caller already knows.</summary>
    public void Error(string reason) => Write(ErrorLine(reason));

    /// <summary>A warning's line, its line feed included. <c>&lt;where&gt;</c> and
    /// <c>&lt;what&gt;</c> are escaped, and <c>&lt;where&gt;</c> holds no <c>": "</c>, so the line
    /// splits at its first three.</summary>
    internal static string WarningLine(string where, string what) =>
        $"{Tool}: warning: {Escape(where, inWhere: true)}: {Escape(what, inWhere: false)}\n";

    /// <summary>An error's line, its line feed included. The contract states no escape for a
    /// reason; it takes <c>&lt;what&gt;</c>'s, so a reason that holds a line break, an argument
    /// echoed or an exception's message, still makes one line.</summary>
    internal static string ErrorLine(string reason) => $"{Tool}: error: {Escape(reason, inWhere: false)}\n";

    // A backslash before a backslash, \n and \r for a line feed and a carriage return, and, in
    // <where>, `:\ ` for a colon followed by a space; docproc inverts it.
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

    // stderr is the only place a failure to write could be told, so one is dropped: the exit code
    // still says what happened.
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
