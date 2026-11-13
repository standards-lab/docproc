# design

The design review: Fable 5.2 evaluated each of the five artifacts v1.ai.processing promotes, and
the architect settled every finding in one review round per artifact. `layers.md`
holds the boundaries and the change lists; this note holds each finding's settled design and its
disposition, with the evidence that justified it, since the evaluations themselves are not kept.
An entry names the task that carries it, and the `layers.md` section that states it where one
does; it doesn't restate a signature that section holds. The note opens with how the review
changes the goal's tasks and closes with each artifact's first tag and the task that releases it.

## How the review changes the goal's tasks

The settled designs grow every build task and move work between them. The changes are pending
edits in the goal record, applied to the roadmap at SHIP.

- **ooxml** grew most, and took docproc's CI from `library`: xlsx streamed in two passes, `ole`
  media as stored, hyperlinks and unread-kind warnings, hand-parsed arguments in place of
  System.CommandLine; docproc's CI with its always-running `ci` gate and ruleset, the root
  workflow releasing `tools/*/v*` tags, the root check's `TOOLS` list, and `tools/go.mod` with
  the shared pack and notices scripts.
- **mail** builds the shared Rust envelope crate under `tools/`, which onenote adopts, and gains
  `smtp_address` for senders and recipients; its fixture generator holds MimeKit at 4.x on .NET 11.
- **onenote** gains the file-only option and the table-of-contents refusal, the encryption
  signal, each drawing's ink alt text, an exact onenote_parser pin with owned workarounds,
  MPL-2.0 allowed by name, release overflow checks, and third-party notices.
- **Every tool** gains the envelope additions: `--version`'s `schema <n>`, the warning escape, and
  error reasons that never lead with the input path; mail its message `media`.
- **library** adds the Go job and the Go module releases to ooxml's CI, classifies failures once,
  replays a miss from the nearest ancestor with a file, sets no default bounds, records a table
  of contents' refusal and a death by signal, and corrects the signed-PDF cause; unrouted
  formats, HTML, and ClamAV stay backlog.
- **docproc** gains a tag, the base module's v0.2.0 when `rundir` lands, reversing "`docproc`
  releases nothing", with `rundir` as one handle and its README naming `stages` as the shipped
  wiring.
- **reports** writes a page per input content and tags `cmd/docproc/v0.1.0`.
- **acceptance** gains measurements over ~/docs: each tool's peak memory, the word error rate of
  its audio before the tag, and the inspection evaluation's bombs through `cmd/docproc`'s
  starting bounds.
- **The docinfer goal** counts a job's outputs by part ID and owns `outdir`'s atomic write; the
  **architecture** task takes the architecture-page edits (the `tools/` module zip, a nested
  command's layout, the rule for external repositories, CI's gate job).

## docproc-ooxml

From spike-ai-pipeline-office v0.1.0. Probes ran on the spike's published binary
(0.1.0+6ed0d0e, linux-x64). Every finding was accepted; the changes are in `layers.md`,
"docproc-ooxml [ooxml]" unless an entry says otherwise.

- **OX-1 · A sheet's charts and pictures had no line for their Markdown.** Accepted → ooxml. The
  renderer writes a sheet's chart tables and picture links after its cell table, and a chart
  sheet has only those, so no line held them. They ride on a closing `rows` line of that sheet
  with `markdown` and no `rows`, bounded as any `rows` line is; a chart sheet is a `sheet` line
  and that closing line. The lines' Markdown fragments concatenate with nothing between them,
  each ending in its own newlines, since the renderer's separators differ by place (`layers.md`,
  "docproc-ooxml"). Rejected: a new line kind for a sheet's drawing.
