# docproc's API

The library module, `github.com/standards-lab/docproc`, extracts: it inspects and routes each part
of an input, reads it through its stage and the tools, stores each output's content, and records
what it prepared in its index. It requires `golang.org/x/text` only and depends on no model concept:
no model client, no inference, no embedding. Inference is docinfer's
(`github.com/standards-lab/docinfer`), and the imports run one way: docinfer imports docproc, and no
docproc package imports docinfer. The base module is built (`library`): each package's doc comment
is the reference for its exported names, and the signatures below are the boundary other layers
build against. The providers are built (`providers`), each module's README and package doc their
reference; `rundir` and `cmd/docproc` are built (`docproc`), `command.md` and each
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

## Inspection and routing

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

## Running the tools

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

## Input from a stream, or a path

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
storage's reader of the job's staged input and its SHA-256 ([`cerebro.md`](../context/cerebro.md),
"Queries and Commands").

An input with `After` set is a resumed store: it is dispatched past that message, and its entry
is never recorded, since it would hold only the messages past `After`.

## Dispatch

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

## Content

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
are the `state` task's ([`cerebro.md`](../context/cerebro.md), "State and subjects").

A content missing within that bound, deleted by hand or swept early under clock skew, is outside
the contract. docproc can't see one: a replay opens no content, its outputs carrying only their
SHA-256. docinfer finds it when it opens the content, on a cache miss or to chunk its text, and
its host acts on it (docinfer's `job` package doc).

`cmd/docproc`'s run directory holds its content in a `FileContent`; Cerebro's is blob storage
through `docproc/storage`, which maps go-storage's `ErrTooLarge` (an object past the store's
`MaxObjectSize`) to `dispatch.ErrTooLarge`, so the part fails recorded ("Dispatch").

## Index

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

## Event shapes

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
data types ([`cerebro.md`](../context/cerebro.md), "Events"). The spike's docproc `schema` shapes
(`spike-ai-pipeline/internal/docproc/schema/part.go`, `record.go`) move into `dispatch` and
`inspect`, and `rundir/schema` aliases them ([`changes.md`](changes.md), "The nested modules"); the
spike's docinfer `outdir` shapes move into docinfer. The shapes grow by addition only, as the tools'
contracts do.

