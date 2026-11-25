# goal · docproc-coverage

- **State:** building
- **Task:** text-last-resort
- **Branch:** text-last-resort

## Tasks

1. [x] text-encoding-keys
2. [x] archives-and-mpeg
3. [ ] text-last-resort
4. [ ] pdf-text-padding
5. [ ] office-soffice
6. [ ] ooxml-unread
7. [ ] ooxml-arm64-floor
8. [ ] tnef

## Task brief · docproc-coverage · text-last-resort

**Problem**

route sends text to the text stage only by an 18-entry extension list or a byte order mark, so
HTML, EPUB chapters, CSS, JavaScript, notes, LICENSE files, and `#!` scripts end Unrouted, and
~/docs leaves 47 such parts unread. inspect exempts `#!` scripts and content of no signature from
its polyglot scan, so a `#!` script with an appended ZIP is flagged executable only (docs-corpus's
exec-4 gap). Processors should claim formats by their own checks, and whatever none claims should
fall back to text.

**Behaviors**
1. Processors claim by signature and structure as today. Content that no processor claims, or
   that fails a claim's own check, routes Text when its first 4096 bytes hold no NUL byte, with
   the reason "no routed signature, text by its head". Content of no signature that holds a NUL
   there routes Unrouted, "no routed signature, not text".
2. The text-extension list goes: a name's extension never routes Text by itself. Content named
   `.txt`, `.csv`, `.json` and the rest routes by the fallback check like any other unclaimed
   content. The rule that a text extension outranks a short signature goes with it.
3. HTML, XHTML, CSS, JavaScript, EPUB entries (chapters, `content.opf`, `toc.ncx`, `mimetype`),
   and extensionless or unlisted text such as LICENSE, NOTICE, and transcripts route Text by
   behavior 1. An SVG still routes vision-converted by its root element, whatever its name.
4. A `#!` script routes Text, with the reason `"#!" line, a script`. Its kind stays `shebang`, and
   inspect still flags it Executable.
5. Deliberate refusals stay Unrouted with today's reasons: a PE, ELF, or Mach-O executable, an XPS
   package, and an `.ost`.
6. An OLE2 compound file with neither a legacy nor a `.msg` extension falls through to the fallback
   check. It holds NULs, so it routes Unrouted, "OLE2 signature without a legacy or .msg
   extension, not text".
7. An mbox is claimed only by a "From " separator line followed by a header block whose first line
   is a field and which holds From and either Date or Received, the header sniff's own test,
   read by route's header parser after the separator line. Content that begins "From " without
   such a block falls to the fallback check: "mbox \"From \" line without a message header, text
   by its head".
8. Content named `.eml`, or stored as message/rfc822 or message/global, whose header isn't
   message-shaped falls to the fallback check. If it passes, it routes Text, "a message by its
   <name or type>, but not header-shaped; text by its head". If it fails, it stays Unrouted with
   today's reason.
9. Content of no signature with a From plus Date or Received header block still routes Nested by
   the header sniff, whatever its name. Byte order marks still route Text by their mark.
10. A stored audio or video MIME type claims content of no signature only when the fallback check
    refuses it. Text stored as `audio/*`, such as a mislabelled mail attachment, routes Text.
11. inspect's ZIP end-of-central-directory scan reaches the `shebang` kind, content of no
    signature, and the three byte order mark kinds. A committed `#!/bin/bash` script with a stored
    ZIP appended is flagged polyglot, "ZIP end-of-central-directory record at offset <n> in a
    shebang". For content of no signature, the evidence says "in content of no signature" instead
    of an empty kind. Polyglot stays class Active, so the part proceeds.
12. The "PDF header after another signature" pattern still skips those kinds, so prose or code
    quoting `%PDF-` is not flagged.
13. No wire value is new: kinds, routes, checks, and dispatch's wire "1" are unchanged. The text
    stage's output and version are unchanged. Route reasons change as behaviors 1, 4, 7, 8, and 10
    state, and the tests and goldens that pin the old reasons are updated.
14. A docproc run over ~/docs prepares its HTML, its EPUB's chapters, and its extensionless or
    unlisted text files as text. Only content holding a NUL in its head, deliberate refusals, and
    the binary-only near-claims stay Unrouted.
15. The limits page no longer names HTML as unrouted. It states the fallback: unclaimed content
    without a NUL in its first 4096 bytes is read as text, BOM-less UTF-16 is refused, and legacy
    multibyte encodings are decoded as windows-1252. route's package documentation and its routes'
    comments state the claims and the fallback as built.

