# docproc-onenote

docproc's system tool for OneNote: a command-line utility that reads one OneNote `.one` section,
from desktop OneNote 2016 and later or downloaded from OneDrive, one `.onepkg` package, or, for
an operator, a `.onetoc2` notebook or a notebook directory, through
[onenote_parser](https://crates.io/crates/onenote_parser) 2.1.0, used unmodified, and writes it
to stdout as JSON Lines, schema `"2"`, with each page's Markdown rendering inside its line as the
text layer. A notebook's section groups and sections are read in its table of contents' order,
and each page at its latest revision, with its rich text, lists, tables, math, note tags, and
recognised handwriting; its images, embedded files, and ink drawings, as SVG, are written out as
media files.

It is one static musl binary each for linux-x64 and linux-arm64, built with Rust, needing no
glibc on the host, for docproc to run like any other system tool. It reads files only, never
Microsoft 365 or the network, and runs no inference: it calls no model, and the text it extracts
is complete without one.

docproc-onenote began as spike-ai-pipeline-onenote v0.1.0; its findings, the fields it recovers
per kind and its time and peak memory per input, stay in that spike
([`docs/findings/onenote-cli.md`](https://github.com/JaimeStill/spike-ai-pipeline-onenote/blob/v0.1.0/docs/findings/onenote-cli.md)).

## Install

Each release, tagged `tools/onenote/vX.Y.Z`, carries two archives and their checksums:

- `docproc-onenote-vX.Y.Z-linux-x64.tar.gz`
- `docproc-onenote-vX.Y.Z-linux-arm64.tar.gz`
- `SHA256SUMS`, covering both.

Each archive holds the `docproc-onenote` binary, the repository's `LICENSE`, and
`THIRD-PARTY-NOTICES.txt` for the crates the binary is built from, the Rust standard library and
musl libc it links, and the code ported into it. Check the archive against `SHA256SUMS` before
unpacking it:

```sh
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf docproc-onenote-v0.1.2-linux-x64.tar.gz
./docproc-onenote --version
```

The binary is static: it links musl and requires no glibc symbol.

## Usage

```sh
docproc-onenote [--media <dir>] [--file-only] <input>
docproc-onenote --version
docproc-onenote --help
```

It reads the one `<input>` and writes its lines to stdout, and nothing else there. The arguments
may come in any order, and a path needn't be UTF-8. An argument beginning with `-`, but `-`
alone, is an option, so an input whose name begins with `-` is given as `./-…`. The input's kind
is sniffed from its content, never from its name:

- a cabinet's signature, `MSCF`, is a `.onepkg` package, read as its notebook;
- a OneStore header, of the desktop revision store format or of OneDrive's packaging format, is a
  section or a table of contents, as onenote_parser's store type says, since a OneDrive
  download's `.onetoc2` carries the section file type in its header; a table of contents is read
  as its notebook, its sections resolved beside it;
- a directory is a notebook, read through the one `.onetoc2` it holds.

With `--file-only`, a table of contents, given as a `.onetoc2` or as its notebook's directory, is
refused, exit 3, with the fixed reason `a table of contents; its sections are read on their own`,
before any section is read; a section and a package read as they do without it. docproc always
passes it: each section beside a `.onetoc2` is its own input, so reading the table of contents
too would read every page twice, and spooled away from its sections it would read none.

With `--media <dir>`, each distinct image, embedded file, and ink drawing is written once into
`<dir>`, created once the input is read, before the line that names it. Its name is the
lowercase hex SHA-256 of its bytes and its stored extension, its case kept, when that is 1 to 8
ASCII letters and digits: an image's own stored extension, else its filename's; an embedded
file's filename's; and `svg` for ink. Each is streamed through a temporary file in `<dir>` and
renamed to its name, so a name there always holds the whole content. Without `--media` nothing
is written to disk and the lines name the files all the same. A stored filename is never used as
a path. `--media` naming something that exists and isn't a directory is a usage error.

`--version` prints the version on its first line and the schema on its second:

```text
0.1.2
schema 2
```

`--help` prints the usage, the three lines that open this section, to stdout and exits 0.
`--version` and `--help` each come alone; with any other argument either is a usage error. A
stdout that can't take their output exits 1.

## Output

The `notebook` line comes first. Then the notebook's entries depth-first, in its table of
contents' order: a `section_group` line before the entries it holds, and a `section` line
followed by its `page` lines, in the order of its page series. Then a `skipped` line for each
section the read left out, in the order the parser reported them. A section input gives the
notebook line, one section line, and its pages. A package reads like its notebook directory, in
every line but the notebook line.

```text
{"line":"notebook","schema":"2","input":"notebook","name":"onenote-rs","id":"f1da443f-a65f-4513-b200-78d8a9910b8d","color":"#9bbbd2"}
{"line":"section","id":"0575dd0a-5612-d746-b82b-983f7a80b282","path":["New Section 1"],"color":"#8aa8e4"}
{"line":"page","id":"5eaf76b1-8dde-8745-a8a7-1a9865cc478a","section":"0575dd0a-5612-d746-b82b-983f7a80b282","level":1,"title":"Test Page",…,"markdown":"# Test Page\n\nTuesday, 27. October 2020 11:47\n\n…"}
```

- `notebook`: the schema; the `input`, `section`, `notebook` (a `.onetoc2` or a directory), or
  `package`; the `name`, a section's file stem, a notebook's directory name, or a package's file
  stem; and a notebook's or package's `id`, its `.onetoc2`'s FileIdentity, and `color`.
- `section_group`: its `id`, the FileIdentity of its own `.onetoc2`; its `parent` group's id; and
  its `path` of names from the notebook's root, its own last.
- `section`: its `id`, its FileIdentity; its `parent` group's id; its `path`, its display name
  last; and its `color`.
- `page`: its `id`, the page's GUID; its `section`'s id; its `level` in the section's page tree,
  1 for a page and 2 for its subpage; its `title`; its `creation_time` and `modification_time`;
  its `author`, as stored; its `markdown`; its `media`; and its `ink_alternatives`.
- A page's `media` are its images, embedded files, and ink drawings, in the order the reader
  visits them: each its `kind` (`image`, `embedded_file`, or `ink`), its `file`, and, as it has
  them, its `filename`, its `size` in bytes, an image's stored `extension`, and an embedded
  file's `type`, `audio` or `video` for a recording. A record whose data is missing or invalid
  has no `file` and no `size`, with a warning.
- A page's `ink_alternatives` are the recognised ink words that have other guesses: each its
  `word`, its index from 0 among the page's recognised words over the recognition's lines, and
  its `alternatives`, best first. The words themselves are in the Markdown.
- `skipped`: its `kind`, `section`; its `name`, a section's file name as the table of contents
  gives it; its `reason`; and `encrypted: true` for a password-protected section.

Ids are lowercase hyphenated GUIDs, colours `"#rrggbb"`, and times ISO 8601 UTC to the second, a
fraction cut, not rounded. A field with no value is left out, never `null`, and so is an empty list.
The same input gives byte-identical stdout and media, but at the three edges [Limits](#limits)
names. The contract, field by field, is docproc's:
[`docs/tool-contract.md`](../../docs/tool-contract.md), its "docproc-onenote" section. From schema
`"2"` on, it grows by addition only; any other change moves the schema, and
[`CHANGELOG.md`](CHANGELOG.md) records it.

### Markdown

A page's `markdown` is rendered from the tool's own reading of the page, none when nothing on it
renders:

- The title as `#`, then the title area's date and time paragraphs, joined by one space, as one
  line; then the containers top to bottom, then left to right, an ink container placed by its
  strokes' bounds.
- An `hN` paragraph one level below the title, capped at 6; code paragraphs fenced, those beside
  each other in one fence; a quote or citation as `>`, those beside each other in one quote.
- Bold `**`, italic `*`, strikethrough `~~`, and links; underline, highlight, colour,
  superscript, and subscript are plain text.
- Lists nested, numbered from their restart, else from 1; a restart that breaks the count, or a
  container's start, parts two lists with `<!-- -->`.
- Every note tag's label as a `[Label]` before its item's text; a check-box tag makes the item a
  task, `- [ ]` or `- [x]`, its label after the box.
- Tables as GFM, the first row the header, short rows padded, a cell's blocks joined by `<br>`;
  a table's tags on a line before it.
- Math as `$…$` inline and `$$…$$` for display, its LaTeX from a port of
  [one2html](https://github.com/msiemens/one2html)'s MIT math lexer, parser, and renderer, which
  keeps Unicode symbols. An equation whose markers don't parse is the LaTeX of its text, with a
  warning.
- An image as `![alt](file)`, its alt text followed by the sentences of its OCR text that the
  alt text lacks, by the shared envelope crate's content rule (Unicode 18.0.0); a hyperlinked
  image as `[![alt](file)](hyperlink)`. An embedded file as `[filename](file)`. Without a file,
  `[image: …]` and `[file: …]`.
- An ink drawing, the page's own ink, a paragraph's handwriting, or an ink block, as
  `![ink: <words>](file)`, its words those its own strokes belong to, in recognition order, or
  `![ink](file)` with none. A recognised word no drawing carries is written once, on one plain
  line after the page's content, so every recognised word is in the Markdown.
- Literal text escaped by the literal-text escape docproc's Rust tools share, which escapes `$`
  as well, and destinations written by their shared destination writer. In a table cell, a `|`
  in a code span, an equation's LaTeX, or a destination is written `\|`.

Each ink SVG is written by the tool's own code: one path per stroke, with its colour, width,
transparency, and pen tip, its numbers rounded to two decimals in a fixed form, so the same ink
gives the same bytes, and so the same name.

## Exit codes and stderr

| Exit code | Meaning |
| --- | --- |
| 0 | Read, skipped sections included. |
| 1 | stdout or the media could not be written, a broken pipe included. |
| 2 | A usage error; the usage follows the error line. |
| 3 | Refused before any output. |
| 4 | A fault of docproc-onenote's own, never the input's. |

stderr holds one line each:

- `docproc-onenote: warning: <where>: <what>` for what was skipped or read with a loss, escaped
  so the line splits at its first three `": "`. `<where>` names the record: a section by its path
  from the notebook's root (`/Group/Section`), followed by `, page <id>` and then `, image <n>`,
  `, embedded file <n>`, or `, equation <n>`, each numbered from 1 within its page;
  `/OneNote_RecycleBin` for the recycle bin; and `/` for the notebook.
- `docproc-onenote: error: <reason>` for exit codes 1 to 4. The reason never leads with the input
  path, and names it only for an input missing or without read permission.

A section that fails to parse inside a notebook is a `skipped` line and a warning,
`<where>: the section is skipped: <reason>`, and the read goes on with exit code 0. A warning also
comes for each warning in onenote_parser's section and notebook reports; a notebook's recycle
bin, a `OneNote_RecycleBin` beside its `.onetoc2`, in the directory or in the package's cabinet,
whose sections and pages are not read; a notebook read that reads no section or section group,
at `/`; a creation time the parser couldn't read, left out; an image or embedded file whose data
is missing or invalid; and an equation whose structure is lost.

A refusal comes before anything is written to stdout or the media directory. docproc-onenote
refuses an input that is missing, `no such file: <path>`, or a file or notebook directory without
read permission, `permission denied: <path>`, the path as given; any other input that can't be
read; a ZIP, `a ZIP archive; extract the OneDrive download first`; a directory with no
`.onetoc2`, or more than one; anything else, `not a OneNote section, notebook, or package`; under `--file-only`, a table of contents; and a section,
notebook, or package onenote_parser can't read, with its reason, or that panics it, `the reader
panicked: <message>`. A section that fails to parse and holds the two constants of an
[MS-ONESTORE] 2.5.19 encryption key, scanned for only after the parse fails, is
password-protected: refused as an input, and skipped inside a notebook with `encrypted: true`,
with a reason beginning `encrypted`. A read thread that can't start, its stack's reservation
refused at the size its limit gives, is exit 4: `internal fault: the read thread can't start`.

onenote_parser prints debug dumps to stdout as it fails on some malformed sections, so the tool
writes its lines to a duplicate of stdout and points file descriptor 1 at `/dev/null` while it
reads: stdout carries the lines and nothing else.

The envelope these follow, its cases stated once for every tool, is
[`tools/envelope/cases.json`](../envelope/cases.json), and the code that writes it is the shared
crate, [`tools/envelope`](../envelope).

## Limits

- A directory read, or a `.onetoc2`'s, reads the sections its table of contents lists and no
  others: a `.one` beside it that the table doesn't list is left out, with no warning. A listed
  section whose file is missing is dropped by the parser without a word; only a read that reads
  no entry at all is warned of.
- A section that fails inside a section group vanishes: the parser discards the group's report,
  so it gets no `skipped` line and no warning. A section whose parse panics the parser refuses
  its whole notebook, since the parser reads the notebook in one call. `skipped` lines follow the
  notebook's entries, not their place in the table of contents.
- onenote_parser holds a notebook whole in memory before its first line is written, and decompresses
  a `.onepkg` whole. A OneDrive-format section parses every cell of its storage index, unreferenced
  ones included: [#48](https://github.com/msiemens/onenote.rs/issues/48) reports a 375 MB section of
  3,232 cells, 373 of them reachable, needing over 9 GB. The spike measured its 161 MB sample at 135
  MiB, and acceptance measured docproc-onenote 0.1.0 at 6.9 MiB on it
  ([`docs/acceptance.md`](../../docs/acceptance.md), "docproc-onenote: the findings"). The tool
  streams media through a fixed buffer, adding no copy of its own. Size a host by the largest
  notebook or section it is given.
- Byte identity has three edges in the parser's hash and directory order, which no committed
  fixture reaches: a section group holding more than one `.onetoc2` is read through the first
  the directory, or the package's map, yields; a OneDrive-format file holding more than one
  storage index or manifest is read through the first its map yields; and a section with two
  faults, its object spaces parsed in map order, reports either.
- The read runs on a thread whose stack is sized at startup from the process's soft
  address-space limit (RLIMIT_AS), since a stack overflow kills the process by signal with no
  line written: 256 MiB with no limit, and under one, a quarter of it, at least 8 MiB and at
  most 256 MiB. No flag or environment changes it. The stack is an address-space reservation:
  its pages are touched only as deep as a read goes, so it costs no resident memory beyond that,
  and the rest of a limit, docproc's memory bound among them, is left to the read. A limit too
  tight for even the 8 MiB stack leaves a thread that can't start (exit 4). At the 256 MiB cap
  the stack allows outlines nested about 118,000 levels deep, and the depth scales with the
  stack under a limit: about 7,400 levels at a 64 MiB limit's 16 MiB, about 3,700 at the 8 MiB
  floor. That depth was measured on the x64 release musl binary, under gdb, from the stack
  pointer at each level of `New Section 1.one`'s nested outline: a level takes 2,272 bytes in
  the tool's reader, 1,776 in onenote_parser, 400 in the Markdown renderer, and at most 64 in
  the drop of the parsed page, so the reader, the deepest, bounds it: its first level begins
  5 KiB below the stack's top. A table nested in a table cell passes through more frames per
  level, and no fixture nests one, so its depth is unmeasured and lower; arm64's frames are not
  measured.
- A page with no stored modification time reports its creation time as its
  `modification_time`, as the parser gives it. An untitled page's `title` is its first line, as
  the parser gives it and OneNote's page list shows it.
- The Markdown's order differs from `media`'s by design: the Markdown places containers by their
  offsets, top to bottom, then left to right, and `media` follows the reader's stored order.
- A password-protected section is refused, or skipped, never read: [MS-ONESTORE] 2.5.19 leaves
  the encryption scheme undocumented. The encryption signal is established for the desktop
  format only; no encrypted OneDrive-format fixture exists.
- Page history isn't read, only a page's latest revision, and merged table cells aren't exposed
  by the parser, so each cell is read as its own.
- An image with no stored extension is named for its filename's, so a desktop printout's PNG
  bytes can be written as `.pdf`: route a printout image by its byte signature, not its
  extension.
- onenote_parser's open issues
  [#39](https://github.com/msiemens/onenote.rs/issues/39), a section failing after a text
  container is copied in from another, and
  [#50](https://github.com/msiemens/onenote.rs/issues/50), a missing page space failing the whole
  section, apply. Anything the parser prints to stdout is lost; the warnings of its reports are
  not.
- No OneNote-written `.onepkg` is tested: packages need desktop OneNote on Windows, so the tests
  build their own.

## Building and testing

The toolchain (Rust with both musl targets, jq, shellcheck, cargo-deny, cargo-about) is pinned in
[`mise.toml`](mise.toml) alone. Its tasks run from this directory, `mise run <task>`, or from the
repository root, `mise -C tools/onenote run <task>`; docproc's own `mise run check` runs this tool's
check. The reasoning behind its configuration is on
[`docs/tool-builds.md`](../../docs/tool-builds.md).

- `check`: verifies formatting, lints with warnings as errors, and runs the tests of
  docproc-onenote and of the shared crate, [`tools/envelope`](../envelope), against the static
  musl release binary, overflow checks on, from the locked crate graph. The tests compare that
  binary's stdout and stderr, run from `tests/fixtures/` with relative inputs, for every committed
  `.one` and `.onetoc2` against its `.jsonl` and `.stderr` goldens under `tests/golden/`, and
  interop's `SUT/Open Notebook.onetoc2` read with `--file-only` against its refusal's.
- `golden`: re-records the goldens from the release binary. The check never writes them, so a
  change to the output is reviewed as a diff of them.
- `publish`: builds the release binary for this machine into `.state/publish/`;
  `tools/pack.sh onenote <version>` runs it and packs the archive, refusing a binary whose
  `--version` isn't `<version>`.
- `currency`: reports crates and mise tools behind their latest, and crates with a RustSec
  advisory. A pin the hold list, `HOLDS` in [`mise.toml`](mise.toml), names is reported as held,
  with its reason: typed-path, until onenote_parser moves. It reads the network, so CI never
  runs it.
- `upgrade`: bumps what `currency` reports as behind to its latest, but a held pin. Review the
  diff and run `check`; docproc's own `mise run upgrade` runs it.

The fixtures are public, each set with its license beside it and listed in its `sources.json`
with its source path, pinned commit, license, and SHA-256, which the tests check:
[Interop-TestSuites](https://github.com/OfficeDev/Interop-TestSuites)' MS-ONESTORE files at
`fe87ed32`, under the MIT License, `tests/fixtures/interop/`, among them the password-protected
`Encryption.one`; and onenote_parser's own samples at `v2.1.0` (`727389a`), under the MPL-2.0,
`tests/fixtures/onenote-rs/`, among them a section group, a notebook with a recycle bin,
OneDrive-format sections, and `handwriting_recognition.one`. The hand truth, written apart from
the tool's output, is `tests/fixtures/sections.json`, `notebooks.json`, and `content.json`, the
Markdown expectations of `tests/markdown.rs`, and the unit tests. Each `.onepkg` is built by the
tests through cab's MSZIP writer from a committed notebook directory, and reads as that
directory's golden in every line but the first.

The version Cargo.toml records must be the release tag's and that of
[`CHANGELOG.md`](CHANGELOG.md)'s newest release section.

Apache-2.0. The release binary also holds onenote_parser 2.1.0, under the MPL-2.0, its source at
<https://github.com/msiemens/onenote.rs> (tag `v2.1.0`), and code ported from one2html 2.0.1,
under the MIT License; `THIRD-PARTY-NOTICES.txt` gives each one's notice.
