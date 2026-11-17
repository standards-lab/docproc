package document

import "github.com/standards-lab/docproc/tool"

// Config is what the package's constructors are built over: the Tools the caller built, through
// either runner, and the renderer a Preparer prepares pages with. Each constructor reads its own
// fields alone, and panics naming a required one that is nil.
type Config struct {
	// Ghostscript is Ghostscript's gs, typically built from GhostscriptSpec; NewGhostscript
	// requires it.
	Ghostscript *tool.Tool
	// QPDF is qpdf, typically built from QPDFSpec; NewQPDF requires it.
	QPDF *tool.Tool
	// PDFDetach is poppler's pdfdetach, typically built from PDFDetachSpec; NewDetacher requires
	// it.
	PDFDetach *tool.Tool
	// Renderer renders and reads the pages a Preparer prepares, typically built by
	// NewGhostscript; NewPreparer requires it.
	Renderer *Ghostscript
}
