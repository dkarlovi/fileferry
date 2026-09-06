package file

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"

	mkvparse "github.com/remko/go-mkvparse"
)

// encodeGray renders a gradient JPEG at a pinned quality. The gradient (rather
// than a flat fill) gives the encoder something to quantize, so the tables
// actually differ between qualities.
func encodeGray(t *testing.T, w, h, quality int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = uint8(i % 251)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestJPEGQuantizationRanksQuality(t *testing.T) {
	var prev float64
	for _, quality := range []int{95, 75, 50, 25} {
		got, err := jpegQuantization(bytes.NewReader(encodeGray(t, 64, 64, quality)))
		if err != nil {
			t.Fatalf("jpegQuantization(quality %d): %v", quality, err)
		}
		if got <= 0 {
			t.Fatalf("jpegQuantization(quality %d) = %v; want a positive mean step", quality, got)
		}
		// Lower encoder quality must mean coarser (larger) quantization steps.
		if prev != 0 && got <= prev {
			t.Errorf("quality %d gave mean step %v; want more than the previous %v", quality, got, prev)
		}
		prev = got
	}
}

func TestJPEGQuantizationRejectsNonJPEG(t *testing.T) {
	cases := map[string][]byte{
		"empty":          {},
		"truncated soi":  {0xff},
		"not a jpeg":     []byte("plain text, definitely not a jpeg"),
		"soi then junk":  {0xff, 0xd8, 0x00, 0x01, 0x02, 0x03},
		"soi then eoi":   {0xff, 0xd8, 0xff, 0xd9},
		"png magic only": {0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := jpegQuantization(bytes.NewReader(body)); err == nil {
				t.Error("jpegQuantization() = nil error; want a failure")
			}
		})
	}
}

func TestRenditionOfReadsWhatItCan(t *testing.T) {
	body := encodeGray(t, 96, 48, 80)
	rend := renditionOf(bytes.NewReader(body), int64(len(body)))
	if !rend.dimsOK || rend.width != 96 || rend.height != 48 {
		t.Errorf("geometry = %s (ok=%v); want 96x48", rend.geometry(), rend.dimsOK)
	}
	if !rend.quantOK || rend.quant <= 0 {
		t.Errorf("quant = %v (ok=%v); want a positive mean step", rend.quant, rend.quantOK)
	}
	if rend.bytes != int64(len(body)) {
		t.Errorf("bytes = %d; want %d", rend.bytes, len(body))
	}

	// Undecodable content still yields a usable rendition: the byte count.
	opaque := []byte("not an image at all")
	rend = renditionOf(bytes.NewReader(opaque), int64(len(opaque)))
	if rend.dimsOK || rend.quantOK {
		t.Errorf("opaque content reported dims=%v quant=%v; want neither", rend.dimsOK, rend.quantOK)
	}
	if rend.bytes != int64(len(opaque)) {
		t.Errorf("bytes = %d; want %d", rend.bytes, len(opaque))
	}
	if got := rend.geometry(); got != "of unreadable dimensions" {
		t.Errorf("geometry = %q; want the unreadable stand-in", got)
	}
}