- **OX-2 · The tool held the whole workbook in memory.** Accepted → ooxml. One sheet of 500,000
  rows by 10 cells (17 MB on disk) took 25.5 s and 8,201 MiB peak; a killed tool exits 137, none
  of the contract's codes, and `rows` lines bound only the reader. Each worksheet part is read
  twice, forward-only: a validate pass that writes nothing collects each sheet's used columns,
  shared-formula anchors, and merges, and its media's hashes, so a refusal still comes before any
  output; a write pass emits the `sheet` line and each `rows` line as its rows are read. docx and
  pptx keep the whole-model path, their `document` line being whole by contract. Acceptance
  measures peak memory over ~/docs. Rejected: deferring with a stated ceiling.
- **OX-3 · stdout's bytes depended on the locale.** Accepted → ooxml. `ruby.docx` wrote `東京\n`
  under UTF-8, `LANG=C`, and an empty environment, and `??\n` under `LC_ALL=en_US.ISO-8859-1`,
  exit 0. stdout and stderr are written as UTF-8 bytes to the raw streams, never `Console.Out`,
  and a CLI test runs a non-ASCII fixture under that locale against its golden.
- **OX-4 · stderr labels and exit codes differed from the envelope.** Accepted → ooxml, and
  library drops the `Labels` list from `internal/cli` (`layers.md`, "docproc's packages
  [library]"). The spike wrote `refused:`, `failed:`, and `media not written:`; a usage error
  had no tool name; `<where>` was the input path, which can hold `": "`; `> /dev/full` exited 4;
  a closed pipe exited 0 over truncated output; a closed stdout aborted with 134. One stderr
  writer, the tool name one constant, `error:` for codes 1 to 4, usage included; `<where>` a
  place such as `sheet 2`, by ordinal; an I/O error on stdout exit 1, a broken pipe included; no
  fault leaves as 134.
- **OX-5 · Embedded and OLE media were a larger change than "Added" read.** Accepted → ooxml.
  `ole` media is the part as stored; unwrapping its compound file, which the Open XML SDK can't
  read, is docproc's, whose inspection already recognises one (`layers.md`, "docproc-ooxml",
  corrected from "the native payload"). A chart's embedded source workbook is never media: nine
  fixtures hold one, and as media its data would be extracted twice. Payloads are found through
  each story part's, sheet's, and slide's own embedded-package and embedded-object
  relationships, `prog_id` from the element naming that relationship; the Markdown links a
  payload as `[<prog_id or the part's file name>](<file>)` after its preview picture; a medium's
  `sheet` is the ordinal of the `sheet` line, where the spike counted sheets written. Generated
  xlsx and pptx fixtures gain an embedded package and an OLE object. Rejected: the tool
  extracting the native payload.
- **OX-6 · The real-world files had no golden.** Accepted → ooxml. The 30 files Word, Excel, and
  PowerPoint wrote were checked only against the tool's own in-process output. Each of the 72
  fixtures gets one `.jsonl` and `.stderr`, compared on the published binary, with no shared-file
  fallback; a `golden` task re-records them and the check never does; the truth tests stay as
  tests of the internal model.
- **OX-7 · The model's JSON machinery can go.** Accepted → ooxml. The model becomes internal and
  loses its JSON attributes, converters, and source-generated context; one `Utf8JsonWriter`
  writer spells out the `document`, `sheet`, and `rows` lines' fields in order and measures a
  `rows` line against 256 KiB.
- **OX-8 · System.CommandLine dropped.** Accepted → ooxml. The command line is one input,
  `--media`, `--version`, and `--help`, and the spike already printed parse errors and caught
  exceptions around the library. Arguments are parsed by hand, the usage text a constant;
  DocumentFormat.OpenXml is the only package; .NET 11 stands. Reverses the plan decision
  "System.CommandLine 3.0.0".
- **OX-9 · A published advisory failed the check without a code change.** Accepted → ooxml. The
  v0.1.0 tag stopped building when ILCompiler 10.0.13 got an advisory. NuGet's audit is off in
  the check's and the release's builds and runs in `currency`, never a gate.
