package file

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

// TransferOutcome describes how TransferEntry resolved a transfer.
type TransferOutcome int

const (
	// Transferred means the source content was copied to the destination and
	// verified (and, for a move, the source deleted afterwards).
	Transferred TransferOutcome = iota
	// Deduplicated means an identical file was already in place, so nothing was
	// copied. For a move the source is still deleted; for a copy it is simply
	// left alone. This is not the happy path for a move (it means a duplicate was
	// downloaded), but it is not an error either.
	Deduplicated
	// SourceSetAside means the target path was already held by a *better*
	// rendition of the same shot, so the source lost it and was stored under an
	// "-alt" name next to it.
	SourceSetAside
	// DestSetAside means the source was the better rendition, so the file already
	// at the target path was renamed to an "-alt" name and the source took its
	// place.
	DestSetAside
)

// TransferResult describes how a transfer resolved.
type TransferResult struct {
	Outcome TransferOutcome
	// AltPath is where the losing rendition ended up, set for the set-aside
	// outcomes and for a deduplication against an existing "-alt" file.
	AltPath string
	// AltHolds reports that the alt slot already holds an identical copy of the
	// losing rendition, so nothing needs to be written there.
	AltHolds bool
	// Detail is a human-readable reason for a set-aside: which rendition won and
	// on which signal. Empty for the ordinary outcomes.
	Detail string
}

// TransferEntry transfers a source entry to destPath on the local filesystem
// with a copy → verify → delete strategy that is safe for MTP sources (which
// cannot be renamed in place):
//
//  1. copy the content to a temporary file next to destPath, hashing as it goes;
//  2. re-read the source and require its SHA-256 to equal the copy's, and its
//     length to match the reported size;
//  3. only then rename the temp file into place and, when op is
//     config.OperationMove, delete the source.
//
// On any failure the temp file is removed and the source is left intact.
//
// If a file already exists at destPath it is reconciled rather than
// overwritten: identical content is a duplicate (see planExisting), and
// differing content is resolved according to policy — reported as an error, or
// settled in favour of the better rendition under
// config.OnConflictKeepHighestQuality.
func TransferEntry(entry Entry, destPath string, op ffcfg.Operation, policy ffcfg.OnConflict) (TransferResult, error) {
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return TransferResult{}, fmt.Errorf("create dir %s: %w", destDir, err)
	}

	// A pre-existing destination is reconciled by checksum rather than blindly
	// overwritten, so we never clobber a different file and never re-copy a
	// duplicate we already have.
	if info, err := os.Stat(destPath); err == nil {
		if info.IsDir() {
			return TransferResult{}, fmt.Errorf("destination %s is a directory", destPath)
		}
		plan, err := planExisting(entry, destPath, policy)
		if err != nil {
			return TransferResult{}, err
		}
		return applyPlan(entry, destPath, op, plan)
	} else if !os.IsNotExist(err) {
		return TransferResult{}, fmt.Errorf("stat destination %s: %w", destPath, err)
	}

	if err := transferTo(entry, destPath, op); err != nil {
		return TransferResult{}, err
	}
	return TransferResult{Outcome: Transferred}, nil
}

// applyPlan carries out what planExisting decided. It is the only place that
// mutates anything once a destination is known to exist.
func applyPlan(entry Entry, destPath string, op ffcfg.Operation, plan TransferResult) (TransferResult, error) {
	switch plan.Outcome {
	case Deduplicated:
		// Identical content is already in place (at destPath, or parked at an
		// "-alt" slot): this source was effectively transferred here already.
		if op == ffcfg.OperationCopy {
			return plan, nil
		}
		where := destPath
		if plan.AltPath != "" {
			where = plan.AltPath
		}
		if err := entry.Delete(); err != nil {
			return TransferResult{}, fmt.Errorf("source %s is a duplicate of %s but failed to delete: %w", entry.DisplayPath(), where, err)
		}
		return plan, nil

	case SourceSetAside:
		// The target path belongs to a better rendition; this one goes beside it.
		if err := transferTo(entry, plan.AltPath, op); err != nil {
			return TransferResult{}, err
		}
		return plan, nil

	case DestSetAside:
		// The source is the better rendition, so the loser vacates the target
		// path first. When an identical copy is already parked at the alt
		// slot, the file at destPath is a proven duplicate of it and is simply
		// removed instead of taking a second slot.
		if plan.AltHolds {
			if err := os.Remove(destPath); err != nil {
				return TransferResult{}, fmt.Errorf("remove superseded %s (already parked at %s): %w", destPath, plan.AltPath, err)
			}
		} else if err := os.Rename(destPath, plan.AltPath); err != nil {
			return TransferResult{}, fmt.Errorf("set aside %s -> %s: %w", destPath, plan.AltPath, err)
		}
		if err := transferTo(entry, destPath, op); err != nil {
			return TransferResult{}, err
		}
		return plan, nil
	}
	return plan, nil
}

// transferTo performs the copy → verify → delete sequence into an empty path.
func transferTo(entry Entry, destPath string, op ffcfg.Operation) error {
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

	if op == ffcfg.OperationCopy {
		return nil
	}
	if err := entry.Delete(); err != nil {
		return fmt.Errorf("copied and verified to %s but failed to delete source %s: %w", destPath, entry.DisplayPath(), err)
	}
	return nil
}