func TestCompareRenditions(t *testing.T) {
	tests := []struct {
		name       string
		a, b       rendition
		wantSign   int
		wantSignal renditionSignal
	}{
		{
			name:       "more pixels wins",
			a:          rendition{width: 256, height: 192, dimsOK: true, bytes: 10},
			b:          rendition{width: 64, height: 64, dimsOK: true, bytes: 999},
			wantSign:   1,
			wantSignal: signalPixels,
		},
		{
			name: "pixels outrank a finer encode",
			// The bigger image is the coarser encode and the smaller file; it
			// still wins, because a crop can never be recovered from.
			a:          rendition{width: 256, height: 192, dimsOK: true, quant: 20, quantOK: true, bytes: 10},
			b:          rendition{width: 64, height: 64, dimsOK: true, quant: 2, quantOK: true, bytes: 999},
			wantSign:   1,
			wantSignal: signalPixels,
		},
		{
			name:       "equal pixels fall through to quantization",
			a:          rendition{width: 128, height: 128, dimsOK: true, quant: 3, quantOK: true, bytes: 10},
			b:          rendition{width: 128, height: 128, dimsOK: true, quant: 9, quantOK: true, bytes: 999},
			wantSign:   1,
			wantSignal: signalQuantization,
		},
		{
			name:       "coarser encode loses",
			a:          rendition{width: 128, height: 128, dimsOK: true, quant: 9, quantOK: true, bytes: 999},
			b:          rendition{width: 128, height: 128, dimsOK: true, quant: 3, quantOK: true, bytes: 10},
			wantSign:   -1,
			wantSignal: signalQuantization,
		},
		{
			name:       "unreadable geometry falls through to size",
			a:          rendition{bytes: 900},
			b:          rendition{bytes: 100},
			wantSign:   1,
			wantSignal: signalBytes,
		},
		{
			name: "one-sided quantization falls through to size",
			// Only one side is a JPEG, so the tables are not comparable.
			a:          rendition{width: 128, height: 128, dimsOK: true, quant: 3, quantOK: true, bytes: 10},
			b:          rendition{width: 128, height: 128, dimsOK: true, bytes: 999},
			wantSign:   -1,
			wantSignal: signalBytes,
		},
		{
			name:       "identical on every signal",
			a:          rendition{width: 128, height: 128, dimsOK: true, quant: 3, quantOK: true, bytes: 500},
			b:          rendition{width: 128, height: 128, dimsOK: true, quant: 3, quantOK: true, bytes: 500},
			wantSign:   0,
			wantSignal: signalNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, signal := compareRenditions(tt.a, tt.b)
			if sgn(got) != tt.wantSign || signal != tt.wantSignal {
				t.Errorf("compareRenditions() = (%d, %v); want sign %d with %v", got, signal, tt.wantSign, tt.wantSignal)
			}
			// The ranking must be symmetric: swapping the arguments flips the
			// verdict and keeps the signal.
			rev, revSignal := compareRenditions(tt.b, tt.a)
			if sgn(rev) != -tt.wantSign || revSignal != tt.wantSignal {
				t.Errorf("compareRenditions(reversed) = (%d, %v); want sign %d with %v", rev, revSignal, -tt.wantSign, tt.wantSignal)
			}
		})
	}
}

