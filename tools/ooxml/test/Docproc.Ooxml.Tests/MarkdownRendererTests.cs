using Docproc.Ooxml.Model;
using Docproc.Ooxml.Rendering;

namespace Docproc.Ooxml.Tests;

// The Markdown renderer's seam, independent of the reader: hand-built models against Markdown
// written by hand from GFM's rules, never a snapshot of docproc-ooxml's own output.
public sealed class MarkdownRendererTests
{
    // The envelope's literal-text rule (context/layers.md, "Common envelope"): its characters
    // anywhere, `$` among them; a `<` only where it could open a tag, a comment, or an
    // instruction, or ends the line; `>` only at a line's start.
    [Fact]
    public void TextMarkdownWouldMisreadIsEscaped()
    {
        var markdown = Render(
            Para("Stars *a* and _b_, [link](x), <tag>, a | pipe, & amp, ~strike~, `code`, $5, back\\slash"),
            Para("a < b > c, 1<2, <!-- c -->, <?x?>, </p>, </ and <"),
            Para("# not a heading"),
            Para("> not a quote"),
            Para("1. not a list"),
            Para("- not a bullet"),
            Para("  indented text\twith a tab"),
            Para("line one\nline two"));

        Assert.Equal(
            """
            Stars \*a\* and \_b\_, \[link\](x), \<tag>, a \| pipe, \& amp, \~strike\~, \`code\`, \$5, back\\slash

            a < b > c, 1<2, \<!-- c -->, \<?x?>, \</p>, </ and \<

            \# not a heading

            \> not a quote

            1\. not a list

            \- not a bullet

            indented text with a tab

            line one\
            line two

            """,
            markdown);
    }

    [Fact]
    public void AdjacentListsOfOneKindSwitchMarkers()
    {
        var markdown = Render(
            List(false, "a"),
            List(false, "b"),
            List(false, "c"),
            List(true, "d"),
            List(true, "e"),
            Para("text"),
            List(false, "f"));

        Assert.Equal(
            """
            - a

            * b

            - c

            1. d

            1) e

            text

            - f

            """,
            markdown);
    }

    [Fact]
    public void AMergedCellsTextIsInItsTopLeftPositionAndTheRestAreEmpty()
    {
        var markdown = Render(new Table(
        [
            new TableRow([new TableCell([Para("A")]) { ColSpan = 2 }, new TableCell([Para("C")])]),
            new TableRow([new TableCell([Para("D")]) { RowSpan = 2 }, new TableCell([Para("E")]), new TableCell([Para("F")])]),
            new TableRow([new TableCell([Para("G")]), new TableCell([Para("H")])]),
        ]));

        Assert.Equal(
            """
            | A |  | C |
            | --- | --- | --- |
            | D | E | F |
            |  | G | H |

            """,
            markdown);
    }

    // A list that resumes a count after a paragraph starts at the number it resumes at; a list
    // with no start counts from 1, and an unordered one has no numbers.
    [Fact]
    public void AnOrderedListCountsFromItsStart()
    {
        var markdown = Render(
            new ListBlock(true, [new ListItem([Para("ten")]), new ListItem([Para("eleven")])]) { Start = 10 },
            Para("text"),
            new ListBlock(true, [new ListItem([Para("twelve")])]) { Start = 12 },
            List(true, "one"));

        Assert.Equal(
            """
            10. ten
            11. eleven

            text

            12. twelve

            1) one

            """,
            markdown);
    }

    // A row that starts late leaves the columns it skips empty, its first cell in the column after
    // them, under a merged cell from above too.
    [Fact]
    public void ARowThatStartsLateLeavesTheColumnsItSkipsEmpty()
    {
        var markdown = Render(new Table(
        [
            new TableRow([new TableCell([Para("A")]), new TableCell([Para("B")]) { RowSpan = 2 }, new TableCell([Para("C")])]),
            new TableRow([new TableCell([Para("F")]) { SkipBefore = 1 }]),
            new TableRow([new TableCell([Para("I")]) { SkipBefore = 2 }]),
        ]));

        Assert.Equal(
            """
            | A | B | C |
            | --- | --- | --- |
            |  |  | F |
            |  |  | I |

            """,
            markdown);
    }

