module github.com/standards-lab/docproc/cmd/docproc

go 1.27.4

require (
	github.com/standards-lab/docproc v0.2.0
	github.com/standards-lab/go-cli-sdk v0.4.0
	github.com/standards-lab/go-core v0.12.0
	golang.org/x/sys v0.50.0
)

require golang.org/x/text v0.44.0 // indirect

// The base module's unreleased rundir.Dir.ReportDir, reached from this checkout during the
// reports task's build. It is removed once v0.3.0 is tagged, when this module requires
// github.com/standards-lab/docproc v0.3.0.
replace github.com/standards-lab/docproc => ../..
