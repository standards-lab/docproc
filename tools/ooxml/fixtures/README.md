# Fixtures

The documents office's tests read, committed and never generated in the check. The pool is two
sets, and nothing else: the generated set in `generated/` and the real-world set in
`realworld/`. The hermetic check, `mise run check`, runs both, wherever it runs, locally or in
CI; no test reads a document from anywhere else, and none is fetched.

## Real-world set

`realworld/` holds documents Word, Excel, and PowerPoint wrote themselves, which the generated
set can't have: Word's own chart and tracked changes, Excel's cached formula results, shared
formulas, and 1904 dates, PowerPoint's bullets inherited from a real master, Office 2016's
chartEx charts. They are copied byte for byte from the test data of four permissively licensed
repositories, each pinned to a commit: dotnet/Open-XML-SDK (MIT), apache/poi (Apache-2.0),
docling-project/docling (MIT), and LilLeapo/rsWordParser (MIT or Apache-2.0).

Three were changed by a tool after their application wrote them: `docx/word-tables.docx` is
Word's file with tables added to its XML by hand and re-zipped, `docx/strict-column-charts.docx`
Word's file re-saved by the Open XML SDK, and the chartEx parts of `docx/chartex.docx` a tool's
XML. The chartEx parts of `pptx/chartex-slide.pptx`, `docx/chartex-waterfall-word.docx`, and
`docx/chartex-funnel-word.docx` are the application's own. `SOURCES.md` gives the evidence for
each and says how a tool's chartEx part is told from Office's. Several files are the regression
files of a reading fault, each named in `SOURCES.md`, so the fault's return fails the check.
`realworld/SOURCES.md` lists each file's repository, commit, path, license, writer, size, and
SHA-256, what it covers, and what its truth covers; the repositories' licenses and notices are
in `realworld/licenses/`.

Each file is `realworld/<format>/<name>.<format>`, with its ground truth beside it in the
generated set's format, `<name>.truth.json`. The truth is written by hand from the file's XML
rather than from office's output, or for the larger files drafted from the XML by a throwaway
script that knows nothing of office and checked by hand. Every truth is whole. The tests compare
the model against it as they do a generated fixture's, and run each file through the published
binary; they keep no exact JSON or Markdown of these files, whose truth is the check. Nothing
regenerates the set: a file is added by hand, with its row in `SOURCES.md` and its truth.

## Generated set

`generate.py` writes each fixture of `generated/` from the definitions in the script itself, then
re-saves it through LibreOffice headless. The docx fixtures are in `generated/docx/`:

- `<name>.python-docx.docx`: written by python-docx, with what python-docx can't write added as
  raw XML or raw package parts: the tracked changes (`w:ins`, `w:del`, `w:moveFrom`, `w:moveTo`,
  a deleted paragraph mark), the drawings of pictures and charts, the chart parts (cached series
  data, no embedded workbook), a picture part of its own and a linked picture's external
  relationship, the footnotes and endnotes parts (with Word's separator and continuation notes),
  and the comments and commentsExtended parts.
- `<name>.libreoffice.docx`: that file opened and saved again by LibreOffice, so the set also
  carries a real application's XML. Both are kept: the first is the XML the script means to
  write, the second what an editor makes of it, and office must read both the same way.
- `<name>.truth.json`: the ground truth both files share, written by the script from the same
  definitions. Its shape is its own, not office's model, so the tests compare the model against
  it element by element. It holds:
  - `elements`: the accepted-state text of each element in document order:
    - `heading`, with its level.
    - `paragraph`.
    - `listItem`, with its nesting level, whether its level is ordered, and whether it starts a
      list rather than continuing the one before it; on an ordered list's first item, its first
      number, `startAt`, when that isn't 1.
    - `table`, with each cell's `elements`, in this same shape whatever the cell holds
      (paragraphs, headings, list items, pictures, charts, tables), its spans, and `skipBefore`,
      the grid columns a row that starts late skips before it. Covered cells are left out. No
      generated fixture has a late row, so the script writes no `skipBefore`; real-world truths
      do.
    - `media`, with its file name (the SHA-256 of its bytes and its extension) and content type,
      or its linked `target`, and its `alt`, `title`, and `section`.
    - `chart`, with its type, title, and series.
  - `marks` on a paragraph or list item: each note or comment reference's offset in the text,
    its kind, and its note's or comment's place, from 1, in the truth's list of them.
  - `absent`: the strings that must appear nowhere in office's output (deleted text).
  - `once`: the strings that must appear exactly once (moved text).
  - `sections`: each section's headers and footers as its pages show them, kind and elements.
  - `footnotes` and `endnotes`: the notes, in the order the text first references them.
  - `comments`: each thread's author, anchored text, resolved state, elements, and replies.
- `<name>.libreoffice.truth.json`: the re-save's own ground truth, where LibreOffice changes what
  the document holds; the script writes it from the same definitions, with the change it knows
  LibreOffice makes.

The xlsx fixtures are in `generated/xlsx/`, the same three or four files each:

- `<name>.openpyxl.xlsx`: written by openpyxl, with what openpyxl can't write patched in as raw
  XML: the shared formula, the cell stored as an ISO date (`t="d"`), and the pictures'
  alternative text.
- `<name>.libreoffice.xlsx`: that file re-saved by LibreOffice.
- `<name>.truth.json` and `<name>.libreoffice.truth.json`: each sheet in workbook order, its name,
  whether it is hidden, and its elements: `table` with the rows that have cells, each its `row`
  number and its `cells`, each cell its `ref`, its value's `type` and `value` (a date as ISO
  8601, from the date the script gave openpyxl), its `formula` or its shared formula's anchor
  (`sharedFrom`), and its `colSpan` and `rowSpan` when merged; then its charts and pictures, a
  picture with its `sheet`. The re-save has its own truth wherever its cells differ, which is in
  every fixture with a formula, a boolean, or an error.

