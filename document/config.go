package document

import "github.com/standards-lab/docproc/tool"

// Config is what the package's constructors are built over: the Tools the caller built, through
// either runner, and the renderer and text reader a Preparer prepares pages with. Each constructor
// reads its own fields alone, and panics naming a required one that is nil.
type Config struct {
	// Ghostscript is Ghostscript's gs, typically built from GhostscriptSpec; NewGhostscript
	// requires it.
	Ghostscript *tool.Tool
	// QPDF is qpdf, typically built from QPDFSpec; NewQPDF requires it.
	QPDF *tool.Tool
	// PDFDetach is poppler's pdfdetach, typically built from PDFDetachSpec; NewDetacher requires
	// it.
	PDFDetach *tool.Tool
	// PDFToText is poppler's pdftotext, typically built from PDFToTextSpec; NewTextReader
	// requires it.
	PDFToText *tool.Tool
	// PDFInfo is poppler's pdfinfo, typically built from PDFInfoSpec; NewInfoReader requires it.
	PDFInfo *tool.Tool
	// Renderer counts and renders the pages a Preparer prepares, typically built by
	// NewGhostscript; NewPreparer requires it.
	Renderer *Ghostscript
	// Text reads the text of the pages a Preparer prepares, typically built by NewTextReader;
	// NewPreparer requires it.
	Text *TextReader
}
