# layers

Cerebro's stack has four layers, each a boundary below: the system tools (quarry-ooxml,
quarry-mail, quarry-onenote) → quarry, the Go processing library → go-ai, the model client →
Cerebro, the data-intelligence capstone's processing service. Each signature or schema here traces
to a capstone task in `standards-lab/context/data-intelligence.md` ("Design") or to spike
evidence. Spike paths are relative to `~/experiments/`; "processing" is
spike-ai-pipeline-processing, whose `office`, `email`, `onenote`, and `dispatch` packages are the
consumers the tools are measured against.

## The tool contract

Each tool runs as a process behind quarry's `tool` package (processing `tool/tool.go`), reads one
input, and writes one contract to stdout: JSON Lines, schema "2", with Markdown inside it as the
text layer. Structured fields carry only what the Markdown can't and a consumer reads.

### Common envelope

- **Lines.** stdout holds JSON Lines and nothing else, one compact object per line. Each line's
  `line` names its kind; the first line carries `schema` (`"2"`) and the input's kind. quarry
  decodes line by line, keeping a line, block, or container of an unknown kind as an `Unknown*`
  value, as processing's `office/model.go`, `email/model.go`, and `onenote/model.go` do.
- **Absence.** A field with no value, or an empty list, is left out, never `null`.
- **Text layer.** A record that holds text carries it as `markdown`: quarry-ooxml's document,
  quarry-mail's message, quarry-onenote's page. Literal text is escaped by htmd's rules, which
  mail's and onenote's Markdown share (onenote README, "Markdown").
- **Media.** With `--media <dir>`, each distinct payload (picture, embedded file, OLE payload,
  attachment, ink SVG) is written once into `<dir>`, created if missing, as stored, before the
  line that names it. Its name is the lowercase hex SHA-256 of its bytes plus `.` and the stored
  extension when that is 1 to 8 ASCII letters and digits. A record names it in `file`, and the
  Markdown links it by that name. Without `--media` nothing is written to disk and the names stand.
  A linked or by-reference target is named, never fetched.
- **stderr.** One line each: `<tool>: warning: <where>: <what>`, and for exit codes 1 to 4
  `<tool>: error: <reason>`.
- **Exit codes.** 0 read (records skipped included); 1 stdout or media not written; 2 usage; 3
  refused before any output; 4 a fault of the tool's own. All three spikes already use these
  meanings (office, mail, and onenote READMEs, "Usage").
- **Determinism.** The same input gives byte-identical stdout and media, proven by committed
  golden outputs (`.jsonl` and `.stderr` per fixture, as mail's `tests/golden/`); no JSON Schema.
- **Out of the tools.** Batch IDs, per-item digests, provenance, warnings as data, and typed
  metadata are quarry's and Cerebro's.

### quarry-ooxml

From spike-ai-pipeline-office 0.1.0 (`src/SpikeAiPipeline.Office/Model/Document.cs`, README "The
JSON model"), which writes either the model (indented JSON) or Markdown, never both. quarry-ooxml
writes one line per document:

```text
document  line, schema, format (docx|xlsx|pptx), properties?, markdown, media?, sheets?
properties  core?, app?, custom?: maps of the package's core, extended, and custom properties,
            each value typed as stored (string, number, boolean, ISO 8601 date)
media[]   kind (picture|embedded|ole), file?, content_type?, part?, target?, prog_id?,
          section? | sheet? | slide?   in document order
sheets[]  name, hidden?, rows[]: row, cells[]: ref, value_type?, value?, formula?,
          shared_from?, col_span?, row_span?
```

- `markdown` is office's Markdown rendering unchanged (`Rendering/MarkdownRenderer.cs`, the
  goldens `test/SpikeAiPipeline.Office.Tests/Expected/*.md`): headings, lists, GFM tables, charts
  as tables of their cached series, notes and comments as GFM footnotes, slides as `# Slide n`.
- `media` holds every picture office writes today (`Media` blocks, `Reading/MediaStore.cs`), plus
  embedded packages (`embedded`, the part as stored) and OLE objects (`ole`, the native payload,
  `prog_id` the object's), which office leaves unextracted (README "What office does not do").
  `part` is the package part, `target` a linked picture's URL; the placement is the docx section,
  xlsx sheet, or pptx slide index.
- `sheets` keeps an xlsx's typed cells, office's `TableCell` less its docx-only fields.
- Field names move to snake_case, as mail's and onenote's are.

### quarry-mail

From spike-ai-pipeline-mail 0.3.1 (`src/model.rs`, README "Schema \"1\"", goldens
`tests/golden/{msg,pst,generated}/`; `.eml` and mbox scored against
`tests/fixtures/mime/truth.json`). Lines `store` and `skipped` keep their schema "1" fields, and
`folder` keeps them less `display_name`, which is always its `path`'s last name (58 of 58 folders
in the run below). A message changes:

```text
message   line, id?, folder?, class?, subject?, sender?, to?, cc?, bcc?, submit_time?,
          delivery_time?, creation_time?, modification_time?, importance?, sensitivity?,
          transport_headers?, message_id?, in_reply_to?, references?, conversation_topic?,
          conversation_index?, markdown?, attachments?
address   name?, address?, address_type?, smtp_address?
attachment  file?, filename?, mime_type?, content_id?, inline, method, size?, message?
```

- `markdown` is schema "1"'s `body.derived.markdown`, moved up; `body` and its `text`, `html`,
  `rtf`, and `derived.html` are cut (measured below).
- `smtp_address` is an Exchange (`EX`) address's SMTP address (`PidTagSmtpAddress`), which schema
  "1" leaves out (README "Known limits").
- `--attachments <dir>` becomes `--media <dir>`; `--after <id>` keeps its resume.

### quarry-onenote

From spike-ai-pipeline-onenote 0.1.0 (`src/model.rs`, README "Schema \"1\""). It commits no golden
outputs: its tests check JSON-pointer expectations (`tests/fixtures/{sections,notebooks,content}.json`)
and run-to-run byte identity, so quarry-onenote adds goldens. Lines `notebook` and `skipped` keep
their schema "1" fields, and `section_group` and `section` keep them less `display_name`, always
their `path`'s last name (16 of 16). A page changes:

```text
page      line, id, section, level, title?, creation_time?, modification_time?, author?,
          markdown?, media?
media[]   kind (image|embedded_file|ink), file?, filename?, size?, extension?, type?
          in the order processing's Page.Media walks them (onenote/media.go)
```

- `content` (containers, outlines, paragraphs, runs, tables, tags), the page's `ink` record, and
  `ink_text` are cut: their text is in `markdown`, and the media they hold move to `media`.

### Versioning

The contracts move to schema "2". Every spike writes "1", and the settled additions alone
(`properties`, OLE media, `smtp_address`) would fit "1"; but the cuts below remove fields each
spike reader of "1" declares, and processing's readers accept exactly "1" (`decodeDocument` in
`office/model.go`, `decodeLine` in `email/model.go` and `onenote/model.go`), so "2" makes an old
reader refuse instead of reading zeros. From "2" on, each tool's schema grows by addition only
(an optional field, a line or media kind); a removal, rename, or change of meaning moves it to
the next version.

