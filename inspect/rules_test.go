package inspect_test

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/docproc/inspect"
	"github.com/standards-lab/docproc/route"
)

type declared struct {
	name                 string
	body                 string
	expanded, compressed uint64
}

func zipDeclaring(t *testing.T, entries ...declared) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		var packed bytes.Buffer
		fw, err := flate.NewWriter(&packed, flate.BestCompression)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
		if err := fw.Close(); err != nil {
			t.Fatal(err)
		}
		h := &zip.FileHeader{
			Name: e.name, Method: zip.Deflate, CRC32: crc32.ChecksumIEEE([]byte(e.body)),
			CompressedSize64: uint64(packed.Len()), UncompressedSize64: uint64(len(e.body)),
		}
		if e.expanded != 0 {
			h.UncompressedSize64 = e.expanded
		}
		if e.compressed != 0 {
			h.CompressedSize64 = e.compressed
		}
		f, err := w.CreateRaw(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(packed.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func manyEntries(t *testing.T, n int) []byte {
	entries := make([]declared, n)
	for i := range entries {
		entries[i] = declared{name: fmt.Sprintf("f%04d.bin", i), body: "x", expanded: 1 << 30}
	}
	return zipDeclaring(t, entries...)
}

func TestManyEntries(t *testing.T) {
	data := manyEntries(t, 2000)
	path := write(t, "many.zip", data)
	r := mustInspect(t, path, "many.zip", rule)
	want := fmt.Sprintf("declared sizes sum to 2147483648000 bytes over 2000 entries in a %d-byte ZIP", len(data))
	if !slices.Equal(checks(r), []inspect.Check{inspect.ArchiveBomb}) || !strings.HasPrefix(evidence(r, inspect.ArchiveBomb), want) || r.Verdict != inspect.Quarantine {
		t.Errorf("%+v, want archive-bomb %q, quarantined", r, want)
	}

	past := rule
	past.Entries = 1999
	r = mustInspect(t, path, "many.zip", past)
	if !slices.Equal(checks(r), []inspect.Check{inspect.ZIPUnreadable}) || evidence(r, inspect.ZIPUnreadable) != "2000 entries listed, past 1999" || r.Verdict != inspect.Quarantine {
		t.Errorf("past Entries: %+v, want zip-unreadable alone, quarantined", r)
	}
	at := rule
	at.Entries = 2000
	if r := mustInspect(t, path, "many.zip", at); slices.Contains(checks(r), inspect.ZIPUnreadable) {
		t.Errorf("at Entries: %+v, want it read", r)
	}

	honest := make([]declared, 2000)
	for i := range honest {
		honest[i] = declared{name: fmt.Sprintf("f%04d.txt", i), body: "an ordinary entry\n"}
	}
	if r := mustInspect(t, write(t, "honest.zip", zipDeclaring(t, honest...)), "honest.zip", rule); len(r.Flags) != 0 {
		t.Errorf("2,000 honest entries: %+v, want no flag", r)
	}
}

func TestZIP64Entries(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for i := range 70000 {
		if _, err := w.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("%d", i), Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := mustInspect(t, write(t, "zip64.zip", buf.Bytes()), "zip64.zip", inspect.Options{Entries: 65535})
	if evidence(r, inspect.ZIPUnreadable) != "70000 entries listed, past 65535" {
		t.Errorf("%+v, want zip-unreadable by the ZIP64 count", r)
	}
}

func TestDeclaredCompressed(t *testing.T) {
	const expanded = 100 << 30
	data := zipDeclaring(t, declared{name: "zeros.bin", body: "zeros", expanded: expanded, compressed: expanded/999 + 1})
	path := write(t, "999.zip", data)
	for name, opts := range map[string]inspect.Options{"rule": rule, "zero": {}} {
		r := mustInspect(t, path, "999.zip", opts)
		want := fmt.Sprintf("declared compressed sizes sum to %d bytes, past the %d-byte ZIP", expanded/999+1, len(data))
		if !slices.Equal(checks(r), []inspect.Check{inspect.ArchiveBomb}) || !strings.HasPrefix(evidence(r, inspect.ArchiveBomb), want) || r.Verdict != inspect.Quarantine {
			t.Errorf("%s: %+v, want archive-bomb %q, quarantined", name, r, want)
		}
		if strings.Contains(evidence(r, inspect.ArchiveBomb), `entry "zeros.bin"`) {
			t.Errorf("%s: the entry alone, at 999:1, is past the rule: %+v", name, r)
		}
	}
	honest := zipDeclaring(t, declared{name: "notes.txt", body: "an ordinary entry\n"})
	if r := mustInspect(t, write(t, "honest.zip", honest), "honest.zip", inspect.Options{}); len(r.Flags) != 0 {
		t.Errorf("an honest ZIP: %+v, want no flag", r)
	}
}

func TestPDFStreams(t *testing.T) {
	small := "%PDF-1.7\n1 0 obj\n<< /Length 30 >>stream\r\n\x00/JavaScript /Launch \xff\nendstream\nendobj\n<< /OpenAction 1 0 R >>\n"
	r := mustInspect(t, write(t, "a.pdf", []byte(small)), "a.pdf", inspect.Options{})
	if got := checks(r); !slices.Equal(got, []inspect.Check{inspect.PDFOpenAction}) {
		t.Errorf("small: %v, want pdf-openaction alone", got)
	}

	pdf := bytes.Repeat([]byte(" "), 3<<16)
	copy(pdf, "%PDF-1.7\n")
	copy(pdf[1<<16-3:], "stream\n")
	copy(pdf[1<<16+100:], "/JS (in the data)")
	copy(pdf[2<<16-4:], "endstream")
	copy(pdf[2<<16+100:], "/AA")
	pdf = append(pdf, "\nmystream\n/Launch\nupstream /XFA\n"...)
	r = mustInspect(t, write(t, "b.pdf", pdf), "b.pdf", inspect.Options{})
	want := []inspect.Check{inspect.PDFAA, inspect.PDFLaunch, inspect.PDFXFA}
	if got := checks(r); !slices.Equal(got, want) {
		t.Errorf("across chunks: %v, want %v", got, want)
	}
	if e := evidence(r, inspect.PDFAA); e != fmt.Sprintf("/AA at offset %d", 2<<16+100) {
		t.Errorf("evidence %q", e)
	}
}

func TestXMLCharsets(t *testing.T) {
	const body = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	for name, c := range map[string]struct {
		content string
		want    []inspect.Check
	}{
		"us-ascii":     {`<?xml version="1.0" encoding="us-ascii"?>` + body, []inspect.Check{inspect.SVGScript}},
		"windows-1252": {"<?xml version=\"1.0\" encoding=\"windows-1252\"?><!-- \x93 -->" + body, []inspect.Check{inspect.SVGScript}},
		"unknown":      {`<?xml version="1.0" encoding="x-no-such-charset"?>` + body, []inspect.Check{inspect.SVGUnreadable}},
		"cut short":    {body[:len(body)-12], []inspect.Check{inspect.SVGScript, inspect.SVGUnreadable}},
		"utf-8":        {body, []inspect.Check{inspect.SVGScript}},
	} {
		r := mustInspect(t, write(t, "a.svg", []byte(c.content)), "a.svg", inspect.Options{})
		if got := checks(r); !slices.Equal(got, c.want) || r.Verdict != inspect.Proceed {
			t.Errorf("%s: %+v, want %v, proceed", name, r, c.want)
		}
	}
}

func docx(t *testing.T, rels map[string]string) []byte {
	t.Helper()
	entries := []declared{{name: "[Content_Types].xml", body: `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`}}
	for _, name := range slices.Sorted(maps.Keys(rels)) {
		entries = append(entries, declared{name: name, body: rels[name]})
	}
	return zipDeclaring(t, entries...)
}

func template(pad int) string {
	return `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		strings.Repeat(" ", pad) +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/attachedTemplate" Target="https://example.invalid/t.dotm" TargetMode="External"/></Relationships>`
}

func TestRelationshipsUnreadable(t *testing.T) {
	cases := map[string]struct {
		rels map[string]string
		want []inspect.Check
	}{
		"malformed":  {map[string]string{"_rels/.rels": `<Relationships><Relationship Id="rId1"`}, []inspect.Check{inspect.RelationshipsUnreadable}},
		"unknown":    {map[string]string{"_rels/.rels": `<?xml version="1.0" encoding="x-no-such-charset"?><Relationships/>`}, []inspect.Check{inspect.RelationshipsUnreadable}},
		"one part":   {map[string]string{"a/_rels/a.xml.rels": template(9 << 20)}, []inspect.Check{inspect.ExternalRelationship}},
		"two parts":  {map[string]string{"a/_rels/a.xml.rels": template(9 << 20), "b/_rels/b.xml.rels": template(9 << 20)}, []inspect.Check{inspect.ExternalRelationship, inspect.RelationshipsUnreadable}},
		"whole part": {map[string]string{"_rels/.rels": template(0)}, []inspect.Check{inspect.ExternalRelationship}},
	}
	for name, c := range cases {
		r := mustInspect(t, write(t, "a.docx", docx(t, c.rels)), "a.docx", inspect.Options{})
		if got := checks(r); !slices.Equal(got, c.want) || r.Verdict != inspect.Proceed {
			t.Errorf("%s: %+v, want %v, proceed", name, r, c.want)
		}
	}
	r := mustInspect(t, write(t, "a.docx", docx(t, cases["two parts"].rels)), "a.docx", inspect.Options{})
	if e := evidence(r, inspect.RelationshipsUnreadable); e != `part "b/_rels/b.xml.rels": past the package's 16777216-byte budget for its relationship parts` {
		t.Errorf("evidence %q", e)
	}
}

func TestNameAndContent(t *testing.T) {
	exe := filepath.Join("testdata", "invoice.pdf.exe")
	f, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	r, err := inspect.Content(t.Context(), f, info.Size(), route.PE, inspect.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := checksOf(r); !slices.Equal(got, []inspect.Check{inspect.Executable}) || r.Kind != route.PE {
		t.Errorf("Content: %v, %s; want executable alone", got, r.Kind)
	}
	names := inspect.Name("invoice.pdf.exe", route.PE)
	if got := checksOf(inspect.Report{Flags: names}); !slices.Equal(got, []inspect.Check{inspect.DoubleExtension, inspect.ExecutableExtension}) {
		t.Errorf("Name: %v", got)
	}
	if got := inspect.Name("report.docx", route.OLE2); len(got) != 0 {
		t.Errorf("an OOXML name on OLE2: %v, want no mismatch", got)
	}
	if got := inspect.Name("report.pdf", route.PNG); len(got) != 1 || got[0].Check != inspect.SignatureMismatch {
		t.Errorf("a PNG named .pdf: %v, want signature-mismatch", got)
	}
	for _, name := range []string{"notes.txt.bz2", "clean.TBZ2", "clean.tbz"} {
		if got := inspect.Name(name, route.Bzip2); len(got) != 0 {
			t.Errorf("a bzip2 stream named %q: %v, want no mismatch", name, got)
		}
	}
	if got := inspect.Name("clean.tar.gz", route.Bzip2); len(got) != 1 || got[0].Check != inspect.SignatureMismatch {
		t.Errorf("a bzip2 stream named .gz: %v, want signature-mismatch", got)
	}
	for _, name := range []string{"encrypted.xlsx", "blob", "old.doc"} {
		r := mustInspect(t, filepath.Join("testdata", "encrypted.xlsx"), name, inspect.Options{})
		if !slices.Contains(checks(r), inspect.OOXMLEncrypted) {
			t.Errorf("named %q: %+v, want ooxml-encrypted", name, r)
		}
	}
	plain := filepath.Join("testdata", "clean.tar")
	if r := mustInspect(t, plain, "x.docx", inspect.Options{}); slices.Contains(checks(r), inspect.OOXMLEncrypted) {
		t.Errorf("a tar named .docx: %+v, want no ooxml-encrypted", r)
	}
}

func TestRuleNegatives(t *testing.T) {
	names := []string{"docs/..notes.txt", "tmp/evil.txt", `docs\notes.txt`, "ab:c.txt", "notes..txt"}
	var entries []declared
	for _, n := range names {
		entries = append(entries, declared{name: n, body: "an ordinary entry\n"})
	}
	if r := mustInspect(t, write(t, "near.zip", zipDeclaring(t, entries...)), "near.zip", rule); len(r.Flags) != 0 {
		t.Errorf("ZIP: %+v, want no flag", r)
	}
	atSize := zipDeclaring(t, declared{name: "at-size.bin", body: "x", expanded: 1 << 30})
	if r := mustInspect(t, write(t, "at-size.zip", atSize), "at-size.zip", rule); len(r.Flags) != 0 {
		t.Errorf("an entry at the rule's size: %+v, want no flag", r)
	}

	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	if err := w.WriteHeader(&tar.Header{Name: "docs/", Typeflag: tar.TypeDir, Mode: 0o755, Format: tar.FormatPAX}); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		body := "an ordinary entry\n"
		if err := w.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body)), Format: tar.FormatPAX}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if r := mustInspect(t, write(t, "near.tar", buf.Bytes()), "near.tar", rule); len(r.Flags) != 0 {
		t.Errorf("tar: %+v, want no flag", r)
	}

	listing := []inspect.Listed{{Name: "docs/..notes.txt", Type: inspect.FileEntry, Declared: 1 << 30, Sized: true}, {Name: "docs", Type: inspect.DirEntry}}
	r, err := inspect.CheckListing(route.SevenZip, 1<<20, listing, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Flags) != 0 {
		t.Errorf("listing: %+v, want no flag", r)
	}
}

func TestFixtureNotes(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("testdata", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(readme))
	for sc.Scan() {
		var name, sum string
		var size int64
		if _, err := fmt.Sscanf(sc.Text(), "| `%s | %d | `%s |", &name, &size, &sum); err != nil {
			continue
		}
		name, sum = strings.TrimSuffix(name, "`"), strings.TrimSuffix(sum, "`")
		listed[name] = true
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := sha256.Sum256(data)
		if int64(len(data)) != size || hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s: %d bytes, SHA-256 %x; the README says %d, %s", name, len(data), got, size, sum)
		}
		if !bytes.Contains(readme, []byte("- `"+name+"`: ")) {
			t.Errorf("%s: the README doesn't say what it holds", name)
		}
	}
	files, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if n := f.Name(); n != "README.md" && n != "generate.go" && !listed[n] {
			t.Errorf("%s: not in the README", n)
		}
	}
	if len(listed) == 0 {
		t.Error("the README lists no fixture")
	}
}