- **OX-10 · Nesting under `tools/ooxml`.** Accepted → ooxml, with library's part in
  "docproc's packages [library]". `tools/ooxml/` holds `src/`, `test/`, `fixtures/`, its own
  `mise.toml` (toolchain pins; `check`, `currency`, `fixtures`, `golden`), `CHANGELOG.md`,
  `README.md`, and `.gitignore`; names `DocprocOoxml.slnx`, `src/Docproc.Ooxml`, namespace
  `Docproc.Ooxml`, binary `docproc-ooxml`. docproc's root `mise run check` runs each tool's own
  check: the root `mise.toml` pins the tools' toolchains and loops a `TOOLS` list beside
  `GO_MODULES`. CI splits the same checks into a Go job and a tool-by-runner matrix on native
  x64 and arm64 runners, since a NativeAOT publish can't cross-link. The ooxml task builds
  docproc's CI and the one root workflow releasing `tools/*/v*` tags, deriving the directory,
  changelog, and version (`${TAG##*/v}`) from the tag and writing
  `docproc-ooxml-v<version>-<rid>.tar.gz` with `SHA256SUMS` and the repository's LICENSE; mail
  and onenote add a matrix row each; library adds the Go job and the Go module releases.
- **OX-11 · Hyperlink targets unread; unread content left no trace.** Accepted → ooxml. External
  hyperlinks render as `[text](target)` in all three formats, an internal anchor staying text;
  4 of the 85 packages in ~/docs hold hyperlinks. Each unread kind a document holds (text boxes,
  equations, SmartArt, pptx comments, xlsx notes) gets one warning. Reading those kinds stays
  backlog.
- **OX-12 · The baseline harness isn't promoted.** Accepted → ooxml. `bench/`, its tests,
  `baseline.sh`, and `docs/findings/` stay in the spike as its evidence, and Markdig with them;
  the acceptance rerun over ~/docs is the measurement from then on.
- **OX-13 · `properties` typed as stored needs a reading rule.** Accepted → ooxml. The SDK parses
  core dates and hides the stored text, and a properties part that fails to parse would refuse a
  readable document. The rule is in `layers.md`, "docproc-ooxml".
- **OX-14 · office's escaping wasn't the shared rule.** Accepted → ooxml. office escaped `<` and
  `>` everywhere and left `$`; onenote follows htmd. The escaped characters are listed in
  `layers.md`, "Common envelope", and ooxml's escaping matches them while every golden is
  re-recorded.
- **OX-15 · A chart in a docx table cell kept only its title.** Accepted → ooxml. It renders as
  its own chunk after the table, its title left in the cell.
- **OX-16 · A private-use character could become a footnote label.** Accepted → ooxml. Found by
  reading: only U+E000 was cleared from the text, the placeholders running from U+E000 up. A
  paragraph renders as segments, each text run escaped and each label appended, with no
  placeholder.
- **OX-17 · An elapsed time past a day became a date in 1900.** Accepted → ooxml. 1.5 in `[h]:mm`
  came out as `1900-01-01T12:00:00`. A value in an elapsed format (`[h]`, `[m]`, `[s]`) stays a
  number, as stored.
- **OX-18 · `_xHHHH_` escapes were written literally.** Accepted → ooxml. Shared and inline
  strings decode them, `_x005F_` the escaped underscore, with a generated fixture cell.
- **OX-19 · Two refusals gave the wrong reason.** Accepted → ooxml. A directory was refused as "no
  such file" and an xlsb as a malformed package. A path that isn't a regular file is "not a
  file", and the main part's exact content type decides the format, so xlsb is "not a docx,
  xlsx, or pptx package".
- **OX-20 · The binary needs glibc.** Accepted → ooxml. The build links libc and libm
  dynamically, with symbols up to GLIBC_2.34, where mail and onenote are static musl. The README
  states the glibc floor and the release job checks it on the built binary; a static musl
  publish is the capstone's `tool-packaging` call.

## docproc-mail

