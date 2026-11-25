# goal · docproc-coverage

- **State:** building
- **Task:** archives-and-mpeg
- **Branch:** archives-and-mpeg

## Tasks

1. [x] text-encoding-keys
2. [ ] archives-and-mpeg
3. [ ] pdf-text-padding
4. [ ] office-soffice
5. [ ] ooxml-unread
6. [ ] ooxml-arm64-floor
7. [ ] tnef
8. [ ] xps
9. [ ] web-content

## Task brief · archives-and-mpeg

**Problem**

docproc leaves RAR, xz, and bzip2 archives unread: they route to no stage. It also mishandles
two video formats. An MPEG program stream routes nowhere. An Ogg file holding video goes to the
audio stage, so its frames are never taken. `~/docs` holds both videos, and the goal's criteria
need them prepared. 7zz already reads RAR and xz, and Go's standard library reads bzip2. The
video stage already reads any file ffmpeg can open, so the video formats only need routing.

**Behaviors**
1. A RAR4 archive (`Rar!\x1a\x07\x00`) and a RAR5 archive (`Rar!\x1a\x07\x01\x00`) route to the
   archive stage, each as its own kind.
   - The stage lists and reads each through 7zz's matching RAR handler, and each entry becomes a
     part.
   - Entries get the checks a 7z's entries get: unsafe names, links, devices, the bomb rule, the
     entries bound, and the bytes cap.
2. A RAR whose first header marks it as one volume of a split set ends with no processor yet. The
   reason names it a split RAR volume.
   - No 7zz run happens for it, so no sibling volume is ever opened.
   - A RAR5 volume whose headers are encrypted hides that mark, so it ends Encrypted instead.
3. A RAR whose headers are encrypted ends Encrypted. An encrypted entry in a RAR with clear
   headers becomes an unread child with the Encrypted outcome, and its siblings are still read.
4. An xz stream (`FD 37 7A 58 5A 00`) routes to the archive stage and becomes one part, read
   through 7zz.
   - The part is named by stripping `.xz`; `.txz` becomes `.tar`.
   - The part must match the size the stream declares, and the bomb rule applies as the content
     is written.
5. A bzip2 stream (`BZh`, a digit 1–9, then the bzip2 block or end-of-stream marker) routes to
   the archive stage and becomes one part, read through Go's `compress/bzip2`.
   - Concatenated streams are read whole.
   - The part is named by stripping `.bz2`; `.tbz2` and `.tbz` become `.tar`.
   - The bomb rule applies as the content is written.
6. A tar inside xz or bzip2 becomes a `.tar` part and is read as a tar, as a gzipped tar is
   today.
7. A stream that begins with the MPEG pack start code `00 00 01 BA`, MPEG-1 or MPEG-2, routes to
   Video. The video stage prepares it as it prepares any video, with frames and audio windows.
8. An Ogg file whose first pages name Theora, VP8, Dirac, or OGM video is a new Ogg-video kind
   and routes to Video. Any other Ogg keeps its current kind and routes to Audio.
9. The 7zz minimum is 26.06; an older 7zz is refused as too old.
10. Nothing routed today changes route. HTML stays unrouted, the head route reads stays 4,096
    bytes, and ISO and UDF images stay unrouted.
11. Run records carry the new kind values. Because of Behavior 8, a record for an `.ogv` now shows
    the Ogg-video kind where it used to show `ogg`.
12. The limits page:
    - names only ISO and HTML as routing to no stage;
    - states in its archive section that split RAR volumes stay unread.

    The tools table states 7zz 26.06 and why.

**Test seams**
- Route's identification over synthetic heads, one case per signature and per boundary: each RAR
  version, the volume flag, both MPEG pack versions, each Ogg video codec, and Ogg audio.
- The archive stage's preparation, driven through a dispatcher:
  - over committed fixtures with the real 7zz, in the integration tier;
  - over a stand-in reader, in the unit tier.
- The every-fixture golden in the integration tier.
- One `docproc run` over the two `~/docs` videos.

