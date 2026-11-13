# design

The design review: Fable 5.2 evaluated each of the five artifacts v1.ai.processing promotes, and
the architect settled every finding in one review round per artifact, 2026-11-12. `layers.md`
holds the boundaries and the change lists; this note holds each finding's settled design and its
disposition, with the evidence that justified it, since the evaluations themselves are not kept.
An entry names the task that carries it, and the `layers.md` section that states it where one
does; it doesn't restate a signature that section holds.

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
  rule needs, so "the Rust crates stay" understated the task. Schema "2"'s rule uses the Unicode
  version its pinned crates carry, 18.0 today, asserted by a unit test, so a currency bump that
  moves it fails the check and becomes a reviewed change: the goldens re-recorded, the CHANGELOG
  naming the version. The payload measurement is rerun on that version, and the change list's
  Toolchain names each crate the rule adds (`layers.md`, "docproc-mail"). onenote's OCR text
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

The remaining three artifacts' reviews follow in their own sections.
