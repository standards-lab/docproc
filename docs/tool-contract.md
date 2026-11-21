# The tool contract

Each tool runs as a process behind docproc's `tool` package (processing `tool/tool.go`), reads one
input, and writes one contract to stdout: JSON Lines, schema "2", with Markdown inside it as the
text layer. Every piece of text the input holds exists once, by the rule
[`structured-text.md`](structured-text.md) states and applies field by field.

## Common envelope

- **Lines.** stdout holds JSON Lines and nothing else, one compact object per line. Each line's
  `line` names its kind; the first line carries `schema` (`"2"`) and the input's kind. docproc
  decodes line by line, keeping a line, block, or container of an unknown kind as an `Unknown*`
  value, as processing's `office/model.go`, `email/model.go`, and `onenote/model.go` do.
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
  refused before any output; 4 a fault of the tool's own. All three spikes use these
  meanings (office, mail, and onenote READMEs, "Usage").
- **Determinism.** The same input gives byte-identical stdout and media, proven by committed
  golden outputs (`.jsonl` and `.stderr` per fixture, as mail's `tests/golden/`); no JSON Schema.
- **Cases.** One committed table, `tools/envelope/cases.json`, holds the envelope's cases: the
  warning escape, a stderr line and the `where` and `what` it splits to, a payload's bytes and
  stored name and the media name they make, the exit codes' meanings, the literal-text escape,
  and the destinations and titles a target and a title make; docproc-ooxml's tests,
  docproc-envelope's, and docproc's `internal/cli`'s read it in place, so no language restates a
  case. docproc-envelope's tests also read each destination and title back through
  pulldown-cmark.
- **Out of the tools.** Warnings as data are docproc's; per-item digests and provenance
  docinfer's; batch IDs Cerebro's; typed metadata docproc's and Cerebro's.

## docproc-ooxml

From spike-ai-pipeline-office 0.1.0 (`src/SpikeAiPipeline.Office/Model/Document.cs`, README "The
JSON model"), which writes either the model (indented JSON) or Markdown, never both. docproc-ooxml
writes a `document` line first, then, per xlsx sheet in workbook order, a `sheet` line and its
`rows` lines, so no line holds a whole sheet:

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
          Markdown table rows
```

- `markdown` is office's Markdown rendering (`Rendering/MarkdownRenderer.cs`, the goldens
  `test/SpikeAiPipeline.Office.Tests/Expected/*.md`): headings, lists, GFM tables, charts as tables
  of their cached series, notes and comments as GFM footnotes, slides as `# Slide n`. An xlsx's
  Markdown is split along its lines, and each sheet's text exists once: the `document` line holds
  the Markdown of no sheet, each `sheet` line its sheet's heading and its table's header row and
  delimiter row, and each `rows` line the Markdown table rows of the rows it carries, never the
  header row, which only the `sheet` line carries. A sheet's drawings, its charts' tables and its
  pictures' links, ride on a closing `rows` line of that sheet with `markdown` and no `rows`,
  bounded as any `rows` line is; a chart sheet is a `sheet` line and that closing line. The
  lines' Markdown fragments concatenate in order with nothing between them into office's whole
  rendering: each ends in its own newline and opens with the blank line that separates it from
  the chunk before it. A picture's image text is its `alt`, or its `title` when it has no `alt`
  (`MarkdownRenderer.cs`, `media.Alt ?? media.Title`), so a title-only picture is
  `![title](file)`. A `title` that differs from the image text, which office leaves out, becomes
  its image's Markdown title, `![alt](file "title")`.
- `media` holds every picture office writes (`Media` blocks, `Reading/MediaStore.cs`), plus
  embedded packages (`embedded`, the part as stored) and OLE objects (`ole`, the part as stored,
  its compound file unwrapped by docproc, `prog_id` from the element naming its relationship),
  which office leaves unextracted (README "What office does not do"). Each is found through its
  story part's, sheet's, or slide's own embedded-package and embedded-object relationships; a
  chart's embedded source workbook is never media. The Markdown links a payload as
  `[<prog_id, or the part's file name>](<file>)` after its preview picture. `part` is the
  package part, `target` a linked picture's URL; the placement is the docx section's index from
  0, the ordinal from 1 of the xlsx sheet's `sheet` line, or the pptx slide's index from 0.
- `properties` is read from the three parts as XML. A core value is a string as stored, dates
  included; an extended value is a number when it is all digits, else a string; a custom value
  takes its variant's type, a file time as its stored ISO 8601 text. Keys keep their stored
  order, and a repeated key keeps its first value with a warning. A part that can't be parsed is
  a warning, never a refusal. The thumbnail is not media.
- A sheet's `rows` lines keep its typed cells, office's `TableCell` less its docx-only fields.
  Each holds the sheet's next rows, in order, with their Markdown, as many as keep the line
  within 256 KiB, and at least one, so a line is bounded by the larger of 256 KiB and one row,
  never by the sheet; a `sheet` line holds one row, its header, and is bounded the same way.
- Field names move to snake_case, as mail's and onenote's are.

## docproc-mail

From spike-ai-pipeline-mail 0.3.1 (`src/model.rs`, README "Schema \"1\"", goldens
`tests/golden/{msg,pst,generated}/`; `.eml` and mbox scored against
`tests/fixtures/mime/truth.json`). Lines `skipped` and `store` keep their schema "1" fields, the
store's `display_name`, a .pst's name and a label, among them; `folder` keeps them less
`display_name`, which duplicates its `path`'s last name (58 of 58 folders in the run
[`payload-measurement.md`](payload-measurement.md) measures). A message changes:

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

- `markdown` is schema "1"'s `body.derived.markdown`, moved up; `body` and its `text`, `html`,
  `rtf`, and `derived.html` are cut (measured in
  [`payload-measurement.md`](payload-measurement.md)). The Markdown is made from the first of these
  the message holds: its stored HTML, the HTML recovered from an RTF that encapsulates it
  (`\fromhtml`), its text body, then the RTF's text, read as below and made Markdown as a text body
  is. An HTML nested deeper than 512 open elements gives way to the text body, else to the HTML's
  text with its tags stripped, with a warning. When the Markdown is made from HTML, stored or
  recovered from the RTF, the tool appends the sentences of the message's source text that the
  Markdown lacks, by the content rule below, in order, as a paragraph escaped as a text body's
  Markdown is. The source text is the text body; a message with an RTF body and no text body has the
  RTF's text, read by the RTF specification: the body decompressed, control words and ignorable
  groups (`\*`, the font, color, and style tables) stripped, `\par` and `\line` as line breaks,
  `\tab` as a tab, symbol words (`\rquote`, `\emdash`, …) as their characters, and `\'hh` decoded in
  the `\ansicpg` code page; nothing separates a table's cells. An RTF that encapsulates HTML
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
- The content rule compares a source text with a Markdown text by standards alone, so no
  fixture tunes it; docproc-onenote's OCR text follows it too. It is written once, in the
  docproc-envelope crate at `tools/envelope` (`src/content.rs`), which docproc-mail and
  docproc-onenote depend on. Its Unicode version, 18.0.0, the one its exactly pinned
  ICU4X 2.4.0 crates carry, is part of schema "2": the normalization, the word and sentence
  boundaries, and the general categories are that version's, a unit test asserts it, and another
  version is a change of meaning, so a crate bump that moves it fails the check and becomes a
  reviewed change, the goldens re-recorded and the CHANGELOG naming the version.
  - **Plain text.** Both are normalized to Unicode NFC. The Markdown is read by CommonMark's
    semantics: a link as its text followed by its destination as one URL token, an image as its
    alt text followed by its destination, emphasis and backslash escapes removed.
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
    aligns the rest, for n source and m Markdown tokens and D edits between them. Where several
    longest common subsequences tie, the rule's is the one that algorithm returns. The shared
    crate is the rule's one normative implementation, pinned by the goldens, so every tool
    appends the same sentences.
  - **Units.** The source's sentences by Unicode sentence boundaries (UAX #29), a line break also
    ending one. Each sentence holding a token outside the alignment is appended, in order; the
    rest is already in the Markdown, so overlap is never written twice.
  - **Bound.** When D passes 10,000 edits, the alignment stops, and a source sentence is
    appended, in order, when its word-token sequence is not that of any sentence of the
    Markdown's plain text, split the same way: a comparison by set membership, linear in the
    tokens, so the rule's cost is bounded for any input.
- `smtp_address` is an Exchange (`EX`) address's SMTP address, which schema "1" leaves out
  (README "Known limits"): a recipient's from its `PidTagSmtpAddress` (0x39FE), the sender's from
  the message's `PidTagSenderSmtpAddress` (0x5D01). It is left out when the input doesn't hold it,
  and docproc's typed metadata then takes the sender's SMTP address from `transport_headers`'
  From. Of the committed fixtures, only Tika's `testPST_variousBodyTypes.pst` holds a sender's.
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
- `--attachments <dir>` becomes `--media <dir>`; `--after <id>` keeps its resume.

## docproc-onenote

From spike-ai-pipeline-onenote 0.1.0 (`src/model.rs`, README "Schema \"1\""). It commits no golden
outputs: its tests check JSON-pointer expectations
(`tests/fixtures/{sections,notebooks,content}.json`) and run-to-run byte identity, so
docproc-onenote adds goldens. Lines `notebook` and `skipped` keep
their schema "1" fields, a `skipped` line gaining an optional `encrypted: true` (below), and
`section_group` and `section` keep them less `display_name`, which duplicates their `path`'s last
name (16 of 16). A page changes:

```text
page      line, id, section, level, title?, creation_time?, modification_time, author?,
          markdown?, media?, ink_alternatives?
media[]   kind (image|embedded_file|ink), file?, filename?, size?, extension?, type?
          in the order processing's Page.Media walks them (onenote/media.go)
ink_alternatives[]  word (its index among the page's recognized ink words, from 0), alternatives[]
                    (strings, best first)   one per word the recognizer gave alternates
```

- `content` (containers, outlines, paragraphs, runs, tables, tags), the page's `ink` record, and
  `ink_text` are cut: their text is in `markdown`, and the media they hold move to `media`.
- The Markdown gains the text the spike reads but leaves out of it. The title's date and time
  paragraphs (`PageDateTime`) follow the title heading as one line, the paragraphs joined by one
  space. An image's `ocr_text` sentences that its `alt_text` lacks, by docproc-mail's content
  rule, follow the alt text in the image's Markdown text; OneNote's own alt text holds most of it
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

The contracts move to schema "2". Every spike writes "1", and the additions alone (`properties`, OLE
media, `smtp_address`, `ink_alternatives`, the text the Markdown gains) would fit "1", but the cuts
([`structured-text.md`](structured-text.md)) remove fields each spike reader of "1" declares, and
processing's readers accept exactly "1" (`decodeDocument` in `office/model.go`, `decodeLine` in
`email/model.go` and `onenote/model.go`), so "2" makes an old reader refuse instead of reading
zeros. From "2" on, each tool's schema grows by addition only (an optional field, a line or media
kind); a removal, rename, or change of meaning moves it to the next version.