From spike-ai-pipeline-mail v0.3.1. Probes ran on the spike's committed musl release binary
(0.3.1, linux-x64) over inputs built in a scratch directory; an entry says where a finding was
found by reading. Every finding was accepted, ML-3, ML-6, ML-11, and ML-12 in the form the round
settled; the changes are in `layers.md`, "docproc-mail [mail]" unless an entry says otherwise,
and an entry that binds docproc-onenote too names onenote.

- **ML-1 · One message's HTML body ended the whole run by signal.** Accepted → mail. htmd 0.5.6
  walks the DOM recursively and html5ever caps no depth: an `.eml` whose HTML nests 4,000
  `<div>` exited 0, and 8,000 (88,184 bytes) to 20,000 exited 139 (SIGSEGV) with nothing on
  stdout or stderr, as did 100,000 unclosed `<font>` tags; a three-message mbox with the deep
  message second wrote 0 lines, the first message's still in the buffer. Time was quadratic
  before the crash, 1.2 s at 20,000 levels and over 100 s at 200,000. By reading, the mapping ran
  outside the per-record guard in all four readers, and a `.pst`'s faulting message wrote no id
  for `--after` to step past. HTML nesting is capped at 512 open elements, Blink's cap, by a pass
  over the tags before htmd; past it the Markdown is made from the text body, else from the
  HTML's text, with a warning (`layers.md`, "docproc-mail"). The per-record guard wraps the read,
  the mapping, and the derivation, so a fault there is that record's `skipped` line, and stdout
  is flushed after each message line.
- **ML-2 · `smtp_address` from `PidTagSmtpAddress` never reached a sender.** Accepted → mail, with
  the fallback in "docproc's packages [library]". 0x39FE is a recipient-row property: a scan of
  the committed `.msg` fixtures found it only in recipient storages (18 of 18 in POI's
  `outlook_30_msg.msg`), and no fixture holds the sender's, `PidTagSenderSmtpAddress` (0x5D01),
  though seven fixtures have 11 Exchange senders. A recipient's `smtp_address` is read from
  0x39FE and the sender's from 0x5D01, left out when absent, and docproc's typed metadata then
  takes the sender's from `transport_headers`' From (`layers.md`, "docproc-mail"). A unit test
  over a built `.msg` holding 0x5D01 shows the sender's; the goldens show the recipients'.
- **ML-3 · The content rule's Unicode 16.0 couldn't be both schema "2" and current.** Settled →
  mail. 16.0 was the measuring tool's (uniseg 0.10.1, a Go library); current Rust crates carry
  18.0 (finl_unicode 1.5.1, unicode-ident 1.0.27), and the spike holds none of the crates the
  rule needs, so "the Rust crates stay" understated the task. Schema "2"'s rule takes the
  Unicode version its pinned crates carry, so a currency bump that moves it is a reviewed change
  (`layers.md`, "docproc-mail"); the payload measurement is rerun on that version, and the change
  list's Toolchain names each crate the rule adds. onenote's OCR text
  follows the rule through the shared crate, so the version binds it too. Rejected: the shared
  crate owning tables generated from UCD 16.0.
- **ML-4 · A folder cycle in a .pst recursed without bound.** Accepted → mail. By reading: the
  walk and the `--after` pre-walk recurse into every node id a hierarchy table lists, with no
  set of folders entered and no depth cap, so a row naming an ancestor repeats the output until
  the stack overflows. The walk keeps the folder node ids it has entered; a row naming one is a
  `skipped` line of kind `folder`, reason "already read", with a warning, shown by a unit test
  over a libyal store with one hierarchy row rewritten.
