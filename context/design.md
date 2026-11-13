# design

This note holds the design review of the five artifacts v1.ai.processing promotes: each
finding's settled design and disposition, with the evidence that justified it, since the
evaluations themselves are not kept. Fable 5.2 evaluated each artifact, the architect settled its
findings in one review round per artifact, and a sixth round gave the shapes and vocabularies the
artifacts share one home each ("Structural single homes"). `layers.md` holds the boundaries and
the change lists. An entry names the task that carries it and the `layers.md` section that states
it, and doesn't restate what that section holds; an entry a later one changed points to it. The
note opens with how the review changes the goal's tasks and closes with each artifact's first tag
and the task that releases it.

## How the review changes the goal's tasks

The settled designs grow every build task and move work between them. The changes are pending
edits in the goal record, applied to the roadmap at SHIP.

- **onenote** gains the file-only option and the table-of-contents refusal, the encryption
  signal, each drawing's ink alt text, an exact onenote_parser pin with owned workarounds,
  MPL-2.0 allowed by name, release overflow checks, and third-party notices.
- **Every tool** gains the envelope additions: `--version`'s `schema <n>`, the warning escape, and
  error reasons that never lead with the input path.
- **library** adds the Go job and the Go module releases to ooxml's CI, classifies failures once,
  replays a miss from the nearest ancestor with a file, sets no default bounds, records a table
  of contents' refusal and a death by signal, and corrects the signed-PDF cause; unrouted
  formats, HTML, and ClamAV stay backlog.
- **docproc** tags the base module v0.2.0 when `rundir` lands, with a committed `go.work` between
  tags (DP-3), makes `rundir` one handle, and has its README name `stages` as the shipped wiring.
- **reports** writes a page per input content and tags `cmd/docproc/v0.1.0`.
- **acceptance** gains measurements over ~/docs: each tool's peak memory, the word error rate of
  its audio before the tag, and the inspection evaluation's bombs through `cmd/docproc`'s
  starting bounds.
- **The docinfer goal** counts a job's outputs by part ID, owns `outdir`'s atomic write, gains
  the `join` package from docproc's `audio` and the one metadata-text function, reads `dispatch`'s
  Meta keys, and states that its "1" includes `dispatch`'s "1" (SH-1, SH-2, SH-8, SH-9).
- **The goal's sync** takes the architecture-page edits: the `tools/` module zip, a nested
  command's layout, the rule for external repositories, and CI's gate job.

## docproc-ooxml

From spike-ai-pipeline-office v0.1.0, built in `tools/ooxml/`, whose README and CHANGELOG state
what each accepted finding changed. Probes ran on the spike's published binary (0.1.0+6ed0d0e,
linux-x64). Every finding was accepted; the entries below keep the evidence and the rejected
alternatives the code can't state, and what the build leaves to later tasks is in `layers.md`,
"docproc-ooxml [ooxml]".

- **OX-1 · A sheet's charts and pictures had no line for their Markdown.** The renderer writes a
  sheet's chart tables and picture links after its cell table, and a chart sheet has only those,
  so no line held them. They ride on a closing `rows` line of that sheet. Rejected: a new line
  kind for a sheet's drawing.
- **OX-2 · The tool held the whole workbook in memory.** One sheet of 500,000 rows by 10 cells
  (17 MB on disk) took 25.5 s and 8,201 MiB peak; a killed tool exits 137, none of the contract's
  codes, and `rows` lines bound only the reader. Each worksheet part is read in two forward-only
  passes; docx and pptx keep the whole model, their `document` line being whole by contract.
  Acceptance measures peak memory over ~/docs. Rejected: deferring with a stated ceiling.
- **OX-4 · stderr labels and exit codes differed from the envelope.** library drops the `Labels`
  list from `internal/cli` (`layers.md`, "docproc's packages [library]").
- **OX-5 · Embedded and OLE media were a larger change than "Added" read.** `ole` media is the
  part as stored; unwrapping its compound file, which the Open XML SDK can't read, is docproc's,
  whose inspection already recognises one. A chart's embedded source workbook is never media:
  nine fixtures hold one, and as media its data would be extracted twice. Rejected: the tool
  extracting the native payload.
- **OX-9 · A published advisory failed the check without a code change.** The v0.1.0 tag stopped
  building when ILCompiler 10.0.13 got an advisory, so NuGet's audit runs in `currency`, never in
  a build.
- **OX-10 · Nesting under `tools/ooxml`.** CI splits the root check into a tool-by-runner matrix
  on native x64 and arm64 runners, since a NativeAOT publish can't cross-link, and a Go job, which
  library adds with the Go module releases. Rejected: a Go-only root check; library building the
  CI.
- **OX-11 · Hyperlink targets unread; unread content left no trace.** Each unread kind a document holds gets one warning;
  reading those kinds stays backlog.
- **OX-12 · The baseline harness isn't promoted.** `bench/`, its tests, `baseline.sh`, and
  `docs/findings/` stay in the spike as its evidence, and Markdig with them; the acceptance rerun
  over ~/docs is the measurement from then on.
- **OX-20 · The binary needs glibc.** The build links libc and libm dynamically, where mail and
  onenote are static musl; the release refuses a binary above the floor its README states. A
  static musl publish is the capstone's `tool-packaging` call.

## docproc-mail

From spike-ai-pipeline-mail v0.3.1, built in `tools/mail/`, whose README and CHANGELOG state what
each accepted finding changed. Probes ran on the spike's committed musl release binary (0.3.1,
linux-x64) over inputs built in a scratch directory; an entry says where a finding was found by
reading. Every finding was accepted, ML-3, ML-6, ML-11, and ML-12 in the form the round settled;
the entries below keep the evidence and the rejected alternatives the code can't state, and what
the build leaves to later tasks is in `layers.md`, "docproc-mail [mail]".

- **ML-1 · One message's HTML body ended the whole run by signal.** htmd 0.5.6 walks the DOM
  recursively and html5ever caps no depth: an `.eml` whose HTML nests 4,000 `<div>` exited 0,
  and 8,000 (88,184 bytes) to 20,000 exited 139 (SIGSEGV) with nothing on stdout or stderr, as
  did 100,000 unclosed `<font>` tags; a three-message mbox with the deep message second wrote 0
  lines, the first message's still in the buffer. Time was quadratic before the crash, 1.2 s at
  20,000 levels and over 100 s at 200,000. The cap of 512 open elements is Blink's.