**Slices**
1. **Route the new kinds** (Behaviors 1, 4, 5, 7, 8, and 10, route's part only). Every new kind
   is identified and routed. Existing routes are unchanged, and `~/docs`'s `.ogg` stays audio.
   The check passes.
2. **RAR and xz through 7zz** (Behaviors 1–4, 6, and 9).
   - 7zz's RAR4, RAR5, and xz handlers; the minimum moves to 26.06.
   - libarchive v3.8.11 RAR4 and RAR5 test archives (plain, encrypted, multi-volume, symlink),
     recorded by commit and SHA-256 like the existing libarchive rows.
   - xz fixtures from the generator through the pinned 7zz: plain, `.tar.xz`, and a bomb.
   - Done when each fixture is prepared with its entries as parts and the every-fixture golden
     holds them.
3. **bzip2 through the standard library** (Behaviors 5 and 6). bzip2 fixtures from the
   generator: plain, `.tar.bz2`, and a bomb. Done when each is prepared, with its entries as parts
   where it holds a tar.
4. **Video** (Behaviors 7, 8, and 11). The proof is a `docproc run` over
   `duck-and-cover-mpeg2.mpeg` and `duck-and-cover-theora.ogv`, each prepared with frames and
   windows.
5. **Docs and release** (Behavior 12).
   - The limits page, the tools table, the route and sevenzip package docs, and the changelog
     under v0.6.0, the new kinds listed as added.
   - After `v0.6.0` is tagged, storage, postgres, and cmd/docproc require v0.6.0, and each
     changelog says what reaches it.

**Out of scope**

- ISO 9660 and UDF images (docproc-backlog).
- Reading split RAR volumes (docproc-backlog).
- MPEG transport streams and raw MPEG video.
- RAR self-extractors, which stay unrouted as executables.
- Any change to HTML's routing, and all of the text-as-the-last-resort task's work.
- The goal manifest's changes, which land through their own `plan docproc-coverage` pull request.
- Tool releases.

**Door**

One-way at the pushed tags, which reach readers outside the repository and change an `.ogv`
record's kind. Everything short of the tags is two-way.

**Release**

`v0.6.0`, `storage/v0.1.4`, `postgres/v0.1.4`, `cmd/docproc/v0.2.4`

## Progress

slices 5/5 committed · standards ✓ · spec ✓ · editor ✓
- plan: rounds 2–4 curated the goal; a `plan docproc-coverage` session follows this task to
  change the manifest.
- escalation, during slice 1: docs-corpus's report that a `#!` script with an appended ZIP
  isn't flagged polyglot (inspect exempts route.Shebang from the scan) goes to
  text-as-the-last-resort, not this task? yes

## Decisions

- goal: XPS (mutool) and web-content (pandoc) moved to docproc-backlog; the
  text-as-the-last-resort task replaces web-content, right after archives-and-mpeg. Under it,
  content with no signature, message shape, text extension, or byte order mark whose head passes
  route's UTF-8 text check routes Text, the HTML-is-not-text rule goes, and a `#!` script routes
  Text. Whether the text-extension list can go entirely (a Windows-1252 sniff; precedence over
  mbox and header-sniffed messages) is that task's first planning question.
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
- docs-corpus · tell corpus.docs the new kind values (RAR4, RAR5, xz, bzip2, MPEG program
  stream, Ogg video), that `.ogv` records change kind from `ogg`, and the released tags.
- coordinator · context/data-intelligence.md, "Carried notes": supersedes the line above; HTML
  and EPUB are read as raw text under text-as-the-last-resort; MHTML and WARC stay unplanned.
- coordinator · roadmap/docproc-coverage.toml, via `plan docproc-coverage`: the
  text-as-the-last-resort task's summary also covers inspect's polyglot scan on `#!` scripts and
  text kinds, exempt today (docs-corpus's exec-4 gap, reopened once docproc flags it).
- coordinator · roadmap/docproc-backlog.toml, via `plan docproc-coverage`: add ISO 9660 and UDF
  images, unrouted while route's head stays 4,096 bytes; XPS, converted to PDF through mutool for
  the document stage (rejected libgxps, no release since 2021; GhostXPS, Windows binaries only);
  HTML and EPUB through pandoc, sandboxed, to gfm without raw HTML (rejected a Go converter,
  third-party dependencies in a base module; a Rust tool on htmd, a release pipeline); and split
  RAR volumes, which docproc leaves unread.
