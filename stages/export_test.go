package stages

import (
	"context"

	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

// The stage adapters and their configurations, for the tests to build each over stand-ins and
// fakes, as Build builds it over the tools.
type (
	DocumentConfig = documentConfig
	DocumentStage  = documentStage
	OfficeConfig   = officeConfig
	OfficeStage    = officeStage
	PictureConfig  = pictureConfig
	PictureStage   = pictureStage
	EmailConfig    = emailConfig
	EmailStage     = emailStage
	OneNoteConfig  = onenoteConfig
	OneNoteStage   = onenoteStage
	ArchiveConfig  = archiveConfig
	ArchiveStage   = archiveStage
	AudioConfig    = audioConfig
	AudioStage     = audioStage
	VideoConfig    = videoConfig
	VideoStage     = videoStage
	VisionStage    = visionStage
	TextStage      = textStage
	QPDFReader     = qpdfReader
	PDFDetacher    = pdfDetacher
	SevenZipReader = sevenZipReader
)

// The adapters' constructors and versions.
var (
	NewDocumentStage = newDocumentStage
	NewOfficeStage   = newOfficeStage
	NewPictureStage  = newPictureStage
	NewEmailStage    = newEmailStage
	NewOneNoteStage  = newOneNoteStage
	NewArchiveStage  = newArchiveStage
	NewAudioStage    = newAudioStage
	NewVideoStage    = newVideoStage
	NewVisionStage   = newVisionStage
	NewTextStage     = newTextStage
	Versions         = versions
	PartErr          = partErr
)

// Classify returns classify's reading of err: whether it is the content's, and its reason.
func Classify(ctx context.Context, err error, memory int64) (content bool, reason string) {
	f := classify(ctx, err, memory)
	return f.content, f.reason
}

// Needs returns the programs each route's stage requires, in the stage table's order.
func Needs() map[route.Route][]string {
	needs := map[route.Route][]string{}
	for _, s := range table {
		needs[s.route] = s.needs
	}
	return needs
}

// PictureConverter returns s's Converter.
func PictureConverter(s *PictureStage) *picture.Converter { return s.converter }

// OfficeConverter returns s's Converter.
func OfficeConverter(s *OfficeStage) *office.Converter { return s.converter }
