package file

import (
	"bytes"
	"fmt"
	"image"
	// Registered for image.DecodeConfig, which reads only the header.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
)

// rendition is everything about an image that can be compared without a human
// looking at it: its pixel geometry, how finely it was encoded, and how many
// bytes that took. Together these tell an original from a crop, a downscale, or
// a recompression of the same shot.
type rendition struct {
	width  int
	height int
	// dimsOK reports whether the geometry could be read at all. Formats Go
	// cannot decode (RAW, HEIC) have no opinion, which is not the same as being
	// a zero-size image.
	dimsOK bool
	// quant is the mean JPEG quantization step, lower being the finer encode;
	// quantOK reports whether the content was a JPEG carrying tables to read.
	quant   float64
	quantOK bool
	// bytes is the encoded size, which is always known.
	bytes int64
}

func (r rendition) pixels() int64 { return int64(r.width) * int64(r.height) }

// geometry renders the pixel dimensions for a human-readable message, or a
// stand-in phrase when they could not be read.
func (r rendition) geometry() string {
	if !r.dimsOK {
		return "of unreadable dimensions"
	}
	return fmt.Sprintf("%dx%d", r.width, r.height)
}

// renditionHeaderBudget caps how much of a file is buffered to inspect it. Both
// the geometry and the quantization tables live in the header, so this is
// generous for any real image; it only ever costs a full read on a conflict,
// which is rare.
const renditionHeaderBudget = 4 << 20

// renditionOf reads what can be compared from the head of an image's content
// plus its already-known encoded size. Nothing here fails: a format that yields
// no geometry and no tables still has a byte count, and the comparison degrades
// to that.
func renditionOf(r io.Reader, size int64) rendition {
	rend := rendition{bytes: size}
	head, err := io.ReadAll(io.LimitReader(r, renditionHeaderBudget))
	if err != nil && len(head) == 0 {
		return rend
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(head)); err == nil {
		rend.width, rend.height, rend.dimsOK = cfg.Width, cfg.Height, true
	}
	if q, err := jpegQuantization(bytes.NewReader(head)); err == nil {
		rend.quant, rend.quantOK = q, true
	}
	return rend
}

// renditionOfEntry inspects a source entry. For an MTP entry this streams off
// the device, so it is deliberately confined to the conflict path.
func renditionOfEntry(entry Entry) rendition {
	rc, err := entry.Open()
	if err != nil {
		return rendition{bytes: entry.Size()}
	}
	defer rc.Close()
	return renditionOf(rc, entry.Size())
}

// renditionOfFile is renditionOfEntry for a file already on disk.
func renditionOfFile(path string) rendition {
	f, err := os.Open(path)
	if err != nil {
		return rendition{}
	}
	defer f.Close()
	var size int64
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}
	return renditionOf(f, size)
}

// renditionSignal names the property that decided a comparison, so the run can
// explain itself.
type renditionSignal int

const (
	// signalNone means the two renditions are indistinguishable.
	signalNone renditionSignal = iota
	// signalPixels means one has more pixels: it is the original, the other a
	// crop or a downscaled export.
	signalPixels
	// signalQuantization means the geometry matched but one is the finer JPEG
	// encode: the other has been through a recompression.
	signalQuantization
	// signalBytes means nothing else could separate them and one simply holds
	// more data — the last resort, and the only signal available for formats
	// whose headers cannot be read at all.
	signalBytes
)

// compareRenditions ranks two renditions of the same shot, cascading from the
// most meaningful signal to the least: pixel count first (an original beats a
// crop regardless of how either was encoded), then JPEG quantization (the finer
// encode beats a recompression of the same geometry), then sheer byte count.
//
// It returns a positive number when a is the one worth keeping, a negative one
// when b is, and zero — with signalNone — when the two cannot be separated,
// which leaves the caller no honest choice but to report the collision.
func compareRenditions(a, b rendition) (int, renditionSignal) {
	if a.dimsOK && b.dimsOK && a.pixels() != b.pixels() {
		return sign(a.pixels() > b.pixels()), signalPixels
	}
	if a.quantOK && b.quantOK && a.quant != b.quant {
		// Lower quantization steps mean less was thrown away.
		return sign(a.quant < b.quant), signalQuantization
	}
	if a.bytes != b.bytes {
		return sign(a.bytes > b.bytes), signalBytes
	}
	return 0, signalNone
}

func sign(aWins bool) int {
	if aWins {
		return 1
	}
	return -1
}

// HumanSize renders a byte count in the largest binary unit that keeps it
// readable, e.g. "100.0 MiB".
func HumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTP"[exp])
}
