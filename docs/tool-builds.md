# The tools' builds

Each system tool under `tools/` has its own mise.toml, which pins its toolchain alone and defines
its tasks; each README's "Building and testing" states what the tasks do. docproc's root check,
currency, and upgrade run each tool's tasks through the root's `TOOLS` list (`mise -C
tools/<format> run check`), and `tools/pack.sh` runs `publish` to build the release binary it
packs; the release workflow runs that on each runner, natively ([CI and releases](ci.md)). Each
task runs in the tool's directory. This page holds the reasoning behind the tools' configuration.

## Every tool

- **PATH.** A task's shell keeps the caller's PATH order, so where a host `dotnet` without the
  pinned SDK (`/usr/bin/dotnet`) or a host `cargo` (`/usr/bin/cargo`) comes before mise's
  installs, a bare `dotnet` or `cargo` would run it, and for cargo the host's Rust. Each task that
  runs dotnet first puts the pinned SDK's directory, mise's `DOTNET_ROOT`, ahead of the rest of
  PATH; each that runs cargo puts rustup's proxies, in mise's `CARGO_HOME`, ahead of the rest of
  PATH, and mise's `RUSTUP_TOOLCHAIN` then picks the pinned toolchain.
- **Goldens.** The goldens are the release binary's own output, so a change to it is reviewed as
  a diff of them; the check compares against them and never writes them.
- **Currency and upgrade.** `currency` is read-only and development-time: it reads the network, so
  CI never runs it. `upgrade` is development-time, as currency is, and its counterpart: it moves
  every pin currency reports as trailing to its latest, but a held one, and rewrites only the
  tool's directory's pins, never the root's (and, for a Rust tool, the envelope's requirements).
  Run it, review the diff, and run the check, and, for a Rust tool, every other Rust tool's.
- **Holds.** A Rust tool's `HOLDS` list holds the pins `upgrade` leaves where they are and
  `currency` reports as held, not trailing: one line each, `<item> <pin>: <reason>`, the item named
  as currency names it: a crate, a mise tool, or, for docproc-mail, a generator package. A hold
  names its pin, so a pin moved by hand is no longer held.

- **Notices.** `tools/notices.sh` writes a release binary's third-party notices for one runtime
  identifier. A tool with a committed supplement, `tools/<format>/notices.txt`, has it appended
  last, verbatim: the notices cargo-about or the NuGet packages can't give, such as code ported
  into the tool or where an MPL-2.0 crate's source is.

## docproc-ooxml

- **NativeAOT and glibc.** `GLIBC_FLOOR` is the newest glibc symbol version the published binary
  may require: NativeAOT links libc and libm dynamically. `tools/pack.sh` refuses to pack a binary
  that requires a newer one.
- **The check.** Every step after the first uses its locked restore: a package graph that differs
  from the committed lock files fails there. The tests run the CLI's process contract on the
  published binary, so it is published first.
- **Publish.** Without the commit suffix the check's builds carry (`<version>+<sha>`), `--version`
  prints exactly the version the csproj records, which pack.sh compares with the tag's.
- **Currency.** NuGet's audit runs in currency, never in a build, so a newly published advisory is
  reported without failing the check. A currency run after `upgrade` reports none of the pins it
  moved but a vulnerable transitive package, which only a newer direct one can move.
- **Upgrade.** It moves the mise tools pinned in the tool's mise.toml alone: mise reads the root's
  config too, whose pins the root's upgrade owns. Each is written to the tool's file at its latest.
  The packages are then upgraded in a new mise process, with the tools just pinned: a new SDK
  moves the runtime packs the NativeAOT publish locks. The packages are restored afresh first, as
  the SDK may be new, then each direct package with a newer release is written to its project at
  the latest, and the lock files rewritten from the projects and checked in locked mode, as the
  check restores them. The fixture generator's packages are pinned exactly in its script's header:
  each with a newer release is pinned at it in place (`uv add` would rewrite the header's list
  across lines), then the script is re-locked, its other locked packages kept where they are, and
  the lock checked.
