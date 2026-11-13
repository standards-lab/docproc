// This module marks a boundary and holds no Go code. A module zip includes every committed file
// below its go.mod, so without this one the system tools under tools/, their sources, fixtures,
// and goldens, would ship in docproc's module zip. The go command stops at a directory holding a
// go.mod, so the subtree belongs to this module, which is never tagged or required.
module github.com/standards-lab/docproc/tools

go 1.27