    // A footnote and an endnote share id 1 in the document; their labels count both kinds in
    // reference order, so they never collide, and a second reference reuses its note's label.
    [Fact]
    public void NotesAreFootnotesNumberedInReferenceOrder()
    {
        var document = Doc(
            new Paragraph([new TextRun("Alpha"), new FootnoteReference(1), new TextRun(" beta"), new EndnoteReference(1), new TextRun(".")]),
            new Paragraph([new TextRun("Again"), new FootnoteReference(1), new TextRun(".")])) with
        {
            Footnotes = [new Note(1, [Para(" First paragraph."), Para("Second paragraph.")])],
            Endnotes = [new Note(1, [Para("End note.")])],
        };

        Assert.Equal(
            """
            Alpha[^1] beta[^2].

            Again[^1].

            [^1]: First paragraph.

                Second paragraph.

            [^2]: End note.

            """,
            MarkdownRenderer.Render(document));
    }

    [Fact]
    public void CommentsAreFootnotesAfterTheirAnchorWithAuthorStateAndReplies()
    {
        var document = Doc(new Paragraph([new TextRun("The anchor"), new CommentReference(7), new TextRun(" text.")])) with
        {
            Comments =
            [
                new Comment(7, "Ana", "anchor", false, [Para("Check this.")], [new CommentReply(8, "Ben", [Para("Done.")])]),
                new Comment(9, null, "", true, [Para("Unanchored.")], []),
            ],
        };

        Assert.Equal(
            """
            The anchor[^c1] text.

            [^c1]: Comment by Ana (open) on "anchor": Check this.

                Reply by Ben: Done.

            [^c2]: Comment by an unknown author (resolved): Unanchored.

            """,
            MarkdownRenderer.Render(document));
    }

    [Fact]
    public void EachDistinctHeaderComesFirstAndEachDistinctFooterLast()
    {
        var document = Doc(Para("Body")) with
        {
            Sections =
            [
                new Section(
                    [new HeaderFooter(HeaderFooterKind.Default, [Para("Top")]), new HeaderFooter(HeaderFooterKind.First, [Para("First top")])],
                    [new HeaderFooter(HeaderFooterKind.Default, [Para("Bottom")])]),
                new Section(
                    [new HeaderFooter(HeaderFooterKind.Default, [Para("Top")])],
                    [new HeaderFooter(HeaderFooterKind.Default, [Para("Bottom 2")]), new HeaderFooter(HeaderFooterKind.Even, [Para("Bottom")])]),
            ],
        };

        Assert.Equal(
            """
            Top

            First top

            Body

            Bottom

            Bottom 2

            """,
            MarkdownRenderer.Render(document));
    }

    [Fact]
    public void AChartIsItsTitleAndATableOfItsSeries()
    {
        var markdown = Render(
            new Chart("bar", "Sales", [new ChartSeries("North", ["Q1", "Q2"], [1.5, null]), new ChartSeries("South", ["Q1", "Q2"], [2, 3])]),
            new Chart("pie", null, [new ChartSeries(null, [], [10, 20])]));

        Assert.Equal(
            """
            Sales

            |  | North | South |
            | --- | --- | --- |
            | Q1 | 1.5 | 2 |
            | Q2 |  | 3 |

            |  |  |
            | --- | --- |
            |  | 10 |
            |  | 20 |

            """,
            markdown);
    }