The pptx fixtures are in `generated/pptx/`:

- `<name>.python-pptx.pptx`: written by python-pptx on its default template, with what it has no
  API for set on its XML: a hidden slide's `show="0"`, the presentation's slide order, each
  paragraph's own bullet (`buNone`, `buChar`, `buAutoNum`), and the pictures' alternative text
  and title. Its charts are its own, with an embedded workbook and cached series.
- `<name>.libreoffice.pptx`: that file re-saved by LibreOffice.
- `<name>.truth.json` and `<name>.libreoffice.truth.json`: each slide in presentation order, its
  `number`, whether it is `hidden`, its elements in the docx truth's shape (a title is a level-1
  `heading`; a picture's `media` has its `slide`, the slide's index), and its `notes`' elements
  when it has notes. `absent` holds the layouts' and master's prompt texts and LibreOffice's
  notes-page slide-number field, which must appear nowhere.

| Fixture | Covers |
| --- | --- |
| `headings` | heading levels 1 to 4 from heading styles, a level-2 heading from a paragraph's outline level alone, paragraphs, a line break, and text Markdown would misread |
| `lists` | bullet lists nested three deep, an ordered list with a nested bullet list, a second ordered list, ordered under bullet under ordered, two ordered lists back to back (the second restarting its count) followed directly by a bullet list |
| `tables` | a header row, a cell of two paragraphs, a horizontally merged cell, a vertically merged cell, a pipe in a cell |
| `tracked-changes` | an insertion and a deletion in one paragraph, a move, a deleted paragraph mark joining two paragraphs, a deleted paragraph, an inserted paragraph, tracked changes in list items and in a table cell, a deleted paragraph mark right before a table |
| `sections` | three sections with even-page headers on: the first with its own default, first-page, and even-page headers and footers and a title page; the second with its own default header (holding a picture) and a first-page header but no title page, inheriting the rest; the third with a title page, inheriting everything; a tracked change, a table, and a list item in headers and footers |
| `headers-even-off` | first-page and even-page headers written but not shown: no title page, no even-page headers in the settings |
| `notes` | footnotes and endnotes referenced out of their id order, a note of two paragraphs, a reference in a list item, a footnote whose only reference was deleted |
| `comments` | a thread of a comment and two replies, a resolved comment anchored across two paragraphs with two paragraphs of text, a comment anchored to tracked text, with commentsExtended |
| `comments-plain` | a comment python-docx writes, with no commentsExtended part: no replies, unresolved |
| `media` | a picture on its own line, one between text, the first picture's bytes again through the same part and through a part of its own, an EMF, a linked picture, a deleted picture, one in a table cell, one in a second section |
| `charts` | a clustered bar chart of two series with a missing value, an untitled pie chart, a line chart |
| `cells` (xlsx) | text, rich text, numbers, booleans, errors; dates, datetimes, and times in default, built-in (14, 22, 20), and custom formats, among them quoted text and AM/PM; numbers in formats that aren't dates (`0.00%`, `#,##0`, and letters quoted, bracketed, and escaped); dates either side of the 1900 system's 29 February; a cell stored as an ISO date; a styled cell with no value; a sparse far corner |
| `formulas` (xlsx) | formulas whose results are a number, text, an error, a boolean, and a date by its format; a shared formula over three cells; an array formula over three |
| `sheets` (xlsx) | five sheets in an order that isn't their names': a hidden, an empty, and a very hidden one; merged cells across columns, rows, and both |
| `merges` (xlsx) | two merged ranges starting on row 1, one ending there and one on row 2, then one starting on row 4, with cells under and beside each once it has ended, so a range kept past its last row would claim a cell not its own |
| `drawings` (xlsx) | a bar chart over the sheet's cells; the same picture twice on one sheet; a second sheet's picture |
| `dates-1904` (xlsx) | dates and a datetime on the 1904 date system's first day (serial 0.5), and a plain number |
| `slides` (pptx) | five slides presented in an order that isn't their parts' (welcome, hidden, agenda, closing, questions), a hidden slide, notes of one and two paragraphs on three slides, a title slide's subtitle whose layout turns the master's bullets off, a text box, and a content placeholder left empty |
| `bullets` (pptx) | a content placeholder's master bullets nested three levels and back, a paragraph whose own bullet is none between lists, a numbered list with a bullet nested in it, a bullet list right after it at the same level; a text box's plain paragraph, its own bullets nested, its own number, an empty paragraph |
| `groups` (pptx) | a text box, a group of a text box and a nested group (a text box and a picture), and a text box placed above them all but last in the shape tree |
| `slide-objects` (pptx) | a table with a cell merged across columns and one across rows, a cell of two paragraphs, a pipe; a titled clustered column chart of two series with a missing value, with notes; the same picture on two slides beside another, with alternative text and a title |

