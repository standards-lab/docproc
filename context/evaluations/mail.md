# Evaluation · docproc-mail (spike-ai-pipeline-mail v0.3.1)

The spike is in good shape to promote: its readers are small, its skip policy is consistent, its
output is deterministic (ordered maps, no clock, no path in stdout; an `afStorage` copy gave the
same bytes on two runs), and its tests are mostly ones that can fail. Three findings change what
the `mail` task has to build. First, one message's HTML body ends the whole run by signal: about
8,000 nested elements (an 88 KB message) overflow the stack in htmd's recursive walk, the process
dies with SIGSEGV, and a three-message mbox holding it writes nothing at all (ML-1). Second,
`smtp_address` as layers.md words it, from `PidTagSmtpAddress`, never reaches a sender: that
property is a recipient's, and none of the seven fixtures with an Exchange sender holds the
sender's (ML-2). Third, the content rule's Unicode 16.0 can't be both part of schema "2" and
current, and the change list's "the Rust crates stay" doesn't count the crates the rule needs
(ML-3).

What was run: the committed v0.3.1 musl release binary
(`target/x86_64-unknown-linux-musl/release/`, `--version` 0.3.1) over inputs built in a scratch
directory. Nothing in the spike was built or changed. Findings marked "by reading" were not run.

## Findings

### ML-1 · One message's HTML body ends the whole run by signal
- **Kind:** issue
- **Weight:** high
- **Where:** src/markdown.rs:19 (`from_html`), src/pst.rs:447, src/msg.rs:204, src/mime.rs:88-89,
  src/mbox.rs:87-91, src/main.rs:110
- **Against:** layers.md "Common envelope", Exit codes (0 to 4 are the only outcomes) and stderr;
  README "A folder, message, or attachment that can't be read … is skipped"
- **Finding:** htmd 0.5.6 walks the DOM recursively (`dom_walker.rs` `walk_node` →
  `walk_children`), and html5ever caps no nesting depth. Run against the release binary:
  - An `.eml` whose HTML body nests 4,000 `<div>` exits 0; 8,000 (88,184 bytes), 12,000, 16,000,
    and 20,000 exit 139 (SIGSEGV), with nothing on stdout or stderr. 100,000 unclosed
    `<font color=red>` tags do the same.
  - A three-message mbox whose second message is the 8,000-deep one exits 139 with **0 lines** on
    stdout: message 1's line was still in the `BufWriter` (main.rs:110) when the process died.
  - Time is quadratic before the crash: 20,000 levels take 1.2 s, 200,000 (2.2 MB) over 100 s.

  A stack overflow is not a panic, so neither `guarded` nor `main`'s `catch_unwind` sees it. By
  reading: the mapping (decoding, RTF, htmd) runs outside the per-record guard in all four readers
  (pst.rs:447, msg.rs:204, mime.rs:88-89, mbox.rs:87), so an ordinary panic there is also not one
  record's skip but exit 4 for the whole store. In a `.pst` the faulting message's id is never
  written, so `--after` can't step past it: the store is unreadable from that message on.
