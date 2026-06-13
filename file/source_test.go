package file

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

func TestOpenSourceLocalScan(t *testing.T) {
	tmpDir := t.TempDir()
	sub := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(tmpDir, "a.jpg"), "a")
	mustWrite(t, filepath.Join(tmpDir, "b.txt"), "b") // filtered out by type
	mustWrite(t, filepath.Join(sub, "c.jpg"), "c")

	src, err := OpenSource(ffcfg.SourceConfig{Path: tmpDir})
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	defer src.Close()

	// Non-recursive: only a.jpg.
	entries, err := src.Scan([]string{"image"}, false)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("non-recursive scan found %d entries; want 1", len(entries))
	}
	if entries[0].Name() != "a.jpg" {
		t.Errorf("entry name = %q; want a.jpg", entries[0].Name())
	}

	// Recursive: a.jpg + sub/c.jpg.
	rec, err := src.Scan([]string{"image"}, true)
	if err != nil {
		t.Fatalf("Scan recurse: %v", err)
	}
	if len(rec) != 2 {
		t.Errorf("recursive scan found %d entries; want 2", len(rec))
	}
}

func TestLocalEntryOpenAndDelete(t *testing.T) {
	tmpDir := t.TempDir()
	p := filepath.Join(tmpDir, "a.jpg")
	mustWrite(t, p, "hello")

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	e := &localEntry{path: p, info: fi}

	if got := e.Size(); got != 5 {
		t.Errorf("Size = %d; want 5", got)
	}
	if got := e.DisplayPath(); got != p {
		t.Errorf("DisplayPath = %q; want %q", got, p)
	}

	rc, err := e.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "hello" {
		t.Errorf("content = %q; want hello", data)
	}

	if err := e.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still exists after Delete")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