What LibreOffice's re-save changes, checked by unzipping it:

- It writes a picture's title and description as one description, the title's line first, and
  no title (`media`, `sections`: their LibreOffice truth says so).
- It gives a section the default header and footer as its first-page ones where it should
  inherit a first-page variant through a section without a title page (`sections`), and writes
  an inherited header's copies with their deletions as plain text, so the fixture keeps its
  tracked header change in a header no other section inherits.
- It pairs footnotes with their references by their order in the notes part, not by id, so its
  first reference shows the first-defined note (`notes`).
- It keeps one part for the bytes the python-docx file stores twice, so only the python-docx
  file has two parts of the same picture; the bytes themselves, the EMF among them, it keeps as
  they are.
- It drops the deleted paragraph mark before the table, writing that text as a plain paragraph:
  the truth is the same, but only the python-docx file has the case.
- It keeps the charts' cached data, the notes (renumbering them, the separators too), the
  comments' replies and resolved state (in its own commentsExtended), and the linked picture.

And in an xlsx file, where openpyxl's file and LibreOffice's differ:

- openpyxl writes every string inline (`t="inlineStr"`), rich text as inline runs; LibreOffice
  writes them all as shared strings, rich text as shared runs. So each variant covers one kind.
- openpyxl writes a formula with an empty cached value (`<v></v>`); LibreOffice computes and
  caches every formula's value, the array formula's other cells' too, which openpyxl leaves out.
- LibreOffice writes a constant boolean as the formula `TRUE()` or `FALSE()` and a constant error
  as a formula of itself (`#N/A`), each with its cached value.
- LibreOffice expands the shared formula into each cell's own formula: only openpyxl's file has
  a shared formula, and LibreOffice writes none of its own.
- LibreOffice writes the ISO-date cell as its serial number in the General format, a number.
- LibreOffice writes the built-in date formats as custom codes (`m/d/yyyy` for 14), so only
  openpyxl's file has built-in date ids; it escapes literal characters (`yyyy\-mm\-dd`) and
  rounds numbers to 15 significant digits, which the times survive to the millisecond.
- openpyxl writes the chart's references with no cached data, so its chart has a title and one
  series with no name, categories, or values; LibreOffice caches them.
- openpyxl stores the twice-shown picture in two parts; LibreOffice keeps one. Either way it is
  one file.
- LibreOffice writes a very hidden sheet as hidden; both are `hidden`. It keeps the merges
  (writing the covered cells as empty styled cells), the 1904 date system, the sheet order, and
  the pictures' alternative text.

And in a pptx file, where python-pptx's file and LibreOffice's differ:

- python-pptx leaves bullets to inheritance: a content placeholder's paragraphs take the
  master's body style's bullets, a subtitle its layout's `buNone`. LibreOffice writes every
  placeholder paragraph's bullet, or `buNone`, on the paragraph itself, so only python-pptx's
  file has inherited bullets; both read the same.
