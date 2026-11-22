# Changelog

All notable changes to `docproc-onenote` are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the CLI adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `tools/onenote/vX.Y.Z` tag carries, and is the release's notes. Its JSON Lines, their
`schema` version included, the arguments, the exit codes, the warning format, the media names,
and the Markdown rules are its output contract: a change to them is a change here. This changelog
covers docproc-onenote only.

## [Unreleased]

### Changed

- The crate graph locks hashbrown 0.17.2, a patch release through lru, in place of 0.17.1. The
  output is unchanged.

## [v0.1.1] - 2026-11-19

### Changed

- The crate graph locks uuid 1.29.3 and pin-project-lite 0.2.18, patch releases, in place of
  1.29.2 and 0.2.17. The output is unchanged.
- An input that is missing, or a file or notebook directory without read permission, is
  refused, still exit 3, with a reason that says which and names the input's path as given,
  after it: `no such file: <path>` or `permission denied: <path>`, in place of the operating
  system's message alone. A directory is still read as a notebook. Every other refusal and
  warning reads as before.

## [v0.1.0] - 2026-11-16

The first release. docproc-onenote was copied, in one commit, from spike-ai-pipeline-onenote
v0.1.0 (8ea6b2d), its schema `"1"` CLI, renamed, and reworked into docproc's system tool; Changed
says how it differs from the spike. The spike's findings, its scorer, and its timing harness stay
in the spike.

### Added

- `docproc-onenote [--media <dir>] [--file-only] <input>`, `--version`, and `--help`, the
  arguments parsed by hand: a static musl binary on Rust 1.100.0 for linux-x64 and linux-arm64,
  overflow checks on in release, its crate graph locked, onenote_parser pinned exactly at 2.1.0.
  `--help` prints the usage to stdout and exits 0.
- `--file-only`: a table of contents, as a `.onetoc2` or its notebook's directory, is refused,
  exit 3, with the fixed reason `a table of contents; its sections are read on their own`,
  before any section is read.
- Schema `"2"` JSON Lines: a `notebook` line, then `section_group` and `section` lines
  depth-first, each section followed by its `page` lines, then a `skipped` line per section left
  out. Output is byte-identical for the same input, pinned by a `.jsonl` and a `.stderr` golden
  for every committed `.one` and `.onetoc2`, and a refusal's for a table of contents read with
  `--file-only`, all run against the release binary from the fixtures' directory.
- A page's `media`: its images, embedded files, and ink drawings, flattened, in the reader's
  visit order, each its `kind`, `file`, `filename`, `size`, `extension`, and `type` as it has
  them.
- A page's `ink_alternatives`: each recognised word with other guesses, by its page-wide index,
  with its alternatives, best first.
- The encryption signal: after a section's parse fails, its file is scanned for the two
  constants of an [MS-ONESTORE] 2.5.19 encryption key. Found, an input is refused with a reason
  beginning `encrypted`, and a notebook's section is a `skipped` line with that reason and
  `encrypted: true`.
- In the Markdown: the title's date and time paragraphs, joined by one space, as one line after
  the title heading; every note tag's label as `[Label]`, a task's after its box; a hyperlinked
  image as `[![alt](file)](hyperlink)`; an image's OCR sentences that its alt text lacks, after
  the alt text, by the content rule of the shared crate, `tools/envelope`, on Unicode 18.0.0;
  each ink drawing's alt text as the words its own strokes belong to, for page ink, paragraph
  ink, and ink blocks, and any recognised word no drawing carries on one plain line after the
  page's content.
- Warnings for a notebook read that reads no section or section group, at `/`, and for a page
  creation time the parser couldn't read.
- The read runs on a thread whose stack, an address-space reservation, is sized from the soft
  address-space limit at startup: 256 MiB with none, and under one, a quarter of it, floored at
  8 MiB and capped at 256 MiB. A thread that can't start at that size is exit 4, `internal
  fault: the read thread can't start`.
