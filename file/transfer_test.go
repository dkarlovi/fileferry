package file

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

// fakeEntry is an in-memory Entry for testing TransferEntry without a device. It is
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

func TestTransferEntryMoveSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "out", "moved.dng")
	content := []byte("raw photo bytes")

	e := &fakeEntry{name: "moved.dng", bodies: [][]byte{content, content}}
	res, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictError)
	if err != nil {
		t.Fatalf("TransferEntry: %v", err)
	}
	if res.Outcome != Transferred {
		t.Errorf("Outcome = %v; want Transferred", res.Outcome)
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

func TestTransferEntryMoveExistingDuplicateDeletesSource(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "out", "moved.dng")
	content := []byte("raw photo bytes")

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dest, content, 0644); err != nil {
		t.Fatalf("seed dest: %v", err)
	}

	e := &fakeEntry{name: "moved.dng", bodies: [][]byte{content}}
	res, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictError)
	if err != nil {
		t.Fatalf("TransferEntry: %v", err)
	}
	if res.Outcome != Deduplicated {
		t.Errorf("Outcome = %v; want Deduplicated", res.Outcome)
	}

	if !e.deleted {
		t.Error("source was not deleted despite matching the existing destination")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("dest content = %q; want %q (existing file must be left intact)", got, content)
	}
	if _, statErr := os.Stat(dest + ".partial"); !os.IsNotExist(statErr) {
		t.Error("no temp file should be created when reconciling an existing duplicate")
	}
}

func TestTransferEntryCopyKeepsSource(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "out", "copied.dng")
	content := []byte("raw photo bytes")

	e := &fakeEntry{name: "copied.dng", bodies: [][]byte{content, content}}
	res, err := TransferEntry(e, dest, ffcfg.OperationCopy, ffcfg.OnConflictError)
	if err != nil {
		t.Fatalf("TransferEntry: %v", err)
	}
	if res.Outcome != Transferred {
		t.Errorf("Outcome = %v; want Transferred", res.Outcome)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("dest content = %q; want %q", got, content)
	}
	if e.deleted {
		t.Error("source was deleted by a copy")
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Error("temp .partial file was left behind")
	}
}

// A copy leaves the source behind, so every subsequent run re-encounters it and
// must recognise the destination as its own earlier copy rather than deleting
// anything or reporting a conflict.
func TestTransferEntryCopyExistingDuplicateKeepsBoth(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "out", "copied.dng")
	content := []byte("raw photo bytes")

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dest, content, 0644); err != nil {
		t.Fatalf("seed dest: %v", err)
	}

	e := &fakeEntry{name: "copied.dng", bodies: [][]byte{content}}
	res, err := TransferEntry(e, dest, ffcfg.OperationCopy, ffcfg.OnConflictError)
	if err != nil {
		t.Fatalf("TransferEntry: %v", err)
	}
	if res.Outcome != Deduplicated {
		t.Errorf("Outcome = %v; want Deduplicated", res.Outcome)
	}
	if e.deleted {
		t.Error("source was deleted by a copy that hit an existing duplicate")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("dest content = %q; want %q (existing file must be left intact)", got, content)
	}
}

func TestTransferEntryExistingDifferentContentErrors(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "moved.dng")
	existing := []byte("a totally different file")

	if err := os.WriteFile(dest, existing, 0644); err != nil {
		t.Fatalf("seed dest: %v", err)
	}

	e := &fakeEntry{name: "moved.dng", bodies: [][]byte{[]byte("incoming source bytes")}}
	_, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictError)
	if err == nil {
		t.Fatal("expected error when destination exists with different content, got nil")
	}
	if e.deleted {
		t.Error("source was deleted despite the destination differing")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(existing) {
		t.Errorf("existing dest was modified: got %q; want %q", got, existing)
	}
}

func TestPreviewTransfer(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte("raw photo bytes")

	t.Run("missing destination would move", func(t *testing.T) {
		dest := filepath.Join(tmpDir, "absent", "moved.dng")
		e := &fakeEntry{name: "moved.dng", bodies: [][]byte{content}}
		res, err := PreviewTransfer(e, dest, ffcfg.OnConflictError)
		if err != nil {
			t.Fatalf("PreviewTransfer: %v", err)
		}
		if res.Outcome != Transferred {
			t.Errorf("Outcome = %v; want Transferred", res.Outcome)
		}
		if e.opens != 0 {
			t.Errorf("source opened %d times; want 0 when destination is absent", e.opens)
		}
	})

	t.Run("matching destination would dedup without touching files", func(t *testing.T) {
		dest := filepath.Join(tmpDir, "dup.dng")
		if err := os.WriteFile(dest, content, 0644); err != nil {
			t.Fatalf("seed dest: %v", err)
		}
		e := &fakeEntry{name: "dup.dng", bodies: [][]byte{content}}
		res, err := PreviewTransfer(e, dest, ffcfg.OnConflictError)
		if err != nil {
			t.Fatalf("PreviewTransfer: %v", err)
		}
		if res.Outcome != Deduplicated {
			t.Errorf("Outcome = %v; want Deduplicated", res.Outcome)
		}
		if e.deleted {
			t.Error("PreviewTransfer must not delete the source")
		}
	})

	t.Run("differing destination errors without touching files", func(t *testing.T) {
		dest := filepath.Join(tmpDir, "conflict.dng")
		existing := []byte("a totally different file")
		if err := os.WriteFile(dest, existing, 0644); err != nil {
			t.Fatalf("seed dest: %v", err)
		}
		e := &fakeEntry{name: "conflict.dng", bodies: [][]byte{content}}
		_, err := PreviewTransfer(e, dest, ffcfg.OnConflictError)
		if err == nil {
			t.Fatal("expected error for differing destination, got nil")
		}
		if e.deleted {
			t.Error("PreviewTransfer must not delete the source")
		}
		got, _ := os.ReadFile(dest)
		if string(got) != string(existing) {
			t.Errorf("existing dest modified: got %q; want %q", got, existing)
		}
	})
}

