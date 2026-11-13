# Real-world sources

Each file here is copied byte for byte from a permissively licensed repository's test data,
pinned to one commit, and renamed; its SHA-256 below is checked against the blob at that commit.
Every one was written by Word, Excel, or PowerPoint, going by its `docProps/app.xml`
`Application`, and none but one file's parts carry the byte-order mark the Open XML SDK writes
when it re-serializes one. Three were then changed outside the application:

- `docx/word-tables.docx` is Word's file with its last two tables added to its XML (they carry
  none of the `w:rsid` attributes Word gives every other table, row, and paragraph) and re-zipped
  by a tool (every entry dated 2026-07-10 08:13, deflated alike).
- `docx/strict-column-charts.docx` is Word's file re-saved by the Open XML SDK, as the SDK's
  test data for ISO 29500 Strict: every XML part but `[Content_Types].xml` carries the SDK's
  byte-order mark. Its chart parts' Strict XML is what it is here for.
- `docx/chartex.docx`'s two chartEx parts are related and typed as ordinary charts (the `chart`
  relationship and content type, not `chartEx`'s), the drawings refer to them with a `c:chart`
  where Word writes a `cx:chart`, and their `mc:Choice` requires `cx`, where Word's own files
  here require `cx1` or `cx2`. So their XML was placed in Word's file by a tool; it is the
  chartEx schema all the same.

