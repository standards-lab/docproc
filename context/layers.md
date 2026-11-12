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
text layer. Every piece of text the input holds exists once: content in the Markdown text layer,
labels as kept plain-text fields. A structured field is cut only when its text is already carried,
in the Markdown or in a kept field, or it can't be plain text and has no processor. Content a tool
reads that its Markdown lacks is rendered into the Markdown, never kept beside it as a duplicate
field ("Consumers of structured text").

### Common envelope

- **Lines.** stdout holds JSON Lines and nothing else, one compact object per line. Each line's
  `line` names its kind; the first line carries `schema` (`"2"`) and the input's kind. docproc
  decodes line by line, keeping a line, block, or container of an unknown kind as an `Unknown*`
  value, as processing's `office/model.go`, `email/model.go`, and `onenote/model.go` do.
- **Absence.** A field with no value, or an empty list, is left out, never `null`.
- **Text layer.** A line that holds text carries it as `markdown`: docproc-ooxml's `document`,
  docproc-mail's `message`, docproc-onenote's `page`. Literal text is escaped by htmd's rules, which
  mail's and onenote's Markdown share (onenote README, "Markdown").
- **Media.** With `--media <dir>`, each distinct payload (picture, embedded file, OLE payload,
  attachment, ink SVG) is written once into `<dir>`, created if missing, as stored, before the
  line that names it. Its name is the lowercase hex SHA-256 of its bytes plus `.` and the stored
  extension when that is 1 to 8 ASCII letters and digits. A line names it in `file`, and the
  Markdown links it by that name. Without `--media` nothing is written to disk and the names stand.
  A linked or by-reference target is named, never fetched.
- **stderr.** One line each: `<tool>: warning: <where>: <what>`, and for exit codes 1 to 4
  `<tool>: error: <reason>`. `<where>` never holds `": "`, so a warning line splits at its first
  three.
- **Exit codes.** 0 read, skipped items included; 1 stdout or media not written; 2 usage; 3
  refused before any output; 4 a fault of the tool's own. All three spikes use these
  meanings (office, mail, and onenote READMEs, "Usage").
