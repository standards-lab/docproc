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
`tests/fixtures/mime/truth.json`). Line `skipped` keeps its schema "1" fields; `store` keeps them
less `display_name`, a .pst's name that no processing package reads; and `folder` keeps them less
`display_name`, which is always its `path`'s last name (58 of 58 folders in the run below). A
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
whitespace collapsed to one space and trimmed, it occurs in its record's Markdown with Markdown
backslash escapes removed and whitespace collapsed the same way (office's document, mail's message,
onenote's page). A mail body form (`text`, `html`, `rtf`, `derived.html`) is an alternative of the
Markdown made from it, and counts whole. The fields counted:

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
  alt text 797 B (all), sheet names 341 B (all); formulas 534 B (67, since Markdown shows a formula only without a value). The larger waste is
  the model's shape: 564,912 B written indented is 182,072 B compact, so indentation is 382,840 B,
  62% of the output; compact, the text block tree is 122,509 B beside sheet tables 40,506 B, media
  blocks 9,773 B, and charts 9,284 B. By family: docx 7.3%, xlsx 4.4%, pptx 3.4% duplicated.
- **mail.** Body forms are the duplication: `text` 1,461,216 B, `rtf` 145,003 B, `derived.html`
  56,631 B, `html` 50,332 B. A `.msg` is 78% duplicated (RTF and the HTML recovered from it), a
  `.pst` 46%, an mbox 31%, an `.eml` 7%. `transport_headers` is 1,305,095 B, 26% of the output;
  114,724 B of it repeats the parsed subject, addresses, and threading fields.
- **onenote.** Alt text 9,684 B (all), run text 7,482 B (6,944), image `ocr_text` 5,572 B (4,926,
  every duplicated byte of it reaching the Markdown inside the same image's alt text), filenames
  3,240 B (2,865), ink alternatives 79 B (8). Media written over mail and onenote: 235 MB.

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
| mail | `message_id`, `in_reply_to`, `references`, `conversation_index` | `email/threads.go` | kept |
| mail | `conversation_topic` | `email/model.go` (`Conversation.Topic`) | kept |
| mail | `transport_headers` | `metadata`: typed mail metadata is quarry's and Cerebro's, built from the raw headers | kept |
| mail | address `name`, `address`, `smtp_address` | `email/model.go` (participants), `metadata` | kept |
| mail | attachment `filename` | `email/route.go`, `dispatch/email.go` (`Found` name) | kept |
| mail | folder `path` | `dispatch/email.go` meta | kept |
| mail | folder `display_name` | none; it is `path`'s last name | **cut** |
| mail | store `display_name` | none; `email/model.go` decodes it and `dispatch/email.go` passes the store through | **cut** |
| onenote | `markdown` | `dispatch/onenote.go` (`page`) | kept |
| onenote | page `title` | `dispatch/onenote.go` meta | kept |
| onenote | runs' `text`, `latex`, `link`; tag `label`; list markers; table cells | none; the Markdown renders them | **cut** |
| onenote | image `alt_text`, `hyperlink`, `ocr_text` | none; `ocr_text` is within `alt_text`, and vision reads every image | **cut** |
| onenote | `ink_text` (words, `alternatives`, `language`) | none; the words are the Markdown's ink link text | **cut** |
| onenote | media `filename` | `dispatch/onenote.go` (`Found` name) | kept |
| onenote | notebook `name`; section and group `path` | `dispatch/onenote.go` meta, `metadata` | kept |
| onenote | page `author`, times | `metadata` | kept |
| onenote | section and group `display_name` | none; it is `path`'s last name | **cut** |

`text-layer` is the one task that may want `ocr_text` back, as an image's existing text layer; it
returns by addition.

## Assumptions

- Assumes quarry's dispatch keeps asking each tool for its Markdown alone as the text it hands to
  inference, as processing's stages do.
- Assumes Athena builds `sparse_rows` from typed cells, not from Markdown (`athena-mapping`).
- Assumes quarry keeps processing's package names `office`, `email`, and `onenote`, not the
  tools' names: each does more than decode its tool (LibreOffice's legacy conversion, threading).
- Assumes docproc's `stages` belongs in the library, so Cerebro and docproc wire stages alike;
  `cmd/internal` keeps only `schema` and `rundir`.
- Assumes every stream input is spooled to scratch before its stages run, since every stage and
  tool reads a path; no stage reads a stream directly.
- Assumes one `infer.Pipeline` per replica, whose per-capability bounds every concurrent job
  shares, rather than bounds per job.
- Assumes a result batch closes at 512 KiB or when no embedded chunk is waiting; the capstone's
  `events` task may tune the cap.
- Assumes an `inference` event carries its text whole. An hours-long transcript could pass 1 MB,
  and the `events` task may move such a text to the object store by its digest.
- Assumes Cerebro sets the index's retention no longer than the cache's, so a seen part's output
  `Ref` resolves; a miss fails the part, so it is reported, never dropped.
- Assumes Cerebro's extraction stage (a caller's prompt and JSON schema) is Cerebro's own, on
  go-ai, sharing `infer.Cache` under its own capability name; no spike has it.
- Assumes the typed metadata shapes wait for the capstone's `metadata` task; until then,
  `Output.Meta` and the tools' records carry metadata.
- Assumes Cerebro speaks Chat Completions to the gateway, and its consumer key is the bearer
  token; a key in another header is v1.ai.client's to add if the gateway asks for one.
- Assumes an `*APIError` of 408, 429, or 503 that outlasts the retry policy stops the job rather
  than being recorded as refused, so the work queue's redelivery retries it.
- Assumes Cerebro and `cmd/docinfer` each keep their own go-ai adapter, since quarry's library
  stays free of go-ai and `cmd/docinfer` is quarry's one module that requires it.
- Assumes extraction runs once per source text, whole, and relays its deltas coalesced to one
  event a second; a text past the model's context is refused, recorded.
- Assumes a job's events share one stream, each job on its own subject, rather than a stream per
  job; the `state` task may revisit it.
