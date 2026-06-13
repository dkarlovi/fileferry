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
// On any failure the temp file is removed and the source is left intact.
func MoveEntry(entry Entry, destPath string) error {
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", destDir, err)
	}

	tmpPath := destPath + ".partial"
	// Copy source -> temp, computing the destination hash inline.
	destHash, written, err := copyToTemp(entry, tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Verify the copy by re-reading the source and comparing hashes. This is the
	// guarantee required before deleting anything from the device.
	if size := entry.Size(); size >= 0 && written != size {
		os.Remove(tmpPath)
		return fmt.Errorf("copy size mismatch for %s: wrote %d bytes, source reports %d", entry.DisplayPath(), written, size)
	}
	srcHash, err := hashEntry(entry)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("re-read source %s for verification: %w", entry.DisplayPath(), err)
	}
	if srcHash != destHash {
		os.Remove(tmpPath)
		return fmt.Errorf("verification failed for %s: source and copied file differ (SHA-256 %s != %s)", entry.DisplayPath(), srcHash, destHash)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("finalize %s: %w", destPath, err)
	}

	if err := entry.Delete(); err != nil {
		return fmt.Errorf("copied and verified to %s but failed to delete source %s: %w", destPath, entry.DisplayPath(), err)
	}
	return nil
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