- The common envelope, through the shared crate: stderr lines `docproc-onenote: warning: <where>:
  <what>` and `docproc-onenote: error: <reason>`, escaped, a reason never leading with the input
  path; exit codes 0 read, 1 stdout or media not written, 2 usage, 3 refused before any output, 4
  a fault of its own.
- Releases: `docproc-onenote-vX.Y.Z-linux-x64.tar.gz` and `-linux-arm64.tar.gz`, each holding the
  binary, LICENSE, and THIRD-PARTY-NOTICES.txt, with `SHA256SUMS`; a release is refused unless the
  binary's `--version` is the tag's and it requires no glibc symbol. The notices give every
  crate's, the Rust standard library's and musl libc's, one2html 2.0.1's MIT notice for the
  ported math code, and where onenote_parser's MPL-2.0 source is.

### Changed

- `--version` prints the version, then `schema 2`, where the spike printed the version alone.
- A page's Markdown carries its text: `content` (containers, outlines, paragraphs, runs, tables,
  and tags), the page's `ink` record, and `ink_text` are gone, and with them an image's
  `alt_text`, `ocr_text`, `hyperlink`, and `printout_page`, an ink record's `strokes` and
  `bounds`, and a recognised word's `language`. A section's and a section group's
  `display_name`, its `path`'s last name, is gone.
- A page's `creation_time` is left out, with a warning, when it is the parser's 1970 fallback for
  a time it couldn't read, no longer written as `1970-01-01T00:00:00Z`.
- A password-protected section is refused, and skipped inside a notebook, as encrypted, no longer
  with the parser's `Unexpected end of file`.
- A refusal's reason no longer leads with the input's path. A `<where>` and a `<what>` are
  escaped by the shared writer, a colon followed by a space in `<where>` among them, so a
  section name holding `": "` no longer splits a warning wrong, and a parser message holding a
  line break stays one line.
- Ink words: each drawing carries its own strokes' words, no longer every word on the page's ink
  image alone, where a paragraph's handwriting and an ink block had none.
- Destinations are written by the shared destination writer, so one holding a space is a link,
  no longer text, and each reads back as its target, a backslash in it escaped, so a UNC path
  keeps both its leading backslashes; in a table cell, a `|` in a code span or an equation's
  LaTeX is escaped. Literal text is escaped by the shared literal-text escape.
- Media are streamed, hashed while copied through a fixed buffer into a temporary file renamed to
  the hash name, no longer read whole and written to their name directly, so a killed run leaves
  no truncated file under a full content's name. The media directory is created once the input is
  read, no longer at its first file.
- Nesting is read on the read thread's stack, up to 256 MiB, no longer the main thread's, on
  which deep nesting died by signal.
- An integer overflow in the parser or the tool is a panic, refused or reported, no longer
  wrapped silently, as the release binary now checks overflow.

### Known limits

- A directory or `.onetoc2` read leaves out a `.one` its table of contents doesn't list, and a
  listed section whose file is missing, with no warning.
- A section that fails inside a section group vanishes, and a section that panics the parser
  refuses its whole notebook.
- onenote_parser holds a notebook, and a decompressed `.onepkg`, whole in memory; a OneDrive-format
  section parses its whole storage index (issue #48).
- Byte identity has three edges in the parser's hash and directory order: a group's table of
  contents, a OneDrive-format file's storage index and manifest, and the order of two faults.
- The read thread's stack is a quarter of an address-space limit, from 8 MiB to 256 MiB; at the
  256 MiB cap it allows outlines nested about 118,000 levels deep on x64, and the depth scales
  with the stack under a limit. A limit too tight for the 8 MiB stack is exit 4.
- A page with no stored modification time reports its creation time; an untitled page's title is
  its first line.
- The Markdown's order differs from `media`'s by design.
- Password-protected sections are not read; the encryption signal is established for the desktop
  format only.
- Page history and merged table cells are not read.
- Linux alone, x64 and arm64.

[Unreleased]: https://github.com/standards-lab/docproc/compare/tools/onenote/v0.1.1...HEAD
[v0.1.1]: https://github.com/standards-lab/docproc/compare/tools/onenote/v0.1.0...tools/onenote/v0.1.1
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/tools/onenote/v0.1.0
