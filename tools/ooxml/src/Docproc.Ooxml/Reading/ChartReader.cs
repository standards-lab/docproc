using System.Globalization;
using System.Xml.Linq;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using Docproc.Ooxml.Model;
using A = DocumentFormat.OpenXml.Drawing;

namespace Docproc.Ooxml.Reading;

// See docs/tool-contract.md, "docproc-ooxml".
internal static class ChartReader
{
    private static readonly XNamespace C = "http://schemas.openxmlformats.org/drawingml/2006/chart";
    private static readonly XNamespace D = "http://schemas.openxmlformats.org/drawingml/2006/main";
    private static readonly XNamespace CX = "http://schemas.microsoft.com/office/drawing/2014/chartex";
    private const string RelationshipsNamespace = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
    private const int MaxPoints = 1 << 20;

    private static readonly Dictionary<XNamespace, XNamespace> StrictNamespaces = new()
    {
        ["http://purl.oclc.org/ooxml/drawingml/chart"] = C,
        ["http://purl.oclc.org/ooxml/drawingml/main"] = D,
    };

    /// <summary>The chart a part holds, or null for a chartEx chart that caches no data, which
    /// only its fallback picture shows.</summary>
    public static Chart? Read(OpenXmlPart part)
    {
        XDocument document;
        using (var stream = part.GetStream(FileMode.Open, FileAccess.Read))
        {
            document = XDocument.Load(stream);
        }
        if (document.Root?.Name.Namespace == CX)
        {
            return ReadChartEx(document.Root);
        }
        foreach (var element in document.Descendants())
        {
            if (StrictNamespaces.TryGetValue(element.Name.Namespace, out var transitional))
            {
                element.Name = transitional + element.Name.LocalName;
            }
        }
        var chart = document.Root?.Element(C + "chart");
        var plots = chart?.Element(C + "plotArea")?.Elements()
            .Where(e => e.Name.Namespace == C && e.Name.LocalName.EndsWith("Chart", StringComparison.Ordinal))
            .ToList() ?? [];
        var type = plots.Count > 0 ? plots[0].Name.LocalName[..^"Chart".Length] : "unknown";
        var series = plots.SelectMany(plot => plot.Elements(C + "ser")).Select(ReadSeries).ToList();
        return new Chart(type, Title(chart?.Element(C + "title")), series);
    }

    /// <summary>An mc:AlternateContent whose choice is a chartEx graphic (Office 2016's charts,
    /// which earlier applications can't show, so the fallback is their picture of it): the chart,
    /// from its cache, or with none cached, the fallback, for the caller to read as it reads any
    /// content. Null when no choice is a chartEx graphic.</summary>
    public static ChartExContent? ChartEx(AlternateContent content, OpenXmlPart host)
    {
        foreach (var choice in content.Elements<AlternateContentChoice>())
        {
            var data = choice.Descendants<A.GraphicData>().FirstOrDefault(d => d.Uri?.Value == CX.NamespaceName);
            if (data is null)
            {
                continue;
            }
            var id = data.ChildElements
                .SelectMany(e => e.GetAttributes())
                .FirstOrDefault(a => a.LocalName == "id" && a.NamespaceUri == RelationshipsNamespace)
                .Value;
            var chart = id is not null && host.TryGetPartById(id, out var part) ? Read(part) : null;
            return new ChartExContent(chart, chart is null ? content.GetFirstChild<AlternateContentFallback>() : null);
        }
        return null;
    }

    private static Chart? ReadChartEx(XElement root)
    {
        var chartData = root.Element(CX + "chartData");
        if (chartData?.Descendants(CX + "pt").Any() != true)
        {
            return null;
        }
        var data = new Dictionary<string, XElement>(StringComparer.Ordinal);
        foreach (var item in chartData.Elements(CX + "data"))
        {
            data.TryAdd((string?)item.Attribute("id") ?? "", item);
        }
        var chart = root.Element(CX + "chart");
        var series = chart?.Element(CX + "plotArea")?.Element(CX + "plotAreaRegion")?.Elements(CX + "series").ToList() ?? [];
        var type = series.Select(s => (string?)s.Attribute("layoutId")).FirstOrDefault() ?? "unknown";
        return new Chart(type, ChartExTitle(chart?.Element(CX + "title")), [.. series.Select(s =>
            ReadChartExSeries(s, (string?)s.Element(CX + "dataId")?.Attribute("val") is { } id ? data.GetValueOrDefault(id) : null))]);
    }