- Assumes the consumer stores chunks by their identity (job, part, source, index), so a batch a
  redelivered job repeats changes nothing (the `chunks` task).

## quarry's API

The library module, `github.com/standards-lab/quarry`, requires `golang.org/x/text` only and
imports no model client. processing already imports spike-ai-pipeline-client only from tests and
the test helpers `internal/visiontest` and `internal/audiotest`; docinfer's client calls
(`spike-ai-pipeline/internal/docinfer/{vision,transcribe,embed}`) move behind `infer`'s
interfaces. `cmd/docinfer` implements them with go-ai and is the only module that imports it;
Cerebro implements them with go-ai too. "Kept" below means processing's signature carries over
as it is.

```text
route, inspect             what a file is, whether it is safe to open, which stage reads it  kept
tool                       the exec and container runners                                    kept
internal/cli               the tools' shared contract: exit codes, stderr lines, Spool       kept
office, email, onenote     a tool's reader, decoding its schema "2"                         changed
document, picture, audio,  the other formats, on their tools                                 kept
  media, sevenzip
dispatch                   stages, the walk over an input's parts, the index                 changed
stages                     the stages wired over the host's tools                            moved from docproc
chunk                      the chunker                                                       moved from docinfer
infer                      the inference interfaces, the cache, the pipeline                 new, from docinfer
event                      the exported event shapes                                         new, from docproc's schema
job                        one job: dispatch, then inference, under the job's context        new
```

### Inspection and routing

Kept (processing `route/route.go`, `route/identify.go`, `inspect/inspect.go`). The dispatcher
runs both on every part before its stage, so a part event carries its route and flags; a caller
calls them directly only to triage a file before queueing it.

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
type Flag struct { Check Check; Class Class; Evidence string }
type Report struct { Kind route.Kind; Flags []Flag; Verdict Verdict }
type Options struct { Policy map[Check]Verdict; BombRatio float64; BombSize uint64 }
func Inspect(ctx context.Context, path, name string, opts Options) (Report, error)
func CheckListing(kind route.Kind, size int64, entries []Listed, opts Options) (Report, error)
```

### Running the tools

`tool` is kept (processing `tool/tool.go`, `tool/exec.go`, `tool/container.go`). Cerebro runs
the exec runner inside an image that bundles the tools (`tool-packaging`). The container runner
stays for hosts like docproc's, since it needs the docker socket (data-intelligence.md, "Missing
layers").

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
parse. The record types follow the contracts: `office.Document` becomes the `document` line,
`email.Message` takes `Markdown` up and `Address.SMTPAddress`, and `onenote.Page` takes
`Markdown` and `Media`.

```go
package email // onenote has the same shape
func NewReader(cfg Config) (*Reader, error) // Config{Mail *tool.Tool}
func (r *Reader) Read(ctx context.Context, req Request) (*Stream, error)
type Request struct { Input string; Media string; After uint64 } // Attachments becomes Media (--media)
func (s *Stream) Records() iter.Seq2[Record, error]

package office
func NewConverter(ctx context.Context, cfg Config) (*Converter, error)
func (c *Converter) Convert(ctx context.Context, doc io.Reader, req Request) (*Result, error)
```

### Input from a stream, or a path

```go
package dispatch
type Input struct {
    Path   string    // a file the caller owns; the dispatcher never removes it
    Reader io.Reader // or a stream, spooled into Config.Dir and hashed as it is copied,
                     // removed with the part
    Name   string    // the stored name, whose extension routing reads
    MIME   string    // the stored MIME type, if any
    After  uint64    // resume a store after this message, mail's --after
}
```

`Reader` is new: processing's `Input` is a path alone. The stages read paths, since mail,
onenote, qpdf, and ffprobe take one, and `office.Converter.Convert` already spools its reader
through `internal/cli.Spool`. So one spool at the input serves every stage. Cerebro passes the
object store's reader for a chunked upload, or the bytes of a small document's Command.

### Dispatch

Kept (processing `dispatch/dispatch.go`, `dispatch/stage.go`, `dispatch/pool.go`): `Dispatch`
yields each output as its stage makes it, and each part's own event once its outcome is known.

```go
type Stage interface {
    Identity() Identity // name, its tools' version, its configuration's hash
    Prepare(ctx context.Context, p Part, dir string) iter.Seq2[Item, error]
}
type Stages map[route.Route]Stage
type Config struct { Stages Stages; Index Index; Dir string; Limits Limits; Inspect inspect.Options }
func New(cfg Config) (*Dispatcher, error)
func (d *Dispatcher) Dispatch(ctx context.Context, in Input) iter.Seq2[Event, error]
func (d *Dispatcher) Pool(ctx context.Context, inputs iter.Seq[Input], workers int) iter.Seq2[PoolEvent, error]

type Event struct {
    Part Part; Output *Output; Outcome Outcome; Reason string; Err error
    Stage Identity; Recorded Outcome; Outputs, Children int
}
func (e Event) Keep() // keep the output's file, or the part's, past the loop body
type Outcome string   // prepared, no processor yet, seen, cycle, quarantined, encrypted,
                      // failed, limited