Office writes a chartEx part with no line break after its XML declaration and a space before
each `/>`, and its chart style and colors parts with no declaration at all, unlike the rest of
its parts; that layout is no sign of a tool. A tool shows instead in a UTF-8 byte-order mark
with a lowercase `utf-8` declaration, a chartEx part typed as an ordinary chart, or a `c:chart`
in the drawing. By those, `pptx/chartex-slide.pptx`'s chartEx part is PowerPoint's own: typed
`application/vnd.ms-office.chartex+xml`, referred to by a `cx:chart` under `Requires="cx1"`,
with its chart style and colors parts and its embedded workbook, in a package of entries all
dated 1980-01-01 (as Office zips them) and no byte-order mark. (Its box and whisker chart is
`docx/chartex.docx`'s, with a title there and none here.) The two `rsWordParser` files are
Word's by the same evidence (below).

Each source repository's license, and its NOTICE where it has one, is in `licenses/`.

| Repository | Commit | License | Notices |
| --- | --- | --- | --- |
| [dotnet/Open-XML-SDK](https://github.com/dotnet/Open-XML-SDK) | `431ab05cf160248cc3885a4a766026d4f8243792` | MIT, © .NET Foundation and Contributors | `licenses/Open-XML-SDK.LICENSE`, `licenses/Open-XML-SDK.NOTICE` |
| [apache/poi](https://github.com/apache/poi) | `6327f056ba3cfdae326a0261a236460592b80f5c` | Apache-2.0, © The Apache Software Foundation | `licenses/poi.LICENSE`, `licenses/poi.NOTICE` (its `legal/`) |
| [docling-project/docling](https://github.com/docling-project/docling) | `7b12671e1d324710e70784c18844d1d97522414f` | MIT, © The Docling Contributors | `licenses/docling.LICENSE` |
| [LilLeapo/rsWordParser](https://github.com/LilLeapo/rsWordParser) | `b1e64fd646a05aec877ced6b05374f536e4b9d96` | MIT or Apache-2.0 (dual), © 2026 rsWordParser contributors | `licenses/rsWordParser.LICENSE-MIT`, `licenses/rsWordParser.LICENSE-APACHE` |

One poi file here is listed in its `test-data/poi-integration-exceptions.csv`:
`xlsx/simple-strict.xlsx` (`SimpleStrict.xlsx`), as "Strict OOXML isn't currently supported,
please see bug #57699", a limit of poi's, not a fault of the file's.

The two rsWordParser files' provenance is self-reported by a young repository: its README's
许可 (license) section says the corpus documents are the project's own work under its two
licenses, and its notes name the writer as Word LTSC 2021. The files bear that out as Word's:
each chartEx part is typed `application/vnd.ms-office.chartex+xml` and referred to by a
`cx:chart` in the drawing, with its chart style and colors parts and its embedded workbook, the
package's entries are all dated 1980-01-01, and no part carries a byte-order mark. Each file's
`docProps/core.xml` names a person as its last editor; it is left as it is, so the file's
SHA-256 matches its source's.

## Files

| File | Repository | Path at the commit | Writer (`app.xml`) | Bytes | SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `docx/chart-column.docx` | Open-XML-SDK | `test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage/v2FxTestFiles/wordprocessing/chart/chart-Column.docx` | Microsoft Office Word 12.0000 | 18619 | `9402cb32b06d08f5a41b60ed2dcbe9dcc279a85a61059869fafa6d4d9540ff15` |
| `docx/chartex.docx` | poi | `test-data/document/chartex.docx` | Microsoft Office Word 16.0000, its chartEx parts a tool's (above) | 133304 | `5d7f0ca9aa26d0244eedbc8ff862f08910689ec3848a036c622b17d56a290b57` |
| `docx/chartex-funnel-word.docx` | rsWordParser | `corpus/real/chart/chartex-funnel-2.docx` | Microsoft Office Word 16.0000 (Word LTSC 2021 per the source repository's notes) | 28371 | `a44ad9f5a4b1e29564465864fd786a0906e48e180c866e97daf068e86456f199` |
| `docx/chartex-waterfall-word.docx` | rsWordParser | `corpus/real/chart/chartex-waterfall-2.docx` | Microsoft Office Word 16.0000 (Word LTSC 2021 per the source repository's notes) | 28811 | `0a7b7cc6b8d5a07a12f35afcc0cdb651046d52efacf0a5c27a01d38e5b4e11f6` |
| `docx/complex-numbered-lists.docx` | poi | `test-data/document/ComplexNumberedLists.docx` | Microsoft Office Word 14.0000 | 14458 | `297a085a7d433af2eeee7661e8db21539452cb585096484774a1e9f5f258b0b6` |
| `docx/content-control-rows.docx` | poi | `test-data/document/tika-3816.docx` | Microsoft Office Word 16.0000 | 22374 | `ab5b32d15d2fd14c633452cc5a8b1cd27cd65038cd208a11ede29d6f8fe24259` |
| `docx/delins.docx` | poi | `test-data/document/delins.docx` | Microsoft Office Word 12.0000 | 17720 | `7ecad102602586be7f1371f117fc567ea47f3ed0ca703b353e003002f1ff8d3f` |
| `docx/endnotes.docx` | poi | `test-data/document/endnotes.docx` | Microsoft Office Word 12.0000 | 13553 | `6e2e1b2dabb0ae6b9b26ddd613dca446ba0710ecc8b4008722abc4ed592934e3` |
| `docx/footnote.docx` | Open-XML-SDK | `test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage/v2FxTestFiles/wordprocessing/footnote & endnote/footnote.docx` | Microsoft Office Word 12.0000 | 13768 | `ae76e87fbdc0e9987b1c62a21b93a49dc7d8687b068aab07008cbebaaa009c3f` |
| `docx/form-footnotes.docx` | poi | `test-data/document/form_footnotes.docx` | Microsoft Office Word 12.0000 | 27824 | `a890294bfbdc63f6d03eab61efc1e98cb51d59f0920541150dd7082f9bdf31f1` |
| `docx/ruby.docx` | poi | `test-data/document/61470.docx` | Microsoft Office Word 16.0000 | 12523 | `abda850b3685027eef32a4c583b2b5f7f20f243026d8c453cdb3315348ed6ff0` |
| `docx/strict-column-charts.docx` | Open-XML-SDK | `test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage/O14ISOStrict/Graphics/2D Column-O12-Word-Charts.docx` | Microsoft Office Word 14.0000, then re-saved by the Open XML SDK (above) | 45855 | `12c5c01309a8ddec5215ba21e5e84c4250f400ea51d15041b4b96e00382e2050` |
| `docx/test-comment.docx` | poi | `test-data/document/testComment.docx` | Microsoft Office Word 16.0000 | 65298 | `06b63b13f0b78a7179151af52c2282557c1ed204899feee9fcef4ca575344f97` |
| `docx/unit-test-formatting.docx` | docling | `tests/data/docx/sources/unit_test_formatting.docx` | Microsoft Office Word 16.0000 | 29578 | `23518f8c635264b038b3e1207292f99cb5d40be35dbcd85d56508d3226ad9a11` |
| `docx/word-tables.docx` | docling | `tests/data/docx/sources/word_tables.docx` | Microsoft Office Word 16.0000, then edited and re-zipped (above) | 14247 | `0d69a7ef0e1ba2b3142394ed708227b95cf37de037ee71f434853d30e3ca5a55` |
| `docx/word-with-attachments.docx` | poi | `test-data/document/WordWithAttachments.docx` | Microsoft Office Word 12.0000 | 144959 | `90352263823e6d3d39a4ec88212134b74bcb0f86b603bc3b51d712a3ec3df9f2` |
| `xlsx/chart-sheet.xlsx` | poi | `test-data/spreadsheet/chart_sheet.xlsx` | Microsoft Excel 12.0000 | 10795 | `93e2d8663bc1a6b7d961f2a74440953ecdaadb1a3f171960129b730f7c935d3e` |
| `xlsx/copy-rows.xlsx` | poi | `test-data/spreadsheet/XSSFSheet.copyRows.xlsx` | Microsoft Excel 15.0300 | 9883 | `85b76d790c3eeb07693ea94293440a9efa2b91e41e2a6bea4b7bbd7ab9edba90` |
| `xlsx/date-format-tests.xlsx` | poi | `test-data/spreadsheet/DateFormatTests.xlsx` | Microsoft Macintosh Excel 16.0300 | 12394 | `16445dc0deba00671ee4315015a970d415ce9886da59c974532d946e99276c66` |
| `xlsx/excel-online-index-space.xlsx` | poi | `test-data/spreadsheet/bug69769.xlsx` | Microsoft Excel Online 16.0300 | 13976 | `098d7379336832d5c286bf93163c760d6595197b970b7a5c1156209169c0480c` |
| `xlsx/simple-monthly-budget.xlsx` | poi | `test-data/spreadsheet/simple-monthly-budget.xlsx` | Microsoft Excel 16.0300 | 18700 | `cae00c6894b95743017aeded4186f15a12c2d8382d71760029df490d58197db6` |
| `xlsx/simple-strict.xlsx` | poi | `test-data/spreadsheet/SimpleStrict.xlsx` | Microsoft Excel 15.0300 | 9225 | `dd6d2142e4c440c5cfaa241aa48883bfc0a76742b4531102dd41aea2f7a4d2ad` |
| `xlsx/with-drawing.xlsx` | poi | `test-data/spreadsheet/WithDrawing.xlsx` | Microsoft Excel 12.0000 | 101696 | `97b1ab359d6aecbe37864f30c85106e386f0a19bea8f290103927362a2701066` |
| `xlsx/with-two-charts.xlsx` | poi | `test-data/spreadsheet/WithTwoCharts.xlsx` | Microsoft Excel 12.0000 | 12810 | `7d29c92e9f7968a5bc9ec076458785295c24d13916b02b8a467c0a8fd013cddd` |
| `pptx/chart-2d.pptx` | Open-XML-SDK | `test/DocumentFormat.OpenXml.Tests.Assets/assets/TestDataStorage/v2FxTestFiles/presentation/Chart_2D.pptx` | Microsoft Office PowerPoint 12.0000 | 62118 | `dad57bde3989dbc48e5c52988773ccf74d1cf56af875e5366e4c92992ce258f3` |
| `pptx/chartex-slide.pptx` | Open-XML-SDK | `test/DocumentFormat.OpenXml.Tests.Assets/assets/TestFiles/Of16-02.pptx` | Microsoft Office PowerPoint 16.0000 | 51740 | `70d0ac69333b86bb6a3c6e489664c9e4298b9cb62440d287d54c5fc7cd79197e` |
| `pptx/layouts.pptx` | poi | `test-data/slideshow/layouts.pptx` | Microsoft Office PowerPoint 14.0000 | 62901 | `9c3d53afa3115de2ba72ec77637a616aeed505a6ff3b30a46db871c4edd32be5` |
| `pptx/linked-pictures.pptx` | poi | `test-data/slideshow/56812.pptx` | Microsoft Macintosh PowerPoint 14.0000 | 58308 | `36545b1502bc7149de9ec537ae2a7e83e6c8baf24c7f72e9944d863c41072b65` |
| `pptx/powerpoint-sample.pptx` | docling | `tests/data/pptx/sources/powerpoint_sample.pptx` | Microsoft Macintosh PowerPoint 16.0000 | 45849 | `24191d47fda511e11984e875f8676506da5ae1fbcaefcc63d81be377d3a3950c` |
| `pptx/shapes.pptx` | poi | `test-data/slideshow/shapes.pptx` | Microsoft Office PowerPoint 14.0000 | 68822 | `19fde9b87e33dd1a95fdbba0cf6abc2278bf03874f4665c7f8b88b6afe4a2571` |

## What each covers, and what its truth covers

Each file's ground truth is `<name>.truth.json` beside it, in the generated set's format
(`../README.md`). Every truth is whole: it holds every element of each kind the truth format
scores, so the tests compare all of the model, not a subset. Each was written from the file's own
XML, unzipped and read part by part, never from office's output:

- An xlsx truth's cells were read from the sheet XML by a throwaway script that knows nothing of
  office, with which styles are dates decided by hand from each number format.
- The truths of `docx/form-footnotes.docx`, `docx/word-tables.docx`, and
  `docx/word-with-attachments.docx` were drafted the same way, by a throwaway script reading
  WordprocessingML as the specification describes it, then checked by hand against the XML (all
  of the second and third; the first at its smart tags, form fields, picture cell, footnote, and
  headings).
- The charts' truths were read from the chart parts by a throwaway script too, a Strict chart's
  namespaces read as Transitional.
- The two rsWordParser files' truths, a chart and two paragraphs each, were written by hand from
  their `word/document.xml` and `word/charts/chartEx1.xml`.

Each truth's table cells hold every element, not only their paragraphs' text. They were
re-derived from the document's XML, every cell's paragraphs surveyed for numbering, heading
styles and outline levels, line breaks, and nested tables. Only `docx/form-footnotes.docx` has
any (headings, list items, and the footnote reference in cells), and its cells were rebuilt from
that survey; the rest hold plain paragraphs.

| File | Covers | Truth covers |
| --- | --- | --- |
| `docx/chart-column.docx` | Word's own chart: a 3-D clustered column chart (`bar3DChart`), titled, three series of cached data over three categories, beside its embedded workbook; a paragraph's text split across runs | all: the paragraph and the chart |
| `docx/chartex.docx` | Word 16's file of six charts: a clustered column chart in the third of three empty bullet items (the chart in a list item), a line chart, a radar chart and a stock chart over date serials, and two chartEx charts in `mc:AlternateContent` with fallback pictures, a box and whisker chart of three series over 22 points and a sunburst of a three-level hierarchy whose branches end at different depths (the regression file for chartEx charts, which were left out; their parts are a tool's, above) | all: the three list items, the paragraph, and the six charts, the chartEx ones as their cached data (`boxWhisker`, `sunburst`, a sunburst point labeled with its innermost level that names it); neither fallback picture, which the cached data replaces |
| `docx/chartex-funnel-word.docx` | Word's own chartEx chart: a funnel of one series over three categories, titled, in `mc:AlternateContent` under `Requires="cx2"` with a fallback picture, between two paragraphs of English and Chinese text | all: the two paragraphs and the chart as its cached data (`funnel`, 10, 20, 30); not the fallback picture, which the cached data replaces (the regression file for a chartEx choice that requires `cx2`, which must be read as `cx1`'s is, by its graphic's uri) |
| `docx/chartex-waterfall-word.docx` | Word's own chartEx chart: a waterfall of one series over three categories, titled, in `mc:AlternateContent` under `Requires="cx1"` with a fallback picture, between two paragraphs of English and Chinese text | all: the two paragraphs and the chart as its cached data (`waterfall`, 10, 20, 30); not the fallback picture |
| `docx/complex-numbered-lists.docx` | numbered lists on three abstract definitions, one with a nested lower-letter level; a definition whose level starts at 10, its list interrupted by a plain paragraph and resumed, counting on at 12 (the regression file for list starts and continuation) | all: 17 elements, the starts as `startAt` 10 and 12 |
| `docx/content-control-rows.docx` | a table row inside three nested content controls (`w:sdt` children of `w:tbl`), its first cell inside a drop-down content control showing its placeholder, "Choose an item."; a glossary part (the regression file for rows and cells in content controls, which were skipped with their text) | all: the paragraph and the table of five rows; the placeholder text in `absent` |
| `docx/delins.docx` | Word's real tracked changes: insertions in runs and of whole paragraphs, two list items deleted with their paragraph marks running on into an inserted paragraph, deleted fields, an inserted empty list item; hyperlink fields; bullet lists of three numbering instances on three abstract definitions | all: 22 elements; the deleted text and field instructions in `absent` |
| `docx/endnotes.docx` | an endnote reference, Word's separator and continuation-separator endnotes, a footnotes part of separators only; paragraphs of text with leading spaces | all: 10 paragraphs, the one endnote |
| `docx/footnote.docx` | a footnote reference, Word's separator and continuation-separator notes, an endnotes part of separators only; text with no-break spaces | all: 4 paragraphs, the one footnote, no endnotes |
| `docx/form-footnotes.docx` | a visa application form: six tables of merged cells (spans up to 15 columns), legacy form fields (`FORMTEXT` results of en spaces, which are blank, and `FORMCHECKBOX`), smart tags (`w:smartTag`, nested two deep) around runs (the regression file for smart tags, whose text was dropped), a picture and a footnote reference in a cell, numbered paragraphs and headings in cells, `w:sym` symbols, Heading 4 and Heading 7 paragraphs, a footer of one empty paragraph | all: six tables with every cell's elements (headings of levels 2, 4, 5, and 6, bullet lists, and the footnote's mark among them), two headings, a paragraph, and the footnote. `w:sym` symbols, which office doesn't read, are left out |
| `docx/ruby.docx` | a ruby (`w:ruby`): 東京 with its reading, とうきょう, as the annotation (the regression file for ruby, which was dropped whole) | all: the paragraph of the base text; the annotation in `absent` |
| `docx/strict-column-charts.docx` | ISO 29500 Strict: three clustered column charts, untitled, of three series over four categories, in Strict's chart and DrawingML namespaces (the regression file for Strict charts, which were read as `unknown` with no series) | all: the three charts |
| `docx/test-comment.docx` | a comment Word wrote with `commentsExtended`, `commentsIds`, and `commentsExtensible`, anchored mid-paragraph, holding a picture | all: the paragraph with its mark, the comment with its anchor and picture |
| `docx/unit-test-formatting.docx` | three sections: the first referencing default, first-page, and even-page headers and footers with no title page and even-page headers off, the second inheriting, the third with a title page; a first-page footer of three paragraphs and a picture in an `INCLUDEPICTURE` field; hyperlinks; a nested bullet list | all: 15 elements and every section's shown headers and footers |
| `docx/word-tables.docx` | seven tables: uniform; spans across outer and inner columns; vertical spans; every kind of span with empty cells, a span of three rows and one of a whole row; a row starting late (`w:gridBefore`, the regression file for late rows, whose cells were shifted left); a vertical span over a row starting late | all: 15 elements, the late row's cell with `skipBefore` 1 |
| `docx/word-with-attachments.docx` | three comments in Russian, Uzbek, and English across paragraphs, one whose range ends between paragraphs (a block-level `w:commentRangeEnd`) with its reference in the paragraph after (the regression file for block-level ranges, whose anchor was lost); OLE objects (a package, two Word 97 documents, two Excel workbooks) shown as WMF and EMF previews; a VML text box and WordArt; hyperlinks; a table; even-page and default headers with `PAGE` fields, even-page headers off | all: 23 elements, the five OLE previews among them as media, the default header, and the three comments with their anchors. The text box's and WordArt's text, which office doesn't read, are left out |
| `xlsx/chart-sheet.xlsx` | a chartsheet between worksheets, its bar chart's series with no categories; empty worksheets | all: four sheets, 10 cells, the chart |
| `xlsx/copy-rows.xlsx` | Excel's cells of many kinds: errors (`#N/A`, `#NAME?`), booleans, dates in built-in format 14, a built-in id (8) given a currency code, formulas over relative, absolute, and area references, string results (`t="str"`, one ending in a space), single-cell array formulas, merges across columns and across rows and columns, shared strings with line breaks | all: 90 cells |
| `xlsx/date-format-tests.xlsx` | the 1904 date system; datetimes with milliseconds in a custom format with escaped characters; shared formulas whose ranges are reused and one of a single cell; formula string results (`t="str"`), with Excel's own `TEXT` of each date beside it confirming the dates | all: both sheets, 194 cells |
| `xlsx/excel-online-index-space.xlsx` | a shared-string index Excel Online stores with a trailing space (`<v>2 </v>`, poi's bug 69769) | all: four cells |
| `xlsx/simple-monthly-budget.xlsx` | cached results of formulas over defined names; currency and percent formats that aren't dates; merged ranges with and without a value in their anchor, two starting on one row, one starting the row after another ends; a bar chart with its title deleted; an Excel table | all: 49 cells and the chart |
| `xlsx/simple-strict.xlsx` | ISO 29500 Strict SpreadsheetML (`conformance="strict"`): two sheets, shared strings, formulas with cached values, and Excel's own ISO-date cell (`t="d"`) in format 14 | all: both sheets, 19 cells |
| `xlsx/with-drawing.xlsx` | five pictures on one sheet, a JPEG, a PNG, two EMFs, and a WMF, each with its alternative text, and a text box among them; two empty sheets | all: three sheets, the five pictures. The text box's text, which office doesn't read in a sheet's drawing, is left out |
| `xlsx/with-two-charts.xlsx` | shared formulas with cached values, one cell inside a shared range holding its own formula; a line chart and an area chart on otherwise empty sheets | all: three sheets, 24 cells, two charts |
| `pptx/chart-2d.pptx` | PowerPoint's chart in a graphic frame, cached values as PowerPoint rounds them (`2.3999999999999995`), with notes | all: the slide, its chart, and its notes |
| `pptx/chartex-slide.pptx` | a slide whose only shape is a chartEx box and whisker chart in `mc:AlternateContent`, untitled, its fallback a picture (the regression file for a slide's chartEx chart, which was left out; its part is PowerPoint's own, above) | all: the slide and its chart, as its cached data, not the fallback picture |
| `pptx/layouts.pptx` | bullets inherited from a real master's body style through every built-in layout, nested nine levels; a title placeholder whose index its layout lacks; text boxes with their own bullets; a picture placeholder; date, footer, and slide-number placeholders with their text; an empty notes page | all: ten slides |
| `pptx/linked-pictures.pptx` | Mac PowerPoint's pictures: one embedded, one both embedded and linked to a file (`r:embed` and `r:link`), one linked only; text boxes | all: the slide's three pictures (the second as its embedded copy, the third as its link's target) and three paragraphs |
| `pptx/powerpoint-sample.pptx` | a table merged across columns (`gridSpan`, `hMerge`) and rows (`rowSpan`, `vMerge`) with empty cells; a paragraph whose own `buNone` overrides the master's bullets; text boxes of auto-numbered and character bullets; notes of one and two paragraphs | all: three slides and their notes |
| `pptx/shapes.pptx` | connectors, a freeform with text, a group of empty shapes, a picture, tables with merged and empty cells and hyperlinked text, a title last in its shape tree | all: six slides |