    // A picture's image text is its alternative text, or else its title; a title that differs
    // from the image text is the image's title, its quotes escaped.
    [Fact]
    public void MediaAreImageLinksToTheirFilesOrTargets()
    {
        var markdown = Render(
            new Media { Name = "abc.png", ContentType = "image/png", Alt = "A [bracketed] picture", Title = "A \"quoted\" title" },
            new Media { Target = "https://example.com/a b.png", Title = "Titled" },
            new Media { Name = "same.png", Alt = "Same", Title = "Same" },
            new Table([new TableRow([new TableCell([Para("Cell"), new Media { Name = "x.emf" }])])]));

        Assert.Equal(
            """
            ![A \[bracketed\] picture](abc.png "A \"quoted\" title")

            ![Titled](<https://example.com/a b.png>)

            ![Same](same.png)

            | Cell<br>![](x.emf) |
            | --- |

            """,
            markdown);
    }

    [Fact]
    public void ASheetIsItsNameAsAHeadingThenItsBlocksAndAHiddenSheetIsMarked()
    {
        var markdown = Render(
            new Sheet("Q1 | *draft*", false, [new Media { Name = "abc.png", Alt = "Logo", Sheet = 0 }]),
            new Sheet("Archive", true, []),
            new Sheet("", false, [new Chart("bar", "Totals", [])]));

        Assert.Equal(
            """
            # Q1 \| \*draft\*

            ![Logo](abc.png)

            # Archive (hidden)

            # Sheet

            Totals

            """,
            markdown);
    }

    // The header row is the column letters the cells use, in the sheet's order (Z before AA), the
    // first column each row's number; columns and rows with no cells are left out.
    [Fact]
    public void ASheetTableIsItsUsedColumnsAndRowsByLetterAndNumber()
    {
        var markdown = Render(new Sheet("S", false,
        [
            new Table(
            [
                new TableRow([Cell("AA2", CellValue.OfText("wide")), Cell("B2", CellValue.OfNumber(1))]) { Row = 2 },
                new TableRow([Cell("Z7", CellValue.OfText("a | b")), Cell("B7", CellValue.OfText("# x\nnext"))]) { Row = 7 },
            ]),
        ]));

        Assert.Equal(
            """
            # S

            |  | B | Z | AA |
            | --- | --- | --- | --- |
            | 2 | 1 |  | wide |
            | 7 | \# x<br>next | a \| b |  |

            """,
            markdown);
    }

    // Numbers in their shortest round-trip form, booleans as TRUE and FALSE, errors and dates as
    // they are; a formula shows its cached value, or with none its text as code, and a shared
    // formula's cell with none names its anchor.
    [Fact]
    public void ASheetCellShowsItsValueOrElseItsFormula()
    {
        var markdown = Render(new Sheet("S", false,
        [
            new Table(
            [
                new TableRow(
                [
                    Cell("A1", CellValue.OfNumber(0.1 + 0.2)),
                    Cell("B1", CellValue.OfNumber(1e21)),
                    Cell("C1", CellValue.OfBoolean(true)),
                    Cell("D1", CellValue.OfBoolean(false)),
                    Cell("E1", CellValue.OfError("#DIV/0!")),
                    Cell("F1", CellValue.OfDate("2026-01-15T10:30:00")),
                ]) { Row = 1 },
                new TableRow(
                [
                    Cell("A2", CellValue.OfNumber(6)) with { Formula = "SUM(A1:A3)" },
                    Cell("B2", null) with { Formula = "SUM(A1:A3)" },
                    Cell("C2", null) with { Formula = "IF(A1>0,\"a|b\",`x`)" },
                    Cell("D2", null) with { SharedFrom = "B2" },
                ]) { Row = 2 },
            ]),
        ]));

        Assert.Equal(
            """
            # S

            |  | A | B | C | D | E | F |
            | --- | --- | --- | --- | --- | --- | --- |
            | 1 | 0.30000000000000004 | 1E+21 | TRUE | FALSE | \#DIV/0! | 2026-01-15T10:30:00 |
            | 2 | 6 | `=SUM(A1:A3)` | ``=IF(A1>0,"a\|b",`x`)`` | (formula shared from B2) |  |  |

            """,
            markdown);
    }