type Output struct {
    Kind OutputKind // text, markdown, image, record, audio
    Name, MediaType, Path string; Value []byte; Meta map[string]string; Ref string
}
type Item struct { Output *Output; Part *Found; Warning *Warning }
type Warning struct { Tool, Where, What string }
```

`Item.Warning` is new. processing yields a tool's stderr warnings as a record output, but the
common envelope's stderr line (`<tool>: warning: <where>: <what>`) parses to typed data, and
warnings as data are quarry's.

`stages` moves from `spike-ai-pipeline/internal/docproc/stages`, less its `WriteTable`, so
docproc and Cerebro wire the same stages from the tools a host holds:

```go
package stages
type Lookup func(program string) (string, error)
func Build(ctx context.Context, lookup Lookup, exclude []route.Route) Set
type Set struct { Stages dispatch.Stages; Tools []Tool; Rows []Row } // Row: route, wired|missing|excluded
```

### Index and cache

The index is kept (processing `dispatch/index.go`, `memory.go`, `file.go`). It is
content-addressed by a part's SHA-256 and its stage's identity. One claimant prepares the
content, and every other claimant waits for its entry, so replicas sharing an index prepare
content once. Cerebro backs it with JetStream (the capstone's `state` task); `MemoryIndex` and
`FileIndex` stay for tests and docproc.

```go
type Key struct { SHA256 string; Stage Identity }
type Index interface {
    Lookup(ctx context.Context, key Key) (Entry, bool, error)
    Outputs(ctx context.Context, key Key) iter.Seq2[Output, error]
    Children(ctx context.Context, key Key) iter.Seq2[Child, error]
    Claim(ctx context.Context, key Key) (token string, ok bool, err error)
    Wait(ctx context.Context, key Key) (Entry, bool, error)
    Append(ctx context.Context, key Key, token string, o Output) error
    AppendChild(ctx context.Context, key Key, token string, c Child) error
    Record(ctx context.Context, key Key, token string, e Entry) error
    Release(ctx context.Context, key Key, token string) error
    Flags(ctx context.Context, sha256 string) ([]inspect.Flag, bool, error)
    RecordFlags(ctx context.Context, sha256 string, flags []inspect.Flag) error
}
```

The cache is new. It generalizes docinfer's held results (`outdir.Dir.Held` and `Hold`, keyed by
`outdir.Key`) to bytes behind an interface, and Cerebro backs it with its object store.

```go
package infer
// Cache holds values by key; a miss is (nil, false, nil).
type Cache interface {
    Get(ctx context.Context, key string) ([]byte, bool, error)
    Put(ctx context.Context, key string, value []byte) error
}
// Key is docinfer's outdir.Key, prefixed "<capability>/": the SHA-256 of the capability, the
// model, the settings that change the result, and the content's SHA-256, length-prefixed.
func Key(capability, model, settings, content string) string
func NewMemoryCache() *MemoryCache
```

A held vision or transcription result holds its text inline, not as a file under docinfer's
`texts/`. `job` also puts each text and Markdown output's bytes under `output/<sha256>` and sets
the output's `Ref` to that key. A part the index reports seen therefore replays outputs whose
text the cache still holds, which data-intelligence.md ("Caching") asks for: content seen before
is replayed, not recomputed. `cmd/docinfer`'s `outdir` implements `Cache` over its `results/`.

### Inference interfaces

quarry defines these, and any model client implements them. Each must be safe for concurrent
use. The pipeline holds the concurrency bounds, the batching, the cache, and the retry of a
rejected embedding batch one text at a time, all of which docinfer's `vision`, `transcribe`, and
`embed` packages hold beside their client calls today.

```go
package infer
type Identity = event.Identity // capability, model, and the settings that change its result
type Usage = event.Usage       // tokens, as the reply reports them
type Profile = event.Profile

type Image struct { Path, MediaType, SHA256 string }
type Window struct { Path, SHA256 string } // a 16 kHz mono WAV, audio's window
type Text struct {
    Text       string
    Incomplete string // why the model stopped early, such as "max_output_tokens"
    Usage      Usage
}

type Describer interface { // vision
    Identity() Identity
    Describe(ctx context.Context, img Image) (Text, error)
}
type Transcriber interface {
    Identity() Identity
    Transcribe(ctx context.Context, w Window) (Text, error)
}
type Embedder interface {
    Identity() Identity
    Embed(ctx context.Context, texts []string) ([][]float32, Usage, error) // one vector per text
}
type Chunker interface {
    Settings() string // its version and bound, which key the held embeddings
    Split(text string) []chunk.Chunk
}

// RefusedError is an implementation's error for an input the endpoint refused: recorded, not
// fatal, and held in the cache when Status is 4xx. Any other error stops the job.
var ErrRefused = errors.New("infer: refused")
type RefusedError struct { Status int; Message string }
func (e *RefusedError) Is(target error) bool // matches ErrRefused
```

```go
package chunk // docinfer's chunk, kept
type Chunk struct { Text string; Start, End int } // the text's [Start, End) bytes
const Version = "1"
type Splitter struct { Bound int } // an infer.Chunker over Split
func Split(text string, bound int) []Chunk
```

The pipeline is built once and runs many jobs. Its bounds are shared by every job it runs at
once, so a replica bounds each capability as a whole ("Distribution": inference fans out inside
a job, bounded per capability).

```go
type Config struct {
    Vision        Describer   // nil skips images, counted
    Transcription Transcriber // nil skips windows, counted
    Embeddings    Embedder    // required
    Chunker       Chunker     // required
    Cache         Cache       // required
    Concurrency   struct{ Vision, Transcription, Embeddings int }
    EmbedBatch    int               // texts per Embed call
    BatchBytes    int               // a result batch's cap; 512 KiB by default
    Join          audio.JoinRequest // how a part's window transcripts join
}
func New(cfg Config) (*Pipeline, error)
// Profile is the resolved identity each result records: the models, settings, and chunker.
func (p *Pipeline) Profile() Profile
// Run infers over one job's sources and yields inference, batch, and done events as made.
func (p *Pipeline) Run(ctx context.Context, job string, sources iter.Seq2[Source, error]) iter.Seq2[event.Event, error]

