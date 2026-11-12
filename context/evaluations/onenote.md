# Evaluation · docproc-onenote (spike-ai-pipeline-onenote v0.1.0)

The spike is sound to promote: it is small, its envelope already matches `layers.md` (exit codes,
stderr lines, media names, absence), its fixtures are pinned and licensed, and its build is the
static musl shape the tools release in. Its weaknesses are at its edges rather than in its core.
The three most consequential findings: recognised ink words leave the contract whenever a page's
handwriting isn't page-level ink, because schema "2" cuts `ink_text` and the Markdown carries the
words on the page's ink image alone (ON-1); a `.onetoc2` reaching the tool through docproc reads
empty from a stream and duplicates every sibling section from a path, and a notebook directory
can't reach it at all (ON-2); and the hand-written expectations `layers.md` says "stay" all point
into the fields schema "2" cuts, so the tool's only truth has to be re-seated before the goldens
replace it (ON-3). An encryption signal is feasible for desktop-format sections today (ON-4).

Evidence was read from the spike at `866ffc6` and from onenote_parser 2.1.0's source in the Cargo
registry; behaviour was checked with the spike's own v0.1.0 musl release binary over its
committed fixtures. Paths are relative to the spike's root; `parser:` paths are relative to
`onenote_parser-2.1.0/src/`, and `processing:` paths to spike-ai-pipeline-processing.

## Findings

### ON-1 · Recognised ink words reach the Markdown only through the page's ink image
- **Kind:** issue
- **Weight:** high
- **Where:** src/markdown.rs:49-61, 95-103, 186, 209-211; src/content.rs:95, 143-146;
  src/section.rs:119-141
- **Against:** layers.md, "docproc-onenote" (`ink_alternatives`: "the words they belong to are the
  ink image's alt text in the Markdown"); "Consumers of structured text" (every piece of text
  exists once)
- **Finding:** `recognised(page)` joins every recognised word of the page, and only the page's
  own ink drawing takes it as alt text (`ink_md(ink, &recognised(page))`). A paragraph's
  handwriting and an ink block are written `ink_md(ink, "")`, so `![ink](file.svg)` with no
  words. The page's ink exists only when the page has ink containers with points
  (`reader.drawing(&page_strokes)`). So a page whose recognised handwriting sits in outlines, or
  whose ink containers hold no points, has `ink_text` in schema "1" and nothing of those words in
  its Markdown; once schema "2" cuts `ink_text`, the words are in no field. The contract also
  assumes one ink image per page: `ink_alternatives[].word` indexes "the page's recognized ink
  words", but a page with page ink and paragraph ink has several ink images and one word list.
  The one ink fixture, `onenote-rs/handwriting_recognition.one`, has page-level ink only, so no
  test shows the gap. The parser gives what closes it: `InkStroke::recognized_word()`
  (parser: onenote/ink.rs:162) links each stroke to its word.
- **Proposed design:** Each drawing's alt text is the words its own strokes belong to, in the
  recognition's order (`![ink: <words>](file.svg)` for page ink, paragraph ink, and ink blocks
  alike). `ink_alternatives[].word` stays the page-wide index over the recognition's lines. A
  recognised word no drawing carries is written once, as a plain line after the page's content,
  so no word is ever outside the Markdown. Unit tests over a built page show a page with paragraph
  ink only and one with both; layers.md's `ink_alternatives` sentence says "the ink images' alt
  texts".
- **Carried by:** onenote

### ON-2 · A table of contents through docproc reads empty or twice, and a directory never arrives
- **Kind:** issue
- **Weight:** high
- **Where:** src/section.rs:24-36; src/notebook.rs:6-16, 43-60; src/sniff.rs:5-7, 59-64;
  README.md:328 ("Known limits")
- **Against:** layers.md, "Input from a stream, or a path" (one file per input, spooled) and
  "Payload measurement" ("sections only: notebooks would count their sections twice");
  validation-first (the failure names the cause)