    // A merged cell's value is in its anchor's position; the positions it covers are empty.
    [Fact]
    public void AMergedSheetCellIsInItsAnchorsPosition()
    {
        var markdown = Render(new Sheet("S", false,
        [
            new Table(
            [
                new TableRow([Cell("A1", CellValue.OfText("Title")) with { ColSpan = 2, RowSpan = 2 }, Cell("C1", CellValue.OfNumber(3))]) { Row = 1 },
                new TableRow([Cell("C2", CellValue.OfNumber(4))]) { Row = 2 },
            ]),
        ]));

        Assert.Equal(
            """
            # S

            |  | A | C |
            | --- | --- | --- |
            | 1 | Title | 3 |
            | 2 |  | 4 |

            """,
            markdown);
    }

    // Each slide under a heading of its number, a hidden one marked; its title a level below,
    // then its content, then its notes after a bold label, which a slide's own text can't make.
    [Fact]
    public void ASlideIsItsNumberAsAHeadingThenItsContentThenItsNotes()
    {
        var markdown = Render(
            new Slide(1, false, [new Heading(1, [new TextRun("Plan")]), List(false, "Hire")])
            {
                Notes = [Para("Speak slowly."), List(true, "Then the plan")],
            },
            new Slide(2, true, [Para("**Notes**")]),
            new Slide(3, false, [new Media { Name = "abc.png", Alt = "Logo", Slide = 2 }]) { Notes = [] });

        Assert.Equal(
            """
            # Slide 1

            ## Plan

            - Hire

            **Notes**

            Speak slowly.

            1. Then the plan

            # Slide 2 (hidden)

            \*\*Notes\*\*

            # Slide 3

            ![Logo](abc.png)

            """,
            markdown);
    }

    // A slide's heading is the outermost, so the slide's own headings sit below it, to the
    // deepest Markdown has; a heading outside a slide keeps its level.
    [Fact]
    public void ASlidesHeadingsSitALevelBelowItsOwn()
    {
        var markdown = Render(
            new Slide(1, false, [new Heading(1, [new TextRun("One")]), new Heading(6, [new TextRun("Six")])]),
            new Heading(1, [new TextRun("After")]));

        Assert.Equal(
            """
            # Slide 1

            ## One

            ###### Six

            # After

            """,
            markdown);
    }

    // An embedded package or OLE object links to its file, its text its program id or else its
    // part's file name, after its preview picture.
    [Fact]
    public void APayloadIsALinkAfterItsPreview()
    {
        var markdown = Render(
            new Media { Name = "preview.emf" },
            new Media { Kind = MediaKind.Ole, Name = "x.bin", ProgId = "Word.Document.8", Source = "/word/embeddings/oleObject1.bin" },
            new Media { Kind = MediaKind.Embedded, Name = "y.docx", Source = "/ppt/embeddings/My_Document.docx" });

        Assert.Equal(
            """
            ![](preview.emf)

            [Word.Document.8](x.bin)

            [My\_Document.docx](y.docx)

            """,
            markdown);
    }

    // An external hyperlink's text is linked to its target, escaped as any text is; its line
    // breaks are spaces, and one with no text is nothing. A destination holding a space is in
    // angle brackets, and a pipe in one is escaped.
    [Fact]
    public void AnExternalHyperlinkIsALink()
    {
        var markdown = Render(
            new Paragraph([new TextRun("Read "), new HyperlinkStart("https://example.invalid/a b"), new TextRun("the *guide*"), new HyperlinkEnd(), new TextRun(" now.")]),
            new Paragraph([new HyperlinkStart("https://example.invalid/?q=1|2"), new TextRun("two\nlines"), new HyperlinkEnd()]),
            new Paragraph([new TextRun("An empty "), new HyperlinkStart("https://example.invalid/"), new TextRun("  "), new HyperlinkEnd(), new TextRun("link")]),
            new Table([new TableRow([new TableCell([new Paragraph([new HyperlinkStart("https://example.invalid/c"), new TextRun("In a cell"), new HyperlinkEnd()])])])]));

        Assert.Equal(
            """
            Read [the \*guide\*](<https://example.invalid/a b>) now.

            [two lines](https://example.invalid/?q=1\|2)

            An empty link

            | [In a cell](https://example.invalid/c) |
            | --- |

            """,
            markdown);
    }

