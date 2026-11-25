package document

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRequest is the cause of every refused request: a DPI of zero or less, a page below 1 or past
// the PDF's last, and an ImagesRequest a Preparer can't run.
var ErrRequest = errors.New("invalid request")

// CheckRender makes Render's refusals, and Render calls it before any tool runs. It refuses a DPI
// of zero or less and a page below 1, wrapping ErrRequest. It runs no tool, so it has no upper
// bound for the page: that needs the count, which a caller walking the pages holds already, and a
// direct Render past the last fails in gs.
func CheckRender(page, dpi int) error {
	if dpi <= 0 {
		return fmt.Errorf("document: %d dpi isn't positive: %w", dpi, ErrRequest)
	}
	if page < 1 {
		return fmt.Errorf("document: page %d is below 1: %w", page, ErrRequest)
	}
	return nil
}

// HasText reports whether text, a page's text layer as a TextReader's Text reads it, holds any:
// the rule for a page having a text layer is that its text isn't empty once whitespace is
// trimmed. A scanned page, its text an image, has none.
func HasText(text string) bool {
	return strings.TrimSpace(text) != ""
}
