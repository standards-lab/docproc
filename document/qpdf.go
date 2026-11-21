package document

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/tool"
)

// QPDFMinimum is the oldest qpdf the package accepts: 11.0, the first whose JSON is version 2,
// whose string encodings Form reads.
const QPDFMinimum = "11.0"

// QPDFSpec declares qpdf, minimum 11.0, for either runner's constructor. qpdf --version prints
// "qpdf version 12.4.2" and a line on its license, and the pattern takes the dotted numbers after
// "qpdf version". A caller that needs another program path or a KillAfter period sets them on the
// Spec returned.
func QPDFSpec() tool.Spec {
	return tool.Spec{
		Program:        "qpdf",
		VersionArgs:    []string{"--version"},
		VersionPattern: regexp.MustCompile(`qpdf version (\d+(?:\.\d+)*)`),
		Minimum:        QPDFMinimum,
	}
}

// QPDF reads what a PDF's structure says before it is rendered, through qpdf: whether it needs a
// password to open, and its AcroForm's fields. Each method is one qpdf run, which reads the PDF by
// its absolute path in the PDF's directory, so either runner's Tool sees the same file, and writes
// nothing. Its methods are safe for concurrent use.
type QPDF struct {
	qpdf *tool.Tool
}

// NewQPDF builds a QPDF over cfg's QPDF; it reads no other field. A nil QPDF is the caller's
// mistake, and panics naming the field. It refuses one older than QPDFMinimum, whatever minimum
// its own Spec declared, with a *tool.VersionError wrapping tool.ErrTooOld.
func NewQPDF(cfg Config) (*QPDF, error) {
	if cfg.QPDF == nil {
		panic("document: Config.QPDF is nil")
	}
	if err := cfg.QPDF.AtLeast(QPDFMinimum); err != nil {
		return nil, err
	}
	return &QPDF{qpdf: cfg.QPDF}, nil
}

const stderrLimit = 16 << 10

const formLimit = 64 << 20

// RequiresPassword reports whether the PDF at pdf needs a password to open, through qpdf
// --requires-password, which opens it with no password: exit 0 when it needs one, 2 when it isn't
// encrypted, and 3 when it is encrypted but opens without one, as a PDF restricted only by its
// permissions does. qpdf exits 2 too when it can't open the file at all; such a run, whose
// standard error holds a line other than a warning, fails with qpdf's message.
func (q *QPDF) RequiresPassword(ctx context.Context, pdf string) (bool, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return false, fmt.Errorf("document: qpdf: %w", err)
	}
	stderr := &cli.LimitedBuffer{Limit: stderrLimit}
	err = q.qpdf.Run(ctx, tool.Invocation{
		Args: []string{"--requires-password", abs}, Dir: filepath.Dir(abs), Stderr: stderr,
	})
	exit, ok := errors.AsType[*tool.ExitError](err)
	switch {
	case err == nil:
		return true, nil
	case !ok:
		return false, fmt.Errorf("document: qpdf: %w", err)
	case exit.Code == 3:
		return false, nil
	case exit.Code == 2:
		if message := qpdfMessage(stderr.String()); message != "" {
			return false, fmt.Errorf("document: qpdf: %s: %w", message, err)
		}
		return false, nil
	}
	return false, qpdfError(stderr.String(), err)
}

// Form returns the AcroForm of the PDF at pdf, through qpdf's JSON, --json --json-key=acroform:
// each terminal field's full name, type, and value, in the AcroForm's order, a radio group's
// buttons as one field. A PDF with no AcroForm, or one with no fields, has a Form with none. A run
// qpdf ends with warnings, exit code 3, has succeeded. A field's value never fails the form: one
// that isn't text, such as a signature field's reference to its signature, is no Value. Only a run
// that fails, JSON past 64 MiB, JSON that doesn't parse, or JSON that isn't version 2 fails.
func (q *QPDF) Form(ctx context.Context, pdf string) (Form, error) {
	abs, err := filepath.Abs(pdf)
	if err != nil {
		return Form{}, fmt.Errorf("document: qpdf: %w", err)
	}
	var stdout bytes.Buffer
	stderr := &cli.LimitedBuffer{Limit: stderrLimit}
	err = q.qpdf.Run(ctx, tool.Invocation{
		Args: []string{"--json=2", "--json-key=acroform", abs}, Dir: filepath.Dir(abs),
		Stdout: &capped{w: &stdout, limit: formLimit}, Stderr: stderr,
	})
	if exit, ok := errors.AsType[*tool.ExitError](err); err != nil && (!ok || exit.Code != 3) {
		if ok {
			return Form{}, qpdfError(stderr.String(), err)
		}
		return Form{}, fmt.Errorf("document: qpdf: %w", err)
	}
	f, err := parseForm(stdout.Bytes())
	if err != nil {
		return Form{}, fmt.Errorf("document: qpdf: form of %s: %w", filepath.Base(abs), err)
	}
	return f, nil
}

func qpdfError(stderr string, err error) error {
	if message := qpdfMessage(stderr); message != "" {
		return fmt.Errorf("document: qpdf: %s: %w", message, err)
	}
	return fmt.Errorf("document: qpdf: %w", err)
}