type Source struct {
    Part   event.Part
    Stage  event.Stage
    Output event.Output // text or markdown: chunked; image: described; audio: transcribed, then joined
    Path   string       // its file, until Done
    Done   func()       // the pipeline is finished with Path
}
```

The sources are docinfer's (`infer/sources.go`): text and Markdown outputs, never records, plus
each image's vision text, each part's joined transcript, and one metadata text per input. `job`
feeds `Run` from dispatch's events. `cmd/docinfer` feeds it from a docproc run directory through
its `source` package and keeps its own directory's schema "1".

### Batches as made

docinfer runs each capability over every input in turn and writes `chunks.jsonl` whole at the
end (`infer/execute.go`, `outdir.Dir.WriteChunks`). `Pipeline.Run` streams per job instead, in
the order things are made:

- an `inference` event as each image's text or each part's joined transcript is made, or read
  from the cache;
- a `batch` event once its chunks' embeddings complete. A batch is yielded when the next chunk
  would pass `BatchBytes`, or when no embedded chunk is waiting, so it never waits to fill;
- a `done` event last, with the counts and digests Cerebro's manifest is built from.

The cap keeps a batch under NATS's default 1 MB `max_payload`: a 768-dimension vector is 4,096
B in base64 (data-intelligence.md, "Results").

### Event shapes

`event` holds the shapes quarry exports, as JSON, on the standard library alone; `infer`
aliases its `Identity`, `Usage`, and `Profile`. Its `Part`, `ContentType`, `Flag`, `Stage`,
and `Output` are docproc's `schema` shapes (`spike-ai-pipeline/internal/docproc/schema/part.go`,
`record.go`) moved into the library. `cmd/internal/schema` uses them and keeps record bytes
unchanged under schema "1". The shapes grow by addition only, as the tools' contracts do.

```go
package event
const Version = "1"
type Kind string // part, output, warning, inference, batch, checkpoint, done
type Event struct {
    Kind       Kind        `json:"kind"`
    Job        string      `json:"job"`
    Part       *Part       `json:"part,omitempty"`   // the part an event is of
    Stage      *Stage      `json:"stage,omitempty"`
    Output     *Output     `json:"output,omitempty"` // Ref, never a local path
    Outcome    string      `json:"outcome,omitzero"` // a part's: dispatch.Outcome
    Recorded   string      `json:"recorded,omitzero"`
    Reason     string      `json:"reason,omitzero"`
    Error      string      `json:"error,omitzero"`
    Outputs    *int        `json:"outputs,omitempty"`
    Children   *int        `json:"children,omitempty"`
    Warning    *Warning    `json:"warning,omitempty"`
    Inference  *Inference  `json:"inference,omitempty"`
    Batch      *Batch      `json:"batch,omitempty"`
    Checkpoint *Checkpoint `json:"checkpoint,omitempty"`
    Done       *Done       `json:"done,omitempty"`
}
type Part struct {
    Name string; Size int64; SHA256 string; ContentType ContentType; Flags []Flag
    Parent string; Depth int; Where string
}
// Stage, Output, ContentType, Flag, Span, Source, and Seam are docproc's and docinfer's shapes
// of those names, carried over; Counts is under "Changes per tool and per quarry package".
type Identity struct {
    Capability string `json:"capability"` // vision | transcription | embeddings
    Model      string `json:"model"`
    Settings   string `json:"settings"`   // prompt, max tokens, effort; prompt, language; chunker
}
type Profile struct {
    Vision        *Identity `json:"vision,omitempty"`
    Transcription *Identity `json:"transcription,omitempty"`
    Embeddings    Identity  `json:"embeddings"`
    Dimensions    int       `json:"dimensions"` // measured from the vectors, as docinfer's Space
    Chunker       string    `json:"chunker"`    // the Chunker's Settings
}
type Usage struct {
    Input  int `json:"input"`
    Output int `json:"output"`
}
type Warning struct {
    Tool  string `json:"tool"`
    Where string `json:"where"`
    What  string `json:"what"`
}
type Inference struct {
    Capability string   `json:"capability"`        // vision | transcription
    Content    string   `json:"content"`           // the image's or windows' SHA-256
    Outcome    string   `json:"outcome"`           // described, incomplete, refused, held
    Text       string   `json:"text,omitzero"`     // whole
    TextSHA256 string   `json:"text_sha256,omitzero"`
    Seams      []Seam   `json:"seams,omitempty"`   // a transcript's, as audio.Join made them
    Usage      Usage    `json:"usage,omitzero"`
    TookMS     int64    `json:"took_ms"`
}
type Batch struct {
    Index  int     `json:"index"`  // from 0 in the job; the batch's id is job and index
    Chunks []Chunk `json:"chunks"`
    Digest string  `json:"digest"` // SHA-256 over its chunks' digests, in order
}
type Chunk struct { // docinfer's outdir.Line, less the input's file fields
    Digest string `json:"digest"` // SHA-256 of Text: the per-item digest
    Text   string `json:"text"`
    Index  int    `json:"index"`  // within its source text
    Span   Span   `json:"span"`
    Source Source `json:"source"` // output, metadata, vision, or transcript, and its ref
    Part   Part   `json:"part"`   // provenance: the part, its parent and depth, where found
    Stage  *Stage  `json:"stage,omitempty"`
    Output *Output `json:"output,omitempty"`
    Vector string  `json:"vector,omitzero"` // base64 little-endian float32, as returned
    Error  string  `json:"error,omitzero"`  // why the endpoint rejected it
}
type Checkpoint struct { // a store's cursor
    After uint64 `json:"after"` // the last message whose events are out
}
type Done struct {
    Profile Profile `json:"profile"` // the resolved identity the consumer registers its space by
    Batches int     `json:"batches"`
    Chunks  int     `json:"chunks"`
    Digest  string  `json:"digest"`  // SHA-256 over the batches' digests, in order
    Counts  Counts  `json:"counts"`  // parts by outcome, and docinfer's counts
    Error   string  `json:"error,omitzero"`
}
```

Batch ids, per-item digests, provenance, warnings, and the profile are quarry's, as the common
envelope keeps them out of the tools. The manifest's sequence range is Cerebro's, from its job stream.

### Per-job cancellation

```go
package job
type Job struct { ID string; Input dispatch.Input }
type Config struct {
    Dispatcher      *dispatch.Dispatcher
    Pipeline        *infer.Pipeline
    Cache           infer.Cache
    CheckpointEvery int // messages between a store's checkpoints
}
func New(cfg Config) (*Runner, error)
// Run dispatches one job's input and infers over it, yielding its events in order: each part
// and output as dispatched, then inference and batches as made, and done last.
func (r *Runner) Run(ctx context.Context, j Job) iter.Seq2[event.Event, error]
```

A job is cancelled through `Run`'s context, which is the job's own. Its end stops the job's tool
runs (`tool.Run`'s SIGTERM, then SIGKILL), its index waits and claims, and its model calls,
since each interface method takes the context. It removes the job's files and yields
`ctx.Err()` alone, last. Other jobs on the same `Runner` go on. Stopping the loop early does the
same. processing's `Dispatch` already behaves this way per call. Two things change: docinfer's
single run-wide context becomes one per job, and `Pool` keeps one context for all its inputs,
since only docproc uses it.

## go-ai's surface

go-ai is built from spike-ai-pipeline-client v0.4.0, which the root's docinfer builds on. The
names below are the spike's (the module root is package `client`); go-ai's own names are
v1.ai.client's. Cerebro and `cmd/docinfer` call only what follows. The model gateway stays
transparent to go-ai: go-ai sees an OpenAI-compatible base URL, and Cerebro speaks Chat
Completions to it.

### The client

From `client.go`, `retry.go`, and `error.go`. docinfer builds one client per endpoint
(`internal/docinfer/config/config.go`, `Endpoint.Client`) over one `http.Client` with a cloned
default transport and no timeout (`app/infrastructure.go`).

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
  503 at capacity (data-intelligence.md, "What it asks of the goals beneath it"); the spike
  proves it by unit tests alone, and v1.ai.client proves it against the live gateway.
- **Timeouts and cancellation** are the context's: each call takes the job's context, so
  cancelling a job ends its requests (`stream.go` closes a stream's connection). go-ai sets no
  limit of its own, and concurrency bounds stay the caller's, `infer.Pipeline`'s.
- **The consumer key** is the bearer token `Token` returns. The gateway's keys carry the
  priority between bulk work and chat ("Cerebro and Athena").

### Generation: vision and extraction

From `client.go`, `stream.go`, and `protocol/`. docinfer's vision sends one user message of the
image, as a data URL, and the prompt, its reply capped and its reasoning effort none by default
(`internal/docinfer/vision/vision.go`, `describe`).

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
reasoning (docinfer's `text`). `StatusIncomplete` gives `infer.Text.Incomplete` its reason, such
as `max_output_tokens`; any status but completed and incomplete is an error.

Extraction streams: `GenerateStream`, folded by an `Accumulator`, whose `OutputTextDelta`s
Cerebro relays as progress ("Events" below). Its output text is the JSON the schema describes,
which Cerebro decodes; the client adds no decoding (`protocol/text.go`).

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
against the gateway's 429 and 503, and the adapter can reopen the window's path but can't read
the reply's Retry-After, which `APIError` doesn't carry. Open for v1.ai.client: a request that
can reopen its clip, such as `TranscriptionRequest.Open func() (io.ReadCloser, error)`, so the
client's one retry loop serves it.

### The adapters

go-ai's types meet quarry's `infer` interfaces in an adapter package of Cerebro's
(`internal/model`) and another of `cmd/docinfer`'s, each its own. quarry's library imports no
model client, and `cmd/docinfer` is the only quarry module that requires go-ai, so neither can
share the other's. Each is about docinfer's `vision`, `transcribe`, and `embed` packages less
their concurrency, which moves to `infer.Pipeline`.

```go
package model
type Vision struct {
    Client *client.Client; Model, Prompt string; MaxTokens int; Reasoning protocol.ReasoningEffort
}
func (v Vision) Identity() infer.Identity // {vision, Model, docinfer's visionSettings}
func (v Vision) Describe(ctx context.Context, img infer.Image) (infer.Text, error)

