# Limits

ODF, WordPerfect, and `.xlsb` route to office, whose stage ends them with no processor yet; ISO
routes to no stage. docproc runs no virus scan and flags no macro in an OLE2 file. Content no
processor claims is read as text, whatever its name, when its first 4096 bytes hold no NUL byte,
and is unrouted when they do. So HTML and an EPUB's entries are read as raw text, with no
structure, and UTF-16 without a byte order mark, which holds NULs, is refused. docproc detects no
text's encoding: it reads text by its byte order mark, as UTF-8, or else as windows-1252, so a
legacy multibyte encoding such as Shift-JIS, EUC-JP, or ISO-2022-JP reads as mojibake. Beyond
that, each stage inherits its tool's limits, as the tools' READMEs
([docproc-ooxml](../tools/ooxml/README.md), [docproc-mail](../tools/mail/README.md),
[docproc-onenote](../tools/onenote/README.md)) state them in full; these are the ones a host sizes
or routes by.

- **office.** Pictures are listed with no layout, so a picture another covers, that sits off the
  slide or page, or that is cropped away is still listed and handed onward. Text boxes,
  equations, SmartArt, a pptx's comments, and an xlsx's cell notes are not read; no formula is
  evaluated, a cell's value being the one the file caches; tracked changes are read accepted
  only. An EMF or WMF is rendered on an A4 page and trimmed to what differs from the page's
  white, so a picture framed in white loses its frame, and an all-white picture is one pixel.
  docproc-ooxml links glibc, 2.34 or newer. Its floor under a memory bound is 384 MiB on x64 and
  on arm64 alike: it converts in 20 of 20 runs at 384 MiB and above, and below that some runs
  fail, by SIGABRT, its last stderr line "Out of memory" or naming libssl, or by exit 255 with no
  message ([Acceptance](acceptance.md#docproc-ooxmls-memory-floor), measured 2026-11-25 on
  GitHub's runners from 6799188, whose docproc-ooxml is dcbead1's).
- **email.** In a `.pst`, an embedded message is listed without its content; an `.ost` is refused;
  TNEF and opaque S/MIME parts are not read, nor a rights-managed message's `message.rpmsg`
  content, each with a warning; a `.msg` or `.pst` message whose class is `IPM.Note.SMIME`, or
  ends in `.SMIME`, compared without regard to case, has the opaque S/MIME warning whatever its
  attachments; a clear-signed `IPM.Note.SMIME.MultipartSigned` message with no body of its own has
  its Markdown made from its `multipart/signed` attachment's bodies, and that content's own
  attachments are not listed; an attachment by reference has no file; every mbox is read as an
  mboxrd. docproc-mail's memory is a small multiple of the largest single attachment, and
  of the file for an `.eml`, which is read whole. The Threader keeps each message's threading keys
  alone: it holds about 362 bytes a message, 435 while Threads runs (spike-ai-pipeline-processing's
  measurement over 200,000 messages). It holds them in memory; it doesn't spill.
- **onenote.** A password-protected section is refused as an input and skipped inside a
  notebook; a page is read at its latest revision only; merged table cells are read as their
  own; a section that fails inside a section group is lost with no warning. docproc-onenote holds
  a notebook whole in memory before its first line, so size a host by the largest notebook or
  section it is given. Its read thread's stack is an address-space reservation of a quarter of
  the memory bound, at least 8 MiB and at most 256 MiB, which counts against the bound.
- **archive.** 7zz reads a RAR4 through its rar handler, a RAR5 through its rar5 handler, and an xz
  file as one stream whose content is one part. Split RAR volumes stay unread: a RAR whose first
  header marks it as one volume of a split set ends with no processor yet, and 7zz never runs for
  it, so no sibling volume is opened. A RAR5 volume whose headers are encrypted hides that mark, so
  it ends Encrypted instead. A listing past `inspect.Options.Entries` fails its part; an entry
  flagged by name or type (a traversal, an absolute name, a link, a device), an encrypted one, and a
  bomb as written are unread children, not read. An archive's entry, or a PDF's attachment, is
  stopped at the write that would take its dispatch past `dispatch.Limits.Bytes`, its partial file
  removed, and is an unread child, limited, with no size or SHA-256. The media docproc-ooxml,
  docproc-mail, and docproc-onenote write are held to the cap only once written.
- **document.** A PDF that needs a password is flagged and not read. An AcroForm whose qpdf JSON
  passes 64 MiB fails `QPDF.Form` rather than being held in memory. A page past the request's
  MaxPixels is rendered at the largest DPI that fits; a page that fits at none fails. A page's text
  is read by a pdftotext run of its own, in pdftotext's reading order: a multi-column page reads
  column by column with no run of spaces padding it, but the paragraphs of columns that line up
  may alternate between the columns, and a word hyphenated at a line's end is joined. A PDF's
  pages share gs processes for their renders alone: at concurrency C, lane k renders pages k,
  k+C, k+2C, and so on, up to `ImagesRequest.PagesPerProcess` pages a process (zero is 8). Through
  the exec runner, the run that counts the pages and the shared processes work in a temporary
  directory of the stream's own, never the input's. A process's pages go to gs as a page list,
  each run of consecutive pages a range, such as 1-8, and a batch ends early where its list would
  pass the 2047 bytes gs takes in an argument. A page MaxPixels lowers gets a process of its own,
  as does a batch of one page, and so does every page of a PDF portfolio, since a shared process
  would render the portfolio's embedded documents in place of its pages; a portfolio page's own
  process renders the cover's page. Every page's PNG is byte for byte what a process of its own
  gives. A page's own processes and its text run work in the PDF's directory, as every tool's runs
  do, but keep their temporary files in their run's own `TMPDIR`, so one ended early, by its bound
  or by the stream stopping, leaves nothing beside the PDF. A shared process's bound is
  `stages.Options.Timeout` read as an idle bound, between its pages, the time it waits on the
  consumer not counted. When one fails on the content, the pages it handed over stand and each
  other page is rendered in a process of its own, bounded by the whole timeout, so only a page
  that fails there fails; a page whose text run fails fails alone, unrendered.
- **audio and media.** A source ffprobe reports no duration for is refused. media reads files
  alone: no live source, stream URL, or capture.