- LibreOffice renames the slide parts in presentation order, so only python-pptx's file has
  parts out of order. It keeps the hidden slide, the notes, the group shapes, the table's
  `gridSpan`, `rowSpan`, `hMerge`, and `vMerge`, the empty placeholder (as an empty paragraph),
  and the pictures' descriptions in one part per content.
- LibreOffice drops a picture's title (`slide-objects`: its LibreOffice truth says so), writes
  each notes page's slide-number placeholder with a `<number>` field, writes a centered title as
  a title, and drops the chart's embedded workbook, keeping its caches.

## Regenerating

From the repository root, with LibreOffice's `soffice` on the path and no LibreOffice window open
on the same profile (the script uses a throwaway one):

```sh
mise run fixtures
```

That runs `uv run --script fixtures/generate.py`; the script's inline metadata pins python-docx,
openpyxl, and python-pptx, and `generate.py.lock` pins the rest. It rewrites every file of
`generated/`, or with fixture names as arguments (`uv run --script fixtures/generate.py notes
media`) only those fixtures' files, and the section below, which lists every fixture's files as
they are on disk. Both
writers stamp the time into the package, so a rerun changes a package's bytes and checksum
even when nothing else changed; regenerate only the fixtures a change needs.

<!-- generated: begin -->
LibreOffice: `LibreOffice 26.8.1.1 680(Build:1)`