type Transcription struct { Client *client.Client; Model, Prompt, Language string }
func (t Transcription) Identity() infer.Identity // {transcription, Model, transcriptionSettings}
func (t Transcription) Transcribe(ctx context.Context, w infer.Window) (infer.Text, error)

type Embeddings struct { Client *client.Client; Model string }
func (e Embeddings) Identity() infer.Identity // {embeddings, Model}; the pipeline adds the chunker
func (e Embeddings) Embed(ctx context.Context, texts []string) ([][]float32, infer.Usage, error)
```

| go-ai | `infer` |
| --- | --- |
| `protocol.Usage{InputTokens, OutputTokens}`, `TranscriptionUsage` | `Usage{Input, Output}` |
| `EmbeddingUsage{PromptTokens}` | `Usage{Input}` |
| `Response.IncompleteDetails.Reason` | `Text.Incomplete` |
| `*APIError` of 408, 429, or 503, retries spent | the error, wrapped: it stops the job |
| any other `*APIError` | `*RefusedError{Status, Message}`: recorded, held when 4xx |
| a transport failure, a cancelled context | the error, wrapped: it stops the job |

docinfer counts every `*APIError` as a rejection and holds a 4xx (`infer/execute.go`, `verdict`),
which would hold a gateway's 429 as a verdict on the image. The adapter keeps the statuses
`RetryPolicy` retries out of `RefusedError`, so a job the gateway turned away fails, the work
queue redelivers it, and the cache replays what it finished. The settings strings are
docinfer's, so `cmd/docinfer`'s held results stay valid.

### Not Cerebro's surface

Not called by Cerebro or `cmd/docinfer`, so not part of this surface: function tools, tool
choice, `FunctionCall` and `FunctionCallOutput` items, the reasoning round trip (`Reasoning`
items, `encrypted_content` envelopes), `ReasoningParam.Summary`, verbosity, image detail,
`InputAudio` (Cerebro transcribes through `Transcribe`), the `Native` tiers, and the refusal
events. `format/messages` and `format/responses` serve `cmd/docinfer`'s endpoints that name
them; Cerebro uses `format/completions` alone. `Config.Query` is `cmd/docinfer`'s. go-ai's
`/models` listing, its `tools/infer` CLI, and its harness-session surface are outside both.

## Cerebro's API and events

Cerebro's shapes below are what the capstone's design tasks start from: `protocol` settles the
Queries and Commands and the upload protocol, `state` the streams and the fold, and `events` the
event protocol (data-intelligence.md, "Design"). Its nested `client` module, tagged `client/vX`,
holds these types and a Go client; Cerebro imports none of its consumers ("Cerebro and Athena",
"API").

### Queries and Commands

Go Elemental Queries and Commands. Every Command that changes a job appends one event to the
job's subject under an expected-last-sequence check, which is its transaction ("State").

```go
package client // capstone-cerebro/client
type Extraction struct {
    Name   string         // the schema's name, 1 to 64 letters, digits, _ and -
    Prompt string
    Schema jsontext.Value // a JSON Schema object
}

