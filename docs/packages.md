# Packages

| Package | What it does |
| --- | --- |
| `route` | Identifies content by its signature, the extension table, and the encrypted-package detector, and names the route of the stage it goes to. |
| `inspect` | Inspects a part before its stage and says whether it may go on: an unsafe part, such as a bomb or an unreadable directory, is quarantined; an active one, such as a link, a VBA project, or an external relationship, is flagged. |
| `tool` | Runs a system tool behind one contract, through the exec runner (Linux) or the container runner (the docker CLI), each reporting a run's exit code, signal, and time bound, and applying a memory bound when one is set. |
| `office`, `email`, `onenote` | The readers of docproc-ooxml, docproc-mail, and docproc-onenote, each decoding its tool's schema `"2"` JSON Lines; `email` also threads a store's messages. |
| `document` | A PDF's pages rendered to PNG files through Ghostscript, with their text layer through pdftotext, its password requirement, form, and embedded files through qpdf and pdfdetach, and its metadata through pdfinfo. |
| `picture` | A TIFF's pages, and EMF, WMF, SVG, HEIC, WebP, and AVIF pictures, converted to PNG through ImageMagick and LibreOffice, and a picture's EXIF metadata read through ImageMagick. |
| `audio` | Speech cut into windows that end in a silence, from one ffmpeg decode, and the audio plan record beside them; the source's metadata record from the same ffprobe run that finds its stream. |
| `media` | A file's audio windows and its video frames, at its scene changes and at least every so often, from one ffmpeg run, and the video plan record beside them; the file's metadata record from the same ffprobe run that finds its streams. |
| `sevenzip` | 7z, RAR4, RAR5, zstd, xz, cabinet, and a ZIP's entries of methods `archive/zip` doesn't read, listed and read through 7-Zip's 7zz. |
| `dispatch` | The walk over an input's parts to their stages; the Stage contract and its error classes; the content store and its layout, and the index, each with a file and a memory implementation; the shapes of its events, parts, outputs, and warnings; and the Meta keys every stage sets and the record media types. It imports `route` and `inspect` alone. |
| `dispatch/dispatchtest` | The Index and Content conformance suites, for every store, the module's own and a provider's, and the fixture stage a host's tests dispatch over. |
| `stages` | `Build` and its `Options`: docproc's shipped wiring, each route's stage over its format package and tools, found on the host, under the host's run bounds and requests, with the one failure classification and each stage's identity. `cmd/docproc` is its first caller. |
| `rundir` | `cmd/docproc`'s run directory, read and written: `Open` to read it and `Create` to write it, its runs, the latest that ended, each run's manifest and record, its content store and the path of a content's file, its index, a quarantined part's kept file, and a run's report directory, `runs/<run>/report/`. One process writes a run directory. |
| `rundir/schema` | The run directory's schema `"2"`: the shapes `cmd/docproc` writes and a reader decodes, a record line, a manifest, an input's file, and a time, each in UTC. It imports no docproc package but `dispatch` and `inspect`. |
| `metadata` | The metadata records' shapes, one per format, each a JSON value under a versioned media type `dispatch` declares: `Office`, an office package's properties and the format it was converted from, `PDF`, a PDF's information dictionary, XMP metadata, and version, `Media`, an audio or video file's container format, duration, bit rate, and tags, and its streams, and `Picture`, a picture's EXIF capture time, camera, orientation, size, GPS position, and properties. It imports no docproc package but `dispatch`, so a reader decodes a record without compiling the stages. |
| `internal/cli` | The envelope reader the three readers share: their lines, stderr warnings, exit codes, and media names; and the ffprobe report audio and media read. |
| `internal/goldentest`, `internal/mediatest`, `internal/tooltest` | Test helpers: PNG goldens, media fixtures, and the unit tier's stand-in tool. |

A host registers `stages.Build`'s stages with a `dispatch.Dispatcher` and ranges over its
events. No bound, cap, or limit has a library default: the run bounds (`stages.Options`' Timeout,
Idle, and Memory), `dispatch.Limits`, and the caps in each stage's request are none until the host
sets them. `stages.Options.Concurrency`, the pages and windows held at once, is one until the host
raises it.

The packages' exported API, and the boundary other layers build against, is on [`api.md`](api.md).