- **Determinism.** The same input gives byte-identical stdout and media, proven by committed
  golden outputs (`.jsonl` and `.stderr` per fixture, as mail's `tests/golden/`); no JSON Schema.
- **Out of the tools.** Warnings as data are docproc's; per-item digests and provenance
  docinfer's; batch IDs Cerebro's; typed metadata docproc's and Cerebro's.

### docproc-ooxml

From spike-ai-pipeline-office 0.1.0 (`src/SpikeAiPipeline.Office/Model/Document.cs`, README "The
JSON model"), which writes either the model (indented JSON) or Markdown, never both. docproc-ooxml
writes a `document` line first, then, per xlsx sheet in workbook order, a `sheet` line and its
`rows` lines, so no line holds a whole sheet's cells:

```text
document  line, schema, format (docx|xlsx|pptx), properties?, markdown?, media?
properties  core?, app?, custom?: maps of the package's core, extended, and custom properties,
            each value typed as stored (string, number, boolean, ISO 8601 date)
media[]   kind (picture|embedded|ole), file?, content_type?, part?, target?, prog_id?,
          section? | sheet? | slide?   in document order
sheet     line, name, hidden?, markdown?   an xlsx sheet's header: its sheet's Markdown
rows      line, rows[]: row, cells[]: ref, value_type?, value?, formula?, shared_from?,
          col_span?, row_span?   the sheet's next rows, in order
```

- `markdown` is office's Markdown rendering (`Rendering/MarkdownRenderer.cs`, the goldens
  `test/SpikeAiPipeline.Office.Tests/Expected/*.md`): headings, lists, GFM tables, charts as
  tables of their cached series, notes and comments as GFM footnotes, slides as `# Slide n`. An
  xlsx's Markdown is split by sheet: the `document` line holds the Markdown of no sheet, and each
  `sheet` line its sheet's. A picture's image text is its `alt`, or its `title` when it has no `alt`
  (`MarkdownRenderer.cs`, `media.Alt ?? media.Title`), so a title-only picture is
  `![title](file)`. A `title` that differs from the image text, which office leaves out, becomes
  its image's Markdown title, `![alt](file "title")`.
- `media` holds every picture office writes (`Media` blocks, `Reading/MediaStore.cs`), plus
  embedded packages (`embedded`, the part as stored) and OLE objects (`ole`, the native payload,
  `prog_id` the object's), which office leaves unextracted (README "What office does not do").
  `part` is the package part, `target` a linked picture's URL; the placement is the docx section,
  xlsx sheet, or pptx slide index.
- A sheet's `rows` lines keep its typed cells, office's `TableCell` less its docx-only fields.
  Each holds the sheet's next rows, in order, as many as keep the line within 256 KiB, and at
  least one, so a line is bounded by the larger of 256 KiB and one row, never by the sheet.
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
          conversation_index?, markdown?, attachments?
address   name?, address?, address_type?, smtp_address?
attachment  file?, filename?, mime_type?, content_id?, inline, method?, size?, message?
```

- `markdown` is schema "1"'s `body.derived.markdown`, moved up; `body` and its `text`, `html`,
  `rtf`, and `derived.html` are cut (measured below). When the Markdown is made from HTML, stored
  or recovered from the RTF, the tool appends the sentences of the message's source text that
  the Markdown lacks, by the content rule below, in order, as a paragraph escaped as a text
  body's Markdown is. The source text is the text body; a message with an RTF body and no text
  body has the RTF's text, read by the RTF specification: the body decompressed, control words
  and ignorable groups (`\*`, the font, color, and style tables) stripped, `\par` and `\line` as
  line breaks, symbol words (`\rquote`, `\emdash`, …) as their characters, and `\'hh` decoded in
  the `\ansicpg` code page. An RTF that encapsulates HTML (`\fromhtml`) has that HTML's text.
- The content rule compares a source text with a Markdown text by standards alone, so no
  fixture tunes it; docproc-onenote's OCR text follows it too. It is written once, in one crate
  under `tools/` that docproc-mail and docproc-onenote both depend on. Its Unicode version, 16.0,
  is part of schema "2": the normalization, word, and sentence boundaries are Unicode 16.0's, and
  another version is a change of meaning.
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
    (Myers 1986, section 4b) aligns the rest, for n source and m Markdown tokens and D edits
    between them. Any longest common subsequence has the same length; where several tie, the
    rule's is the one that algorithm returns, so every implementation appends the same
    sentences.
  - **Units.** The source's sentences by Unicode sentence boundaries (UAX #29), a line break also
    ending one. Each sentence holding a token outside the alignment is appended, in order; the
    rest is already in the Markdown, so overlap is never written twice.
- `smtp_address` is an Exchange (`EX`) address's SMTP address (`PidTagSmtpAddress`), which schema
  "1" leaves out (README "Known limits").
- `--attachments <dir>` becomes `--media <dir>`; `--after <id>` keeps its resume.

### docproc-onenote

From spike-ai-pipeline-onenote 0.1.0 (`src/model.rs`, README "Schema \"1\""). It commits no golden
outputs: its tests check JSON-pointer expectations (`tests/fixtures/{sections,notebooks,content}.json`)
and run-to-run byte identity, so docproc-onenote adds goldens. Lines `notebook` and `skipped` keep
their schema "1" fields, and `section_group` and `section` keep them less `display_name`, which
duplicates their `path`'s last name (16 of 16). A page changes:

```text
page      line, id, section, level, title?, creation_time?, modification_time?, author?,
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
  Markdown leaves out; the words they belong to are the ink image's alt text in the Markdown
  (`![ink: <words>](<file>.svg)`), in the same order.

### Versioning

The contracts move to schema "2". Every spike writes "1", and the additions alone
(`properties`, OLE media, `smtp_address`, `ink_alternatives`, the text the Markdown gains) would
fit "1", but the cuts below remove fields each spike reader of "1" declares, and processing's
readers accept exactly "1" (`decodeDocument` in `office/model.go`, `decodeLine` in
`email/model.go` and `onenote/model.go`), so "2" makes an old reader refuse instead of reading
zeros. From "2" on, each tool's schema grows by addition only (an optional field, a line or
media kind); a removal, rename, or change of meaning moves it to the next version.

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
  it is made from the text body, nothing is. Compared by docproc-mail's content rule instead
  (UAX #29 by uniseg 0.10.1 on Unicode 16.0, CommonMark by markdown-it-py), the sentences it
  appends are 295 B in 7 messages, each a text body that says what its HTML doesn't: cpython's
  `msg_10.eml` (143 B, the encodings its text parts name) and `msg_15.eml` (18 B), the generated
  inline-image message as `.eml`, `.msg`, and in `mailbox.pst` (30 B each, "It is taped into the
  log book.", where the HTML shows the image), POI's `ASCII_UTF-8_CP1252_LCID1031_HTML.msg`
  (16 B), and Tika's `testMSG_chinese.msg` (28 B). Outlook 2003's welcome message, once in
  Tika's `test-outlook2003.msg` and twice in libyal's `outlook.pst`, appends nothing: its stored
  text body holds U+0099 where its HTML holds `™` (the .msg's `PidTagBody` is `PT_UNICODE`, code
  unit 0x0099), a character the sending client mis-decoded, not one docproc-mail decodes, and as
  a control and a symbol both only separate words, so `SharePoint` matches. No decoder
  correction applies: PT_STRING8 is read in its message's code page, and a MIME part's charset
  label as the WHATWG Encoding Standard reads it (mail-parser maps `iso-8859-1` and `us-ascii` to
  windows-1252). The RTF's text, read as "docproc-mail" states, compared the same way, lacks
  nothing where the Markdown is made from it; beside a stored HTML body it lacks 28 B, the line
  `testMSG_chinese.msg`'s text body also holds.
- **onenote.** Alt text 9,684 B (all), run text 7,482 B (6,944), image `ocr_text` 5,572 B (4,926,
  every duplicated byte of it reaching the Markdown inside the same image's alt text), filenames
  3,240 B (2,865), run links 114 B (all, as link targets), tag labels 57 B (27), ink alternatives
  79 B (8). The run text missing, 538 B, is every title's date and time paragraphs; the
  `ocr_text` missing, 646 B, is OCR text no alt text holds, of which the content rule appends
  566 B, the sentences of 6 images, whitespace collapsed; the tag labels missing, 30 B, are
  "To Do", which the spike writes only as a task box. No image carries a `hyperlink`. Media
  written over mail and onenote: 235 MB.

## Consumers of structured text

Every piece of text a file holds exists once: content in the Markdown text layer, labels as kept
plain-text fields. docinfer embeds text and
Markdown outputs only, never records (`spike-ai-pipeline/internal/docinfer/infer/sources.go`,
`textKinds`), and chunks Markdown at its headings (`internal/docinfer/chunk/doc.go`), so the
Markdown is the content's one path to inference. A field's outcome follows from that, the first
that applies:

- **Kept** when a consumer reads it (a processing package, or a capstone task), whether or not
  its text is in the Markdown, or when it is a label: plain text that names or annotates the
  content rather than being it, which the Markdown leaves out and metadata keeps.
- **Cut** when its text is already carried: in the Markdown, or in a kept field, as folder,
  section, and group `display_name` duplicate their `path`'s last name. Either is duplication.
- **Into Markdown** when its tool reads content the Markdown lacks: the tool renders it into the
  Markdown, never keeps it beside it as a duplicate field, and the field is cut, since the
  Markdown carries it. A link target the Markdown lacks is text, rendered as a Markdown link.
- **Cut** when it can't be plain text and has no processor.

| Tool | Field | Consumer | |
| --- | --- | --- | --- |
| ooxml | `markdown` | `dispatch/office.go` (asks office for Markdown alone) | kept |
| ooxml | text block tree: heading, paragraph, list, docx table inlines; sections, notes, comments (author, anchor, replies) | none: `dispatch/office.go` never requests the model; the Markdown holds their text | **cut** |
| ooxml | chart title, series names, categories, values | none; the Markdown's table holds them | **cut** |
| ooxml | picture `alt` | none; it is the Markdown's image text | **cut** |
| ooxml | picture `title` | none; the Markdown lacks it unless it is the image text, and takes it as "docproc-ooxml" states | into Markdown |
| ooxml | sheet `name`, cell `value`, `formula` | `athena-mapping` (Athena's tabular rows into `sparse_rows` need typed cells); names and values are in the Markdown, a formula is its cell's expression, whose value the Markdown shows | kept |
| ooxml | `media` (`file`, `part`, `target`, `prog_id`, placement) | office's stage (embedded and OLE media become child parts); references, not text, and a linked picture's `target` is also its Markdown image's source | added |
| ooxml | `properties` | `metadata` | added |
| mail | `markdown` | `dispatch/email.go` (`own`), `email/model.go` (`Conversation`) | kept |
| mail | `body.html`, `body.derived.html` | none; `Conversation.HTML` copies `html` and nothing reads it; the Markdown is made from them | **cut** |
| mail | `body.text`, `body.rtf` | none; the Markdown holds their text, but 295 B of text bodies beside an HTML body (and 28 B of RTF, the same line), which the tool appends as "docproc-mail" states | into Markdown |
| mail | `subject` | `email/threads.go` (`baseSubject`), `dispatch/email.go` meta | kept |
| mail | `message_id`, `in_reply_to`, `references`, `conversation_index` | `email/threads.go` | kept |
| mail | `conversation_topic` | `email/model.go` (`Conversation.Topic`) | kept |
| mail | `transport_headers` | `metadata`: typed mail metadata is docproc's and Cerebro's, built from the raw headers | kept |
| mail | address `name`, `address`, `smtp_address` | `email/model.go` (participants), `metadata` | kept |
| mail | attachment `filename` | `email/route.go`, `dispatch/email.go` (`Found` name) | kept |
| mail | folder `path` | `dispatch/email.go` meta | kept |
| mail | folder `display_name` | none; it duplicates `path`'s last name, a kept field | **cut** |
| mail | store `display_name` | `metadata`; a .pst's name, a label the Markdown leaves out. No processing package reads it: `email/model.go` decodes it, and `dispatch/email.go` passes the store through | kept, a label |
| onenote | `markdown` | `dispatch/onenote.go` (`page`) | kept |
| onenote | page `title` | `dispatch/onenote.go` meta | kept |
| onenote | runs' `text`, `latex`, `link`; list markers; table cells | none; the Markdown renders them, links as link targets | **cut** |
| onenote | the title's date and time runs (`PageDateTime`) | none; the Markdown lacks them (538 B), and takes them under the title, joined by one space | into Markdown |
| onenote | tag `label` | none; the Markdown renders it as "docproc-onenote" states, since a task box carries the task state, not the label's words | into Markdown |
| onenote | image `alt_text` | none; it is the Markdown's image text | **cut** |
| onenote | image `ocr_text` | none; 4,926 B is within `alt_text`, and the sentences the alt text lacks by the content rule (566 B) join the Markdown's image text, where `text-layer` reads it | into Markdown |
| onenote | image `hyperlink` | none; the Markdown lacks it, and makes the image a link to it | into Markdown |
| onenote | image `printout_page`, ink `strokes` and `bounds` | none; not plain text, and vision reads every image and ink SVG | **cut** |
| onenote | `ink_text` words | none; they are the ink image's alt text in the Markdown (`![ink: <words>](<file>.svg)`) | **cut** |
| onenote | `ink_text` `alternatives` | `metadata`; the recognizer's alternates, a label the Markdown leaves out | kept, a label, as `ink_alternatives` |
| onenote | `ink_text` `language` | none; a Windows LCID number (`u16`, `model.rs`), not text: it can't be plain text and has no processor | **cut** |
| onenote | media `filename` | `dispatch/onenote.go` (`Found` name) | kept |
| onenote | notebook `name`; section and group `path` | `dispatch/onenote.go` meta, `metadata` | kept |
| onenote | page `author`, times | `metadata` | kept |
| onenote | section and group `display_name` | none; it duplicates `path`'s last name, a kept field | **cut** |

`text-layer` reads an image's OCR text from the Markdown's image text, where the tool renders
the OCR text the alt text lacks; no `ocr_text` field returns.

## docproc's API

The library module, `github.com/standards-lab/docproc`, extracts: it inspects and routes each part
of an input, reads it through its stage and the tools, stores each output's content, and records
what it prepared in its index. It requires `golang.org/x/text` only and depends on no model
concept: no model client, no inference, no embedding. Inference is docinfer's ("docinfer's API"),
and the imports run one way: docinfer imports docproc, and no docproc package imports docinfer.
processing imports spike-ai-pipeline-client only from tests and the test helpers
`internal/visiontest` and `internal/audiotest`, which move to docinfer with the tests that call a
model. "Kept" below means processing's signature carries over as it is.

The modules:

```text
github.com/standards-lab/docproc              the library below; requires golang.org/x/text only
github.com/standards-lab/docproc/storage      Content over go-storage's Client; requires the
                                              library and go-storage
github.com/standards-lab/docproc/database     Index over go-database; requires the library and
                                              go-database
github.com/standards-lab/docproc/cmd/docproc  the docproc command; requires the library,
                                              go-cli-sdk, go-core, and golang.org/x/sys
```

Each store is an interface the library declares (`dispatch.Content`, `dispatch.Index`), with a
filesystem and a memory implementation in the base module on the standard library alone. A
provider is a nested sub-module that implements one over a stack library: `storage` the content
store over go-storage's `Client`, and `database` the index over go-database, one row per entry,
its claims, leases, and fencing as conditional updates. Each provider pins only its own level,
above the tier it depends on (`repository-topology.md`, "Adjacent libraries"), and the base
module stays tier-free.

The library's packages:

```text
route, inspect             what a file is, whether it is safe to open, which stage reads it  kept; Flag tagged
tool                       the exec and container runners                                    kept
internal/cli               the tools' shared contract: exit codes, stderr lines, Spool       kept
office, email, onenote     a tool's reader, decoding its schema "2"                         changed
document, picture, audio,  the other formats, on their tools; audio's Seam JSON-tagged       kept
  media, sevenzip
dispatch                   stages, the walk over an input's parts, the content store and     changed
                             the index; the shapes of its events, parts, outputs, and
                             warnings
stages                     the stages wired over the host's tools                            moved from the spike's docproc
rundir, rundir/schema      cmd/docproc's run directory: its shapes, read and written         moved from the spike's docproc
```

### Inspection and routing

Kept (processing `route/route.go`, `route/identify.go`, `inspect/inspect.go`). The dispatcher
runs both on every part before its stage, so a part's event carries its route and flags; a caller
calls them directly only to triage a file before queueing it. `inspect.Flag` gains JSON tags
(`check`, `class`, `evidence`, the spike's docproc `schema.Flag`), since a part's shape carries it.

```go
package route
type Kind string   // the format a signature names; Unknown is ""
type Route string  // document, office, vision, vision-converted, nested, onenote, archive,
                   // audio, video, text; unrouted
func Identify(head []byte) Kind                                // at most HeadLen (4096) bytes
func File(path, name, mimeType string) (Route, string, error) // the route, and why

package inspect
type Verdict string // proceed | quarantine
type Class string   // active | unsafe
type Check string   // signature-mismatch, zip-traversal, zip-bomb, 7z-traversal, …
type Flag struct { Check Check; Class Class; Evidence string } // tagged check, class, evidence
type Report struct { Kind route.Kind; Flags []Flag; Verdict Verdict }
type Options struct { Policy map[Check]Verdict; BombRatio float64; BombSize uint64 }
func Inspect(ctx context.Context, path, name string, opts Options) (Report, error)
func CheckListing(kind route.Kind, size int64, entries []Listed, opts Options) (Report, error)
```

### Running the tools

`tool` is kept (processing `tool/tool.go`, `tool/exec.go`, `tool/container.go`). Cerebro runs
the exec runner inside an image that bundles the tools (`tool-packaging`). The container runner
stays for hosts like `cmd/docproc`'s, since it needs the docker socket (data-intelligence.md,
"Missing layers").

```go
package tool
type Spec struct {
    Program string; VersionArgs []string; VersionPattern *regexp.Regexp
    Minimum string; VersionExit int; KillAfter time.Duration
}
type Invocation struct {
    Args []string; Dir string; Stdin io.Reader; Stdout, Stderr io.Writer; Usage *Usage
}
func NewExec(ctx context.Context, spec Spec) (*Tool, error)
func NewScript(ctx context.Context, spec Spec, script string) (*Tool, error)
func NewContainer(ctx context.Context, docker string, image Image, spec Spec) (*Tool, error)
// Run runs once; ctx's end sends the process group SIGTERM, then SIGKILL after KillAfter.
func (t *Tool) Run(ctx context.Context, inv Invocation) error
```

`office`, `email`, and `onenote` keep their readers over a `*tool.Tool`. Each decodes its tool's
schema "2" line by line (the contracts above), refuses another schema with `ErrSchema`, and keeps
an unknown kind as an `Unknown*` value. `internal/cli` keeps exit codes 1 to 4 and the stderr
parse. The readers' types follow the contracts: `office.Document` becomes the `document` line,
`office.Sheet` the `sheet` line and `office.Rows` a `rows` line, `email.Message` takes `Markdown`
up and `Address.SMTPAddress`, and `onenote.Page` takes `Markdown`, `Media`, and
`InkAlternatives`. Each reader streams its tool's lines as records, office's included, so no
reader holds a whole sheet.

```go
package email // onenote has the same shape
func NewReader(cfg Config) (*Reader, error) // Config{Mail *tool.Tool}
func (r *Reader) Read(ctx context.Context, req Request) (*Stream, error)
type Request struct { Input string; Media string; After uint64 } // Attachments becomes Media (--media)
func (s *Stream) Records() iter.Seq2[Record, error]

package office
func NewConverter(ctx context.Context, cfg Config) (*Converter, error)
// Convert reads the document at req.Path, which the caller owns; only a legacy document or an
// RTF is copied, into the LibreOffice conversion's input.
func (c *Converter) Convert(ctx context.Context, req Request) (*Stream, error)
type Request struct { Path, Dir, Name string; MetafileScale int } // Format goes
func (s *Stream) Records() iter.Seq2[Record, error] // the document, then each sheet and its rows
```

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
    After  uint64    // resume a store after this message, mail's --after
}
```

`Reader` and `SHA256` are new: processing's `Input` is a path alone. The stages read paths, since
mail, onenote, qpdf, and ffprobe take one, and `office.Converter.Convert` takes the part's path in
its `Request`. So one spool at the input serves every stage. The spool's check is the input's one
whole-file check: a mismatch stops the dispatch with an error matching `ErrSHA256`, which no
redelivery changes. Cerebro always passes blob storage's reader of the job's staged input and its
SHA-256 ("Queries and Commands").

An input with `After` set is a resumed store: it is dispatched past that message, and its entry
is never recorded, since it would hold only the messages past `After`.

### Dispatch

Kept (processing `dispatch/dispatch.go`, `dispatch/stage.go`, `dispatch/pool.go`): `Dispatch`
yields each output as its stage makes it, and each part's own event once its outcome is known.

```go
type Stage interface {
    Identity() Identity // name, its tools' version, its configuration's hash
    Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error]
}
type Stages map[route.Route]Stage
type Config struct {
    Stages Stages; Index Index; Content Content; Dir string; Limits Limits
    Inspect inspect.Options
}
func New(cfg Config) (*Dispatcher, error)
func (d *Dispatcher) Dispatch(ctx context.Context, in Input) iter.Seq2[Event, error]
func (d *Dispatcher) Pool(ctx context.Context, inputs iter.Seq[Input], workers int) iter.Seq2[PoolEvent, error]

func (e Event) Keep() // keep the output's file, or the part's, past the loop body
type Outcome string   // prepared, no processor yet, seen, cycle, quarantined, encrypted,
                      // failed, limited
type Item struct {
    Output  *Output
    Part    *Found
    Warning *Warning
    After   uint64 // a store's: set on a message's last output, the message's cursor
}
```

`Item.Warning` and `Event.Warning` are new. processing yields a tool's stderr warnings as a record
output, but the common envelope's stderr line (`<tool>: warning: <where>: <what>`) parses to typed
data, and warnings as data are docproc's. A warning's event carries its part and stage, as an
output's does, and the index records it, so a seen part replays its warnings. `Item.After` and
`Event.After` are new: the mail stage sets the cursor on each message's last output, so a host
checkpoints a store by the outputs it has handled ("One job, and its cancellation").

The dispatcher hashes each output as its stage yields it, filling `Output.SHA256`, then puts its
content into `Content`, and appends the output to the part's entry only after the put, so
everything the index records is stored. A seen part replays its entry's log from the index, in
the order it was prepared ("Index"), each output opened from `Content` into `Dir`, so a replayed
output's event carries its file as a prepared one's does. A replayed output's or warning's event
carries `Outcome` seen and the entry's outcome in `Recorded`, as processing marks a replayed
output; a child's event carries its `Child.Part`, its `Depth` set from the replaying walk, since
the same content can sit at another depth.

A part is seen when its entry's `Stages` each equal the current identity of the stage of that
name, one check per stage the subtree used. A replayed child recorded prepared, encrypted, no
processor yet, or seen is looked up under its route's current stage and replays its own log; a
child of any other outcome replays as recorded. A miss while replaying, an index entry or a
content that has expired, or a stage wired since for a child that had none, drops the replayed
part's entry, so the part is not seen again, and stops the dispatch with an error matching
`ErrStale`: the redelivery prepares the part again, and its children with it.

Each stage names where it found a part (`Found.Where`), stably across runs, as processing's do:
mail's `<message>, attachment n`, a ZIP's `zip entry <name>`. The dispatcher keeps `Where` unique
among a part's siblings: a `Where` a sibling already has gains ` (n)`, its occurrence among them
from 2. A part's `ID` is the chain of `Where` from the input: the input's ID is `/`, and a found
part's is its parent's ID followed by its `Where` as one more path segment, `%` and `/`
percent-encoded, after a `/` unless the parent's ends in one. So the ID names one occurrence of
content in the input, stable across runs, where the content's SHA-256 can't.

A quarantined part's file is kept only when the caller calls `Keep` in its event, as any part's
is; otherwise the dispatcher removes it with the part, its flags in the part's event.

`dispatch` declares the JSON shapes of what it yields beside its other types, on the standard
library alone: an event, its part, its stage's `Identity`, an output, a warning. `Event` is itself
the shape, one for each of its three kinds: a part's own event, an output's, and a warning's,
each setting only its own fields, so no parallel wire type copies it. `Part` takes the shape of
the spike's docproc `schema.Part` (`spike-ai-pipeline/internal/docproc/schema/part.go`), its
local fields left out of the JSON, and gains `ID`, which `Parent` now carries in place of the
parent's SHA-256; processing's `Part.Kind`, `Route`, `Reason`, and `MIME` move into its
`ContentType`. `Identity` is the spike's docproc `schema.Stage` as JSON, so no `Stage` type is
added; `Stage` stays the interface.

```go
package dispatch
const Version = "1" // of the shapes below, grown by addition only
type Event struct {
    Part     Part     `json:"part"`
    Stage    Identity `json:"stage,omitzero"`
    Output   *Output  `json:"output,omitempty"`  // an output's event
    Warning  *Warning `json:"warning,omitempty"` // a warning's event
    After    uint64   `json:"after,omitzero"`    // an output's: the store's cursor, Item.After
    Outcome  Outcome  `json:"outcome,omitzero"`  // a part's own; seen on a replayed output's
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
    Stored      string      `json:"stored,omitzero"`   // the stored MIME type
    Detected    string      `json:"detected,omitzero"` // cmd/docproc's: an input's sniffed type
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
```

A count left out is zero, as the common envelope's absence rule has it. `Err` stays local: a
failed part's `Reason` already holds its text, as processing sets both. `Output.Path` and `Value`
never marshal: an output's content is found by its `SHA256` in `Content`, so the index records an
output without them and nothing clears them.

`stages` moves from `spike-ai-pipeline/internal/docproc/stages`, less its `WriteTable`, so
`cmd/docproc` and Cerebro wire the same stages from the tools a host holds:

```go
package stages
type Lookup func(program string) (string, error)
func Build(ctx context.Context, lookup Lookup, exclude []route.Route) Set
type Set struct { Stages dispatch.Stages; Tools []Tool; Rows []Row } // Row: route, wired|missing|excluded
```

### Content

The content store holds every output's content by its SHA-256, and a host puts its own texts in
it the same way (docinfer's `job`, its inference texts). It is new: processing's caller stores an
output and sets its `Ref`, which goes.

```go
package dispatch
type Content interface {
    // Put stores r's bytes as the content sha256 names. Content already held is kept, its
    // retention renewed.
    Put(ctx context.Context, sha256 string, r io.Reader) error
    // Open opens the content sha256 names; a miss matches fs.ErrNotExist.
    Open(ctx context.Context, sha256 string) (io.ReadCloser, error)
}
func NewFileContent(dir string) (*FileContent, error)
func NewMemoryContent() *MemoryContent
```

`cmd/docproc`'s run directory holds its content in a `FileContent`; Cerebro's is blob storage
through `docproc/storage`.

### Index

The index is kept (processing `dispatch/index.go`, `memory.go`, `file.go`). It is
content-addressed by a part's SHA-256 and its stage's identity. One claimant prepares the
content, and every other claimant waits for its entry, so replicas sharing an index prepare
content once. Cerebro backs it with PostgreSQL through `docproc/database`; `MemoryIndex` and
`FileIndex` stay for tests and `cmd/docproc`.

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
    Drop(ctx context.Context, key Key) error // a recorded entry, so its content is prepared again
    Flags(ctx context.Context, sha256 string) ([]inspect.Flag, bool, error)
    RecordFlags(ctx context.Context, sha256 string, flags []inspect.Flag) error
}
type Entry struct {
    Outcome  Outcome    `json:"outcome"`
    Reason   string     `json:"reason,omitzero"`
    Outputs  int        `json:"outputs,omitzero"`
    Children int        `json:"children,omitzero"`
    Stages   []Identity `json:"stages"` // the distinct stages its subtree ran
}
type Recorded struct { // one item of an entry's log; one field is set
    Output  *Output  `json:"output,omitempty"`
    Warning *Warning `json:"warning,omitempty"`
    Child   *Child   `json:"child,omitempty"`
}
type Child struct { // a found part, as its parent's entry records it
    Part    Part    `json:"part"`
    Outcome Outcome `json:"outcome"`
}
```

An entry's outputs, warnings, and children are one log, appended as the part's stage yields them
and read back in that order, so a replay is the preparation's order. `Items` and `Append` replace
processing's `Outputs`, `Children`, `Append`, and `AppendChild`. `Child` holds the found part's
`Part` whole, in place of processing's `SHA256`, `Name`, `Where`, `Size`, and `Route`, so a
replayed child's part is the part as found. The index records it as its JSON has it: `Path` and
`After`, local, are never recorded.

A claim is a lease, and every implementation honors one contract. `Claim` returns the lease with
the token, since the lease belongs to the claim the index granted, and a provider that sets it
per claim needs no second call. A claim lasts its lease from its last `Claim` or `Renew`; the
dispatcher renews it on a timer, at a third of the lease, for as long as the part's stage runs.
A claim not renewed within its lease may be taken over by another claimant, and from then on the
first token's `Renew`, `Append`, and `Record` fail. Its dispatch then stops the part's stage,
records nothing, and waits for the new claimant's entry, as any claimant that lost the claim
waits. `FileIndex`'s lease is its stale duration.

### Event shapes

The package that produces a change owns its event kinds and their data types: the kinds in its
`messaging.go`, the data types beside its other types, as JSON on the standard library alone.
go-web-service's `domain/organization/messaging.go` and `domain/document/messaging.go` hold only
their `event.Define[T]` calls and a `Recorder` alias, over data types in the package's
`entities.go` (`OrganizationData`, `OrganizationDeletedData`, `LogoUploadedData`,
`LogoDeletedData`; `DirectoryData`, `DirectoryDeletedData`, `FileUploadedData`,
`FileDeletedData`), and `admin/database/messaging.go` defines its events over `string`. No
separate events package exists; go-core's `event` is the CloudEvents envelope alone.

docproc and docinfer are libraries that emit no CloudEvents themselves, so neither declares event
kinds or has a `messaging.go`. Their shapes sit in their producing packages' ordinary files:
docproc's are `dispatch`'s (above), versioned by `dispatch.Version`, and docinfer's are its root
package's and `job`'s ("docinfer's API"), both versioned by `docinfer.Version`. Cerebro's
`messaging.go` declares its `cerebro.job.*` kinds, each over one of these or over its own data
types ("Events"). The spike's docproc `schema` shapes
(`spike-ai-pipeline/internal/docproc/schema/part.go`, `record.go`) move into `dispatch` and
`inspect`, and `rundir/schema` aliases them ("The nested modules"); the spike's docinfer `outdir`
shapes move into docinfer. The shapes grow by addition only, as the tools' contracts do.

## docinfer's API

docinfer (`github.com/standards-lab/docinfer`) infers over what docproc extracts: it describes
images, transcribes audio, chunks and embeds text, extracts by a caller's JSON schema, holds
results in a cache, and runs one job's dispatch and inference together. It is an adjacent
library that depends on docproc and on go-ai, an infrastructure library. Whatever may not depend
on go-ai's tier may not depend on docinfer either (`repository-topology.md`, "Adjacent
libraries"): an application SDK may not reach go-ai through it, and only the layers above go-ai's
tier, such as Cerebro and go-web-service, may use it. It calls go-ai directly, with no adapter
module between them ("docinfer's go-ai use"). It is built from the spike's docinfer, less its
command's run directories, and holds until go-ai's library releases.

The modules:

```text
github.com/standards-lab/docinfer               the library below; requires docproc and go-ai
github.com/standards-lab/docinfer/storage       Cache over go-storage's Client; requires the
                                                library and go-storage
github.com/standards-lab/docinfer/cmd/docinfer  the docinfer command; requires the library,
                                                docproc, go-ai, go-cli-sdk, and go-core
```

The base module declares `Cache` with a filesystem and a memory implementation on the standard
library, and `storage` is its provider, as docproc's are ("docproc's API"). Among the tiers the
base module depends on go-ai alone.

The library's packages:

```text
docinfer               the pipeline over go-ai, the cache, batches as made;       new, from the spike's docinfer
                         the shapes of its results
chunk                  the chunker                                                moved from the spike's docinfer
job                    one job: dispatch, then inference, under its context;      new
                         its updates
internal/endpointtest  a fake OpenAI-compatible endpoint for go-ai's client       from the spike's docinfer apptest
internal/visiontest,   the model-calling test helpers                             moved from processing
  audiotest
```

The inference package is the module's root, `docinfer`, so a caller writes `docinfer.Pipeline`
and `docinfer.Batch`, not `infer.Pipeline` under a module already named for inference. `chunk`
stands alone, and the root imports it. `job` composes docproc's dispatcher with the pipeline,
which `cmd/docinfer` runs without it, so the root never imports `job`. The imports run one way:
the root imports docproc's `dispatch` and `audio` and go-ai's `client` and `protocol`; `job`
imports `dispatch` and the root.

### The cache

The cache generalizes the spike's docinfer held results (`outdir.Dir.Held` and `Hold`, keyed by
`outdir.Key`) to bytes behind an interface, the seam a host backs. Every held value is one
image's, window's, chunk's, or extraction's result, bounded by its token cap, so each is a small
object, and Cerebro keeps them in blob storage through `docinfer/storage`.

```go
package docinfer
type Cache interface {
    // Get returns keys' values in one round trip, values[i] keys[i]'s, nil for a miss; a held
    // value is never empty.
    Get(ctx context.Context, keys ...string) ([][]byte, error)
    Put(ctx context.Context, key string, value []byte) error
}
// Key is the spike's docinfer outdir.Key, prefixed "<capability>/": the SHA-256 of the
// identity's capability, model, and settings, and the content's SHA-256, length-prefixed.
func Key(id Identity, content string) string
func NewFileCache(dir string) (*FileCache, error)
func NewMemoryCache() *MemoryCache

// Held is a held value's JSON, the bytes a Cache holds under a Key, versioned by Version: one
// model result. A refusal is held; a failure never is.
type Held struct {
    Text       string `json:"text,omitzero"`       // vision, transcription, extraction: inline
    Vector     string `json:"vector,omitzero"`     // embeddings: as Chunk.Vector
    Incomplete string `json:"incomplete,omitzero"` // why the model stopped: max_output_tokens
    Refused    string `json:"refused,omitzero"`    // why the endpoint refused the content
}
```

A held text is inline, not a file under the spike's docinfer `texts/`. The spike's held results
are not carried over: its vision and transcription records hold their text as a path into
`texts/`, under keys without the capability prefix, so docinfer's cache starts empty. A miss
costs one model call, so starting empty costs one rerun's calls and no code, where translating
the records is code with a single use.

Content lives apart from the cache, under one key scheme each: docproc's `Content` holds every
output's content, and `job` puts every inference text into it, by SHA-256 ("Content"); the cache
holds results by `Key` alone. A part docproc's index reports seen therefore replays outputs whose
content `Content` still holds, which data-intelligence.md ("Caching") asks for: content seen
before is replayed, not recomputed. `cmd/docinfer`'s `outdir` keeps a `FileCache` under its
`results/`.

### The pipeline

The pipeline holds the batching, the cache, the go-ai calls and the mapping of their failures,
and the retry of a rejected embedding batch one text at a time, which the spike's docinfer
`vision`, `transcribe`, and `embed` packages and `infer`'s `vision.go` and `transcription.go`
hold. It is configured with go-ai's client per capability ("docinfer's go-ai use"). docinfer
declares no `Describer`, `Transcriber`, or `Embedder`: its tests drive go-ai's client against a
fake OpenAI-compatible endpoint, as the spike's docinfer `apptest.Serve` does, which tests the
go-ai mapping too, where a fake behind an interface would skip it.

```go
package docinfer
type Config struct {
    Vision        *Vision           // nil skips images, counted
    Transcription *Transcription    // nil skips windows, counted
    Extraction    *Extraction       // nil refuses a run that asks for extraction
    Embeddings    Embeddings        // required
    Chunker       chunk.Splitter    // required: its Bound
    Dimensions    int               // required: the embedding space's; a vector of another
                                    // length is an error that stops the job
    Cache         Cache             // required
    Bounds        *Bounds           // required: the requests in flight, shared
    Pending       int               // a job's inputs admitted and not yet done
    EmbedBatch    int               // texts per Embed call
    BatchBytes    int               // a result batch's cap ("Batches as made")
    Join          audio.JoinRequest // how a part's window transcripts join
}
// Bounds bounds the requests in flight per capability, across every Pipeline built with it.
type Bounds struct{ … }
func NewBounds(vision, transcription, extraction, embeddings int) *Bounds
func New(cfg Config) (*Pipeline, error)
// Profile is the resolved identity: the models, settings, dimensions, and chunker, known before
// any job runs.
func (p *Pipeline) Profile() Profile
// Run infers over one job's inputs and yields its inference texts, extraction progress,
// batches, and counts as made.
func (p *Pipeline) Run(ctx context.Context, opts Options, inputs iter.Seq2[Input, error]) iter.Seq2[Result, error]

type Options struct { // one run's
    Extract *Extract // a caller's prompt and schema; nil extracts nothing
    Final   bool     // the job's last delivery ("docinfer's go-ai use")
}
type Extract struct {
    Name   string         `json:"name"`   // the schema's, 1 to 64 letters, digits, _ and -
    Prompt string         `json:"prompt"`
    Schema jsontext.Value `json:"schema"` // a JSON Schema object
}
type Input struct {
    // Event is an output's event: text or markdown chunked; image described; audio transcribed,
    // then joined per part. Its Output's Path, or Value, holds the content until Done.
    Event    dispatch.Event
    Metadata string // or the input's metadata text, Event then the depth-0 part's own event
    Done     func() // the pipeline is finished with the content
}
type Result struct { // one field is set
    Inference *Inference
    Progress  *Progress
    Batch     *Batch
    Done      *Counts // last
}
```

A replica holds one `Bounds`, and builds one pipeline per profile over it, so it bounds each
capability as a whole whatever profiles its jobs use ("Distribution": inference fans out inside a
job, bounded per capability). A pipeline is built once and runs many jobs. It joins work in
flight by `Key`: a result one job has asked a model for, another job waits for rather than asks
again. Each go-ai call takes the job's context, and the client is safe for concurrent use.

The inputs are the spike's docinfer sources (`infer/sources.go`): text and Markdown outputs,
never records, plus each image's vision text, each part's joined transcript, and one metadata
text per input. `Input` is the spike's source under another name, since `Source` is a chunk's
origin in docinfer's shapes, and it carries dispatch's event as it is. `job` feeds `Run` from
dispatch's events. `cmd/docinfer` feeds it from a `cmd/docproc` run directory through its `source`
package, which reads it with docproc's `rundir`, its metadata text the spike's `metadataText`.

Extraction runs a caller's prompt and JSON schema, the run's `Extract`, over each source text the
pipeline chunks: each text and Markdown output, and each inference text, once each, whole. It
streams (`GenerateStream`), and the pipeline yields its deltas as `Progress`, coalesced to at
most one a second per source text, each carrying the text since the last. Its result is an
`Inference` of capability extraction, whose text is the JSON the schema describes; nothing
decodes it. Its cache key is `Key(Identity{"extraction", model, settings}, content)`, whose
settings hold the prompt's and the schema's SHA-256, its token bound, and its effort. Athena's
summary chain maps onto it (`athena-mapping`).

`chunk` is the spike's docinfer chunk (`Version`, `Chunk`, and `Split`), plus `Splitter`, new: the
pipeline's chunker, its `Bound` and its `Settings`. Its `Chunk` holds its offsets as a `Span`, the
spike's docinfer `outdir.Span` moved here, so the chunker's result and docinfer's `Chunk`, which
embeds it, share one shape:

```go
package chunk
// Chunk's JSON is docinfer.Chunk's, which embeds it, versioned by docinfer.Version.
type Chunk struct {
    Text string `json:"text"`
    Span Span   `json:"span"`
}
type Span struct { // the text's [Start, End) bytes: the spike's docinfer outdir.Span
    Start int `json:"start"`
    End   int `json:"end"`
}
const Version = "1" // of the splitting, which keys the held embeddings
type Splitter struct { Bound int }
func (s Splitter) Settings() string // Version and Bound, which key the held embeddings
func (s Splitter) Split(text string) []Chunk
func Split(text string, bound int) []Chunk
```

### Batches as made

The spike's docinfer runs each capability over every input in turn and writes `chunks.jsonl`
whole at the end (`infer/execute.go`, `outdir.Dir.WriteChunks`). `Pipeline.Run` streams per job
instead, in the order things are made:

- an `Inference` as each image's text, each part's joined transcript, or each extraction is
  made, or read from the cache, and an extraction's `Progress` as it streams;
- a `Batch` once its chunks' embeddings complete. A batch is yielded when the next chunk would
  pass `BatchBytes`, 512 KiB by default, or when no embedded chunk is waiting, so it never waits
  to fill;
- the job's `Counts` last.

`Run` pulls its next input only while fewer than `Pending` of the job's inputs are admitted and
not done, so a slow model holds the job's dispatch back rather than filling scratch with files
the pipeline has yet to read.

The cap keeps a batch under NATS's default 1 MB `max_payload`: a 768-dimension vector is 4,096
B in base64 (data-intelligence.md, "Results").

A batch carries no index: Cerebro identifies it ("Results and the manifest"), and
`cmd/docinfer` writes chunks, not batches.

### docinfer's shapes

```go
package docinfer
const Version = "1" // of the shapes below and job's, grown by addition only
// A chunk's span is chunk.Span; a transcript's seams are docproc's audio.Seam, whose JSON names
// are outdir.Seam's; Counts is under "docinfer's packages".
type Capability string // vision, transcription, extraction, embeddings
type Identity struct {
    Capability Capability `json:"capability"`
    Model      string     `json:"model"`
    Settings   string     `json:"settings"` // vision: prompt, max tokens, effort; transcription:
                                            // prompt, language; extraction: the SHA-256 of the
                                            // prompt and of the schema, max tokens, effort;
                                            // embeddings: none
}
type Profile struct {
    Vision        *Identity `json:"vision,omitempty"`
    Transcription *Identity `json:"transcription,omitempty"`
    Extraction    *Identity `json:"extraction,omitempty"` // its settings max tokens and effort
    Embeddings    Identity  `json:"embeddings"`
    Dimensions    int       `json:"dimensions"` // declared: Config.Dimensions, checked per vector
    Chunker       string    `json:"chunker"`    // the Splitter's Settings
}
type Usage struct {
    Input  int `json:"input"`
    Output int `json:"output"`
}
type Outcome string // described, transcribed, extracted, incomplete, refused, failed
type Inference struct {
    Capability Capability   `json:"capability"`           // vision, transcription, extraction
    Part       string       `json:"part"`                 // its part's ID
    Content    []string     `json:"content"`              // the SHA-256 of the image, the windows
                                                           // in order, or the source text
    Outcome    Outcome      `json:"outcome"`
    Failed     []int        `json:"failed,omitempty"`     // a transcript's windows refused or
                                                           // failed, by index into Content
    Held       bool         `json:"held,omitzero"`        // read from the cache, not asked of a model
    TextSHA256 string       `json:"text_sha256,omitzero"` // the text, in docproc's Content
    Text       string       `json:"-"`                    // local: job puts it in Content
    Seams      []audio.Seam `json:"seams,omitempty"`      // a transcript's, as audio.Join made them
    Usage      Usage        `json:"usage,omitzero"`
    TookMS     int64        `json:"took_ms"`
}
type Progress struct { // an extraction's text since its last progress
    Content string `json:"content"` // the source text's SHA-256
    Delta   string `json:"delta"`
}
// Batch holds its chunks, and each part and source text they come from once, by index.
type Batch struct {
    Parts   []dispatch.Part `json:"parts"`   // provenance: each, its parent and depth, where found
    Sources []Source        `json:"sources"`
    Chunks  []Chunk         `json:"chunks"`
    Digest  string          `json:"digest"`  // SHA-256 over its chunks' digests, in order
}
// Source is the spike's docinfer outdir.Source, the text a chunk is cut from, less Ref and File,
// its directories' paths, plus where the text sits.
type Source struct {
    Kind       string             `json:"kind"`            // output, metadata, vision, transcript
    SHA256     string             `json:"sha256,omitzero"` // the text's; an output's is its
                                                            // Output.SHA256, held there alone
    Incomplete string             `json:"incomplete,omitzero"`
    Part       int                `json:"part"`            // an index into the batch's Parts
    Stage      *dispatch.Identity `json:"stage,omitempty"`
    Output     *dispatch.Output   `json:"output,omitempty"` // the output it is, or the image or
                                                             // first window its inference read
}
// Chunk is the spike's docinfer outdir.Line less the input's file fields (Input, File,
// ContentType) and its Part and Output, which its Source holds, plus Digest. outdir.Part's
// outcome leaves in the part's own event, not on each chunk. It embeds chunk.Chunk, whose fields
// encode inline, so its JSON is {digest, text, span, index, source, vector, error}.
type Chunk struct {
    Digest      string `json:"digest"` // SHA-256 of Text: the per-item digest
    chunk.Chunk        // inline: text, span
    Index       int    `json:"index"`           // within its source text
    Source      int    `json:"source"`          // an index into the batch's Sources
    Vector      string `json:"vector,omitzero"` // go-ai's []float32 as standard base64 of
                                                // little-endian float32s, the spike's EncodeVector
    Error       string `json:"error,omitzero"`  // why the endpoint refused it, or it failed
}
```

A transcript's outcome is transcribed when every window was; otherwise it is failed when a window
failed, and refused when none failed, and `Failed` lists those windows. A chunk is identified by its
job, its part's `ID`, its source's kind and text SHA-256, and its index: the same content in two
places is two chunks, and a redelivery's repeat is the same one.

Per-item digests, provenance, and the profile are docinfer's, and warnings docproc's, as the
common envelope keeps them out of the tools. A batch's identity is Cerebro's ("Results and the
manifest").

### One job, and its cancellation

```go
package job
type Config struct {
    Dispatcher      *dispatch.Dispatcher
    Pipeline        *docinfer.Pipeline
    Content         dispatch.Content // where inference texts go: the dispatcher's own
    CheckpointEvery int              // messages between a store's checkpoints
}
func New(cfg Config) (*Runner, error)
// Run dispatches one job's input and infers over it, yielding its updates as made, in the order
// below, and done last.
func (r *Runner) Run(ctx context.Context, in dispatch.Input, opts docinfer.Options) iter.Seq2[Update, error]

// Update is one thing Run yields; one field is set. It is the Go side's one-of alone: each
// field's type is its own JSON shape, so Update has none.
type Update struct {
    Event      *dispatch.Event     // a part's own, an output's, or a warning's
    Inference  *docinfer.Inference
    Progress   *docinfer.Progress
    Batch      *docinfer.Batch
    Checkpoint *Checkpoint
    Done       *Done
}
type Checkpoint struct { // a store's cursor, a low watermark
    After uint64 `json:"after"` // the last message, in read order, whose updates, and every
                                // earlier message's, are out, their batches included
}
type Done struct { // one attempt's
    Parts   map[dispatch.Outcome]int `json:"parts"`   // by outcome, every depth
    Outputs int                      `json:"outputs"` // output events
    Counts  docinfer.Counts          `json:"counts"`
}
```

A job's ID is its host's: `Run` takes the input alone, and Cerebro names the job in each event's
subject. `Update` carries no kind, since the field set names it, and no job.

The dispatcher stores each output's content before its event ("Dispatch"), and `Run` puts each
inference text into `Content` before its inference's update. The files `job` keeps for the
pipeline, from an output's event on, it removes when the pipeline is done with them. `job` keeps
no quarantined part's file: its bytes stay within the job's input, which the caller holds. A
process that dies leaves its files in the dispatcher's `Dir`, so a host empties its `Dir` when it
starts: no job outlives its process, and Cerebro's work queue redelivers the job.

Updates keep one order: an output's update precedes anything derived from it (its inference, the
batches holding its chunks), and a part's update follows its outputs', warnings', and children's.
Nothing else is promised: inference and batches interleave with dispatch's updates as they are
made.

`job` tracks a store's progress by its low watermark alone. An output event's `After` marks its
message's last output, and a checkpoint carries the highest message whose updates, and every
earlier message's, are out, at most every `CheckpointEvery` messages. A store resumed after it
repeats no message whose batches are out and skips none whose batches are not. A host resumes
from the last checkpoint: Cerebro reads it from the job's subject and sets `Input.After` on the
redelivery, and docproc records no entry for the resumed input ("Input from a stream, or a
path").

The metadata text of a job's input is the spike's docinfer `metadataText` over what a stream has:
its lines `file name`, `size`, `kind`, `route`, `route reason`, `stored MIME type`, and `detected
MIME type`, from the depth-0 part, in the spike's order and form. `job` feeds it to the pipeline
as the `Metadata` of the depth-0 part's own event.

`job` threads no mail. A store's message and conversation records are record outputs, held in
`Content` like any other, and carry the threading keys; a consumer threads them with docproc's
`email.Threader`, so no Threader state joins the checkpoint. A message's threading keys ride on
its output event once the `metadata` task types them.

A job is cancelled through `Run`'s context, which is the job's own. Its end stops the job's tool
runs (`tool.Run`'s SIGTERM, then SIGKILL), its index waits and claims, and its go-ai calls, since
each takes the context. It removes the job's files and yields `ctx.Err()` alone, last. Other jobs
on the same `Runner` go on. Stopping the loop early does the same. processing's `Dispatch`
behaves this way per call. The spike's docinfer single run-wide context becomes one per job;
docproc's `Pool` keeps one context for all its inputs, since only `cmd/docproc` uses it.

## go-ai's surface

go-ai is built from spike-ai-pipeline-client v0.4.0, which the spike's docinfer builds on. The
names below are the spike's (the module root is package `client`); go-ai's own names are
v1.ai.client's. docinfer (its pipeline and `cmd/docinfer`) calls only what follows, over clients
Cerebro builds. The model gateway stays transparent to go-ai: go-ai sees an OpenAI-compatible base
URL, and Cerebro speaks Chat Completions to it.

### The client

From `client.go`, `retry.go`, and `error.go`. The spike's docinfer builds one client per
endpoint (`internal/docinfer/config/config.go`, `Endpoint.Client`) over one `http.Client` with a
cloned default transport and no timeout (`internal/docinfer/app/infrastructure.go`).

```go
package client
type TokenSource func(ctx context.Context) (string, error) // called once per attempt
type Config struct {
    BaseURL string        // the gateway's API root, such as "http://gateway:4000/v1"
    Query   url.Values    // cmd/docinfer only: the router's {"autoload": "false"}
    Token   TokenSource   // Cerebro's consumer key, as the bearer token
    HTTP    *http.Client  // its transport's ResponseHeaderTimeout bounds the wait for a reply
    Format  format.Format // Cerebro: completions.New(); cmd/docinfer: its endpoint's format
    Retry   *RetryPolicy  // nil retries nothing
}
type RetryPolicy struct { MaxAttempts int; BaseDelay, MaxDelay time.Duration }
func New(cfg Config) (*Client, error) // makes no request; safe for concurrent use

// APIError is a non-2xx reply, returned wrapped, reached with errors.As.
type APIError struct { Status int; Message, Type, Code string; Body []byte }
```

- **Retry.** A policy resends a request on a 408, 429, or 503 reply, or a connection failure
  before any reply, honoring Retry-After, with full-jitter exponential backoff; never another
  status, a started stream, or `Transcribe`. Cerebro sets one, since the gateway answers 429 and
  503 at capacity (data-intelligence.md, "What it asks of the goals beneath it"). The spike
  proves it by unit tests alone; proving it against the live gateway is asked of v1.ai.client.
- **Timeouts and cancellation** are the context's: each call takes the job's context, so
  cancelling a job ends its requests (`stream.go` closes a stream's connection). go-ai sets no
  limit of its own, and concurrency bounds stay the caller's, `docinfer.Pipeline`'s.
- **The consumer key** is the bearer token `Token` returns. The gateway's keys carry the
  priority between bulk work and chat ("Cerebro and Athena").

### Generation: vision and extraction

From `client.go`, `stream.go`, and `protocol/`. The spike's docinfer vision sends one user
message of the image, as a data URL, and the prompt, its reply capped and its reasoning effort
none by default (`internal/docinfer/vision/vision.go`, `describe`).

```go
func (c *Client) Generate(ctx context.Context, req *protocol.Request) (*protocol.Response, error)
// GenerateStream is lazy: each range sends the request. Each pair is an event or an error; the
// events run from ResponseCreated to one terminal event (completed, incomplete, failed).
func (c *Client) GenerateStream(ctx context.Context, req *protocol.Request) iter.Seq2[protocol.Event, error]

package protocol
type Request struct {
    Model           string
    Instructions    string         // extraction: the caller's prompt
    Input           []Item         // one Message{Role: RoleUser, Content: [InputImage?, InputText]}
    Reasoning       ReasoningParam // {Effort}: vision's none
    Text            TextParam      // {Format: JSONSchemaFormat}: extraction's schema
    MaxOutputTokens int            // 0, or at least MinMaxOutputTokens (16)
}
type InputImage struct { ImageURL string } // DataURL(mediaType, data)
type JSONSchemaFormat struct { Name string; Schema jsontext.Value; Strict bool }
type Response struct {
    Output []Item; Status Status; IncompleteDetails *IncompleteDetails
    Error *ResponseError; Usage Usage // Usage{InputTokens, OutputTokens, …}
}
type Accumulator struct{ … } // Add(Event) error; Response() *Response, partial after a break
// the events read: OutputTextDelta{Delta}, and the terminal ResponseCompleted,
// ResponseIncomplete, ResponseFailed
```

A reply's text is the `OutputText` of its assistant messages, joined by a blank line, never its
reasoning (the spike's docinfer `text`). `StatusIncomplete` gives the reason a source's
`Incomplete` records, such as `max_output_tokens`; any status but completed and incomplete is an
error.

Extraction streams: `GenerateStream`, folded by an `Accumulator`, whose `OutputTextDelta`s
docinfer yields as progress ("The pipeline"). Its output text is the JSON the schema describes,
and the client adds no decoding (`protocol/text.go`).

### Embeddings and transcription

From `embed.go` and `transcribe.go`; both go to OpenAI-compatible routes whatever the Format.

```go
type EmbeddingRequest struct { Model string; Input []string }
type Embeddings struct { Vectors [][]float32; Usage EmbeddingUsage } // Vectors[i] is Input[i]'s
type EmbeddingUsage struct { PromptTokens, TotalTokens int }
func (c *Client) Embed(ctx context.Context, req *EmbeddingRequest) (*Embeddings, error)

type TranscriptionRequest struct {
    Model    string
    File     io.Reader // the window, uploaded as it is read
    FileName string    // the window's base name
    Language, Prompt string
}
type Transcription struct { Text string; Usage TranscriptionUsage }
type TranscriptionUsage struct { InputTokens, OutputTokens, TotalTokens int }
func (c *Client) Transcribe(ctx context.Context, req *TranscriptionRequest) (*Transcription, error)
```

`Transcribe` is never retried, since its file is a one-shot reader. Cerebro needs it retried
against the gateway's 429 and 503, and docinfer can reopen the window's path but can't read the
reply's Retry-After, which `APIError` doesn't carry. The fix is asked of v1.ai.client ("Requests
to go-ai").

### docinfer's go-ai use

docinfer's pipeline calls go-ai's client itself, one configuration per capability. It is the
spike's docinfer `vision`, `transcribe`, and `embed` packages, their concurrency moved into
`Bounds`. The go-ai mapping lives here, beside the calls it maps, and is stated here alone.

```go
package docinfer
type Vision struct {
    Client *client.Client; Model, Prompt string; MaxTokens int; Reasoning protocol.ReasoningEffort
}
func (v Vision) Identity() Identity // {vision, Model, the spike's visionSettings}
type Transcription struct { Client *client.Client; Model, Prompt, Language string }
func (t Transcription) Identity() Identity // {transcription, Model, transcriptionSettings}
type Extraction struct {
    Client *client.Client; Model string; MaxTokens int; Reasoning protocol.ReasoningEffort
}
func (x Extraction) Identity(e Extract) Identity // {extraction, Model, e hashed, MaxTokens, …}
type Embeddings struct { Client *client.Client; Model string }
func (e Embeddings) Identity() Identity // {embeddings, Model, ""}
// Permanent reports whether an error Run yields is one no redelivery changes: a status the
// table below marks permanent, or a mismatched input (dispatch.ErrSHA256).
func Permanent(err error) bool
```

| go-ai | docinfer |
| --- | --- |
| `protocol.Usage{InputTokens, OutputTokens}`, `TranscriptionUsage` | `Usage{Input, Output}` |
| `EmbeddingUsage{PromptTokens}` | `Usage{Input}` |
| `Response.IncompleteDetails.Reason` | `Source.Incomplete`; the inference's outcome `incomplete` |
| `*APIError` 400, 413, 415, or 422 | refused: the inference's outcome `refused`, or the chunk's `Error`; recorded and held |
| `*APIError` 401, 403, 404, or any other 4xx but 408 and 429 | permanent: the error, wrapped, stops the job, and `Permanent` reports it |
| `*APIError` 408 or 429, retries spent | the error, wrapped: it stops the job, and a redelivery retries it |
| `*APIError` 5xx, retries spent | the error, wrapped: it stops the job; on the last delivery (`Options.Final`), the item's outcome `failed`, or the chunk's `Error`, recorded, never held, and the job goes on |
| a transport failure, a cancelled context | the error, wrapped: it stops the job |

The spike's docinfer counts every `*APIError` as a rejection and holds a 4xx
(`infer/execute.go`, `verdict`), which would hold a gateway's 429 as a verdict on the image, and
records a 5xx as a hole in the run. docinfer refuses only the statuses that judge the content:
a malformed or unprocessable request (400, 422), content too large (413), or a media type the
model won't take (415). A status that judges the caller or the route (401, 403, 404) fails the
same way on every delivery, so it ends the job at once. A job the gateway turned away or failed
transiently stops, the work queue redelivers it, and the cache replays what it finished; only
the last delivery records a 5xx, so one item a model can't answer never holds the rest back. The
pipeline keys held embeddings by the embedder's identity with the chunker's `Settings` as its
settings, the settings strings the spike's.

Cerebro, per profile, builds a go-ai client over the gateway and sets `docinfer.Vision`,
`Transcription`, `Extraction`, and `Embeddings` into a `docinfer.Config`, over its replica's one
`Bounds`; `cmd/docinfer` does the same per endpoint of its configuration.

### What neither caller uses

docinfer and Cerebro call none of the following, so none is part of this surface: function
tools, tool choice, `FunctionCall` and `FunctionCallOutput` items, the reasoning round trip
(`Reasoning` items, `encrypted_content` envelopes), `ReasoningParam.Summary`, verbosity, image
detail, `InputAudio` (docinfer transcribes through `Transcribe`), the `Native` tiers, and the
refusal events. `format/messages` and `format/responses` serve `cmd/docinfer`'s endpoints that
name them; Cerebro uses `format/completions` alone. `Config.Query` is `cmd/docinfer`'s. go-ai's
`/models` listing, its `tools/infer` CLI, and its harness-session surface are outside both.

## Cerebro's API and events

Cerebro's shapes below are what the capstone's design tasks start from: `protocol` settles the
Queries and Commands and the upload protocol, `state` the stores, streams, and the fold, and
`events` the event protocol (data-intelligence.md, "Design"). Its nested `client` module, tagged
`client/vX`, holds these types and a Go client; Cerebro imports none of its consumers ("Cerebro
and Athena", "API").

### Queries and Commands

Go Elemental Queries and Commands. Every Command that changes a job appends one event to the
job's subject under an expected-last-sequence check, which is its transaction ("State").

```go
package client // capstone-cerebro/client

// Commands
type Submit struct { // a small document, whole: protocol
    ID         string // the caller's idempotency ID
    Profile    string // a versioned processing profile in Cerebro's configuration, never a model
    Name, MIME string
    Content    []byte // at most Cerebro's configured small-document size
    Extraction *docinfer.Extract
}
type OpenUpload struct { Name, MIME string; Size int64; SHA256 string }
type AppendChunk struct { Upload string; Offset int64; SHA256 string; Bytes []byte }
type CompleteUpload struct { Upload, Profile string; Extraction *docinfer.Extract }
type AbortUpload struct { Upload string }
type CancelJob struct { Job string }

// Queries
type GetJob struct { Job string }       // -> Job
type GetUpload struct { Upload string } // -> Upload: the offset to resume from
type GetOutput struct { SHA256 string } // -> the content a job's update names, streamed
type ListProfiles struct{}              // -> []Profile

type Upload struct { ID string; Size, Received int64; ChunkSize int; Expires time.Time } // Expires in UTC
type Profile struct { Name string; Resolved docinfer.Profile } // docinfer's resolved identity
type Job struct {
    ID, Profile, Name string; Size int64; SHA256 string
    State    State  // queued, running, completed, failed, cancelled
    Attempt  int    // the work queue's delivery, from 1
    Sequence uint64 // the job's last event
    Manifest *Manifest
    Error    string
}

func (c *Client) Submit(ctx context.Context, cmd Submit) (Job, error)
func (c *Client) OpenUpload(ctx context.Context, cmd OpenUpload) (Upload, error)
func (c *Client) AppendChunk(ctx context.Context, cmd AppendChunk) (Upload, error)
func (c *Client) CompleteUpload(ctx context.Context, cmd CompleteUpload) (Job, error)
func (c *Client) AbortUpload(ctx context.Context, cmd AbortUpload) error
func (c *Client) CancelJob(ctx context.Context, cmd CancelJob) (Job, error)
func (c *Client) GetJob(ctx context.Context, q GetJob) (Job, error)
func (c *Client) GetUpload(ctx context.Context, q GetUpload) (Upload, error)
func (c *Client) GetOutput(ctx context.Context, q GetOutput) (io.ReadCloser, error)
func (c *Client) ListProfiles(ctx context.Context, q ListProfiles) ([]Profile, error)
```

- **Small documents** arrive whole in `Submit`, a one-call upload: Cerebro stages the bytes in
  blob storage at `input/<sha256>`, hashing them as it writes, before it returns the job queued.
  A larger one goes through the upload: chunks wait in blob storage, any replica takes any chunk,
  a chunk whose digest doesn't match is refused, and `GetUpload` gives the offset to resume from
  after a drop. `CompleteUpload` checks that the chunks cover the file, every byte from 0 to
  `Size` received, and queues the job. Either way the job's input is durable, so any replica, on
  any delivery, reads it: `dispatch.Input`'s `Reader` is always blob storage's reader, and its
  `SHA256` the one `Submit` took or `OpenUpload` declared. The whole file is checked once, by the
  dispatcher's spool ("Input from a stream, or a path"); a mismatch is permanent, so the job
  fails at once. A staged input outlives the work queue's `MaxDeliver` deliveries of its job.
  `Submit`'s `ID` makes a retried Submit return the first one's job; its key's exact form is the
  `protocol` task's. The upload's shape graduates to go-web-sdk (`chunked-upload-graduation`).
- **A job** is one input, the unit of retry ("Distribution"). Its ID is a UUIDv7 Cerebro
  assigns; `Submit` and `CompleteUpload` return it queued. The replica running the job extends
  its work-queue delivery while the job runs (an in-progress acknowledgement), so a long job
  isn't redelivered under it. It runs `job.Runner.Run` with `docinfer.Options.Final` set on the
  work queue's last delivery, and on a redelivery resumes a store from its last checkpoint
  (docinfer's "One job, and its cancellation").
- **Fencing.** `CancelJob` appends `cancel_requested`. The replica running the job watches its
  subject through a headers-only consumer, so it reads each event's `type` and never its own
  batches' data back; it folds the cancel and ends the job's context. An append that fails its
  expected-sequence check reads the subject's tail: a `cancel_requested` there ends the job's
  context, and only `cancelled` follows; a `started` newer than its own means a redelivery holds
  the job, and the replica ends the job's context and appends nothing more.
- **Outputs** are read by content: `GetOutput` streams, from docproc's `Content`, the content a
  SHA-256 names, an output's or an inference text's, records included.
- **The profile** resolves to a `docinfer.Pipeline`'s models, settings, dimensions, and chunker,
  the extraction model among them. Each attempt's `started` carries the resolved
  `docinfer.Profile`, before any batch, so the consumer registers the matching embedding space
  ("Models and stages"; the `chunks` task). The profile declares its embedding dimensions, and
  the pipeline checks every vector against them. The `vision` task's prompt and sampling live in
  a profile.
- **Extraction** is optional: a caller's `docinfer.Extract`, which docinfer runs (docinfer's
  "The pipeline").

### State and subjects

Cerebro holds only its jobs, its uploads, and each job's event stream. docproc's index and
content store and docinfer's cache are facets of its internal state, behind the libraries'
interfaces and wired to their providers at its composition root; it holds no state for an
external service. It runs three stores ("State"):

```text
blob storage     input/<sha256>              a job's staged input
                 upload/<upload>/<offset>    an upload's staged chunks
                 content/<sha256>            docproc's Content, through docproc/storage
                 cache/<docinfer.Key>        docinfer's Cache, through docinfer/storage
PostgreSQL       docproc's index             one row per entry, through docproc/database
                 go-messaging's outbox and inbox, through go-messaging/postgres, where used
JetStream        CEREBRO_WORK                cerebro.work: work-queue retention, one message
(file storage)                               per job, {"job"}
                 CEREBRO_JOBS                cerebro.job.<job>: every event of one job, on its
                                             own subject; limits retention
                 CEREBRO_UPLOADS             cerebro.upload.<upload>: an upload's opened, chunk,
                                             completed, and aborted, if the upload keeps a stream
```

JetStream persists to its own file storage, never to PostgreSQL. The names and retentions are
the `state` task's to fix. Whether a job's events go through go-messaging's outbox or append to
the stream directly is the `events` task's.

Each job's events go on one subject of its own, `cerebro.job.<job>`, so the job has a gap-free
sequence of its own: the expected-last-subject-sequence check guards one subject, and the
manifest's range is that subject's ("Results and the manifest"). The subjects share one stream
rather than a stream per job: a consumer filters `cerebro.job.>` or one job's subject, and
~/docs' 592 files, data-intelligence.md's 592 jobs, make no streams to reap.
`standards-lab/context/messaging.md`'s subject and stream conventions (an event's subject is its
type; one owner's stream captures `<prefix>.>`) govern a web service's events for committed
mutations of executed commands. A job's progress and result stream is a different use case,
which those conventions don't govern. The browser never subscribes: Cerebro's events are
service-to-service, and Athena relays its own stream ("Browser").

go-messaging doesn't carry this yet: its NATS provider publishes each event on `Subject: e.Type`
(`go-messaging/nats/broker.go`, `Publish`) and filters a durable consumer by types
(`nats/source.go`, `FilterSubjects: sub.Types`), and a delivery lasts its `AckWait`. What Cerebro
asks of it is under "Requests to go-messaging".

### Events

Each event is a CloudEvents 1.0 event in binary mode, its attributes NATS headers
(`standards-lab/context/messaging.md`, "Decisions"): `type` names it, `source` is `cerebro`, `id`
is a UUIDv7, `subject` is the job's ID, and `Nats-Msg-Id`, the length-prefixed `source` followed
by the `id` as go-messaging keys it, deduplicates a retried append. Consumers de-duplicate
through go-messaging's inbox on `source` and `id`. Its NATS subject is its job's,
`cerebro.job.<job>`, not its type ("State and subjects"). Neither docproc nor docinfer declares
event types: Cerebro's own `messaging.go` defines each type below with go-core's
`event.Define[T]` over its own payload, the producer's shape for what docproc and docinfer yield
and Cerebro's own data types, beside its other types, for the rest. The type and the subject carry
the kind and the job, so no payload repeats them, and `dataschema` names the payload's shape and
its producing package's `Version` (`dispatch.Version`, `docinfer.Version`, or Cerebro's own), its
exact form the `events` task's.

```text
type                                 data                                        whose
cerebro.job.queued                   {profile, name, mime, size, sha256, extraction}  Cerebro's
cerebro.job.started                  {attempt, replica, profile}                 Cerebro's
cerebro.job.part                     dispatch.Event, a part's own                 docproc's
cerebro.job.output                   dispatch.Event, an output's (SHA256, no path) docproc's
cerebro.job.warning                  dispatch.Event, a warning's                  docproc's
cerebro.job.inference                docinfer.Inference                           docinfer's
cerebro.job.progress                 docinfer.Progress                            docinfer's
cerebro.job.batch                    docinfer.Batch; batchdigest, batchchunks     docinfer's
cerebro.job.checkpoint               job.Checkpoint                               docinfer's
cerebro.job.cancel_requested         {}                                           Cerebro's
cerebro.job.completed                Manifest                                     Cerebro's
cerebro.job.attempt.failed           {attempt, error}                             Cerebro's
cerebro.job.failed                   {attempt, error}                             Cerebro's
cerebro.job.cancelled                {}                                           Cerebro's
```

- **docinfer's updates pass through.** `job.Runner.Run`'s updates go out as they are made, each
  as the type its set field names, its data that field's JSON unchanged: a dispatch event is a
  part's own, an output's, or a warning's by which of its fields is set. A warning is defined
  over `dispatch.Event`, not `dispatch.Warning`, since a warning alone names no part. The one
  exception is `done`, which becomes no event of its own: its `job.Done` goes into the manifest's
  entry for its attempt.
- **Cerebro's own** are the job's lifecycle and the manifest. The job's state is the fold of its
  subject: queued, then running from `started`, then one of completed, failed, or cancelled. An
  attempt that fails while the work queue has deliveries left appends `attempt.failed`, and the
  redelivery's `started` follows. `failed` is terminal: appended once the deliveries are spent,
  or at once on an error `docinfer.Permanent` reports.
- **Size.** Every event fits NATS's 1 MB `max_payload`: a batch closes at its cap ("Batches as
  made"), and an output and an inference, an extraction's included, carry their content's
  SHA-256, never the content.

### Results and the manifest

A result batch reaches the consumer as a `cerebro.job.batch` event: docinfer's `Batch`, its
chunks' text, provenance, and base64 vectors. The event also carries its batch's digest and chunk
count as the CloudEvents extension attributes `batchdigest` and `batchchunks`, so the
headers-only fold yields the manifest without reading a batch back. The consumer commits each
batch in one transaction, de-duplicated through its inbox (data-intelligence.md's evidence 7).
The job ends with `completed`, whose data is the manifest:

```go
type Manifest struct { // its range: every sequence from 1 through the completed event's own
    Batches  int       `json:"batches"`  // the batch events in the range, every attempt's
    Chunks   int       `json:"chunks"`   // the chunks those batches hold
    Digest   string    `json:"digest"`   // SHA-256 over those batches' digests, in sequence order
    Attempts []Attempt `json:"attempts"` // one per started, in order
}
type Attempt struct {
    Done *job.Done `json:"done,omitempty"` // docinfer's; nil for an attempt that ended without one
}
```

Each event carries the job's sequence, from 1 with no gap, in the CloudEvents `sequence`
extension as a decimal string. The expected-last-subject-sequence check lets one appender write
the subject at a time, so the job's sequence is contiguous whatever the stream's global sequence
interleaves. A batch is identified by its job and its event's sequence, which no attempt reuses,
so docinfer numbers no batches and nothing renumbers them. The consumer checks completeness by
holding every sequence from 1 through the `completed` event's own and by recomputing `Batches`,
`Chunks`, and `Digest` over the batches it committed. Since a redelivered job's attempt replays
content seen before, and a store resumes from its last checkpoint, the range and digest cover
every attempt's batches. Counts are reported per attempt, never summed: an attempt that failed
reports none, so a sum would count some attempts and not others. The digest covers the chunks'
digests, which cover their text, and not their vectors: it proves which chunks arrived, and a
vector is checked by its length against the profile's dimensions.

### Open questions

- **Live sources.** docproc's `media.Streamer` reads a URL source (srt, rtmp, http, udp) as it
  arrives, but docinfer's `job.Runner.Run` takes a `dispatch.Input`, a path or a spooled reader.
  Whether a live source is a job, and through which Command, is the `protocol` task's.
- **The small-document size**, `Submit`'s limit and the upload's threshold, is a setting whose
  default the `protocol` task picks.
- **Transcription retry** needs go-ai's change ("Requests to go-ai").

## Changes per tool and per package

What each promotion changes from its spike, derived from the sections above; the task that
builds it is in brackets, or the docinfer goal for docinfer's. Every tool moves to schema "2" and
the common envelope, and each re-records its goldens under "2" as one `.jsonl` and one `.stderr`
per fixture, compared byte for byte, which tests its determinism.

### docproc-ooxml [ooxml]

From spike-ai-pipeline-office 0.1.0.

- **Toolchain.** .NET 10 to .NET 11 (`Directory.Build.props`' `net10.0`, mise's `dotnet`
  10.0.400), and System.CommandLine 2.0.14 to 3.0.0, done first. The publish's
  `-p:NuGetAudit=false`, which the ILCompiler 10.0.13 advisory forces under .NET 10, goes with
  the move. DocumentFormat.OpenXml 3.5.1 stays.
- **One run.** `--format json|markdown` goes: a compact `document` line holds the Markdown,
  properties, and media, then one `sheet` line per xlsx sheet holds its cells, where office
  writes one rendering, its JSON indented (indentation is 62% of its output, measured above). No
  line holds a whole workbook, so a reader streams it.
- **Cut.** The block tree (headings, paragraphs, lists, docx tables, their inlines), sections,
  headers and footers, notes, comments, chart blocks, and picture `alt` and `title`; their text is
  the Markdown's.
- **Into the Markdown.** A picture's `title`, when it differs from the image text, as its
  image's Markdown title, `![alt](file "title")`, which office's renderer leaves out; a
  title-only picture keeps `![title](file)`, its title already the image text. The goldens of
  `media.python-docx.docx`, `sections.python-docx.docx`, and `slide-objects.python-pptx.pptx`
  show it.
- **Kept.** The sheets' cells, one `sheet` line each, as office's `TableCell` less its docx-only
  fields; the Markdown renderer.
- **Added.** `properties` (core, app, custom, typed as stored); `media` as one list in document
  order, each with its placement (section, sheet, or slide); `embedded` packages and `ole`
  payloads with `prog_id` written as stored, which office leaves unextracted.
- **Envelope.** `line` and `schema` "2"; snake_case names (office's are camelCase); media names
  keep the stored extension only when it is 1 to 8 ASCII letters and digits, where `MediaStore`
  keeps `Path.GetExtension` whole.
- **Goldens.** `test/…/Expected/*.json` and `*.md` become one `.jsonl` and `.stderr` per fixture;
  an xlsx fixture's `.jsonl` holds its `document` line, then its `sheet` lines.

### docproc-mail [mail]

From spike-ai-pipeline-mail 0.3.1.

- **Toolchain.** The fixture generator's MimeKit 4.18.2 to 5.0.0, its fixture truth unchanged.
  The Rust crates stay.
- **Cut.** `body` (`text`, `html`, `rtf`, `derived.html`); folder `display_name`. RTF and HTML
  are still decoded, since the Markdown is made from them.
- **Into the Markdown.** When the Markdown is made from HTML, stored or recovered from the RTF,
  the text body's sentences the Markdown lacks, by the schema's content rule (CommonMark plain
  text, NFC, UAX #29 words and sentences, GFM autolinks and RFC 3986 URLs, longest common
  subsequence by Hirschberg's algorithm, Unicode 16.0), appended in order as a paragraph,
  escaped as a text body's Markdown is. The rule is the one crate under `tools/` that
  docproc-onenote shares, and this task writes it. The goldens of
  `generated/eml/03-html-inline-image`, `generated/msg/03-html-inline-image`,
  `generated/pst/mailbox`, `msg/poi-ASCII_UTF-8_CP1252_LCID1031_HTML`, and
  `msg/tika-testMSG_chinese` show it. `msg/tika-test-outlook2003`'s and `pst/libyal-outlook`'s
  append nothing: their text body differs from the HTML only in links written as `<url>` and a
  stored U+0099 where the HTML holds `™`, neither a word.
- **Kept.** The store's `display_name`, a label kept as metadata.
- **Moved.** `body.derived.markdown` up to the message's `markdown`.
- **Added.** `smtp_address` on an Exchange (`EX`) address, from `PidTagSmtpAddress`.
- **Flags.** `--attachments <dir>` becomes `--media <dir>`; `--after <id>` keeps its resume.
- **Envelope.** `schema` "2". The Markdown links an inline image by its attachment's `file`, the
  envelope's media rule, where mail's keeps the `cid:` source as written.
- **Goldens.** `tests/golden/{msg,pst,generated}/` re-recorded under "2". `.eml` goldens are
  added for the MIME path, `tests/golden/generated/eml/`, one per message the generator writes
  to `tests/fixtures/generated/eml/`; `03-html-inline-image`, which ~/docs also holds as `.eml`,
  shows the appended line "It is taped into the log book."

### docproc-onenote [onenote]

From spike-ai-pipeline-onenote 0.1.0.

- **Toolchain.** typed-path held at 0.12.4; it moves to 0.13.0 only with onenote_parser.
- **Cut.** A page's `content` (containers, outlines, paragraphs, runs, tables, tags), its `ink`
  record, and `ink_text`'s words and `language` (a Windows LCID number, not text); image
  `alt_text`, `ocr_text`, `hyperlink`, `printout_page`; ink `strokes` and `bounds`; section and
  section group `display_name`.
- **Into the Markdown.** The title's date and time paragraphs, as one line under the title
  heading, joined by one space; an image's `ocr_text` sentences its `alt_text` lacks by the
  content rule's shared crate, after the alt text in the image's Markdown text; an image's
  `hyperlink`, as a link around the image; every tag's label as `[Label]`, a task tag's after
  its task box (`- [ ] [To Do] …`). The golden of every fixture with pages (all but the refused
  `Encryption.one`) shows the date and time, and interop's `FileSyncWOPI_OneNoteWithODB.one` the
  OCR text; no fixture holds a hyperlinked image, so unit tests over a built page show it, as
  `markdown.rs`'s own tests build one.
- **Kept.** `ink_text`'s `alternatives`, a label, as the page's `ink_alternatives`, each with its
  word's index among the page's recognized words; `handwriting_recognition.one`'s golden shows them.
- **Added.** A page's `media`, flattened: each image, embedded file, and ink SVG that `content`
  and `ink` held, in the order processing's `onenote/media.go` walks them; that walk moves into
  the tool. An embedded file's `file_type` becomes `type`.
- **Envelope.** `schema` "2"; a page's `creation_time` and `modification_time` become optional,
  left out when absent.
- **Goldens.** Added, since the spike commits none. Each fixture gains a `.jsonl` and `.stderr`; the
  JSON-pointer expectations in `tests/fixtures/*.json` stay as the readable checks.

### docproc's packages [library]

From spike-ai-pipeline-processing v0.9.0, as `github.com/standards-lab/docproc`, requiring
`golang.org/x/text` alone. It holds no inference: the pipeline, `chunk`, `job`, and
`cmd/docinfer` are docinfer's.

```text
route               kept
inspect             changed: Flag gains JSON tags (check, class, evidence), the spike's docproc
                    schema.Flag
tool                kept
internal/cli        kept: exit codes 1 to 4, the stderr parse, Spool
document, picture,  kept
  media, sevenzip
audio               changed: Seam gains JSON tags (matched, matches, overlap, position), the
                    spike's docinfer outdir.Seam's names, so docinfer's inferences carry it
office              changed: decodes the schema "2" document and sheet lines, streamed as records;
                    Convert reads Request.Path, copying only a legacy document or an RTF;
                    Request.Format and the block tree's decoder go, since one run returns the
                    Markdown, properties, media, and sheets
email               changed: Message.Markdown up, Address.SMTPAddress; Body, Conversation.HTML,
                    and Folder.DisplayName go; Request.Attachments becomes Media
onenote             changed: Page.Markdown, Page.Media, and Page.InkAlternatives decode the
                    tool's fields; content.go's decoder and media.go's walk go
dispatch            changed: Input.Reader, spooled, and Input.SHA256, checked; Item.Warning and
                    Event.Warning, typed, in place of a warnings record output; each output
                    hashed into Output.SHA256; a seen part replays its whole subtree, or is
                    prepared again when a descendant's stage changed; office's stage passes
                    the part's path and makes embedded and OLE media child parts; Pool keeps
                    one context for its inputs. Index gains Renew, under the lease contract,
                    and Warnings and AppendWarning; Child becomes {Part, Outcome}, the found
                    Part whole in place of its SHA256, Name, Where, Size, and Route; FileIndex
                    renews its claims. Its shapes, beside its other types, with Version "1":
                    Event gains JSON tags, Err left out; Part takes the spike's docproc
                    schema.Part shape, its Kind, Route, Reason, and MIME moving into the new
                    ContentType; Identity takes schema.Stage's JSON names; Output takes the
                    spike's docproc JSON names, Path and Value never marshalled; Warning {tool,
                    where, what} is new, parsed from the envelope's stderr line, since the
                    spike's docproc schema has no Warning type and processing carries tool
                    warnings as []string (email.Stream.Warnings, office.Result.Warnings)
stages              moved from the root's internal/docproc/stages, less WriteTable
rundir,             moved from the root's internal/docproc/{rundir,schema} [docproc]: public
  rundir/schema     packages of this module, as "The nested modules" says
internal/goldentest, kept: test helpers that call no model
  mediatest, textscore,
  corpustest
internal/visiontest, moved to docinfer [the docinfer goal], with the integration and findings
  audiotest          tests that call a model, since docproc depends on no model client
```

processing's `onenote.Medium.Where` names a medium's outline path, from the walk. The contract's
media carry no placement, so docproc's `Where` names the medium's page and its index in `media`.
An optional placement field returns by addition if the `design` task asks for one.

### docinfer's packages [the docinfer goal]

From the root's `internal/docinfer` and processing's model-calling tests, as
`github.com/standards-lab/docinfer`, requiring docproc and go-ai.

```text
docinfer             new, the root package: Pipeline, Config, Cache, Key, and MemoryCache, from
                     the spike's docinfer infer (sources, execute), vision, transcribe, and embed,
                     their go-ai calls kept and their concurrency moved into Pipeline, and
                     outdir's Key and Hold; Vision, Transcription, and Embeddings over go-ai's
                     client, with the go-ai mapping (a 4xx other than 408 and 429 refused,
                     anything else stopping the job; usage; incomplete). Its shapes, beside its
                     other types, with Version "1": Capability, Identity, Usage, Profile,
                     Outcome, Inference, Batch (no index), Done, the Counts below, its Source
                     less Ref and File, plus the source's part, stage, and output, and its Line
                     as Chunk, embedding chunk.Chunk, less the input's file fields, its part,
                     and its output, plus Digest, its source by index into the batch; a
                     transcript's seams are docproc's audio.Seam
chunk                moved from the root's internal/docinfer/chunk; Chunk's Start and End become
                     its Span, outdir.Span moved here, and its fields gain JSON tags (text,
                     span); Splitter gains Settings, its Version and Bound
job                  new: dispatch, then inference, under one job's context, Run taking the
                     input alone; Outputs, the content store; Update, the Go side's one-of of
                     what Run yields, with no JSON, kind, or job of its own; its shapes, beside
                     its other types, under docinfer's Version: Checkpoint and Done
internal/            from the root's internal/docinfer/apptest: Serve and its replies, the fake
  endpointtest       OpenAI-compatible endpoint the pipeline's tests drive go-ai's client against
internal/visiontest, moved from processing, with the integration and findings tests that call a
  audiotest          model
```

`Counts` carries the spike's docinfer `outdir.Counts` per job, counting occurrences rather than
distinct content: each image, window, and chunk as often as the job holds it, and the cache hits
among them, so no count needs a set of every digest the job saw. `Chunks` and `Lines` go, since
`Done.Chunks` counts the chunks in the batches (a `Chunk` is one chunk in one place); `Outputs`
and the parts by outcome are dispatch's facts, in `job.Done`. Each `Usage` is less `Total`.

```go
type Counts struct {
    Texts         int                  `json:"texts"`    // text and Markdown outputs chunked
    Metadata      int                  `json:"metadata"` // metadata texts: one per job
    Embedded      int                  `json:"embedded"` // chunks with a vector
    Rejected      int                  `json:"rejected"` // chunks the endpoint rejected
    Held          int                  `json:"held"`     // of those, read from the cache
    Requests      int                  `json:"requests"` // embeddings requests sent
    Images        int                  `json:"images"`   // image outputs, vision run or not
    Vision        *VisionCounts        `json:"vision,omitempty"` // nil when skipped
    Windows       int                  `json:"windows"`
    Transcription *TranscriptionCounts `json:"transcription,omitempty"`
    Tokens        Tokens               `json:"tokens"`   // this job's requests only
}
type VisionCounts struct { // each image under its outcome; Empty of the Incomplete
    Described, Incomplete, Empty, Refused, Held, Requests int
}
type TranscriptionCounts struct { // each window under its outcome
    Transcribed, Failed, Held, Requests, Transcripts int
    SeamsMatched, SeamsUnmatched                     int
}
type Tokens struct { Vision, Transcription *Usage; Embeddings Usage }
```

`Embedded` and `Rejected` add up to `Done.Chunks`. The JSON names are the spike's, snake_case
(`seams_matched`).

### The nested modules

From the spike root's `cmd/` and `internal/`; the third column names the spike packages each
module is built from. Each command is a nested module of its library's repository, so neither
library requires go-cli-sdk or go-core.

```text
docproc/cmd/docproc    [docproc]   cmd/docproc, internal/docproc/{app, apptest, run, report};
                                   requires docproc, go-cli-sdk, go-core, and golang.org/x/sys;
                                   wires docproc's stages, keeps WriteTable; run takes schema's
                                   Statx, on golang.org/x/sys; its report is the reports task's
                                   [reports]
docinfer/cmd/docinfer  [docinfer   cmd/docinfer, internal/docinfer/{app, apptest, config,
                        goal]      infer, models, outdir, source, report}; requires docinfer,
                                   docproc, go-ai, go-cli-sdk, and go-core; builds
                                   docinfer.Vision, Transcription, and Embeddings per endpoint
                                   where vision, transcribe, and embed were; infer keeps its run
                                   command (commands.go, progress.go), its execute and sources
                                   now the root package's, and feeds Pipeline.Run from source;
                                   outdir implements docinfer.Cache over results/; its
                                   directory stays schema "1"
```

`rundir` and `rundir/schema` are public packages of docproc's base module, with no module or tag
of their own. `cmd/docinfer`, in docinfer's repository, reads `cmd/docproc`'s run directory through
them, and Go's internal rule is by import path, so docproc's `cmd/internal` would bar it. They
version with the library, which both commands require. `schema` aliases the moved shapes
(`dispatch.Part`, `ContentType`, `Identity` as its `Stage`, and `inspect.Flag`). Its `Output`
stays the spike's docproc own, since `dispatch.Output` carries a `sha256` that `cmd/docproc`'s
schema "1" record lacks, so its record bytes stay unchanged. Its `Statx` (`stat_linux.go`, on
`golang.org/x/sys/unix`) moves to `cmd/docproc`'s `run`, its one caller, so the base module
requires `golang.org/x/text` alone.

`cmd/docinfer`'s tests write the run directories they read with `rundir`, rather than running
`cmd/docproc`'s app, which is internal to it; its `apptest` takes the `Recorder` it used
from the spike's docproc `apptest` as its own.

### Requests to go-ai

Requests to v1.ai.client, which docinfer and the capstone build on:

- A transcription request that can reopen its clip, such as `TranscriptionRequest.Open func()
  (io.ReadCloser, error)`, so `Transcribe` joins the one retry loop and honors Retry-After; or,
  failing that, the Retry-After an `*APIError` carried.
- `RetryPolicy`'s retry of 408, 429, and 503 proven against the live gateway, not by unit tests
  alone.
- The surface above kept: `Generate`, `GenerateStream` with `Accumulator`, `Embed`,
  `Transcribe`, `*APIError`'s `Status` and `Message`, the usage fields docinfer maps, and
  `Config.Query` and the three formats for `cmd/docinfer`.
- A consumer key in a header other than the bearer token, only if the gateway asks for one.

### Requests to go-messaging

Queued as data-intelligence.md's asks, not changed now ("Streams and subjects"): a publish on a
subject the caller names, where the NATS provider publishes on the event's type; a durable
consumer filtered by subject, where it filters by types; the CloudEvents `sequence` extension
carried; and events delivered in order within a delivery group.

## Assumptions

- Assumes docproc's dispatch keeps asking each tool for its Markdown alone as the text it hands to
  inference, as processing's stages do.
- Assumes Athena builds `sparse_rows` from typed cells, not from Markdown (`athena-mapping`).
- Assumes docproc keeps processing's package names `office`, `email`, and `onenote`, not the
  tools' names: each does more than decode its tool (LibreOffice's legacy conversion, threading).
- Assumes the spike's docproc `stages` belongs in docproc, so Cerebro and `cmd/docproc` wire stages
  alike, and its `schema` and `rundir` become docproc's public `rundir` and `rundir/schema`, so
  `cmd/docinfer` in docinfer's repository reads a `cmd/docproc` run directory.
- Assumes inference needs nothing of docproc beyond `dispatch` (its dispatcher, events, and
  shapes), `audio`'s join and its `Seam`, and, for `cmd/docinfer`, `rundir`, so docproc depends on
  no model concept and imports no docinfer package.
- Assumes every stream input is spooled to scratch before its stages run, since every stage and
  tool reads a path; no stage reads a stream directly.
- Assumes one `docinfer.Pipeline` per replica, whose per-capability bounds every concurrent job
  shares, rather than bounds per job.
- Assumes a result batch closes at 512 KiB or when no embedded chunk is waiting; the capstone's
  `events` task may tune the cap.
- Assumes an extraction's value fits an event; the `events` task may move a larger one to the
  object store by its digest.
- Assumes Cerebro sets the index's retention no longer than `job.Outputs`', so a seen part's
  output resolves by its SHA-256; a miss fails the part, so it is reported, never dropped.
- Assumes Cerebro's extraction stage (a caller's prompt and JSON schema) is Cerebro's own, on
  go-ai, sharing `docinfer.Cache` under its own capability name; no spike has it.
- Assumes the typed metadata shapes wait for the capstone's `metadata` task; until then,
  `Output.Meta` and the tools' lines carry metadata.
- Assumes Cerebro speaks Chat Completions to the gateway, and its consumer key is the bearer
  token; a key in another header is v1.ai.client's to add if the gateway asks for one.
- Assumes a 4xx other than 408 and 429 is the endpoint's verdict on the content, recorded and
  held, and every other failure, every 5xx among them, stops the job, so the work queue's
  redelivery retries it.
- Assumes extraction runs once per source text, whole, and relays its deltas coalesced to one
  event a second; a text past the model's context is refused, recorded.
- Assumes go-ai's library releases before docinfer is built; the docinfer goal holds until it
  does.
- Assumes go-messaging gains per-subject publish and filter, the `sequence` extension, and
  ordering within a delivery group before Cerebro's `events` task builds on it.
- Assumes the consumer stores chunks by their identity (job, part, source, index), so a batch a
  redelivered job repeats changes nothing (the `chunks` task).