func TestTransferEntryHashMismatchDoesNotDelete(t *testing.T) {
	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "moved.dng")

	// First Open (copy) and second Open (verify re-read) differ → corruption.
	e := &fakeEntry{name: "moved.dng", bodies: [][]byte{[]byte("good-copy-bytes"), []byte("DIFFERENT-bytes")}}
	_, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictError)
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

// TestCollisionErrorIdenticalInBothModes locks in that a destination that
// exists with different content is reported identically by the dry run and the
// real move: it is the same situation, so it must not read (or be presented) as
// two different problems depending on whether --ack was passed.
func TestCollisionErrorIdenticalInBothModes(t *testing.T) {
	content := []byte("incoming source bytes")
	existing := []byte("a totally different file")

	seed := func(t *testing.T) (string, *fakeEntry) {
		t.Helper()
		dest := filepath.Join(t.TempDir(), "conflict.dng")
		if err := os.WriteFile(dest, existing, 0644); err != nil {
			t.Fatalf("seed dest: %v", err)
		}
		return dest, &fakeEntry{name: "conflict.dng", bodies: [][]byte{content}}
	}

	previewDest, previewEntry := seed(t)
	_, previewErr := PreviewTransfer(previewEntry, previewDest, ffcfg.OnConflictError)
	if previewErr == nil {
		t.Fatal("PreviewTransfer: expected error for differing destination, got nil")
	}

	moveDest, moveEntry := seed(t)
	_, moveErr := TransferEntry(moveEntry, moveDest, ffcfg.OperationMove, ffcfg.OnConflictError)
	if moveErr == nil {
		t.Fatal("TransferEntry: expected error for differing destination, got nil")
	}

	// The paths differ per temp dir, so compare the messages with the
	// destination path factored out.
	normalize := func(err error, dest string) string {
		return strings.ReplaceAll(err.Error(), dest, "<dest>")
	}
	if got, want := normalize(moveErr, moveDest), normalize(previewErr, previewDest); got != want {
		t.Errorf("TransferEntry error = %q; want the same as PreviewTransfer's %q", got, want)
	}
}

// jpegOf renders a solid-grey JPEG of the given size. Dimensions are what the
// keep-largest policy judges on, and the fill lets two images of the same size
// differ in content.
func jpegOf(t *testing.T, w, h int, fill uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = fill
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func altOf(dest string) string {
	ext := filepath.Ext(dest)
	return strings.TrimSuffix(dest, ext) + "-alt" + ext
}

// TestKeepLargestSetsAsideSmallerDestination is the Picasa-crop case: the
// original arrives and finds a smaller edit already holding its target path.
// The original takes the path and the crop is renamed beside it.
func TestKeepLargestSetsAsideSmallerDestination(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "2016-12-21-19-41-38.jpg")
	crop := jpegOf(t, 64, 64, 0x40)
	if err := os.WriteFile(dest, crop, 0o644); err != nil {
		t.Fatal(err)
	}
	original := jpegOf(t, 256, 192, 0x80)
	e := &fakeEntry{name: "IMG_20161221_194137.jpg", bodies: [][]byte{original}}

	res, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictKeepLargest)
	if err != nil {
		t.Fatalf("TransferEntry: %v", err)
	}
	if res.Outcome != DestSetAside {
		t.Fatalf("Outcome = %v; want DestSetAside", res.Outcome)
	}
	if res.AltPath != altOf(dest) {
		t.Errorf("AltPath = %q; want %q", res.AltPath, altOf(dest))
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, original) {
		t.Error("target path does not hold the original")
	}
	if got, _ := os.ReadFile(res.AltPath); !bytes.Equal(got, crop) {
		t.Error("alt path does not hold the crop")
	}
	if !e.deleted {
		t.Error("source was not deleted after a move")
	}
}

