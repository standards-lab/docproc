# The tool contract

Each tool runs as a process behind docproc's `tool` package, reads one input, and writes one
contract to stdout: JSON Lines, schema "2", with Markdown inside it as the text layer. Every piece
of text the input holds exists once, by the rule [`structured-text.md`](structured-text.md) states
and applies field by field.

## Common envelope

- **Lines.** stdout holds JSON Lines and nothing else, one compact object per line. Each line's
  `line` names its kind; the first line carries `schema` (`"2"`) and the input's kind. docproc
  decodes line by line, keeping a line, block, or container of an unknown kind as an `Unknown*`
  value.
- **Absence.** A field with no value, or an empty list, is left out, never `null`.
- **Text layer.** A line that holds text carries it as `markdown`: docproc-ooxml's `document`,
  docproc-mail's `message`, docproc-onenote's `page`. Literal text is escaped by htmd's rules, as
  the docproc-envelope crate's literal-text escape (`tools/envelope/src/markdown.rs`) writes them
  for the Rust tools and docproc-ooxml's escape matches: a backslash before each of
  ``\ * _ ` [ ] | ~ & $``, and before `<` when what follows could open an HTML tag, comment, or
  instruction (a letter, `/` then a letter or nothing, `!`, `?`, or the text's end); at a line's
  start, before a first `-`, `=`, `+`, `#`, or `>`, and before the `.` or `)` after one to nine
  digits. A link's or an image's destination and title read back as written under CommonMark,
  by the rules docproc-envelope's `destination` and `title` write and docproc-ooxml's
  `MarkdownRenderer` matches. Both escape each `\`, and each `&` that could begin a character
  reference (an optional `#`, ASCII letters and digits, then `;`: the envelope's
  `reference_like`). A destination holding whitespace, a control character, `<`, or `>` is
  written `<…>`, its `<` and `>` escaped; any other is written bare, its `(` and `)` escaped. A
  title also escapes `"`. A destination percent-encodes a line feed, a carriage return, or a NUL
  (`%0A`, `%0D`, `%00`), which no destination can hold; a title writes them as `&#10;`, `&#13;`,
  and U+FFFD. A `|` is left to a table cell's writer.
- **Media.** With `--media <dir>`, each distinct payload (picture, embedded file, OLE payload,
  attachment, ink SVG) is written once into `<dir>`, created if missing, as stored, before the
  line that names it. Its name is the lowercase hex SHA-256 of its bytes plus `.` and the stored
  extension, its case kept, when that is 1 to 8 ASCII letters and digits. A line names it in
  `file`, and the Markdown links it by that name. Without `--media` nothing is written to disk and
  the names stand. A linked or by-reference target is named, never fetched.
- **stderr.** One line each: `<tool>: warning: <where>: <what>`, and for exit codes 1 to 4
  `<tool>: error: <reason>`. `<where>` never holds `": "`, so a warning line splits at its first
  three. `<where>` and `<what>` are escaped: a backslash before a backslash, `\n` and `\r` for a
  line feed and a carriage return, and, in `<where>`, `:\ ` for a colon followed by a space;
  docproc inverts it. A `<reason>` never leads with the input path, which docproc already knows.
- **Refusal reasons.** One vocabulary for every tool: a `<reason>` beginning `encrypted` means the
  input is encrypted, and docproc-onenote's table-of-contents reason is fixed text
  ("docproc-onenote"); any other reason is prose for a person, and docproc reads nothing else
  from it.
- **Version.** `--version` writes the version alone on its first line and `schema <n>` on its
  second, exit 0, so a reader's spec refuses a tool of another schema at startup.
- **Exit codes.** 0 read, skipped items included; 1 stdout or media not written; 2 usage; 3
  refused before any output; 4 a fault of the tool's own.