// PreviewTransfer reports what TransferEntry would do, without modifying
// anything. It is the dry-run counterpart of TransferEntry and runs the exact
// same decision, so a dry run and a real run never disagree.
func PreviewTransfer(entry Entry, destPath string, policy ffcfg.OnConflict) (TransferResult, error) {
	info, err := os.Stat(destPath)
	if os.IsNotExist(err) {
		return TransferResult{Outcome: Transferred}, nil
	}
	if err != nil {
		return TransferResult{}, fmt.Errorf("stat destination %s: %w", destPath, err)
	}
	if info.IsDir() {
		return TransferResult{}, fmt.Errorf("destination %s is a directory", destPath)
	}
	return planExisting(entry, destPath, policy)
}

// planExisting decides what to do about a destination that already exists,
// touching nothing. Identical content is a duplicate. Differing content is a
// collision between two different files that want the same name — under
// config.OnConflictError that is simply an error, and under
// config.OnConflictKeepHighestQuality it is settled by compareRenditions: the
// better rendition keeps the target path and the other is parked under an "-alt"
// name beside it, so neither is ever lost.
//
// The policy only declines to guess when the two are indistinguishable on every
// signal it has, which for differing content means they are the same size down
// to the byte. Then it falls back to the error, leaving both files untouched.
func planExisting(entry Entry, destPath string, policy ffcfg.OnConflict) (TransferResult, error) {
	destHash, err := hashFile(destPath)
	if err != nil {
		return TransferResult{}, fmt.Errorf("hash existing destination %s: %w", destPath, err)
	}
	srcHash, err := hashEntry(entry)
	if err != nil {
		return TransferResult{}, fmt.Errorf("re-read source %s for duplicate check: %w", entry.DisplayPath(), err)
	}
	if srcHash == destHash {
		return TransferResult{Outcome: Deduplicated}, nil
	}

	conflict := fmt.Errorf("destination %s already exists and differs from source %s (SHA-256 %s != %s); leaving both untouched", destPath, entry.DisplayPath(), destHash, srcHash)
	if policy != ffcfg.OnConflictKeepHighestQuality {
		return TransferResult{}, conflict
	}

	src := renditionOfEntry(entry)
	dst := renditionOfFile(destPath)
	cmp, by := compareRenditions(src, dst)
	if cmp == 0 {
		return TransferResult{}, conflict
	}
	detail := explainRendition(by, src, dst, cmp > 0)

	if cmp < 0 {
		alt, holds, err := findAltSlot(destPath, srcHash)
		if err != nil {
			return TransferResult{}, err
		}
		outcome := SourceSetAside
		if holds {
			// An identical copy is already parked there; nothing new to write.
			outcome = Deduplicated
		}
		return TransferResult{Outcome: outcome, AltPath: alt, AltHolds: holds, Detail: detail}, nil
	}

	alt, holds, err := findAltSlot(destPath, destHash)
	if err != nil {
		return TransferResult{}, err
	}
	return TransferResult{Outcome: DestSetAside, AltPath: alt, AltHolds: holds, Detail: detail}, nil
}

// explainRendition puts into words why one rendition beat the other, always
// phrased from the incoming source's point of view because that is the file the
// user is watching move.
func explainRendition(by renditionSignal, src, dst rendition, srcWins bool) string {
	pick := func(won, lost string) string {
		if srcWins {
			return won
		}
		return lost
	}
	switch by {
	case signalPixels:
		return fmt.Sprintf("source %s has %s pixels than the %s at the target path",
			src.geometry(), pick("more", "fewer"), dst.geometry())
	case signalQuantization:
		return fmt.Sprintf("both are %s, but the source is the %s JPEG encode (mean quantization step %.1f vs %.1f)",
			src.geometry(), pick("finer", "coarser"), src.quant, dst.quant)
	case signalBytes:
		return fmt.Sprintf("nothing but size separates them: the source is the %s encode (%s vs %s)",
			pick("larger", "smaller"), HumanSize(src.bytes), HumanSize(dst.bytes))
	}
	return "the two renditions could not be told apart"
}

// maxAltSlots caps the "-alt" search so a pathological directory cannot spin
// forever; in practice a shot has a handful of renditions at most.
const maxAltSlots = 100

// findAltSlot picks where the losing rendition goes: the first "-alt" name
// beside destPath that is either free, or already holds a file with hash
// loserHash — in which case the loser is already parked there and holds is
// true, which keeps repeated runs idempotent instead of piling up -alt2, -alt3.
// Slots taken by a *different* file are skipped.
func findAltSlot(destPath, loserHash string) (path string, holds bool, err error) {
	ext := filepath.Ext(destPath)
	stem := strings.TrimSuffix(destPath, ext)
	for i := 1; i <= maxAltSlots; i++ {
		candidate := fmt.Sprintf("%s-alt%s", stem, ext)
		if i > 1 {
			candidate = fmt.Sprintf("%s-alt%d%s", stem, i, ext)
		}
		info, statErr := os.Stat(candidate)
		if os.IsNotExist(statErr) {
			return candidate, false, nil
		}
		if statErr != nil {
			return "", false, fmt.Errorf("stat %s: %w", candidate, statErr)
		}
		if info.IsDir() {
			continue
		}
		sum, hashErr := hashFile(candidate)
		if hashErr != nil {
			return "", false, fmt.Errorf("hash %s: %w", candidate, hashErr)
		}
		if sum == loserHash {
			return candidate, true, nil
		}
	}
	return "", false, fmt.Errorf("no free -alt slot beside %s after %d tries", destPath, maxAltSlots)
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