- **ML-5 · A message whose only body was plain RTF had no Markdown.** Accepted → mail. The spike
  made Markdown from the stored HTML, else the HTML recovered from a `\fromhtml1` RTF, else the
  text body, so a plain-RTF-only message got none, and schema "2" cuts `body.rtf`, its text's
  only other carrier; no fixture shows it (10 of 46 messages hold RTF, all with a text body). The
  order is stated in full: stored HTML, HTML from the RTF, the text body, then the RTF's text
  (`layers.md`, "docproc-mail"). The RTF text reader is the existing lexer and group stack with a
  second sink, not a second walker; a built-message unit test covers a plain-RTF-only and a
  `\fromtext` message.
- **ML-6 · Linking inline images by `file` was more than a rename.** Accepted → mail. Run over a
  built `.eml`: the Markdown kept `cid:One%2Bimg@example.com` beside a `content_id` of
  `One+img@example.com`; an image referenced by Content-Location stayed `![b](pic2.png)`; an
  unmatched `cid:` stayed as written; a 300 KB `data:` image made a 400,038-byte Markdown with no
  media file. Every reader made the Markdown before reading the attachments. Attachments are read
  first, a nested message's too; a `cid:` is percent-decoded and matched to `content_id`, another
  source to the Content-Location (0x3713 in MAPI, read for matching only, no new field); a
  `data:` image is written as media and linked, with no attachment record; a reference that
  matches nothing is kept as written, with a warning. `inline` has one meaning in every format,
  "the Markdown links it", where the spike's was `ATT_MHTML_REF` in MAPI and Content-Disposition
  in MIME, so one message differed as `.msg` and `.eml` (`layers.md`, "docproc-mail"). Rejected:
  each format keeping its own meaning of `inline`.
- **ML-7 · A warning's `<where>` carried folder names.** Accepted → mail. A `.pst` place joined
  the folders' display names with `/`, so a folder named `Project: Alpha` breaks the warning's
  split at `": "`, and a name with a line break makes two lines. A place names the folder by node
  id, `folder 32802, message 2097188, attachment 5`, and docproc joins the id to the path the
  folder's line gave it.
- **ML-8 · The goldens proved the debug build, not the binary that ships.** Accepted → mail and
  onenote, onenote's in "docproc-onenote [onenote]". `cargo test` ran the debug binary and the
  check ran only `--version` on the release musl one; tests/pst.rs notes that its corrupted store
  panics in debug and reads differently in release, where arithmetic in outlook-pst, cfb, and
  compressed-rtf on corrupt lengths wraps silently; the 92-crate graph compiled twice. The
  release profile sets `overflow-checks = true`, and the check runs
  `cargo test --release --locked --target <musl>`, so the goldens run against the release
  artifact and the debug build leaves the check.
- **ML-9 · The shared crate holds the envelope both Rust tools duplicated.** Accepted → mail,
  which writes the crate, and onenote, which adopts it ("docproc-onenote [onenote]"). `jsonl.rs`
  was identical byte for byte; the guard, the fault, the panic hook, the warning, and the
  argument loop differed in names; the media name rule and the escape were written twice. The
  copies had drifted: mail's usage error lacked `error:`, mail's `--version > /dev/full` exited 4
  where onenote's exits 1, and mail created the media directory on every run where onenote
  creates it before its first file. mail also escaped a text body by running html5ever and htmd
  over built HTML and patching what htmd left. The untagged path crate under `tools/` holds the
  whole Rust envelope: the fault type with its exit codes, the error and warning lines, the lines
  writer, the record guard, the media store (name rule, write-once, one directory rule), the
  literal-text escape, and the content rule; each tool keeps its flags, readers, and model, and
  the three drifts settle to the envelope's wording.
- **ML-10 · codepage-strings goes.** Accepted → mail. A 2021 wrapper over `codepage` and
  encoding_rs, both already direct dependencies, pinned exactly for a US-ASCII workaround, it
  alone brings 11 of the lock's 92 crates, and its BOM-sniffing decode reads a PT_STRING8 that
  starts FF FE as UTF-16 whatever its code page. `decode` becomes about 30 lines over
  `codepage::to_encoding` and `decode_without_bom_handling`, an unknown code page decoded lossily
  as 1252 with the existing note; a DOS code page the review of ~/docs finds becomes a table in
  the tool.
