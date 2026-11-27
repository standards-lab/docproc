// Package picture converts pictures to PNG for a model's vision, and reads a picture's EXIF
// metadata. It provides pre-inference preparation only; a layer above sends what it returns to a
// model. The package builds on every 64-bit platform, on the standard library and this module's
// tool, route, and metadata packages; every program
// it runs is a tool.Tool the caller built: ImageMagick's magick, MagickSpec declaring it, minimum
// 7.1, and LibreOffice's soffice, LibreOfficeSpec declaring it, minimum 24.2, which the container
// runner runs in LibreOfficeImage under its full lockdown.
//
// A Converter, built over a Config of the two Tools, both required, and magick's Limits, converts
// the kinds a vision model decodes only once converted, route.VisionConverted's TIFF, EMF, WMF,
// SVG, HEIC, WebP, and AVIF, to PNG, and no conversion encodes a picture lossily: each writes PNG,
// without the time of the run, so a picture converted twice is the same bytes both times. Every
// magick run carries the Limits as -limit options, its format listing included; the package sets
// no limit of its own, and a zero Limits passes none. The caller tells each picture's kind; the
// package reads no signature and names no PNG by the picture's content. A TIFF's first image, the
// one an Office application displays, is converted through magick in a batch; ConvertPages
// converts a standalone TIFF's every page instead, one PNG each, in one magick run. A HEIC's
// primary image, never a thumbnail or another image, and an animated WebP's or AVIF's first frame
// are converted through magick, read through the kind's own coder and turned upright by their
// stored orientation: libheif applies a HEIC's or AVIF's rotation and mirror, and -auto-orient a
// WebP's EXIF orientation. NewConverter runs magick's format listing, magick -list format, once,
// and a HEIC, WebP, or AVIF whose reader the listing doesn't mark readable, as when ImageMagick was
// built without libheif or libwebp, is refused with ErrNoReader. An EMF or WMF is rendered through
// soffice's Draw PNG export as an A4 page at a scale times 96 dpi, DefaultMetafileScale, 3, by
// default and at most MaxMetafileScale, 8, the run's profile giving LibreOffice a British English
// locale so the page is A4 whatever the system's paper, and the page is trimmed to the picture
// through magick: what differs from the page corners' color. An SVG is rendered at a request's
// Density, DefaultDensity, 150 dpi, by default, through magick's rsvg coder, librsvg, named so that
// magick never falls back to its own renderer or a delegate, and flattened onto white.
//
// Each picture is converted in a directory of its own: it is copied, under its PNG's stem and its
// kind's extension, into a fresh directory in the caller's working directory, which holds the copy
// alone, and its converter reads the copy, so an SVG's relative references find nothing; the
// directory is removed however the conversion ends, and the picture's own file is never changed.
// librsvg runs no script, an SVG's script element and event handlers included, fetches no URL
// over a network, and loads no file but one beside the SVG it reads, which here is none: a
// script-bearing or referencing SVG converts to the PNG of what it draws itself.
//
// RunLibreOffice is the one LibreOffice runner: it renders a batch's metafiles, and the office
// package's legacy conversion runs it too. Each run works in a directory of its own in the
// caller's working directory, which holds a copy of each input, soffice's user profile, started
// with an optional registry, its output, and, under the exec runner, its HOME, so no run leaves
// anything elsewhere but in its TMPDIR, which the exec runner makes for the run and removes.
//
// An EXIFReader, built over a Config's Magick and Limits alone, reads the EXIF a picture's first
// image holds into a metadata.Picture, the record the stages package's picture and vision stages
// yield: its capture time in UTC, its camera, its orientation, its size, its GPS position in
// decimal degrees, and every EXIF property as magick prints it. One magick run reads the picture
// from its standard input, through its kind's coder, and writes nothing, so the picture is never
// changed; a JPEG, PNG, TIFF, HEIC, WebP, or AVIF is read, and a GIF, BMP, SVG, EMF, or WMF, which
// holds no EXIF magick reads, runs nothing.
//
// ConvertPages converts one picture, read as its Request's Kind, writing its PNGs into the working
// directory named by the Request's Stem: a TIFF's every page, named by the stem, -page, its number
// from 1, and .png, returned in page order; any other kind's one PNG, the stem and .png. The
// stages package's picture stage converts a standalone picture through it, its stem the part's
// SHA-256. ConvertBatch converts several at once, each read as the kind its Picture names and
// written to the PNG it names, the EMFs and WMFs in one soffice run: the office package converts a
// document's pictures through it. A kind the package doesn't convert, a stem that isn't a plain
// name, a scale out of range, and a negative density or limit are refused with ErrRequest, and a
// HEIC, WebP, or AVIF whose reader magick lacks with ErrNoReader, before anything runs; a
// conversion that fails leaves no PNG.
//
// Its exported identifiers:
//
//   - [MagickSpec] declares ImageMagick's magick for either runner's constructor.
//   - [MagickMinimum] is the oldest magick the package accepts, 7.1.
//   - [LibreOfficeSpec] declares LibreOffice's soffice for either runner's constructor.
//   - [LibreOfficeMinimum] is the oldest LibreOffice the package accepts, 24.2.
//   - [LibreOfficeImage] is the default image soffice runs in through the container runner.
//   - [Config] is what a Converter is built over: magick, LibreOffice, and magick's limits.
//   - [Limits] are ImageMagick's resource limits every magick run carries.
//   - [Converter] converts TIFF, EMF, WMF, SVG, HEIC, WebP, and AVIF to PNG.
//   - [NewConverter] builds a Converter over a Config, reading magick's format listing.
//   - [Converter.ConvertPages] converts one picture, a TIFF to a PNG per page, as a Request says.
//   - [Request] is one picture's conversion: its working directory, the picture's path and kind,
//     its PNGs' stem, the scale an EMF or WMF is rendered at, and the density an SVG is.
//   - [Converter.ConvertBatch] converts several pictures, each to the PNG its Picture names, as a
//     Batch says.
//   - [Batch] is several pictures' conversion: its working directory, its metafiles' scale, its
//     SVGs' density, and its pictures.
//   - [Picture] is one picture of a Batch: its path, the kind it is read as, and its PNG's path.
//   - [Converter.RunLibreOffice] runs soffice once, as a LibreOfficeRun says.
//   - [LibreOfficeRun] is one soffice run: its working directory, its --convert-to, its
//     --infilter, its inputs, and its profile's registry.
//   - [LibreOfficeInput] is one input of a run: its path and the name soffice reads it under.
//   - [LibreOfficeOutput] is what a run wrote: its output directory and soffice's message.
//   - [LibreOfficeOutput.Remove] removes the run's directory.
//   - [DefaultMetafileScale] is the scale EMF and WMF are rendered at by default, 3.
//   - [MaxMetafileScale] is the largest scale they may be rendered at, 8.
//   - [CheckMetafileScale] returns the scale a request asks for, or refuses it.
//   - [DefaultDensity] is the density an SVG is rendered at by default, 150 dpi.
//   - [ErrRequest] is a refused request's cause: a scale out of range, a negative density or
//     limit, an empty working directory, a kind it doesn't convert, a stem that isn't one, two
//     PNGs of one stem, or a LibreOfficeRun missing what it needs.
//   - [ErrNoReader] is the cause of a HEIC's, WebP's, or AVIF's refusal without magick's reader.
//   - [EXIFReader] reads a picture's EXIF metadata through magick.
//   - [NewEXIFReader] builds an EXIFReader over a Config's Magick and Limits.
//   - [EXIFReader.Metadata] returns a picture's EXIF metadata as a metadata.Picture, or nil.
//   - [ErrMetadata] is the cause of magick EXIF output that isn't read.
package picture
