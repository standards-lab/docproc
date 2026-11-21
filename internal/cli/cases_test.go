package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/docproc/dispatch"
	"github.com/standards-lab/docproc/internal/cli"
	"github.com/standards-lab/docproc/internal/tooltest"
)

type envelopeCases struct {
	ExitCodes []struct {
		Code    int    `json:"code"`
		Meaning string `json:"meaning"`
	} `json:"exit_codes"`
	Warnings []struct {
		Where string `json:"where"`
		What  string `json:"what"`
		Line  string `json:"line"`
	} `json:"warnings"`
	Errors []struct {
		Reason string `json:"reason"`
		Line   string `json:"line"`
	} `json:"errors"`
	MediaNames []struct {
		BytesHex   string `json:"bytes_hex"`
		StoredName string `json:"stored_name"`
		Name       string `json:"name"`
	} `json:"media_names"`
}

func readCases(t *testing.T) envelopeCases {
	t.Helper()
	data, err := os.ReadFile(tooltest.Tools(t, "envelope/cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c envelopeCases
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.ExitCodes) == 0 || len(c.Warnings) == 0 || len(c.Errors) == 0 || len(c.MediaNames) == 0 {
		t.Fatal("cases.json has an empty group")
	}
	return c
}

func TestMediaNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, c := range readCases(t).MediaNames {
		content, err := hex.DecodeString(c.BytesHex)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		hash := hex.EncodeToString(sum[:])
		if got := cli.MediaName(hash, c.StoredName); got != c.Name {
			t.Errorf("%q stored as %q: name %q, want %q", c.BytesHex, c.StoredName, got, c.Name)
		}
		m, err := cli.ResolveMedium(dir, c.Name)
		if err != nil || m.Path != filepath.Join(dir, c.Name) || m.SHA256 != hash {
			t.Errorf("ResolveMedium(%q) = %+v, %v", c.Name, m, err)
		}
		if m, err := cli.ResolveMedium("", c.Name); err != nil || m.Path != "" || m.SHA256 != hash {
			t.Errorf("ResolveMedium without a directory (%q) = %+v, %v", c.Name, m, err)
		}
	}
}

func TestResolveMediumRefuses(t *testing.T) {
	t.Parallel()
	const hash = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	for _, name := range []string{
		"", "image1.png", "../" + hash, hash + "/x", "../x/" + hash[6:],
		"BA7816BF8F01CFEA414140DE5DAE2223B00361A396177A9CB410FF61F20015AD",
		hash[:63], hash + "a", hash + ".", hash + ".123456789", hash + ".jp-g", hash + ".pngé",
		hash + ".png.gz", hash + ".png/..",
	} {
		if m, err := cli.ResolveMedium("/media", name); err == nil {
			t.Errorf("ResolveMedium(%q) = %+v, want an error", name, m)
		}
	}
}

func TestVersionPattern(t *testing.T) {
	t.Parallel()
	for out, want := range map[string]string{
		"0.1.1\nschema 2\n":         "0.1.1",
		"0.1.1\nschema 2":           "0.1.1",
		"0.1.0+8ea6b2d\nschema 2\n": "0.1.0",
		"1.2.3-rc.1\nschema 2\n":    "1.2.3",
		"0.1.1\nschema 1\n":         "",
		"0.1.1\nschema 20\n":        "",
		"0.1.1\n":                   "",
		"0.1.1\nschema 2\nmore\n":   "",
		"v0.1.1\nschema 2\n":        "",
		"spike 0.1.1\nschema 2\n":   "",
	} {
		m := cli.VersionPattern.FindStringSubmatch(out)
		got := ""
		if m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%q: version %q, want %q", out, got, want)
		}
	}
}

func TestRefusalClass(t *testing.T) {
	t.Parallel()
	for reason, want := range map[string]error{
		"encrypted package; decrypt it first": dispatch.ErrEncrypted,
		"encrypted":                           dispatch.ErrEncrypted,
		cli.TableOfContents:                   dispatch.ErrReadElsewhere,
		"a table of contents; its sections are read on their own.": nil,
		"not encrypted":     nil,
		"Encrypted section": nil,
		"an OLE compound file, such as a legacy document": nil,
		"": nil,
	} {
		if got := cli.RefusalClass(reason); got != want {
			t.Errorf("RefusalClass(%q) = %v, want %v", reason, got, want)
		}
	}
}
