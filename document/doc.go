// Package document prepares a PDF's pages for a model's vision: each page rendered to a PNG file,
// and its text layer read when a request says to. It provides pre-inference preparation only; a
// layer above sends the prepared pages to a model and owns the requests, retries, and any final
// step. The package builds on every platform, on the standard library and this module's tool
// package; every system tool it runs is a tool.Tool the caller built. It also reads what a PDF
// holds beside its pages, its password requirement, its form fields, and its embedded files,
// through qpdf and pdfdetach.
//
// The renderer, Ghostscript, is the renderer bake-off's winner, one gs process per call. Count
// returns a PDF's page count with gs's own PDF interpreter; Render writes one page, 1-based, as
// PNG at a DPI to a writer, with gs's png16m device, text and graphics anti-aliased, to stdout;
// and Text reads a page's text layer with gs's txtwrite device, keeping gs's physical layout, so a
// multi-column page or a form reads out of reading order. HasText is the rule for a page having a
// text layer, its text not empty once whitespace is trimmed. Render refuses a DPI of zero or less
// and a page below 1 before any tool runs, as CheckRender does. It doesn't count the pages: gs
// past the last page exits zero with a message in place of a PNG, which Render returns as its
// error, writing nothing. Each run reads the PDF by its absolute path in the PDF's directory, so
// either runner's Tool sees the same file. NewGhostscript takes a gs Tool built through either
// runner, GhostscriptSpec declaring it, in a Config, and refuses one older than 10.0. Each
// constructor panics naming a required Config field that is nil, the caller's mistake, and
// returns an error only for the environment's fault, a tool too old. Ghostscript is
// AGPL-licensed, and the package runs it as a separate program, never linked.
//
// Two adapters read what a PDF's structure holds beside its pages, each over a tool.Tool the
// caller built. QPDF, over qpdf, minimum 11.0, the first whose JSON is version 2: RequiresPassword
// asks qpdf --requires-password whether the PDF needs a password to open, exit 0 yes, 2 not
// encrypted, 3 encrypted but opening without one, as a PDF restricted only by its permissions
// does, and an exit 2 with qpdf's error a file it can't open; and Form reads qpdf's JSON of the
// AcroForm, --json-key=acroform, into a Form, each terminal field's full name, type, and value,
// a checkbox's or radio group's checked state, a choice's options and selected values, and
// whether a signature field is signed, a radio group's widgets as one field; a field's value never
// fails the form, only JSON that doesn't parse or isn't version 2. Detacher, over poppler's pdfdetach, minimum 22.02, whose -v
// exits 99: List lists the embedded files, the name tree's and the file attachment annotations',
// not the media behind a Screen or Rendition annotation; and Save writes one file's content to
// the caller's writer, pdfdetach told to save it to /dev/stdout, so it writes no file and a stored
// name is only ever a name. Each reads the PDF by its absolute path in the PDF's directory.
//
// A Preparer, built at the composition root over a Ghostscript, streams a PDF's prepared pages
// through Images, which reads the PDF at the caller's path, counts its pages, and renders each
// page to a PNG file in the directory the ImagesRequest names, page-N.png, yielding a PageImage of
// its number, the page count, its file's path, and the DPI it was rendered at. A request may read
// each page's text layer into its PageImage. A request's MaxPixels bounds a page's pixels: a page
// past it is rendered at the largest DPI that fits, which its PageImage names, the pages' sizes
// read in the one gs run that counts them. At most the request's concurrency of pages are held at
// once, a page's slot released only when the consumer takes it, and pages come back in page
// order, a failed page as its number and count with its error, the stream carrying on. A refused
// request, a failed count, or an ended context is the stream's one error. The stream is lazy,
// breaking out of it cancels the work in flight and returns once that work has ended, and a page
// file not yet yielded is removed; a yielded one is the caller's.
//
// Its exported identifiers:
//
//   - [CheckRender] makes Render's refusals of a DPI and a page.
//   - [Ghostscript] is the renderer, over Ghostscript's gs.
//   - [NewGhostscript] builds the renderer over a Config's gs Tool.
//   - [Ghostscript.Count] counts a PDF's pages with gs.
//   - [Ghostscript.Render] renders one page as PNG with gs.
//   - [Ghostscript.Text] reads one page's text layer with gs.
//   - [HasText] is the rule for a page having a text layer.
//   - [GhostscriptSpec] declares Ghostscript's gs for either runner's constructor.
//   - [GhostscriptMinimum] is the oldest gs the renderer accepts, 10.0.
//   - [ErrRequest] is every refusal's cause: a DPI of zero or less, a page below 1, or past the
//     last for Text and for a caller that holds the count, and an ImagesRequest a Preparer can't
//     run.
//   - [Config] is what the constructors are built over: the Tools, and a Preparer's renderer.
//   - [Preparer] prepares a PDF's pages for a model's vision.
//   - [NewPreparer] builds a Preparer over a Config's renderer.
//   - [ImagesRequest] is one PDF's run through Images: its pages' directory, DPI, concurrency,
//     text layers, and most pixels.
//   - [DefaultDPI] is the DPI of a request of zero DPI, 150.
//   - [PageImage] is one prepared page's number, the page count, its PNG file, its DPI, and its
//     text layer.
//   - [Preparer.Images] streams a PDF's prepared pages, in page order.
//   - [QPDF] reads a PDF's password requirement and AcroForm through qpdf; [NewQPDF] builds one,
//     [QPDFSpec] declares qpdf, and [QPDFMinimum] is the oldest it accepts, 11.0.
//   - [QPDF.RequiresPassword] reports whether a PDF needs a password to open.
//   - [QPDF.Form] reads a PDF's AcroForm into a [Form] of [FormField]s, typed [TextField],
//     [CheckboxField], [RadioField], [ChoiceField], [PushbuttonField], or [SignatureField], a
//     field's value never failing it.
//   - [Detacher] lists and saves a PDF's embedded files through pdfdetach; [NewDetacher] builds
//     one, [PDFDetachSpec] declares pdfdetach, and [PDFDetachMinimum] is the oldest it accepts,
//     22.02.
//   - [Detacher.List] lists a PDF's embedded files as [Attachment]s.
//   - [Detacher.Save] writes one embedded file's content to a writer; [ErrNoAttachment] is the
//     cause of a number the PDF has no file of.
package document
