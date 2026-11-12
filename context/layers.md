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

`Item.Warning` is new. processing yields a tool's stderr warnings as a record output, but slice
1's stderr line (`<tool>: warning: <where>: <what>`) parses to typed data, and warnings as data
are quarry's.

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
// Stage, Output, ContentType, Flag, Span, Source, Seam, and Counts are docproc's and docinfer's
// shapes of those names, carried over.
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
type Usage struct { Input, Output int }
type Warning struct { Tool, Where, What string }
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
    Stage  *Stage `json:"stage,omitempty"`
    Output *Output `json:"output,omitempty"`
    Vector string `json:"vector,omitzero"` // base64 little-endian float32, as returned
    Error  string `json:"error,omitzero"`  // why the endpoint rejected it
}
type Checkpoint struct { After uint64 } // a store's cursor: the last message whose events are out
type Done struct {
    Profile Profile `json:"profile"` // the resolved identity the consumer registers its space by
    Batches int     `json:"batches"`
    Chunks  int     `json:"chunks"`
    Digest  string  `json:"digest"`  // SHA-256 over the batches' digests, in order
    Counts  Counts  `json:"counts"`  // docinfer's VisionCounts and TranscriptionCounts, and parts
    Error   string  `json:"error,omitzero"`
}
```

Batch ids, per-item digests, provenance, warnings, and the profile are quarry's, as slice 1
keeps them out of the tools. The manifest's sequence range is Cerebro's, from its job stream.

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

Filled by slice 3.

## Cerebro's API and events

Filled by slice 3.

## Changes per tool and per quarry package

Filled by slice 4.
