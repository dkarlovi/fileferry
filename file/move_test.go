package file

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

// fakeEntry is an in-memory Entry for testing MoveEntry without a device. It is
// intentionally NOT a localPathProvider, so it exercises the streamed path.
type fakeEntry struct {
	name    string
	bodies  [][]byte // content returned by successive Open calls
	opens   int
	deleted bool
}

func (e *fakeEntry) Name() string        { return e.name }
func (e *fakeEntry) DisplayPath() string { return "fake://" + e.name }
func (e *fakeEntry) Size() int64         { return int64(len(e.bodies[0])) }
func (e *fakeEntry) ModTime() time.Time  { return time.Time{} }
func (e *fakeEntry) Delete() error       { e.deleted = true; return nil }

func (e *fakeEntry) Open() (io.ReadCloser, error) {
	body := e.bodies[len(e.bodies)-1]
	if e.opens < len(e.bodies) {
		body = e.bodies[e.opens]
	}
	e.opens++
	return io.NopCloser(strings.NewReader(string(body))), nil
}

// TestFilenameMetaPixelFormat covers the compact "yyyymmdd" date specifier plus
// a trailing wildcard, matching Pixel filenames like
// PXL_20260106_182648043.RAW-02.ORIGINAL.dng.
func TestFilenameMetaPixelFormat(t *testing.T) {
	pattern := "PXL_{meta.taken.date:yyyymmdd}_{meta.taken.time:hhmmss}.*"
	meta := parseMetadataFromFilenamePattern("PXL_20260106_182648043.RAW-02.ORIGINAL.dng", pattern)
	if meta == nil || meta.TakenTime == nil {
		t.Fatalf("expected metadata with TakenTime, got %+v", meta)
	}
	got := meta.TakenTime.Format("2006-01-02 15:04:05")
	if got != "2026-01-06 18:26:48" {
		t.Errorf("TakenTime = %q; want 2026-01-06 18:26:48", got)
	}
	if meta.Extension != "dng" {
		t.Errorf("Extension = %q; want dng", meta.Extension)
	}
}

// TestProcessFileSkipsContentWhenFilenameSufficient verifies the fast path:
// when the filename pattern fully satisfies the target template, the file's
// content is never opened (important for MTP, where opening streams the file).
func TestProcessFileSkipsContentWhenFilenameSufficient(t *testing.T) {
	e := &fakeEntry{name: "PXL_20260106_182648043.RAW-02.ORIGINAL.dng", bodies: [][]byte{[]byte("unused")}}
	cfg := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{
			"Phone": {
				Patterns: []string{"PXL_{meta.taken.date:yyyymmdd}_{meta.taken.time:hhmmss}.*"},
				Target:   ffcfg.TargetPathConfig{Path: "/out/{meta.taken.year}/{meta.taken.datetime}.{file.extension}"},
			},
		},
	}

	result := processFile(e, ffcfg.SourceConfig{}, "Phone", cfg)
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if e.opens != 0 {
		t.Errorf("entry was opened %d times; want 0 (filename should satisfy the template)", e.opens)
	}
	if result.NewPath == "" || result.Metadata == nil || result.Metadata.TakenTime == nil {
		t.Errorf("expected resolved path and metadata, got NewPath=%q meta=%+v", result.NewPath, result.Metadata)
	}
}

func TestMoveEntrySuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "out", "moved.dng")
	content := []byte("raw photo bytes")

	e := &fakeEntry{name: "moved.dng", bodies: [][]byte{content, content}}
	if err := MoveEntry(e, dest); err != nil {
		t.Fatalf("MoveEntry: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("dest content = %q; want %q", got, content)
	}
	if !e.deleted {
		t.Error("source was not deleted after a verified copy")
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Error("temp .partial file was left behind")
	}
}

func TestMoveEntryHashMismatchDoesNotDelete(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "moved.dng")

	// First Open (copy) and second Open (verify re-read) differ → corruption.
	e := &fakeEntry{name: "moved.dng", bodies: [][]byte{[]byte("good-copy-bytes"), []byte("DIFFERENT-bytes")}}
	err := MoveEntry(e, dest)
	if err == nil {
		t.Fatal("expected verification error, got nil")
	}
	if e.deleted {
		t.Error("source was deleted despite failed verification")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Error("destination file should not exist after failed verification")
	}
	if _, statErr := os.Stat(dest + ".partial"); !os.IsNotExist(statErr) {
		t.Error("temp .partial file should be cleaned up after failure")
	}
}
