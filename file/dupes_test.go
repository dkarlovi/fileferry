package file

import (
	"os"
	"path/filepath"
	"testing"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

// dupeTestConfig builds a one-profile config whose target tree is root and
// whose filename pattern reproduces the canonical filename, so canonical paths
// can be resolved without EXIF or ffprobe.
func dupeTestConfig(root string) *ffcfg.Config {
	return &ffcfg.Config{Profiles: map[string]ffcfg.ProfileConfig{
		"Pictures": {
			Sources:  []ffcfg.SourceConfig{{Path: filepath.Join(root, "inbox"), Types: []string{"image"}}},
			Patterns: []string{"{meta.taken.date}-{meta.taken.time}.jpg"},
			Target:   ffcfg.TargetPathConfig{Path: filepath.Join(root, "{meta.taken.year}", "{meta.taken.date}", "{meta.taken.datetime}.{file.extension}")},
		},
	}}
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func onlyProfile(t *testing.T, reports []ProfileDupes) ProfileDupes {
	t.Helper()
	if len(reports) != 1 {
		t.Fatalf("expected 1 profile report, got %d", len(reports))
	}
	return reports[0]
}

func TestTargetRoot(t *testing.T) {
	sep := string(os.PathSeparator)
	cases := []struct{ tmpl, want string }{
		{filepath.Join("/photos", "{meta.taken.year}", "{x}.jpg"), filepath.FromSlash("/photos")},
		{filepath.Join("/photos", "set-{meta.taken.year}", "{x}.jpg"), filepath.FromSlash("/photos")},
		{sep + "{meta.taken.year}" + sep + "x.jpg", sep},
		{filepath.FromSlash("/photos/all.jpg"), filepath.FromSlash("/photos")},
		{"", ""},
	}
	for _, tc := range cases {
		if got := TargetRoot(tc.tmpl); got != tc.want {
			t.Errorf("TargetRoot(%q) = %q; want %q", tc.tmpl, got, tc.want)
		}
	}
}

// TestFindDupesKeepsCanonicalCopy covers the everyday case: the same file twice
// in the same folder, one under the name the config asks for and one under a
// name that means nothing to it. The canonical one survives.
func TestFindDupesKeepsCanonicalCopy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2016", "2016-01-10")
	canonical := writeFile(t, filepath.Join(dir, "2016-01-10-21-14-39.jpg"), "same content")
	copyOf := writeFile(t, filepath.Join(dir, "2016-01-10-21-14-39 (1).jpg"), "same content")

	reports, err := FindDupes(dupeTestConfig(root), "")
	if err != nil {
		t.Fatalf("FindDupes: %v", err)
	}
	rep := onlyProfile(t, reports)
	if len(rep.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", rep.Errors)
	}
	if rep.Scanned != 2 {
		t.Errorf("Scanned = %d; want 2", rep.Scanned)
	}
	if len(rep.Sets) != 1 {
		t.Fatalf("expected 1 dupe set, got %d", len(rep.Sets))
	}
	set := rep.Sets[0]
	if set.Reason != KeepCanonical {
		t.Errorf("Reason = %q; want %q", set.Reason, KeepCanonical)
	}
	if set.Keep != canonical {
		t.Errorf("Keep = %q; want %q", set.Keep, canonical)
	}
	if len(set.Delete) != 1 || set.Delete[0] != copyOf {
		t.Errorf("Delete = %v; want [%s]", set.Delete, copyOf)
	}
	if set.Canonical != canonical {
		t.Errorf("Canonical = %q; want %q", set.Canonical, canonical)
	}
	// Nothing may be touched by a scan.
	for _, p := range []string{canonical, copyOf} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("FindDupes removed %s: %v", p, err)
		}
	}
}