// Commands
type Submit struct { // a small document, whole: protocol
    Profile    string // a versioned processing profile in Cerebro's configuration, never a model
    Name, MIME string
    Content    []byte // at most Cerebro's configured small-document size
    Extraction *Extraction
}
type OpenUpload struct { Name, MIME string; Size int64; SHA256 string }
type AppendChunk struct { Upload string; Offset int64; SHA256 string; Bytes []byte }
type CompleteUpload struct { Upload, Profile string; Extraction *Extraction }
type AbortUpload struct { Upload string }
type CancelJob struct { Job string }

// Queries
type GetJob struct { Job string }       // -> Job
type GetUpload struct { Upload string } // -> Upload: the offset to resume from
type ListProfiles struct{}              // -> []Profile

type Upload struct { ID string; Size, Received int64; ChunkSize int; Expires time.Time } // Expires in UTC
type Profile struct { Name string; Resolved event.Profile } // quarry's resolved identity
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
func (c *Client) ListProfiles(ctx context.Context, q ListProfiles) ([]Profile, error)
```

- **Small documents** arrive whole in `Submit`; Cerebro passes the bytes to `dispatch.Input`'s
  `Reader`. A larger one goes through the upload: chunks wait in the object store, any replica
  takes any chunk, a chunk whose digest doesn't match is refused, and `GetUpload` gives the
  offset to resume from after a drop. `CompleteUpload` checks the whole file's digest and queues
  the job over the object store's reader. The upload's shape graduates to go-web-sdk
  (`chunked-upload-graduation`).
- **A job** is one input, the unit of retry ("Distribution"). Its ID is a UUIDv7 Cerebro
  assigns; `Submit` and `CompleteUpload` return it queued. `CancelJob` appends
  `cancel_requested`. The replica running the job watches its subject, so it folds the cancel
  and ends the job's context; an append racing the cancel fails its expected-sequence check, so
  only `cancelled` follows it (quarry's "Per-job cancellation").
- **The profile** resolves to an `infer.Pipeline`'s models, settings, and chunker, and to the
  extraction model. A job records the resolved `event.Profile`, so the consumer registers the
  matching embedding space ("Models and stages"; the `chunks` task). The `vision` task's prompt
  and sampling live in a profile.
- **Extraction** is optional, a caller's prompt and schema run by Cerebro on go-ai over each
  source text the pipeline chunks: each text and Markdown output, by its `Ref`, and each
  inference text. Its cache key is `infer.Key("extraction", model, settings, content)`, whose
  settings hold the prompt's and schema's SHA-256, its token bound, and its effort. Athena's
  summary chain maps onto it (`athena-mapping`).

### Streams and subjects

JetStream holds all of Cerebro's state, disposable; it owns no SQL ("State"). The stream and
bucket names are the `state` task's to fix.

```text
CEREBRO_WORK     cerebro.work                work-queue retention: one message per job, {"job"}
CEREBRO_JOBS     cerebro.job.<job>           every event of one job, on its own subject;
                                             limits retention, max age the object store's TTL
CEREBRO_UPLOADS  cerebro.upload.<upload>     an upload's opened, chunk, completed, aborted
object store     upload/<upload>/<offset>    staged chunks
                 cache/<infer.Key>           infer.Cache: held results, output/<sha256> texts
