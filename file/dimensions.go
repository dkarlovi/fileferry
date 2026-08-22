package file

import (
	"image"
	// Registered for image.DecodeConfig, which reads only the header.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// rendition is the pixel geometry of an image, used to tell an original from a
// crop or a downscaled export of the same shot.
type rendition struct {
	width  int
	height int
}

func (r rendition) pixels() int64 { return int64(r.width) * int64(r.height) }

// renditionOf reads the dimensions of an image from a reader-backed source. It
// decodes only the header, so it costs a few kilobytes rather than the whole
// file — which matters for an MTP entry, where reading streams off the device.
//
// The second return value reports whether the dimensions could be determined at
// all: formats Go cannot decode (RAW, HEIC) simply have no opinion, and callers
// must treat that as "unknown" rather than as a zero-size image.
func renditionOfEntry(entry Entry) (rendition, bool) {
	rc, err := entry.Open()
	if err != nil {
		return rendition{}, false
	}
	defer rc.Close()
	cfg, _, err := image.DecodeConfig(rc)
	if err != nil {
		return rendition{}, false
	}
	return rendition{width: cfg.Width, height: cfg.Height}, true
}

// renditionOfFile is renditionOfEntry for a file already on disk.
func renditionOfFile(path string) (rendition, bool) {
	f, err := os.Open(path)
	if err != nil {
		return rendition{}, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return rendition{}, false
	}
	return rendition{width: cfg.Width, height: cfg.Height}, true
}