- **Proposed design:** No input ends the process by signal, and a fault inside one record is that
  record's.
  1. Bound HTML nesting before htmd sees it: a cheap pass over the tags keeps a running count of
     open elements and, past a stated cap (512, the depth Blink's parser caps at), the Markdown is
     made from the text body, else from the HTML's text with tags stripped, with a warning. The
     cap also bounds the quadratic time.
  2. The per-record guard wraps the whole record, read, map, and derive, so a panic in the mapping
     is a `skipped` line; exit 4 is left for faults outside any record.
  3. Flush stdout after each message line, so what a consumer holds at any exit is every completed
     record.
  4. Tests: a unit test at the cap and one past it, and an mbox test with the deep message between
     two good ones whose lines both arrive, run against the release binary (ML-8).
- **Carried by:** mail

### ML-2 · `smtp_address` from `PidTagSmtpAddress` never reaches a sender
- **Kind:** issue
- **Weight:** high
- **Where:** src/mapping.rs:27-33, 232-239 (`address`); src/props.rs (no SMTP tag);
  tests/fixtures/msg/
- **Against:** layers.md "docproc-mail" and "Added" (`smtp_address` … from `PidTagSmtpAddress`);
  roadmap `tasks.mail` ("so a sender has one identity across .pst and .eml")
- **Finding:** `PidTagSmtpAddress` (0x39FE) is a recipient-row and address-book property. A
  message's sender has its own, `PidTagSenderSmtpAddress` (0x5D01). Scanning the committed `.msg`
  fixtures for property streams: 0x39FE occurs only in recipient storages, once per recipient
  (18 of 18 in POI's `outlook_30_msg.msg`, 2 in `testMSG_att_msg.msg`, 1 each in
  `msgClassAppointment`, `testMSG_chinese`, `testMSG_forwarded`), and **no fixture holds 0x5D01**.
  The goldens have an Exchange (`EX`) sender in seven fixtures (five `.msg`, `tika/testPST.pst`,
  `tika/testPST_variousBodyTypes.pst`; 11 senders). Built as layers.md words it, every one of
  them keeps its X.500 name alone, and the roadmap's stated purpose is unmet.
- **Proposed design:** A recipient's `smtp_address` is 0x39FE; the sender's is
  `PidTagSenderSmtpAddress` (0x5D01). layers.md says what happens when the sender's is absent,
  as in every committed fixture (these predate the property): it is left out, and docproc's typed
  metadata takes the sender's SMTP address from `transport_headers`' From, which the tool already
  carries. No committed fixture can show the sender's, so a unit test over a built `.msg`
  (tests/common/msg.rs builds them) with 0x5D01 shows it; the goldens show the recipients'.
- **Carried by:** mail (the fallback from headers: library)

### ML-3 · The content rule's Unicode 16.0 can't be both schema "2" and current, and its crates are uncounted
- **Kind:** issue
- **Weight:** high
- **Where:** Cargo.toml (no Unicode or CommonMark crate); ../spike-ai-pipeline-onenote/Cargo.toml
  (`finl_unicode = "1.5.1"`)
- **Against:** layers.md "docproc-mail", the content rule ("Its Unicode version, 16.0, is part of
  schema \"2\""), and "docproc-mail [mail]" → Toolchain ("The Rust crates stay");
  principles/rolling-currency.md; principles/minimal-footprint.md
- **Finding:** The rule needs NFC, UAX #29 word and sentence boundaries, general categories L\* and
  N\*, a CommonMark reader, GFM autolinks, RFC 3986 normalization, and Myers' diff. The spike has
  none of these, so "the Rust crates stay" understates the task: each is a new crate or
  hand-written code, a footprint decision not yet made. The Unicode version was the measuring
  tool's (uniseg 0.10.1, a Go library), not any Rust crate's. Current Rust crates are past it:
  onenote's category crate, finl_unicode 1.5.1, states Unicode 18.0.0 in its README, as does
  unicode-ident 1.0.27 in this lock. So a 16.0 rule is built either on crates held at old
  releases, against rolling currency, with nothing that fails when a bump moves them, or on
  tables the crate owns.
- **Proposed design:** Settle this before the first golden is recorded, since the goldens pin it.
  The simpler of two:
  1. **The crate owns its tables** (recommended if 16.0 must hold): word-break, sentence-break,
     category, and NFC data generated from UCD 16.0 into the shared crate, no Unicode crate
     required, so currency never touches the rule's meaning.
  2. **Schema "2" names the version the pinned crates carry**, each crate's `UNICODE_VERSION`
     asserted in a unit test, so a currency bump that moves Unicode fails the check and is a
     recorded hold (as typed-path is) or a schema change. The measurement in layers.md is then
     rerun on that version.

  Either way the change list's Toolchain line names the crates the rule adds, each with its
  reason, as Cargo.toml's comments do today.
- **Carried by:** mail (the decision: architecture)

### ML-4 · A folder cycle in a .pst recurses and writes without bound
- **Kind:** issue
- **Weight:** medium
- **Where:** src/pst.rs:354-405 (`Walk::folder`), 411-418 (`Walk::holds`)
- **Against:** layers.md "Common envelope", Exit codes; README's skip policy
- **Finding:** By reading, not run (no crafted store): `folder` recurses into every node id its
  hierarchy table lists, with no set of folders already walked and no depth cap. `.msg` nesting
  is capped at 16 (msg.rs:56, "a compound file's directory can't loop"), but a `.pst`'s
  hierarchy table is data: a row naming an ancestor's node id makes the walk re-enter it. Each
  level writes the folder's line and re-reads all its messages, so the output grows without end
  until the stack overflows, the same signal death as ML-1. `holds`, the `--after` pre-walk,
  recurses the same way before any output.
- **Proposed design:** The walk keeps the set of folder node ids it has entered. A hierarchy row
  naming one already entered is a `skipped` line of kind `folder` with reason "already read", and
  a warning. A unit test over a libyal store copy with one hierarchy row rewritten shows it, as
  tests/pst.rs's `flipped` tests rewrite bytes today.
- **Carried by:** mail

### ML-5 · A message whose only body is plain RTF has no Markdown, and schema "2" cuts its text
- **Kind:** issue
- **Weight:** medium
- **Where:** src/mapping.rs:86-117 (`body`), src/markdown.rs:7-15 (`derive`), src/rtf.rs:89-95
- **Against:** layers.md "docproc-mail" (the Markdown is the text layer; `body.rtf` cut) and
  "docproc-mail [mail]" → Into the Markdown
- **Finding:** `derive` makes Markdown from the stored HTML, else the HTML recovered from a
  `\fromhtml1` RTF, else the text body. A message with a plain RTF body and no text body gets
  `(None, None)`: no Markdown. Schema "1" still carried its text in `body.rtf`; schema "2" cuts
  that, so the message's content is gone. layers.md reads the RTF's text only as the source for
  appending "when the Markdown is made from HTML", a condition a plain-RTF-only message never
  meets, yet its change list plans "unit tests over a built RTF-only message show the RTF's
  text", which can pass only if the Markdown is made from that text. No committed fixture has
  RTF without a text body (of 46 messages, 10 hold RTF, all with text), so nothing shows it yet.
  The RTF text reader is also new code: rtf.rs holds only the de-encapsulator.
- **Proposed design:** layers.md states the order in full: stored HTML, HTML recovered from RTF,
  the text body, then the RTF's text read as a text body. The RTF text reader is the existing
  lexer and group stack (rtf.rs:114-193, 196-255, the `INVISIBLE` table) with a second sink, not
  a second walker: de-encapsulation copies `\*\htmltag` groups, plain reading copies visible
  text. The built-message unit test covers a plain-RTF-only message and a `\fromtext` one.
- **Carried by:** mail

### ML-6 · Linking inline images by `file` is more than a rename, and `data:` images flood the Markdown
- **Kind:** issue
- **Weight:** medium
- **Where:** src/markdown.rs:19-25; src/mapping.rs:56, 147-176; src/mime.rs:88-127, 322-330;
  src/props.rs (no 0x3713)
- **Against:** layers.md "Common envelope", Media ("the Markdown links it by that name"), and
  "docproc-mail [mail]" → Envelope
- **Finding:** Run over a built `.eml`:
  - The Markdown keeps `cid:One%2Bimg@example.com`; the attachment's `content_id` is
    `One+img@example.com`. A `cid:` URL is percent-encoded (RFC 2392), so matching the two as
    strings misses.
  - An image referenced by `Content-Location` (`src="pic2.png"`) is written as media but the
    Markdown keeps `![b](pic2.png)`: the contract has no field to match it by, and the MAPI side
    reads no `PidTagAttachContentLocation` (0x3713), though `ATT_MHTML_REF` means "by its
    content-id or location" (mapping.rs:138).
  - A `cid:` with no attachment stays `cid:missing@…`; layers.md doesn't say what it becomes.
  - A `data:` image goes into the Markdown whole: a 300 KB picture gave a 400,038-byte Markdown
    with its base64 inline and no media file. That text is chunked and embedded.

  And in every reader the Markdown is made before the attachments are read (mapping.rs:56 runs
  inside `message`; pst.rs:447 precedes the attachment loop at 455), so the link needs the order
  reversed, including for nested messages.
- **Proposed design:** Attachments are read first; the HTML's image sources are then rewritten
  before htmd runs. A `cid:` is percent-decoded and matched to `content_id`; a relative or
  absolute source is matched to the attachment's Content-Location (0x3713 in MAPI, read for
  matching only, no new field); a `data:` image is decoded, written as media like any payload,
  and linked by its name, without an attachment record. A reference that matches nothing is kept
  as written, with a warning. `inline` could then mean the same in every format ("the body
  links it"; today it is `ATT_MHTML_REF` in MAPI and Content-Disposition in MIME, so the same
  message differs as `.msg` and `.eml`), but no consumer reads it, so that is the architect's
  call. Unit tests over built messages show each case.
