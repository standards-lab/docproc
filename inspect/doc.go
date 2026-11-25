// Package inspect inspects a part before its stage, on the standard library, golang.org/x/text,
// and route alone, and says whether the part may go on to it. Unsafe means unsafe to read: content
// no stage may open without harm to the host. Content that could act, were something to run it,
// and content a stage leaves unread, is only flagged, since no stage of this layer executes
// content.
//
// [Content] reads content already open, its size, and its kind, as route.Identify names it, and
// returns a [Report]: the kind, its [Flag]s, each naming its [Check], its [Class], and its
// evidence, and the [Verdict] they come to under the [Options]. A content check depends on the
// kind and the bytes alone, so the same content gives the same report on every run and an index
// may record it by the content's SHA-256. [Name] returns a name's flags, reading nothing, so a
// dispatcher computes them at every name a part has. [Inspect] is both over the file at a path,
// for a caller that triages a file before queueing it. Content streams: its memory is bounded
// whatever the content's size, and a ZIP is read by its central directory and a tar by its
// headers, no entry extracted.
//
// Its two classes:
//
//   - [Active]: flagged, the part proceeding by default. Content that could act, since no stage
//     executes it: a polyglot, a ZIP's end-of-central-directory record inside content that isn't
//     a container, or a PDF header after another signature; executable content, by route's PE,
//     ELF, Mach-O, and "#!" kinds; an OOXML package's VBA project, macro sheet, ActiveX
//     controls, OLE objects, and external relationships other than hyperlinks, such as a remote
//     template or a linked picture; the PDF names pdfid counts, /JavaScript, /JS, /OpenAction,
//     /AA, /Launch, /EmbeddedFile, /RichMedia, /XFA, and /Encrypt, and /EmbeddedFiles and /EF,
//     which mark an embedded file whose stream names no type, hex-escaped forms such as
//     /J#61vaScript included, outside the PDF's stream data; and an SVG's script elements,
//     event handler attributes, hrefs that leave the document, and foreignObjects. A name's
//     findings: a signature that disagrees with its extension, an executable extension (.exe,
//     .dll, .js, .vbs, .scr, .bat, .cmd, .ps1, .lnk, .hta, .jar, .msi), and one after another,
//     as in "invoice.pdf.exe". Content a stage leaves unread: an archive entry whose name has a
//     ".." element, is absolute, or begins with a drive letter, a link, symbolic or hard, and a
//     device, a FIFO, or a socket, each entry an unread child, the others read; an encrypted
//     archive entry, an /Encrypt PDF, and an encrypted OOXML package, an OLE2 compound file
//     holding an EncryptedPackage stream whatever its name; and an SVG or an OOXML
//     relationships part whose scan ended early, what follows unseen.
//   - [Unsafe]: flagged, the part quarantined by default: no stage runs on it, its content is
//     kept, and it is reported with its evidence. A bomb: an archive entry past the bomb rule by
//     the sizes it declares, its ratio against its compressed size, or against the whole
//     archive's where its format shares one or declares none; an archive whose entries' declared
//     sizes sum past the rule against its own size; a ZIP whose entries' declared compressed
//     sizes sum past its own size, whatever the rule; and a gzip or zstd stream, or a PDF's
//     embedded file, past the rule as a stage writes it. An unreadable directory: a ZIP whose
//     central directory can't be read, or a tar whose headers can't all be, or either holding
//     more entries than the Options allow.
//
// The archive checks are seven rules, whatever the archive's format: [ArchiveTraversal],
// [ArchiveAbsolutePath], [ArchiveSymlink], [ArchiveHardlink], [ArchiveDevice], [ArchiveEncrypted],
// and [ArchiveBomb]. A stage that reads an archive through a program raises them too: it calls
// [CheckListing] with the program's listing of a 7z, a cabinet, a RAR, or a zstd or xz stream
// before it extracts anything, and raises ArchiveBomb on the bytes it writes.
//
// The Options carry no default bound. The zero Options weigh each flag by its class, apply no
// bomb rule but the one no ZIP passes, its declared compressed sizes against its own size, and
// bound no listing. [Options.Policy] overrides any check's default action, so a caller may
// quarantine a macro or a traversing name, or let a bomb proceed. A bomb rule, [Options.Bomb],
// is set by its ratio and its size together, and content is past it when it passes both.
// [Options.Entries] bounds a ZIP's or a tar's entries: a ZIP whose end record lists more is
// unreadable before archive/zip reads its central directory.
//
// The XML scans, an SVG's and an OOXML package's relationships parts', decode the charset the
// XML declares, by its IANA name through x/text's ianaindex, or UTF-16 by a byte order mark; a
// charset no decoder knows ends the scan early. A package's relationships parts share one budget
// of 16 MiB, expanded.
//
// The checks read what pdfid and a careful reader read, not everything: a PDF name inside a
// compressed object stream is unseen, its stream data skipped; legacy OLE2 documents' macros and
// ODF packages' scripts aren't checked; and a container's members are not inspected here, but as
// parts of their own once the stage that extracts them yields them, the stage enforcing the bomb
// rule on the bytes it writes, since a declared size may lie. A gzip stream is checked as a gzip
// no further: the tar a .tar.gz holds is inspected once the stage decompresses it.
//
// Its exported identifiers:
//
//   - [Inspect] inspects the file at a path, named; [Content] inspects open content of a kind;
//     [Name] checks a name. Each returns its [Report], or Name its flags.
//   - [CheckListing] checks an archive's listing, its [Listed] entries, each of an [EntryType],
//     [FileEntry], [DirEntry], [SymlinkEntry], [HardlinkEntry], or [DeviceEntry], and returns its
//     Report.
//   - [Options] tune them: a policy, a bomb rule, and an entry bound. [Options.Validate] refuses
//     invalid ones, matching [ErrOptions]; [Options.Bomb] returns the [BombRule], whose
//     [BombRule.Past] weighs content against it; and [Options.Verdict] is the verdict flags come
//     to.
//   - [Flag] is one finding, its [Check], [Class], and evidence, JSON by its tags.
//   - [Class] is [Active] or [Unsafe]; [Verdict] is [Proceed] or [Quarantine].
//   - [Check] names a check, by its rule. Of class Active: [SignatureMismatch], [Polyglot],
//     [Executable], [ExecutableExtension], and [DoubleExtension]; [VBAProject], [MacroSheet],
//     [ActiveX], [OLEObject], [ExternalRelationship], [RelationshipsUnreadable], and
//     [OOXMLEncrypted]; the PDF names, [PDFJavaScript], [PDFJS], [PDFOpenAction], [PDFAA],
//     [PDFLaunch], [PDFEmbeddedFile], [PDFRichMedia], [PDFXFA], and [PDFEncrypt]; [SVGScript],
//     [SVGEventHandler], [SVGExternalHref], [SVGForeignObject], and [SVGUnreadable]; and
//     [ArchiveTraversal], [ArchiveAbsolutePath], [ArchiveSymlink], [ArchiveHardlink],
//     [ArchiveDevice], and [ArchiveEncrypted]. Of class Unsafe: [ArchiveBomb], [ZIPUnreadable],
//     and [TarUnreadable]. [Checks] lists them, for a policy's configuration.
package inspect
