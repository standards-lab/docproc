# goal · docproc-coverage

- **State:** building
- **Task:** text-encoding-keys
- **Branch:** text-encoding-keys

## Tasks

1. [ ] text-encoding-keys
2. [ ] archives-and-mpeg
3. [ ] pdf-text-padding
4. [ ] office-soffice
5. [ ] ooxml-unread
6. [ ] ooxml-arm64-floor
7. [ ] tnef
8. [ ] xps
9. [ ] web-content

## Task brief · text-encoding-keys

**Problem**

The text stage records how it decoded a text in one combined value, such as
`windows-1252 (fallback)` or `utf-16le (byte order mark)`. A reader has to parse that string to
get the charset back, and a reader that looks the string up as an encoding name finds nothing.
The architect settled that the value splits into two keys before the coverage work adds new text
sources. The task also brings every trailing dependency current, so the goal's later tasks build
on current dependencies.

**Behaviors**
1. The text output carries `encoding`, the charset alone, in lowercase: one of `utf-8`,
   `utf-16le`, `utf-16be`, `utf-32le`, `utf-32be`, or `windows-1252`.
2. When a byte order mark chose the encoding (UTF-8, UTF-16, or UTF-32), the output also carries
   `encoding_by` = `bom`.
3. When the stage fell back to windows-1252, no mark and not valid UTF-8, the output carries
   `encoding` = `windows-1252` and `encoding_by` = `fallback`; valid UTF-8 followed by a stray
   byte takes the same pair.
4. When the whole text is valid UTF-8 with no mark, an empty text included, the output carries
   `encoding` = `utf-8` and no `encoding_by`.
5. No `encoding` value holds a space or a parenthesis.
6. The text, its outputs, its warnings and their wording, and pass-through are unchanged; the
   U+FFFD warning names the charset alone.
7. The text stage's identity version moves from "2" to "3", so an index's earlier text entries
   are prepared again instead of replayed.
8. Run-directory record lines carry the two keys. Dispatch's wire stays "1" and the run schema
   stays "2"; the API docs and dispatch's shape docs record this as an exception to the
   addition-only rule, with its reason.
9. dispatch exports the new key beside the existing encoding key; the shape docs define both
   keys, their values, and when `encoding_by` is absent, and the API's text section names the
   pair.
10. docproc's currency command exits 0, or reports only held items.

**Test seams**
- The text stage's Prepare: each case asserts the output's meta map exactly.
- One `docproc run` record line over a fallback text, asserting the two keys.

**Slices**
1. **Upgrade.** sqlate v0.8.0, sqlate/postgres v0.9.0, and sqlint v0.5.0; gotenberg
   9.10.1-libreoffice, tag and digest together; uv 0.13.10; htmd 0.6.1; cargo-deny 0.22.1;
   crc32fast 1.5.6. MimeKit 5.0.0, MsgKit 3.1.0, and typed-path 0.13.0 stay held. Done when
   currency reports only those holds and the check passes; mail's and onenote's lock files move
   without a tool release.
2. **Split the key** (Behaviors 1–7), one test case per value written today. The check passes.
3. **Record the shape** (Behaviors 8–9). The proof is a record line from a fallback text; the
   CHANGELOG lists the split under v0.5.0 as breaking, with the old-to-new mapping.
4. **Dependent modules.** storage, postgres, and cmd/docproc require v0.5.0, and each CHANGELOG
   says what reaches it.

**Out of scope**

Encoding detection (docproc-backlog); any other stage's meta and any tool's contract;
docs-corpus's coverage reader, corpus.docs's to fix; a wire or schema version bump; docproc-mail
and docproc-onenote releases for their lock-file moves.

**Door**

One-way: the tags reach readers outside the repository, and new runs no longer carry the combined
value. Everything short of the pushed tags is two-way.

**Release**

`v0.5.0`, `storage/v0.1.3`, `postgres/v0.1.3`, `cmd/docproc/v0.2.3`

## Progress

slices 3/4 committed (4 at SHIP) · standards f61df1f, 1efe45e · spec no gaps · editor done
- slice 3: cmd/docproc reaches the unreleased base through a temporary replace (27b1246, 1e0bff1);
  slice 4 runs at SHIP on `text-encoding-keys-require` once v0.5.0 is tagged, as #27 did.

## Decisions

- archives-and-mpeg: bzip2 streams through compress/bzip2, beside gzip, not 7zz.
- archives-and-mpeg: split RAR volumes stay unread, an unread part with a reason; docproc-backlog.
- web-content: HTML and EPUB through pandoc, sandboxed, to gfm without raw HTML; rejected a Go
  converter (third-party dependencies in a base module) and a Rust tool on htmd (a release
  pipeline).
- xps: XPS converts to PDF through mutool, then the document stage; rejected libgxps (no release
  since 2021) and GhostXPS (Windows binaries only).
- text-encoding-keys: docs-corpus's tools/coverage, the one reader of the combined value, decodes
  outputs by it and is corpus.docs's to fix (outputs are always UTF-8).
- text-encoding-keys: held MimeKit at 4.18.2: 5.0.0 declined, PstBuilder.Eml 1.3.0 is built on
  MimeKit 4.
- text-encoding-keys: held MsgKit at 3.0.6: 3.1.0 declined, it needs MimeKit.Core 5.
- text-encoding-keys: held typed-path at 0.12.4: 0.13.0 declined, onenote_parser 2.1.0 requires
  ^0.12.3.

## Pending edits

- coordinator · context/data-intelligence.md, "Carried notes": HTML and EPUB are planned in
  docproc-coverage; MHTML and WARC stay unplanned.
- docs-corpus · once text-encoding-keys ships, tell the corpus.docs session the released tags and
  the run-directory schema the release writes ("2").
