# docproc-mail

docproc's system tool for mail: a command-line utility that reads one Outlook `.pst` store,
through Microsoft's [outlook-pst](https://github.com/microsoft/outlook-pst-rs), one `.msg`
message, through its own MS-OXMSG reader over [cfb](https://crates.io/crates/cfb), or one `.eml`
message or mbox file, through Stalwart's [mail-parser](https://crates.io/crates/mail-parser), and
writes it to stdout as JSON Lines, schema `"2"`, with each message's Markdown rendering inside its
line as the text layer. A store's folder tree is read, each message in the folder it is stored in,
with its addresses, times, and threading fields as stored; its attachments are written out as
media files. It builds no threads; docproc does.

It is one static musl binary each for linux-x64 and linux-arm64, built with Rust, needing no
glibc on the host, for docproc to run like any other system tool. It runs no inference: it calls
no model, and the text it extracts is complete without one.

docproc-mail began as spike-ai-pipeline-mail v0.3.1; its findings, the fields it recovers per
kind and its time and peak memory per store, stay in that spike
([`docs/findings/mail-cli.md`](https://github.com/JaimeStill/spike-ai-pipeline-mail/blob/v0.3.1/docs/findings/mail-cli.md)).

## Install

Each release, tagged `tools/mail/vX.Y.Z`, carries two archives and their checksums:

- `docproc-mail-vX.Y.Z-linux-x64.tar.gz`
- `docproc-mail-vX.Y.Z-linux-arm64.tar.gz`
- `SHA256SUMS`, covering both.

Each archive holds the `docproc-mail` binary, the repository's `LICENSE`, and
`THIRD-PARTY-NOTICES.txt` for the crates the binary is built from. Check the archive against
`SHA256SUMS` before unpacking it:

```sh
sha256sum --check --ignore-missing SHA256SUMS
tar -xzf docproc-mail-v0.1.0-linux-x64.tar.gz
./docproc-mail --version
```

The binary is static: it links musl and requires no glibc symbol (see [Limits](#limits) for what
that costs).

## Usage

```sh
docproc-mail [--media <dir>] [--after <id>] <input>
docproc-mail --version
docproc-mail --help
```

It reads the one `<input>` and writes its lines to stdout, and nothing else there. The arguments
may come in any order. The input's format is sniffed from its content, never from its name, in
this order: a PST header; an OLE2 compound file, which must hold an MS-OXMSG message; a first line
starting `From `, an mbox's separator line; else an RFC 5322 header block, an `.eml`: a first line
that is a header field, and a `From` or `Date` field before the block ends. A leading UTF-8
byte-order mark is skipped.

With `--media <dir>`, each distinct attachment content, and each `data:` image the Markdown
links, is written once into `<dir>`, created once the input is read, as stored, before the line
that names it. Its name is the lowercase hex SHA-256 of its bytes and its stored extension,
its case kept, when that is 1 to 8 ASCII letters and digits. Without `--media` nothing is written
to disk and the lines name the files all the same. An attachment by reference has no file, and
its target is never fetched.

With `--after <id>`, a read of a `.pst` or an mbox resumes after the message whose `id` is given:
in a `.pst` its node id, in an mbox its 1-based ordinal, empty entries counted. The resumed read
is the tail of the whole read, byte for byte: the store line, every folder line, and every
`skipped` line of a folder or of a folder's table, all as the whole read writes them, since a
consumer places messages by their folders; and of the messages, only the lines after the given
message's. The messages up to and including it are passed over unread, with no warnings and no
media: a `.pst`'s by their node ids in their folders' contents tables, never opened; an mbox's by
scanning its separator lines, never parsed. The id is decimal digits naming a whole number from 1
to 4,294,967,295; anything else, or `--after` with a `.msg` or an `.eml`, a single message, is a
usage error. An id that names no message is refused.

`--version` prints the version on its first line and the schema on its second:

```text
0.1.0
schema 2
```

`--help` prints the usage, the three lines that open this section, to stdout and exits 0.
`--version` and `--help` each come alone; with any other argument either is a usage error. A
stdout that can't take their output exits 1.

## Output

A `.pst` gives a `store` line, then each folder depth-first from the IPM subtree's root: its
`folder` line, its messages' `message` lines in its contents table's order, then its subfolders.
A `.msg` and an `.eml` give a `store` line and one `message` line. An mbox gives a `store` line,
held until its first message parses, then each message in file order; it has no folders. A
`skipped` line stands wherever a folder, a folder's contents or hierarchy table, a message, or an
attachment can't be read.

```text
{"line":"store","schema":"2","format":"pst","encoding":"unicode","password":false,"display_name":"Personal Folders"}
{"line":"folder","id":32802,"path":["Top of Personal Folders"]}
{"line":"message","id":2097188,"folder":33090,"class":"IPM.Note","subject":"Welcome to Microsoft Office Outlook 2003",…,"markdown":"…"}
```

- `store`: the schema, the `format` (`pst`, `msg`, `eml`, or `mbox`), and a `.pst`'s `encoding`
  (`unicode` or `ansi`), `password` flag, and `display_name`.
- `folder`: its node `id`, its `parent`, its `path` of display names from the IPM subtree's root,
  and its `container_class`.
- `message`: whatever its message class, its `id` and `folder` where it has them, its class,
  subject, `sender`, `to`, `cc`, and `bcc`, its submit, delivery, creation, and modification
  times in ISO 8601 UTC, importance, sensitivity, raw transport headers, and threading fields as
  stored (`message_id`, `in_reply_to`, `references`, `conversation_topic`, `conversation_index`),
  then its `markdown`, its `attachments`, and its optional `media`. An address has a `name`, an
  `address`, and an `address_type` (`SMTP`, `EX`), and an Exchange (`EX`) address its
  `smtp_address` where the input holds it: a recipient's `PidTagSmtpAddress`, the sender's
  `PidTagSenderSmtpAddress`.
- An attachment has its `file`, `filename`, `mime_type`, `content_id`, `inline`, `method`, and
  `size`; in a `.msg`, an `.eml`, or an mbox, an embedded message is nested under its `message`.
  `inline` means the Markdown links it, in every format.
- `skipped`: the record's `kind`, its `folder`, `message`, and `id` where it has them, and the
  `reason`.

The Markdown is made from the first of a message's stored HTML, the HTML recovered from its RTF
body (`\fromhtml1`), its text body, then its RTF body's own text. Its images link media names: a
`cid:` source names the attachment whose `content_id` it is, any other source the attachment
whose Content-Location it is, and a `data:` image is written as media and listed in the message's
`media`, with no attachment record. A source that names nothing is kept as written, with a
warning. When the Markdown is made from HTML, the sentences of the message's text body, or of its
RTF body's text, that the Markdown lacks are appended to it, in order, as one paragraph, by the
content rule docproc's tools share (Unicode 18.0.0).

A field with no value is left out, never `null`, and so is an empty list. The same input gives
byte-identical stdout and media. The contract, field by field, is docproc's:
[`context/layers.md`](../../context/layers.md), "The tool contract" and its "docproc-mail"
section. From schema `"2"` on, it grows by addition only; any other change moves the schema, and
[`CHANGELOG.md`](CHANGELOG.md) records it.

## Exit codes and stderr

| Exit code | Meaning |
| --- | --- |
| 0 | Read, skipped records included. |
| 1 | stdout or the media could not be written, a broken pipe included. |
| 2 | A usage error; the usage follows the error line. |
| 3 | Refused before any output. |
| 4 | A fault of docproc-mail's own, never the input's. |

stderr holds one line each:

- `docproc-mail: warning: <where>: <what>` for what was skipped or read with a loss, escaped so
  the line splits at its first three `": "`. `<where>` names the record: in a `.pst`, `store`,
  or `folder <id>` followed by `, contents table` or `, hierarchy table`, or by `, message <id>`
  and `, attachment <n>` down to the record; in a `.msg` or an `.eml`, `message` followed by
  `, attachment <n>` and `, message` for each level of nesting; in an mbox, `message <n>`
  followed in the same way. `<what>` is `skipped: <reason>` for a skipped record.
- `docproc-mail: error: <reason>` for exit codes 1 to 4. The reason never leads with the input
  path.

A record that can't be read inside a readable input, whether its reader errs or panics, is a
`skipped` line and a warning, and the run goes on with exit code 0. So is a `.pst` folder a
hierarchy table names twice, `skipped: already read`. A warning also comes for a string decoded
lossily, a header value that doesn't map, an HTML body nested past 512 open elements (its
Markdown made from the text body, else the HTML's text), a compressed RTF body whose stated size
is more than nine times what its data can yield (not decompressed), an image source that names
nothing, a TNEF or opaque S/MIME part, and a `.pst` embedded message listed without its content.

A refusal comes before anything is written to stdout or the media directory. docproc-mail refuses
an input that can't be read; an input in none of the four formats, an mbox among them none of
whose entries is a message (an empty mbox is read, giving its store line); an `.ost`, the
offline cache of a server mailbox, to be exported from Outlook as a `.pst`; a PST version
outlook-pst doesn't read (it reads 14 and 15, ANSI, and 23, Unicode); a store whose IPM subtree
can't be read; a compound file that isn't an MS-OXMSG message, or whose top-level properties
can't be read; an `.eml` of 4 GiB or more, or one mail-parser can't parse; and an `--after` id
that names no message.

The envelope these follow, its cases stated once for every tool, is
[`tools/envelope/cases.json`](../envelope/cases.json), and the code that writes it is the shared
crate, [`tools/envelope`](../envelope).

## Limits

- In a `.pst`, an embedded message has no content: its attachment is listed with its metadata
  alone, with a warning, since outlook-pst 1.2.0 can't open one. In a `.msg`, an `.eml`, or an
  mbox it is read in full.
- No class-specific field is written: a contact, an appointment, or a task is a `message` line
  with the message fields alone.
- TNEF (`winmail.dat`) and opaque S/MIME (`application/pkcs7-mime`) parts are not read; each
  gives its message a warning. An `IPM.Note.SMIME` `.msg` or `.pst` message gets none.
- ANSI stores are read but untested: no ANSI `.pst` is among the fixtures.
- An embedded message in a `.msg`, an `.eml`, or an mbox is read to 16 levels of nesting; one
  deeper is listed without it, with a warning.
- Memory is bounded by the largest single attachment, a small multiple of it, and for an `.eml`
  by the file, which is read whole; an mbox is read one message at a time. Size a host by the
  largest attachment it is given.
- The static musl binary is slower than a glibc build would be, which points at musl's allocator.
  In the spike's finding
  ([`docs/findings/mail-cli.md`](https://github.com/JaimeStill/spike-ai-pipeline-mail/blob/v0.3.1/docs/findings/mail-cli.md),
  "The release binary against a glibc build" and ".eml and mbox"), it took 2.0 times a glibc
  build's time on the 2.1 GB scale store (13.27 s against 6.51 s), 2.3 times on plaso's smaller
  store (1.59 s against 0.71 s), and 1.7 times on the scale store's messages as a 2.7 GB mboxrd
  (6.86 s against 4.09 s), with less peak memory. It is released static all the same, so it runs
  on any Linux host.
- `.ost` files are refused, and an attachment by reference has no file.
- mail-parser decodes US-ASCII and ISO-8859-1 as windows-1252, and the five bytes windows-1252
  leaves undefined as U+FFFD, with a lossy-decoding warning in a body or an attachment but none
  in a header's encoded words.
- An mbox is read as an mboxrd: a line quoted `>From ` loses one `>`, so an mboxo's body line
  that was `>From ` to begin with loses its `>`. mboxcl's Content-Length is ignored, so an
  unquoted body line starting `From ` starts a new message.
- An `.eml` of 4 GiB or more is refused: mail-parser's offsets are 32-bit.
- A `.pst` item outlook-pst 1.2.0 can't open, such as a contact stored without a sub-node tree,
  is skipped. A folder's table that can't be read is told from an empty one only by the folder's
  own count, and its reason can't name the fault; a folder whose count claims no items while it
  has some loses them silently.

## Building and testing

The toolchain (Rust with both musl targets, jq, shellcheck, cargo-deny, cargo-about) is pinned
in [`mise.toml`](mise.toml) alone, and the fixture generator's .NET SDK in
[`mise.fixtures.toml`](mise.fixtures.toml). Its tasks run from this directory,
`mise run <task>`, or from the repository root, `mise -C tools/mail run <task>`; docproc's own
`mise run check` runs this tool's check.

- `check`: shellchecks the scripts, verifies formatting, lints with warnings as errors, and runs
  the tests of docproc-mail and of the shared crate, [`tools/envelope`](../envelope), against the
  static musl release binary, overflow checks on, from the locked crate graph. The tests compare
  that binary's stdout and stderr, at exit code 0, for every committed input under
  `tests/fixtures/` against its `.jsonl` and `.stderr` goldens under `tests/golden/`.
- `golden`: re-records the goldens from the release binary. The check never writes them, so a
  change to the output is reviewed as a diff of them.
- `publish`: builds the release binary for this machine into `.state/publish/`;
  `tools/pack.sh mail <version>` runs it and packs the archive, refusing a binary whose
  `--version` isn't `<version>`.
- `mboxrd`: writes the MIME group's mboxrd, `tests/fixtures/mime/nine.mboxrd`, from its nine
  `.eml`; the check rebuilds it and compares it byte for byte.
- `currency`: reports crates, mise tools, and the fixture generator's packages behind their
  latest, and crates with a RustSec advisory. A pin the hold list, `HOLDS` in
  [`mise.toml`](mise.toml), names is reported as held, with its reason. It reads the network, so
  CI never runs it.
- `upgrade`: bumps what `currency` reports as behind to its latest, but a held pin. Review the
  diff and run `check`; docproc's own `mise run upgrade` runs it.
- `fixtures`, with `MISE_ENV=fixtures`: regenerates the generated `.pst` and `.msg` fixtures and
  their manifest from their `.eml` and item sources, on .NET 11, MimeKit held at 4.18.2. Each
  other fixture is listed with its source, license, and SHA-256 in its group's `sources.json`.

The version Cargo.toml records must be the release tag's and that of
[`CHANGELOG.md`](CHANGELOG.md)'s newest release section.

Apache-2.0.
