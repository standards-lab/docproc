using DocumentFormat.OpenXml;
using Docproc.Ooxml.Writing;

namespace Docproc.Ooxml.Reading;

/// <summary>The kinds of content docproc-ooxml doesn't read, each warned of once, where the
/// document first holds one (context/design.md, OX-11): text boxes, equations, SmartArt, pptx
/// comments, and xlsx notes, such as <c>slide 4: comments not read</c>. Reading them is
/// backlog.</summary>
internal sealed class UnreadKinds(List<Warning> warnings)
{
    public const string TextBoxes = "text boxes";
    public const string Equations = "equations";
    public const string SmartArt = "SmartArt";
    public const string Comments = "comments";
    public const string Notes = "notes";

    /// <summary>The graphic data URI of a SmartArt diagram.</summary>
    public const string DiagramUri = "http://schemas.openxmlformats.org/drawingml/2006/diagram";

    /// <summary>The namespace of Office Math, a docx paragraph's equations.</summary>
    public const string MathNamespace = "http://schemas.openxmlformats.org/officeDocument/2006/math";

    /// <summary>The namespace of a pptx or xlsx text body's equations, inside alternate
    /// content.</summary>
    public const string DrawingMathNamespace = "http://schemas.microsoft.com/office/drawing/2010/main";

    private readonly HashSet<string> found = new(StringComparer.Ordinal);

    /// <summary>Warns of a kind at the given place, unless the document already held one.</summary>
    public void Found(string where, string kind)
    {
        if (found.Add(kind))
        {
            warnings.Add(new Warning(where, $"{kind} not read"));
        }
    }

    /// <summary>Whether an element is or holds a SmartArt diagram's graphic data.</summary>
    public static bool HoldsSmartArt(OpenXmlElement element) =>
        Self(element).Concat(element.Descendants()).Any(e => e.LocalName == "graphicData"
            && e.GetAttributes().Any(a => a.LocalName == "uri" && a.Value == DiagramUri));

    /// <summary>Whether an element is or holds an equation: Office Math, or a text body's math
    /// inside alternate content.</summary>
    public static bool HoldsEquation(OpenXmlElement element) =>
        Self(element).Concat(element.Descendants()).Any(e => e.NamespaceUri == MathNamespace
            || (e.NamespaceUri == DrawingMathNamespace && e.LocalName == "m"));

    private static IEnumerable<OpenXmlElement> Self(OpenXmlElement element) => [element];
}