    // A paragraph renders as segments: each text run escaped on its own, a private-use character
    // kept as it is, each label appended where its reference is, and only text that begins a line
    // escaped as a line's start.
    [Fact]
    public void ReferencesAreLabelsBetweenTextSegments()
    {
        var document = new Document(SourceFormat.Docx,
        [
            new Paragraph([new TextRun("Private \uE000 and \uE001 use"), new FootnoteReference(1), new TextRun("\n1. after a break"), new FootnoteReference(2)]),
            new Paragraph([new FootnoteReference(1), new TextRun("# not a line's start")]),
        ])
        {
            Footnotes = [new Note(1, [Para("First.")]), new Note(2, [Para("Second.")])],
        };

        Assert.Equal(
            """
            Private U+E000 and U+E001 use[^1]\
            1\. after a break[^2]

            [^1]# not a line's start

            [^1]: First.

            [^2]: Second.

            """.Replace("U+E000", "\uE000", StringComparison.Ordinal).Replace("U+E001", "\uE001", StringComparison.Ordinal),
            MarkdownRenderer.Render(document));
    }

    // A chart in a docx table cell, which a GFM cell can't hold, renders after the table, its
    // title left in the cell, through nested tables.
    [Fact]
    public void AChartInATableCellFollowsTheTable()
    {
        var chart = new Chart("bar", "Cell chart", [new ChartSeries("Units", ["A"], [3])]);
        var nested = new Chart("pie", "Nested chart", [new ChartSeries("Share", ["B"], [1])]);
        var markdown = Render(
            new Table(
            [
                new TableRow(
                [
                    new TableCell([Para("Cell"), chart]),
                    new TableCell([new Table([new TableRow([new TableCell([nested])])])]),
                ]),
            ]),
            Para("After."));

        Assert.Equal(
            """
            | Cell<br>Cell chart | Nested chart |
            | --- | --- |

            Cell chart

            |  | Units |
            | --- | --- |
            | A | 3 |

            Nested chart

            |  | Share |
            | --- | --- |
            | B | 1 |

            After.

            """,
            markdown);
    }

    // A sheet cell with an external hyperlink links what it shows.
    [Fact]
    public void ASheetCellWithAHyperlinkLinksItsValue()
    {
        var markdown = Render(new Sheet("Links", false,
        [
            new Table(
            [
                new TableRow(
                [
                    Cell("A1", CellValue.OfText("The guide")) with { Target = "https://example.invalid/guide" },
                    Cell("B1", CellValue.OfNumber(42)) with { Target = "https://example.invalid/answer" },
                    Cell("C1", CellValue.OfText("plain")),
                ]) { Row = 1 },
            ]),
        ]));

        Assert.Equal(
            """
            # Links

            |  | A | B | C |
            | --- | --- | --- | --- |
            | 1 | [The guide](https://example.invalid/guide) | [42](https://example.invalid/answer) | plain |

            """,
            markdown);
    }

    private static TableCell Cell(string reference, CellValue? value) => new(null) { Ref = reference, Value = value };

    private static Paragraph Para(string text) => new([new TextRun(text)]);

    private static ListBlock List(bool ordered, string text) => new(ordered, [new ListItem([Para(text)])]);

    private static Document Doc(params Block[] blocks) => new(SourceFormat.Docx, blocks);

    private static string Render(params Block[] blocks) => MarkdownRenderer.Render(Doc(blocks));
}
