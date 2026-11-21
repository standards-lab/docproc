# layers

This note states, as signatures and schemas, the boundaries between the five layers of Cerebro's
stack: the system tools (docproc-ooxml, docproc-mail, docproc-onenote), docproc (the Go extraction
library), docinfer (the Go inference library), go-ai (the model client), and Cerebro (the
data-intelligence capstone's processing service). Each arrow points from a layer to a layer that
uses it:

```text
                    ┌────────────────────────┐
                    │                        ▼
system tools ──► docproc ──► docinfer ──► Cerebro
                               ▲             ▲
go-ai ─────────────────────────┴─────────────┘
```

docproc extracts and knows no model; docinfer infers over what docproc extracts, calling go-ai
directly; Cerebro runs docinfer's jobs over docproc's dispatcher, stages, and stores, which it
wires, and builds the go-ai clients docinfer calls. Each signature or schema traces to a capstone
task in `standards-lab/context/data-intelligence.md` ("Design") or to spike evidence.

Spike paths are relative to `~/experiments/`. "processing" is spike-ai-pipeline-processing, whose
`office`, `email`, `onenote`, and `dispatch` packages are the consumers the tools are measured
against. "the root" is spike-ai-pipeline, and "the spike's docproc" and "the spike's docinfer" are
the commands under its `internal/docproc` and `internal/docinfer`; docproc and docinfer alone are
the libraries, and `cmd/docproc` and `cmd/docinfer` their commands.

## The tool contract

Each tool runs as a process behind docproc's `tool` package (processing `tool/tool.go`), reads one
input, and writes one contract to stdout: JSON Lines, schema "2", with Markdown inside it as the
text layer. Every piece of text the input holds exists once, by the rule "Consumers of structured
text" states and applies field by field.

### Common envelope

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

### docproc-ooxml

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

### docproc-mail

From spike-ai-pipeline-mail 0.3.1 (`src/model.rs`, README "Schema \"1\"", goldens
`tests/golden/{msg,pst,generated}/`; `.eml` and mbox scored against
`tests/fixtures/mime/truth.json`). Lines `skipped` and `store` keep their schema "1" fields, the
store's `display_name`, a .pst's name and a label, among them; `folder` keeps them less
`display_name`, which duplicates its `path`'s last name (58 of 58 folders in the run below). A
message changes:

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
  `rtf`, and `derived.html` are cut (measured below). The Markdown is made from the first of
  these the message holds: its stored HTML, the HTML recovered from an RTF that encapsulates it
  (`\fromhtml`), its text body, then the RTF's text, read as below and made Markdown as a text
  body is. An HTML nested deeper than 512 open elements gives way to the text body, else to the
  HTML's text with its tags stripped, with a warning. When the Markdown is made from HTML, stored
  or recovered from the RTF, the tool appends the sentences of the message's source text that
  the Markdown lacks, by the content rule below, in order, as a paragraph escaped as a text
  body's Markdown is. The source text is the text body; a message with an RTF body and no text
  body has the RTF's text, read by the RTF specification: the body decompressed, control words
  and ignorable groups (`\*`, the font, color, and style tables) stripped, `\par` and `\line` as
  line breaks, `\tab` as a tab, symbol words (`\rquote`, `\emdash`, …) as their characters, and
  `\'hh` decoded in the `\ansicpg` code page; nothing separates a table's cells. An RTF that
  encapsulates HTML (`\fromhtml`) has that HTML's text.
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

### docproc-onenote

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

### Versioning

The contracts move to schema "2". Every spike writes "1", and the additions alone (`properties`, OLE
media, `smtp_address`, `ink_alternatives`, the text the Markdown gains) would fit "1", but the cuts
("Consumers of structured text") remove fields each spike reader of "1" declares, and processing's
readers accept exactly "1" (`decodeDocument` in `office/model.go`, `decodeLine` in `email/model.go`
and `onenote/model.go`), so "2" makes an old reader refuse instead of reading zeros. From "2" on,
each tool's schema grows by addition only (an optional field, a line or media kind); a removal,
rename, or change of meaning moves it to the next version.

## Payload measurement

The measurement is taken over `~/docs` as it stood on 2026-11-12. It finds, for each structured
field, the text already carried, in the Markdown or in a kept field, which is cut, and the content
the Markdown lacks, which its tool renders into the Markdown ("Consumers of structured text");
labels kept as plain-text fields are not counted. The bytes size both. It is rerun
once the planned goal reviewing `~/docs` settles what the corpus holds.

The three spikes were built from their release tags (office `v0.1.0`, mail `v0.3.1`, onenote
`v0.1.0`; the commits past them touch only a README) and run over every file in `~/docs`, at any
depth, whose extension names their formats; no golden fallback was needed. Parts nested in
archives or attachments, and legacy formats LibreOffice converts first, are outside it. Only
inputs that exit 0 are measured.

```sh
S=<scratch dir>
for t in office:v0.1.0 mail:v0.3.1 onenote:v0.1.0; do mkdir -p $S/src/${t%:*}
  git -C ~/experiments/spike-ai-pipeline-${t%:*} archive ${t#*:} | tar -x -C $S/src/${t%:*}; done
(cd $S/src/mail && CARGO_TARGET_DIR=$S/target-mail cargo build --release --locked)
(cd $S/src/onenote && CARGO_TARGET_DIR=$S/target-onenote cargo build --release --locked)
(cd $S/src/office && mise trust . && mise exec -- dotnet publish src/SpikeAiPipeline.Office \
   -c Release -r linux-x64 -p:NuGetAudit=false -o $S/office-bin)
run() { # run <tool> <find -iname patterns…>; one numbered directory per input under $S/out/<tool>
  t=$1; shift; i=0; p=(); for e; do p+=(-o -iname "*.$e"); done
  find ~/docs -type f \( "${p[@]:1}" \) -print0 | sort -z | while IFS= read -r -d '' f; do
    i=$((i+1)); o=$S/out/$t/$i; mkdir -p $o; case $t in
    office)  $S/office-bin/spike-ai-pipeline-office "$f" --format markdown > $o/text.md
             $S/office-bin/spike-ai-pipeline-office "$f" --media $o/media > $o/model.json ;;
    mail)    $S/target-mail/release/spike-ai-pipeline-mail --attachments $o/media "$f" > $o/out.jsonl ;;
    onenote) $S/target-onenote/release/spike-ai-pipeline-onenote --media $o/media "$f" > $o/out.jsonl ;;
    esac 2> $o/stderr; echo $? > $o/exit; done; }
run office docx docm dotx dotm xlsx xlsm xltx xltm pptx pptm potx potm ppsx
run mail eml msg pst mbox
run onenote one   # sections only: notebooks would count their sections twice
```

office's publish needs `-p:NuGetAudit=false`: restore fails on Microsoft.DotNet.ILCompiler
10.0.13's advisory GHSA-p3jp-9v83-76w9 as an error, which the move to .NET 11 clears.

The measure: a structured text field is a string the contract carries outside its Markdown; its
bytes are the string's UTF-8 bytes before JSON escaping. A string is duplicated when, with
whitespace collapsed to one space and trimmed, it occurs in its line's Markdown with Markdown
backslash escapes removed and whitespace collapsed the same way (office's document, mail's message,
onenote's page). A mail body form (`text`, `html`, `rtf`, `derived.html`) is an alternative of the
Markdown made from it, and counts whole in the table; the mail bullet below compares its text
with the Markdown. The fields counted:

- **office**: every string in the model, and every numeric cell `value` as its JSON text, except
  the enumerations and references `type`, `format`, `schema`, `contentType`, `source`, `target`,
  `valueType`, `ref`, `sharedFrom`, `chartType`, `kind`, and a media block's `name`.
- **mail**: each message's (nested ones included) `subject`, `transport_headers`,
  `conversation_topic`, attachment `filename`, and body forms; addresses, folder paths, and the
  `store` line's `display_name` (one per .pst) are not counted.
- **onenote**: each page's `title`, `author`, and every string under `content` and `ink_text`
  except `kind`, `style`, `file`, `extension`, `font_color`, `highlight`, `math`, `shape`,
  `status`, `completed_time`, `type`, `number`, `prefix`, `suffix`, `bullet`, `id`, `section`, and
  the times.

Output is the bytes on stdout; office's is `model.json` plus `,"markdown":` and `text.md` as one
JSON string, as one run will write them. The run reproduced byte for byte.

| Tool | Read | Output | Markdown | Structured text | Duplicated | Share |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| office | 85 of 85 (43 docx, 25 xlsx, 17 pptx families) | 622,303 B | 53,972 B | 36,179 B | 35,526 B | 5.7% |
| mail | 72 of 79 (47 eml, 3 mbox, 16 msg, 6 pst); 599 messages | 5,034,929 B | 1,486,558 B | 3,057,909 B | 1,713,649 B | 34.0% |
| onenote | 16 of 17 sections; 20 pages | 136,702 B | 29,706 B | 26,802 B | 24,765 B | 18.1% |

Refused: 7 cpython `.eml` fixtures that are no message (`msg_11`, `18`, `19`, `37`–`40`), and
`Encryption.one`, password-protected; both as their READMEs state.

- **office.** 98% of the model's text is in the Markdown: inline text 24,237 B (24,145
  duplicated), text cell values 5,547 B (5,505), numeric cell values 391 B (359), chart
  categories, names, and titles 3,288 B (all), comment anchors and authors 1,024 B (all), picture
  alt text 797 B (all), sheet names 341 B (all); formulas 534 B (67, since Markdown shows a
  formula only without a value). The inline text and cell values not counted duplicated are in
  the Markdown under markup the measure doesn't remove: a paragraph's line break as a trailing
  backslash (92 B), a cell's line break as `<br>` (42 B), booleans as `TRUE` and `FALSE` (32 B).
  Picture titles, 20 B, are the one text the Markdown lacks. The larger waste is the model's
  shape: 564,912 B written indented is 182,072 B compact, so indentation is 382,840 B, 62% of the
  output. Compact, the text block tree is 122,509 B beside sheet tables 40,506 B, media blocks
  9,773 B, and charts 9,284 B. By family: docx 7.3%, xlsx 4.4%, pptx 3.4% duplicated.
- **mail.** Body forms are the duplication: `text` 1,461,216 B, `rtf` 145,003 B, `derived.html`
  56,631 B, `html` 50,332 B. A `.msg` is 78% duplicated (RTF and the HTML recovered from it), a
  `.pst` 46%, an mbox 31%, an `.eml` 7%. `transport_headers` is 1,305,095 B, 26% of the output;
  114,724 B of it repeats the parsed subject, addresses, and threading fields.
- **mail's text body against the Markdown.** The span is a line of the text body (split at line
  feeds, CR LF as one), whitespace collapsed and trimmed, empty lines skipped, compared with its
  message's Markdown by the duplication rule above. Of 1,235,972 B of text-body lines, 19,149 B in
  15 messages are not in the Markdown by that rule, all where the Markdown is made from HTML; where
  it is made from the text body, nothing is. docproc-mail's content rule is measured over the
  same `~/docs` as docproc-mail ships it: the docproc-envelope crate on Rust 1.100.0, with NFC,
  the dictionary-free UAX #29 word and sentence boundaries, and general categories from ICU4X
  2.4.0 on Unicode 18.0.0, CommonMark read by pulldown-cmark 0.13.6, and the crate's own Myers
  alignment. A scratch build of the tool writes each comparison to stderr, comparing the Markdown
  htmd makes from the HTML before anything is appended. Of the 31 messages whose Markdown is made
  from HTML, 28 with a source text, the sentences the rule appends are 295 B in 7 messages, each
  a text body that says what its HTML doesn't: cpython's `msg_10.eml` (143 B, the encodings its
  text parts name) and `msg_15.eml` (18 B), the generated inline-image message as `.eml`, `.msg`,
  and in `mailbox.pst` (30 B each, "It is taped into the log book.", where the HTML shows the
  image), POI's `ASCII_UTF-8_CP1252_LCID1031_HTML.msg` (16 B), and Tika's `testMSG_chinese.msg`
  (28 B). Outlook 2003's welcome message, once in Tika's `test-outlook2003.msg` and twice in
  libyal's `outlook.pst`, appends nothing: its stored text body holds U+0099 where its HTML holds
  `™` (the .msg's `PidTagBody` is `PT_UNICODE`, code unit 0x0099), a character the sending client
  mis-decoded, not one docproc-mail decodes, and as a control and a symbol both only separate
  words, so `SharePoint` matches. No decoder
  correction applies: PT_STRING8 is read in its message's code page, and a MIME part's charset
  label as the WHATWG Encoding Standard reads it (mail-parser maps `iso-8859-1` and `us-ascii` to
  windows-1252). The RTF's text, read as "docproc-mail" states, compared the same way, lacks
  nothing where the Markdown is made from it (2 messages); beside a stored HTML body (2 messages)
  it lacks 57 B, all in `testMSG_chinese.msg`: 28 B, the line its text body also holds, and 29 B,
  a table's numbers 1 to 19, which the RTF's text runs together into one word, since nothing
  separates its cells.
- **onenote.** Alt text 9,684 B (all), run text 7,482 B (6,944), image `ocr_text` 5,572 B (4,926,
  every duplicated byte of it reaching the Markdown inside the same image's alt text), filenames
  3,240 B (2,865), run links 114 B (all, as link targets), tag labels 57 B (27), ink alternatives
  79 B (8). The run text missing, 538 B, is every title's date and time paragraphs; the
  `ocr_text` missing, 646 B, is OCR text no alt text holds, all of it in 6 images with no alt
  text; the tag labels missing, 30 B, are "To Do", which the spike writes only as a task box.
  No image carries a `hyperlink`. Media written over mail and onenote: 235 MB.
- **onenote's OCR text against the alt text.** docproc-onenote's content rule is measured over
  the same 17 sections, unchanged against `~/docs`' MANIFEST.json, as docproc-onenote ships it:
  onenote_parser 2.1.0, and the docproc-envelope crate on Rust 1.100.0, with NFC, the
  dictionary-free UAX #29 word and sentence boundaries, and general categories from ICU4X 2.4.0
  on Unicode 18.0.0, both texts read as plain text with no CommonMark reader, and the crate's own
  Myers alignment. A scratch build of the tool writes each image's comparison of its `ocr_text`
  with its `alt_text` to stderr. Of the 135 images holding OCR text (5,572 B), the sentences the
  rule appends are 566 B, whitespace collapsed, in those 6 images: interop's
  `FileSyncWOPI_OneNoteWithODB.one` (414 B in 40 sentences, the recognizer's reading of a scan as
  CJK characters) and five in `LargeData.one` (30 or 31 B each, a number and "This is a testing
  document."). The other 80 B of the 646 are the line breaks and spaces around those sentences
  and one sentence, `',`, that holds no word, which the rule never appends. Every other image's
  OCR text is in its alt text by the rule.

## Consumers of structured text

Every piece of text a file holds exists once: content in the Markdown text layer, labels as kept
plain-text fields. docinfer embeds text and Markdown outputs only, never records
(`spike-ai-pipeline/internal/docinfer/infer/sources.go`, `textKinds`), and chunks Markdown at its
headings (`internal/docinfer/chunk/doc.go`), so the Markdown is the content's one path to
inference. A field's outcome follows from that, the first that applies:

- **Kept** when a consumer reads it (a processing package, or a capstone task), whether or not
  its text is in the Markdown, or when it is a label: plain text that names or annotates the
  content rather than being it, which the Markdown leaves out and metadata keeps.
- **Cut** when its text is already carried: in the Markdown, or in a kept field, as folder,
  section, and group `display_name` duplicate their `path`'s last name. Either is duplication.
- **Into Markdown** when its tool reads content the Markdown lacks: the tool renders it into the
  Markdown, never keeps it beside it as a duplicate field, and the field is cut, since the
  Markdown carries it. A link target the Markdown lacks is text, rendered as a Markdown link.
- **Cut** when it can't be plain text and has no processor.

Owner is the layer that produces the field's value as the settled designs leave it (the tool,
docproc, docinfer, or Cerebro), with the layer that holds a form of it beside; a cut field's text
is owned where it is carried. Capstone task is the task of
`standards-lab/context/data-intelligence.md` ("Design") that reads the field, or, where none
does, the docproc stage that reads it.

| Tool | Field | Consumer | Owner | Capstone task | |
| --- | --- | --- | --- | --- | --- |
| ooxml | `markdown` | `dispatch/office.go` (asks office for Markdown alone) | tool; docinfer chunks it | chunks | kept |
| ooxml | text block tree: heading, paragraph, list, docx table inlines; sections, notes, comments (author, anchor, replies) | none: `dispatch/office.go` never requests the model; the Markdown holds their text | tool, in `markdown` | chunks, through `markdown` | **cut** |
| ooxml | chart title, series names, categories, values | none; the Markdown's table holds them | tool, in `markdown` | chunks, through `markdown` | **cut** |
| ooxml | picture `alt` | none; it is the Markdown's image text | tool, in `markdown` | chunks, through `markdown` | **cut** |
| ooxml | picture `title` | none; the Markdown lacks it unless it is the image text, and takes it as "docproc-ooxml" states | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| ooxml | external hyperlink targets | none; the Markdown lacks them, and renders each as a link, an internal anchor staying text | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| ooxml | sheet `name`, cell `value`, `formula` | `athena-mapping` (Athena's tabular rows into `sparse_rows` need typed cells); names and values are in the Markdown, a formula is its cell's expression, whose value the Markdown shows | tool; office's stage yields each sheet's rows as a record output | athena-mapping | kept |
| ooxml | `media` (`file`, `part`, `target`, `prog_id`, placement) | office's stage (embedded and OLE media become child parts); references, not text, and a linked picture's `target` is also its Markdown image's source | tool; docproc makes embedded and OLE media child parts | none: office's stage | added |
| ooxml | `properties` | `metadata` | tool | metadata | added |
| mail | `markdown` | `dispatch/email.go` (`own`), `email/model.go` (`Conversation`) | tool; docinfer chunks it | chunks | kept |
| mail | `body.html`, `body.derived.html` | none; `Conversation.HTML` copies `html` and nothing reads it; the Markdown is made from them | tool, in `markdown` | chunks, through `markdown` | **cut** |
| mail | `body.text`, `body.rtf` | none; the Markdown holds their text, but 295 B of text bodies beside an HTML body (and 28 B of RTF, the same line), which the tool appends as "docproc-mail" states | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| mail | `subject` | `email/threads.go` (`baseSubject`), `dispatch/email.go` meta | tool | metadata | kept |
| mail | `message_id`, `in_reply_to`, `references`, `conversation_index` | `email/threads.go` | tool; docproc's `Threader` threads by them | none: the email stage's `Threader` | kept |
| mail | `conversation_topic` | `email/model.go` (`Conversation.Topic`) | tool | none: the email stage's `Conversation` | kept |
| mail | `transport_headers` | `metadata`: typed mail metadata is docproc's and Cerebro's, built from the raw headers | tool; typed by docproc and Cerebro, as `metadata` settles | metadata | kept |
| mail | address `name`, `address`, `smtp_address` | `email/model.go` (participants), `metadata` | tool; docproc takes a sender's SMTP address from From when the tool leaves it out | metadata | kept |
| mail | attachment `filename` | `email/route.go`, `dispatch/email.go` (`Found` name) | tool | none: the email stage, naming the found part | kept |
| mail | message `media` (`file`, `mime_type`) | the email stage (a `data:` image the Markdown links becomes a found part); a reference, not text | tool; docproc makes each a child part | none: the email stage | added |
| mail | folder `path` | `dispatch/email.go` meta | tool | metadata | kept |
| mail | folder `display_name` | none; it duplicates `path`'s last name, a kept field | tool, in `path` | metadata, through `path` | **cut** |
| mail | store `display_name` | `metadata`; a .pst's name, a label the Markdown leaves out. No processing package reads it: `email/model.go` decodes it, and `dispatch/email.go` passes the store through | tool | metadata | kept, a label |
| onenote | `markdown` | `dispatch/onenote.go` (`page`) | tool; docinfer chunks it | chunks | kept |
| onenote | page `title` | `dispatch/onenote.go` meta | tool | metadata | kept |
| onenote | runs' `text`, `latex`, `link`; list markers; table cells | none; the Markdown renders them, links as link targets | tool, in `markdown` | chunks, through `markdown` | **cut** |
| onenote | the title's date and time runs (`PageDateTime`) | none; the Markdown lacks them (538 B), and takes them under the title, joined by one space | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| onenote | tag `label` | none; the Markdown renders it as "docproc-onenote" states, since a task box carries the task state, not the label's words | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| onenote | image `alt_text` | none; it is the Markdown's image text | tool, in `markdown` | chunks, through `markdown` | **cut** |
| onenote | image `ocr_text` | none; 4,926 B is within `alt_text`, and the sentences the alt text lacks by the content rule (566 B) join the Markdown's image text | tool, in `markdown` | text-layer, through `markdown`'s image text | into Markdown |
| onenote | image `hyperlink` | none; the Markdown lacks it, and makes the image a link to it | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| onenote | image `printout_page`, ink `strokes` and `bounds` | none; not plain text, and vision reads every image and ink SVG | tool, in the image and the ink SVG | vision, through the image | **cut** |
| onenote | `ink_text` words | none; they are the ink image's alt text in the Markdown (`![ink: <words>](<file>.svg)`) | tool, in `markdown` | chunks, through `markdown` | **cut** |
| onenote | `ink_text` `alternatives` | `metadata`; the recognizer's alternates, a label the Markdown leaves out | tool | metadata | kept, a label, as `ink_alternatives` |
| onenote | `ink_text` `language` | none; a Windows LCID number (`u16`, `model.rs`), not text: it can't be plain text and has no processor | none: cut | none: no processor | **cut** |
| onenote | media `filename` | `dispatch/onenote.go` (`Found` name) | tool | none: the onenote stage, naming the found part | kept |
| onenote | `skipped` `encrypted` | none in docproc, which reads one section a part and maps a refusal whose reason begins `encrypted` to `ErrEncrypted`; an operator's notebook read | tool | none: no stage reads it; an operator's notebook read | added |
| onenote | notebook `name`; section and group `path` | `dispatch/onenote.go` meta, `metadata` | tool | metadata | kept |
| onenote | page `author`, times | `metadata` | tool | metadata | kept |
| onenote | section and group `display_name` | none; it duplicates `path`'s last name, a kept field | tool, in `path` | metadata, through `path` | **cut** |
| all | `schema`, and `--version`'s `schema <n>` | `internal/cli`, each reader's spec refusing a tool of another schema at startup, and the readers' decoders | tool | none: every reader | kept, the version line added |
| all | stderr warnings (`<tool>: warning: <where>: <what>`) | `internal/cli`, parsed to `dispatch.Warning` | docproc, as data; the tool writes the line | events, as Cerebro's warning event | kept |

`text-layer` reads an image's OCR text from the Markdown's image text; the contract carries no
`ocr_text` field.

## docproc's API

The library module, `github.com/standards-lab/docproc`, extracts: it inspects and routes each part
of an input, reads it through its stage and the tools, stores each output's content, and records
what it prepared in its index. It requires `golang.org/x/text` only and depends on no model concept:
no model client, no inference, no embedding. Inference is docinfer's
(`github.com/standards-lab/docinfer`), and the imports run one way: docinfer imports docproc, and no
docproc package imports docinfer. The base module is built (`library`): each package's doc comment
is the reference for its exported names, and the signatures below are the boundary other layers
build against. The providers are built (`providers`), each module's README and package doc their
reference; `rundir` and `cmd/docproc` are built (`docproc`), the README's "The command" and each
package's doc their reference, and `cmd/docproc`'s report is built (`reports`), `internal/report`'s
package doc its reference.

The modules:

```text
github.com/standards-lab/docproc              the library below; requires golang.org/x/text only
github.com/standards-lab/docproc/storage      Content over go-storage's Client, built:
                                              New(Config{Client, Prefix, Renew}); requires the
                                              library and go-storage
github.com/standards-lab/docproc/postgres     Index over PostgreSQL through sqlate, built:
                                              New(ctx, Config{DB *sqlate.DB, Lease, Flush}),
                                              Index.Close, Migrations, Verify, Statements, and
                                              NewRetention(db, age); requires the library,
                                              sqlate, sqlate/postgres, and pgx
github.com/standards-lab/docproc/cmd/docproc  the docproc command, built: version and run;
                                              requires the library, go-cli-sdk, go-core, and
                                              golang.org/x/sys
```

Each store is an interface the library declares (`dispatch.Content`, `dispatch.Index`), with a
filesystem and a memory implementation in the base module on the standard library alone. A
provider is a nested sub-module named for its target system that implements one over a stack
or adjacent library: `storage` the content store in object storage over go-storage's `Client`
and its providers, and `postgres` the index in PostgreSQL ("Index"). `postgres` takes a
`*sqlate.DB`, its pool on pgx's `database/sql` driver for `Wait`'s LISTEN, its `Verify` a
`sqlate.Session` as go-messaging's `postgres` does, and requires sqlate for its statements and
migrations and sqlate's `postgres` for the dialect; the pool behind the session is the host's,
which Cerebro opens through go-database and wraps with `sqlate.Wrap`, so no provider requires
go-database. A provider over a tier pins only its own level, above that tier
(`repository-topology.md`, "A repository's place follows its base module"):
`storage` sits above go-storage's, `postgres` depends on no tier, and the base module stays
tier-free.

The library's packages:

```text
route, inspect             what a file is, whether it is safe to open, which stage reads it
tool                       the exec and container runners; only the exec runner needs Linux
internal/cli               the envelope reader: lines, stderr, exit codes, the media name
                             rule, the refusal reasons' mapping, the schema and version
                             pattern; Spool, LimitedBuffer, LineWriter, WriteTemp, UTC
office, email, onenote     a tool's reader, decoding its schema "2"; exec runner only
document, picture,         the other formats, on their tools; audio's and media's plan
  audio, media, sevenzip     records beside them
dispatch                   the walk over an input's parts, the Stage contract and its error
                             classes, the content store and its layout, the index; the
                             shapes of its events, parts, outputs, warnings, and stage
                             table, the Meta keys and record media types consumers read;
                             imports route and inspect only, any 64-bit platform
dispatch/dispatchtest      the Index and Content conformance suites and the fixture stage
stages                     Build and its Options, Routes and ParseRoutes, the stage
                             adapters, unexported, the failure classification, the
                             identities
internal/tooltest,         test helpers: the unit tier's stand-in tool, golden sums, and
  goldentest, mediatest      media's stand-in ffmpeg run
rundir, rundir/schema      cmd/docproc's run directory: its shapes, read and written
                             (built, `docproc`)
```

No root package. Another module reaches `dispatch`, `dispatch/dispatchtest`, `route`, `inspect`,
`stages.Build`, `rundir`, and the record types docinfer decodes (the plan records, the readers' line
types, `email.Threader`); the module builds on every 64-bit platform, and only running a tool needs
Linux.

### Inspection and routing

The dispatcher runs both on every part before its stage, so a part's event carries its route and
flags; a caller calls them directly only to triage a file before queueing it. `File` and `Inspect`
take a path for that caller, as thin wrappers over `At` and `Content`, which take the open file,
its size, and its kind, and which the dispatcher calls with the file it hashed, so no package
below `dispatch` opens or identifies a part again. `inspect.Flag` is JSON (`check`, `class`,
`evidence`), since a part's shape carries it.

```go
package route
type Kind string   // the format a signature names; Unknown is ""
type Route string  // document, office, vision, vision-converted, nested, onenote, archive,
                   // audio, video, text; unrouted
func Identify(head []byte) Kind                                // at most HeadLen (4096) bytes
func File(path, name, mimeType string) (Route, string, error) // the route, and why
func At(r io.ReaderAt, size int64, head []byte, name, mimeType string) (Route, string, error)
func Ext(name string) string                                   // trimmed and lowercased
func EncryptedPackage(r io.ReaderAt, size int64) (bool, error) // an OLE2 EncryptedPackage
func LegacyTarget(ext string) string // the OOXML extension a legacy one converts to

package inspect
type Verdict string // proceed | quarantine
type Class string   // active | unsafe
type Check string   // a rule, never a format: signature-mismatch, archive-traversal,
                    // archive-absolute-path, archive-symlink, archive-hardlink, archive-device,
                    // archive-encrypted, archive-bomb, zip-unreadable, tar-unreadable, …
type Flag struct { Check Check; Class Class; Evidence string } // tagged check, class, evidence
type Report struct { Kind route.Kind; Flags []Flag; Verdict Verdict }
type Options struct {
    Policy map[Check]Verdict; BombRatio float64; BombSize uint64
    Entries int // an archive's listed entries, past which it is unreadable; zero for no bound
}
func (o Options) Validate() error                 // matches ErrOptions
func (o Options) Bomb() BombRule                  // the rule; the zero rule is passed by nothing
func (o Options) Verdict(flags []Flag) Verdict    // under the current policy
type BombRule struct { Ratio float64; Size uint64 }
func (b BombRule) Past(expanded, against uint64) bool
func Inspect(ctx context.Context, path, name string, opts Options) (Report, error) // content flags
func Content(ctx context.Context, r io.ReaderAt, size int64, kind route.Kind, opts Options) (Report, error)
func Name(name string, kind route.Kind) []Flag    // the name checks, no I/O
func CheckListing(kind route.Kind, size int64, entries []Listed, opts Options) (Report, error)
```

A refused `Options` matches `inspect.ErrOptions`, the package's one refusal class in place of an
`ErrRequest`.

A content check depends on the kind and the bytes alone; a name check is `Name`'s, computed at
every occurrence. A link, a device, and a traversing or absolute name in an archive are active,
the entry an unread child ("Dispatch") and the others read; a bomb and an unreadable directory
are unsafe. The bomb rule weighs each entry and the sum of an archive's entries.

### Running the tools

Cerebro runs the exec runner inside an image that bundles the tools (`tool-packaging`). The
container runner stays for hosts like `cmd/docproc`'s, since it needs the docker socket
(data-intelligence.md, "Missing layers"). `tool` reports a run's facts and decides no outcome;
`stages` classifies them ("Dispatch").

```go
package tool
type Spec struct {
    Program string; VersionArgs []string; VersionPattern *regexp.Regexp
    Minimum string; VersionExit int
    KillAfter   time.Duration // SIGTERM to SIGKILL; zero sends both at once
    MemoryLimit int64         // bytes, zero for none: --memory, or the exec runner's RLIMIT_AS
    Env         []string      // added to the exec runner's fixed environment
    Timeout, Idle time.Duration // every run whose Invocation sets neither; zero for none
}
type Invocation struct {
    Args []string; Dir string; Stdin io.Reader; Stdout, Stderr io.Writer; Usage *Usage
    Timeout time.Duration // a unit run's own bound, zero for none
    Idle    time.Duration // a stream run's: ended when it writes nothing for this long
}
type ExitError struct {
    Program  string
    Code     int    // -1 for the exec runner's death by signal, 128 plus its number the container's
    Signal   string // the signal's name when the tool died by one; empty for an exit
    TimedOut bool   // the run passed its own Timeout or Idle, not the caller's context
    Message  string // the last 4 KiB of stderr, when the caller passed no writer for it
}
func NewExec(ctx context.Context, spec Spec) (*Tool, error)
func NewContainer(ctx context.Context, docker string, image Image, spec Spec) (*Tool, error)
// Run runs once; ctx's end sends the process group SIGTERM, then SIGKILL after KillAfter.
func (t *Tool) Run(ctx context.Context, inv Invocation) error
func (t *Tool) AtLeast(minimum string) error // a *VersionError when older
func (t *Tool) Name() string
func (t *Tool) Version() Version
func (t *Tool) VersionLine() string     // the whole line the version was found on
func (t *Tool) Image() (Image, bool)    // a container tool's image
```

The exec runner starts each program with a fixed environment (`PATH`, `LANG=C.UTF-8`, `HOME` the
invocation's `Dir` when it has one, `TMPDIR` a new directory of the run's own under the host's
`TMPDIR`, then `Spec.Env`), and kills its process group whenever the leader has exited. It removes
the run's `TMPDIR` once the leader is reaped, whatever the run's outcome, and a run whose `TMPDIR`
can't be made fails before the program starts. It writes the program's `oom_score_adj` of 1000
to /proc once the program has started, best effort. The container runner passes no `Spec.Env`, the
image's environment being the container's, and a `--pids-limit` of 4096.

`office`, `email`, and `onenote` keep their readers over a `*tool.Tool`, through the exec runner
only: a constructor refuses a tool whose image is a container. `internal/cli` holds the one envelope
reader all three run: it runs the tool with stdout and stderr on pipes, checks the first line's kind
and `schema` (one `ErrSchema`), yields each line's kind and raw JSON, parses stderr as it arrives
into a warning's `where` and `what`, maps exit codes 1 to 4 and a signal to one `ExitError`, and
resolves a media name in the media directory by the envelope's name rule. It exports the schema
constant (`"2"`) and the version pattern, which requires `--version`'s schema line, once, so each
reader's `Spec` takes them, and holds the one mapping of a refusal's reason ("Common envelope"): a
reason beginning `encrypted` to `dispatch.ErrEncrypted`, and, beside it, onenote's table-of-contents
reason to `dispatch.ErrReadElsewhere`. A reader keeps its request, its arguments, and its line
types, decodes its tool's schema "2" line by line (the contracts above), and keeps an unknown kind
as an `Unknown*` value. The readers' types follow the contracts: `office.Document` is the
`document` line, `office.Sheet` the `sheet` line and `office.Rows` a `rows` line, `email.Message`
carries `Markdown`, `Address.SMTPAddress`, and `Media`, and `onenote.Page` carries `Markdown`,
`Media`, and `InkAlternatives`. Each reader streams its tool's lines as records, office's included,
so no reader holds a whole sheet, and returns the sequence itself, so no `Stream` type holds a
context.

```go
package email // onenote has the same shape, its Request taking FileOnly in place of After
func NewReader(cfg Config) (*Reader, error) // Config{Mail *tool.Tool}; a nil Mail panics by name
func (r *Reader) Read(ctx context.Context, req Request) iter.Seq2[Record, error]
type Request struct { Input string; Media string; After uint64 } // Media: --media

package onenote
type Request struct { Input string; Media string; FileOnly bool } // FileOnly: --file-only

package office
func NewConverter(ctx context.Context, cfg Config) (*Converter, error) // Config{Office, Pictures}
// Convert reads the document at req.Path, which the caller owns; only a legacy document or an
// RTF is copied, into the LibreOffice conversion's input. LibreOffice, the tool, the picture
// batch, and every removal run inside the sequence; the document comes first, after the batch.
func (c *Converter) Convert(ctx context.Context, req Request) iter.Seq2[Record, error]
type Request struct { Path, Dir, Name string; MetafileScale int }
```

`office.Config` takes the tool and a `*picture.Converter`, whose `RunLibreOffice` is the one
LibreOffice runner, for a legacy document's or an RTF's conversion, and which converts every
picture.

### Input from a stream, or a path

```go
package dispatch
type Input struct {
    Path   string    // a file the caller owns; the dispatcher never removes it
    Reader io.Reader // or a stream, spooled into Config.Dir and hashed as it is copied,
                     // removed with the part
    SHA256 string    // the content's SHA-256 when the caller knows it: checked as the input is
                     // spooled or read, a mismatch an error matching ErrSHA256, never hashed twice
    Name   string    // the stored name, whose extension routing reads
    MIME   string    // the stored MIME type, if any
    After  uint64    // resume a store after the message this cursor names, mail's --after:
                     // opaque, compared by equality alone
}
```

The stages read paths, since mail, onenote, qpdf, and ffprobe take one, and
`office.Converter.Convert` takes the part's path in its `Request`. So one spool at the input serves
every stage. The spool's check is the input's one whole-file check: a mismatch stops the dispatch
with an error matching `ErrSHA256`, which no redelivery changes. Cerebro always passes blob
storage's reader of the job's staged input and its SHA-256 ([`cerebro.md`](cerebro.md), "Queries and
Commands").

An input with `After` set is a resumed store: it is dispatched past that message, and its entry
is never recorded, since it would hold only the messages past `After`.

### Dispatch

`Dispatch` yields each output as its stage makes it, and each part's own event once its outcome
is known.

```go
type Stage interface {
    Identity() Identity // name, its tools' version, its configuration's hash
    Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error]
}
type UntrustedWhere interface { Stage; UntrustedWhere() } // a marker: its Where are the content's
type Stages map[route.Route]Stage
type Config struct {
    Stages Stages; Index Index; Content Content; Dir string; Limits Limits
    Inspect inspect.Options
    Puts    int           // content puts in flight per dispatch, at least 1
    Release time.Duration // bounds a claim's Release after the dispatch's context ended
}
func New(cfg Config) (*Dispatcher, error)
func (d *Dispatcher) Dispatch(ctx context.Context, in Input) iter.Seq2[Event, error]
func (d *Dispatcher) Pool(ctx context.Context, inputs iter.Seq[Input], workers int) iter.Seq2[PoolEvent, error]
func (d *Dispatcher) Content() Content // Config.Content, for a host's own puts and reads
func (d *Dispatcher) Index() Index     // Config.Index, for a host's drop of an entry

type Outcome string   // prepared, no processor yet, seen, cycle, quarantined, encrypted,
                      // read elsewhere (a table of contents, whose sections are read as their
                      // own parts; not a failure), failed, limited
type Item struct { // one is set; OutputItem, PartItem, WarningItem, and FlagItem make one
    Output  *Output
    Part    *Found
    Warning *Warning
    Flag    *inspect.Flag // a flag a stage raises about its own part
    After   uint64 // a store's: set on a message's last output, the message's cursor
}
type Found struct {
    Path, Name, MIME, Where string
    SHA256  string         // the stage's claim of the content's SHA-256, checked
    Release func()         // called once the part is settled; see below
    Outcome Outcome        // an unread child's, with no file: its outcome, reason, and flags
    Reason  string
    Flags   []inspect.Flag
}
// A stage's classes.
var ErrEncrypted, ErrReadElsewhere, ErrNoProcessor, ErrRefused, ErrTooLarge error
var ErrClaimLost error // a renewal, append, or record refused: the dispatch ends, transient
var ErrSHA256, ErrRequest error
type StoreError struct { Store string; Err error } // "index" or "content": the store's failure
```

A stage's `Prepare` runs on a goroutine of its own: each item is handed to the walk and taken before
the stage goes on, as a yield is. A tool's stderr warning, the common envelope's
`<tool>: warning: <where>: <what>`, parses to a typed `Warning`. A warning's event carries its part and stage, as an
output's does, and the index records it, so a seen part replays its warnings. A warning says a unit
of the part read badly or not at all (a page, a window, the form, a tool's `skipped` item with no
content to name), its `Where` naming the unit; a stage's own warning names the stage in `Tool`. A
store's warning is appended where its reader yields it: the order between a warning and a record is
best effort, since they travel on two pipes, but a warning never precedes its store's first record,
and a resumed replay and a resumed preparation both carry the warnings read after the cursor. The
mail stage sets the cursor, `Item.After` and `Event.After`, on each message's last output, so a host
checkpoints a store by the outputs it has handled (docinfer's `job` package doc). `After` is
opaque and compared by equality alone, never ordered: a `.pst`'s ids are node ids, which don't
ascend in read order.

`Item.Flag` is how a stage raises a flag about its own part, the document stage qpdf's findings
and the archive stage its listing's. A flag yielded before the part's first output is weighed,
and a quarantine verdict stops the stage; a flag yielded later is recorded and reported on the
part's own event, and never unwinds events already out.

A found part (`Found`) names an occurrence. It may carry no file and instead an outcome
(quarantined, encrypted, no processor yet, failed, limited), a reason, and flags: an unread child,
such as an archive's link or encrypted entry, an entry past the bomb rule, an entry the bytes cap
stopped, a reader's medium or attachment with no content, or a `skipped` line naming a section or
message. The dispatcher yields its own event and records it as a `Child`, its `Part` with a name
and `Where` and no SHA-256 or size, so counts by outcome are true. A part has three ways to say what it didn't read: a flag, a warning,
or a child; no Meta key and no record output says it. An archive entry whose flags come to
quarantine is such a child, never extracted.

`Limits.Bytes` bounds what a stage writes, not only what the dispatcher admits.
`dispatch.Remaining(ctx)` returns the bytes the dispatch may still admit: the cap less the sizes of
the parts admitted when `Prepare` started, read on the walk's goroutine before the stage's
goroutine starts. The archive stages and the document stage's attachments write each found part
against it, less what the same `Prepare` has written whole before; a part whose bytes would pass it
is stopped at that write, its partial file removed, and found as an unread child, `limited`, with
no size or SHA-256. The dispatcher gives that child its own reason, "past the bytes cap, <cap>",
and marks its parent's subtree incomplete, so the parent isn't indexed and a larger cap prepares
it again. The cap check precedes the bomb check. The media docproc-ooxml, docproc-mail, and
docproc-onenote write are held to the cap only once written.

A stage that holds content a later item names again, as a store's tool writes each distinct
content once, sets `Found.Release`. The dispatcher calls it once the part is settled: its entry
recorded or seen, or its route without a stage. The stage may then drop what it holds, and a
later item naming the same content arrives as a `Found` with no file, no outcome, and the same
`SHA256`, dispatched as the released part was. The email, onenote, and office stages release so,
so a store's working directory holds the content of the records in flight; a part the dispatcher
doesn't settle is never released, and its later item is dispatched from its file again.

A stage's outcome is classified once, in `stages`, from the facts `tool` reports, for a unit and
for a part alike:

```text
the content's, recorded     the tool exited nonzero by its own code: a docproc tool's 3 and 4,
                            gs's 1, qpdf's 2, 7zz's 8 (out of memory), and every other tool's
                            every nonzero exit; it died by its own fault (SIGSEGV, SIGABRT,
                            SIGBUS, SIGFPE, SIGILL), an allocation refused under the memory bound
                            included; it passed its own time bound (TimedOut); its output didn't
                            parse; a format package's content error (no audio or media stream,
                            an archive 7zz can't open or list, or listing past Entries, an item
                            unlike its listing); a put over the store's size (ErrTooLarge)
the run's, retried          the context ended; SIGKILL, SIGTERM, or another signal docproc didn't
                            send; the program didn't start; a docproc tool's exit 1 (not written)
                            or 2 (usage); 7zz's exit 7 (command line) or 255 (stopped); soffice's
                            exit 77 (a fatal error at its start); any error on docproc's side of
                            the pipe
encrypted                   a stage's ErrEncrypted alone, mapped from a refusal's reason by
                            internal/cli's one mapping ("Running the tools")
read elsewhere              a stage's ErrReadElsewhere alone, onenote's table-of-contents
                            refusal, by the same mapping
```

A unit's failure of the content's class is a warning, and the part is prepared with the outputs it
has; a part fails only when nothing of its content can be read, and then fails with `ErrRefused`,
recorded, its subtree complete, so its ancestors record. A failure of the run's class fails the
part unrecorded, so a transient fault is never replayed as a permanent gap. A flag decides no
outcome: content flagged `pdf-encrypt` whose stage fails is Failed. A recorded failure stays
until its entry is dropped or ages out; a time bound is in no stage's identity, so a host that
raises it drops what the old bound failed.

office's stage yields a docx's or pptx's Markdown as one Markdown output, named `document`, and
an xlsx's per sheet: the `document` line's Markdown as that output, then two outputs for each
sheet, both named `sheet/<n>`, n the sheet's index as `media` places it: a Markdown output, its
`sheet` line's Markdown followed by its `rows` lines', and a record output, its `rows` lines as
JSON Lines, each without its `markdown`, so each sheet's text exists once in `Content`. Each
sheet output's `Meta` holds the sheet's name under `sheet`, so no sheet's name collides with
`document`. The stage writes a sheet's outputs to their files as its lines stream, so nothing
holds a whole sheet, and each reaches `Content` as any output's file does.

The dispatcher hashes each output as its stage yields it, filling `Output.SHA256` (the part's own
SHA-256 when `Output.Path` is the part's file), then puts its content into `Content`, and appends
the output to the part's entry only after the put, so everything the index records is stored.
Preparation is a pipeline per part being prepared: a queue of at most `Config.Puts` outputs, each
hashed on the walk's goroutine, put on a put goroutine, then appended and yielded in order from
the walk's goroutine. The queue drains before a found part is dispatched and before the part's own
event, so concurrency exists across runs of consecutive outputs (a PDF's pages, a video's frames);
a stopped loop or an ended context waits for the puts in flight before `Dispatch` returns. A put
that fails with `ErrTooLarge`, which a provider maps from its store's, fails the part, recorded;
any other put error stops the dispatch, as an index failure does. A stage that knows a found
part's SHA-256 claims it on `Found` (`Found.SHA256`), as a reader's medium named by the envelope's
media rule is known, and the dispatcher checks the claim as it checks `Input.SHA256`, against the
hash it takes anyway: a mismatch, a file a killed tool left short, fails that child as the run's
fault. The dispatcher never reads a name as a hash; the media name rule is `internal/cli`'s
alone.

An output is appended before its event is yielded, and a part's entry is recorded before the
part's own event, so an attempt stopped at that event still leaves the entry. An event's files are
the caller's to read, link, or move until its body returns, and the dispatcher then removes what is
left, a quarantined part's file included; nothing the body does reaches the index.

A seen part replays its entry's log from the index, in the order it was prepared ("Index"). A
replayed output's event carries its `SHA256` and no file: a consumer opens its content from
`Content` only when it needs the bytes. A replayed output's or warning's event carries `Outcome`
seen and the entry's outcome in `Recorded`. A replayed child's part is its recorded `Part` with its
`ID`, `Parent`, and `Depth` the replaying walk's: its parent's ID followed by its recorded `Where`,
since the same content can sit at another place. A replay of a resumed input skips its log's prefix:
every item through the item whose `Recorded.After` equals `Input.After`, so an item with no `After`
is skipped only when it precedes that item; when no item's does, the dispatch ends with an error, as
mail refuses an id that names no message. A replay checks the outputs and children it replayed
against `Entry.Outputs` and `Entry.Children`, a mismatch ending the dispatch as an index failure,
and replayed parts count toward `Limits` as prepared ones do, a replayed part past a cap Limited and
its entry kept.

A part is seen when its entry's `Stages` each equal the current identity of the stage of that name,
one check per stage the subtree used; `New` maps each name to its wired stage and refuses two
stages that share a name with different identities. An entry found but not fresh is dropped, then
claimed. A replayed child recorded prepared, encrypted, no processor yet, or seen is looked up
under its route's current stage and replays its own log; a child of any other outcome replays as
recorded. A part resolves its children one level down before it yields its first event, looking up
each that needs it without recursing. A miss (a child whose entry is gone or not seen, or a stage
wired since for a child that had none) is resolved by the nearest part that has a file: a
replaying part with a file (the input, or a part a stage found in this dispatch) drops its own
entry, claims it, and is prepared as not seen, its stage finding the children again with files,
the live ones replaying and the missing one prepared; a replaying part with no file passes the miss
up to its parent's resolve. A miss met deeper, after events are out, is resolved the same way, and
the events already replayed repeat as prepared events. A dispatch may therefore repeat a part's
events: events are at-least-once, so a consumer counts by part ID and cursor, never by event. A
replay costs a lookup per replayed child, O(parts).

Flags have two sources. A part's content flags are recorded in its `Entry`, its stage's flags
(`Item.Flag`) among them; its name flags come from `inspect.Name` at every occurrence, seen or not.
Its verdict is taken over both under the current `Options`, so a seen part whose recorded flags now
come to quarantine is Quarantined, with no replay; a policy relaxed since changes nothing until the
entry ages out, and a new check applies to content prepared after it ships. The policy is in no
stage's identity; the bomb rule (`Options.Bomb`) is in the archive and document stages'.

A stage's identity is derived, never assembled: its format package's request, checked and with
every default filled, hashed whole, a field that doesn't change the output (a directory, a path,
concurrency) tagged out of the JSON where it is declared; the tools' versions, from each tool's
whole version line in a fixed order, so a development build and a release differ; and the
adapter's own version. The schema a reader decodes and the bomb rule are fields of what is hashed.

Each stage names where it found a part (`Found.Where`), stably across runs, as processing's do:
mail's `<message>, attachment n`, a ZIP's `zip entry <name>`, and a medium a tool's line lists
`<line>, media <n>`, n its index in the line's `media`. Before its uniqueness rule, the dispatcher
normalises a `Where`: invalid UTF-8 and C0 and C1 controls percent-encoded byte by byte, and one
past 255 bytes cut so that the hex of its SHA-256's first 8 bytes, 16 digits, fits after it within
the 255. Uniqueness among siblings is each stage's to give, stated in `Stage`'s contract; the mail,
onenote, and office stages give it by construction, and the dispatcher keeps the sibling set only
for a stage that implements `UntrustedWhere` (the archive stage, and the document stage for its
attachments), whose siblings `inspect.Options.Entries` bounds. There, a `Where` a sibling already
has gains ` (n)`, n the smallest from 2 that no sibling's `Where` already takes, repeated until it
is unique. A part's `ID` is the chain of `Where` from the input: the input's ID is `/`, and a found
part's is its parent's ID followed by its `Where` as one more path segment, `%` and `/`
percent-encoded, after a `/` unless the parent's ends in one. So the ID names one occurrence of
content in the input, stable across runs, where the content's SHA-256 can't.

Every string of the wire shapes leaves the dispatcher under one rule, applied before an event is
yielded or an item appended: valid UTF-8, no U+0000, no C0 or C1 control but tab and line feed in a
reason, and a stated cap per field (a name, a `Where`, a reason, a warning's parts, a flag's
evidence, a Meta value); a string past its cap is cut as a `Where` is. The wire's package is
`encoding/json` for `dispatch`'s shapes and the index; a record output is the bytes its stage made,
and its `MediaType` names that shape and its version, so a reader checks what it decodes. Every
`Meta` key a stage sets (`MetaPage`, `MetaCount`, `MetaWindow`, `MetaSheet`, `MetaDPI`, and the
rest) and every record output's media type (`MediaAudioPlan`,
`application/vnd.docproc.audio-plan+json; version=1`, and a tool's lines under that tool's media
type with `schema=2`, such as `MediaMail`) is an exported constant in `dispatch`, beside `Output`
and `OutputKind`, each documented with the stage that writes it and its format, and versioned by
`dispatch.Version`; `stages` writes them, and a consumer reads them without importing `stages`. No
window carries `count`: a part's windows are cut as it decodes, and its plan record, last, counts
them.

`dispatch` declares the JSON shapes of what it yields beside its other types, on the standard
library alone: an event, its part, its stage's `Identity`, an output, a warning, and the stage table
a host reports (`Tool`, `Row`, `State`). `Event` is itself the shape, one for each of its three
kinds: a part's own event, an output's, and a warning's, each setting only its own fields, so no
parallel wire type copies it. `Part`'s local fields are left out of the JSON, and `Parent`
carries its parent's `ID`; what routing read of the part is its `ContentType`. `Identity` is a
stage's JSON, and `Stage` the interface.

```go
package dispatch
// Version is "1", grown by addition only. It versions the shapes below and the vocabularies
// they carry (route's Kind and Route; inspect's Check, Class, and Flag), the index's shapes
// (Key, Entry, Recorded, Child), the stage table, the Meta keys, and the record media types.
const Version = "1"
type Event struct {
    Part     Part     `json:"part,omitzero"` // left out when zero: rundir's error line
    Stage    Identity `json:"stage,omitzero"`
    Output   *Output  `json:"output,omitempty"`  // an output's event
    Warning  *Warning `json:"warning,omitempty"` // a warning's event
    After    uint64   `json:"after,omitzero"`    // an output's: the store's cursor, Item.After
    Outcome  Outcome  `json:"outcome,omitzero"`  // a part's own; seen on a replayed output's
                                                // or warning's
    Recorded Outcome  `json:"recorded,omitzero"` // a seen event's: its entry's outcome
    Reason   string   `json:"reason,omitzero"`   // a failed part's holds Err's text
    Err      error    `json:"-"`
    Outputs  int      `json:"outputs,omitzero"`  // a part's own, from here down
    Children int      `json:"children,omitzero"`
}
type Part struct {
    ID          string         `json:"id"`              // the chain of Where from the input
    Name        string         `json:"name"`
    Size        int64          `json:"size"`
    SHA256      string         `json:"sha256"`
    ContentType ContentType    `json:"content_type"`
    Flags       []inspect.Flag `json:"flags,omitempty"`
    Parent      string         `json:"parent,omitzero"` // its parent's ID
    Depth       int            `json:"depth"`
    Where       string         `json:"where,omitzero"`
    Path        string         `json:"-"` // its file, while its stage runs
    After       uint64         `json:"-"` // a store's resume, mail's --after
}
type ContentType struct {
    Kind        route.Kind  `json:"kind,omitzero"`
    Route       route.Route `json:"route,omitzero"`
    RouteReason string      `json:"route_reason,omitzero"`
    Stored      string      `json:"stored,omitzero"` // the stored MIME type
}
type Identity struct {
    Name    string `json:"name"`
    Version string `json:"version"`
    Config  string `json:"config"`
}
type Output struct {
    Kind      OutputKind        `json:"kind"` // text, markdown, image, record, audio
    Name      string            `json:"name,omitzero"`
    MediaType string            `json:"media_type,omitzero"`
    Meta      map[string]string `json:"meta,omitempty"`
    SHA256    string            `json:"sha256"` // its content's, by which Content holds it
    Path      string            `json:"-"`      // local: its file, while its part runs
    Value     []byte            `json:"-"`      // local: its bytes, when it has no file
}
type Warning struct {
    Tool  string `json:"tool"`
    Where string `json:"where"`
    What  string `json:"what"`
}
type Tool struct { // a tool stages.Build tried
    Program string `json:"program"`
    Minimum string `json:"minimum"`
    From    string `json:"from,omitzero"`    // the path it was found at, or its image
    Version string `json:"version,omitzero"` // its whole version line
    Error   string `json:"error,omitzero"`   // Err's text
    Err     error  `json:"-"`
}
type State string // wired, missing, excluded; a missing or excluded route has no processor yet
type Row struct { // one route's line of the stage table
    Route  route.Route `json:"route"`
    State  State       `json:"state"`
    Stage  Identity    `json:"stage,omitzero"` // the wired stage's
    Tools  string      `json:"tools,omitzero"` // its tools and their versions
    Reason string      `json:"reason,omitzero"`
}
```

A count left out is zero, as the common envelope's absence rule has it. `Err` stays local: a
failed part's `Reason` already holds its text, as processing sets both. `Output.Path` and `Value`
never marshal: an output's content is found by its `SHA256` in `Content`, so the index records an
output without them and nothing clears them. `Tool.Err` stays local beside its text in `Error`, as
`Event.Err` beside `Reason`. `ContentType` holds what routing read of a part, never a host's own
sniffing of an input, which is the host's record's (`rundir/schema`'s `File`).

`stages` is docproc's shipped wiring, so `cmd/docproc` and Cerebro wire the same stages from the
tools a host holds. It holds the stage adapters, unexported: a stage has its tools or doesn't
exist, so no adapter has a partial mode.

```go
package stages
type Lookup func(program string) (string, error)
// Build refuses only options it can't take, inspect's Validate and a request's own check, with
// an error wrapping ErrRequest.
func Build(ctx context.Context, lookup Lookup, opts Options) (Set, error)
func Routes() []route.Route                             // the ten routes, in the table's order
func ParseRoutes(names []string) ([]route.Route, error) // an unknown name matches ErrRequest
type Options struct { // the zero value wires every route, with no bound
    Exclude     []route.Route
    Inspect     inspect.Options // validated once; the archive and document stages' bomb rule
    Timeout     time.Duration   // Spec.Timeout of the unit tools: gs, qpdf, pdfdetach, magick,
                                // soffice, docproc-ooxml, ffprobe
    Idle        time.Duration   // Spec.Idle of the stream tools: ffmpeg, docproc-mail,
                                // docproc-onenote, 7zz
    Memory      int64           // tool.Spec.MemoryLimit for every Spec Build builds
    Env         []string        // tool.Spec.Env
    Concurrency int             // a PDF's pages and an audio part's windows held; zero is one
    Document    document.ImagesRequest // each stage's request, its own defaults where zero;
    Picture     picture.Request        // a part's own fields (Dir, Path, Kind, Stem) are the
    Audio       audio.Request          // part's; Document.Text is false, no text layer read,
    Video       media.Request          // unless the host sets it; Document.PagesPerProcess,
                                       // outside the identity, is the most pages one gs
                                       // process takes, zero being 8
    Magick      picture.Limits  // ImageMagick's -limit values on every magick run
}
type Set struct {
    Stages dispatch.Stages; Tools []dispatch.Tool; Rows []dispatch.Row // a row per route
    Inspect inspect.Options // for dispatch.Config.Inspect, so one value exists
}
```

A host that requires a route reads `Set.Rows` and fails its own startup. The library ships no
policy: each bound is none unless the host sets it, or the format's own default, and
`cmd/docproc`'s configuration carries the operator's starting values, Cerebro's its own. Each
bound has one owner, and passing it has one outcome:

```text
per dispatch      parts, bytes, depth               dispatch.Limits     the part Limited, unrecorded
per archive       bomb ratio and size, per entry    inspect.Options     a flag; in the archive and
                  and summed; entries listed                            document stages' identities
per part's        pages, frames, pixels, a frame's  the stage's         prepared with what it has and
  outputs         largest side                      request             a warning, recorded; in its
                                                                        identity
per run           time (Timeout, Idle), memory,     tool, from Options  classified as above; in no
                  processes                                             identity
per process       puts in flight, concurrency,      dispatch.Config,    no outcome
                  the release timeout               Options
```

### Content

The content store holds every output's content by its SHA-256, and a host puts its own texts in
it the same way (docinfer's `job`, its inference texts).

```go
package dispatch
type Content interface {
    // Put stores r's bytes as the content sha256 names, all or nothing, when that content is
    // absent or older than the store's renew age; otherwise it skips the write.
    Put(ctx context.Context, sha256 string, r io.Reader) error
    // Open opens the content sha256 names; a miss matches fs.ErrNotExist.
    Open(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
func NewFileContent(dir string, renew time.Duration) (*FileContent, error) // 0 never rewrites
func NewMemoryContent() *MemoryContent
// ContentFile is the content sha256 names' file, relative to a FileContent's directory: its first
// two hex digits, then the whole name. The layout is stable, a reader that isn't Go following it.
func ContentFile(sha256 string) string
```

The interface has no renewal of its own. An implementation reads a content's age from its
store's modification time (`FileContent` its file's, `docproc/storage` go-storage's
`Object.ModifiedAt` from `Stat`), so a content prepared again is rewritten once it is older than
the renew age, and its age restarts. A put is all or nothing: `FileContent` writes a temporary
file in its directory and renames it to `ContentFile`'s path, and a reader never sees part of a
content.

The host sweeps content by age, never by reference. Its retention is at least the index entries'
retention plus the renew age plus the job stream's retention: every put an entry's preparation
makes follows the entry's creation, from which its age runs ("Index"), and leaves its content
younger than the renew age, so a content is never older than an entry naming it plus the renew
age, however long the stage runs, and no live entry names a swept content. Cerebro's retentions
are the `state` task's ([`cerebro.md`](cerebro.md), "State and subjects").

A content missing within that bound, deleted by hand or swept early under clock skew, is outside
the contract. docproc can't see one: a replay opens no content, its outputs carrying only their
SHA-256. docinfer finds it when it opens the content, on a cache miss or to chunk its text, and
its host acts on it (docinfer's `job` package doc).

`cmd/docproc`'s run directory holds its content in a `FileContent`; Cerebro's is blob storage
through `docproc/storage`, which maps go-storage's `ErrTooLarge` (an object past the store's
`MaxObjectSize`) to `dispatch.ErrTooLarge`, so the part fails recorded ("Dispatch").

### Index

The index is content-addressed by a part's SHA-256 and its stage's identity. One claimant prepares
the content, and every other claimant waits for its entry, so replicas sharing an index prepare
content once. Cerebro backs it with PostgreSQL through `docproc/postgres`; `MemoryIndex` and
`FileIndex` stay for tests and `cmd/docproc`. `dispatch/dispatchtest` holds the contract as
conformance suites, `Index(t, new)` and `Content(t, new)`, with the fixture stage, which every
implementation and both providers run.

```go
type Key struct { SHA256 string; Stage Identity }
type Index interface {
    Lookup(ctx context.Context, key Key) (Entry, bool, error)
    Items(ctx context.Context, key Key) iter.Seq2[Recorded, error] // the log, in append order
    Claim(ctx context.Context, key Key) (token string, lease time.Duration, ok bool, err error)
    Renew(ctx context.Context, key Key, token string) error
    Wait(ctx context.Context, key Key) (Entry, bool, error)
    Append(ctx context.Context, key Key, token string, r Recorded) error
    Record(ctx context.Context, key Key, token string, e Entry) error
    Release(ctx context.Context, key Key, token string) error
    Drop(ctx context.Context, key Key) error // an entry and its log, so its content is prepared again
}
func NewMemoryIndex(lease time.Duration) *MemoryIndex // a zero lease never expires
func NewFileIndex(dir string) (*FileIndex, error)
type Entry struct {
    Outcome  Outcome        `json:"outcome"`
    Reason   string         `json:"reason,omitzero"`
    Outputs  int            `json:"outputs,omitzero"`
    Children int            `json:"children,omitzero"`
    Stages   []Identity     `json:"stages"`          // the distinct stages its subtree ran
    Flags    []inspect.Flag `json:"flags,omitempty"` // the part's content flags
}
type Recorded struct { // one item of an entry's log; one of Output, Warning, Child is set
    Output  *Output  `json:"output,omitempty"`
    Warning *Warning `json:"warning,omitempty"`
    Child   *Child   `json:"child,omitempty"`
    After   uint64   `json:"after,omitzero"` // an output's: its Item.After
}
type Child struct { // a found part, as its parent's entry records it
    Part    Part    `json:"part"`
    Outcome Outcome `json:"outcome"`
}
```

An entry's outputs, warnings, and children are one log, appended as the part's stage yields them and
read back in that order, so a replay is the preparation's order. `Child` holds the found part's
`Part` whole, so a replayed child's part is the part as found, less its place: the index keeps its
`Where` and ignores its `ID`, `Parent`, and `Depth`, which the replaying walk sets ("Dispatch").
`Path` and `After`, local, are never recorded. `Key`, `Entry`, `Recorded`, and `Child` are versioned
by `dispatch.Version` with the event shapes, since a provider stores them and a replay reads them
back; `dispatch`'s goldens hold an entry's log beside a dispatch whose part is routed and flagged.

A claim is a lease, and every implementation honors one contract. `Claim` returns the lease with the
token, since the lease belongs to the claim the index granted, and a provider that sets it per claim
needs no second call. A claim lasts its lease from its last `Claim` or `Renew`, and is held from
`Claim` to `Record` or `Release`, through every yield of the part's subtree, the caller's loop body
included. One renewer goroutine per dispatch holds the claims in flight, the ancestor chain, and
renews each at a third of its lease whatever the walk's goroutine is doing; it ends with the
dispatch. A claim not renewed within its lease may be taken over by another claimant, and from then
on the first token's `Renew`, `Append`, and `Record` fail. A renewal the index refuses ends the
dispatch with an error matching `ErrClaimLost`, which a host treats as transient, and an index
refuses an old token's call with an error wrapping `ErrClaimLost`, as dispatchtest's suites check:
the redelivery waits on the new claimant and replays. A takeover, or a `Release`, discards the log
items the old token appended. After the dispatch's context ends, `Release` runs under
`Config.Release` and its failure is dropped, since the lease's end releases the claim anyway;
`Record` runs under the dispatch's own context, so an ended context records nothing.

An append needs no durability before `Record`: a provider may buffer the log, flushing in batches
and at `Record`, which commits the log whole or fails, and a lost claim surfaces at a flush, a
`Renew`, or `Record`. The dispatcher appends from the walk's goroutine alone. `Items` fails when
its entry is replaced or dropped while it reads.

`FileIndex` is single-process: its claims are `MemoryIndex`'s in-memory table, and only entries
and logs are files, a log written beside its entry under a temporary name and renamed at `Record`,
its directories sharded by the name's first two hex digits. A claim never expires while its
process lives, so `Claim` returns a lease of zero, none to renew. Two processes sharing a
directory may each prepare the same content, the later record replacing the earlier whole.

`docproc/postgres` holds one row per entry, keyed by its `Key`, with its claim's token and lease,
and one row per log item, keyed by its entry, the token that appended it, and its position in the
log. A claim, a renewal, a flush of appends (multi-row), and a record are each one conditional
statement on the entry's current token, so fencing is the database's. The module ships its schema's
migrations, as go-messaging's `postgres` does, and the host applies them. The host sweeps entries
by age: over `docproc/postgres` with its `Retention`, `NewRetention(db, age)`, whose `Pass(ctx,
budget)` drops at most the budget of entries older than the age, oldest first, with their logs,
and never a claim under a live lease, a pass from the host's sweeper; over `MemoryIndex` and
`FileIndex` with `Drop`. The retention is the `state` task's, under "Content"'s bound. An entry's
age runs from its creation, its first claim, and neither a replay nor `Record` refreshes it.

### Event shapes

The package that produces a change owns its event kinds and their data types: the kinds in its
`messaging.go`, the data types beside its other types, as JSON on the standard library alone.
go-web-service's `domain/organization/messaging.go` and `domain/document/messaging.go` hold their
`event.Define[T]` calls, a `Recorder` alias, and the raise and emit helpers that emit them, over
data types in the package's `entities.go` (`OrganizationData`, `OrganizationDeletedData`,
`LogoUploadedData`, `LogoDeletedData`; `DirectoryData`, `DirectoryDeletedData`, `FileUploadedData`,
`FileDeletedData`), and `admin/database/messaging.go` defines its event over `string`, with its
`Recorder` alias and the `OnReset` hook that emits it. No separate events package exists; go-core's
`event` is the CloudEvents envelope alone.

docproc and docinfer are libraries that emit no CloudEvents themselves, so neither declares event
kinds or has a `messaging.go`. Their shapes sit in their producing packages' ordinary files:
docproc's are `dispatch`'s (above), versioned by `dispatch.Version`, and docinfer's are its root
package's and `job`'s (docinfer's README, "Packages"), both versioned by `docinfer.Version`.
Cerebro's `messaging.go` declares its `cerebro.job.*` kinds, each over one of these or over its own
data types ([`cerebro.md`](cerebro.md), "Events"). The spike's docproc `schema` shapes
(`spike-ai-pipeline/internal/docproc/schema/part.go`, `record.go`) move into `dispatch` and
`inspect`, and `rundir/schema` aliases them ("The nested modules"); the spike's docinfer `outdir`
shapes move into docinfer. The shapes grow by addition only, as the tools' contracts do.

## Changes per tool and per package

What each promotion changes from its spike, derived from the sections above; the task that
builds it is in brackets. Every tool moves to schema "2" and
the common envelope, and each re-records its goldens under "2" as one `.jsonl` and one `.stderr`
per fixture, compared byte for byte, which tests its determinism.

### docproc-ooxml [ooxml]

Built at `tools/ooxml/`: its README states the tool, and its CHANGELOG how it differs from
spike-ai-pipeline-office 0.1.0. What it leaves to later tasks:

- **A tool's wiring.** Each tool's `mise.toml` pins its toolchain alone and defines `check`,
  `publish`, `golden`, `currency`, and `upgrade`, and `GLIBC_FLOOR` when its binary links glibc;
  the root's `TOOLS` lists it. CI's tool matrix runs the tools whose path filter in the `changes`
  job matched, each tool with its own filter. `tools/pack.sh` and the release workflow take any
  tool by its name.
- **Memory.** Its peak resident memory over ~/docs is 42.8 MiB (`acceptance.md`, "Peak memory");
  its arm64 address-space floor is unmeasured.
- **Unread kinds.** Reading text boxes, equations, SmartArt, pptx comments, and xlsx notes is
  backlog; each kind a document holds is one warning.

### docproc-mail [mail]

Built at `tools/mail/`: its README states the tool, and its CHANGELOG how it differs from
spike-ai-pipeline-mail 0.3.1. What it leaves to later tasks:

- **Shared crate.** The docproc-envelope crate at `tools/envelope` holds the Rust tools' common
  envelope and the content rule; its `src/lib.rs` lists the parts. It has no lock or toolchain of
  its own: each Rust tool's check formats, lints, and tests it against that tool's Cargo.lock, and
  each Rust tool's CI filter takes the `&envelope` anchor, the crate's directory and
  `tools/notices-rust.txt`, so a change to either runs every Rust tool and no .NET one. Each Rust
  tool's `upgrade`, through `tools/currency.sh`, raises the crate's requirements with its own and
  re-locks its own graph. docproc-mail and docproc-onenote both use it.
- **Holds.** A tool's hold list is `HOLDS` in its `mise.toml`'s `[env]`, one
  `<item> <pin>: <reason>` per line, which `upgrade` skips and `currency` reports as held; a hold
  names its pin, so moving the pin by hand ends it. mail's list holds its fixture generator's
  MimeKit at 4.18.2, and onenote's holds typed-path at 0.12.4.
- **Notices.** `tools/notices.sh`'s cargo branch runs cargo-about, pinned in each Rust tool's
  `mise.toml`, against the accepted licenses in the tool's `about.toml`; mail's accepts MIT,
  Apache-2.0, BSD-3-Clause, and Unicode-3.0. cargo-about walks proc macros' build-time
  subtrees too, so the notices name those crates.
- **Code pages.** A DOS code page the review of ~/docs finds, which the `codepage` crate doesn't
  know, becomes a table in the tool, as US-ASCII (20127) is in `src/codepage.rs`.
- **Memory.** Its peak resident memory over ~/docs is 3.7 MiB (`acceptance.md`, "Peak memory").

### docproc-onenote [onenote]

Built at `tools/onenote/`: its README states the tool, and its CHANGELOG how it differs from
spike-ai-pipeline-onenote 0.1.0. Nothing is asked of onenote_parser upstream: each workaround of
its gaps is the tool's own tested code. What it leaves to later tasks:

- **Parser pin.** onenote_parser is pinned `=2.1.0`, since the workarounds rest on that release's
  exact text and logic, but it isn't held: an `upgrade` that moves it is a reviewed diff, which
  the workaround tests and the goldens check. typed-path is held at 0.12.4 and moves to 0.13.0
  only with onenote_parser.
- **Memory.** The read thread's stack is an address-space reservation of at most a quarter of
  RLIMIT_AS (README, "Limits"), so `cmd/docproc`'s memory-bound starting value leaves room for
  it. Its peak resident memory over ~/docs is 10.6 MiB (`acceptance.md`, "Peak memory"); a
  per-tool timing, if one is wanted, is one harness for all three tools.
- **Unicode.** docproc-envelope's general-category predicates (Cf, Zs, N*, Sm), which the math
  port reads, make the crate the Rust tools' one Unicode home; the Go and .NET sides are later
  tasks'.

### docproc's packages [library]

Built as `github.com/standards-lab/docproc` from spike-ai-pipeline-processing v0.9.0 and the
spike's `stages`: the README states the packages, the tools they run, and the limits, each
package's doc comment its exported names, and the CHANGELOG how v0.1.0 differs from the spike.
The integration tier, `mise run test:integration`, builds the three docproc tools from the same
checkout and runs on the developer's machine before each Go tag. What it leaves to later tasks:

```text
rundir,             built [docproc]: public packages of this module, since its v0.2.0; one Dir from
  rundir/schema     Open, which reads and creates nothing, or Create, which makes the directory,
                    rundir.json, the index, and the content store; Runs, Latest, Manifest, Record
                    an iter.Seq2 ending on ErrNotEnded for an unended run, Content, Kept,
                    ContentPath, and ReportDir, a run's report directory, added by reports in
                    v0.3.0; the writer's Index, Begin, and Quarantine, and Run's Write and End;
                    rundir/schema shapes alone, the Event line, the Manifest with its Build,
                    File with detected, Span, and Time, made by TimeOf in UTC, importing no docproc
                    package but dispatch and inspect; rundir/testdata one committed schema "2" run
                    directory, read and written again, compared as text
storage (module)    built [providers]: storage.New(Config{Client, Prefix, Renew}), dispatch.Content
                    in object storage over go-storage's Client at <prefix>ab/<sha256>, go-storage's
                    ErrTooLarge mapped to dispatch.ErrTooLarge; no sweeper, the host's lifecycle
                    rule on age
postgres (module)   built [providers]: postgres.New(ctx, Config{DB *sqlate.DB, Lease, Flush}),
                    dispatch.Index in PostgreSQL over sqlate, an entry's row with its flags and its
                    log items' rows, inserted in multi-row statements, its claims, leases, and
                    fencing as conditional statements, Items read under the entry's token, Wait by
                    LISTEN/NOTIFY on one listener connection per Index until Index.Close; its
                    migrations, Migrations, with Verify and Statements; NewRetention(db, age),
                    whose budgeted Pass drops aged entries oldest first
```

The memory bound has no default: `tool.Spec.MemoryLimit` and `stages.Options.Memory` are none
until the host sets them. `cmd/docproc`'s starting value, 2 GiB, stands: no part of a ~/docs run
fails by it, every tool peaks under it, and neither of PI-1's bombs moves a starting bound
(`acceptance.md`, "Peak memory", "Bombs").

Built [acceptance]: a PDF's pages share gs processes. At concurrency C, pages k, k+C, k+2C, and so
on go to the k-th of C concurrent processes through gs's page list, at most
`document.ImagesRequest.PagesPerProcess` pages to a process, a count outside the stage identity as
`Concurrency` is; zero is 8, the smallest count within 5% of the fastest on the 915-page CFR volume
(cfr-2024-title32-vol1.pdf), and a negative count is refused. A page list writes consecutive pages
as ranges and ends a batch where it would pass the 2047 bytes gs takes in an argument. A portfolio,
a one-page batch, and a page `MaxPixels` lowers take the per-page path, and a shared process that
fails on the content leaves each undelivered page a process of its own. `DOCPROC_UNIT_TIMEOUT`
bounds a shared process as an idle bound between its pages. Under the container runner, which
mounts only the PDF's directory, text is read a page a process while renders share. Every page's
PNG and text is identical to the per-page path's (`acceptance.md`, "PDF pages in shared gs
processes").

Built [hardening]: each exec-runner run's `TMPDIR` is a directory of the run's own under the
host's `TMPDIR`, removed once the program is reaped, so a gs ended early leaves nothing beside its
input ("Running the tools"). A portfolio page's own gs runs read the PDF through the non-portfolio
half of gs's `runpdf`, so the page is its cover's, one PNG and that page's text; the stage
adapters' version is 6, so an index prepares its content again. A ZIP holding
`FixedDocumentSequence.fdseq`, whole or as its first piece, routes `Unrouted` as an XPS package,
whatever its name. The three gaps acceptance left are built: soffice's exit 77, LibreOffice's
fatal error at its start, a full disk among its causes, is the run's failure, retried ("Dispatch");
`sevenzip`'s `Extract` returns its writer's error rather than 7zz's broken pipe; and the email
stage's release test dispatches a generated mbox of 100 messages, each with a distinct 64 KiB
attachment, and asserts the working directory's peak stays below the attachments' sum.

### The nested modules

From the spike root's `cmd/` and `internal/`; the third column names the spike packages each
module is built from. `cmd/docproc` is a nested module of docproc's repository, so the library
requires neither go-cli-sdk nor go-core.

```text
docproc/cmd/docproc    [docproc]   built from cmd/docproc and internal/docproc/{app, apptest,
                                   run}: main.go at the module's root; internal/app,
                                   internal/apptest, and internal/run; and integration, the
                                   built-binary tier's harness (below); requires docproc,
                                   go-cli-sdk, go-core, and golang.org/x/sys; app and apptest
                                   started from go-cli-sdk-template's template/v0.3.0, which pins
                                   go-cli-sdk v0.4.0, not ported; the graph's node the stage
                                   builder, stages.Build over exec.LookPath, which apptest.Tools
                                   and apptest.Stages replace, export_test.go gone; wires
                                   docproc's stages, keeps one table writer; run collects the inputs
                                   before it builds the stages, follows a named argument that is a
                                   symbolic link, and fails on one that is neither file nor
                                   directory, or on arguments that yield no input; run is a runner
                                   struct with one method, its recorder writing one line per event,
                                   replay.go, its subtrees, and the warnings counters and row gone;
                                   run holds one fileOf, over Statx on golang.org/x/sys, and detect,
                                   from schema's Statx, FileOf, Stater, and Detect, filling File's
                                   detected beside Statx, the birth-time probe its one
                                   export_test.go hook; an input's time from its first event to its
                                   last, on the loop's goroutine; copies a quarantined part's file
                                   in its loop body, Keep gone, and stores outputs in the run
                                   directory's FileContent, its output handler writing the line
                                   alone; stops the run, exit 1, on a store's failure, failed parts
                                   and error inputs exit 0, stated in the help, the summary's last
                                   line counting both; --workers at least 1; its configuration ships
                                   the operator's starting bounds, which the library leaves none:
                                   dispatch.Limits (parts, bytes, depth), Config.Release,
                                   stages.Options' Timeout, Idle, Memory, and Concurrency, and the
                                   outputs' caps in each stage's request (pages, frames, MaxPixels,
                                   a frame's MaxSide); docproc's README names stages as the shipped
                                   wiring and cmd/docproc its first caller; a committed go.work
                                   over the base module and cmd/docproc, carrying changes between
                                   the base module's tags
                       [reports]   built: internal/report, `docproc report <run-dir>
                                   [--run <id>]` and `docproc run --report`, on schema "2" in
                                   place of internal/docproc/report's "1" pages; index.html and
                                   one <sha256>.html per distinct input content, written into
                                   the run's report directory, runs/<run>/report/, which
                                   rundir's package doc places in the run directory's layout and
                                   Dir.ReportDir names; Render over rundir's handle, its golden pages over a
                                   committed run directory; the base module tagged v0.3.0 for
                                   ReportDir, then cmd/docproc/v0.1.0; internal/report's package
                                   doc states the pages
                       [acceptance] built: DOCPROC_AUDIO_LIMIT, stages.Options.Audio's Limit,
                                   starting 30s; a value of 4 s or less, not over twice the 2 s
                                   overlap, or one that doesn't parse, fails the run before any
                                   tool is looked up; whether audio that fits one model request
                                   goes unsplit is the host's choice, made by setting it to the
                                   model's request size, docproc's own default staying 30 s
```

`cmd/docproc`'s `main.go` sits at its module's root, so
`go install github.com/standards-lab/docproc/cmd/docproc@<version>` installs it, and its packages
sit under `internal/`: `app` and `apptest`, the composition root and its fixtures, and `run` and
`report`, its command packages. Its one package outside `internal/` is `integration`, the
built-binary tier's harness, as go-cli-sdk-template's template/v0.3.0 places it. The module's one public surface is the command, and the logic an integrator calls is the
library's, so it is an exception to Go Elemental's command packages at a module's root level,
which `STANDARDS.md` states; go-web-service's `tools/slab` layout would make the install path
`cmd/docproc/cmd/docproc` and `run.Commands` API under its tags.

`rundir` and `rundir/schema` are public packages of docproc's base module, with no module or tag
of their own. `cmd/docinfer`, in docinfer's repository, reads `cmd/docproc`'s run directory
through them, and Go's internal rule is by import path, so docproc's `cmd/internal` would bar
it. They version with the library, which both commands require, on the base module's tags
(`design.md`, "Releases").

```go
package rundir
// Open opens the run directory at root to read it, creating nothing: it fails with
// fs.ErrNotExist when root is no run directory, and refuses another schema, naming both.
func Open(root string) (*Dir, error)
// Create opens root to write it, making the directory, its rundir.json, the index, and the
// content store when they are absent; over an existing directory it refuses as Open does.
func Create(root string) (*Dir, error)
func (d *Dir) Runs() ([]string, error)                         // its runs' ids, in order
func (d *Dir) Latest() (string, error)                         // the latest run that ended
func (d *Dir) Manifest(id string) (schema.Manifest, error)
func (d *Dir) Record(id string) iter.Seq2[schema.Event, error] // its lines, in order
func (d *Dir) Content() dispatch.Content                       // the run directory's FileContent
func (d *Dir) Kept(sha256 string) (string, bool, error)        // quarantine/<sha256>, if kept
func (d *Dir) ReportDir(id string) (string, error)             // runs/<run>/report, not made
func ContentPath(sha256 string) string // a content's file from the root: the content directory
                                       // joined to dispatch.ContentFile(sha256)
// The writer's, on a Dir from Create:
func (d *Dir) Index() dispatch.Index
func (d *Dir) Begin(m schema.Manifest) (*Run, error)
func (d *Dir) Quarantine(sha256, path string) (string, error) // quarantine/<sha256>, kept once
func (r *Run) ID() string                                     // a version 7 UUID
func (r *Run) Write(e schema.Event) error                     // one line, written through
func (r *Run) End(err error) error
```

One process writes a run directory. Its root's `rundir.json` holds its schema, and every method
that takes an id refuses one that isn't a run of the directory, so no caller checks an id first.
`Begin` removes the `.work-*` directories a killed run left. A record is appended, never renamed:
each line is written through as it is made, so a killed run's record loses at most its last line,
and a reader of a run whose manifest has no `end` drops a last line with no newline; `Latest`
names only a run that ended, and a reader given an unended run by id says so. `Record` yields the
lines in order and ends on the first that doesn't decode.

The run directory is schema "2", which includes `dispatch`'s shapes at `dispatch.Version` "1", so
a break there is a break here. `schema` aliases those shapes (`dispatch.Part`, `ContentType`,
`Identity` as its `Stage`, `Output`, and `Warning`, and `inspect.Flag`), so no shape is defined
twice, and a part's `parent` is its parent's ID, a change of meaning "1" can't take. The
manifest's tools and stage table are `dispatch.Tool` and `dispatch.Row`, which `stages.Set` holds
and `cmd/docproc`'s `run` copies in, so `schema` imports no docproc package but `dispatch` and
`inspect`, and a reader of the run directory, `cmd/docinfer` among them, never compiles `stages` and
the format packages beneath it. An output's content is in the run directory's `FileContent`, found
by its `sha256` in place of "1"'s `ref` path under `outputs/`; the content store's directory and a
content's path in it (`ContentPath`, the content directory joined to `dispatch.ContentFile`'s
layout) are part of "2", since the report links a content's file and a reader that isn't Go follows
it.

The record holds one line per event, of four kinds: `part`, `output`, `warning`, and `error`, an
input whose dispatch stopped before its own event. A `warning` line is a warning event dispatch
yields; "1"'s `warning`, a string the command wrote on a part line when no record held a seen
part's children, is gone with the copy that wrote it, since the dispatcher replays children. A reader
may tell the first three kinds by which of `output` and `warning` is set.

```go
package schema
type Event struct {
    Kind  string `json:"event"` // part, output, warning, error
    Input string `json:"input"` // the input's own absolute path
    dispatch.Event              // its fields inline, its Part left out on an error line
    File       *File  `json:"file,omitempty"`      // an input's own part line
    Quarantine string `json:"quarantine,omitzero"` // quarantine/<sha256>, while it is kept
    Time       *Span  `json:"time,omitempty"`      // an input's: its first event to its last
    Error      string `json:"error,omitzero"`      // an error line's text: Err never marshals
}
```

`quarantine` names the kept file whenever it exists, whether this event copied it or an earlier run
did, and the flags are the line's, with no `flags.json`. The record's other shapes, `File`, `Span`,
and the manifest, are its own; `File` holds what `cmd/docproc` knows of an input's file beside the
part, its `detected` the sniffed MIME type, filled by `run` beside `Statx`. The manifest holds
`schema` and `run` for a manifest read alone, no `record`, whose path is the layout's
(`runs/<id>/record.jsonl`), `docproc`, a `Build` of the command's module version and the
library's from the build info, and `collected`, the number of inputs its arguments named. Every
`Time` is UTC, made by `TimeOf`.
The spike's `schema` `Statx`, `FileOf`, `Stater`, and `Detect`, which
imports `net/http`, are now `cmd/docproc`'s `run`'s, their one caller (`stat_linux.go`, on
`golang.org/x/sys/unix`), so `rundir/schema` is shapes alone and the base module requires
`golang.org/x/text` alone.

## Assumptions

- Assumes docproc's dispatch keeps asking each tool for its Markdown alone as the text it hands to
  inference, as processing's stages do.
- Assumes Athena builds `sparse_rows` from typed cells, not from Markdown (`athena-mapping`).
- Assumes docproc keeps processing's package names `office`, `email`, and `onenote`, not the
  tools' names: each does more than decode its tool (LibreOffice's legacy conversion, threading).
- Assumes `cmd/docinfer`, in docinfer's repository, reads a `cmd/docproc` run directory through
  docproc's public `rundir` and `rundir/schema`.
- Assumes inference needs nothing of docproc beyond `dispatch` (its dispatcher, events, shapes,
  and Meta keys) and, for `cmd/docinfer`, `rundir`, so docproc depends on no model concept and
  imports no docinfer package.
- Assumes every stream input is spooled to scratch before its stages run, since every stage and
  tool reads a path; no stage reads a stream directly.
- Assumes the typed metadata shapes wait for the capstone's `metadata` task; until then,
  `Output.Meta` and the tools' lines carry metadata.
