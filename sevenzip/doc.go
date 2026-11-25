// Package sevenzip lists and reads archives through 7-Zip's 7zz: a 7z archive, a zstd or xz
// stream, a RAR4 or RAR5 archive, a Microsoft cabinet, and a ZIP's entries of methods archive/zip
// doesn't read, such as Deflate64, implode, LZMA, xz, zstd, and PPMd, each named by the route.Kind
// route identified it as. It provides pre-inference preparation only: the stages package's
// archive stage reads these kinds through it. It builds on every 64-bit platform: its types on
// the standard library and route alone, its Reader on this module's tool package, and the program
// it runs is a tool.Tool the caller built: 7-Zip's 7zz, SevenZipSpec declaring it, minimum 26.06,
// the mise pin. The package raises its minimum to the pinned minor when a decoder's defect is
// fixed past it: RAR's CVE-2025-53816, fixed in 25.00, and xz's CVE-2026-14266, fixed in 26.02,
// are past 24.07, the minimum that fixed zstd's CVE-2024-11477, and 26.03 through 26.06 fix
// vulnerabilities they don't name.
//
// A Reader, built over a Config of the Tool, runs 7zz for three things, each run under the
// caller's context. List reads an archive's headers through 7zz's technical listing, -slt, and
// returns its Listing: each item, a directory among them, as an Entry, its listed name, its
// declared size and CRC-32, whether it is encrypted, its method, the Unix mode its attributes
// carry, and a link's target, in the archive's order. The listing is parsed as 7zz writes it,
// and parsing stops, and 7zz with it, at an item past the request's Entries, the caller's
// inspect.Options.Entries. Read yields every item of a listed archive with its content: one
// 7zz run writes the content of the items it holds, split by the listing's sizes in its order,
// each held to its size and CRC-32, and an item that is unsized, encrypted, or not a plain file
// falls back to the one-item run. Extract is that one-item run, writing one item's content to
// the caller's writer. A solid 7z block decodes from its start, so items read one at a time each
// cost a decode of their block's start, which the whole run pays once.
//
// 7zz writes only to its standard output, -so: it is never given a path to write to, so an
// item's stored name, a traversing or absolute one included, is only ever a name. An item is
// named by its listed name, matched whole: -spd keeps it from being a wildcard, and -i! (or, for
// the whole run's exclusions, -x!) from being a list file. Every run is told its archive's
// format, -t, so 7zz never guesses one, a RAR4 its rar handler and a RAR5 its rar5 handler, each
// refusing the other's version, and given an empty password, -p, so it never prompts for one. A
// RAR volume of a split set opens its sibling volumes by name, so a caller reading one file alone
// refuses a volume, as route.RARVolume reports it, before any run. An archive whose headers are
// encrypted fails to list, and an encrypted item to read, with ErrEncrypted. A file 7zz can't open
// as the format named fails with ErrNotArchive. A run 7zz ends with a warning, exit code 1, has
// succeeded. Any other failure names 7zz's own error lines, and wraps the run's *tool.ExitError.
//
// # The listing
//
// The reader relies on this form of 7zz's technical listing, l -slt, in UTF-8 (-sccUTF-8), as
// 26.06 writes it: a banner and the archive's name, ignored; a line "--", then the archive's
// properties; a line of ten "-", then each item's properties, a blank line ending each
// item. A property is one line, "Name = value", or "Name =" with an empty value, which is no
// value. Lines end in "\n" alone, and a value runs to it, so a carriage return in a stored name
// is part of the name. 7zz lists a line break in a stored name as "_": the listing never breaks
// within a value, but the listed name is no longer the stored one, and naming it to 7zz matches
// nothing, so such an item read alone fails its size check, and an excluded name holding a "_"
// can't be trusted to exclude. Of the archive's properties, Method alone is read. Of an item's,
// Path, Size, CRC (hexadecimal), Encrypted ("+"), Method, Attributes (Windows letters, then a
// Unix mode such as "drwxr-xr-x", when it carries one), Folder ("+"), Symbolic Link, and Hard
// Link are read; others are ignored. A line of an item that isn't a property fails the listing,
// with ErrListing, as does a listing with no items section.
//
// Its exported identifiers:
//
//   - [SevenZipSpec] declares 7-Zip's 7zz for either runner's constructor.
//   - [SevenZipMinimum] is the oldest 7zz the package accepts, 26.06.
//   - [Config] is the Tool a Reader is built over.
//   - [Reader] lists and reads through 7zz; [NewReader] builds one.
//   - [Reader.List] lists an archive, given a [ListRequest], into a [Listing] of [Entry] items.
//   - [Reader.Read] yields each [Item] of an archive, given a [ReadRequest], with its content.
//   - [Reader.Extract] writes one item's content, given an [ExtractRequest], to a writer.
//   - [Entry.IsSymlink] and [Entry.IsDevice] report what an item's mode or link makes it.
//   - [ErrEncrypted], [ErrNotArchive], [ErrRequest], [ErrListing], [ErrEntries], and
//     [ErrContent] are the causes of a refusal or a failure.
package sevenzip