- **ML-11 · The build, release, and CI files assumed the crate was the repository.** Accepted →
  mail, in the shape round 1 settled (OX-10). Nested at `tools/mail`, the release trigger, the
  asset name, the version check, the changelog path, the check's and currency's paths, and the
  tool's name as a literal all break. Neither the spike nor docproc had a ruleset, so "CI checks
  gate the merge" was habit, and a required per-tool check that a path filter skips would block
  every other pull request. mail's part is its files under `tools/mail`, its own `mise.toml`
  tasks resolving paths from their own directory, the tool's name from `CARGO_PKG_NAME`, the
  asset `docproc-mail-v<version>-linux-<arch>.tar.gz` from the root workflow, and a matrix row.
  Settled in this round: docproc's CI gains one always-running final `ci` job that a GitHub
  ruleset on docproc requires, so a path-skipped job never blocks a merge, built by ooxml with
  the CI ("docproc-ooxml [ooxml]"); rulesets for other repositories are out of scope. Rejected:
  one release workflow per tool.
- **ML-12 · MimeKit 5.0.0 would sit under two packages built for 4.x.** Settled → mail. The
  generator's direct MimeKit reference lifts what MsgKit 3.0.6 (>= 4.18.1) and PstBuilder.Eml
  1.3.0 (>= 4.17.0) resolve to, past a Pkcs advisory; both were compiled against MimeKit 4, so a
  major version under them can fail at run time, and since neither format is byte-stable a
  regeneration rewrites all ten generated `.pst` and `.msg` files, the manifest's hashes, and the
  `.msg` goldens' times. The generator also stayed on .NET 10 beside ooxml's 11. MimeKit holds
  at the latest 4.x, a recorded hold until MsgKit or PstBuilder.Eml releases on MimeKit 5; the
  generator moves to .NET 11 beside ooxml and regenerates once, with the schema "2" re-record.
  Rejected: MimeKit 5.0.0; this reverses the plan's decision.
- **ML-13 · Two kinds of content never reach the contract.** Accepted: the limits in `layers.md`,
  "docproc-mail", and the backlog entries in the roadmap's mail backlog. A `.pst`'s embedded
  messages are listed from their table row with no content, since outlook-pst 1.2.0 can't open
  them (three in the goldens), and class-specific fields are unread (two `IPM.Contact`, both with
  no body, and two `IPM.Appointment` in the goldens). The contract states both: a `.pst`
  attachment of method `embedded_message` has no `message` and no `file`, and no class-specific
  field is written. The backlog carries the first with its upstream condition, outlook-pst's
  unmerged fix; filling either is an addition under "2".
- **ML-14 · The spike's evidence machinery stays in the spike.** Accepted → mail. About 2,200
  lines, the 1,435-line scorer that every check compiled and never ran, the timing wrapper, and
  the generator's scale mode, score evidence 16 and time the 2 GB scale store; none tests the
  contract. `tools/mail` takes `src/`, `tests/`, and the generator's `fixtures` mode; the rest
  stays in the spike at v0.3.1, and the README's cost figures cite its finding.
- **ML-15 · Named properties and multi-valued values were read for no field.** Accepted → mail.
  About 110 lines behind `#[allow(dead_code)]` parse `.msg` named properties, whose faults become
  warnings about data nothing reads, and `Value::Multiple` is built by both readers and read by
  none. Both go, a multi-valued property becoming `Value::Other`; named properties return with
  the first field that reads one.