- **Goldens.** Each fixture's goldens sit at its path under `fixtures/`, less its extension, so a
  removed fixture's goldens go with the directory. A fixture that exits other than 0 is recorded
  too, its exit code in a `.exit` golden beside the other two, and reported; a fixture with no
  `.exit` golden exits 0.
- **Directory.Build.props.** Every project builds with warnings as errors and the latest analyzers,
  so the check fails on the first warning, including the NativeAOT and trim analyzers' warnings.
  Every project restores from a committed `packages.lock.json`, which the check and the publish
  restore in locked mode, so the package graph changes only in a reviewed commit. NuGet's audit is
  off in every build: a newly published advisory must not fail a build whose code didn't change.
  It runs in `mise run currency` instead.
- **The CLI's project.** The CLI publishes as one NativeAOT binary per runtime identifier, with no
  .NET runtime needed on the host. The binary is docproc-ooxml; the project and its namespaces keep
  .NET's naming. The runtime identifiers are listed so the lock file holds the NativeAOT packages
  of both, and a publish for either restores nothing the lock doesn't pin. libc's `write` and
  `statx` (`Native.cs`) bind directly into the binary, which links libc anyway, rather than
  through a library lookup at run time. The model, the reader, the renderer, and the line writer
  are internal: the binary's process contract is its interface. The tests reach them in process.
  The Open XML SDK is the CLI's only package; the command line is parsed by hand.
- **Notices.** The notices give every NuGet package the binary links and the .NET runtime
  NativeAOT compiles into it, with the runtime's license, followed once each by the third-party
  notices those packages bundle: each distinct text once, by its SHA-256, naming the packages that
  bundle it in first-seen order. The release binary is built from the projects under `src/`; the
  tests' packages are never linked. The packages are read from each lock file's framework-wide
  section and its section for the runtime identifier, less the NativeAOT toolchain's: the compiler
  and the trimmer run at build time and link nothing of their own, and what the binary links of
  .NET is the NativeAOT runtime pack's, given as the runtime's entry, at the one version the
  restore downloaded. The script carries one license text, MIT's, given under a package's
  copyright when the package ships no license file, and refuses a package under any other license:
  a new license is a reviewed change to the script. It reads the tool's restored package graph and
  the NuGet packages folder, so it runs after the tool's check or publish has restored.
- **Ignored.** `.state/` is untracked state: the check's NativeAOT publish.

## The Rust tools

