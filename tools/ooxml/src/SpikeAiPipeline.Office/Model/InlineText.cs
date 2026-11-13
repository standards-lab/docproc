using System.Text;

namespace SpikeAiPipeline.Office.Model;

/// <summary>The plain text of inlines.</summary>
public static class InlineText
{
    /// <summary>The text of the given inlines, concatenated; inlines that carry no text add
    /// nothing.</summary>
    public static string Of(IReadOnlyList<Inline> inlines)
    {
        var text = new StringBuilder();
        foreach (var inline in inlines)
        {
            if (inline is TextRun run)
            {
                text.Append(run.Text);
            }
        }
        return text.ToString();
    }
}