- **ML-16 · The mbox reader would have had no golden.** Accepted → mail. The change list added
  `.eml` goldens for `generated/eml/` only, leaving the MIME group's `.eml`, the mboxrd, and
  `mbox.rlug` truth-scored, so an mbox's ordinals, `skipped` lines, and warning places were
  pinned by nothing; the generated goldens didn't assert the exit code; the golden loop was
  written three times; no fixture is an ANSI `.pst`. One golden helper in tests/common (run,
  exit 0, `.jsonl`, `.stderr`, `UPDATE_GOLDEN`) covers every committed input, the MIME group
  included. An ANSI store joins the fixtures if a freely licensed one is found; until then the
  README's limit stands and the backlog carries it.
- **ML-17 · Inputs were refused or passed through without a word.** Accepted → mail. An `.eml`
  that starts with a UTF-8 byte-order mark was refused, exit 3; any text starting `From ` was an
  mbox, one `skipped` message and exit 0; by reading, TNEF (`winmail.dat`) and opaque S/MIME
  parts became attachments, their content unread and the Markdown empty or partial with no
  warning. A leading byte-order mark is skipped before the header check; an mbox whose every
  entry fails it is refused, the `store` line held until the first message parses; TNEF and
  opaque S/MIME are a warning on the message and a README limit, and reading them is backlog.
- **ML-18 · A compressed RTF's stated size was trusted for an allocation.** Accepted → mail. By
  reading, compressed-rtf 1.0.1 allocates the header's raw size, up to 4 GiB, after a CRC a
  crafted body can satisfy, and a failed allocation aborts the process, unseen by
  `catch_unwind`. A two-byte token yields at most 17 bytes, so a raw size above nine times the
  compressed length is refused before decompressing, a note on the message.
- **ML-19 · Peak memory followed the largest attachment, unstated.** Accepted → mail. Every
  attachment is held whole, a `.pst`'s twice; the spike measured 65 MiB on its scale store, whose
  largest attachment is 16 MiB. The `.pst` copy goes, and the README states the bound, a small
  multiple of the largest single attachment, or of the file for an `.eml`, from which Cerebro's
  `tool-packaging` sizes replicas.
- **ML-20 · The release archive carried no notices for what it links.** Accepted → mail and
  onenote, onenote's in "docproc-onenote [onenote]". The archive held the binary and `LICENSE`;
  the 92 locked crates are all permissive, but 18 are MIT alone, encoding_rs adds BSD-3-Clause,
  and one carries Unicode-3.0, each asking that its notice travel with a binary, and Cerebro's
  image takes the archive as its unit. Release packing writes a third-party notices file from
  the locked graph into the archive, and the check fails on a license outside the permissive
  set.

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
  doesn't list (`interop/SUT`'s `FileSyncWOPI_OneNoteWithODB.one`). docproc always passes a
  file-only option, `--file-only`, under which a table of contents is refused, exit 3, with a
  fixed reason, before any section is read; docproc's onenote stage records that refusal as the
  part's outcome, not a failure. Notebook and directory reads stay for an operator; the notebook
  read warns when it read no entry, and the README lists unlisted sections as a limit. Rejected:
  the tool reading notebooks for docproc.
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
  the 285 MB of large sections the spike measured, holds either. Only after a section's parse
  fails does the tool scan for the two constants; found, it refuses with a reason beginning
  `encrypted`, and a `skipped` line gains an optional `encrypted: true`, an addition under schema
  "2". A false match relabels a read that already failed, never costing content. docproc maps
  the reason to `ErrEncrypted`, as office's stage does. The scan is the tool's own tested code;
  the OneDrive packaging format, with no encrypted fixture, stays unestablished, and decryption
  keeps waiting on a specification. Rejected: leaving encrypted sections reported failed.
- **ON-5 · The parser hid absent times and titles.** Accepted → onenote. By reading:
  `created_time()` is never absent, an unreadable FILETIME becoming `1970-01-01T00:00:00Z` with a
  parser warning; `updated_time()` returns the creation time when no modification time is
  stored, behind a private option; and an untitled page's title is the first outline's first
  paragraph. `creation_time` is left out when it is the parser's 1970 fallback, with a warning;
  `layers.md` and the README state that a page with no stored modification time reports its
  creation time, and that an untitled page's `title` is its first line, as OneNote's page list
  shows it.
