//go:build linux

package onenote_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// onenoteCopy is the copy of onenote's fixtures and expectations, pinned by its MANIFEST.
var onenoteCopy = filepath.Join("testdata", "onenote")

// fixtures is the copy's fixtures directory: onenote's tests/fixtures, the public sets in
// interop/ and onenote-rs/, each with its LICENSE and sources.json, and the expectation files.
var fixtures = filepath.Join(onenoteCopy, "fixtures")

// manifestLine matches a file's line in the MANIFEST: its SHA-256, two spaces, and its path.
var manifestLine = regexp.MustCompile(`^([0-9a-f]{64})  (\S.*)$`)

// commitHash matches a full git commit hash.
var commitHash = regexp.MustCompile(`^[0-9a-f]{40}$`)

// readManifest reads the copy's MANIFEST: its header, each key and its value, and the files it
// lists, each path and its SHA-256.
func readManifest(t *testing.T) (header, listed map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(onenoteCopy, "MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	header, listed = map[string]string{}, map[string]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		if m := manifestLine.FindStringSubmatch(line); m != nil {
			listed[m[2]] = m[1]
		} else if key, value, ok := strings.Cut(line, " "); ok && key != "#" {
			header[key] = value
		}
	}
	return header, listed
}

// TestManifest: the copy of onenote's fixtures is exactly the files its MANIFEST lists, each with
// the SHA-256 listed, and the MANIFEST names the tag and the commit it was copied from. Each
// public set keeps its LICENSE and its sources.json.
func TestManifest(t *testing.T) {
	header, listed := readManifest(t)
	if header["tag"] == "" || !commitHash.MatchString(header["commit"]) {
		t.Errorf("manifest pins tag %q at commit %q, want a tag and a full commit hash", header["tag"], header["commit"])
	}
	found := map[string]string{}
	err := filepath.WalkDir(fixtures, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(onenoteCopy, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		found[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) == 0 {
		t.Fatal("the manifest lists no files")
	}
	for _, path := range slices.Sorted(maps.Keys(listed)) {
		if got, ok := found[path]; !ok {
			t.Errorf("%s: listed, not in the copy", path)
		} else if got != listed[path] {
			t.Errorf("%s: SHA-256 %s, the manifest's %s", path, got, listed[path])
		}
	}
	for _, path := range slices.Sorted(maps.Keys(found)) {
		if _, ok := listed[path]; !ok {
			t.Errorf("%s: in the copy, not listed", path)
		}
	}
	for _, set := range []string{"interop", "onenote-rs"} {
		for _, file := range []string{"LICENSE", "sources.json"} {
			if _, ok := listed["fixtures/"+set+"/"+file]; !ok {
				t.Errorf("%s has no %s", set, file)
			}
		}
	}
}