- **The binary.** Each binary is static, linked against musl, so it needs no glibc and sets no
  `GLIBC_FLOOR`. A release binary checks integer arithmetic as a debug one does
  (`overflow-checks` in each Cargo.toml's release profile): an overflow panics, a fault the CLI
  reports, instead of wrapping silently into a wrong offset or length. Every test runs the
  release musl binary, so no debug build is made.
- **The envelope crate.** The shared envelope crate, `tools/envelope`, is untagged and
  unpublished, with no lock or toolchain of its own: each Rust tool depends on it by path, and
  each Rust tool's check formats, lints, and tests it with the tool's toolchain, against the tool's
  Cargo.lock, so it is tested in every graph that links it (docproc-onenote's without its
  `markdown` feature), and docproc's root check reaches it through `TOOLS`. Its manifest is
  formatted by path, which reads no lock and writes none. Its `.gitignore` ignores the build
  output of a cargo run in its directory, and a Cargo.lock, which it has none of. Its features:
  `markdown`, the content rule's Markdown-to-plain reader, which docproc-mail enables.
- **Currency and upgrade.** `tools/currency.sh` reports the crates, their RustSec advisories, and
  the mise tools, as it does for every Rust tool, and moves every mise tool and every crate,
  raising the shared envelope crate's requirements, `tools/envelope/Cargo.toml`, then re-locking
  the tool's graph; [Rust tools' currency](#rust-tools-currency) gives the detail.
- **cargo-about** is not in mise's registry, so it comes from its GitHub release. `tools/notices.sh`
  runs it, in the tool's environment, for the release's third-party notices and their license
  check, under the tool's `about.toml`. Each crate's notice is given under licenses from the
  accepted list, in its order, and a crate whose license expression the list can't satisfy fails
  the script, and with it docproc's root check: adding a license is a reviewed change to
  `about.toml`. cargo-about walks the normal dependency graph for the target, proc macros and their
  own dependencies included, though they run only at build time and the binary links none of
  them; it has no setting to leave them out. Build and dev dependencies are left out. The tool
  itself is unpublished and carries the repository's LICENSE, so it is ignored. The accepted list
  is the Rust tools' shared one: Unicode-3.0 is docproc-envelope's ICU4X crates' license, which
  the binary links (unicode-ident, reached at build time through proc-macro2, needs it too), and
  BSD-3-Clause encoding_rs's WHATWG data's. A license outside the shared list is accepted for the
  one crate that needs it, so no other crate can bring it in unreviewed.
- **Notices.** A Rust tool's notices give every crate cargo-about walks in the tool's locked graph
  for the runtime identifier's musl target, then each license text once with the crates it
  covers, then `tools/notices-rust.txt`: the notices of the Rust standard library and musl libc,
  which every Rust tool's binary links, the tool's pinned Rust version and the target written into
  it. cargo-about runs in the tool's mise environment, its pinned Rust's cargo ahead of a host
  cargo, as the tool's own tasks run it. Its `cargo metadata` reads every crate in Cargo.lock, on
  every platform, so `cargo fetch --locked` first downloads any the tool's build didn't (a no-op
  once they're in the cache); `--frozen` then keeps Cargo.lock as it is and reads no network, and
  `--fail` fails on a license it can't read or the accepted list can't satisfy. Each crate's
  chosen licenses are the texts that name it. A license text cargo-about made up from the
  license's template, for a crate that ships none, is given once per crate, under its authors'
  copyright in place of the template's placeholder.
- **cargo-deny** is read by `currency`, which runs `cargo deny check advisories` alone; the license
  policy is cargo-about's. An unmaintained crate anywhere in the locked graph is reported, a
  transitive one too, not only a workspace member's direct dependency: cargo-deny 0.22.0 narrowed
  this default to `workspace`, so each `deny.toml` sets it to `all`.

### Rust tools' currency

`tools/currency.sh` is each Rust tool's `currency` and `upgrade`. A tool's pins of its own beyond
the crates and mise tools, such as docproc-mail's fixture generator's, stay in its tasks; the
workflows' action pins are docproc's root currency's. A report writes only cargo-deny's advisory
database. After an upgrade, a report shows none of the pins it moved but the held, and an advisory
only a newer release of a crate can clear. Run it, review the diff, and run each Rust tool's check.

- **cargo.** Each cargo call runs the tool's pinned cargo, in its directory, rustup's proxies in
  mise's `CARGO_HOME` ahead of a host cargo on PATH; mise's `RUSTUP_TOOLCHAIN` picks the
  toolchain. Each call is a new mise process, so after the upgrade has rewritten the mise files
  it runs the toolchain just pinned.
- **The mise tools.** Every environment's mise file is loaded, so `mise.<env>.toml`'s tools, such
  as docproc-mail's fixture generator's SDK, are read with `mise.toml`'s; the root's are its own.
  The upgrade rewrites each in place, its version alone, as `mise use` would write rust's table
  as a bare version. A backend-qualified name, such as `github:EmbarkStudios/cargo-about`, is a
  quoted key holding a `/`, so the name is matched with its quotes optional and its regex
  characters escaped, under a `|` delimiter.
- **The crates.** Direct and indirect: updates within each requirement (cargo's `Updating`) and
  newer releases beyond it (`Unchanged ... (available: ...)`), read from a `--dry-run` that leaves
  Cargo.lock as it is. Each scan is captured before it is read, and each capture ends in
  `|| return`, so a command that fails fails the script instead of reporting nothing.
- **Advisories.** Crates in the locked graph with a RustSec advisory: a vulnerability, an
  unmaintained or unsound crate, or a yanked release. cargo-deny fetches the advisory database
  first, and reads it in currency, never in the check, so a newly published advisory fails no
  build.
- **Raising requirements.** A direct dependency with a release beyond its requirement gets that
  release as its requirement, an exact pin (`=`) kept exact, in each manifest that requires it:
  the tool's own and that of each crate it requires by path, the shared envelope crate's among
  them, a crate both require (serde) in both, a dependency listed again under
  `[dev-dependencies]` in both its lines. An indirect one beyond its requirement moves only when
  the crate that requires it does; a path crate has no release to move to. The manifests are read
  without resolving the graph (`--no-deps`), so a lock another tool's upgrade left trailing the
  envelope's requirements is read as it stands and re-locked.
- **Re-locking.** Then every crate within its requirement moves to its latest, held ones left at
  their pins; a requirement raised above, the tool's or the envelope's by another tool's upgrade,
  is within now, so this re-locks the tool's graph to it. An update can move other crates with
  the one it names, as re-locking regex moves regex-automata, so a crate whose pin an earlier
  update already moved is no longer locked at that pin, and is passed over.
- **The envelope.** It has no lock of its own, so each Rust tool's upgrade raises its requirements
  by the same rules as the tool's and then re-locks the tool's own graph. Run in turn, the second
  tool finds the envelope's requirements already at their latest and only re-locks to them, so no
  tool's locked check trails the crate. A bump of the envelope's ICU4X crates that moves the
  Unicode version fails its unit test asserting it, so it stays a reviewed change.

### The envelope's dependencies

- sha2, RustCrypto's SHA-256: a medium is named by the hash of its content.
- The content rule's Unicode (`src/content.rs`) and the general categories (`src/category.rs`),
  all ICU4X 2.4.0 on Unicode 18.0.0, a version that is part of schema "2": `icu_normalizer`, NFC
  normalization; `icu_properties`, Unicode general categories: a word token holds a letter or a
  digit (L* or N*), and the Rust tools' one source of them, `src/category.rs`'s predicates (Cf, Zs,
  N*, Sm); and `icu_segmenter`, UAX #29 word and sentence boundaries, the word segmenter's
  dictionary-free, rule-based form: no LSTM model (the `auto` and `lstm` features) and no
  dictionaries are linked.
- The `*_data` crates are the data the three carry, linked through them already, named for the
  Unicode version each states, which a unit test asserts. They are not dev-dependencies: the crate
  is tested from each tool's graph, where it is no workspace member, and cargo tests no such crate
  with dev-dependencies.
- pulldown-cmark, a CommonMark reader (`src/content/markdown.rs`), for a tool that compares a
  source text with its Markdown; behind the `markdown` feature, so a tool comparing two plain texts
  links none.

### docproc-mail

- The fixture generator's toolchain is in `mise.fixtures.toml`, loaded only with
  `MISE_ENV=fixtures`, so neither the check nor CI installs .NET: `MISE_ENV=fixtures mise run
  fixtures` regenerates `tests/fixtures/generated/` from its `.eml` and item sources. The check
  never runs it; it reads the committed outputs. The fixtures task runs from `generator/`, so its
  `global.json` pins the SDK, with the pinned SDK's directory first on PATH.
- Currency reports the mise tools, `mise.fixtures.toml`'s among them; the fixture generator's
  packages are this tool's own: its NuGet packages with a newer release, run from `generator/` so
  its `global.json` picks the SDK. A held one is reported, with the hold's reason, and leaves the
  exit status alone.
- Upgrade moves every mise tool, here and in `mise.fixtures.toml`, and every crate, then the
  fixture generator's: the SDK's pin in `global.json`, the one `mise.fixtures.toml` pins, and its
  packages, each but a held one, each pinned exactly in `Generator.csproj`: each with a newer
  release is pinned at it in place, then the lock file rewritten from the project and checked in
  locked mode, as the fixtures task restores it. Each runs in a new mise process, with the SDK just
  pinned.
- The generator (`generator/Generator.csproj`) turns the hand-written `.eml` and item sources into
  `.pst` and `.msg` fixtures; the check never builds it. Its lock file pins the transitive packages
  too, so a rerun restores the same graph, and the manifest's package versions are read from the
  lock file the build restored. It uses MimeKit directly, to read a source's Received date.
- Its dependencies: docproc-envelope, with its Markdown-to-plain reader (`markdown`); codepage and
  encoding_rs, code pages to text (`src/codepage.rs`): codepage maps a Windows code page to its
  encoding_rs encoding, which decodes it, and a charset an HTML body's `<meta>` names, a WHATWG
  label encoding_rs knows, back to its code page; compressed-rtf, Microsoft's MS-OXRTFCP
  decompressor, `PidTagRtfCompressed` to RTF; htmd, HTML to Markdown, for the derived Markdown
  body, which brings html5ever and its default DOM, ego-tree (ISC alone, accepted for it alone in
  `about.toml`); pulldown-cmark, the CommonMark reader the envelope's `markdown` feature already
  links, which finds the code spans and code blocks of the Markdown htmd writes, so a text `&` is
  written escaped outside them and as itself within (`src/markdown.rs`); cfb, Compound File Binary
  (MIT), the container of a `.msg`, under the CLI's own MS-OXMSG reader (`src/msg.rs`), which
  brings fnv, uuid and web-time; and mail-parser, Stalwart's RFC 5322 and MIME parser (Apache-2.0
  OR MIT), the reader of an `.eml` and of an mbox's messages (`src/eml.rs`, `src/mbox.rs`), which
  brings hashify and encodify; `full_encoding` brings encoding_rs's multi-byte charsets,
  ISO-2022-JP and GB2312 among them.
- Its `.gitattributes`: the `.eml` sources are hashed in the generated fixtures' manifest and keep
  MIME's CRLF line breaks, and the mboxrd holds those sources byte for byte, CRLF and all, and the
  check compares it so; mime4j's `mbox.rlug` and its splits, and its NOTICE's CRLF, are kept as
  upstream wrote them, so their SHA-256s hold. Git converts none of them.

### docproc-onenote

- Its dependencies: docproc-envelope, also its one source of Unicode: the math port (`src/math/`)
  sorts an equation's characters by its general-category predicates; onenote_parser (MPL-2.0),
  the OneNote reader, used unmodified: sections, notebooks, and, through `onepkg`, packages, which
  brings cab 0.6 for `.onepkg`, and time and typed-path, which its API takes and returns; libc,
  for dup2, to point file descriptor 1 at `/dev/null` while onenote_parser runs
  (`src/stdout.rs`); and cab, the cabinet reader onenote_parser reads `.onepkg` packages with,
  taken directly to list a package's entries for its recycle bin, which the parser drops without a
  word, and, in the tests, its MSZIP writer, which builds the packages they read. Its
  dev-dependencies: time's macros, `utc_datetime!` for the unit tests' times, and roxmltree, an
  XML reader, so the tests check each ink SVG is well-formed; the CLI writes SVG itself.
- onenote_parser is pinned exactly because the CLI works around this release's exact text and
  logic: `src/notebook.rs` matches its warning for a section it can't read, "failed to import
  section ", and follows its choice of a package's `.onetoc2` and its cabinet entry names'
  normalisation; `src/section.rs` matches its NotASectionFile message, "Not a section file: ";
  `src/stdout.rs` keeps its dumps on malformed sections off the lines.
- Its licenses outside the shared list: onenote_parser and its proc macros are MPL-2.0 alone. The
  supplement `tools/onenote/notices.txt`, which notices.sh appends, says where onenote_parser's
  source is, as MPL-2.0 §3.2(a) requires of a binary that holds it. foldhash, hashbrown's hasher
  in the lru cache onenote_parser keeps, is Zlib alone.
- The golden helper in the tests' common module writes the goldens under `UPDATE_GOLDEN`, each
  committed OneStore input's two at its path under `tests/golden`.
- Its `.gitattributes` keeps the public fixture sets as upstream wrote them, so their SHA-256s
  hold.