```

The job's "event stream" is a subject in one stream rather than a stream per job: the
expected-last-subject-sequence check guards one subject, a consumer filters `cerebro.job.>` or
one job's subject, and ~/docs' 592 jobs make no streams to reap. The browser never subscribes:
Cerebro's events are service-to-service, and Athena relays its own stream ("Browser").

### Events

Each event is a CloudEvents 1.0 event in binary mode, its attributes NATS headers
(`standards-lab/context/messaging.md`, "Decisions"): `type` names it, `source` is `cerebro`, `id` is a UUIDv7,
`subject` is the job's ID, and `Nats-Msg-Id`, the length-prefixed `source` followed by the `id` as go-messaging keys it,
deduplicates a retried append. Consumers
de-duplicate through go-messaging's inbox on `source` and `id`.

```text
type                                 data                                        whose
cerebro.job.queued                   {profile, name, mime, size, sha256, extraction}  Cerebro's
cerebro.job.started                  {attempt, replica}                          Cerebro's
cerebro.job.part                     event.Event, kind part                       quarry's
cerebro.job.output                   event.Event, kind output (Ref, no path)      quarry's
cerebro.job.warning                  event.Event, kind warning                    quarry's
cerebro.job.inference                event.Event, kind inference                  quarry's
cerebro.job.batch                    event.Event, kind batch, Index renumbered    quarry's
cerebro.job.checkpoint               event.Event, kind checkpoint                 quarry's
cerebro.job.extraction.progress      {source, delta}                              Cerebro's
cerebro.job.extracted                Extracted                                    Cerebro's
cerebro.job.cancel_requested         {}                                           Cerebro's
cerebro.job.completed                Manifest                                     Cerebro's
cerebro.job.failed                   {attempt, error}                             Cerebro's
cerebro.job.cancelled                {}                                           Cerebro's
```

- **quarry's shapes pass through.** `job.Runner.Run`'s events go out as they are made, their
  data quarry's `event.Event` JSON unchanged (event "1"), one CloudEvents type per `Kind`. Two
  things change on the way: `Batch.Index` is renumbered across attempts, so a batch's id, job
  and index, stays unique, and quarry's `done` becomes no event of its own: its `Done` goes into
  the manifest.
- **Cerebro's own** are the job's lifecycle, extraction, and the manifest. The job's state is
  the fold of its subject: queued, then running from `started`, then one of completed, failed,
  or cancelled. A `failed` the work queue will redeliver is followed by another `started`.
- **Extraction progress** relays the stream's `OutputTextDelta`s, coalesced to at most one event
  a second per source text, each carrying the text since the last. `extracted` carries the
  result:

```go
type Extracted struct {
    Source  event.Source   `json:"source"`         // the text extracted from: an output's Ref, or an inference's
    Content string         `json:"content"`        // the text's SHA-256
    Name    string         `json:"name"`           // the extraction's
    Outcome string         `json:"outcome"`        // extracted, incomplete, refused, held
    Value   jsontext.Value `json:"value,omitzero"` // the JSON the schema describes
    Error   string         `json:"error,omitzero"`
    Usage   event.Usage    `json:"usage,omitzero"`
    TookMS  int64          `json:"took_ms"`
}
```

- **Size.** Every event fits NATS's 1 MB `max_payload`: a batch closes at 512 KiB, an output
  carries its `Ref`, never its text. An `inference` event's whole text, and an extraction's
  value, may not; moving such a text to the object store by its digest is the `events` task's
  (Assumptions).

### Results and the manifest

A result batch reaches the consumer as a `cerebro.job.batch` event: quarry's `event.Batch`, its
chunks' text, provenance, and base64 vectors. The consumer commits each batch in one
transaction, de-duplicated through its inbox (data-intelligence.md's evidence 7). The job ends with `completed`, whose
data is the manifest:

```go
type Manifest struct {
    Profile  event.Profile // the resolved identity the consumer registers its space by
    First    uint64        // the job's first event's sequence: 1
    Last     uint64        // this event's sequence
    Batches  int           // batch events in [First, Last]
    Chunks   int
    Digest   string        // SHA-256 over those batches' digests, in sequence order
    Counts   event.Counts  // quarry's Done.Counts, the last attempt's
    Attempts int
}
```

The sequence range is Cerebro's own ("Event shapes" above). Each event carries the
job's sequence, from 1 with no gap, in the CloudEvents `sequence` extension as a decimal string:
the expected-last-subject-sequence check lets one appender write the subject at a time, so the
job's sequence is contiguous whatever the stream's global sequence interleaves. The consumer
checks completeness by holding every sequence from `First` to `Last` and by recomputing
`Batches`, `Chunks`, and `Digest` over the batches it committed. Since a redelivered job's
attempt replays content seen before, and a store resumes from its last checkpoint, the range and
digest cover every attempt's batches, and the manifest's digest is Cerebro's, not quarry's
`Done.Digest`, which covers one attempt.

### Open

- **Live sources.** `media.Streamer` reads a URL source (srt, rtmp, http, udp) as it arrives,
  but quarry's `job.Job` takes a `dispatch.Input`, a path or a spooled reader. Whether a live
  source is a job, and through which Command, is the `protocol` task's.
- **The small-document size**, `Submit`'s limit and the upload's threshold, is a setting whose
  default the `protocol` task picks.
- **Transcription retry** needs go-ai's change above (v1.ai.client).

## Changes per tool and per quarry package

What each promotion changes from its spike, derived from the sections above; the task that
builds it is in brackets. Every tool moves to schema "2" and the common envelope, and each
re-records its goldens under "2" as one `.jsonl` and one `.stderr` per fixture, compared byte for
byte, which tests its determinism.

### quarry-ooxml [ooxml]

From spike-ai-pipeline-office 0.1.0.

- **Toolchain.** .NET 10 to .NET 11 (`Directory.Build.props`' `net10.0`, mise's `dotnet`
  10.0.400), and System.CommandLine 2.0.14 to 3.0.0, first. The publish's
  `-p:NuGetAudit=false`, which the ILCompiler 10.0.13 advisory forces under .NET 10, goes with
  the move. DocumentFormat.OpenXml 3.5.1 stays.
- **One run.** `--format json|markdown` goes: one compact `document` line holds the Markdown and
  the model, where office writes one rendering, its JSON indented (indentation is 62% of its
  output, measured above).
- **Cut.** The block tree (headings, paragraphs, lists, docx tables, their inlines), sections,
  headers and footers, notes, comments, chart blocks, and picture `alt` and `title`; their text is
  the Markdown's, unchanged.
- **Kept.** `sheets`, as office's `TableCell` less its docx-only fields; the Markdown renderer.
- **Added.** `properties` (core, app, custom, typed as stored); `media` as one list in document
  order, each with its placement (section, sheet, or slide); `embedded` packages and `ole`
  payloads with `prog_id` written as stored, which office leaves unextracted.
- **Envelope.** `line` and `schema` "2"; snake_case names (office's are camelCase); media names
  keep the stored extension only when it is 1 to 8 ASCII letters and digits, where `MediaStore`
  keeps `Path.GetExtension` whole.
- **Goldens.** `test/…/Expected/*.json` and `*.md` become one `.jsonl` and `.stderr` per fixture.

### quarry-mail [mail]

From spike-ai-pipeline-mail 0.3.1.

- **Toolchain.** The fixture generator's MimeKit 4.18.2 to 5.0.0, its fixture truth unchanged.
  The Rust crates stay.
- **Cut.** `body` (`text`, `html`, `rtf`, `derived.html`); folder and store `display_name`. RTF
  and HTML are still decoded, since the Markdown is made from them.
- **Moved.** `body.derived.markdown` up to the message's `markdown`.
- **Added.** `smtp_address` on an Exchange (`EX`) address, from `PidTagSmtpAddress`.
- **Flags.** `--attachments <dir>` becomes `--media <dir>`; `--after <id>` keeps its resume.
- **Envelope.** `schema` "2". The Markdown links an inline image by its attachment's `file`, the
  envelope's media rule, where mail's keeps the `cid:` source as written.
- **Goldens.** `tests/golden/{msg,pst,generated}/` re-recorded under "2".

### quarry-onenote [onenote]

From spike-ai-pipeline-onenote 0.1.0.

- **Toolchain.** typed-path held at 0.12.4; it moves to 0.13.0 only with onenote_parser.
- **Cut.** A page's `content` (containers, outlines, paragraphs, runs, tables, tags), its `ink`
  record, and `ink_text`; image `alt_text`, `ocr_text`, `hyperlink`, `printout_page`; ink
  `strokes` and `bounds`; section and section group `display_name`.
- **Added.** A page's `media`, flattened: each image, embedded file, and ink SVG that `content`
  and `ink` held, in the order processing's `onenote/media.go` walks them today, which moves into
  the tool. An embedded file's `file_type` becomes `type`.
- **Envelope.** `schema` "2"; a page's `creation_time` and `modification_time` become optional,
  left out when absent.
- **Goldens.** Added: it commits none today. Each fixture gains a `.jsonl` and `.stderr`; the
  JSON-pointer expectations in `tests/fixtures/*.json` stay as the readable checks.

### quarry's packages [library]

From spike-ai-pipeline-processing v0.9.0, as `github.com/standards-lab/quarry`, requiring
`golang.org/x/text` alone.

```text
route, inspect      kept
tool                kept
internal/cli        kept: exit codes 1 to 4, the stderr parse, Spool
document, picture,  kept
  audio, media,
  sevenzip
office              changed: decodes the schema "2" document line; Request.Format and the block
                    tree's decoder go, since one run returns the Markdown, properties, media, and
                    sheets
email               changed: Message.Markdown up, Address.SMTPAddress; Body, Conversation.HTML,
                    and Folder.DisplayName go; Request.Attachments becomes Media
onenote             changed: Page.Markdown and Page.Media decode the tool's lists; content.go's
                    decoder and media.go's walk go
dispatch            changed: Input.Reader, spooled; Item.Warning, typed, in place of a warnings
                    record output; office's stage makes embedded and OLE media child parts;
                    Pool keeps one context for its inputs
stages              moved from the root's internal/docproc/stages, less WriteTable
chunk               moved from the root's internal/docinfer/chunk, unchanged
infer               new: the interfaces, Cache, Key, and Pipeline, from docinfer's infer, vision,
                    transcribe, and embed less their client calls, and outdir's Key and Hold
event               new: docproc's schema shapes, docinfer's Seam, Span, Source, Line as Chunk,
                    and the Counts below
job                 new: dispatch, then inference, under one job's context
internal/goldentest, kept: test helpers that call no model
  mediatest, textscore,
  corpustest
internal/visiontest, moved to cmd/docinfer [docinfer], with the integration and findings tests
  audiotest          that call a model, since the library's module requires no client
```

`onenote.Medium.Where` names a medium's outline path today, from the walk; the contract's media
carry no placement, so a medium's `Where` names its page and its index in `media`. An optional
placement field returns by addition if the `design` task asks for one.

`event.Counts` carries docinfer's `outdir.Counts` per job, less `Lines`, which `Done.Chunks`
counts (an `event.Chunk` is one chunk in one place), with each `Usage` less `Total`, and with the
job's parts by outcome:

```go
type Counts struct {
    Parts         map[string]int       `json:"parts"`    // by dispatch.Outcome, every depth
    Outputs       int                  `json:"outputs"`  // output events
    Texts         int                  `json:"texts"`    // text and Markdown outputs chunked
    Metadata      int                  `json:"metadata"` // metadata texts, one per input
    Chunks        int                  `json:"chunks"`   // distinct chunks, by content
    Held          int                  `json:"held"`     // distinct chunks the cache held
    Embedded      int                  `json:"embedded"`
    Rejected      int                  `json:"rejected"` // this job or held
    Requests      int                  `json:"requests"` // embeddings requests sent
    Images        int                  `json:"images"`   // image outputs, vision run or not
    Vision        *VisionCounts        `json:"vision,omitempty"` // nil when skipped
    Windows       int                  `json:"windows"`
    Transcription *TranscriptionCounts `json:"transcription,omitempty"`
    Tokens        Tokens               `json:"tokens"`   // this job's requests only
}
type VisionCounts struct {
    Images, Held, Described, Incomplete, Empty, Refused, Requests, Texts int
}
type TranscriptionCounts struct {
    Distinct, Held, Transcribed, Failed, Requests, Transcripts int
    SeamsMatched, SeamsUnmatched                               int
}
type Tokens struct { Vision, Transcription *Usage; Embeddings Usage }
```

The JSON names are docinfer's, snake_case (`seams_matched`).

### The command modules

From the spike root's `cmd/` and `internal/`; the third column names the spike packages each
module is built from.

```text
cmd/docproc    [docproc]   cmd/docproc, internal/docproc/{app, apptest, run, report}; requires
                           go-cli-sdk and go-core; wires the library's stages, keeps
                           WriteTable; its report is the reports task's [reports]
cmd/docinfer   [docinfer]  cmd/docinfer, internal/docinfer/{app, apptest, config, models,
                           outdir, source, report}; requires go-ai, the only module that does;
                           its go-ai adapter replaces vision, transcribe, and embed; infer keeps
                           its commands and feeds Pipeline.Run from source; outdir implements
                           infer.Cache over results/; its directory stays schema "1"
cmd/internal   [docproc]   schema and rundir, importable by cmd/ alone; schema's shapes alias
                           event's, its record bytes unchanged under schema "1"
```

### Asked of go-ai

Requests to v1.ai.client, which `docinfer` and the capstone build on:

- A transcription request that can reopen its clip, such as `TranscriptionRequest.Open func()
  (io.ReadCloser, error)`, so `Transcribe` joins the one retry loop and honors Retry-After; or,
  failing that, the Retry-After an `*APIError` carried.
- `RetryPolicy`'s retry of 408, 429, and 503 proven against the live gateway, not by unit tests
  alone.
- The surface above kept: `Generate`, `GenerateStream` with `Accumulator`, `Embed`,
  `Transcribe`, `*APIError`'s `Status` and `Message`, the usage fields the adapters map, and
  `Config.Query` and the three formats for `cmd/docinfer`.
- A consumer key in a header other than the bearer token, only if the gateway asks for one.
