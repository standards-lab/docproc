package stages

import (
	"context"

	"github.com/standards-lab/docproc/office"
	"github.com/standards-lab/docproc/picture"
	"github.com/standards-lab/docproc/route"
)

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

func Classify(ctx context.Context, err error, memory int64) (content bool, reason string) {
	f := classify(ctx, err, memory)
	return f.content, f.reason
}

func Needs() map[route.Route][]string {
	needs := map[route.Route][]string{}
	for _, s := range table {
		needs[s.route] = s.needs
	}
	return needs
}

func PictureConverter(s *PictureStage) *picture.Converter { return s.converter }

func OfficeConverter(s *OfficeStage) *office.Converter { return s.converter }