    private static string? ChartExTitle(XElement? title)
    {
        var tx = title?.Element(CX + "tx");
        if (tx?.Element(CX + "rich") is { } rich)
        {
            return RichText(rich);
        }
        return NonEmpty(tx?.Element(CX + "txData")?.Element(CX + "v")?.Value);
    }

    private static ChartSeries ReadChartExSeries(XElement series, XElement? data)
    {
        var name = NonEmpty(series.Element(CX + "tx")?.Element(CX + "txData")?.Element(CX + "v")?.Value);
        var categories = data?.Element(CX + "strDim")
            ?? data?.Elements(CX + "numDim").FirstOrDefault(d => (string?)d.Attribute("type") == "x");
        var values = data?.Elements(CX + "numDim").FirstOrDefault(d => (string?)d.Attribute("type") != "x");
        return new ChartSeries(
            name,
            categories is null ? [] : Levels(categories),
            values is null ? [] : [.. Levels(values).Select(Number)]);
    }

    private static List<string?> Levels(XElement dimension)
    {
        var levels = dimension.Elements(CX + "lvl")
            .Select(lvl => Points(
                lvl.Elements(CX + "pt").Select(pt => (Index(pt), (string?)pt.Value)),
                (string?)lvl.Attribute("ptCount")))
            .ToList();
        if (levels.Count == 0)
        {
            return [];
        }
        var result = levels[0];
        for (var i = 0; i < result.Count; i++)
        {
            result[i] = levels.Select(level => i < level.Count ? level[i] : null).FirstOrDefault(v => !string.IsNullOrEmpty(v)) ?? result[i];
        }
        return result;
    }

    private static string? Title(XElement? title)
    {
        var tx = title?.Element(C + "tx");
        if (tx?.Element(C + "rich") is { } rich)
        {
            return RichText(rich);
        }
        return tx is null ? null : Text(tx);
    }

    private static string? RichText(XElement rich) =>
        NonEmpty(string.Join("\n", rich.Elements(D + "p").Select(p => string.Concat(p.Descendants(D + "t").Select(t => t.Value)))));

    private static ChartSeries ReadSeries(XElement series)
    {
        var name = series.Element(C + "tx") is { } tx ? Text(tx) : null;
        var categories = series.Element(C + "cat") ?? series.Element(C + "xVal");
        var values = series.Element(C + "val") ?? series.Element(C + "yVal");
        return new ChartSeries(
            name,
            categories is null ? [] : Points(categories),
            values is null ? [] : [.. Points(values).Select(Number)]);
    }

    private static string? Text(XElement tx)
    {
        if (tx.Element(C + "v") is { } literal)
        {
            return literal.Value;
        }
        var points = tx.Descendants(C + "strCache").FirstOrDefault() is { } cache ? Cache(cache) : [];
        return NonEmpty(string.Join(" ", points.OfType<string>()));
    }

    private static List<string?> Points(XElement source)
    {
        foreach (var data in source.Elements())
        {
            var cache = data.Name.LocalName switch
            {
                "strRef" => data.Element(C + "strCache"),
                "numRef" => data.Element(C + "numCache"),
                "multiLvlStrRef" => data.Element(C + "multiLvlStrCache")?.Element(C + "lvl"),
                "strLit" or "numLit" => data,
                _ => null,
            };
            if (cache is not null)
            {
                return Cache(cache);
            }
        }
        return [];
    }

    private static List<string?> Cache(XElement cache) =>
        Points(
            cache.Elements(C + "pt").Select(pt => (Index(pt), pt.Element(C + "v")?.Value)),
            (string?)cache.Element(C + "ptCount")?.Attribute("val"));

    private static int Index(XElement pt) =>
        int.TryParse((string?)pt.Attribute("idx"), CultureInfo.InvariantCulture, out var i) ? i : -1;

    private static List<string?> Points(IEnumerable<(int Index, string? Value)> cached, string? count)
    {
        var points = cached.Where(pt => pt.Index >= 0).ToList();
        var declared = int.TryParse(count, CultureInfo.InvariantCulture, out var n) ? n : 0;
        var length = Math.Clamp(declared, points.Count > 0 ? points.Max(pt => pt.Index) + 1 : 0, MaxPoints);
        var result = new List<string?>(new string?[length]);
        foreach (var (index, value) in points.Where(pt => pt.Index < length))
        {
            result[index] = value;
        }
        return result;
    }

    private static double? Number(string? value) =>
        double.TryParse(value, NumberStyles.Float, CultureInfo.InvariantCulture, out var number) ? number : null;

    private static string? NonEmpty(string? value) => string.IsNullOrEmpty(value) ? null : value;
}

internal sealed record ChartExContent(Chart? Chart, AlternateContentFallback? Fallback);
