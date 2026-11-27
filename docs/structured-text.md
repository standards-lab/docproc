# Consumers of structured text

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
docproc, docinfer, or Bunrei), with the layer that holds a form of it beside; a cut field's text is
owned where it is carried. Capstone task is the task of `standards-lab/context/data-intelligence.md`
("Design") that reads the field, or, where none does, the docproc stage that reads it. Consumer
paths are spike-ai-pipeline-processing's ([`layers.md`](layers.md)).

| Tool | Field | Consumer | Owner | Capstone task | |
| --- | --- | --- | --- | --- | --- |
| ooxml | `markdown` | `dispatch/office.go` (asks office for Markdown alone) | tool; docinfer chunks it | chunks | kept |
| ooxml | text block tree: heading, paragraph, list, docx table inlines; sections, notes, comments (author, anchor, replies) | none: `dispatch/office.go` never requests the model; the Markdown holds their text | tool, in `markdown` | chunks, through `markdown` | **cut** |
| ooxml | chart title, series names, categories, values | none; the Markdown's table holds them | tool, in `markdown` | chunks, through `markdown` | **cut** |
| ooxml | picture `alt` | none; it is the Markdown's image text | tool, in `markdown` | chunks, through `markdown` | **cut** |
| ooxml | picture `title` | none; the Markdown lacks it unless it is the image text, and takes it as [`tool-contract.md`](tool-contract.md), "docproc-ooxml", states | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| ooxml | external hyperlink targets | none; the Markdown lacks them, and renders each as a link, an internal anchor staying text | tool, in `markdown` | chunks, through `markdown` | into Markdown |
| ooxml | sheet `name`, cell `value`, `formula` | `athena-mapping` (Athena's tabular rows into `sparse_rows` need typed cells); names and values are in the Markdown, a formula is its cell's expression, whose value the Markdown shows | tool; office's stage yields each sheet's rows as a record output | athena-mapping | kept |
| ooxml | `media` (`file`, `part`, `target`, `prog_id`, placement) | office's stage (embedded and OLE media become child parts); references, not text, and a linked picture's `target` is also its Markdown image's source | tool; docproc makes embedded and OLE media child parts | none: office's stage | added |
| ooxml | `properties` | `metadata` | tool | metadata | added |
| mail | `markdown` | `dispatch/email.go` (`own`), `email/model.go` (`Conversation`) | tool; docinfer chunks it | chunks | kept |
| mail | `body.html`, `body.derived.html` | none; `Conversation.HTML` copies `html` and nothing reads it; the Markdown is made from them | tool, in `markdown` | chunks, through `markdown` | **cut** |
| mail | `body.text`, `body.rtf` | none; the Markdown holds their text, but 295 B of text bodies beside an HTML body (and 28 B of RTF, the same line), alternatives to the HTML that the tool doesn't append, as [`tool-contract.md`](tool-contract.md), "docproc-mail", states | tool, in `markdown` | chunks, through `markdown` | **cut** |
| mail | `subject` | `email/threads.go` (`baseSubject`), `dispatch/email.go` meta | tool | metadata | kept |
| mail | `message_id`, `in_reply_to`, `references`, `conversation_index` | `email/threads.go` | tool; docproc's `Threader` threads by them | none: the email stage's `Threader` | kept |
| mail | `conversation_topic` | `email/model.go` (`Conversation.Topic`) | tool | none: the email stage's `Conversation` | kept |
| mail | `transport_headers` | `metadata`: typed mail metadata is docproc's and Bunrei's, built from the raw headers | tool; typed by docproc and Bunrei, as `metadata` settles | metadata | kept |
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
| onenote | tag `label` | none; the Markdown renders it as [`tool-contract.md`](tool-contract.md), "docproc-onenote", states, since a task box carries the task state, not the label's words | tool, in `markdown` | chunks, through `markdown` | into Markdown |
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
| all | stderr warnings (`<tool>: warning: <where>: <what>`) | `internal/cli`, parsed to `dispatch.Warning` | docproc, as data; the tool writes the line | events, as Bunrei's warning event | kept |

`text-layer` reads an image's OCR text from the Markdown's image text; the contract carries no
`ocr_text` field.