- **Determinism.** The same input gives byte-identical stdout and media, proven by committed
  golden outputs (`.jsonl` and `.stderr` per fixture, as mail's `tests/golden/`); no JSON Schema.
- **Cases.** One committed table, `tools/envelope/cases.json`, holds the envelope's cases: the
  warning escape, a stderr line and the `where` and `what` it splits to, a payload's bytes and
  stored name and the media name they make, the exit codes' meanings, the literal-text escape,
  and the destinations and titles a target and a title make; docproc-ooxml's tests,
  docproc-envelope's, docproc-mail's, and docproc's `internal/cli`'s read it in place, so no
  language restates a case. docproc-mail's tests also read each destination and title back
  through pulldown-cmark.
- **Out of the tools.** Warnings as data are docproc's; per-item digests and provenance
  docinfer's; batch IDs Cerebro's; typed metadata docproc's and Cerebro's.

## docproc-ooxml

docproc-ooxml writes a `document` line first, then, per xlsx sheet in workbook order, a `sheet` line
and its `rows` lines, so no line holds a whole sheet:

```text
document  line, schema, format (docx|xlsx|pptx), properties?, markdown?, media?
properties  core?, app?, custom?: maps of the package's core, extended, and custom properties,
            each value typed as stored, by the rule below
media[]   kind (picture|embedded|ole), file?, content_type?, part?, target?, prog_id?,
          section? | sheet? | slide?   in document order
sheet     line, name, hidden?, markdown?   an xlsx sheet's header: its heading and its table's
                                          header row
rows      line, markdown?, rows[]: row, cells[]: ref, value_type?, value?, formula?,
          shared_from?, col_span?, row_span?   the sheet's next rows, in order, and their
          Markdown table rows; after the last sheet's, the comments' definitions
```

- `markdown` is the document's Markdown rendering: headings, lists, GFM tables, charts as tables of
  their cached series, notes and comments as GFM footnotes, slides as `# Slide n`. An xlsx's
  Markdown is split along its lines, and each sheet's text exists once: the `document` line holds
  the Markdown of no sheet, each `sheet` line its sheet's heading and its table's header row and
  delimiter row, and each `rows` line the Markdown table rows of the rows it carries, never the
  header row, which only the `sheet` line carries. A sheet's drawings, its charts' tables, its
  pictures' links, and its shapes' paragraphs, ride on a closing `rows` line of that sheet with `markdown` and no `rows`,
  bounded as any `rows` line is; a chart sheet is a `sheet` line and that closing line. The
  notes' and comments' footnote definitions follow the last sheet's lines on `rows` lines of their
  own, with `markdown` and no `rows`, each holding as many of the next definitions as keep it
  within 256 KiB, and at least one. The lines'
  Markdown fragments concatenate in order with nothing between them into the document's whole
  rendering: each ends in its own newline and opens with the blank line that separates it from the
  chunk before it. A picture's image text is its `alt`, or its `title` when it has no `alt`, so a
  title-only picture is `![title](file)`. A `title` that differs from the image text becomes its
  image's Markdown title, `![alt](file "title")`.
- `media` holds every picture the document shows, plus embedded packages (`embedded`, the part as
  stored) and OLE objects (`ole`, the part as stored, its compound file unwrapped by docproc,
  `prog_id` from the element naming its relationship). Each is found through its story part's,
  sheet's, or slide's own embedded-package and embedded-object relationships; a chart's embedded
  source workbook is never media. The Markdown links a payload as
  `[<prog_id, or the part's file name>](<file>)` after its preview picture. `part` is the package
  part, `target` a linked picture's URL; the placement is the docx section's index from 0, the
  ordinal from 1 of the xlsx sheet's `sheet` line, or the pptx slide's index from 0. A docx's media
  run in the order of its Markdown's chunks: each section's headers, the body, each section's
  footers, the footnotes, the endnotes, and the comments with their replies, nested blocks in place;
  a medium shown more than once is listed each time, with its own placement.
- `properties` is read from the three parts as XML. A core value is a string as stored, dates
  included; an extended value is a number when it is all digits, else a string; a custom value
  takes its variant's type, a file time as its stored ISO 8601 text. Keys keep their stored
  order, and a repeated key keeps its first value with a warning. A part that can't be parsed is
  a warning, never a refusal. The thumbnail is not media. The SDK's typed properties are never
  used, as they parse core dates and hide the stored text. A value is an element's text: an
  element with none, as an unset core property is written, or holding elements, as the extended
  heading pairs and titles of parts do, has no value and is left out, as is a custom property
  with no name or whose variant is a vector, an array, or empty. A custom variant's integer and
  real types are numbers and its boolean true or false, unless the text is no such value; any
  other type, file times and currency among them, is a string. An all-digits number drops its
  leading zeros, which a JSON number can't have.
- A sheet's `rows` lines keep its typed cells. Each holds the sheet's next rows, in order, with
  their Markdown, as many as keep the line within 256 KiB, and at least one, so a line is bounded by
  the larger of 256 KiB and one row, never by the sheet; a `sheet` line holds one row, its header,
  and is bounded the same way.
- Field names are snake_case, as mail's and onenote's are. Non-ASCII text is written as
  itself, never as a `\u` escape: the lines are data, never embedded in HTML.
- **Refusals.** The format is the main part's exact content type, the part the package's
  `officeDocument` relationship names, in Transitional's or Strict's namespace; any other, such
  as an xlsb's binary workbook, is none of the three, whatever the file's name. An OLE compound
  file is an encrypted package when its root storage holds an `EncryptedPackage` stream
  ([MS-OFFCRYPTO] 2.3.4.4), names compared without regard to case as [MS-CFB] compares them, and
  a legacy binary document otherwise. Only the compound file's directory is read, each step
  bounded by the file's length, so a cycle or a reference past its end is malformed, never a
  loop. The SDK loads a package's parts the first time any is asked for, failing then on a
  relationship to a part the package lacks, so they are loaded once on opening and that failure
  refuses the package as malformed. A reason is escaped as a `<what>` is, so one holding a line
  break, an echoed argument or an exception's message, stays one line.

The model the lines are written from is one tree for the three formats, its blocks in document
order, every block but a media block the document's text, complete without inference. Each
format is read as below; a warning's `<where>` is a docx `section n` or `comments`, an xlsx
`sheet n` (its `sheet` line's ordinal, or for a sheet with no line its ordinal in the workbook),
or a pptx `slide n`, and the unread kinds (equations and SmartArt) are warned of once each,
where the document first holds one
([`design-review.md`](design-review.md), OX-11).

- **docx: the text.** Tracked changes are read accepted: insertions and a move's destination are
  read, deletions and a move's source skipped, so the runs are walked rather than read through
  the SDK's `InnerText`, which holds `w:delText`. A paragraph whose mark was deleted or moved runs
  on into the next, which keeps its own properties; a table ends that, the carried text a
  paragraph of its own before it. Paragraphs, tables, rows, and cells are read through the
  content controls, custom XML, and smart tags that wrap them (the SDK has no smart-tag type, so
  one is read as the custom XML it is like). A content control showing its placeholder holds a
  prompt (Word's "Choose an item."), so its runs are left out wherever it is. Field codes are
  instructions, not text; a field's result is ordinary runs. A ruby's base text is read and its
  annotation left out. An external hyperlink's text links to its target; an internal one's, to a
  bookmark, stays text. Empty paragraphs and headings are spacing and left out; an empty list
  item is kept. A paragraph's pictures, charts, and text boxes follow it as blocks in the order
  they occur, inside a list item after its paragraph.
- **docx: headings and lists.** A heading's level is the paragraph's own outline level or else its
  style chain's (each style's `basedOn`, nearest first, a cycle ending it; a paragraph with no
  style has the default paragraph style): an outline level, or a style named `heading N`; outline
  level 9 is body text. A list paragraph's numbering instance and level come from its own
  `w:numPr` or its style's, within Word's nine levels; instance 0 removes numbering, and an
  instance or definition that doesn't exist leaves the paragraph out of any list. A definition
  with only a `numStyleLink` takes its levels from the numbering its style names. A level
  formatted as a bullet or as nothing, or missing, is unordered; one with no format is decimal.
  A paragraph is numbered as Word counts: one more than the last number of its level in its
  abstract definition, which every instance of the definition shares, or else the level's start
  (0 when unset); an instance that overrides the level's start sets it the first time it numbers
  that level, and numbering a level restarts the levels below. Consecutive list paragraphs make
  up lists: a deeper level opens a list inside the last item, and at the same level an item
  continues the list unless its orderedness differs, it belongs to another abstract definition,
  or it moves to an instance that overrides the level's start. A list starts at its first item's
  number, so one a paragraph interrupted continues the count.
- **docx: tables.** A horizontal merge is a cell's `w:gridSpan`; a vertical merge is a `w:vMerge`
  restart cell and the continue cells below it, folded into its row span, so a covered position
  has no cell. A row that starts late (`w:gridBefore`) gives its first cell the columns it skips;
  one that ends early needs nothing. A table nested more than eight deep refuses the document,
  as it is reached, naming the story and the number from 1 of the first paragraph at or after it.
- **docx: drawings and objects.** A DrawingML drawing's pictures and charts are read in document
  order with the drawing's description and title, a picture's blip embedding a part or linking a
  target. A VML picture's title is its alternative text, VML having no other. A VML object's payload
  is its `o:OLEObject`'s, or a Word 2010 `w:objectEmbed`'s, relationship. A chartEx chart, in an
  `mc:AlternateContent`, is its cached data, or with none cached its fallback's picture, never both.
  A text box, DrawingML's `wps:txbx` or VML's `v:textbox`, is its `w:txbxContent`'s blocks, read
  as the body's are; one inside another is the outer one's content, and a drawing's pictures and
  objects are looked for outside its text boxes, so nothing in a text box is read twice. Any other
  alternate content is read for its text boxes alone, from the first choice that holds one or else
  the fallback, so a text box Word writes as DrawingML with its VML copy as the fallback is read
  once. A SmartArt diagram and an equation are not read, wherever they are.
- **docx: sections, notes, and comments.** A section is a paragraph's `w:sectPr`, the body's own
  the last. Each section's headers and footers are those its pages show: its own, or the previous
  section's where it has none of a kind; a first-page one counts only with a title page and an
  even-page one only when the settings turn even and odd headers on, and one with no blocks is
  left out. Each is read for every section that shows it, so its media are placed in that
  section. A reference of a type no header or footer has (Word's are default, first, and even) is
  skipped with a warning. A footnote or endnote is read when the text first references it, a
  note a note references after it; Word's separator and continuation notes are not the text's.
  A comment whose `commentsExtended` entry, its last paragraph's, names a parent is a reply in
  its root's thread, and a thread is resolved when its root's entry is done; without
  `commentsExtended` each comment is a thread of its own, unresolved. A thread's anchor is the
  accepted text of its range, paragraphs joined by a newline; it is marked where the range ends,
  or where its reference is when the range ends between paragraphs or there is none.
- **xlsx: sheets.** A sheet whose reference names no part (an empty or unknown relationship id) is
  skipped with a warning and the others read. A worksheet holds its cells, then its drawing, then
  its objects; a chartsheet its drawing alone; a dialog or macro sheet nothing read. Each
  worksheet part is read in two forward passes, one row held at a time
  ([`design-review.md`](design-review.md), OX-2): the first finds whatever refuses the workbook
  and collects what the `sheet` line needs before the rows, which a forward reading meets after
  the cells (the merges, the external hyperlinks, and a shared formula's anchor, which can follow
  a cell that names it); the second gives the rows as they are written. Only the worksheet's own
  children count: a row inside alternate content is passed over. A sheet is refused with the
  failure a reading of the whole part meets first: its XML, then a shared formula's attributes,
  then the first failing row number or cell, in order, that no merge covers.
- **xlsx: cells.** A row's number is as stored or else one past the row before; a cell's
  reference is optional, and one without follows the cell before it. A table holds the cells
  with a value or a formula, and the cells a note or comment anchors, whether the part holds them
  or not, such a cell with nothing else its `ref` alone. A merged range's anchor, its top-left cell, carries its spans, and
  the cells it covers are left out; a range whose anchor holds nothing leaves nothing. Excel
  refuses overlapping ranges; where a file holds them, a cell is looked for only in the covering
  range that starts nearest its left. A shared formula's text is on its anchor, the first cell
  whose formula holds it, and a cell holding only a shared index is kept when the index has an
  anchor. A cell's external hyperlink is the first of the sheet's whose range holds it; one to a
  place in the workbook stays text.
- **xlsx: values.** A cell's value is a shared or inline string, a formula's string, a boolean, an
  error, an ISO date, or a number, a date when its style's number format shows a date or a time.
  An empty value is none, as openpyxl writes a formula it has not computed, but a formula's
  string can be empty. A shared string's index may carry white space around it, as Excel Online
  writes `<v>2 </v>` and Excel reads it. Rich text is flattened, its phonetic runs (`rPh`) left
  out, and `_xHHHH_` escapes (ECMA-376 Part 1, 22.9.2.19, `ST_Xstring`) are decoded left to right
  to their UTF-16 code units, `_x005F_` the escaped underscore, whose own underscore never starts
  another. The built-in formats 14 to 17 are dates, 18 to 21 and 45 to 47 times, and 22 both;
  the other built-in ones, the locale-dependent among them, leave the number raw. A custom
  format's date and time parts are its tokens outside quoted text, escaped and skipped
  characters, and brackets, an `m` being minutes after an hour or before a second and months
  otherwise; a format with an elapsed-time bracket (`[h]`, `[m]`, `[s]`) is a duration, which can
  pass a day, so its value stays a number. In the 1900 date system serial 1 is 1 January 1900
  and serial 60 is 29 February 1900, a day that never was, kept from Lotus 1-2-3; in the 1904
  system serial 0 is 1 January 1904. Times are rounded to the millisecond.
- **xlsx: drawings and objects.** A drawing's pictures, charts, and shapes' text are read in the
  order it anchors them, through groups, each picture with its own drawing properties'
  alternative text and title; a chartEx chart is read as in a docx, and any other alternate
  content is read as its first branch, the choices in order and then the fallback, that holds
  anything read, so a shape written twice is read once. A shape's text, a text box's among them,
  is its paragraphs and lists, read as a pptx text box's with its own list style alone. A SmartArt
  diagram is not read. A worksheet's OLE object is
  its preview picture (its `objectPr`'s), then its payload: Excel writes the object twice in
  alternate content, the preview only in the choice's copy, both naming one relationship.
- **xlsx: notes and comments.** A worksheet's threads are its legacy comments part's notes, first,
  in stored order, then each threaded comments part's threads. A note (`comment`, its author
  named by its part's `authors`) is its rich text flattened as a cell's, one paragraph, a thread
  of its own, open, notes having no state. Excel writes a note on each threaded comment's cell as
  that comment's copy for readers without threaded comments, its text beginning
  `[Threaded comment]`, so a note on a cell a threaded comment anchors is skipped, whatever its
  text. A threaded comment (`threadedComment`, its author the workbook persons part's person its
  `personId` names) is its text, one paragraph; it is a reply in the thread of the comment its
  `parentId` names, earlier in the sheet, and a thread of its own otherwise, resolved when its
  `done` is set. A thread anchors its cell, or for a cell a merged range covers the range's
  anchor; one whose reference is no cell anchors nothing. Ids number roots and replies from 1 in
  reading order across the workbook.
- **pptx.** Slides are read in presentation order, hidden ones included and marked, each slide's
  shapes in its shape tree's order, back to front, never re-sorted by position, a group in place. A
  title placeholder's text is the slide's heading, its paragraphs its lines; other shapes'
  paragraphs are paragraphs and lists. A placeholder holds only the slide's own text, so an empty
  one is left out and its layout's prompt never read. A placeholder with no type is a content
  placeholder. A slide's placeholder takes after its layout's, and that one after its master's, of
  the same index, or else the first of its type, a centered title matching a title; a layout
  placeholder's kind on the master is title, date, footer, slide number, or header, or else body. A
  paragraph's bullet is its own or else the first its list styles give for its level, nearest first:
  the shape's own; for a placeholder its layout's and master's placeholders' and the master's title,
  body, or other text style by its type; for any other shape the master's other text style; and last
  the presentation's default. A character, picture, or auto-number bullet makes a list item, an
  auto-number ordered from its `startAt` (1 by default); none, or `buNone`, makes a paragraph. Empty
  paragraphs are left out. Lists group as a docx's do by level and orderedness, and a paragraph ends
  every list. A run's own `a:hlinkClick` links its text; a link to a slide or an action stays text.
  Content in `mc:AlternateContent` is not read but for a chartEx chart's, read as in a docx; an OLE
  object's frame is its preview picture, then its payload, PowerPoint writing the object twice with
  the preview only in the fallback's copy. A notes page's text is its body placeholders' paragraphs,
  never the slide image, number, header, footer, or date, their bullets from their own list style
  and the notes master's.
- **pptx: comments.** A slide's comments are its legacy comments part's, first, in stored order,
  then each modern comments part's. A legacy comment (`p:cm`, its author named by
  `commentAuthors.xml`) is its `p:text`, one paragraph; it is a reply in the thread of the comment
  its PowerPoint 2013 threading extension (`p15:parentCm`) names by author and index, earlier on
  the slide, and a thread of its own otherwise, open, legacy comments having no state. A modern
  comment (`p188:cm`, its author named by `authors.xml`) is a thread, its text body read as a text
  box's paragraphs and lists with its own list style alone, its reply list's replies in stored
  order, and resolved when its status is `resolved`. A thread anchors no text: its place is its
  slide.
- **Charts.** A chart is its part's cached data, never its rendering or its embedded workbook. A
  DrawingML chart, Transitional or ISO 29500 Strict (its namespaces renamed to Transitional's),
  takes its type from its first plot's kind; a chartEx chart, which Word and PowerPoint refer to
  by a `cx:chart` the SDK doesn't type, takes its first series' layout, and with no point cached
  anywhere is not a chart. A title is its own rich text or a cell's cache; with neither it is the
  application's to make up, so it has none. A series' categories are its `cat` or, for scatter
  and bubble, its `xVal`, and a multi-level cache gives its innermost level. A chartEx series'
  categories are its data's first text dimension or else its x values, and its values the first
  numeric dimension that isn't x values (a sunburst's or treemap's sizes, a region map's colors);
  a hierarchy labels each point with its innermost level that names it. Points are placed by
  index up to the declared count, a point the cache lacks null, and a count beyond any point is
  capped, so a malformed count can't exhaust memory.
- **The Markdown.** Footnotes and endnotes share one label count in the order the rendering
  reaches their references (`[^1]`, `[^2]`), so their ids never collide; comments have their own
  (`[^c1]`). A note no reference reached is labeled after the rest. A definition's first
  paragraph is on its label's line, the rest indented four spaces after a blank line. Each
  comment is its author, its thread's state, its anchor, its text, and its replies; a pptx's
  comments, which anchor no text, are marked after their slide's heading, `# Slide 2[^c1]`, so
  their labels count in slide order, and an xlsx's after their cell's text in its table cell,
  `| 2 | Rent[^c1] |`, so they count in sheet, row, and column order, a thread that anchors no
  cell labeled after the rest. A list that follows another of its kind switches markers
  (`-` to `*`, `.` to `)`), since Markdown would join them; an item's other blocks are indented
  to its marker's width, a nested list tight. A line break in a paragraph is a hard break, a
  backslash at the line's end; each line is trimmed, since leading spaces make an indented code
  block, each text run escaped on its own, and a line left empty dropped. A hyperlink's text
  keeps to one line, and one with no text is nothing. A docx table is a GFM table, its first
  row the header, a merged cell's text in its top-left
  position and the positions it covers or a late row skips empty, a cell's lines and blocks
  joined by `<br>`; a chart in a cell renders after the table, its title left in the cell. A
  sheet is a heading of its name, marked when hidden, then a table headed by the column letters
  its cells use, each row under its number, columns and rows with no cells left out; a cell shows
  its value (a number in its shortest round-trip form, a boolean as `TRUE` or `FALSE`), or else
  its formula as a code span, or a shared formula's anchor, a code span's fence longer than any
  backtick run in it and its pipes escaped. A chart is its title, then a table with a column per
  series and a row per category, the first series' categories naming the rows. A slide is a
  heading of its number, marked when hidden, its own headings a level below, then its notes after
  a bold "Notes" label, which no escaped slide text can make.

## docproc-mail

The `store`, `folder`, and `skipped` lines are as [tools/mail/README.md](../tools/mail/README.md),
"Output", states; a `folder` holds no `display_name`, which would duplicate its `path`'s last name
(58 of 58 folders in the run [`payload-measurement.md`](payload-measurement.md) measures). A
message's line:

```text
message   line, id?, folder?, class?, subject?, sender?, to?, cc?, bcc?, submit_time?,
          delivery_time?, creation_time?, modification_time?, importance?, sensitivity?,
          transport_headers?, message_id?, in_reply_to?, references?, conversation_topic?,
          conversation_index?, markdown?, attachments?, media?
address   name?, address?, address_type?, smtp_address?
attachment  file?, filename?, mime_type?, content_id?, inline, method?, size?, message?
media[]   file, mime_type?   a payload the Markdown links that no attachment names, in the
                             Markdown's order
```

- `markdown` is the message's Markdown; its stored text, HTML, and RTF bodies are not written
  ([`payload-measurement.md`](payload-measurement.md) measures them). The Markdown is made from the
  first of these the message holds: its stored HTML, the HTML recovered from an RTF that
  encapsulates it (`\fromhtml`), its text body, then the RTF's text, read as below and made Markdown
  as a text body is. An HTML nested deeper than 512 open elements gives way to the text body, else
  to the HTML's text with its tags stripped, with a warning. Markdown made from HTML, stored or
  recovered from the RTF, is the HTML's alone: nothing of the text body or the RTF's text is
  appended to it, as the parts of a `multipart/alternative` are each an alternative version of the
  same information (RFC 2046 §5.1.4). The RTF's text is read only for a message with neither HTML
  nor a text body, by the RTF specification: the body decompressed, control words and ignorable
  groups (`\*`, the font, color, and style tables) stripped, `\par` and `\line` as line breaks,
  `\tab` as a tab, a table's `\cell` and `\nestcell` as a tab, `\row`, `\page`, and `\sect` as line
  breaks, a nested table's `\nonesttables` fallback skipped, symbol words (`\rquote`, `\emdash`, …)
  as their characters, and `\'hh` decoded in the `\ansicpg` code page. An RTF that encapsulates HTML
  (`\fromhtml`) has that HTML's text.
- In Markdown made from HTML, the text's `&`s, and each link's and image's destination, title,
  and alt text, read back as the HTML holds them. The tool converts the HTML with htmd 0.6 over
  its default DOM, ego-tree. A pass over htmd's output, `bare`, removes each escape htmd 0.5.6
  didn't write unless removing it changes how CommonMark reads the text, so a text `&` is written
  `\&` where it could begin a character reference and bare elsewhere. The pass leaves code, math,
  raw HTML, and link destinations and titles as written. The tool's own handlers write table
  cells, so a heading in a cell stays a heading, and links and images, in htmd 0.6's form but
  through the envelope's destination and title rules, an alt text's lines through the
  literal-text escape. `tools/mail/src/markdown.rs` holds the rules, and the tool's CHANGELOG,
  v0.1.2, lists where its output differs from htmd 0.5.6's.
- The content rule compares a source text with a target text by standards alone, so no fixture tunes
  it; docproc-onenote's OCR text follows it, and docproc-mail, whose Markdown appends nothing,
  doesn't. It is written once, in the docproc-envelope crate at `tools/envelope` (`src/content.rs`),
  which docproc-onenote depends on. Its Unicode version, 18.0.0, the one its exactly pinned ICU4X
  2.4.0 crates carry, is part of schema "2": the normalization, the word and sentence boundaries,
  and the general categories are that version's, a unit test asserts it, and another version is a
  change of meaning, so a crate bump that moves it fails the check and becomes a reviewed change,
  the goldens re-recorded and the CHANGELOG naming the version.
  - **Plain text.** Both are normalized to Unicode NFC.
  - **URLs.** In plain text, a URL is a `<scheme:…>` in angle brackets, its whitespace removed
    (RFC 3986 appendix C), or a GFM extended autolink (`www.`, `http://`, `https://`, with the
    extension's domain and trailing-punctuation rules, a `www.` link taking `http:`). URL tokens
    compare after RFC 3986 §6.2.2 syntax-based normalization: scheme and host case-insensitive,
    percent-encoding normalized, dot segments removed.
  - **Words.** The rest splits at Unicode word boundaries (UAX #29), and a segment holding a
    letter or digit (general categories L* and N*) is a word token; marks and format characters
    stay inside their word (WB4). The comparison is of words, and symbols, punctuation, and
    controls are not words. Word tokens compare case-sensitively.
  - **Alignment.** A longest common subsequence of the two token sequences. The common prefix
    and suffix are trimmed, and Myers' O((n + m)·D) algorithm with its linear-space refinement
    (E. W. Myers, "An O(ND) Difference Algorithm and Its Variations", Algorithmica 1, 1986)
    aligns the rest, for n source and m target tokens and D edits between them. Where several
    longest common subsequences tie, the rule's is the one that algorithm returns. The shared
    crate is the rule's one normative implementation, pinned by the goldens, so every tool
    following it appends the same sentences.
  - **Units.** The source's sentences by Unicode sentence boundaries (UAX #29), a line break also
    ending one. Each sentence holding a token outside the alignment is appended, in order; the
    rest is already in the target, so overlap is never written twice.
  - **Bound.** When D passes 10,000 edits, the alignment stops, and a source sentence is appended,
    in order, when its word-token sequence is not that of any sentence of the target, split the same
    way: a comparison by set membership, linear in the tokens, so the rule's cost is bounded for any
    input.
- `smtp_address` is an Exchange (`EX`) address's SMTP address: a recipient's from its
  `PidTagSmtpAddress` (0x39FE), the sender's from the message's `PidTagSenderSmtpAddress` (0x5D01).
  It is left out when the input doesn't hold it, and docproc's typed metadata then takes the
  sender's SMTP address from `transport_headers`' From. Of the committed fixtures, only Tika's
  `testPST_variousBodyTypes.pst` holds a sender's.
- `inline` means the Markdown links the attachment, in every input format. Attachments are read
  before the Markdown is made, a nested message's included, and the HTML's image sources are
  rewritten to their media names: a `cid:` source, percent-decoded, matches an attachment's
  `content_id`, and any other source its Content-Location (`PidTagAttachContentLocation`, 0x3713,
  in MAPI), read for matching only. A `data:` image is written as media and linked by its name,
  with no attachment record, and listed in the message's optional `media`, an addition under
  "2", so every file a tool writes is named by a line. A source that matches nothing is kept as
  written, with a warning.
- The contract has two limits: a `.pst` attachment of `method` `embedded_message` has no `message`
  and no `file`, since the reader can't open it, and no class-specific field is written, so a
  contact, an appointment, or a task is a `message` line with the message fields alone. Filling
  either is an addition under "2".
- `--media <dir>` names the directory a line's files are written to, and `--after <id>` resumes a
  read after the message whose `id` is given.

## docproc-onenote

The `notebook`, `section_group`, `section`, and `skipped` lines are as
[tools/onenote/README.md](../tools/onenote/README.md), "Output", states, a `skipped` line carrying
an optional `encrypted: true` (below); a `section_group` or `section` holds no `display_name`, which
would duplicate its `path`'s last name (16 of 16). A page's line:

```text
page      line, id, section, level, title?, creation_time?, modification_time, author?,
          markdown?, media?, ink_alternatives?
media[]   kind (image|embedded_file|ink), file?, filename?, size?, extension?, type?
          in the reader's stored order
ink_alternatives[]  word (its index among the page's recognized ink words, from 0), alternatives[]
                    (strings, best first)   one per word the recognizer gave alternates
```

- `content` (containers, outlines, paragraphs, runs, tables, tags), the page's `ink` record, and
  `ink_text` are cut: their text is in `markdown`, and the media they hold move to `media`.
- The Markdown gains the text the spike reads but leaves out of it. The title's date and time
  paragraphs (`PageDateTime`) follow the title heading as one line, the paragraphs joined by one
  space. An image's `ocr_text` sentences that its `alt_text` lacks, by the content rule above,
  follow the alt text in the image's Markdown text; OneNote's own alt text holds most of it
  (4,926 of 5,572 B). An image's `hyperlink` makes the image a link, `[![alt](file)](hyperlink)`.
  Every tag's label is written the same way, as `[Label]` before its item's text, a task tag's
  after its task box (`- [ ] [To Do] …`): the box carries the task state, not the label's
  words.
- `ink_alternatives` is a label kept as a plain-text field: the recognizer's alternates, which the
  Markdown leaves out; the words they belong to are the ink images' alt texts in the Markdown
  (`![ink: <words>](<file>.svg)`), in the same order. Each drawing, the page's ink, a
  paragraph's, or an ink block, takes as its alt text the words its own strokes belong to, in the
  recognition's order, and `word` indexes the page-wide list over the recognition's lines. A
  recognised word no drawing carries is written once, as one plain line after the page's content,
  so every word is in the Markdown.
- `creation_time` is left out when it is the parser's 1970 fallback for an unreadable time, with
  a warning. A page with no stored modification time reports its creation time as
  `modification_time`, as the parser gives it. An untitled page's `title` is its first line, as
  OneNote's page list shows it.
- docproc reads sections and packages, one file per part, and always passes `--file-only`: under
  it a table of contents (`.onetoc2`) is refused, exit 3, with the fixed reason "a table of
  contents; its sections are read on their own", before any section is read. Without it, a table
  of contents or a notebook directory is read whole, an operator's mode, and a notebook read that
  reads no entry warns at `/`.
- A section that fails to parse and holds [MS-ONESTORE] 2.5.19's encryption-key header and footer
  constants (`0xFB6BA385DAD1A067`, `0x2649294F8E198B3C`) is encrypted: as the input it is refused,
  exit 3, with a reason beginning `encrypted`, and inside a notebook its `skipped` line carries
  that reason and `encrypted: true`.

## Versioning

Each tool's schema, "2", grows by addition only (an optional field, a line or media kind); a
removal, rename, or change of meaning moves it to the next version.

