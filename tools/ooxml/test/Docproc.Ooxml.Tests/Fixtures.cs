namespace Docproc.Ooxml.Tests;

internal static class Fixtures
{
    public static string Root { get; } = FindRoot();

    public static readonly string[] Docx =
    [
        "headings", "lists", "tables", "tracked-changes",
        "sections", "headers-even-off", "notes", "comments", "comments-plain", "media", "charts", "links", "text-boxes", "unread",
    ];

    public static readonly string[] Xlsx = ["cells", "formulas", "sheets", "merges", "drawings", "dates-1904", "sheet-links", "sheet-embeddings", "sheet-text-boxes"];

    public static readonly string[] Pptx = ["slides", "bullets", "groups", "slide-objects", "slide-links", "slide-embeddings"];

    public static readonly string[] DocxWriters = ["python-docx", "libreoffice"];

    public static readonly string[] XlsxWriters = ["openpyxl", "libreoffice"];

    public static readonly string[] PptxWriters = ["python-pptx", "libreoffice"];

    public const string RealWorld = "realworld";

    public static readonly string[] RealWorldDocx =
    [
        "chart-column", "chartex", "chartex-funnel-word", "chartex-waterfall-word", "complex-numbered-lists", "content-control-rows",
        "delins", "endnotes", "footnote", "form-footnotes", "ruby", "strict-column-charts", "test-comment", "unit-test-formatting",
        "word-tables", "word-with-attachments",
    ];

    public static readonly string[] RealWorldXlsx =
    [
        "chart-sheet", "copy-rows", "date-format-tests", "excel-online-index-space", "simple-monthly-budget", "simple-strict",
        "with-drawing", "with-two-charts",
    ];

    public static readonly string[] RealWorldPptx = ["chart-2d", "chartex-slide", "layouts", "linked-pictures", "powerpoint-sample", "shapes"];

    public static readonly IReadOnlyDictionary<string, string> Refused = new Dictionary<string, string>
    {
        ["encrypted"] = "docx",
        ["legacy"] = "doc",
    };

    public const string RefusedVariant = "libreoffice";

    public static TheoryData<string, string> Variants
    {
        get
        {
            var data = new TheoryData<string, string>();
            foreach (var (names, writers) in new[] { (Docx, DocxWriters), (Xlsx, XlsxWriters), (Pptx, PptxWriters) })
            {
                foreach (var name in names)
                {
                    foreach (var variant in writers)
                    {
                        data.Add(name, variant);
                    }
                }
            }
            return data;
        }
    }

    public static TheoryData<string, string> All
    {
        get
        {
            var data = Variants;
            AddRealWorld(data);
            return data;
        }
    }

    public static TheoryData<string, string> WithGoldens
    {
        get
        {
            var data = All;
            foreach (var name in Refused.Keys)
            {
                data.Add(name, RefusedVariant);
            }
            return data;
        }
    }

    private static void AddRealWorld(TheoryData<string, string> data)
    {
        foreach (var name in RealWorldDocx.Concat(RealWorldXlsx).Concat(RealWorldPptx))
        {
            data.Add(name, RealWorld);
        }
    }

    public static string Format(string name) =>
        Xlsx.Contains(name) || RealWorldXlsx.Contains(name) ? "xlsx"
        : Pptx.Contains(name) || RealWorldPptx.Contains(name) ? "pptx"
        : "docx";

    public static string DocumentPath(string name, string variant) =>
        Refused.TryGetValue(name, out var extension)
            ? Path.Combine(Root, "fixtures", "generated", "refused", $"{name}.{variant}.{extension}")
        : variant == RealWorld
            ? Path.Combine(Root, "fixtures", "realworld", Format(name), $"{name}.{Format(name)}")
            : Path.Combine(Root, "fixtures", "generated", Format(name), $"{name}.{variant}.{Format(name)}");

    public static string TruthPath(string name, string variant)
    {
        if (variant == RealWorld)
        {
            return Path.Combine(Root, "fixtures", "realworld", Format(name), $"{name}.truth.json");
        }
        var directory = Path.Combine(Root, "fixtures", "generated", Format(name));
        var own = Path.Combine(directory, $"{name}.{variant}.truth.json");
        return File.Exists(own) ? own : Path.Combine(directory, $"{name}.truth.json");
    }

    public static string GoldenRoot { get; } = Path.Combine(Root, "test", "Docproc.Ooxml.Tests", "Expected");

    public static string GoldenPath(string name, string variant, string extension)
    {
        var fixture = Path.GetRelativePath(Path.Combine(Root, "fixtures"), DocumentPath(name, variant));
        return Path.Combine(GoldenRoot, Path.ChangeExtension(fixture, extension));
    }

    private static string FindRoot()
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
        {
            if (File.Exists(Path.Combine(dir.FullName, "DocprocOoxml.slnx")))
            {
                return dir.FullName;
            }
        }
        throw new InvalidOperationException("the tool's root, holding DocprocOoxml.slnx, is not above the test assembly");
    }
}
