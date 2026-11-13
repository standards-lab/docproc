# Changelog

All notable changes to `docproc-mail` are documented here. The format follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/), and the CLI adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is headed by the version
its `tools/mail/vX.Y.Z` tag carries, and is the release's notes. Its JSON Lines, their `schema`
version included, the arguments, the exit codes, the warning format, and the media names are its
output contract: a change to them is a change here. This changelog covers docproc-mail only.

## [Unreleased]

## [v0.1.0] - 2026-11-13

The first release. docproc-mail was copied, in one commit, from spike-ai-pipeline-mail v0.3.1
(9cbbdd3), its schema `"1"` CLI, renamed, and reworked into docproc's system tool; Changed says
how it differs from the spike. The spike's findings, its scorer, and its scale fixtures stay in
the spike.

### Added

- `docproc-mail [--media <dir>] [--after <id>] <input>`, `--version`, and `--help`, the arguments
  parsed by hand: a static musl binary on Rust 1.100.0 for linux-x64 and linux-arm64, overflow
  checks on in release, its crate graph locked. `--version` prints the version, then `schema 2`;
  `--help` prints the usage to stdout and exits 0.
- Schema `"2"` JSON Lines: a `store` line, then a `.pst`'s `folder` lines, and a `message` line
  per message, with a `skipped` line wherever a record can't be read. Output is byte-identical
  for the same input, pinned by a `.jsonl` and a `.stderr` golden for every committed input, the
  MIME group's `.eml` files, its mboxrd and mbox, and the generated `.eml` files among them, all
  run against the release binary.
- `smtp_address` on an Exchange (`EX`) address: a recipient's from `PidTagSmtpAddress`
  (0x39FE), the sender's from `PidTagSenderSmtpAddress` (0x5D01), left out when absent.
- A message's optional `media`: each `data:` image its Markdown links, written as media with no
  attachment record.
- Markdown for a message whose only body is plain RTF or `\fromtext` RTF, read from the RTF's own
  text.
- The content rule: when the Markdown is made from HTML, the sentences of the text body, or of
  the RTF body's text, that the Markdown lacks are appended in order, as one escaped paragraph.
  It is the shared crate's, `tools/envelope`, on ICU4X 2.4.0 (Unicode 18.0.0) and pulldown-cmark
  0.13.6, with its own linear-space Myers alignment, falling back to sentence membership past
  10,000 edits.
- Warnings for an HTML body nested past 512 open elements, a TNEF part, an opaque S/MIME part,
  an image source that names nothing, and a `.pst` folder a hierarchy table names twice.
- Refusals: an mbox none of whose entries is a message, with nothing on stdout.
- The common envelope, through the shared crate: stderr lines `docproc-mail: warning: <where>:
  <what>` and `docproc-mail: error: <reason>`, escaped, a reason never leading with the input
  path; exit codes 0 read, 1 stdout or media not written, 2 usage, 3 refused before any output, 4
  a fault of its own.
- Releases: `docproc-mail-vX.Y.Z-linux-x64.tar.gz` and `-linux-arm64.tar.gz`, each holding the
  binary, LICENSE, and THIRD-PARTY-NOTICES.txt, with `SHA256SUMS`; a release is refused unless the
  binary's `--version` is the tag's and it requires no glibc symbol.

### Changed

- `--attachments <dir>` is now `--media <dir>`; the media directory is created once the input is
  read. `--after <id>` resumes as before.
- A message's Markdown is its `markdown` field; `body`, with its `text`, `html`, `rtf`, and
  `derived` bodies, is gone. A folder's `display_name`, its `path`'s last name, is gone.
- `inline` means the Markdown links the attachment, in every format, no longer `ATT_MHTML_REF`
  in MAPI or an inline Content-Disposition in MIME. Image sources are rewritten to media names: a
  percent-decoded `cid:` matched to `content_id`, any other source to the Content-Location.
  Attachments, a nested message's included, are read before the Markdown.
- The Markdown comes from the first of stored HTML, HTML recovered from the RTF, the text body,
  then the RTF's own text. A text body is escaped by the shared literal-text escape, no longer
  through html5ever and htmd.
- A `.pst` warning's `<where>` names folders by node id (`folder 32802, message 2097188,
  attachment 5`), no longer by display-name path; a colon followed by a space in `<where>` is
  escaped.
- A usage error is an `error:` line followed by the usage, and `--version` or `--help` with a
  stdout that can't be written exits 1.
- Faults stay within one record: the guard wraps the read, the mapping, and the Markdown, so a
  fault there is that record's `skipped` line, and stdout is flushed after each message. A `.pst`
  folder cycle is a `skipped` folder, `already read`, in the walk and the `--after` pre-walk. A
  compressed RTF whose stated size is more than nine times what its data can yield is not
  decompressed, with a warning.
- A leading UTF-8 byte-order mark is skipped. A `.pst` attachment is hashed and written from the
  reader's buffer, with no copy.
- PT_STRING8 is decoded in its code page through codepage and encoding_rs, with no byte-order
  mark sniffing; a code page they don't know is decoded lossily as windows-1252, with a warning.
  codepage-strings and the `.msg` named-property reader are gone.

### Known limits

- In a `.pst`, an embedded message has no content: outlook-pst 1.2.0 can't open one.
- No class-specific fields.
- TNEF and opaque S/MIME parts are not read.
- ANSI stores are read but untested: no ANSI `.pst` is among the fixtures.
- An embedded message in a `.msg`, an `.eml`, or an mbox is read to 16 levels of nesting.
- Peak memory is a small multiple of the largest single attachment, or of the file for an
  `.eml`.
- The static musl binary takes 1.7 to 2.3 times a glibc build's time, as measured in the spike's
  finding (spike-ai-pipeline-mail v0.3.1, `docs/findings/mail-cli.md`).
- Linux alone, x64 and arm64.

[Unreleased]: https://github.com/standards-lab/docproc/compare/tools/mail/v0.1.0...HEAD
[v0.1.0]: https://github.com/standards-lab/docproc/releases/tag/tools/mail/v0.1.0