## Payload measurement

The three spikes were built from their `HEAD` and run over every top-level input in `~/docs` of
their formats; no golden fallback was needed. Parts nested in archives or attachments, and legacy
formats LibreOffice converts first, are outside it.

```sh
S=<scratch dir>
for t in office mail onenote; do mkdir -p $S/src/$t
  git -C ~/experiments/spike-ai-pipeline-$t archive HEAD | tar -x -C $S/src/$t; done
(cd $S/src/mail && CARGO_TARGET_DIR=$S/target-mail cargo build --release --locked)
(cd $S/src/onenote && CARGO_TARGET_DIR=$S/target-onenote cargo build --release --locked)
(cd $S/src/office && mise trust . && mise exec -- dotnet publish src/SpikeAiPipeline.Office \
   -c Release -r linux-x64 -p:NuGetAudit=false -o $S/office-bin)
# per office input (docx docm dotx dotm xlsx xlsm xltx xltm pptx pptm potx potm ppsx):
$S/office-bin/spike-ai-pipeline-office "$f" --media $o/media > $o/model.json
$S/office-bin/spike-ai-pipeline-office "$f" --format markdown > $o/text.md
# per mail input (eml msg pst mbox):
$S/target-mail/release/spike-ai-pipeline-mail --attachments $o/media "$f" > $o/out.jsonl
# per OneNote section (.one; notebooks would count their sections twice):
$S/target-onenote/release/spike-ai-pipeline-onenote --media $o/media "$f" > $o/out.jsonl
```

office's publish needs `-p:NuGetAudit=false`: restore fails on Microsoft.DotNet.ILCompiler
10.0.13's advisory GHSA-p3jp-9v83-76w9 as an error, which the move to .NET 11 clears.

The measure: a structured text field is a string the contract carries outside its Markdown; its
bytes are the string's UTF-8 bytes before JSON escaping. A string is duplicated when, with
whitespace collapsed and Markdown backslash escapes removed, it occurs in its record's Markdown
(office's document, onenote's page). A mail body form (`text`, `html`, `rtf`, `derived.html`) is an
alternative of the Markdown made from it, and counts whole. Output is the bytes on stdout; office's
is the model plus its Markdown as one JSON string, as one run will write them. The run reproduced
byte for byte.

| Tool | Read | Output | Markdown | Structured text | Duplicated | Share |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| office | 85 of 85 (43 docx, 25 xlsx, 17 pptx families) | 622,303 B | 53,972 B | 36,179 B | 35,526 B | 5.7% |
| mail | 72 of 79 (47 eml, 3 mbox, 16 msg, 6 pst); 599 messages | 5,034,929 B | 1,486,558 B | 3,057,909 B | 1,713,649 B | 34.0% |
| onenote | 16 of 17 sections; 20 pages | 136,702 B | 29,706 B | 26,802 B | 24,765 B | 18.1% |

