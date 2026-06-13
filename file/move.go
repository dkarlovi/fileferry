package file

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
)

// MoveEntry moves a source entry to destPath on the local filesystem with a
// copy → verify → delete strategy that is safe for MTP sources (which cannot be
// renamed in place):
//
//  1. copy the content to a temporary file next to destPath, hashing as it goes;
//  2. re-read the source and require its SHA-256 to equal the copy's, and its
//     length to match the reported size;
//  3. only then rename the temp file into place and delete the source.
//
// MoveOutcome describes how MoveEntry resolved a move.
type MoveOutcome int

const (
	// Moved means the source content was copied to the destination, verified
	// and the source deleted.
	Moved MoveOutcome = iota
	// Deduplicated means the destination already held an identical file, so the
	// source was deleted without copying anything. This is not the happy path
	// (it means a duplicate was downloaded), but it is not an error either.
	Deduplicated
)

// On any failure the temp file is removed and the source is left intact.
//
// If a file already exists at destPath, it is treated as a possible accidental
// duplicate (e.g. the same file downloaded twice): the source and the existing
// destination are compared by SHA-256. If they match, the destination is left
// as-is and the source is deleted, as if this run had performed the move, and
// Deduplicated is returned. If they differ, an error is returned and neither
// file is touched.
func MoveEntry(entry Entry, destPath string) (MoveOutcome, error) {
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return Moved, fmt.Errorf("create dir %s: %w", destDir, err)
	}

	// A pre-existing destination is reconciled by checksum rather than blindly
	// overwritten, so we never clobber a different file and never re-copy a
	// duplicate we already have.
	if info, err := os.Stat(destPath); err == nil {
		if info.IsDir() {
			return Moved, fmt.Errorf("destination %s is a directory", destPath)
		}
		return reconcileExisting(entry, destPath)
	} else if !os.IsNotExist(err) {
		return Moved, fmt.Errorf("stat destination %s: %w", destPath, err)
	}

	tmpPath := destPath + ".partial"
	// Copy source -> temp, computing the destination hash inline.
	destHash, written, err := copyToTemp(entry, tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return Moved, err
	}

	// Verify the copy by re-reading the source and comparing hashes. This is the
	// guarantee required before deleting anything from the device.
	if size := entry.Size(); size >= 0 && written != size {
		os.Remove(tmpPath)
		return Moved, fmt.Errorf("copy size mismatch for %s: wrote %d bytes, source reports %d", entry.DisplayPath(), written, size)
	}
	srcHash, err := hashEntry(entry)
	if err != nil {
		os.Remove(tmpPath)
		return Moved, fmt.Errorf("re-read source %s for verification: %w", entry.DisplayPath(), err)
	}
	if srcHash != destHash {
		os.Remove(tmpPath)
		return Moved, fmt.Errorf("verification failed for %s: source and copied file differ (SHA-256 %s != %s)", entry.DisplayPath(), srcHash, destHash)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return Moved, fmt.Errorf("finalize %s: %w", destPath, err)
	}

	if err := entry.Delete(); err != nil {
		return Moved, fmt.Errorf("copied and verified to %s but failed to delete source %s: %w", destPath, entry.DisplayPath(), err)
	}
	return Moved, nil
}

// reconcileExisting handles the case where destPath already holds a file. It
// hashes both the existing destination and the source: on a match the source is
// a duplicate that was effectively already moved here, so it is deleted; on a
// mismatch both files are left untouched and an error is returned.
func reconcileExisting(entry Entry, destPath string) (MoveOutcome, error) {
	if _, err := compareDestination(entry, destPath); err != nil {
		return Moved, err
	}

	// Identical content: this is a duplicate of a file already moved into place.
	if err := entry.Delete(); err != nil {
		return Moved, fmt.Errorf("source %s is a duplicate of %s but failed to delete: %w", entry.DisplayPath(), destPath, err)
	}
	return Deduplicated, nil
}

// PreviewMove reports what MoveEntry would do, without modifying either file. It
// is the dry-run counterpart of MoveEntry: if the destination already exists it
// hashes both files, returning Deduplicated when they match or an error when
// they differ; otherwise it returns Moved.
func PreviewMove(entry Entry, destPath string) (MoveOutcome, error) {
	info, err := os.Stat(destPath)
	if os.IsNotExist(err) {
		return Moved, nil
	}
	if err != nil {
		return Moved, fmt.Errorf("stat destination %s: %w", destPath, err)
	}
	if info.IsDir() {
		return Moved, fmt.Errorf("destination %s is a directory", destPath)
	}
	return compareDestination(entry, destPath)
}

// compareDestination hashes the existing file at destPath and the source entry.
// It returns Deduplicated when their SHA-256 sums match, or an error describing
// the mismatch otherwise. It never modifies either file, so it is shared by the
// move (reconcileExisting) and dry-run (PreviewMove) paths.
func compareDestination(entry Entry, destPath string) (MoveOutcome, error) {
	destHash, err := hashFile(destPath)
	if err != nil {
		return Moved, fmt.Errorf("hash existing destination %s: %w", destPath, err)
	}
	srcHash, err := hashEntry(entry)
	if err != nil {
		return Moved, fmt.Errorf("re-read source %s for duplicate check: %w", entry.DisplayPath(), err)
	}
	if srcHash != destHash {
		return Moved, fmt.Errorf("destination %s already exists and differs from source %s (SHA-256 %s != %s); leaving both untouched", destPath, entry.DisplayPath(), destHash, srcHash)
	}
	return Deduplicated, nil
}

// hashFile computes the hex SHA-256 of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hexSum(h), nil
}

// copyToTemp streams the entry's content into tmpPath, returning the hex SHA-256
// of what was written and the number of bytes written.
func copyToTemp(entry Entry, tmpPath string) (hexHash string, written int64, err error) {
	rc, err := entry.Open()
	if err != nil {
		return "", 0, fmt.Errorf("open source %s: %w", entry.DisplayPath(), err)
	}
	defer rc.Close()

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", 0, fmt.Errorf("create temp %s: %w", tmpPath, err)
	}
	defer f.Close()

	h := sha256.New()
	written, err = io.Copy(io.MultiWriter(f, h), rc)
	if err != nil {
		return "", 0, fmt.Errorf("copy %s: %w", entry.DisplayPath(), err)
	}
	if err := f.Sync(); err != nil {
		return "", 0, fmt.Errorf("flush %s: %w", tmpPath, err)
	}
	return hexSum(h), written, nil
}

// hashEntry computes the hex SHA-256 of the entry's full content.
func hashEntry(entry Entry) (string, error) {
	rc, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err
	}
	return hexSum(h), nil
}

func hexSum(h hash.Hash) string {
	return fmt.Sprintf("%x", h.Sum(nil))
}