**Test seams**
- route's `At` and `File`: a route and reason for each behavior's fixture, built in the tests.
- inspect's `Content`: the polyglot flag and its evidence for the appended-ZIP script and for
  content of no signature.
- A docproc run over ~/docs at Verify for behavior 14. The unit tier reads no ~/docs.

**Slices**
1. Upgrade docproc: claude-plugins 0.24.0 → 0.24.1, and sha2 0.11.1 → 0.11.2 in the Rust tools.
   Done when currency reports only the held MimeKit, MsgKit, and typed-path, and the check passes.
2. route's text fallback: the no-NUL check for unclaimed content. The extension list, the
   short-signature rule, and the HTML rule go. `#!` routes Text. The OLE2 near-claim falls through
   with its reason. Deliberate refusals stay. Behaviors 1–6, 9, and 13.
3. Message and media claims: the strict mbox claim, the non-shaped message fallback, and stored
   media yielding to text. Behaviors 7, 8, and 10.
4. inspect's polyglot reach: the EOCD scan for shebang, no-signature, and BOM content, the
   evidence wording, and the appended-ZIP fixture. Behaviors 11 and 12.
5. Docs and changelog: limits, route's package documentation, the design review's note that HTML
   is unrouted (marked as superseded), and the base module's CHANGELOG Unreleased section.
   Behavior 15, with 14 verified by a run over ~/docs.

**Out of scope**

- A text retry for a part whose stage fails at runtime, such as a corrupt PDF: no stage falls back
  today. It goes to docproc-backlog as a pending edit.
- route's mbox claim skipping a leading UTF-8 byte order mark, as mail's sniff does: a note.
- Detecting legacy multibyte encodings: Shift-JIS, EUC-JP, and ISO-2022-JP text still decodes as
  windows-1252, and the mojibake stays a stated limit.
- Reading HTML or EPUB structurally (pandoc or similar): this task reads them as raw text.

**Door**

One-way for the tags: a pushed v0.7.0 and its require tags reach docproc's callers. Routing and
inspection are two-way until the tags are pushed.

**Release**
- `v0.7.0`
- then, in the text-last-resort-require pull request, `storage/v0.1.5`, `postgres/v0.1.5`, and
  `cmd/docproc/v0.2.5`, each with its CHANGELOG

## Progress

slices 0/5 committed · standards — · spec — · editor —

## Decisions

- goal: XPS (mutool) and web-content (pandoc) moved to docproc-backlog; text-last-resort replaces
  web-content, right after archives-and-mpeg, and reads HTML and EPUB as raw text.
- text-encoding-keys: docs-corpus's tools/coverage, the one reader of the combined value, decodes
  outputs by it and is corpus.docs's to fix (outputs are always UTF-8).
- text-encoding-keys: held MimeKit at 4.18.2: 5.0.0 declined, PstBuilder.Eml 1.3.0 is built on
  MimeKit 4.
- text-encoding-keys: held MsgKit at 3.0.6: 3.1.0 declined, it needs MimeKit.Core 5.
- text-encoding-keys: held typed-path at 0.12.4: 0.13.0 declined, onenote_parser 2.1.0 requires
  ^0.12.3.
- text-last-resort: route keeps no text-extension list. Processors claim formats by their own
  checks, and content no processor claims, or that fails a claim's check, routes Text when its
  first 4096 bytes hold no NUL. PE, ELF, Mach-O, XPS, and .ost stay Unrouted.
- text-last-resort: a `#!` script routes Text, keeping the kind `shebang` and the Executable flag.
- text-last-resort: an mbox is a "From " line followed by a header block with From and Date or
  Received. A .eml or stored message that isn't header-shaped falls back to text.
- text-last-resort: a stored audio or video MIME type claims only content the text fallback
  refuses. An OLE2 without a legacy or .msg extension falls through and stays Unrouted as not
  text.
- text-last-resort: inspect's EOCD polyglot scan reaches shebang, no-signature, and BOM content;
  the %PDF- pattern doesn't.

## Pending edits

- docs-corpus · tell corpus.docs the released tags, that `#!` scripts and other unlisted text now
  route text, and that exec-4's check:polyglot can reopen.
- coordinator · roadmap/docproc-backlog.toml: a text retry for a part whose stage fails at
  runtime, such as a corrupt PDF.