- **Finding:** docproc's `Input` is one file or one stream, and processing routes any OneStore
  signature to the onenote stage (processing: route/route.go:141). The tool reads a `.onetoc2`
  as its whole notebook, resolving sections beside it. Spooled from a stream, as Cerebro always
  passes input, nothing is beside it: the parser filters entries whose file is missing without a
  word (parser: onenote/mod.rs:151-153), so the tool writes one `notebook` line and exits 0 with
  no warning. Given by path, as `cmd/docproc` walks a share, it reads every section, and each
  sibling `.one` is also its own input, so every page is extracted twice under two parts. The
  header can't tell a section from a table of contents (sniff.rs:5-7), so docproc's route can't
  avoid the run. A notebook directory, which the roadmap summary names, isn't a file and can't be
  an `Input`: only an operator running the tool by hand reaches that mode. Two smaller gaps sit
  beside it: a directory read leaves out any `.one` its table of contents doesn't list, silently
  (`interop/SUT` holds `FileSyncWOPI_OneNoteWithODB.one`, which the read of that directory never
  names), and the README's known limits state the missing-file case but not this one.
- **Proposed design:** The tool gains one option docproc always passes for a file part, such as
  `--file-only`: a table of contents is then refused, exit 3, with a fixed reason ("a table of
  contents; its sections are read on their own") in place of `notebook::read`. `parse_section`
  already stops at the store type before reading any section, so the refusal is cheap. docproc's
  onenote stage records that refusal as the part's outcome, not a failure. Sections and `.onepkg`
  are the inputs docproc reads; notebook, directory, and `.onetoc2` reads stay the operator's.
  The notebook read warns at `/` when it read no entry at all, and the README lists unlisted
  sections among the limits.
- **Carried by:** onenote (the option, the warning, the README); library (the stage's outcome)

### ON-3 · The expectations layers.md keeps all point into the fields schema "2" cuts
- **Kind:** issue
- **Weight:** high
- **Where:** tests/fixtures/content.json (every pointer); tests/fixtures/sections.json (`content`
  kind counts); tests/content.rs:326; tests/sections.rs:122; tests/media.rs:31; tests/ink.rs:52;
  bench/findings.rs
- **Against:** layers.md, "docproc-onenote [onenote]" → Goldens ("The JSON-pointer expectations in
  `tests/fixtures/*.json` stay as the readable checks"); system-tools.md ("Golden outputs prove a
  contract")
- **Finding:** Every pointer in `content.json` starts `/content`, `/ink`, or `/ink_text`, the
  three fields the change list cuts. `sections.json` expects each page's containers counted by
  kind, and `tests/content.rs` (346 lines), `tests/media.rs`'s `records`, and `tests/ink.rs`'s
  container checks walk the same tree in the binary's output. None of it can run against schema
  "2". These files are the tool's only hand truth (README, "Known limits": no authored truth), and
  a golden records what the tool wrote, not what is right, so dropping them leaves the goldens
  proving themselves. The change list also omits three things goldens need here: a refusal's
  stderr leads with the input's path as given (src/main.rs:78, src/sniff.rs:60), so a `.stderr`
  golden holds whatever path the test passed; parser-worded reasons and panic texts in `.stderr`
  pin upstream's message text and the build profile (ON-6); and no `.onepkg` is committed, since
  the tests build packages in memory (tests/notebooks.rs:119).
- **Proposed design:** Re-seat the truth on what schema "2" carries. `content.json` becomes
  pointers into `media` and `ink_alternatives`; `sections.json` drops the kind counts and keeps
  ids, levels, titles, times, and refusals; the run, list, table, and tag truth moves to
  hand-written Markdown expectations, as `tests/markdown.rs:36` already holds for the test page,
  and to the unit tests `src/content.rs` already has over built runs. Goldens run with the
  fixtures' directory as the working directory and relative inputs, so a `.stderr` is the same on
  every machine. A package has no golden of its own: its read must equal its directory's golden
  in every line but the first, as today's test asserts.
- **Carried by:** onenote

### ON-4 · An encryption signal is feasible for desktop-format sections
- **Kind:** issue
- **Weight:** medium
- **Where:** src/section.rs:24-36; src/notebook.rs:131-148; tests/cli.rs:169; README.md:328
- **Against:** roadmap.toml, the library task's backlog ("`Encryption.one` … reported failed
  rather than encrypted"); validation-first (the failure names the cause)
- **Finding:** `Encryption.one` is refused with the parser's `Unexpected end of file`, and inside
  a notebook skipped with the same reason, so docproc can only call it failed. The signal exists
  in the file without decrypting anything. [MS-ONESTORE] 2.5.19's encryption-key structure opens
  with the fixed 8 bytes `0xFB6BA385DAD1A067` and closes with `0x2649294F8E198B3C`.
  `Encryption.one` (desktop format) holds each three times (first at offsets 5608 and 6687); none
  of the other 22 committed OneStore files holds either, nor do the 285 MB of large sections under
  `.state/findings/large/`. The parser already parses the node that points there (`0x07C`,
  parser: onestore/desktop/file_node/mod.rs:214) and discards it. Not established: the
  OneDrive packaging format, which has an encryption-key revision role
  (parser: onestore/fsshttpb/revision_role.rs:13) and no encrypted fixture.
- **Proposed design:** Only when a section's parse fails, the tool looks for the 2.5.19 header
  and footer constants in the file. Found, the reason is fixed text beginning `encrypted` (as
  office's refusal reads, processing: internal/cli/cli_test.go:50) and a `skipped` line gains an
  optional `encrypted: true`, an addition under schema "2". Exit code 3 stands; the envelope's
  codes are settled. docproc's onenote reader maps that reason to `ErrEncrypted`, as
  `dispatch/office.go:143` does. It relabels a read that already failed, so a false match costs a
  label, never content. Ask upstream for an error kind raised from node `0x07C` and the FSSHTTPB
  role, which would replace the scan and cover OneDrive-format sections. Decryption itself keeps
  waiting on a specification, as the roadmap says.
- **Carried by:** onenote (the signal); library (the mapping)

### ON-5 · The parser hides absent times and titles, so "left out when absent" can't be built as written
- **Kind:** issue
- **Weight:** medium
- **Where:** src/section.rs:104-106; src/model.rs:89-90
- **Against:** layers.md, "docproc-onenote [onenote]" → Envelope ("`creation_time` and
  `modification_time` become optional, left out when absent"); data-intelligence.md, `metadata`
  (authoritative metadata capture)
- **Finding:** The parser's API has no absent time. `created_time()` is never optional: an
  unreadable FILETIME becomes `1970-01-01T00:00:00Z` with a warning (parser:
  onenote/page.rs:210-216). `updated_time()` returns the creation time when the page stores no
  modification time (parser: onenote/page.rs:56-58), and the option behind it is private. So the
  tool would write 1970 as a page's creation time, and can't tell a never-modified page from one
  modified at its creation second. `title` has the same shape: for a page with no title area the
  parser gives the first outline's first paragraph (parser: onenote/page.rs:222-228), so a page
  line can carry a `title` its Markdown has no `#` heading for. Cerebro's `metadata` task reads
  all three as stored facts.
- **Proposed design:** `creation_time` is left out when it is the parser's 1970 fallback, the
  warning line saying why. `modification_time` is written as the parser gives it, and layers.md
  and the README state that a page with no stored modification time reports its creation time;
  ask upstream for the option. The README states that an untitled page's `title` is its first
  line, as OneNote's page list shows it.
- **Carried by:** onenote

### ON-6 · The binary the tests prove is not the binary that ships
- **Kind:** issue
- **Weight:** medium
- **Where:** Cargo.toml (no `[profile.release]`); mise.toml:16-19; tests/cli.rs:176-191;
  src/media.rs:122
- **Against:** layers.md, "Common envelope" → Determinism; release-and-ci.md ("One check per
  repository", read-only and deterministic)
- **Finding:** `cargo test --locked` builds and runs the debug binary; the check then builds the
  musl release binary and runs only `--version`. The two differ on malformed input, by the
  tests' own account: `a_parser_panic_is_refused_with_exit_3` relies on debug overflow checks,
  and "a release build reads a different error there". So that test can't pass against the
  shipped binary, wrapped arithmetic in the parser runs on in release where debug stops, and a
  `.stderr` golden recorded from one profile won't match the other. Only the findings run, on
  the laptop, exercises the release binary. Separately, the unit test at src/media.rs:122 writes
  under the system temporary directory, though the check's description says it writes only
  `target/`.
- **Proposed design:** `[profile.release]` sets `overflow-checks = true`, so the parser stops in
  release where it stops in debug; the cost is small beside parsing. The check runs the golden
  and CLI tests against the musl release binary it just built (`cargo test --release --target
  <musl>`), on both CI runners, so the artifact proven is the artifact packed. The media unit
  test moves to an integration test under `CARGO_TARGET_TMPDIR`.
- **Carried by:** onenote

### ON-7 · Memory is unbounded in four places, and one of them is the tool's own
- **Kind:** issue
- **Weight:** medium
- **Where:** src/content.rs:349-369; src/media.rs:35-61; src/notebook.rs:43-45, 68-81;
  README.md:328
- **Against:** roadmap.toml, the library task's backlog (the OneNote CLI's memory is unbounded);
  layers.md, "Common envelope" → Media
- **Finding:** (1) The tool's own: `Reader::file` reads each image and embedded file whole
  (`read_to_end`), then hashes, then `fs::write`s it, though the parser hands back a streaming
  `Read` over a file it reads on demand (parser: onestore/shared/file_blob.rs:90-101). A recording
  of several gigabytes is held whole, with or without `--media`. `fs::write` to the final name is
  also not atomic: a run killed mid-write (docproc sends SIGTERM, then SIGKILL) leaves a
  truncated file named by the full content's hash. (2) A notebook is parsed whole before its
  first line is written (`parse_notebook`), so its peak is the sum of its sections. (3) A
  `.onepkg` is read whole, decompressed whole in memory by the parser (parser:
  onenote/mod.rs:255-261), and its cabinet opened a second time by the tool. (4) Issue #48, which
  the README states. The README's limits name (4) only.
- **Proposed design:** Media are streamed: hashed while copied through a fixed buffer into a
  temporary file in the media directory, then renamed to the hash name; without `--media`, hashed
  only. That bounds the tool's own memory by the buffer and makes every named file whole. The
  README states (2) and (3). A bound on the whole run stays docproc's, in the `tool` package, as
  the backlog has it; when it lands, an allocation failure aborts the process by signal, outside
  exit codes 1 to 4, which docproc's `internal/cli` must report as failed.
- **Carried by:** onenote (streaming, README); library (the run's bound)

### ON-8 · Some Markdown the tool writes doesn't parse as what it means
- **Kind:** issue
- **Weight:** medium
- **Where:** src/markdown.rs:353-364, 389-398, 492-494, 506-518, 617-618
- **Against:** layers.md, "Common envelope" → Text layer ("escaped by htmd's rules, which mail's
  and onenote's Markdown share"); "docproc-mail", the content rule ("a link as its text followed
  by its destination as one URL token")
- **Finding:** `destination` escapes parentheses only, "as htmd writes it". htmd also wraps a
  destination holding a space in `<…>` (htmd 0.5.6, element_handler/anchor.rs:105-118), since
  CommonMark ends a bare destination at a space. A OneNote link to a file path or a share
  (`\\server\Team Notes\plan.docx`) is therefore written as text that isn't a link, and the
  content rule and any reader see no URL. The same function will write the image `hyperlink`
  schema "2" adds. In a table cell, a code paragraph is written as a code span of its stored
  text (line 618) and an equation as its bare LaTeX (lines 353-364); neither escapes `|`, which
  GFM reads as a cell boundary even inside a code span, so `a | b` in code or `|x|` in math
  shifts the row's cells. No fixture holds either case.
- **Proposed design:** One destination writer, in the crate mail and onenote share: a
  destination holding a space, a control character, `<`, or `>` is written `<…>` with `<` and
  `>` escaped, else bare with its parentheses escaped. In a table cell, `|` is written `\|` in
  code spans and LaTeX. Unit tests over built pages show a spaced link, a hyperlinked image with
  a spaced target, and a cell holding each.
- **Carried by:** onenote; mail (the shared writer)

### ON-9 · Five workarounds stand on onenote_parser's private behaviour, under a caret requirement
- **Kind:** issue
- **Weight:** medium
- **Where:** Cargo.toml:14, 20, 24; src/section.rs:160-164; src/notebook.rs:36-39, 68-93,
  187-250; src/stdout.rs:25-33; src/guard.rs
- **Against:** system-tools.md ("Tools are chosen for support"); the spike's STANDARDS.md ("A
  dependency joins only when it is actively maintained"); minimal-footprint; rolling-currency
- **Finding:** The parser is current and maintained (v2.1.0 released 2026-10-19, MPL-2.0, three
  releases in 2026), by one person (240 of about 250 contributions). The tool works around five
  of its gaps: (a) the error kind is private, so a table of contents is told by the message
  prefix `"Not a section file: "` and a failed section by `"failed to import section "`; (b)
  one `println!` on a malformed node (parser: onestore/desktop/file_node/mod.rs:284) costs the
  `libc` dependency, an `unsafe` `dup2`, and `stdout.rs`; (c) the recycle bin is dropped
  silently, so the tool takes `cab` directly, opens the cabinet a second time, and mirrors the
  parser's private name normalisation and its choice of `.onetoc2` (`components`,
  `package_toc_dir`, `holds_recycle_bin`, about 60 lines that must track upstream); (d) a section
  that fails inside a section group vanishes, since the group's report is discarded (parser:
  onenote/mod.rs:263-281); (e) a parser panic in one section refuses the whole notebook, since
  only the whole `parse_notebook` can be guarded. The requirement is `"2.1.0"`, a caret, though
  (a) and (c) depend on this release's exact text and logic; mail pins such crates exactly
  (`outlook-pst = "=1.2.0"`). `cab` 0.6.0, taken directly, was last released 2024-04-05.
- **Proposed design:** Pin `onenote_parser = "=2.1.0"`, with the comment mail's pins carry.
  File upstream asks, each of which deletes code here: a public error kind (with ON-4's
  encryption kind), the `println!` moved to `log`, notebook reports that name skipped entries
  (recycle bin, missing files, failed sections at any depth) as data, and the modification time
  as an option (ON-5). Until they land, keep (a) to (c) as they are, and add (d) and (e) to the
  README's limits; (e) is not listed today. `cab` stays a dev-dependency and a transitive one
  once (c) has an upstream answer. typed-path stays held, as settled.
- **Carried by:** onenote

### ON-10 · After the cut, the model is the renderer's private input: trim it, and collect `media` where it is stored
- **Kind:** simplification
- **Weight:** medium
- **Where:** src/model.rs:76-392; src/content.rs:35-99, 135-166, 284-345, 567-603;
  src/markdown.rs:19-63, 162-169, 314-323
- **Against:** none: simplification (it makes layers.md's "Cut" and "Added" items smaller than
  they read)
- **Finding:** `markdown::page` renders from `model::Page` (`content`, `ink`, `ink_text`), so
  cutting those fields removes their serialization, not their types. What nothing reads once
  they aren't written: a run's `underline`, `superscript`, `subscript`, `highlight`, and
  `font_color` (`marks` reads bold, italic, and strikethrough only); a list's `bullet`, `prefix`,
  `suffix`, and the numbering format's name (the renderer reads whether `number` is set and
  `restart`), with `number_format`; a tag's `completed_time`, and its `shape`, which is the
  `Debug` text of an upstream enum that the renderer then substring-matches for `"CheckBox"`; a
  word's `language`; an image's `printout_page`. The "walk that moves into the tool" need not be
  ported either. The reader already visits media in exactly the order processing's `Page.Media`
  walks them (processing: onenote/media.go:86-170): containers in stored order, an element's
  blocks before its children, a table's cells row by row, a paragraph's handwriting with its
  paragraph, the page's ink last (content.rs:95). `image`, `embedded_file`, and `drawing` are the
  three places a medium is stored.
- **Proposed design:** `model`'s page content becomes a crate-private intermediate with no
  `Serialize`, less the fields above; a tag carries `task: bool` decided where the shape is read.
  The reader appends one `media` entry at each of its three store sites, a record without data
  included (no `file`), and the page line serializes that list. The Markdown keeps its own order
  (containers sorted by offset, markdown.rs:20-27), which differs from `media`'s by design; the
  README says so.
- **Carried by:** onenote

### ON-11 · The shared crate should hold the envelope both Rust tools implement, and pin its Unicode data
- **Kind:** simplification
- **Weight:** medium
- **Where:** src/jsonl.rs; src/media.rs:35-81; src/main.rs:38-86, 110-114, 161-168; src/guard.rs;
  src/markdown.rs:451-494 (beside spike-ai-pipeline-mail `src/jsonl.rs`, `src/files.rs`,
  `src/mapping.rs:178-193`, `src/main.rs`, `src/markdown.rs:57-90`)
- **Against:** layers.md, "Common envelope"; "docproc-mail", the content rule ("written once, in
  one crate"; "Its Unicode version, 16.0, is part of schema \"2\"")
- **Finding:** The two tools already hold the envelope twice. `jsonl.rs` is byte-identical in
  both. The media name rule (SHA-256, an extension of 1 to 8 ASCII letters and digits) is
  `media::name` here and `mapping::file_name` there, and the once-only writer is `Media` here and
  `Files` there, with one divergence: mail creates the directory once the input is known to be
  read, "so a run that exits 0 always leaves it", and onenote creates it at the first file, so
  `--media <dir>` over a section with no media leaves no directory (checked with
  `interop/SUT/OneWithoutFileData.one`). `Fault`, the exit-code mapping, `warn`, and `panic_text`
  are parallel. The literal-text escape is htmd plus `escape_rest` in mail and a hand-written
  `escape` here. Two further points follow from the settled path dependency. Each tool has its
  own `Cargo.lock`, so the crate's Unicode segmentation dependency can resolve to different
  versions in the two tools, and "every tool appends the same sentences" would then rest on two
  locks agreeing. And onenote compares two plain texts (an image's `ocr_text` against its
  `alt_text`), so it needs the rule's tokenizer, alignment, and sentence units, not its CommonMark
  reader.
- **Proposed design:** The crate holds the envelope and the rule: the lines writer, the media
  store (name rule, write-once, directory created once the input is read, ON-7's streaming), the
  stderr line writers with the `<where>` rule (ON-14), the exit-code mapping, the destination
  writer (ON-8), and the content rule. The rule's entry point takes plain token sequences; the
  Markdown-to-plain reading is a separate function, behind a Cargo feature only mail enables, so
  onenote links no CommonMark parser. The crate requires its Unicode crates with `=` and a unit
  test asserts their Unicode version is 16.0, so both locks resolve alike. docproc's CI runs
  both tools' checks when the crate changes.
- **Carried by:** mail (it writes the crate); onenote (adopts it)

### ON-12 · The release and currency machinery assumes the tool is the repository
- **Kind:** issue
- **Weight:** medium
- **Where:** .github/workflows/release.yml:16-17, 48, 80; .github/workflows/ci.yml;
  scripts/release-pack.sh:30-37; scripts/currency.sh:11, 37; mise.toml; .gitattributes;
  THIRD-PARTY-NOTICES.md; tests/fixtures/
- **Against:** independent-releases; release-and-ci.md ("Releases", "What ships in a module
  zip"); roadmap.toml (the subtree kept out of the Go module zip); data-intelligence.md,
  `tool-packaging`
- **Finding:** Nested under `tools/onenote` and tagged `tools/onenote/v<version>`: the release
  trigger is `v*.*.*`; `release-pack.sh` compares the binary's version with `${tag#v}`, which
  for `tools/onenote/v0.1.0` is the whole tag, so every pack is refused; the archive name embeds
  the tag, which now holds slashes; the changelog step reads the root `CHANGELOG.md`.
  `currency.sh` reads `.github/workflows/*.yml` under `$MISE_PROJECT_ROOT`, which the tool's
  directory won't have, and under `set -o pipefail` the missing glob fails the script. docproc's
  root `mise run check` loops Go modules only, and the Rust check needs both musl targets on two
  runners, so it can't be the one `check` job. The scripts are near copies of mail's
  (`release-pack.sh` differs in the name and the notices file; `bench/rusage.c` says it is a copy
  of a copy), so three tools would carry three. release-and-ci.md says a module zip ships every
  committed file below its `go.mod`, with no supported exclusion; the 2.3 MB of fixtures, MPL-2.0
  samples among them, ship in docproc's zip unless a module boundary keeps `tools/` out. The
  archive carries `LICENSE` and `THIRD-PARTY-NOTICES.md` for the MPL-2.0 parser; Cerebro's image
  must carry them with the binary.
- **Proposed design:** One release workflow in docproc keyed by tag prefix: `tools/<tool>/v*`
  selects the tool's directory, changelog, and pack step; the version check strips through the
  last `/v`; assets are named `docproc-onenote-v<version>-linux-<arch>.tar.gz` with one
  `SHA256SUMS` per release. One pack script and one notices script under `tools/`, taking the
  tool's name. CI gains one job per tool, filtered to `tools/<tool>/**` and the shared crate's
  path, each running that directory's `mise run check` on x64 and arm64. Action pins are
  docproc's root currency's; the tool's currency reports its crates and mise tools only. A
  `go.mod` at `tools/` (as the templates' boundary at `template/`) keeps the subtree out of the
  library's zip. The nested `.gitattributes` keeps its fixture rules, which git reads relative
  to its own directory. `tool-packaging` copies each archive's licence files into the image.
- **Carried by:** docproc (the workflow, scripts, CI, module boundary); onenote (its directory's
  check); architecture (the module-zip exception on the page)

### ON-13 · The findings harness is spike evidence, not part of the tool
- **Kind:** simplification
- **Weight:** low
- **Where:** bench/findings.rs; bench/rusage.c; scripts/findings.sh; Cargo.toml:39-41;
  mise.toml (`findings`); docs/findings/
- **Against:** none: simplification
- **Finding:** `bench/findings.rs` (976 lines), `bench/rusage.c` (131, a C program the check
  never builds), and `scripts/findings.sh` (109) score the binary against `sections.json`,
  `notebooks.json`, and `content.json` and time it on three fetched sections. The check compiles
  the scorer on every run and never runs it. Its field scores are over the fields schema "2"
  cuts (ON-3), and the tests assert the same files, so a passing check already implies its
  result. Time and memory per input are what `layers.md`'s payload measurement and the goal's
  acceptance rerun measure for all three tools.
- **Proposed design:** docproc-onenote takes `src/`, `tests/`, and the release files, and leaves
  `bench/`, `scripts/findings.sh`, the `[[example]]`, the `findings` task, and `docs/findings/`
  in the spike, where the evidence stays cited. If a per-tool timing is wanted, it is one
  harness for all three tools, in the acceptance task.
- **Carried by:** onenote; acceptance (a shared timing, if any)

### ON-14 · The stderr line rules are documented, not enforced
- **Kind:** issue
- **Weight:** low
- **Where:** src/section.rs:64-69, 145-158; src/notebook.rs:146; src/main.rs:112-114
- **Against:** layers.md, "Common envelope" → stderr ("`<where>` never holds `\": \"`"; one line
  each)
- **Finding:** `place` escapes control characters in a section's or group's name and nothing
  else, so a name holding `": "` puts it in `<where>`, and a reader splitting at the first three
  separators misreads the line. OneNote's own names can't hold a colon, but a section's display
  name is stored in the file and a section input's name is its file's, so a crafted or renamed
  input can. `<what>` is passed through from the parser's warning messages unescaped, so a
  message holding a line break would write two lines.
- **Proposed design:** The shared warning writer (ON-11) owns both rules: in `<where>`, a colon
  followed by a space is written with the space escaped, and control characters are escaped in
  `<where>` and `<what>` alike. A unit test shows a name holding `": "` and a message holding a
  line break.
- **Carried by:** mail (the shared writer); onenote

### ON-15 · Byte identity has three edges that rest on hash or directory order in the parser
- **Kind:** issue
- **Weight:** low
- **Where:** src/notebook.rs:43-45, 68-69 (the calls into the parser)
- **Against:** layers.md, "Common envelope" → Determinism
- **Finding:** The tool's own code is deterministic: struct-ordered JSON, a stable sort with
  `total_cmp`, fixed-form SVG numbers, a `HashSet` used for membership only. Three parser paths
  are not. A section group's `.onetoc2` is the first that `read_dir` yields (parser:
  onenote/mod.rs:263-281): directory order on disk, and `HashMap` key order inside a package
  (parser: onepkg.rs:127), so a group directory holding two is read through either. A OneDrive
  package's storage index and manifest are `HashMap::values().next()` (parser:
  fsshttpb/data_element/mod.rs:117, 127). And object spaces are parsed in `HashMap` order (parser:
  onestore/fsshttpb/mod.rs:125), so a file with two faults reports either first, which a
  `.stderr` golden would catch only sometimes. Well-formed OneNote output doesn't reach any of
  them; no committed fixture does.
- **Proposed design:** State the three as limits beside the determinism promise, and ask
  upstream to sort or use ordered maps there. No code in the tool.
- **Carried by:** onenote

### ON-16 · Nesting depth is unbounded, and a stack overflow dies by signal
- **Kind:** issue
- **Weight:** low
- **Where:** src/content.rs:120-166, 262-281; src/markdown.rs:131-159, 577-605;
  src/math/parser.rs; src/main.rs:68
- **Against:** layers.md, "Common envelope" → Exit codes (4, a fault of the tool's own)
- **Finding:** Outline items, groups, tables in cells, and equations recurse to the depth the
  file gives, in the reader, the renderer, the math port, and the parser beneath them; nothing
  caps it (mail caps a `.msg`'s nesting at 16). `catch_unwind` turns a panic into exit 3 or 4,
  but a stack overflow is a SIGSEGV abort, which no handler catches and no exit code 1 to 4
  reports. Read from the code; no input was built to trigger it.
- **Proposed design:** The read runs on a thread with a large fixed stack, which bounds the
  depth far past any real page at no cost in resident memory, and the README names the limit.
  docproc's `internal/cli` already treats a code outside 1 to 4 as its own case; the library
  should record death by signal as failed, with the signal named.
- **Carried by:** onenote; library (the signal's outcome)
