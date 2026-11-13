namespace Docproc.Ooxml.Tests;

/// <summary>The committed fixtures and the files the tests compare docproc-ooxml's output with.</summary>
internal static class Fixtures
{
    /// <summary>The repository root, found from the test assembly's directory.</summary>
    public static string Root { get; } = FindRoot();

    /// <summary>The generated docx fixtures, by name.</summary>
    public static readonly string[] Docx =
    [
        "headings", "lists", "tables", "tracked-changes",
        "sections", "headers-even-off", "notes", "comments", "comments-plain", "media", "charts", "links", "unread",
    ];

    /// <summary>The generated xlsx fixtures, by name; no name is a docx fixture's too.</summary>
    public static readonly string[] Xlsx = ["cells", "formulas", "sheets", "merges", "drawings", "dates-1904", "sheet-links", "sheet-embeddings"];

    /// <summary>The generated pptx fixtures, by name; no name is a docx or xlsx fixture's too.</summary>
    public static readonly string[] Pptx = ["slides", "bullets", "groups", "slide-objects", "slide-links", "slide-embeddings"];

    /// <summary>The writers of each generated docx fixture: python-docx, and LibreOffice's re-save.</summary>
    public static readonly string[] DocxWriters = ["python-docx", "libreoffice"];

    /// <summary>The writers of each generated xlsx fixture: openpyxl, and LibreOffice's re-save.</summary>
    public static readonly string[] XlsxWriters = ["openpyxl", "libreoffice"];

    /// <summary>The writers of each generated pptx fixture: python-pptx, and LibreOffice's re-save.</summary>
    public static readonly string[] PptxWriters = ["python-pptx", "libreoffice"];

    /// <summary>The variant name of the real-world files, which have one each, as their
    /// application wrote it (<c>fixtures/realworld/</c>).</summary>
    public const string RealWorld = "realworld";

    /// <summary>The real-world docx files, by name; no name is a generated fixture's too.</summary>
    public static readonly string[] RealWorldDocx =
    [
        "chart-column", "chartex", "chartex-funnel-word", "chartex-waterfall-word", "complex-numbered-lists", "content-control-rows",
        "delins", "endnotes", "footnote", "form-footnotes", "ruby", "strict-column-charts", "test-comment", "unit-test-formatting",
        "word-tables", "word-with-attachments",
    ];

    /// <summary>The real-world xlsx files, by name.</summary>
    public static readonly string[] RealWorldXlsx =
    [
        "chart-sheet", "copy-rows", "date-format-tests", "excel-online-index-space", "simple-monthly-budget", "simple-strict",
        "with-drawing", "with-two-charts",
    ];

    /// <summary>The real-world pptx files, by name.</summary>
    public static readonly string[] RealWorldPptx = ["chart-2d", "chartex-slide", "layouts", "linked-pictures", "powerpoint-sample", "shapes"];

    /// <summary>The refused fixtures (<c>fixtures/generated/refused/</c>), by name, with their
    /// extensions: documents docproc-ooxml refuses, which LibreOffice stored, so their one variant
    /// is <see cref="RefusedVariant"/>. They have goldens and no truth.</summary>
    public static readonly IReadOnlyDictionary<string, string> Refused = new Dictionary<string, string>
    {
        ["encrypted"] = "docx",
        ["legacy"] = "doc",
    };

    /// <summary>The one variant of each refused fixture: LibreOffice stored it.</summary>
    public const string RefusedVariant = "libreoffice";

    /// <summary>Every generated fixture, docx, xlsx, then pptx, in each of its variants.</summary>
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

    /// <summary>The generated fixtures' variants, then the real-world files.</summary>
    public static TheoryData<string, string> All
    {
        get
        {
            var data = Variants;
            AddRealWorld(data);
            return data;
        }
    }

    /// <summary>Every fixture with goldens: those <see cref="All"/> converts, then the refused
    /// ones.</summary>
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

    /// <summary>A fixture's format, its file extension.</summary>
    public static string Format(string name) =>
        Xlsx.Contains(name) || RealWorldXlsx.Contains(name) ? "xlsx"
        : Pptx.Contains(name) || RealWorldPptx.Contains(name) ? "pptx"
        : "docx";

    /// <summary>A fixture variant's file: a generated fixture's, or a real-world file.</summary>
    public static string DocumentPath(string name, string variant) =>
        Refused.TryGetValue(name, out var extension)
            ? Path.Combine(Root, "fixtures", "generated", "refused", $"{name}.{variant}.{extension}")
        : variant == RealWorld
            ? Path.Combine(Root, "fixtures", "realworld", Format(name), $"{name}.{Format(name)}")
            : Path.Combine(Root, "fixtures", "generated", Format(name), $"{name}.{variant}.{Format(name)}");

    /// <summary>A fixture variant's ground truth: the variant's own where the generator wrote one,
    /// because LibreOffice's re-save changed what the document holds, or else the fixture's; a
    /// real-world file's is its own, written by hand from its XML.</summary>
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

    /// <summary>The directory of the goldens: each fixture's exact stdout (<c>.jsonl</c>) and
    /// stderr (<c>.stderr</c>) on the published binary, and its exit code (<c>.exit</c>) when that
    /// isn't 0, at the fixture's path under
    /// <c>fixtures/</c>, less its extension. Only <c>mise run golden</c> writes them.</summary>
    public static string GoldenRoot { get; } = Path.Combine(Root, "test", "Docproc.Ooxml.Tests", "Expected");

    /// <summary>A fixture variant's golden of the given extension: <c>jsonl</c>, <c>stderr</c>,
    /// or <c>exit</c>, which only a fixture that exits other than 0 has, holding its exit
    /// code.</summary>
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
