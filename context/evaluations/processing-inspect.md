# Evaluation · processing, inspection and routing (spike-ai-pipeline-processing v0.9.0)

The four packages are sound to carry: `route` and `inspect` are on the standard library alone,
their signatures already match `layers.md`, no stage of the layer ever writes to a stored name,
and `sevenzip` wraps the `7zz` program rather than a Go dependency, so the module stays on
`golang.org/x/text`. The weaknesses are in what the checks claim against what they hold. The
three most consequential findings: the bomb rule is per entry and conjunctive at thresholds that
sit at Deflate's ceiling, so 2 TiB declared across 2,000 entries, or one 100 GiB entry at 999:1,
proceeds, and the only other bound, `dispatch.Limits`, is none by default (PI-1); an XML encoding
declaration other than UTF-8 ends the SVG and relationship scans at their first token, so a
scripted SVG or a remote-template `.rels` yields no flag and still goes to its stage (PI-2); and
`sevenzip`'s one-item `Extract` makes a solid 7z, 7-Zip's default, cost one decode of its block
per entry, measured at about 1.2 s an entry against 5.4 s for the whole of a 2,000-file archive
(PI-3).

Evidence was read from the spike at tag `v0.9.0` with a clean tree. Behaviour marked "ran" was
checked on a scratch copy of `route`, `inspect`, and `sevenzip` with Go 1.27.4 and the pinned
7zz 26.05; nothing was written in the spike. Paths are relative to the spike's root. The one
external fact, 7-Zip's CVE-2024-11477, was read from ZDI-24-1532.

## Findings

### PI-1 · The bomb rule passes an archive that expands without bound
- **Kind:** issue
- **Weight:** high
- **Where:** inspect/zip.go:67-70; inspect/tar.go:60-63; inspect/listing.go:119-122;
  inspect/inspect.go:211-219 (`DefaultBombRatio`, `DefaultBombSize`); dispatch/dispatch.go:97-117
  (`Limits`, "none by default")
- **Against:** data-intelligence.md's inspect stage; inspect/doc.go:2-4 (unsafe is "content no
  stage may open without harm to the host"); baseline-standards.md (a default bound is a
  starting point the application overrides)
- **Finding:** An entry is a bomb only past both 1,000:1 and 1 GiB, each entry weighed alone.
  Ran: a 185,802-byte ZIP of 2,000 entries, each declaring 1 GiB less one byte from 1 MiB, 2 TiB
  in all, yields no flag and proceeds; one entry declaring 100 GiB at 999:1 proceeds too. Deflate
  tops out near 1,032:1 (the spike's own `bomb.zip` is "about 1,030:1"), so for ZIP and gzip the
  default ratio catches only runs of a single byte, and a little noise takes an entry under it.
  The archive stage applies the same two thresholds to the bytes it writes, so it stops neither
  case. Nothing sums: a ZIP's declared compressed sizes may exceed the file (overlapping
  entries), and the total expanded size is never compared with anything. What bounds the disk is
  `Limits.Bytes`, which is zero unless the host sets it.
- **Proposed design:** Keep the per-entry rule and its two options. Add the archive's own total
  to the same rule: the sum of the entries' declared expanded sizes against the archive's size,
  under the same ratio and size, raising the kind's bomb check with the total as evidence; and a
  ZIP whose declared compressed sizes sum past the file's size is a bomb outright. The stage
  holds the bytes it writes for one archive to the same total. `dispatch.New` documents that
  `Limits.Bytes` is the only absolute bound, and Cerebro's and `cmd/docproc`'s configuration set
  it; acceptance dispatches a many-entry bomb and a 999:1 one.
- **Carried by:** library; acceptance