- **ON-6 · The binary the tests proved was not the binary that ships.** Accepted → onenote,
  settled with mail as ML-8, whose onenote bullet carries it; the spike's test
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
  destination writer, in the shared crate, writes a destination holding a space, a control
  character, `<`, or `>` as `<…>` with `<` and `>` escaped, else bare with its parentheses
  escaped; in a table cell `|` is written `\|` in code spans and LaTeX.
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
  which adopts the crate mail writes (ML-9). `jsonl.rs` is byte-identical in both, the media name
  rule and once-only writer, the faults, and the escape parallel, with one drift: mail created
  the media directory once the input was known to be read, onenote at its first file, so
  `--media` over `interop/SUT/OneWithoutFileData.one` left no directory. This settles ML-9's open
  "one directory rule": the directory is created once the input is read. onenote compares two
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
  `architecture` task, which the session records.
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
- **onenote_parser's MPL-2.0.** Settled → onenote. The parser is MPL-2.0, outside ML-20's
  permissive set. The Rust tools' license check allows MPL-2.0 by name for docproc-onenote only,
  and its notices state where onenote_parser's source is. Rejected: treating the license as a
  blocker.
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
set out, and every decision as recommended; Q1 settled the findings, Q2 to Q16 decisions 1 to
15, Q17 decision 19, Q18 decision 17, Q19 decision 18, Q20 decision 20, and Q1 took decision 16
with PX-5. The changes are in `layers.md`, "docproc's packages [library]", unless an entry names
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
  goal reads the constants.
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
accepted every finding, DP-3 and DP-10 in the form it settled. An entry is carried by docproc
unless it names another task; the command's changes are in `layers.md`, "The nested modules",
and `rundir`'s in the `rundir` row of "docproc's packages [library]", with its API under "The
nested modules".

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
  nor had `cmd/docinfer`, under round 1's "`docproc` releases nothing". The `docproc` task tags
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
  extension a browser opens; a content named by its SHA-256 has none, so a linked text
  downloads, and a page that names a content's file fixes `FileContent`'s layout for every reader
  that isn't Go. The layout is part of schema "2" and `rundir` exports a content's path; the
  report inlines text, Markdown, and records from the content store, points `<img>` and
  `<audio>` at the file, and renders over the reader's handle.
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
  layout is a pending edit for the `architecture` task.
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
  docproc: the manifest's shapes stay `rundir/schema`'s,
  filled by `run` from `stages.Set`, and `stages`' carry no JSON names (`layers.md`, "The nested
  modules"). Rejected: `schema` aliasing them, which ties schema "2" to an unversioned package
  and has every reader of the run directory, `cmd/docinfer` among them, compile `stages` and the
  format packages for its shapes.
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

- **The tools.** The root workflow ooxml builds releases each `tools/*/v*` tag: it derives the
  tool's directory, changelog, and version from the tag and publishes
  `docproc-<format>-v<version>-<rid>.tar.gz` for linux-x64 and linux-arm64, built on native
  runners, each archive holding the binary, the repository's LICENSE, and the tool's
  third-party notices, with `SHA256SUMS` over the archives. ooxml's release job checks its glibc
  floor. mail and onenote add a matrix row each.
- **The library.** `library` tags the base module, `storage`, and `postgres`, through the Go
  module releases it adds to docproc's CI.
- **docproc.** The `docproc` task tags the base module v0.2.0 when `rundir` lands (DP-3), and a
  committed `go.work` over the base module and `cmd/docproc` carries changes between tags.
- **reports.** `reports` tags the base module again only if it changed `rundir`, then
  `cmd/docproc/v0.1.0`.
- **Untagged.** The shared Rust crate under `tools/` is a path dependency of mail and onenote and
  is never tagged; a change to it runs both tools' checks.