// TestFindDupesFallsBackWhenNoCopyIsCanonical covers two identical files whose
// names both parse but disagree: neither name can be trusted, so one is kept by
// name order and the set is reported as still needing a rename.
func TestFindDupesFallsBackWhenNoCopyIsCanonical(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2016", "2016-01-10")
	first := writeFile(t, filepath.Join(dir, "2016-01-10-21-14-39.jpg"), "same content")
	second := writeFile(t, filepath.Join(dir, "2016-01-10-21-15-10.jpg"), "same content")

	reports, err := FindDupes(dupeTestConfig(root), "Pictures")
	if err != nil {
		t.Fatalf("FindDupes: %v", err)
	}
	rep := onlyProfile(t, reports)
	if len(rep.Sets) != 1 {
		t.Fatalf("expected 1 dupe set, got %d", len(rep.Sets))
	}
	set := rep.Sets[0]
	if set.Reason != KeepFallback {
		t.Errorf("Reason = %q; want %q", set.Reason, KeepFallback)
	}
	if set.Canonical != "" {
		t.Errorf("Canonical = %q; want empty (names disagree)", set.Canonical)
	}
	if set.Keep != first {
		t.Errorf("Keep = %q; want %q (first by name)", set.Keep, first)
	}
	if len(set.Delete) != 1 || set.Delete[0] != second {
		t.Errorf("Delete = %v; want [%s]", set.Delete, second)
	}
}

// TestFindDupesIgnoresSameSizeDifferentContent guards the cheap pre-filter:
// equal size only makes two files candidates, the hash decides.
func TestFindDupesIgnoresSameSizeDifferentContent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2016", "2016-01-10")
	writeFile(t, filepath.Join(dir, "2016-01-10-21-14-39.jpg"), "content one")
	writeFile(t, filepath.Join(dir, "2016-01-10-21-15-10.jpg"), "content two")

	reports, err := FindDupes(dupeTestConfig(root), "")
	if err != nil {
		t.Fatalf("FindDupes: %v", err)
	}
	if sets := onlyProfile(t, reports).Sets; len(sets) != 0 {
		t.Fatalf("expected no dupe sets, got %+v", sets)
	}
}

// TestFindDupesOnlyGroupsSiblings verifies that identical files in different
// folders are left alone: the target template derives the folder from the
// content, so copies that belong together always land together.
func TestFindDupesOnlyGroupsSiblings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "2016", "2016-01-10", "2016-01-10-21-14-39.jpg"), "same content")
	writeFile(t, filepath.Join(root, "2017", "2017-01-10", "2017-01-10-21-14-39.jpg"), "same content")

	reports, err := FindDupes(dupeTestConfig(root), "")
	if err != nil {
		t.Fatalf("FindDupes: %v", err)
	}
	rep := onlyProfile(t, reports)
	if len(rep.Sets) != 0 {
		t.Fatalf("expected no dupe sets across folders, got %+v", rep.Sets)
	}
	if rep.Scanned != 2 {
		t.Errorf("Scanned = %d; want 2", rep.Scanned)
	}
}

// TestFindDupesSkipsForeignTypes keeps the scan to the file types the profile
// actually manages.
func TestFindDupesSkipsForeignTypes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2016", "2016-01-10")
	writeFile(t, filepath.Join(dir, "notes.txt"), "same content")
	writeFile(t, filepath.Join(dir, "notes-copy.txt"), "same content")

	reports, err := FindDupes(dupeTestConfig(root), "")
	if err != nil {
		t.Fatalf("FindDupes: %v", err)
	}
	rep := onlyProfile(t, reports)
	if rep.Scanned != 0 || len(rep.Sets) != 0 {
		t.Fatalf("expected nothing scanned, got Scanned=%d Sets=%+v", rep.Scanned, rep.Sets)
	}
}

func TestFindDupesUnknownProfile(t *testing.T) {
	if _, err := FindDupes(dupeTestConfig(t.TempDir()), "Nope"); err == nil {
		t.Fatal("expected an error for an unknown profile")
	}
}