### PI-2 · An XML encoding declaration hides an SVG's script and a package's external relationship
- **Kind:** issue
- **Weight:** high
- **Where:** inspect/svg.go:22-33; inspect/zip.go:125-136
- **Against:** inspect/doc.go:20-24 (the SVG and OOXML checks); svg.go:14-17 ("reads leniently,
  as a browser would")
- **Finding:** Both scans use `encoding/xml` with no `CharsetReader`, and return at the first
  `Token` error. A declaration naming any encoding but UTF-8 is that error. Ran: an SVG with an
  `onload`, a `<script>`, and a remote `<image href>` yields all three flags with no declaration
  or `encoding="UTF-8"`, and none with `ISO-8859-1`, `us-ascii`, or `windows-1252`; it is still
  kind `svg`, routed vision-converted, and goes to the picture stage's renderer. A `.rels` with
  an `attachedTemplate` external target yields `external-relationship` as UTF-8 and nothing as
  `ISO-8859-1` or as UTF-16, which OPC allows. A UTF-16 SVG is kind `utf-16 bom`, so it is
  routed text and never scanned. No fixture declares another encoding, so no test shows it.
- **Proposed design:** Both decoders take a `CharsetReader` over `golang.org/x/text`'s
  `encoding/ianaindex`, the module's one requirement, and a UTF-16 byte order mark is decoded
  before the SVG root is sought, in `route.Identify`'s `isSVG` and in the scan. A scan that ends
  on a decode error before the document's end raises a flag of its own, active
  (`svg-unreadable`, and the relationships part by name), so "checked as far as it decodes" is
  visible in the report rather than silent. Fixtures: `script.svg` and `macro.docm` each in
  ISO-8859-1 and UTF-16.
- **Carried by:** library

### PI-3 · A solid 7z costs one decode of its block per entry
- **Kind:** issue
- **Weight:** high
- **Where:** sevenzip/sevenzip.go:132-155 (`Extract`, one item); sevenzip/doc.go:31-32;
  dispatch/sevenzip.go:113-134, 176-179; docs/findings/formats.md:123-128, 356-357 ("none was
  measured")
- **Against:** none in `layers.md` (`sevenzip` is "kept"); the library task's summary treats the
  same shape in audio (a decode from the start per window) as a defect to bound
- **Finding:** `Reader.Extract` writes one named item per 7zz run, and the archive stage calls it
  for every file. 7-Zip writes solid archives by default, and one item of a solid block is
  reached by decoding the block from its start. Ran with 7zz 26.05 on a 2,000-file, 259 MB
  (211 MB packed, LZMA2, four solid blocks) archive: the first item extracts in 0.006 s, an item
  mid-block or at the end in 1.2 to 1.3 s, and the whole archive in one run in 5.4 s. The stage's
  loop is therefore about 20 minutes for what one run reads in seconds, and it grows with the
  square of the block. The spike's only solid fixture is 41 KB.
- **Proposed design:** `sevenzip.Reader` gains the whole-archive read the stage needs and loses
  nothing of its safety: one `7zz e -so` run with no name, its standard output split by the
  listing's sizes, in the listing's order (ran: a four-file archive writes its items
  concatenated in listing order), yielded as `iter.Seq2[Entry, io.Reader]` or written through a
  callback per entry. 7zz is still never given a path to write to, each item is still held to
  its listed size and CRC-32, and an item the listing leaves unsized, or an encrypted one, falls
  back to today's single-item run. The single-item `Extract` stays for a ZIP's entry of another
  method. The library task checks the order claim against a multi-block and a mixed-method
  archive before relying on it.
- **Carried by:** library

### PI-4 · A report depends on the name, and the index records it by content alone
- **Kind:** issue
- **Weight:** medium
- **Where:** inspect/doc.go:8-10 ("an index may record it by the content's SHA-256");
  inspect/inspect.go:375-389 (`names`), 441-455 (`mismatch`); inspect/scan.go:82, 99;
  inspect/inspect.go:339; dispatch/dispatch.go:421, 479, 688
- **Against:** layers.md, "Dispatch" ("a part's flags are recorded by its SHA-256 … a seen part
  keeps the verdict it was given"); "Index" (`Flags`, `RecordFlags` by `sha256`)
- **Finding:** `signature-mismatch`, `executable-extension`, and `double-extension` are functions
  of the name, and the name's extension also gates three content scans: the PDF names for a
  `.pdf` that isn't a PDF, the SVG scan for content of no signature named `.svg`, and
  `ooxml-encrypted`. The index keys flags by SHA-256 and a seen part takes them from it, so the
  same bytes seen first as `report.pdf` and later as `report.pdf.exe` carry no executable flags
  the second time, and a part can carry evidence quoting a name it doesn't have. Under a policy
  that quarantines a name check the verdict is wrong too.
- **Proposed design:** Split the report by what it is a function of, inside the kept signature.
  Content checks depend on the kind and the bytes only: the PDF scan runs for kind PDF, the SVG
  scan for kind SVG, and the encrypted-package check for OLE2 whatever the name. Name checks are
  a pure function, `inspect.Name(name string, kind route.Kind) []Flag`, with no I/O. The
  dispatcher records the content flags by SHA-256 and computes the name flags for every
  occurrence, seen or not, and weighs the verdict over both. `layers.md`'s open question, whether
  a changed policy re-inspects seen content, gets the same answer for free: the verdict is
  recomputed from recorded flags under the current policy, since `Options.verdict` reads flags
  alone.
- **Carried by:** library; architecture (layers.md's "Dispatch" paragraph)

### PI-5 · The 7zz floor admits the zstd decoder's known memory-corruption defect
- **Kind:** issue
- **Weight:** medium
- **Where:** sevenzip/sevenzip.go:19-21 (`SevenZipMinimum = "24.01"`), 63-75; sevenzip/doc.go:6-8
- **Against:** rolling-currency.md; STANDARDS.md ("An adapter checks its tool's version when it
  is constructed, so a missing or outdated tool fails at startup")
- **Finding:** The minimum is 24.01, chosen as "the first release whose zstd decoder reads a zstd
  stream". CVE-2024-11477 (ZDI-24-1532) is an integer underflow in exactly that decoder, rated
  remote code execution, fixed in 24.07. `NewReader` therefore accepts 24.01 through 24.06 to
  decode untrusted zstd streams and ZIP zstd entries. The pin is 26.05, so the check's own runs
  are unaffected; the floor is what a host's image is held to. The listing parser also relies on
  7zz replacing a line break in a stored name with `_` (ran on 26.05: `a\nb.txt` lists as
  `a_b.txt`), which no test holds and no older release was checked for; doc.go:29-31 describes
  the other behaviour.
- **Proposed design:** The floor is the oldest release with no known defect in a decoder the
  package uses, and no lower than 24.07; the simpler rule that fits rolling currency is the
  floor equal to the pinned release's minor at each bump. A fixture with a line break in a name
  holds the `_` replacement, and doc.go states what the listing shows. The tool-packaging image
  pins the same release.
- **Carried by:** library; tool-packaging

### PI-6 · Evidence can be megabytes long, invalid UTF-8, and hold a NUL
- **Kind:** issue
- **Weight:** medium
- **Where:** inspect/zip.go:146 (`"%s: %s %s"`, the part's name and the target unquoted);
  inspect/zip.go:32 (`err.Error()`); inspect/svg.go:38, 48 (element and attribute names);
  inspect/svg.go:77-90 (`clip`, used for an href only)
- **Against:** layers.md, "Inspection and routing" (`Flag` is a wire shape: "a part's shape
  carries it") and "Index" (`RecordFlags`, in PostgreSQL through `docproc/postgres`)
- **Finding:** Entry names are quoted with `%q` everywhere but in the external-relationship
  evidence, which writes the ZIP entry's name raw, and only an SVG href is clipped. Ran: a
  package whose `.rels` part is named `w\xff\x00ord/_rels/x.rels` yields evidence that is not
  valid UTF-8 and holds a NUL; `encoding/json/v2`, which dispatch uses beside v1, refuses to
  marshal it ("invalid UTF-8 within /Evidence"), and a PostgreSQL text or jsonb column refuses
  the NUL. A relationship whose target is 1 MiB yields 1,048,612 bytes of evidence. Dispatch
  treats an index failure as fatal to the dispatch (dispatch/dispatch.go:421-424), so one crafted
  docx could stop an input once the index is PostgreSQL. Evidence is also joined into a
  quarantined event's `Reason`.
- **Proposed design:** One unexported constructor makes every evidence string: untrusted text
  goes through `%q` and `clip`, and the whole is cut to a stated maximum (the 120 bytes
  `maxEvidence` already names, per quoted value). `Flag`'s doc comment states the guarantee:
  valid UTF-8, no control characters, bounded. A test inspects a package and an SVG with hostile
  names and holds the guarantee on every flag.
- **Carried by:** library

### PI-7 · An archive is quarantined whole for entries no stage could be harmed by
- **Kind:** issue
- **Weight:** medium
- **Where:** inspect/inspect.go:145-163 (`classes`); inspect/tar.go:46-55; inspect/zip.go:58-66;
  dispatch/tar.go:17-25, 101; dispatch/sevenzip.go:163-173; sevenzip/doc.go:15-17
- **Against:** inspect/doc.go:2-4 (unsafe is "content no stage may open without harm to the
  host"); data-intelligence.md (extraction over share drives)
- **Finding:** A symbolic or hard link, a device, and a traversing or absolute name are class
  unsafe, so one such entry quarantines the archive and none of its entries is read. Yet the
  archive stage writes every entry into its own directory by SHA-256, makes no link and no node,
  and already records a link or device entry as "no part" when a policy lets the archive proceed
  (dispatch/tar.go:101, dispatch/sevenzip.go:163-173); 7zz is never given a path to write to. By
  the package's own definition these entries harm no stage. Symbolic links are ordinary in
  source tarballs and backups, and absolute names in tars written with `-P`, so real archives
  lose all their content to a flag about one entry. `~/docs` holds no such archive, so no
  finding measured it.
- **Proposed design:** The link, device, traversal, and absolute-path checks become class
  active: flagged on the archive, the entry recorded with its check as the stage already does,
  the other entries read. Bombs and unreadable directories stay unsafe. A host that wants the
  old behaviour names the checks in `Options.Policy`. If the architect keeps them unsafe,
  inspect/doc.go's definition changes to say so ("unsafe to extract by name"), since the code and
  the definition disagree today.
- **Carried by:** library (the architect's call on the default)

### PI-8 · The options are validated per call, defaulted twice, and held three times
- **Kind:** simplification
- **Weight:** medium
- **Where:** inspect/inspect.go:236-256 (`Options.check`); dispatch/archive.go:167-183
  (`bombRule`); dispatch/dispatch.go:119, 130-151 (`New` never checks `Inspect`);
  dispatch/archive.go:45; dispatch/document.go:58; inspect/zip.go:67-68, inspect/tar.go:60-61,
  inspect/listing.go:119-120 (the rule's arithmetic, three times, and again in dispatch's
  `bombWriter`)
- **Against:** validation-first.md (a scope validates once, at its boundary); roadmap.toml, the
  library task's backlog ("inspect exports no options check")
- **Finding:** `bombRule` checks its options through `inspect.CheckListing(route.Zstd, 0, nil,
  opts)` and then repeats `check`'s defaulting by hand. `dispatch.New` validates nothing of
  `Config.Inspect`, so a policy naming an unknown check fails every part at its inspection
  rather than the construction. `ArchiveConfig.Inspect` and `DocumentConfig.Inspect` are second
  and third copies that "should be the dispatcher's" (dispatch/doc.go:165), and nothing holds
  them equal.
- **Proposed design:** inspect exports the rule, not just a check:
  `func (o Options) Validate() error`, matching `ErrOptions`, and
  `func (o Options) Bomb() BombRule` with `BombRule{Ratio float64; Size uint64}` and
  `func (b BombRule) Past(expanded, against uint64) bool`. Inspect's three sites and dispatch's
  writer call `Past`; `dispatch.New` calls `Validate`; `bombRule` and the `CheckListing` detour
  go. The stage configs take a `BombRule` (or the stages read the dispatcher's options through
  the wiring in `stages`), so there is one `Options` value.
- **Carried by:** library

### PI-9 · The unit tier runs real programs, reads `~/docs`, and walks other packages' fixtures
- **Kind:** issue
- **Weight:** medium
- **Where:** sevenzip/sevenzip_test.go:1, 67-78, 110-283 (untagged but for `linux`; `reader`
  fails without the pinned 7zz); inspect/clean_test.go:20, 75-80 (runs `mail`, imports `email`
  and `tool`); inspect/docs_test.go:61-127 and route/real_test.go:122 (`~/docs`);
  route/real_test.go:33; mise.toml:28-48
- **Against:** tests-and-docs.md, "The unit tier: black-box and hermetic" and "Integration and
  acceptance suites" (a suite that needs a real process sits behind a build tag)
- **Finding:** `go test -race ./...`, the check's unit tier, lists and extracts through the real
  7zz, reads every mail fixture through the real mail binary from inside `inspect`'s tests, and
  inspects and routes the architect's `~/docs` when the machine has it, so the tier's result
  depends on the machine. In docproc the mail binary is `tools/mail`'s build, so the library's
  unit tier would need a Rust build first. `inspect` and `route` tests also walk
  `../office/testdata`, `../document/testdata`, `../picture/testdata`, `../tool/testdata`, and
  `../email/testdata`, which the office and email changes re-seat.
- **Proposed design:** Unit tier: `sevenzip`'s parser and request tests over the stand-in that
  `TestMain` already provides, and `inspect`'s and `route`'s over their own `testdata`. Behind
  `-tags integration`: `TestList`, `TestExtract`, `TestExtractStops`, `TestListRefused`, and
  `TestCleanAttachments`. The `~/docs` tests move to the acceptance task's corpus run, which
  names the corpus by a variable rather than the home directory. The clean-fixture sweep becomes
  one test in the package that owns the fixtures' meaning (dispatch's or `stages`'), not a walk
  out of `inspect`.
- **Carried by:** library; acceptance

### PI-10 · Routing refuses content its stage would read
- **Kind:** issue
- **Weight:** medium
- **Where:** route/route.go:178-209, 216-237, 337-378; route/identify.go:179; layers.md:1816
- **Against:** layers.md, "Changes per tool and per package" → docproc-ooxml ("exact content
  type decides the format"); roadmap.toml, the library task's backlog (a bare RFC 822 file)
- **Finding:** Four cases, each ran. A ZIP that is an OOXML package but is named without an
  OOXML extension (a hash name, an attachment named `ATT00001`, `report.docx.`) is routed
  archive and exploded into XML parts, though the tool now decides its format from the
  package's content types and `inspect` already recognises the package by
  `[Content_Types].xml`. An `.eml` that begins with a UTF-8 byte order mark is "not
  header-shaped" and unrouted, since `fieldName` reads the mark as part of the first field. A
  message with no extension and no stored type is unrouted (the backlog's case), while prose
  with no extension that begins "From " is routed nested as an mbox. A PDF whose `%PDF-` is not
  at offset 0 (one leading newline, or a byte order mark) is unrouted or text, though PDF readers
  accept the header anywhere in the first 1,024 bytes; `~/docs`' `header-no-version-info.pdf`
  shows the flag side of it.
- **Proposed design:** `route.File` reads a ZIP's central directory when its name has no OOXML
  extension, and routes office when it holds `[Content_Types].xml` (the reason naming the
  content type). `headerShaped` skips a leading UTF-8 byte order mark. Content of no signature
  and no text extension is nested when its header is a message's by a stricter shape than
  `.eml`'s: a first-line field, and both a `From` and a `Date` (or a `Received`) before the
  block ends. `Identify` names a PDF whose header begins within the first 1,024 bytes after
  white space or a byte order mark. Each gets a fixture; the mbox rule is unchanged.
- **Carried by:** library

### PI-11 · Two- and three-byte signatures outrank a text extension
- **Kind:** issue
- **Weight:** medium
- **Where:** route/identify.go:199 (`ID3`), 209 (`MZ`), 210 (`#!`); route/route.go:154-157,
  190-192, 203-205
- **Against:** route/identify.go:163-164 ("the strong signatures are checked before the weak
  ones"); route/route.go:238-242 (the mbox rule already lets a text extension decide first)
- **Finding:** `byKind` returns before the text extension is read for every kind in
  `kindRoutes`. Ran: `codes.csv` beginning `MZ,Mozambique` and `notes.txt` beginning `MZ is` are
  kind `pe`, unrouted as "an executable", and flagged `executable`; `ids.csv` beginning
  `ID3,name` is routed audio as an MP3; `readme.md` beginning `#!` is unrouted as a script. The
  mbox signature already defers to a text extension; these three don't.
- **Proposed design:** Make the signatures as strong as their neighbours rather than add an
  extension rule: a PE is `MZ` with `PE\0\0` at the offset its header's `e_lfanew` names, within
  the head; an ID3 tag is `ID3`, a version byte under 0xFF twice, and four size bytes each under
  0x80; `#!` is followed by `/` after optional spaces. Each has a negative case in
  `TestIdentify`.
- **Carried by:** library

### PI-12 · The encrypted-package check and the extension tables exist twice, and disagree
- **Kind:** simplification
- **Weight:** low
- **Where:** route/route.go:93-100, 180-189, 273-293; inspect/scan.go:50, 99-101;
  inspect/inspect.go:431-436, 441-446; route/route.go:77-81; inspect/inspect.go:365-367;
  route/route.go:178
- **Against:** none: simplification
- **Finding:** `route` finds an `EncryptedPackage` directory entry at a 128-byte boundary with
  its terminating null; `inspect` finds the same UTF-16 bytes anywhere, unterminated. So a
  renamed `.doc` whose text holds the word is unrouted by one and `ooxml-encrypted` by the
  other. Each reads the whole file for it, after the dispatcher has read it once to hash it.
  `ooxmlExtensions` is declared in both packages with different members (`.xlsb`), and the two
  read a name's extension differently: `inspect` trims trailing dots and spaces, `route` doesn't
  (ran: `report.docx.` is archive to `route` and `.docx` to `inspect`).
- **Proposed design:** `route` owns both facts and `inspect` imports them, as it already imports
  `route`: an exported `route.Ext(name string) string` (trimmed, lowercased) and one
  encrypted-package detector that `inspect` calls. One table of OOXML extensions, in `route`.
  With PI-4, `ooxml-encrypted` no longer needs the name at all.
- **Carried by:** library

### PI-13 · The PDF name scan flags random bytes in a large PDF and sees no object stream
- **Kind:** issue
- **Weight:** low
- **Where:** inspect/scan.go:19-27, 181-251; inspect/doc.go:60-61
- **Against:** inspect/doc.go:8-10 (a report an index records); data-intelligence.md's inspect
  stage
- **Finding:** The scan reads names across the whole file, compressed streams included, as
  pdfid does. `/JS`, `/AA`, and `/EF` are three bytes and a delimiter, about one chance in 268
  million per byte each. Ran: 512 MiB of random bytes after a PDF header yields `pdf-aa` and
  `pdf-js` ("and 2 more"). So a scanned PDF of a few hundred megabytes is likelier than not to
  carry a false `pdf-js`, `pdf-aa`, or `pdf-embeddedfile`, and a host that quarantines
  `pdf-js` by policy quarantines it. The other direction is documented: a catalog in a
  compressed object stream, which current writers produce by default, shows none of its names.
- **Proposed design:** Keep the scan as the cheap pre-stage check, but skip stream data: between
  `stream` and `endstream` the scanner reads nothing, which removes the false positives and
  costs a second two-word pattern. The names that matter in object streams are the document
  stage's to raise, since it already runs qpdf and stages can add flags
  (dispatch/stage.go:81-86); see "For the synthesis".
- **Carried by:** library

### PI-14 · Exported identifiers nothing outside the package's tests uses
- **Kind:** simplification
- **Weight:** low
- **Where:** inspect/inspect.go:166-177 (`Checks`, `ClassOf`), 199-202 (`Report.Has`);
  route/identify.go:90-92 (`PNGSignature`, one caller, document/ghostscript.go:283);
  sevenzip/listing.go:42-46, 74-78 (`Entry.Packed`, `Listing.Type`, `Listing.PhysicalSize`);
  sevenzip/sevenzip.go:21, 27, 36, 52 (`SevenZipMinimum`, `SevenZipSpec`, `ErrNoSevenZip`,
  `Config.SevenZip`); doc.go:1-4
- **Against:** baseline-standards.md, "A type is introduced only when it is needed";
  topology-and-naming.md (the identifier in code stays the package's)
- **Finding:** `Checks`, `ClassOf`, and `Has` are called only from tests; dispatch's tests build
  an `inspect.Report{Flags: …}` just to call `Has`. `Packed`, `Type`, and `PhysicalSize` are
  parsed and read by no caller. `sevenzip.SevenZipSpec` and `sevenzip.SevenZipMinimum` repeat
  the package's name. The root `package processing` is a doc comment with no symbol, and
  `layers.md`'s package list has no root package.
- **Proposed design:** Keep `Checks` only if `cmd/docproc` or Cerebro lists the vocabulary (a
  policy's configuration needs it; say so in its comment), drop `ClassOf` (a flag carries its
  class), and drop `Has` or move it to `[]Flag`'s user. Drop the three unread listing fields.
  `sevenzip.Spec()` and `sevenzip.Minimum`, if the other adapters' names allow one convention
  (synthesis). No root package in docproc, or a `package docproc` doc.go that only points at the
  packages.
- **Carried by:** library

### PI-15 · Inspect's memory and time are not bounded for a ZIP
- **Kind:** issue
- **Weight:** low
- **Where:** inspect/doc.go:10-12 and inspect/inspect.go:287-292 ("its memory bounded whatever
  the file's size"); inspect/zip.go:30, 16-18, 110-112, 125-131; sevenzip/sevenzip.go:114-121
- **Against:** the package's own contract
- **Finding:** `zip.NewReader` holds every central directory record, so memory follows the
  entry count, not a constant: millions of empty entries fit in a file of tens of megabytes.
  Each `.rels` part is decompressed up to 16 MiB with no total across the package and no
  context check inside one, so a package of thousands of highly compressed `.rels` parts makes
  Inspect decompress and tokenise gigabytes before any stage. `Reader.List` buffers 7zz's whole
  listing in memory.
- **Proposed design:** Read the end-of-central-directory record's entry count before
  `zip.NewReader` and raise `zip-unreadable`, with the count as evidence, past a bound in
  `Options` (zero selecting a default). One budget for all of a package's relationship parts,
  the scan stopping with the flag PI-2 adds when it is spent. `List` parses 7zz's output as it
  streams and stops past the same entry bound. The doc comment then says what is bounded.
- **Carried by:** library

### PI-16 · Tests that restate the table they check
- **Kind:** issue
- **Weight:** low
- **Where:** inspect/inspect_test.go:120-122; inspect/listing_test.go:56; inspect/inspect_test.go:344-373
- **Against:** tests-and-docs.md (a test proves a behavior only if it can fail)
- **Finding:** `f.Class != inspect.ClassOf(f.Check)` compares two reads of the same unexported
  map, so it cannot fail. `TestClasses` lists the unsafe checks again and compares the list with
  the map: it fails only when someone edits one and not the other, which proves the edit, not a
  behaviour. The behaviour, that an unsafe flag quarantines and an active one proceeds, is
  already held by `TestSynthesized`'s verdicts.
- **Proposed design:** Drop the two class comparisons, and with PI-14 `ClassOf`. Replace
  `TestClasses` with the verdict each class comes to under the zero `Options`, over one fixture
  per class, which `TestSynthesized` nearly is. `inspect/testdata/README.md` lists each
  fixture's SHA-256 with no test holding it; either add the sums test `sevenzip` has or drop the
  column.
- **Carried by:** library

### PI-17 · Each contract is stated three times
- **Kind:** simplification
- **Weight:** low
- **Where:** inspect/doc.go:13-46, 68-90; inspect/inspect.go:42-139, 281-292; route/doc.go:14-46;
  route/route.go:19-64, 163-172
- **Against:** tests-and-docs.md, "doc.go and godoc" (the inventory has a one-clause role per
  identifier; "each contract is stated once, on its symbol's own doc comment")
- **Finding:** `inspect/doc.go` states every check's rule in full, then inventories the checks;
  each constant's comment states the rule again, and `Inspect`'s comment a third time. `route`
  does the same with its routes. The copies have already drifted in one place:
  sevenzip/doc.go:29-31 describes a listing behaviour 7zz 26.05 doesn't have (PI-5).
- **Proposed design:** `doc.go` keeps the two classes, the two entry points, and the inventory,
  one clause each; each check's and route's rule lives on its constant. The README links to
  godoc rather than restating the rules.
- **Carried by:** library

### PI-18 · ClamAV does not fit Inspect's contract
- **Kind:** issue
- **Weight:** low
- **Where:** inspect/doc.go:8-10, 49-51; inspect/inspect.go:239-242; dispatch/dispatch.go:479, 688
- **Against:** roadmap.toml, the library task's backlog ("ClamAV could become an `inspect`
  check")
- **Finding:** Inspect runs no program, and its report is a function of the bytes, so the index
  keeps it by SHA-256 for good. A scanner's answer is a function of its signature database too,
  and a seen part is never inspected again. The check vocabulary is also closed: `Options.check`
  refuses a policy naming a check outside `classes`, so a host cannot give a policy to a check
  the package doesn't declare.
- **Proposed design:** Not an `inspect` check. If the deployment needs a scan, it is either
  Cerebro's, on the staged blob before the job is queued, with no docproc change; or a tool
  adapter the dispatcher runs as a stage-like step whose identity carries the database's
  version, so a new database makes content unseen. The backlog comment is reworded to say
  which, and the architect decides when a requirement names it.
- **Carried by:** roadmap

### PI-19 · Formats the spike left unrouted are not in the task's backlog
- **Kind:** issue
- **Weight:** low
- **Where:** docs/findings/formats.md:132, 364; route/identify.go:178-219;
  inspect/zip.go:58-66 (a ZIP entry's device mode unchecked, where a tar's and a 7z's are)
- **Against:** roadmap.toml, the library task's backlog (lists ODF, WordPerfect, `.xlsb`, ODG
  only)
- **Finding:** The formats finding names RAR, ISO, and bare xz and bzip2 streams as "outside the
  task", and `Identify` names none of them, so `.tar.xz` and `.tar.bz2`, commoner than
  `.tar.zst`, are unrouted. 7zz reads all four and the standard library reads bzip2. ASF (`.wmv`,
  `.wma`), MPEG program and transport streams, AIFF, and ADTS AAC have no kind either, and an
  `.m4a` whose major brand is `mp42` or `isom` is kind `mp4`, routed video, and flagged
  `signature-mismatch`. None of this reached the roadmap's backlog.
- **Proposed design:** Add a backlog comment naming them. xz and bzip2 are the cheap pair: a
  kind each, routed archive, read as zstd is (one unnamed item through 7zz, or `compress/bzip2`),
  with the stream bomb check PI-8's `BombRule` already gives.
- **Carried by:** roadmap

## For the synthesis

- **One vocabulary, three spellings.** The four 7-Zip formats are `route.Kind` (`SevenZip`,
  `MSCF`, `Zstd`, `ZIP`), `sevenzip.Format` (`SevenZ`, `Cabinet`, `Zstd`, `ZIP`), and inspect's
  check prefixes, joined by a table in dispatch/sevenzip.go:17-36. `inspect.Listed` and
  `EntryType` mirror `sevenzip.Entry`, with dispatch/sevenzip.go:56-68 and 107-111 converting.
  In one module `inspect.CheckListing` could take `sevenzip.Listing` (its types file builds on
  the standard library alone), or `sevenzip.Format` could be a `route.Kind`. `layers.md` keeps
  `CheckListing`'s signature, so this is the synthesis's to weigh, not a finding.
- **Checks are format times rule.** Traversal, absolute path, symlink, hardlink, device, bomb,
  and encrypted exist per format (ZIP, tar, 7z, cab), 24 constants and a dispatch table for
  seven rules, and PI-19's formats would add more. The names are wire vocabulary and policy
  keys once Version "1" ships, so whether a policy ever needs `tar-symlink` apart from
  `zip-symlink` should be settled before then.
- **Checks inspect declares and never raises.** `GzipBomb`, `PDFAttachmentBomb`, and the
  as-written 7z, cabinet, and zstd bombs are raised by dispatch as `string(inspect.X)` into
  record metadata (dispatch/archive.go:223-229, tar.go, zip.go, document.go:265), where the type
  is lost. The dispatch group should say whether records carry `inspect.Flag` itself.
- **Three reads of each file.** The dispatcher hashes the input, `route.File` opens it again
  (and reads an OLE2 whole), and `Inspect` opens it a third time and streams it for PDFs,
  non-containers, and OLE2. `route.File` takes no context. If the dispatch group reshapes the
  spool, `Identify`'s head and the scan could ride the hashing pass.
- **PDF structure belongs to the stage that has qpdf.** PI-13's blind spot (names in object
  streams) and `pdf-encrypt` on a PDF that opens without a password (`encryption-nocopy.pdf`)
  are answerable by the document stage, which could raise flags from qpdf's view; dispatch's
  `encrypted` (dispatch/dispatch.go:648-652) reads `pdf-encrypt` today.
- **`Limits` is the real bomb bound** (PI-1), and it is the dispatch group's type; its default
  and Cerebro's values should be reviewed together with the bomb rule.
- **Adapter conventions.** `NewReader` re-checks the minimum its `Spec` already declares
  (sevenzip/sevenzip.go:63-72), `Config` is a one-field struct, and the `<Tool>Spec`,
  `<Tool>Minimum`, `ErrNo<Tool>` names stutter; the same shape is in `email` and the other
  adapters, so one decision covers them all.
- **Fixtures cross packages.** dispatch reads `inspect/testdata` and `sevenzip/testdata`, and
  `inspect` and `route` read office's, document's, picture's, tool's, and email's (PI-9). The
  office and email re-seating should name one home for shared fixtures.
- **HTML is unrouted by design** (route/route.go:106-109), while docproc-mail holds an HTML to
  Markdown conversion; saved web pages and `.xls` files that are HTML are common on share
  drives. Outside this group, but no task carries it.