Refused: 7 cpython `.eml` fixtures that are no message (`msg_11`, `18`, `19`, `37`–`40`), and
`Encryption.one`, password-protected; both as their READMEs state.

- **office.** 98% of the model's text is in the Markdown: inline text 24,237 B (24,145
  duplicated), text cell values 5,547 B (5,505), chart categories, names, and titles 3,288 B (all),
  comment anchors and authors 1,024 B (all), picture alt text 797 B (all), sheet names 341 B (all);
  formulas 534 B (67, since Markdown shows a formula only without a value). The larger waste is
  the model's shape: 564,912 B written indented is 182,072 B compact, so indentation is 382,840 B,
  62% of the output; compact, the text block tree is 122,509 B beside sheet tables 40,506 B, media
  blocks 9,773 B, and charts 9,284 B. By family: docx 7.3%, xlsx 4.4%, pptx 3.4% duplicated.
- **mail.** Body forms are the duplication: `text` 1,461,216 B, `rtf` 145,003 B, `derived.html`
  56,631 B, `html` 50,332 B. A `.msg` is 78% duplicated (RTF and the HTML recovered from it), a
  `.pst` 46%, an mbox 31%, an `.eml` 7%. `transport_headers` is 1,305,095 B, 26% of the output;
  114,724 B of it repeats the parsed subject, addresses, and threading fields.
- **onenote.** Alt text 9,684 B (all), run text 7,482 B (6,944), image `ocr_text` 5,572 B (4,926,
  every duplicated byte of it reaching the Markdown inside the same image's alt text), filenames 3,240 B (2,865),
  ink alternatives 79 B (8). Media written over mail and onenote: 235 MB.

## Consumers of structured text

A field stays when a consumer reads it: a processing package, or a capstone task. The rest are
cut. docinfer embeds text and Markdown outputs only, never records
(`spike-ai-pipeline/internal/docinfer/infer/sources.go`, `textKinds`), and chunks Markdown at its
headings (`internal/docinfer/chunk/doc.go`), so the Markdown is the text's one consumer path.

| Tool | Field | Consumer | |
| --- | --- | --- | --- |
| ooxml | `markdown` | `dispatch/office.go` (asks office for Markdown alone) | kept |
| ooxml | text block tree: heading, paragraph, list, docx table inlines; sections, notes, comments (author, anchor, replies) | none: `dispatch/office.go` never requests the model | **cut** |
| ooxml | chart title, series names, categories, values | none; the Markdown's table holds them | **cut** |
| ooxml | picture `alt`, `title` | none; `alt` is the Markdown's image text | **cut** |
| ooxml | sheet `name`, cell `value`, `formula` | `athena-mapping` (Athena's tabular rows into `sparse_rows` need typed cells) | kept |
| ooxml | `properties` | `metadata` | added |
| mail | `markdown` | `dispatch/email.go` (`own`), `email/model.go` (`Conversation`) | kept |
| mail | `body.text`, `body.html`, `body.rtf`, `body.derived.html` | none; `Conversation.HTML` copies `html` and nothing reads it | **cut** |
| mail | `subject` | `email/threads.go` (`baseSubject`), `dispatch/email.go` meta | kept |
| mail | `conversation_topic` | `email/model.go` (`Conversation.Topic`) | kept |
| mail | `transport_headers` | `metadata`: typed mail metadata is quarry's and Cerebro's, built from the raw headers | kept |
| mail | address `name`, `address`, `smtp_address` | `email/model.go` (participants), `metadata` | kept |
| mail | attachment `filename` | `email/route.go`, `dispatch/email.go` (`Found` name) | kept |
| mail | folder `path` | `dispatch/email.go` meta | kept |
| mail | folder `display_name` | none; it is `path`'s last name | **cut** |
| onenote | `markdown` | `dispatch/onenote.go` (`page`) | kept |
| onenote | page `title` | `dispatch/onenote.go` meta | kept |
| onenote | runs' `text`, `latex`, `link`; tag `label`; list markers; table cells | none; the Markdown renders them | **cut** |
| onenote | image `alt_text`, `hyperlink`, `ocr_text` | none; `ocr_text` is within `alt_text`, and vision reads every image | **cut** |
| onenote | `ink_text` (words, `alternatives`, `language`) | none; the words are the Markdown's ink link text | **cut** |
| onenote | media `filename` | `dispatch/onenote.go` (`Found` name) | kept |
| onenote | `author`, times; notebook `name`; section and group `path` | `dispatch/onenote.go` meta, `metadata` | kept |
| onenote | section and group `display_name` | none; it is `path`'s last name | **cut** |

`text-layer` is the one task that may want `ocr_text` back, as an image's existing text layer; it
returns by addition.

## Assumptions

- Assumes quarry's dispatch keeps asking each tool for its Markdown alone as the text it hands to
  inference, as processing's stages do.
- Assumes Athena builds `sparse_rows` from typed cells, not from Markdown (`athena-mapping`).

## quarry's API

Filled by slice 2.

## go-ai's surface

Filled by slice 3.

## Cerebro's API and events

Filled by slice 3.

## Changes per tool and per quarry package

Filled by slice 4.