func qpdfMessage(stderr string) string {
	var lines []string
	for line := range strings.Lines(stderr) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "WARNING: ") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}

type capped struct {
	w     io.Writer
	n     int
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.n+len(p) > c.limit {
		return 0, fmt.Errorf("output past %d bytes", c.limit)
	}
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// Form is a PDF's AcroForm: its terminal fields, in the AcroForm's order, as qpdf reads them.
type Form struct {
	Fields []FormField `json:"fields"`
}

// The field types a FormField names.
const (
	TextField       = "text"
	CheckboxField   = "checkbox"
	RadioField      = "radio"
	ChoiceField     = "choice"
	PushbuttonField = "pushbutton"
	SignatureField  = "signature"
)

// FormField is one terminal field of an AcroForm: its full name, its parents' partial names and
// its own joined by dots; its type, one of the field types, or the PDF's own field type, such as
// "Btn", for one none names; and its value.
//
// Value is the field's value when it is one string or name: a text field's text; a checkbox's or a
// radio group's state, the name of the appearance it shows, such as "Yes" or "Off"; and a choice's
// value when one is selected. A name is given without its slash, and a string as text, a binary
// one with any bytes that aren't UTF-8 replaced. A value of any other kind, or a string qpdf
// encodes in a way Form doesn't know, is no Value, and doesn't fail the form. Checked, for a
// checkbox or a radio group, says whether its state is other than "Off". For a choice, Options are
// its options as qpdf gives them, each option's display text, and Selected its selected values,
// its export values, one or more; a choice with several selected has no Value. Signed, for a
// signature field, says whether it has a value, the signature dictionary a signed field refers
// to.
type FormField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Value    string   `json:"value,omitzero"`
	Checked  *bool    `json:"checked,omitzero"`
	Options  []string `json:"options,omitzero"`
	Selected []string `json:"selected,omitzero"`
	Signed   *bool    `json:"signed,omitzero"`
}

type qpdfForm struct {
	Version  int `json:"version"`
	AcroForm struct {
		HasAcroForm bool `json:"hasacroform"`
		Fields      []struct {
			FullName      string          `json:"fullname"`
			FieldType     string          `json:"fieldtype"`
			FieldFlags    int64           `json:"fieldflags"`
			IsText        bool            `json:"istext"`
			IsCheckbox    bool            `json:"ischeckbox"`
			IsRadioButton bool            `json:"isradiobutton"`
			IsChoice      bool            `json:"ischoice"`
			Choices       []string        `json:"choices"`
			Value         json.RawMessage `json:"value"`
		} `json:"fields"`
	} `json:"acroform"`
}

const pushbutton = 1 << 16

func parseForm(b []byte) (Form, error) {
	var q qpdfForm
	if err := json.Unmarshal(b, &q); err != nil {
		return Form{}, err
	}
	if q.Version != 2 {
		return Form{}, fmt.Errorf("qpdf JSON version %d, want 2", q.Version)
	}
	f := Form{Fields: []FormField{}}
	if !q.AcroForm.HasAcroForm {
		return f, nil
	}
	seen := map[string]bool{}
	for _, qf := range q.AcroForm.Fields {
		if seen[qf.FullName] {
			continue
		}
		seen[qf.FullName] = true
		field := FormField{Name: qf.FullName}
		values := qpdfValues(qf.Value)
		if len(values) == 1 {
			field.Value = values[0]
		}
		switch {
		case qf.IsText:
			field.Type = TextField
		case qf.IsCheckbox, qf.IsRadioButton:
			field.Type = CheckboxField
			if qf.IsRadioButton {
				field.Type = RadioField
			}
			field.Checked = new(field.Value != "" && field.Value != "Off")
		case qf.IsChoice:
			field.Type = ChoiceField
			field.Options = qf.Choices
			field.Selected = values
		case qf.FieldType == "/Btn" && qf.FieldFlags&pushbutton != 0:
			field.Type = PushbuttonField
		case qf.FieldType == "/Sig":
			field.Type = SignatureField
			field.Signed = new(len(qf.Value) != 0 && string(qf.Value) != "null")
		default:
			field.Type = strings.TrimPrefix(qf.FieldType, "/")
		}
		f.Fields = append(f.Fields, field)
	}
	return f, nil
}

func qpdfValues(raw json.RawMessage) []string {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if v, ok := qpdfText(one); ok {
			return []string{v}
		}
		return nil
	}
	var many []json.RawMessage
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil
	}
	var values []string
	for _, m := range many {
		var s string
		if err := json.Unmarshal(m, &s); err != nil {
			continue
		}
		if v, ok := qpdfText(s); ok {
			values = append(values, v)
		}
	}
	return values
}

func qpdfText(s string) (string, bool) {
	switch {
	case strings.HasPrefix(s, "u:"):
		return s[2:], true
	case strings.HasPrefix(s, "b:"):
		b, err := hex.DecodeString(s[2:])
		if err != nil {
			return "", false
		}
		return strings.ToValidUTF8(string(b), "\uFFFD"), true
	case strings.HasPrefix(s, "n:/"):
		return s[3:], true
	case strings.HasPrefix(s, "/"):
		return s[1:], true
	}
	return "", false
}