func sgn(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func TestHumanSize(t *testing.T) {
	tests := map[int64]string{
		0:               "0 B",
		512:             "512 B",
		1024:            "1.0 KiB",
		1536:            "1.5 KiB",
		1024 * 1024:     "1.0 MiB",
		3 * 1024 * 1024: "3.0 MiB",
	}
	for size, want := range tests {
		if got := HumanSize(size); got != want {
			t.Errorf("HumanSize(%d) = %q; want %q", size, got, want)
		}
	}
}

// mp4Box wraps a payload in a box header: a big-endian length covering the
// whole box, then the four-character type.
func mp4Box(typ string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	box := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(box, uint32(8+len(body)))
	copy(box[4:], typ)
	return append(box, body...)
}

// tkhdOf builds a version 0 track header reporting the given display size.
// Everything else is left at zero except the unity matrix, which go-mp4 still
// has to read past to reach the size at the end.
func tkhdOf(w, h int) []byte {
	p := make([]byte, 84)
	// [0:4] version and flags, [4:32] times, ids and durations, [32:40] layer,
	// alternate group, volume and a reserved pair — all zero is valid here.
	matrix := []uint32{0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000}
	for i, v := range matrix {
		binary.BigEndian.PutUint32(p[40+i*4:], v)
	}
	// Width and height are 16.16 fixed point.
	binary.BigEndian.PutUint32(p[76:], uint32(w)<<16)
	binary.BigEndian.PutUint32(p[80:], uint32(h)<<16)
	return mp4Box("tkhd", p)
}

// mp4Of assembles a movie out of one track header per given size.
func mp4Of(sizes ...[2]int) []byte {
	traks := [][]byte{}
	for _, s := range sizes {
		traks = append(traks, mp4Box("trak", tkhdOf(s[0], s[1])))
	}
	return append(mp4Box("ftyp", []byte("isomiso2mp41")), mp4Box("moov", traks...)...)
}

func TestVideoGeometryReadsMP4TrackHeaders(t *testing.T) {
	// A real phone video carries the picture alongside tracks that have no
	// size of their own: sound, and Pixel's "mett" telemetry track. The
	// picture is the largest of them.
	body := mp4Of([2]int{0, 0}, [2]int{1920, 1080}, [2]int{0, 0})
	w, h, ok := videoGeometry(bytes.NewReader(body), int64(len(body)), "mp4")
	if !ok || w != 1920 || h != 1080 {
		t.Errorf("videoGeometry() = %dx%d (ok=%v); want 1920x1080", w, h, ok)
	}
}

func TestVideoGeometryIgnoresWhatItCannotRead(t *testing.T) {
	body := mp4Of([2]int{1920, 1080})
	// The right boxes under the wrong extension are not a video.
	if _, _, ok := videoGeometry(bytes.NewReader(body), int64(len(body)), "jpg"); ok {
		t.Error("videoGeometry() read geometry for a jpg; want none")
	}
	// Neither is a video extension over content that holds no boxes.
	junk := []byte("not a container at all")
	if _, _, ok := videoGeometry(bytes.NewReader(junk), int64(len(junk)), "mp4"); ok {
		t.Error("videoGeometry() read geometry from junk; want none")
	}
	if _, _, ok := videoGeometry(bytes.NewReader(nil), 0, "mp4"); ok {
		t.Error("videoGeometry() read geometry from an empty file; want none")
	}
}

// TestRenditionOfContentRanksVideos is the case this signal exists for: a phone
// writes a downscaled rendition of a clip next to the original, both carrying
// the same capture time, so both want the same target name. Byte count alone
// cannot be trusted to tell them apart — here the derived clip is the larger
// file — but the pixel count can.
func TestRenditionOfContentRanksVideos(t *testing.T) {
	original := mp4Of([2]int{3840, 2160})
	derived := mp4Of([2]int{1920, 1080})

	orig := renditionOfContent(bytes.NewReader(original), int64(len(original)), "mp4")
	if !orig.dimsOK || orig.width != 3840 || orig.height != 2160 {
		t.Fatalf("geometry = %s (ok=%v); want 3840x2160", orig.geometry(), orig.dimsOK)
	}
	// Give the derived clip the heavier encode, so only geometry can decide.
	drv := renditionOfContent(bytes.NewReader(derived), int64(len(derived))*10, "mp4")

	cmp, by := compareRenditions(orig, drv)
	if cmp <= 0 || by != signalPixels {
		t.Errorf("compareRenditions() = %d by %v; want the original to win on pixels", cmp, by)
	}
}

func TestPixelHandlerKeepsTheLargestTrack(t *testing.T) {
	h := &pixelHandler{}
	for _, dims := range [][2]int64{{1920, 1080}, {320, 240}} {
		if err := h.HandleInteger(mkvparse.PixelWidthElement, dims[0], mkvparse.ElementInfo{}); err != nil {
			t.Fatalf("HandleInteger(PixelWidth): %v", err)
		}
		if err := h.HandleInteger(mkvparse.PixelHeightElement, dims[1], mkvparse.ElementInfo{}); err != nil {
			t.Fatalf("HandleInteger(PixelHeight): %v", err)
		}
	}
	if err := h.HandleInteger(mkvparse.ElementID(0x1234), 9999, mkvparse.ElementInfo{}); err != nil {
		t.Fatalf("HandleInteger(other): %v", err)
	}
	if h.width != 1920 || h.height != 1080 {
		t.Errorf("pixelHandler = %dx%d; want 1920x1080", h.width, h.height)
	}

	// Clusters hold the frames, which is the bulk of the file and none of the
	// answer, so the parse must not descend into them.
	if descend, _ := h.HandleMasterBegin(mkvparse.ClusterElement, mkvparse.ElementInfo{}); descend {
		t.Error("HandleMasterBegin(Cluster) = true; want the parse to skip clusters")
	}
	if descend, _ := h.HandleMasterBegin(mkvparse.TracksElement, mkvparse.ElementInfo{}); !descend {
		t.Error("HandleMasterBegin(Tracks) = false; want the parse to descend")
	}
}