// TestKeepLargestSetsAsideSmallerSource is the mirror image: the incoming file
// is the crop, so the original at the target path is left strictly alone and
// the crop is filed under the alt name.
func TestKeepLargestSetsAsideSmallerSource(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "2016-12-21-19-41-38.jpg")
	original := jpegOf(t, 256, 192, 0x80)
	if err := os.WriteFile(dest, original, 0o644); err != nil {
		t.Fatal(err)
	}
	crop := jpegOf(t, 64, 64, 0x40)
	e := &fakeEntry{name: "crop.jpg", bodies: [][]byte{crop}}

	res, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictKeepLargest)
	if err != nil {
		t.Fatalf("TransferEntry: %v", err)
	}
	if res.Outcome != SourceSetAside {
		t.Fatalf("Outcome = %v; want SourceSetAside", res.Outcome)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, original) {
		t.Error("the original at the target path was disturbed")
	}
	if got, _ := os.ReadFile(res.AltPath); !bytes.Equal(got, crop) {
		t.Error("alt path does not hold the crop")
	}
}

// TestKeepLargestIsIdempotent guards against -alt2, -alt3 piling up: importing
// the same crop again finds it already parked and treats it as a duplicate.
func TestKeepLargestIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "shot.jpg")
	if err := os.WriteFile(dest, jpegOf(t, 256, 192, 0x80), 0o644); err != nil {
		t.Fatal(err)
	}
	crop := jpegOf(t, 64, 64, 0x40)

	first := &fakeEntry{name: "crop.jpg", bodies: [][]byte{crop}}
	if _, err := TransferEntry(first, dest, ffcfg.OperationMove, ffcfg.OnConflictKeepLargest); err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	second := &fakeEntry{name: "crop.jpg", bodies: [][]byte{crop}}
	res, err := TransferEntry(second, dest, ffcfg.OperationMove, ffcfg.OnConflictKeepLargest)
	if err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	if res.Outcome != Deduplicated {
		t.Fatalf("Outcome = %v; want Deduplicated", res.Outcome)
	}
	if res.AltPath != altOf(dest) {
		t.Errorf("AltPath = %q; want %q", res.AltPath, altOf(dest))
	}
	if !second.deleted {
		t.Error("duplicate source was not deleted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v; want just the original and one -alt", names)
	}
}

// TestKeepLargestDeclinesWhenItCannotSee covers the deliberate limits of the
// policy: same pixel count, or content whose dimensions cannot be read, falls
// back to the untouched-and-report behaviour.
func TestKeepLargestDeclinesWhenItCannotSee(t *testing.T) {
	cases := map[string]struct{ destBody, srcBody []byte }{
		"equal dimensions": {jpegOf(t, 128, 128, 0x20), jpegOf(t, 128, 128, 0x90)},
		"not an image":     {[]byte("destination bytes"), []byte("source bytes!!!!!")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "shot.jpg")
			if err := os.WriteFile(dest, tc.destBody, 0o644); err != nil {
				t.Fatal(err)
			}
			e := &fakeEntry{name: "src.jpg", bodies: [][]byte{tc.srcBody}}

			if _, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictKeepLargest); err == nil {
				t.Fatal("expected an error, got none")
			}
			if got, _ := os.ReadFile(dest); !bytes.Equal(got, tc.destBody) {
				t.Error("destination was modified")
			}
			if e.deleted {
				t.Error("source was deleted")
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("directory gained files: %d entries", len(entries))
			}
		})
	}
}

// TestPreviewMatchesKeepLargestTransfer pins the dry run to the real run: the
// preview must announce the same resolution and change nothing.
func TestPreviewMatchesKeepLargestTransfer(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "shot.jpg")
	crop := jpegOf(t, 64, 64, 0x40)
	if err := os.WriteFile(dest, crop, 0o644); err != nil {
		t.Fatal(err)
	}
	original := jpegOf(t, 256, 192, 0x80)
	e := &fakeEntry{name: "src.jpg", bodies: [][]byte{original}}

	res, err := PreviewTransfer(e, dest, ffcfg.OnConflictKeepLargest)
	if err != nil {
		t.Fatalf("PreviewTransfer: %v", err)
	}
	if res.Outcome != DestSetAside || res.AltPath != altOf(dest) {
		t.Errorf("preview = %+v; want DestSetAside at %s", res, altOf(dest))
	}
	if res.Detail == "" {
		t.Error("preview gave no reason for the set-aside")
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, crop) {
		t.Error("preview modified the destination")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("preview created files: %d entries", len(entries))
	}
	if e.deleted {
		t.Error("preview deleted the source")
	}
}

// TestErrorPolicyStillRefuses keeps the default honest: without an opt-in, a
// colliding rendition is still a hands-off error.
func TestErrorPolicyStillRefuses(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "shot.jpg")
	if err := os.WriteFile(dest, jpegOf(t, 64, 64, 0x40), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &fakeEntry{name: "src.jpg", bodies: [][]byte{jpegOf(t, 256, 192, 0x80)}}
	if _, err := TransferEntry(e, dest, ffcfg.OperationMove, ffcfg.OnConflictError); err == nil {
		t.Fatal("expected an error under the default policy")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("directory gained files: %d entries", len(entries))
	}
}