| File | SHA-256 |
| --- | --- |
| `generated/docx/headings.python-docx.docx` | `e7ab7a872d563c7ed0d0d51902738643e5ddb705c9dd364b8f5d0aa54f366095` |
| `generated/docx/headings.libreoffice.docx` | `63586ce4480810a81c05c9b0109062be4c76dfce9e62a39641d31ddf58f22aa4` |
| `generated/docx/headings.truth.json` | `41b138b81b26fda92e03039891c3c0cde4d6b7119ecd27055e7782c4ff312050` |
| `generated/docx/lists.python-docx.docx` | `5d61af165027d3cad297a0dbaf2cc8b51ec2274aee648dd2398d2fdc9367e99b` |
| `generated/docx/lists.libreoffice.docx` | `261277388b4c59a885f5060a9db58ba96c3c128ea528a6a26dd0747870725ad6` |
| `generated/docx/lists.truth.json` | `9906d980e45f375953b730d772a325d3e3ec0b3ac7e2643915c6d795f19ead4c` |
| `generated/docx/tables.python-docx.docx` | `4bcc3b658b559c2d5c58a8a518ad5d6578056412218569b000f26aec9e5860b1` |
| `generated/docx/tables.libreoffice.docx` | `01116cf56ffbb4ad1f360272e2fabd8cd6e60d25e8081202f4b2d0aad91b7d2c` |
| `generated/docx/tables.truth.json` | `450b9f593887ff45a3eab9b6e5fe5d5b16f4aaa665cce1a03d1efb2b1a64df04` |
| `generated/docx/tracked-changes.python-docx.docx` | `d0e21769efbb2f8a83db3171e0443e8550023b415464459b9e0ab58672f63084` |
| `generated/docx/tracked-changes.libreoffice.docx` | `9378482803e88c832f61848c56f53b935ef336f3c443977c78800ddfb3bb73a3` |
| `generated/docx/tracked-changes.truth.json` | `92afe9096a8a90c0f3427bffeb2c2ad26889155b896b8b9b0ad003065f14e531` |
| `generated/docx/sections.python-docx.docx` | `ce66fb89cc609a7be34e066d038ded68a2c366242963c6f3d1e49f110b31f4aa` |
| `generated/docx/sections.libreoffice.docx` | `dbe71997cd80eec4639a4974a9694d4c658f2f11d5165f93ca9ce4c6b909df5b` |
| `generated/docx/sections.truth.json` | `4352fac9a72512e6bf9404df76ef5849dfb3724119e1ceabcc75986977be1546` |
| `generated/docx/sections.libreoffice.truth.json` | `0d0c48dc17ca489351161e2a2198dc918541804afdf649b55f9d1882d29714e4` |
| `generated/docx/headers-even-off.python-docx.docx` | `b1fb197b0b18968bec8b386c6c6c29b29446d7ed952e36a21314c6947ab3fb0c` |
| `generated/docx/headers-even-off.libreoffice.docx` | `2650103f4ce06530fd1b0e305d3c1ba1a7071633a3019ba0d442a959db5a2c3c` |
| `generated/docx/headers-even-off.truth.json` | `89f9f6313cbbec642faab90969fca2f9e2a474f97453c00a518f4c9ac9215712` |
| `generated/docx/notes.python-docx.docx` | `12482a309c45f1e871728f6fe9db5744a61dfca146f7ee60ebf0a263a12b397b` |
| `generated/docx/notes.libreoffice.docx` | `f9cafb26909d822de42cd4357b6b207719429fbafa301c405dc695f3457f61c6` |
| `generated/docx/notes.truth.json` | `f87b3fa9a7176774fb87d25c6ce30d6b72b36cdb81fe35af1989f053be9f667c` |
| `generated/docx/notes.libreoffice.truth.json` | `ebdee61e95c96e6f7b83d0d97ddc227a987d4b88acfa6db4ce24d11d675f5ee5` |
| `generated/docx/comments.python-docx.docx` | `afae352365b03bbdd0e90fbe0fb6804c1d2dde0d4d5de416f3fbdd3b33b87bcb` |
| `generated/docx/comments.libreoffice.docx` | `e683efab836f1e27e1abd65c58d34df37cdabb52d1214f2fabba28f5a0f5ef43` |
| `generated/docx/comments.truth.json` | `8aa467240b95417bc2beb097881d0d11266ffb88a5d8448d2de4f1a223d8efcb` |
| `generated/docx/comments-plain.python-docx.docx` | `f2fc1d900935d6882976a7e41506ee4abd339bd58c047902f96e6f8252fe0111` |
| `generated/docx/comments-plain.libreoffice.docx` | `c1d1f034522856494c7f9ee99cb3cdfe65011232a8fccde3a276db3b85836ee4` |
| `generated/docx/comments-plain.truth.json` | `b93eaf88d0ee175238b88aa1a75afc5df74f04d5784dcd0b6f313ba869a7f9cb` |
| `generated/docx/media.python-docx.docx` | `6cee5bc1fa49db7dbcf277282e3748c55f2f76828bcd846afb4c25eb0765d552` |
| `generated/docx/media.libreoffice.docx` | `07f3a8c5d58d9345fcbbf42b725ee32c92ed722bc9de31d5fad5f84169159af7` |
| `generated/docx/media.truth.json` | `5b7503aa78643dfc7e819fc3a8714ec6f9ee9b51d16f029c9d55d692391e8061` |
| `generated/docx/media.libreoffice.truth.json` | `7057fc6d2b01cb4778141b94321e5b46762f6aae805b16e0151c36e0a70015d6` |
| `generated/docx/charts.python-docx.docx` | `adda58a2b466af5c31018928b3e89517575244296f16df9b201d91b549312118` |
| `generated/docx/charts.libreoffice.docx` | `c49582d00fc63052e4c2a0bb04a64d8fd45933c39539a82434f9bbc7e24c0f0d` |
| `generated/docx/charts.truth.json` | `8c3df2d8aa2f02bfde11bdbbbf82a9642662d025119aaf40a45f282048af3ca1` |
| `generated/xlsx/cells.openpyxl.xlsx` | `09a8905b112b0ddec364654a37db011a91c2aeb01eb91dc69ff1af926b55b767` |
| `generated/xlsx/cells.libreoffice.xlsx` | `86f57558bc76ae5e344560c1db633bdc408782f5f241b5570a14a93429658f54` |
| `generated/xlsx/cells.truth.json` | `e640ad62b70fd036e556f32c565fd32d642aa634983faf79318f7a2a751f78aa` |
| `generated/xlsx/cells.libreoffice.truth.json` | `6ea8932d290b9b1035bce35a929dbd216ef2d7b63f9d3816961ae480c6183a0f` |
| `generated/xlsx/formulas.openpyxl.xlsx` | `808318c4cbc8e0fdf2c451c665707dd502ba768ff637d638009ce8882cb9a9d9` |
| `generated/xlsx/formulas.libreoffice.xlsx` | `00e94e40ef94156339a9d2cdbbf271bb2cd48069c93e534baa6e8aa3402dfd28` |
| `generated/xlsx/formulas.truth.json` | `85ab3414d53cc0614e46d8cd1ac47a9d86acdd7c058a313af90135c51b79a03d` |
| `generated/xlsx/formulas.libreoffice.truth.json` | `d0527e11129e873dc71a4e0de4262a7586862facc65300ec83ef82df67ae1714` |
| `generated/xlsx/sheets.openpyxl.xlsx` | `4f9ff886b58cb379dce3ae2543ca0ea22db3138a02ba40a4c53ccc8570c02844` |
| `generated/xlsx/sheets.libreoffice.xlsx` | `76aaac745885c3f8273d7b7a42667095138bb0177539473938cd098f42f49338` |
| `generated/xlsx/sheets.truth.json` | `8d16ccc52fcfb17001ce016205d76358f9fe6d28e17dadbc7382d35599b1d7c3` |
| `generated/xlsx/merges.openpyxl.xlsx` | `0c4c11659aadc740b1367bb0932d489e894ee7630995b662ef90a98ae45cf3cc` |
| `generated/xlsx/merges.libreoffice.xlsx` | `b5ba5ca9175ab45537835858f737198560888ca45ddc11df2a6022db013288e1` |
| `generated/xlsx/merges.truth.json` | `6459cacfc0f5e541c4b973dbb0934efb6cf5cb7f9e8dde67be389350062326ad` |
| `generated/xlsx/drawings.openpyxl.xlsx` | `cc1d815da48fe887b677eceba421c63b31d6911b9b50f87e526e3f4ef7045100` |
| `generated/xlsx/drawings.libreoffice.xlsx` | `9c48b55053519fecff0d4fe116a0c5da62f92fb5760bf5b0ac642229a4d4e923` |
| `generated/xlsx/drawings.truth.json` | `15d4349cd6df7254bb6fcc8c393661f510ff0dccd337ea70870b809b10ede855` |
| `generated/xlsx/drawings.libreoffice.truth.json` | `9d9142de5ae4415c4a7c017df1461fcedd3f23d32d4b6e39a041ee9a2bed21ae` |
| `generated/xlsx/dates-1904.openpyxl.xlsx` | `1b35da15738c50395bc484dffc0450381121b1204671adaaf8a23c3364e89df2` |
| `generated/xlsx/dates-1904.libreoffice.xlsx` | `2d7e9b1992ee47476404f0c5d279bcde64013579e31dad9812b5d22f2fdd6a99` |
| `generated/xlsx/dates-1904.truth.json` | `482d62d7d9fd3c8bf92d81e6d29ca3aab2912338f3d51d0fb7e653dff3e81fd9` |
| `generated/pptx/slides.python-pptx.pptx` | `fc9f5f4fb65f618489c0561fa1b6aa4c1395d815fc438535058d86711cbe47f0` |
| `generated/pptx/slides.libreoffice.pptx` | `956895b6d18ea6609ac03cbb8e723dd639b131beade1b4d2bde17da2ae71736b` |
| `generated/pptx/slides.truth.json` | `feca71bc485c7f545641188beb60ef4bef05a54c52e9b406574c32a15abc4635` |
| `generated/pptx/bullets.python-pptx.pptx` | `368cb19ff346a43e5b61440624effb435db23d7986ec160a5eb3d46e0b55f843` |
| `generated/pptx/bullets.libreoffice.pptx` | `b90c750a44e99340f0eb422337f85269bbd05ebbae99a38e8d0c48ee62f9af18` |
| `generated/pptx/bullets.truth.json` | `f0e86d26d97b4d7f998212e9a30516935a749fe5ea49d44b740a0831b2bf5835` |
| `generated/pptx/groups.python-pptx.pptx` | `7e8b0812be4623916fc2405c30af04f8e7989cc7a19d769b39a2e6ffb0dd5050` |
| `generated/pptx/groups.libreoffice.pptx` | `12c2fbb5418348021492f912227f590a70bf8bc0f25f91d6ed716bd12a7db0bb` |
| `generated/pptx/groups.truth.json` | `9d1fd65bf64bbe477837e058956471fb92c6411ad5409e7f41bf9c190d0b9515` |
| `generated/pptx/slide-objects.python-pptx.pptx` | `32514146bd061049ebdbe813375913454a131ad6c6fc714adc60020fabc39717` |
| `generated/pptx/slide-objects.libreoffice.pptx` | `7c0b5e40f22001166b9e4ee6f3b8e68a5c21fc6ea73e8ed77e15caffd4918995` |
| `generated/pptx/slide-objects.truth.json` | `e01a1d4068f68da865feefd2bc1f0acd25faa9eaee5164ed37102a3408a52cac` |
| `generated/pptx/slide-objects.libreoffice.truth.json` | `aca5bab816c442c3e83e98033bf66aea784d1fac2a8012db2241fe36f05f3952` |
<!-- generated: end -->