- **Carried by:** mail

### ML-7 · A warning's `<where>` carries folder names, which can hold `": "`
- **Kind:** issue
- **Weight:** medium
- **Where:** src/pst.rs:361-363 (`place`), 438, 456
- **Against:** layers.md "Common envelope", stderr ("`<where>` never holds `": "`, so a warning
  line splits at its first three"; one line each)
- **Finding:** A `.pst` warning's place is the folder's display names joined by `/`, as stored
  (`/Top of Personal Folders/Inbox, message 2097188`). A folder named `Project: Alpha` puts
  `": "` in `<where>`, so docproc's split puts the rest of the path into `<what>`; a name with a
  line break makes two lines. Folder names are user text, and a name with `/` is already
  ambiguous. The other formats' places are fixed words and numbers and are safe.
- **Proposed design:** A `.pst` place names the folder by node id: `folder 32802, message
  2097188, attachment 5`. The id is on the folder's line, so docproc joins it to the path it
  already keeps. The stderr goldens are re-recorded under "2" anyway.
- **Carried by:** mail

### ML-8 · The goldens prove the debug build, not the binary that ships
- **Kind:** issue
- **Weight:** medium
- **Where:** tests/common/mod.rs:14 (`CARGO_BIN_EXE_…`); tests/pst.rs:317-318; mise.toml:15-18;
  Cargo.toml (no `[profile.release]`)
- **Against:** layers.md "Common envelope", Determinism (byte-identical output proven by goldens);
  go-elemental release-and-ci.md "One check per repository" (the gate is the artifact's)
- **Finding:** `cargo test` runs the debug binary; the check then builds the release musl binary
  and runs only `--version` on it. The two differ where it matters: tests/pst.rs says of its
  corrupted store "the tests run the debug binary, whose overflow checks make that a panic (a
  release build reads a different error there)". In release, arithmetic in outlook-pst, cfb, and
  compressed-rtf on corrupt lengths wraps silently instead of panicking into the guard, so the
  shipped binary's behavior on malformed input, its skipped lines and stderr, is untested, and
  stack use differs too (ML-1's threshold). The check also compiles the 92-crate graph twice.
- **Proposed design:** `[profile.release] overflow-checks = true` in both Rust tools, so a wrapped
  length is a guarded panic in the binary that ships. The check runs
  `cargo test --release --locked --target <musl>`, so the goldens run against the release
  artifact and the graph is built once; the debug build leaves the check.
- **Carried by:** mail, onenote

### ML-9 · The shared crate should hold the envelope both tools already duplicate, and it has drifted
- **Kind:** simplification
- **Weight:** medium
- **Where:** src/main.rs:38-93, 245-264; src/jsonl.rs; src/guard.rs; src/files.rs;
  src/mapping.rs:178-193; src/markdown.rs:32-90; against ../spike-ai-pipeline-onenote/src/
  main.rs, jsonl.rs, guard.rs, media.rs, markdown.rs:446-500
- **Against:** layers.md "Common envelope"; "the content rule … written once, in one crate under
  `tools/`"
- **Finding:** The two Rust tools each carry the common envelope by hand. `jsonl.rs` is identical
  byte for byte; `guard.rs`, `Fault`, `fail`, the panic hook, `panic_text`, `warn`, and the
  argument loop differ only in names; the media name rule (SHA-256 plus a 1-to-8 alphanumeric
  extension) is written twice (mapping.rs `file_name`, onenote media.rs `name`); onenote's
  `escape` is a copy "as mail's Markdown escapes it". The copies have already drifted:
  - mail's usage error is `<tool>: <reason>` (main.rs:82, pinned by tests/cli.rs), without the
    `error:` the envelope and onenote write;
  - mail's `--version` uses `println!` (main.rs:99), so `--version > /dev/full` exits 4
    "internal fault" (run); onenote writes it and exits 1;
  - mail creates the media directory on every successful run (files.rs:28), onenote before its
    first file, so an input with no media leaves a directory under one tool and none under the
    other.

  mail also escapes a text body by building HTML and running html5ever and htmd over it, then
  patching what htmd 0.5.6 leaves (`escape_rest`), where onenote's 30-line `escape` does it
  directly. The appended sentences ("escaped as a text body's Markdown is") need that function
  in the shared crate anyway.
- **Proposed design:** The untagged path crate holds the Rust tools' envelope, not the content
  rule alone: the fault type with its exit codes and error line, the warning line, the lines
  writer, the record guard, the media store (name rule, write-once, one directory rule), the
  literal-text escape, and the content rule. Each tool keeps its flags, readers, and model.
  mail's `from_text` calls the shared escape, so a text body no longer passes through html5ever
  and no longer changes when htmd does. The three drifts above settle to the envelope's wording.
- **Carried by:** mail (writes the crate), onenote (adopts it)

### ML-10 · codepage-strings can go: encoding_rs and codepage are already direct dependencies
- **Kind:** simplification
- **Weight:** medium
- **Where:** Cargo.toml (`codepage-strings = "=1.0.2"`, `codepage`, `encoding_rs`);
  src/codepage.rs:184-208, 353-367
- **Against:** principles/minimal-footprint.md; spike STANDARDS.md ("actively maintained")
- **Finding:** codepage-strings 1.0.2 (README: "Bart Massey 2021") is a 295-line wrapper that
  maps a code page through `codepage::to_encoding` to encoding_rs, else to oem_cp's DOS tables.
  The spike pins it exactly "because the CLI works around" its missing US-ASCII
  (codepage.rs:185, 353), and already depends on `codepage` and `encoding_rs` directly for
  `meta_charset`. It alone brings 11 of the lock's 92 crates: oem_cp, ahash, getrandom,
  lazy_static, zerocopy and its derive, version_check, and four platform shims. Its decode calls
  encoding_rs's BOM-sniffing `decode`, so a PT_STRING8 that starts with bytes FF FE is read as
  UTF-16 whatever its code page.
- **Proposed design:** `decode` becomes about 30 lines over `codepage::to_encoding` and
  `Encoding::decode_without_bom_handling`, with UTF-16LE, UTF-8, and US-ASCII handled as now and
  a code page `codepage` doesn't know decoded lossily as 1252 with the existing note. The DOS
  pages oem_cp adds (437, 850, …) aren't MAPI string code pages; if the review of `~/docs` finds
  one, it is a table in the tool. The exact pin and its workaround go.
- **Carried by:** mail

### ML-11 · The build, release, and CI files assume the crate is the repository
- **Kind:** issue
- **Weight:** medium
- **Where:** .github/workflows/release.yml:14-15, 37, 50, 60, 77; .github/workflows/ci.yml;
  scripts/release-pack.sh:29, 35; scripts/currency.sh:11, 47; mise.toml:11-18;
  src/main.rs:38-41; tests/ (the tool's name in literals), tests/golden/**/*.stderr
- **Against:** principles/independent-releases.md; go-elemental release-and-ci.md; docproc
  context/README.md (`tools/<format>/v<version>`); roadmap `tasks.mail` ("CI checks gate the
  merge")
- **Finding:** Nested at `tools/mail`, these break as written:
  - release.yml triggers on `v*.*.*`, and uses `github.ref_name` in the asset's file name; for
    `tools/mail/v0.1.0` that name holds slashes. release-pack.sh compares `--version` with
    `${tag#v}`, and the changelog path is the repository's root.
  - currency.sh and the check `cd "$MISE_PROJECT_ROOT"`, list `*.sh` from the repository, and
    read `.github/workflows/*.yml` relative to the crate. docproc's root `mise run check` loops
    Go modules only, and its currency reads only the root's mise tools, not `tools/mail`'s Rust
    pin, crates, or the `fixtures` environment.
  - "CI checks gate the merge" has nothing behind it: neither the spike nor
    standards-lab/docproc has a ruleset (`gh api repos/…/rulesets` is empty for both). The
    spike's four pull requests merged after their checks passed (22 s to 7 min later), by
    habit. With
    per-tool path filters, a required check that never starts blocks every other pull request.
  - The tool's name is a literal in `NAME`, `USAGE`, release-pack.sh, the workflows, the test
    assertions, and every `.stderr` golden.
- **Proposed design:** One CI workflow in docproc with a job per artifact (the Go check, each
  tool's check on linux-x64 and linux-arm64), each job deciding from the changed paths whether
  to do work, and one always-running final job that the ruleset requires, so a skipped tool
  never blocks a merge. One release workflow per tool, triggered by `tools/mail/v*.*.*`, that
  strips the prefix once into `VERSION` and names assets
  `docproc-mail-v<version>-linux-<arch>.tar.gz` with `SHA256SUMS`. The tool's check and currency
  are tasks in `tools/mail/mise.toml` that resolve paths from their own directory, and the
  root's `check` and `currency` call them. The name comes from `CARGO_PKG_NAME` in `src/` and
  `tests/`, so the rename is Cargo.toml's line and the goldens.
- **Carried by:** mail (its files); the workflow shape and the ruleset: whichever tool task lands
  first, with onenote and ooxml following it

### ML-12 · MimeKit 5.0.0 sits under two packages built for 4.x, and the bump regenerates every generated fixture
- **Kind:** issue
- **Weight:** medium
- **Where:** generator/Generator.csproj:20-27; generator/packages.lock.json; generator/global.json;
  mise.fixtures.toml; generator/Fixtures.cs:149-163; tests/generated.rs:622, 716;
  tests/fixtures/generated/manifest.json (`note`)
- **Against:** layers.md "docproc-mail [mail]" → Toolchain ("MimeKit 4.18.2 to 5.0.0, its fixture
  truth unchanged"); principles/rolling-currency.md
- **Finding:** The generator calls MimeKit once, to read a Received date (Fixtures.cs:152-153).
  Its direct reference exists to lift the version MsgKit 3.0.6 (which asks for 4.18.1) and
  PstBuilder.Eml 1.3.0 (4.17.0) resolve to, past a Pkcs advisory. Both were compiled against
  MimeKit 4; a major version under them can fail at run time, which only a regeneration shows.
  The regeneration is not free: the manifest's own note says neither format is byte-stable
  (random store UIDs and search keys; MsgKit writes the time of conversion as creation and
  modification times), and tests/generated.rs:716 requires the manifest's tools to equal the
  csproj's pins. So the bump rewrites all ten generated `.pst` and `.msg` files, every manifest
  hash, and the `.msg` goldens' times. The change list also leaves the generator on `net10.0`
  and SDK 10.0.402 with `rollForward: disable`, while docproc-ooxml moves to .NET 11, giving one
  repository two .NET pins.
- **Proposed design:** One regeneration, done once with the schema "2" re-record so the goldens
  change a single time: MsgKit and PstBuilder.Eml at releases built on MimeKit 5 if they exist,
  the generator on .NET 11 beside ooxml. If neither package has moved, MimeKit stays at 4.x as a
  recorded hold (as typed-path is for onenote) rather than forcing 5 under them. The Received
  date is read with a few lines of the generator's own, so the direct MimeKit reference goes
  when the transitive one no longer needs lifting.
- **Carried by:** mail

### ML-13 · Two kinds of content never reach the contract, and neither layers.md nor the roadmap records them
- **Kind:** issue
- **Weight:** medium
- **Where:** src/pst.rs:7-15, 308-315, 543-552; README "Known limits"; tests/pst.rs:239
- **Against:** layers.md "The tool contract" ("Every piece of text the input holds exists once");
  data-intelligence.md `metadata` (per-format metadata for mail)
- **Finding:** The roadmap's mail inputs are the allocator timing and the `.msg` nesting cap.
  Two larger limits the README states aren't carried anywhere in the target:
  - A `.pst`'s embedded messages (a message forwarded as an attachment) are listed from their
    table row with no content and no `message`: outlook-pst 1.2.0 can't open them. The goldens
    hold three (`tika/testPST`, `java-libpst/dist-list`). The contract's `attachment … message?`
    reads as if every format fills it.
  - Class-specific fields are not read: a contact, an appointment, or a task is a message line
    with the message fields alone (the goldens hold 2 `IPM.Contact`, both with no body at all,
    and 2 `IPM.Appointment`), so a contact's numbers and an appointment's start, end, and place
    are in no line.
- **Proposed design:** layers.md's docproc-mail section states both as the contract's limits, so
  docproc's and Cerebro's `metadata` design doesn't assume them: a `.pst` attachment of method
  `embedded_message` has no `message` and no `file`, and no class-specific field is written.
  Both go to the roadmap's mail backlog beside the nesting cap, the first with its upstream
  condition (outlook-pst's unmerged fix). Filling either later is an addition under "2".
- **Carried by:** roadmap (the backlog entries); layers.md's wording: the design task

### ML-14 · The spike's evidence machinery doesn't belong in the tool
- **Kind:** simplification
- **Weight:** low
- **Where:** bench/findings.rs (1,435 lines), bench/rusage.c, scripts/findings.sh, Cargo.toml
  (`[[example]]`), generator/Scale.cs (511 lines), docs/findings/, mise.toml `findings`,
  mise.fixtures.toml `scale`
- **Against:** none: simplification
- **Finding:** About 2,200 lines score the spike's evidence 16 and time the 2 GB scale store. The
  check compiles the 1,435-line scorer on every run and never runs it. None of it tests the
  contract: the fixtures' truth is already asserted in tests/.
- **Proposed design:** `tools/mail` takes `src/`, `tests/`, and the generator's `fixtures` mode.
  The scorer, the timing wrapper, the scale mode, and the finding stay in the spike at v0.3.1,
  where they take a binary by path if the allocator question (roadmap input) is opened. The
  README's cost figures cite the spike's finding.
- **Carried by:** mail

### ML-15 · The named-property reader and multi-valued values are read for no field
- **Kind:** simplification
- **Weight:** low
- **Where:** src/msg.rs:46-50, 137, 158-164, 290-301, 461-483, 561-669 and their tests;
  src/props.rs:68-70; src/pst.rs:568-585
- **Against:** none: simplification
- **Finding:** `.msg` named properties are parsed into a map behind `#[allow(dead_code)]` ("the
  model reads no named property yet … so the first field that needs one has it"), about 110
  lines plus tests, and its faults become `named properties` warnings about data nothing uses.
  `Value::Multiple` ("the mapping reads none yet") is built by both readers, with the `.msg`
  side's per-value stream reads, and read by none.
- **Proposed design:** Remove both. A multi-valued property becomes `Value::Other`. If
  class-specific fields are ever taken up (ML-13), named properties return with the field that
  reads them.
- **Carried by:** mail

### ML-16 · The mbox reader would be the one format without a golden
- **Kind:** issue
- **Weight:** low
- **Where:** tests/fixtures/mime/ (`eml/09-threading.eml`, `nine.mboxrd`, `rlug/`);
  tests/generated.rs:573-596; tests/pst.rs:23, tests/msg.rs:57; README "Known limits" (ANSI)
- **Against:** layers.md "Changes per tool and per package" (one `.jsonl` and one `.stderr` per
  fixture) and "docproc-mail [mail]" → Goldens
- **Finding:** The change list adds `.eml` goldens for `generated/eml/` only. The MIME group's
  own `.eml`, the mboxrd, and mime4j's `mbox.rlug` stay truth-scored, so an mbox's ordinals,
  `skipped` lines, and warning places are pinned by no golden. The generated goldens compare
  stdout alone and don't assert the exit code. The golden loop is written three times. The ANSI
  `.pst` path (`Pst::Ansi`, `AnsiMessage`, `AnsiAttachment`) is promoted untested: no fixture is
  ANSI.
- **Proposed design:** One golden helper in tests/common (run, exit 0, `.jsonl`, `.stderr`,
  `UPDATE_GOLDEN`) over every committed input, the MIME group included. An ANSI store joins the
  fixtures if a freely licensed one is found; until then the README's limit stands and the
  backlog carries it.
- **Carried by:** mail

### ML-17 · Inputs refused or passed through without a word
- **Kind:** issue
- **Weight:** low
- **Where:** src/sniff.rs:52-63, 94-135; src/mime.rs:313-374; README "Known limits"
- **Against:** layers.md "Common envelope", Exit codes; README "Known limits" (lists none of
  these)
- **Finding:**
  - An `.eml` that starts with a UTF-8 byte-order mark is refused, exit 3, "neither a .pst, a
    .msg, an .eml nor an mbox" (run): `field_name` rejects the first byte.
  - Any text whose first five bytes are `From ` is an mbox: it gets a `store` line, one
    `skipped` message, and exit 0, not a refusal.
  - By reading: a TNEF part (`winmail.dat`, where Exchange puts the RTF body and attachments
    for a non-Outlook reader) and an opaque S/MIME part (`application/pkcs7-mime`) are written
    as attachments and their content is not read; the message's Markdown is empty or partial
    with no warning.
- **Proposed design:** Skip a leading byte-order mark before the header-shape check. An mbox whose
  entries all fail the header check is refused when nothing has been written; with streaming
  that means holding the `store` line until the first message parses. TNEF and opaque S/MIME
  are named in the README's limits with a warning on the message, and go to the backlog.
- **Carried by:** mail

### ML-18 · A compressed RTF's stated size is trusted for an allocation of up to 4 GiB
- **Kind:** issue
- **Weight:** low
- **Where:** src/rtf.rs:25-56
- **Against:** layers.md "Common envelope", Exit codes
- **Finding:** `decompress` checks the header's raw size only for the uncompressed type
  (rtf.rs:41). For the compressed type, compressed-rtf 1.0.1 does
  `Vec::with_capacity(raw_size as usize)` after a CRC a crafted body can satisfy. A failed
  allocation aborts the process; it is not a panic, so the `catch_unwind` here doesn't see it.
- **Proposed design:** Refuse a raw size the format can't reach before calling the decompressor:
  a token of two bytes yields at most 17, so a raw size above nine times the compressed length
  is "says it holds more than it can", a note on the message.
- **Carried by:** mail

### ML-19 · Peak memory follows the largest attachment, several times over, and isn't stated
- **Kind:** issue
- **Weight:** low
- **Where:** src/pst.rs:327; src/msg.rs:370-377, 535-559; src/eml.rs:30; src/mime.rs:358
- **Against:** data-intelligence.md (replicas sized for any job); README (states time, not the
  memory bound)
- **Finding:** Every attachment is held whole: a `.pst`'s in outlook-pst's buffer and again in
  `data.buffer().to_vec()` (pst.rs:327), a `.msg`'s in a `Vec`, an `afStorage` rebuilt in
  memory, an `.eml` read whole and its parts decoded beside it. The finding measured 65 MiB on
  the scale store, whose largest attachment is 16 MiB; a store with a 1 GiB attachment needs a
  few GiB, and nothing caps or reports it.
- **Proposed design:** Drop the copy at pst.rs:327 (hash and write from the borrowed buffer). The
  README states the bound: peak memory is a small multiple of the largest single attachment, or
  of the file for an `.eml`. Cerebro's `tool-packaging` sizes replicas from that.
- **Carried by:** mail

### ML-20 · The release archive carries the tool's license and no notices for what it links
- **Kind:** issue
- **Weight:** low
- **Where:** scripts/release-pack.sh:43-44
- **Against:** system-tools.md "Licenses travel with the image"
- **Finding:** The archive holds the binary and `LICENSE`. Its lock names 92 crates, all
  permissive (none copyleft-only), but 18 are MIT alone, encoding_rs adds BSD-3-Clause, and
  one carries Unicode-3.0, each of which asks that its notice travel with a binary. Cerebro's
  image takes the archive as its unit.
- **Proposed design:** release-pack writes a `THIRD-PARTY` notices file from the locked graph
  into the archive, the same step for both Rust tools; the check fails on a license outside the
  permissive set the spike's STANDARDS.md line already names by judgement.
- **Carried by:** mail, onenote