- **ML-2 · `smtp_address` from `PidTagSmtpAddress` never reached a sender.** 0x39FE is a
  recipient-row property: a scan of the committed `.msg` fixtures found it only in recipient
  storages (18 of 18 in POI's `outlook_30_msg.msg`), so the sender's is read from
  `PidTagSenderSmtpAddress` (0x5D01). Of the committed fixtures only Tika's
  `testPST_variousBodyTypes.pst` holds 0x5D01, so a unit test over a built `.msg` shows it too.
  docproc's fallback to `transport_headers`' From is in "docproc's packages [library]".
- **ML-3 · The content rule's Unicode 16.0 couldn't be both schema "2" and current.** 16.0 was
  the measuring tool's (uniseg 0.10.1, a Go library), while current Rust crates carry 18.0
  (finl_unicode 1.5.1, unicode-ident 1.0.27). Schema "2"'s rule takes the Unicode version its
  pinned crates carry, so a currency bump that moves it is a reviewed change (`layers.md`,
  "docproc-mail"). Rejected: the shared crate owning tables generated from UCD 16.0.
- **ML-6 · Linking inline images by `file` was more than a rename.** Run over a built `.eml`: the
  Markdown kept `cid:One%2Bimg@example.com` beside a `content_id` of `One+img@example.com`; an
  image referenced by Content-Location stayed `![b](pic2.png)`; an unmatched `cid:` stayed as
  written; a 300 KB `data:` image made a 400,038-byte Markdown with no media file. The spike's
  `inline` was `ATT_MHTML_REF` in MAPI and Content-Disposition in MIME, so one message differed
  as `.msg` and `.eml`. Rejected: each format keeping its own meaning of `inline`.
- **ML-9 · The shared crate holds the envelope both Rust tools duplicated.** The two spikes'
  `jsonl.rs` were identical byte for byte; their guard, fault, panic hook, warning, and argument
  loop differed in names; the media name rule and the escape were written twice, and the copies
  had drifted. onenote adopts the crate ("docproc-onenote [onenote]"). Rejected: a crate holding
  the content rule alone.
- **ML-10 · codepage-strings goes.** A 2021 wrapper over `codepage` and encoding_rs, both already
  direct dependencies, it alone brought 11 of the lock's 92 crates, and its BOM-sniffing decode
  read a PT_STRING8 that starts FF FE as UTF-16 whatever its code page. Its US-ASCII (20127)
  workaround stays as the tool's own code: `codepage` doesn't know 20127, and POI's
  `example_sent_regular.msg` uses it.
- **ML-11 · The build, release, and CI files assumed the crate was the repository.** Neither the
  spike nor docproc had a ruleset, so "CI checks gate the merge" was habit, and a required
  per-tool check that a path filter skips would block every other pull request; docproc's
  always-running `ci` job, which its ruleset requires, settles it. Rejected: one release
  workflow per tool.
- **ML-12 · MimeKit 5.0.0 would sit under two packages built for 4.x.** The generator's direct
  MimeKit reference lifts what MsgKit 3.0.6 (>= 4.18.1) and PstBuilder.Eml 1.3.0 (>= 4.17.0)
  resolve to, past a Pkcs advisory; both were compiled against MimeKit 4, so a major version
  under them can fail at run time. MimeKit is held at 4.18.2 until MsgKit or PstBuilder.Eml
  releases on MimeKit 5. Rejected: MimeKit 5.0.0, which reverses the plan's decision.
- **ML-15 · Named properties and multi-valued values were read for no field.** Named properties
  return with the first field that reads one.

## docproc-onenote

From spike-ai-pipeline-onenote v0.1.0. Evidence was read from the spike at `866ffc6` and from
onenote_parser 2.1.0's source; probes ran on the spike's v0.1.0 musl release binary over its
committed fixtures, and an entry says where a finding was found by reading. Every finding was
accepted, ON-2 and ON-4 in the form round 3 settled; the changes are in `layers.md`,
"docproc-onenote [onenote]" unless an entry says otherwise. Two rulings close the section: the
parser's MPL-2.0 license, and the rule that nothing is asked of the parser upstream.

- **ON-1 · Recognised ink words reached the Markdown only through the page's ink image.**
  Accepted → onenote. The spike joined every recognised word of the page and gave them to the
  page's own ink drawing alone; a paragraph's handwriting and an ink block were written
  `![ink](file.svg)` with no words, and the page's ink exists only when its ink containers hold
  points, so a page whose handwriting sits in outlines had its words in `ink_text` alone, which
  schema "2" cuts. The one ink fixture, `handwriting_recognition.one`, has page ink only, so no
  test showed it. The parser links each stroke to its word (`InkStroke::recognized_word()`), so
  each drawing's alt text is the words its own strokes belong to, in the recognition's order,
  `![ink: <words>](file.svg)` for page ink, paragraph ink, and ink blocks alike; a recognised
  word no drawing carries is one plain line after the page's content, and
  `ink_alternatives[].word` stays the page-wide index (`layers.md`, "docproc-onenote", now "the
  ink images' alt texts"). Unit tests over built pages show paragraph ink alone and both kinds.
- **ON-2 · A table of contents through docproc read empty or twice, and a directory never
  arrived.** Settled → onenote, with the stage's outcome in "docproc's packages [library]". The
  tool read a `.onetoc2` as its whole notebook, resolving sections beside it. Spooled from a
  stream, as Cerebro passes every input, nothing is beside it, and the parser drops a missing
  entry without a word: one `notebook` line, exit 0, no warning. Given by path, as `cmd/docproc`
  walks a share, every section was read, and each sibling `.one` is also its own input, so every
  page came out twice under two parts. The header can't tell a section from a table of contents,
  so the route couldn't avoid the run, and a notebook directory isn't a file, so it never
  reaches docproc. By reading, a directory read also leaves out any `.one` its table of contents
  doesn't list (`interop/SUT`'s `FileSyncWOPI_OneNoteWithODB.one`). docproc always passes
  `--file-only`, under which the tool refuses a table of contents before reading any section,
  and docproc records that refusal as the part's outcome, not a failure: the outcome
  `read elsewhere`, mapped in `internal/cli` (SH-5). Notebook and directory reads stay for an
  operator (`layers.md`, "docproc-onenote"). Rejected: the tool reading notebooks for docproc.
- **ON-3 · The expectations `layers.md` kept all pointed into the fields schema "2" cuts.**
  Accepted → onenote. Every pointer in `tests/fixtures/content.json` starts `/content`, `/ink`,
  or `/ink_text`; `sections.json` counts each page's containers by kind; `tests/content.rs`,
  `tests/media.rs`, and `tests/ink.rs` walk the same tree. They are the tool's only hand truth,
  and a golden records what the tool wrote, not what is right. A refusal's stderr leads with the
  input path as given, and no `.onepkg` is committed. The truth is re-seated: `content.json`
  points into `media` and `ink_alternatives`; `sections.json` keeps ids, levels, titles, times,
  and refusals; the run, list, table, and tag truth moves to hand-written Markdown expectations
  and the unit tests `src/content.rs` already has. Goldens run in the fixtures' directory with
  relative inputs, and a package's read equals its directory's golden in every line but the
  first.
- **ON-4 · An encryption signal is feasible for desktop-format sections.** Settled → onenote, with
  the mapping in "docproc's packages [library]". `Encryption.one` was refused with the parser's
  `Unexpected end of file`, and skipped inside a notebook with the same reason, so docproc could
  only call it failed. [MS-ONESTORE] 2.5.19's encryption-key structure opens with the fixed bytes
  `0xFB6BA385DAD1A067` and closes with `0x2649294F8E198B3C`; `Encryption.one` holds each three
  times (first at offsets 5608 and 6687), and none of the other 22 committed OneStore files, nor
  the 285 MB of large sections the spike measured, holds either. Only after a section's parse fails
  does the tool scan for the two constants; found, it refuses with a reason beginning `encrypted`,
  and a `skipped` line gains an optional `encrypted: true`, an addition under schema "2". A false
  match relabels a read that already failed, never costing content. docproc maps the reason to
  `ErrEncrypted` by the one prefix mapping SH-5 puts in `internal/cli`. The scan is the tool's own
  tested code; the OneDrive packaging format, with no encrypted fixture, stays unestablished, and
  decryption keeps waiting on a specification. Rejected: leaving encrypted sections reported
  failed.
- **ON-5 · The parser hid absent times and titles.** Accepted → onenote. By reading:
  `created_time()` is never absent, an unreadable FILETIME becoming `1970-01-01T00:00:00Z` with a
  parser warning; `updated_time()` returns the creation time when no modification time is
  stored, behind a private option; and an untitled page's title is the first outline's first
  paragraph. `creation_time` is left out when it is the parser's 1970 fallback, with a warning;
  `layers.md` and the README state that a page with no stored modification time reports its
  creation time, and that an untitled page's `title` is its first line, as OneNote's page list
  shows it.
- **ON-6 · The binary the tests proved was not the binary that ships.** Accepted → onenote,
  settled with mail's release-mode check, which onenote's change list carries; the spike's test
  `a_parser_panic_is_refused_with_exit_3` relied on debug overflow checks a release build lacks.
  The media unit test that wrote under the system temporary directory moves under
  `CARGO_TARGET_TMPDIR`, so the check writes only `target/`.
- **ON-7 · Memory was unbounded in four places, one of them the tool's own.** Accepted →
  onenote, with the run's bound in "docproc's packages [library]". The tool read each image and
  embedded file whole before hashing and writing it, though the parser hands back a streaming
  reader, and `fs::write` to the hash name left a truncated file under the full content's name
  when a run was killed mid-write. The parser holds a notebook whole before its first line and
  decompresses a `.onepkg` whole, beside issue #48, which alone the README named. Media are
  streamed, hashed while copied through a fixed buffer into a temporary file in the media
  directory and renamed to the hash name, or hashed only without `--media`; the README states
  the parser's memory. A bound on the whole run is docproc's, in `tool`; an allocation failure
  under it aborts the tool by signal, which docproc reports as failed (ON-16).
- **ON-8 · Some Markdown the tool wrote didn't parse as what it meant.** Accepted → onenote, mail
  taking it through the shared crate. By reading: a destination escaped parentheses only, where
  htmd wraps one holding a space in `<…>`, so a link to `\\server\Team Notes\plan.docx` was text,
  not a link, and no URL reached the content rule; in a table cell, a code span and an
  equation's LaTeX left `|`, which GFM reads as a cell boundary even in a code span. One
  destination writer goes in the shared crate, and a table cell's code spans and LaTeX escape `|`
  (`layers.md`, "docproc-onenote [onenote]", Destinations).
- **ON-9 · Five workarounds stood on onenote_parser's behaviour, under a caret requirement.**
  Accepted → onenote. The parser is current and maintained, by one person; the tool works around
  (a) its private error kind, told by message prefix, (b) a `println!` that costs `libc`, an
  `unsafe` `dup2`, and `stdout.rs`, (c) a silently dropped recycle bin, mirrored through `cab`
  and the parser's private name normalisation, (d) a failed section inside a group vanishing,
  and (e) one section's panic refusing its whole notebook. (a) and (c) depend on 2.1.0's exact
  text and logic, under `"2.1.0"`. onenote_parser is pinned `=2.1.0` with the comment mail's
  pins carry; (a) to (c) stay as the tool's own tested code, (d) and (e) join the README's
  limits, `cab` stays a direct dependency, and typed-path stays held.
- **ON-10 · After the cut, the model is the renderer's private input.** Accepted → onenote. Once
  `content`, `ink`, and `ink_text` aren't written, nothing reads a run's underline, superscript,
  subscript, highlight, and font colour, a list's bullet, prefix, suffix, and format, a tag's
  completed time and its shape's `Debug` text, a word's language, or an image's printout page;
  and the reader already visits media in the order processing's `Page.Media` walks them. The
  page content becomes a crate-private intermediate with no `Serialize`, less those fields, a
  tag carrying `task: bool`; the reader appends a `media` entry at each of its three store sites
  (image, embedded file, drawing), so processing's walk isn't ported, and the README says the
  Markdown's order differs from `media`'s by design.
- **ON-11 · The shared crate holds the envelope both Rust tools implement.** Accepted → onenote,
  which adopts docproc-envelope (ML-9). The spikes' `jsonl.rs` was byte-identical in both, and
  the media name rule and once-only writer, the faults, and the escape parallel, with one drift:
  mail created the media directory once the input was known to be read, onenote at its first
  file, so `--media` over `interop/SUT/OneWithoutFileData.one` left no directory. The directory
  is created once the input is read. onenote compares two
  plain texts (`ocr_text` with `alt_text`), so the content rule's entry point takes plain token
  sequences, and the Markdown-to-plain reader sits behind a Cargo feature only mail enables, so
  onenote links no CommonMark parser. Each tool has its own lock, so the crate requires its
  Unicode crates exactly; the version they carry is round 2's (ML-3), not the 16.0 this finding
  assumed. docproc's CI runs both Rust tools' checks when the crate changes.
- **ON-12 · The release and currency machinery assumed the tool was the repository.** Accepted →
  onenote, with the repository's part in "docproc-ooxml [ooxml]", which lands the first tool.
  Nested under `tools/onenote`, the release trigger, the pack script's version check, the asset
  name, the changelog path, and the currency script's workflow glob all break; the scripts are
  near copies of mail's. release-and-ci.md ships every committed file below a `go.mod`, so the
  2.3 MB of fixtures, MPL-2.0 samples among them, would ship in docproc's module zip. One pack
  script and one notices script under `tools/` take the tool's name, and a `go.mod` at `tools/`
  keeps the subtree out of the library's zip, both landed by ooxml; the tool's currency reports
  its crates and mise tools only, action pins being the root's; the nested `.gitattributes` keeps
  its fixture rules. The module-zip exception on the architecture page is a pending edit for the
  goal's sync.
- **ON-13 · The findings harness is spike evidence.** Accepted → onenote. `bench/findings.rs`
  (976 lines), `bench/rusage.c`, and `scripts/findings.sh` score the binary over the fields schema
  "2" cuts, and the check compiled the scorer on every run and never ran it. They stay in the
  spike with `docs/findings/`; a per-tool timing, if one is wanted, is one harness for all three
  tools, in the acceptance task.
- **ON-14 · The stderr line rules were documented, not enforced.** Accepted → onenote, through the
  shared crate. By reading: `place` escaped control characters only, so a section name holding
  `": "`, stored in the file or taken from a renamed input, split a warning wrong, and `<what>`
  passed the parser's messages through unescaped. The shared warning writer writes a colon
  followed by a space in `<where>` with the space escaped, and escapes control characters in
  `<where>` and `<what>`.
- **ON-15 · Byte identity has three edges in the parser's hash or directory order.** Accepted →
  onenote. By reading: a section group's `.onetoc2` is the first `read_dir` or a package's
  `HashMap` yields, a OneDrive package's storage index and manifest are `HashMap::values().next()`,
  and object spaces parse in `HashMap` order, so two faults report in either order. No committed
  fixture reaches them. The README states the three as limits beside the determinism promise.
- **ON-16 · Nesting depth was unbounded, and a stack overflow dies by signal.** Accepted →
  onenote, with the signal's outcome in "docproc's packages [library]". By reading: outline items,
  groups, tables in cells, and equations recurse to the file's depth in the reader, the renderer,
  the math port, and the parser, and a stack overflow is a SIGSEGV no handler catches. The read
  runs on a thread with a large fixed stack, and the README names the limit; docproc records a
  tool's death by signal as failed, naming the signal.
- **onenote_parser's MPL-2.0.** Settled → onenote. The parser is MPL-2.0, outside the
  permissive licenses the Rust tools accept. docproc-onenote's license check allows MPL-2.0 by
  name, and its notices state where onenote_parser's source is. Rejected: treating the license as
  a blocker.
- **Nothing is asked upstream.** Settled → onenote. The evaluation proposed asks to onenote_parser
  (ON-4's error kind, ON-5's modification-time option, ON-9's four, ON-15's ordered maps). The
  architect's ruling: these are docproc's requirements, and docproc fills the gaps for its own
  needs. Every workaround is owned, tested code in docproc-onenote, the tool fills a gap itself
  where it needs to, and nothing is filed upstream.

## processing

From spike-ai-pipeline-processing v0.9.0, read at its tag with a clean tree, its one caller in
spike-ai-pipeline at `6a631f1`, and mail v0.3.1. Four group evaluations (inspection and routing,
PI; tool runners and readers, PT; stages and media, PS; dispatch, content, and index, PD) were
synthesised into sixteen cross-package findings (PX) and twenty decisions; the groups ran their
probes on scratch copies and an entry says where a finding was run, the rest found by reading.
Round 4 accepted every finding as proposed, merged and with conflicts resolved as the synthesis
set out, and every decision as recommended; each decision names the round's question that
settled it. The changes are in `layers.md`, "docproc's packages [library]", unless an entry names
another task; the signatures and sentences the decisions changed are in `layers.md`'s "docproc's
API" sections. An entry merged into a PX finding is settled there and only points to it.

### Decisions

- **1 · Which failures are recorded (Q2).** Settled → library; acceptance counts recorded
  failures by class. PX-1's table, classified once in `stages` from `ExitError.Signal` and
  `TimedOut`: a tool's own exit, its own fatal signal, its own time bound, and output that
  doesn't parse are the content's, recorded; the context ending, a SIGKILL or SIGTERM docproc
  didn't send, a failed start, exit 1 or 2, and docproc's side of the pipe are the run's,
  retried. It is the one rule under which ON-16's signal is recorded failed. Rejected: PD-11's
  narrow rule, only a tool's exit 3.
- **2 · A replay miss (Q3).** Settled → library; the docinfer goal for `job`'s counting. The
  nearest ancestor with a file drops its entry and prepares again, resolving its children one
  level down before its first event; events are at-least-once, and "drops the entries along its
  replay path" goes. Refines the goal's "prepares the replaying part in place". Rejected: found
  parts' bytes kept in `Content`, which doubles blob storage for every store and archive;
  `ErrStale` for redelivery.
- **3 · A lost claim (Q4).** Settled → library. One renewer goroutine per dispatch renews every
  claim in flight from `Claim` to `Record` or `Release`; a refused renewal ends the dispatch with
  `ErrClaimLost`, transient. Rejected: unwinding the part in place, which needs a context tree
  and repeats events anyway.
- **4 · The resume cursor (Q5).** Settled → library; the docinfer goal for the checkpoint's
  wording. The replay skips through the item whose `After` equals `Input.After`, `After` opaque
  and compared by equality alone; no such item ends the dispatch with an error. Supersedes the
  goal's "at most `Input.After`". Rejected: numeric comparison, since mail matches `id == after`
  (mail: src/pst.rs:433) and a `.pst`'s node ids don't ascend in read order.
- **5 · `Event.Keep` (Q6).** Settled → library; docproc for `run`. Dropped: an event's files are
  the caller's to read, link, or move until its body returns; an output is appended, and a
  part's entry recorded, before its event is yielded. Rejected: keeping it, six callers, all
  tests.
- **6 · Flags (Q7).** Settled → library. Content flags in the part's `Entry`, name flags from
  `inspect.Name` at every occurrence, the verdict over both under the current options; `Item.Flag`
  replaces `QuarantineError`; `inspect.Options` gains `Validate`, `Bomb`, and `Verdict`. Answers
  `layers.md`'s open question on a changed policy. Rejected: `Index.Flags` and `RecordFlags`,
  the policy frozen at preparation.
- **7 · The module's shape (Q8).** Settled → library; `layers.md`'s package table, done here.
  The adapters move into `stages`; the envelope reader stays in `internal/cli`; `Warning` stays
  `dispatch`'s; `Read` and `Convert` return the sequence. Rejected: adapters in `dispatch` and a
  public envelope package.
- **8 · `stages.Build`'s options (Q9).** Settled → library. One `Options` struct, each bound's
  owner and outcome stated once in `layers.md`, "Dispatch". Rejected: the three
  settled arguments, under which Cerebro can set none of the groups' bounds.
- **9 · Default bounds (Q10).** Settled → library; docproc (`cmd/docproc`'s starting values);
  acceptance. No `Limits` or run bound has a library default; `cmd/docproc` ships starting
  values, and acceptance dispatches PI-1's bombs through it; the bomb rule sums across an
  archive's entries. Rejected: `dispatch.New` refusing a zero `Limits.Bytes`.
- **10 · Envelope additions (Q11).** Settled → ooxml, mail, onenote; library reads them. Three
  additions under schema "2", before the tool tasks start: `--version`'s second line
  `schema <n>`; the warning escape's form stated, and an error reason never leading with the
  input path; a `message` line's optional `media` for `data:` images (`layers.md`, "Common
  envelope", "docproc-mail"). Rejected: the reader parsing Markdown for PT-7; a schema found
  only at the first read.
- **11 · Archive entries that harm no stage (Q12).** Settled → library. Links, devices, and
  traversing and absolute names become active, the entry an unread child, the rest read; bombs
  and unreadable directories stay unsafe. Rejected: unsafe, the archive quarantined whole and
  `inspect`'s definition reworded.
- **12 · Check names before Version "1" (Q13).** Settled → library. Seven rule names for 24
  per-format ones. Rejected: per-format names, with no evidence of a policy that differs by
  format.
- **13 · Silence detectors (Q14).** Settled → library; acceptance. The tracker alone: one decode,
  audio in an MP4 and an M4A windowed alike, about 250 lines less, at 0.009 to 0.021 worse word
  error rate on three corpora; acceptance measures word error rate on ~/docs' audio before the
  tag, and silencedetect stays in the spike at v0.9.0. Rejected: both detectors, three decodes.
- **14 · Live sources (Q15).** Settled → library. `media`'s reader, URL, and pacing surface stays
  in the spike, by the rule applied to every callerless surface, until `protocol` names an owner.
  Rejected: promoting it with a doc saying no stage uses it.
- **15 · The unread child (Q16).** Settled → library. `Found` may carry an outcome and no file;
  `Meta["warnings"]`, the entry records, and the medium records go. Rejected: records with
  string Meta.
- **16 · A store's warning order (Q1, with PX-5).** Settled → library. Where read, best effort
  (PT-2). Rejected: after the last record (PD-19), which disagrees with decision 4 on a resume.
- **17 · Constructors and names (Q18).** Settled → library; docproc's STANDARDS.md. A constructor
  panics by name on a missing required dependency, and PX-16's five other lines apply; settles
  the goal's deferred "constructors returning errors vs panicking". Rejected: an error for a nil
  tool, as the spike.
- **18 · Test tiers (Q19).** Settled → library; acceptance. The findings and bake-off tiers,
  `corpustest`, and the model harnesses stay in the spike; the readers replay the tools' goldens
  in place; `dispatch/dispatchtest` is public; `internal/tooltest` holds the stand-in. Rejected:
  `layers.md`'s helpers row as written.
- **19 · `FileIndex` (Q17).** Settled → library. Single-process, its claims in memory. Rejected:
  cross-process claim files with `Renew`.
- **20 · The time bound's reach (Q20).** Settled → library. A recorded timeout's bound is out of
  the stage's identity, stated in `dispatch`'s doc: a host that raises it drops what the old
  bound failed. Rejected: the bound in the identity, preparing everything again when raised.

### Inspection and routing

- **PI-1 · The bomb rule passed an archive that expands without bound.** Accepted → library;
  acceptance; decided in decision 9. Ran: a 185,802-byte ZIP of 2,000 entries declaring 2 TiB
  yielded no flag, and one entry at 999:1 declaring 100 GiB proceeded, Deflate topping out near
  1,032:1. The rule also weighs the sum of the entries' declared sizes, a ZIP whose declared
  compressed sizes pass its file is a bomb, and the stage holds what it writes to the same total.
- **PI-2 · An XML encoding declaration hid an SVG's script and a package's external
  relationship.** Accepted → library. Ran: `ISO-8859-1`, `us-ascii`, and `windows-1252` SVGs
  yielded none of their three flags, and an `ISO-8859-1` or UTF-16 `.rels` no
  `external-relationship`. Both scans take a `CharsetReader`, a UTF-16 mark is decoded first, and
  a scan ended early raises its own active flag.
- **PI-3 · A solid 7z cost one decode of its block per entry.** Accepted → library. Ran on a
  2,000-file, four-block archive: an item mid-block took 1.2 s and the whole archive 5.4 s in one
  run, about 20 minutes as the stage looped. One `7zz e -so` run split by the listing's sizes,
  each item held to its size and CRC-32, the one-item run kept for the rest; the library task
  checks the order on a multi-block and a mixed-method archive first.
- **PI-4 · A report depended on the name, and the index recorded it by content.** Merged into
  PX-6; decision 6.
- **PI-5 · The 7zz floor admitted CVE-2024-11477's zstd decoder.** Accepted → library;
  tool-packaging pins the same release.
- **PI-6 · Evidence could be megabytes, invalid UTF-8, and hold a NUL.** Merged into PX-7.
- **PI-7 · An archive was quarantined whole for entries no stage could be harmed by.** Decided in
  decision 11.
- **PI-8 · The inspection options were validated per call and held three times.** Merged into
  PX-6; decision 6.
- **PI-9 · The unit tier ran real programs and read ~/docs.** Merged into PX-9; decision 18.
- **PI-10 · Routing refused content its stage would read.** Accepted → library.
- **PI-11 · Two- and three-byte signatures outranked a text extension.** Accepted → library.
- **PI-12 · The encrypted-package check and the extension tables existed twice.** Merged into
  PX-10.
- **PI-13 · The PDF name scan flagged random bytes and saw no object stream.** Accepted →
  library; its flags from qpdf ride PX-6's `Item.Flag`.
- **PI-14 · Exported identifiers nothing outside the tests used.** Accepted → library, its names
  merged into PX-16 and its root package into PX-2; `route.PNGSignature` stays, `media` its second
  caller (PS-13).
- **PI-15 · Inspect's memory and time were unbounded for a ZIP.** Accepted → library; its entry
  bound comes in through PX-3, and PD-13 relies on it.
- **PI-16 · Tests restated the table they checked.** Merged into PX-9.
- **PI-17 · Each contract was stated three times.** Merged into PX-16.
- **PI-18 · ClamAV doesn't fit Inspect's contract.** Backlog: the roadmap's comment is reworded.
- **PI-19 · Formats the spike left unrouted weren't in the backlog.** Backlog.
- **HTML is unrouted by design.** Backlog: no task carries saved web pages or HTML `.xls`.

### Tool runners and readers

- **PT-1 · The readers' unit tier needed a released binary.** Merged into PX-9; decision 18.
  `TestReal*` ran the tool from its GitHub release, and 7.9 MB of fixtures were copied under
  manifests; the tools' committed goldens become the readers' fixtures, read in place.
- **PT-2 · stderr was kept as its first 1 MiB and read only after success.** Accepted → library;
  decision 16. A run that warned 1 MiB and then failed had an empty `Message`, and a failed or
  resumed read dropped every warning. stderr is a pipe parsed as it arrives, warnings delivered
  before the read's final error, the message the last `error:` line.
- **PT-3 · The run's memory bound had no mechanism.** Accepted → library; acceptance measures it
  over ~/docs on all three tools first. Ran: a tool killed by a signal reported `exit code -1`.
  `Spec.MemoryLimit`: the container runner's `--memory`, the exec runner's `oom_score_adj` 1000
  and `RLIMIT_AS` by `prlimit64`, or docproc-ooxml's collector limit through `Spec.Env` if it
  can't start under it; its outcome is PX-1's, its way in PX-3's.
- **PT-4 · Three copies of the line reader.** Accepted → library, relocated by PX-2: the envelope
  reader in `internal/cli`, `Warning` `dispatch`'s; its stand-in is PX-9's.
- **PT-5 · Death by signal was lost below `internal/cli`.** Merged into PX-1.
- **PT-6 · The typed warning needed a parse.** Accepted → library (the parse, the folder join);
  mail, onenote, ooxml (the envelope's two rules, decision 10).
- **PT-7 · A `data:` image was media no line named.** Decided in decision 10 → mail (the
  field); library (the reader, through PX-14).
- **PT-8 · The exec runner handed every tool the host's environment.** Accepted → library; the
  tools' user is tool-packaging's. Ran: a parent's variable reached the tool.
- **PT-9 · The office stream left five things of `Result` unplaced.** Accepted → library; its
  soffice runner is PX-13's, its media PX-14's.
- **PT-10 · The readers routed media the dispatcher routes again.** Merged into PX-2; its reason
  text into PX-4, its media into PX-14.
- **PT-11 · A tool of another schema passed the startup check.** Accepted → ooxml, mail, onenote
  (`--version`, decision 10); library; `Tool.AtLeast` merged into PX-16.
- **PT-12 · The Threader held six times the backlog's figure.** Accepted → library; the roadmap
  comment states 362 bytes a message. Ran on 200,000 messages: 362 bytes a message, 435 under
  `Threads`.
- **PT-13 · A tool's children outlived a normal exit and the host's death.** Accepted → library.
  Ran: a background child outlived an exit 0.
- **PT-14 · The mail and onenote readers worked through the exec runner only, unsaid.** Merged
  into PX-2.
- **PT-15 · `NewScript` had no caller.** Accepted → library; the architecture page's wording.
- **PT-16 · The reader swapped a message line for a `Conversation`.** Accepted → library.
- **PT-17 · A missing required tool was an error.** Merged into PX-16; decision 17.
- **PT-18 · Decoded times were returned as parsed.** Merged into PX-16.
- **PT-19 · The findings tier is the spike's evidence.** Merged into PX-9.
- **PT-20 · The container lockdown had no process or privilege bound.** Accepted → library.

### Stages and media

- **PS-1 · A signed PDF failed its whole document part in the form reader.** Accepted → library;
  the roadmap's summary is corrected. Ran: `qpdf --json=2 --json-key=acroform` over
  `cfr-2024-title32-vol1.pdf` exits 3, success with warnings, and writes a `/Sig` field whose
  value is `"3033 0 R"`; processing's own form reader refuses that reference value, not qpdf, so
  every signed PDF failed. A field's value never fails the form, and a signature field gains
  `Signed`.
- **PS-2 · One failed read failed the part, unrecorded.** Merged into PX-1 (the outcome) and PX-4
  (the unit's warning). One page gs refused of 1,823 cost the whole part on every rerun.
- **PS-3 · The audio stage decoded the source once per window.** Accepted → library; decision 13.
  645 windows of the 4.4 h audiobook decoded about 322 times its length. One decode cut into
  windows by `audio`'s stream cutter, byte for byte v0.9.0's windows.
- **PS-4 · A PDF's pages were rendered ahead of the consumer without bound.** Accepted → library;
  `MaxPixels` comes in through PX-3.
- **PS-5 · No run had a time or resource bound, and no stage capped its outputs.** Merged into
  PX-1 (the timeout's outcome) and PX-3 (the bounds; an output cap is the request's, recorded
  with a warning, never `limited`).
- **PS-6 · The audio plan trusted ffprobe's duration.** Accepted → library.
- **PS-7 · Corrections, regions, and the text-skip are experiment surface.** Accepted → library:
  they stay in the spike.
- **PS-8 · The vision harness and the bake-off tiers couldn't move as `layers.md` said.** Merged
  into PX-9; decision 18.
- **PS-9 · `Images` spooled the PDF a second time.** Merged into PX-10.
- **PS-10 · A model's limits were library constants.** Merged into PX-8; `Limit`, `Density`, and
  `DefaultDPI` accepted with it.
- **PS-11 · The audio identity left out four fields it obeys.** Merged into PX-8; decision 13
  leaves one detector.
- **PS-12 · media's live sources had no way in.** Decided in decision 14.
- **PS-13 · The same helpers were written two to five times.** Accepted → library; `atLeast`
  merged into PX-16, the soffice runner PX-13's.
- **PS-14 · Every package rebuilt the stand-in.** Merged into PX-9: `internal/tooltest`, not
  `tool/tooltest`.
- **PS-15 · picture read each file three times.** Merged into PX-10.
- **PS-16 · Constructors disagreed with the construction rule.** Merged into PX-16; decision 17.
- **PS-17 · A PDF cost two gs processes a page, serially.** Accepted → library (the concurrency,
  through PX-3); acceptance times the 1,823-page PDF per page and by ranges.

### Dispatch, content, and index

- **PD-1 · A replay miss couldn't be prepared in place.** Decided in decision 2. A replayed child
  has no file and nothing stores a found part's bytes, and dropping the replay path made the next
  dispatch read a whole `.pst` for one missing attachment.
- **PD-2 · "At most `Input.After`" skipped messages in a `.pst`.** Decided in decision 4. The
  skipped prefix keeps the store's and folders' outputs, so a resumed replay and preparation yield
  the same events.
- **PD-3 · Puts before events made the walk a pipeline.** Accepted → library; its failure class
  merged into PX-1, its reused hash into PX-10. The queue, its drain before a found part and the
  part's own event, and `ErrTooLarge` mapped from go-storage's are stated in `layers.md`.
- **PD-4 · A lease had to outlast the loop body.** Decided in decision 3. A claim lasts from
  `Claim` through every yield of the part's subtree, so a timer on the walk's goroutine never
  fires while the body blocks it.
- **PD-5 · `Event.Keep` had no caller.** Decided in decision 5.
- **PD-6 · Flags can leave the `Index`.** Merged into PX-6; decision 6.
- **PD-7 · `FileIndex`'s cross-process claims can't meet the lease contract.** Decided in
  decision 19; its directories shard by the name's first two hex digits.
- **PD-8 · A recorded entry no longer fresh couldn't be claimed.** Accepted → library; its
  one-name rule merged into PX-8.
- **PD-9 · An append needs no durability before `Record`.** Accepted → library.
- **PD-10 · Any failure on content flagged encrypted was recorded Encrypted.** Accepted →
  library, less its Ghostscript mapping, which PX-12 supersedes; the class is PX-1's.
- **PD-11 · A refusal the content decides was never recorded.** Merged into PX-1; decision 1.
- **PD-12 · `Where` came from untrusted names.** Merged into PX-7.
- **PD-13 · Three per-dispatch maps grew with a store.** Accepted → library; the sibling set is
  bounded by PI-15's entry bound.
- **PD-14 · Release and Record ran unbounded under `WithoutCancel`.** Accepted → library; the
  release timeout is a per-process bound through PX-3.
- **PD-15 · A replay couldn't tell a truncated log.** Accepted → library.
- **PD-16 · The adapters were a second package inside `dispatch`.** Merged into PX-2; decision 7.
- **PD-17 · The providers need the index contract as a suite.** Merged into PX-9.
- **PD-18 · The test tiers didn't divide where `layers.md` did.** Merged into PX-9.
- **PD-19 · A stage's own and a store's warnings had no place.** Merged into PX-4; its order is
  superseded by PX-5.
- **PD-20 · The archive stage took its own copy of the options.** Merged into PX-6, its `Check`
  dropped.
- **PD-21 · `FileOptions.Now` was a clock, and a claim's time local.** Merged into PX-16.
- **PD-22 · dispatch's shapes had no committed output.** Merged into PX-9.

### Across the module

- **PX-1 · A failed run was classified in five places.** Accepted → library; acceptance;
  decisions 1 and 20. `tool` reports the facts (`ExitError.Signal`, `TimedOut`, the run's own
  time bound), `dispatch` declares the classes, and one function in `stages` classifies.
- **PX-2 · The module's shape follows from one fact: only `stages` calls the format packages.**
  Accepted → library; decision 7. The build tag stays on the exec runner alone.
- **PX-3 · Every group added a value the host must supply.** Accepted → library; docproc;
  decisions 8 and 9. Each bound is stated once with its owner and outcome (`layers.md`,
  "Dispatch"); a unit run takes a deadline, a stream run an idle bound.
- **PX-4 · "The part held something not read" had seven spellings.** Accepted → library; decision
  15. A flag, a warning, or a child, so counts by outcome become true and a package's encrypted
  section gets its outcome.
- **PX-5 · Two groups ordered a store's warnings differently.** Accepted → library; decision 16.
- **PX-6 · Flags got four partial designs.** Accepted → library; decision 6.
- **PX-7 · Untrusted text reached the wire by six fields.** Accepted → library. One guard at the
  dispatcher's exit for every wire string, `encoding/json` the wire's package; one hostile-bytes
  test.
- **PX-8 · A stage's identity was assembled by hand.** Accepted → library. The resolved request,
  the tools' whole version lines, and the adapter's version, hashed whole.
- **PX-9 · The groups' test findings are one design.** Accepted → library; acceptance; decision
  18.
- **PX-10 · Format packages worked out again what the part carries.** Accepted → library.
- **PX-11 · `Output.Meta` was an undeclared wire vocabulary.** Accepted → library; the docinfer
  goal reads the constants. SH-1 moves them from each writing package into `dispatch`.
- **PX-12 · Every adapter supported tool sets no wiring builds.** Accepted → library.
- **PX-13 · The LibreOffice runner was written twice.** Accepted → library.
- **PX-14 · Three readers turned media into parts by three rules.** Accepted → library.
- **PX-15 · The archive vocabulary was spelled three times.** Accepted → library; decision 12.
- **PX-16 · Conventions the groups each asked for once per package.** Accepted → library, the six
  lines into docproc's STANDARDS.md with it; decision 17.

## cmd/docproc

From spike-ai-pipeline's docproc command at `6a631f1`, over its library,
spike-ai-pipeline-processing v0.9.0, with the spike's docinfer read for its use of `rundir`. Every
finding was found by reading; `go vet` passed over the command, and nothing else ran. Round 5
accepted every finding, DP-3 and DP-10 in the form it settled, and a follow-up settled DP-16's
form. An entry is carried by docproc unless it names another task; the command's changes are in
`layers.md`, "The nested modules", and `rundir`'s in the `rundir` row of "docproc's packages
[library]", with its API under "The nested modules".

- **DP-1 · The schema "2" event line couldn't be the embed `layers.md` listed.** Accepted →
  docproc, and library for `Part`'s `omitzero`. Five things didn't fit. An input's error line
  carried its text in `error` (run/execute.go:235), and `dispatch.Event.Err` never marshals, so
  the text had no field; `Event.Part`, a value tagged `part`, would marshal an empty part on an
  error line, where "1" leaves it out; `schema.Event`'s kind field is named `Event`, and
  embedding `dispatch.Event` declares a second field of that name, which doesn't compile; "1"'s
  `warning`, the command's note that no record held a seen part's children (run/replay.go:240),
  has no successor once the library replays children, while dispatch's `Warning` is a line kind
  "1" never had; and a zero count is now left out. The line embeds `dispatch.Event` inline
  beside its own kind, an error line's text, and the record's fields, `Event.Part` gaining
  `omitzero`, and its kinds are `part`, `output`, `warning`, and `error` (`layers.md`, "The
  nested modules").
- **DP-2 · The record was the one file not written whole, and a killed run's was unreadable.**
  Accepted → docproc. `Run.Write` encoded into a 4,096-byte `bufio.Writer` flushed only in `End`;
  run 1 over ~/docs wrote 12,008 lines in 9.5 MB, about 800 bytes a line, so the file on disk
  ended mid-line for nearly all of its 27 minutes. A run killed by SIGKILL, the OOM killer, power
  loss, or a panic never reaches `End`: its manifest has no `end`, its record ends in a torn line
  with up to a buffer of finished lines lost though the index recorded their parts, and
  `ReadRecord` fails the whole record (`event N: unexpected EOF`), so `docproc report` and
  docinfer's `source` both refused the run. `Latest` returned the greatest id, ended or not, so
  the default run of both was that one, or one still running, and the killed run's `.work-*`
  directory stayed. Each line is flushed as written, one `write`, no fsync; a reader drops an
  unended run's last line when it has no newline; `Latest` is the latest ended run, and a reader
  given an unended run by id says so; `Begin` removes leftover `.work-*` directories, under one
  writer per run directory, which `rundir`'s doc states.
- **DP-3 · `rundir` landed in the library's module after `library` tagged it.** Settled → docproc
  and reports. `rundir` and `rundir/schema` are base-module packages the `docproc` task builds,
  after `library` tags the base module v0.1.0, and `cmd/docproc`'s module requires the base
  module by version, so `cmd/docproc/v0.1.0` had no tagged library holding `rundir` to require,
  nor had `cmd/docinfer`, under the plan's "`docproc` releases nothing". The `docproc` task tags
  the base module's next minor, v0.2.0, when `rundir` lands; `reports` tags it again only if it
  changes `rundir`, before it tags `cmd/docproc/v0.1.0`; a committed `go.work` over the base
  module and `cmd/docproc` carries changes between tags. Reverses "`docproc` releases
  nothing". Rejected: `library` building `rundir` so v0.1.0 holds it, which tags a public
  package before its one writer exists.
- **DP-4 · The copy of a seen part's subtree goes.** Accepted → docproc. The spike's dispatcher
  never replayed a seen part's children, so `run/replay.go` (249 lines) copied each subtree from
  the earliest record holding it, loading every earlier record into memory first, with a second
  copy of the report's tree builder and a warnings column. The library's dispatcher replays the
  log, children included, so the file, the counters, and the summary's warnings row go, and an
  index kept while `runs/` is deleted replays fully.
- **DP-5 · `rundir` becomes one handle that a reader opens without creating anything.** Accepted →
  docproc, and the docinfer goal for `outdir`'s own atomic write, a pending edit. `Open` made the
  directory and its index, so readers used seven free functions over `(root, id)`, `ValidID` a
  check each caller had to remember (four sites), passing any path element, and `Runs` counting
  any stray directory. `Open` reads and creates nothing, `Create` writes; every id is checked as a
  run of the directory; `Load`, `WriteRunFile`, and `WriteFile` leave the API, the report writing
  its pages with the command's own helper.
- **DP-6 · The run directory carried no version of its own.** Accepted → docproc. Only docinfer
  compared the manifest's schema; `docproc run` opened an index and store of any layout, and
  ~/docs holds schema "1" directories the acceptance rerun will meet. `Create` writes
  `rundir.json`, and `Open` and `Create` refuse another schema, or `runs/` without the file,
  naming both versions; the schema page states that "2" includes `dispatch`'s shapes at
  `dispatch.Version` "1", a break there a break here.
- **DP-7 · The content store's layout joins the contract.** Accepted → docproc (the layout and its
  path), and reports (the pages). The "1" report linked each output under `outputs/` by an
  extension a browser opens; a content named by its SHA-256 has none, so a linked text downloads,
  and a page that names a content's file fixes `FileContent`'s layout for every reader that isn't
  Go. The layout is part of schema "2" and `rundir` exports a content's path, joined to the layout
  `dispatch` exports (SH-6); the report inlines text, Markdown, and records from the content store,
  points `<img>` and `<audio>` at the file, and renders over the reader's handle.
- **DP-8 · A page per input SHA-256 collided, and some inputs have none.** Accepted → reports. 17
  of ~/docs's 592 inputs share their content with another, and an input stopped before its own
  event has no SHA-256. A page per distinct input content lists every input path holding it with
  each one's file metadata, outcome, and time, the tree and outputs once; the index page lists
  every input, error inputs included. The roadmap's `reports` summary says "per input content", a
  pending edit.
- **DP-9 · An index failure was an input's error, and the run exited 0.** Accepted → library (the
  error's type) and docproc. The pool yields an index or content-store failure with its input, so
  a full disk became an `error` line per input and exit 0. The library types a store's failure
  apart from an input's; `run` stops on it, exit 1; failed parts and unreadable inputs keep exit
  0, stated in the help, the summary's last line counting both.
- **DP-10 · The nested module's layout.** Settled → docproc; STANDARDS.md states the exception.
  go-web-service's `tools/slab` puts domain packages at the nested module's root, which here
  would make the install path `cmd/docproc/cmd/docproc` and `run.Commands` API under
  `cmd/docproc/v0.1.0`. `main.go` sits at the module's root, installed by
  `go install github.com/standards-lab/docproc/cmd/docproc@…`, with `app`, `apptest`, `run`, and
  `report` under `internal/`. Rejected: slab's layout. The architecture page's nested-command
  layout is a pending edit for the goal's sync.
- **DP-11 · `rundir`'s contract was tested only from the command.** Accepted → docproc.
  `rundir/testdata` holds one committed schema "2" run directory that `rundir`'s tests read
  through the reader's API and write again through the writer's.
- **DP-12 · A symbolic link or special file argument yielded nothing, silently.** Accepted →
  docproc. A named argument that is a link is followed; one that is neither file nor directory,
  or arguments yielding no input, fail the run before any tool is probed.
- **DP-13 · `export_test.go` exported the command's body.** Accepted → docproc. The graph's node
  becomes the stage builder, which `apptest` replaces, and `export_test.go` goes.
- **DP-14 · `FileOf`, `Stater`, and `Detect` move with `Statx`.** Accepted → docproc, so
  `rundir/schema` is shapes alone.
- **DP-15 · The manifest's `record` repeated the layout and was trusted as a path.** Accepted →
  docproc. `record` and `Run.Record` go; `run` stays.
- **DP-16 · The manifest's `Tool` and `StageState` copied `stages.Tool` and `Row`.** Accepted →
  library (the shapes) and docproc (the manifest), in the form the follow-up settled, which
  supersedes round 5's: the stage table's shapes are declared once in `dispatch`, versioned by
  `dispatch.Version`; `stages.Set` holds them, and `rundir/schema`'s manifest uses them, so
  `schema` imports no docproc package but `dispatch` and `inspect` (`layers.md`, "Dispatch", "The
  nested modules"). Rejected: `schema` aliasing `stages`' shapes, which ties schema "2" to an
  unversioned package and has `schema`, and so `cmd/docinfer`, compile `stages` and every format
  package beneath it; `schema` keeping its own copies, the standards review's interim form, which
  leaves two definitions of one shape to drift.
- **DP-17 · Every tool was probed before the inputs were checked.** Accepted → docproc.
- **DP-18 · `flags.json` repeated the record.** Accepted → docproc. The kept file is
  `quarantine/<sha256>`, named on the line whenever it exists.
- **DP-19 · The port to go-cli-sdk v0.4.0 and go-core v0.12.0.** Accepted → docproc, starting
  `internal/app` and `internal/apptest` from go-cli-sdk-template at v0.4.0.
- **DP-20 · The manifest didn't say what wrote it or how many inputs it had.** Accepted → docproc.
- **DP-21 · `--workers` refused a value above the host's CPUs.** Accepted → docproc.
- **DP-22 · `ReadRecord` returned the whole record.** Accepted → docproc. `Record` is a
  sequence.
- **DP-23 · An input's time included its wait at the pool's feeder.** Accepted → docproc.
- **DP-24 · `execute` took six inputs, and `schema.Time` kept its zone.** Accepted → docproc.
- **DP-25 · The roadmap's `docproc` summary named a role `stages` now has.** Accepted → docproc
  (its README names `stages` as the shipped wiring and `cmd/docproc` its first caller); the
  summary's rewording is a pending edit.

## Structural single homes

Round 6, the architect's follow-up to DP-16's fix, looked across the artifacts for a shape, a
vocabulary, or a rule stated in two places, or held by a package that isn't its owner, and
accepted all nine findings as recommended. Each settles one home; its signatures and change-list
lines are in `layers.md`, the section an entry names, and the docinfer goal's parts are pending
edits in the goal record.

- **SH-1 · The Meta keys a consumer reads.** Accepted → library; the docinfer goal reads the
  constants. Refines PX-11, which put each key in the package whose stage writes it: decision 7
  moved every writer into `stages`, so a consumer reading `window`, `count`, `page`, or `sheet`
  would import `stages` and every format package beneath it, as DP-16 found for the manifest.
  The keys are constants in `dispatch` beside `Output`, each documented with its writer and its
  format, versioned by `dispatch.Version`; docinfer reads them there (`layers.md`, "Dispatch").
  Rejected: the constants in `stages`.
- **SH-2 · The transcript join was inference held in docproc.** Accepted → library (`audio`
  drops it) and the docinfer goal (`join`), a pending edit. `audio` held `Join`, `JoinRequest`,
  `Seam`, and `Transcript`, and `Seam` gained JSON tags, only so docinfer's inferences could
  carry it: nothing in docproc joins transcripts, docinfer's `Version` versioned a shape docproc
  declared, and docinfer's root imported `audio` for it. They move to a small `join` package
  beside `chunk`, `JoinRequest` as `Request`, stating docinfer's own seam-word setting, versioned
  by `docinfer.Version`; docinfer needs nothing of docproc but `dispatch`, and `rundir` for
  `cmd/docinfer` (`layers.md`, "The pipeline", "Assumptions"). Rejected: `audio` keeping the join
  for docinfer to import.
- **SH-3 · The envelope's cases were tested three times.** Accepted → ooxml (writes the table and
  its `tools/` wiring), mail (the shared crate reads it), library (`internal/cli` reads it). The
  warning escape, the split of a stderr line into `where` and `what`, the media name a payload's
  bytes and stored name make, the exit codes' meanings, and the literal-text escape were stated
  once in prose and tested in C#, Rust, and Go, each over its own cases; ML-9's drifts came from
  copies of this kind. One committed table, `tools/envelope/cases.json`, is read in place by all
  three, and `internal/cli` exports the schema constant and the version pattern once for the
  readers' specs (`layers.md`, "Common envelope"). Rejected: each language keeping its own cases.
- **SH-4 · The found-part hash check read a name as a hash.** Accepted → library. The dispatcher
  checked a found part whose name was a SHA-256, so the envelope's media name rule had a second
  home in `dispatch` beside `internal/cli`, and a part named like a hash was checked while one
  whose stage knew its hash under another name was not. A stage claims a found part's SHA-256 on
  `Found`, which the dispatcher checks as it checks `Input.SHA256`; the media name rule stays in
  `internal/cli` alone (`layers.md`, "Dispatch"). Rejected: the dispatcher keeping the rule.
- **SH-5 · A refusal's reason was mapped stage by stage.** Accepted → ooxml (its encrypted
  refusal), onenote (its goldens), library (the mapping, the outcome). office's and onenote's
  stages each mapped a reason beginning `encrypted`, onenote's stage matched its table of
  contents' fixed reason, and the contract stated no reason docproc-ooxml gives an encrypted
  package, an OLE compound file as a legacy document is. "Common envelope" holds one vocabulary:
  for every tool a reason beginning `encrypted` means an encrypted input, and onenote's
  table-of-contents reason is fixed text. `internal/cli` holds the one prefix mapping, the
  table-of-contents mapping beside it, which yields `dispatch`'s new outcome `read elsewhere`
  (`ErrReadElsewhere`): the part's content is read through its sibling sections, so it is no
  failure. docproc-ooxml refuses an `EncryptedPackage` with a reason beginning `encrypted`, and a
  legacy OLE file with one that neither begins with nor contains the word; goldens pin the texts
  (ooxml's encrypted fixture's `.stderr`, `Encryption.one`, a `.onetoc2`). Rejected: each stage
  mapping its own tool's reasons.
- **SH-6 · The content store's layout was written twice.** Accepted → library (`dispatch`) and
  docproc (`rundir`). `FileContent`'s layout, sharded by a name's first two hex digits, was its
  own, and `rundir.ContentPath`, which DP-7 made part of schema "2", restated it. `dispatch`
  exports it once, `ContentFile(sha256)`, a path relative to the store's directory, whose doc
  states the layout stable; `FileContent` writes by it, and `ContentPath` joins the run
  directory's content directory to it (`layers.md`, "Content"). Rejected: `rundir` restating the
  layout under schema "2".
- **SH-7 · A record output didn't say what it holds.** Accepted → library. A record output's
  shape, an audio or video plan or a tool's lines, was known only from its stage and its name,
  versioned by nothing a reader could check. Its `Output.MediaType` names the shape and its
  version (`application/vnd.docproc.audio-plan+json; version=1`, a tool's lines with
  `schema=2`), constants in `dispatch` beside `OutputKind` and the Meta keys, which `stages`
  sets (`layers.md`, "Dispatch"). Rejected: a Meta key for the shape's version.
- **SH-8 · `ContentType.Detected` was a host's field in a shared shape.** Accepted → docproc
  (`rundir/schema`'s `File`, `cmd/docproc`'s `run`), library (`dispatch` drops it), and the
  docinfer goal (the metadata function), a pending edit. `Detected` was `cmd/docproc`'s sniffed
  type of an input, which no stage fills and a stream never has, inside a shape every event
  carries, and docinfer's metadata text read it from the depth-0 part, built by `job` and by
  `cmd/docinfer` alike. It moves to `rundir/schema`'s `File`, filled by `run` beside `Statx`;
  docinfer builds an input's metadata text with one function over `dispatch.Part`, and
  `cmd/docinfer` adds its file lines (`layers.md`, "One job, and its cancellation"). Rejected:
  keeping it in `dispatch.ContentType`, left empty everywhere but `cmd/docproc`.
- **SH-9 · `dispatch.Version` didn't say what it covers.** Accepted → library; the docinfer goal
  for its version's doc. `Version` named "the shapes below", while the shapes carry route's
  `Kind` and `Route` and inspect's `Check`, `Class`, and `Flag`, which change without a field
  changing, and the index's `Key`, `Entry`, `Recorded`, and `Child` are what a provider stores;
  no golden held a routed or flagged part, and docinfer's "1" carried dispatch's shapes without
  saying so. `Version`'s doc names the shapes, the vocabularies they carry, and the index's
  shapes; `dispatch`'s goldens hold a routed, flagged part and an entry's log; docinfer's "1"
  states that it includes dispatch's "1", as `rundir`'s "2" does (`layers.md`, "Dispatch",
  "docinfer's shapes"). Rejected: a version of their own for route's, inspect's, and the index's
  vocabularies.

## Releases

Every new artifact starts at v0.1.0, whatever its spike's version, and its CHANGELOG's first entry
links the spike release, or commit, it is promoted from. The tools release first, since the
library's readers take their first schema "2" tags as their minimums.

```text
tools/ooxml/v0.1.0     docproc-ooxml     ooxml      from spike-ai-pipeline-office v0.1.0
tools/mail/v0.1.0      docproc-mail      mail       from spike-ai-pipeline-mail v0.3.1
tools/onenote/v0.1.0   docproc-onenote   onenote    from spike-ai-pipeline-onenote v0.1.0
v0.1.0                 docproc           library    from spike-ai-pipeline-processing v0.9.0
storage/v0.1.0         docproc/storage   library    new
postgres/v0.1.0        docproc/postgres  library    new
v0.2.0                 docproc           docproc    rundir and rundir/schema landed
cmd/docproc/v0.1.0     cmd/docproc       reports    from spike-ai-pipeline's docproc at 6a631f1
```

- **The tools.** `.github/workflows/release.yml` releases each `tools/*/v*` tag through
  `tools/pack.sh`, which runs the tool's `publish` task; each tool's README, "Install", lists
  its archives.
- **The library.** `library` tags the base module, `storage`, and `postgres`, through the Go
  module releases it adds to docproc's CI.
- **docproc.** The `docproc` task tags the base module v0.2.0 when `rundir` lands (DP-3), and a
  committed `go.work` over the base module and `cmd/docproc` carries changes between tags.
- **reports.** `reports` tags the base module again only if it changed `rundir`, then
  `cmd/docproc/v0.1.0`.
- **Untagged.** docproc-envelope, the shared Rust crate at `tools/envelope`, is a path
  dependency of each Rust tool and is never tagged; a change to it runs every Rust tool's check.
