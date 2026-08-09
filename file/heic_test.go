package file

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// box assembles an ISO-BMFF box: 4-byte size, 4-char type, payload.
func box(boxType string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := make([]byte, 4, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(8+len(body)))
	out = append(out, boxType...)
	return append(out, body...)
}

func be16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// buildHEIF assembles a minimal HEIF container holding a single "Exif" item
// with the given payload, plus one unrelated item so the lookup has to
// discriminate. The item's `iloc` offset is patched once the final layout is
// known, mirroring how a real file points into its `mdat`.
func buildHEIF(exifPayload []byte) []byte {
	infeExif := box("infe", []byte{2, 0, 0, 0}, be16(3), be16(0), []byte("Exif"), []byte{0})
	infeImage := box("infe", []byte{2, 0, 0, 0}, be16(1), be16(0), []byte("hvc1"), []byte{0})
	iinf := box("iinf", []byte{0, 0, 0, 0}, be16(2), infeImage, infeExif)

	// offset_size=4, length_size=4, base_offset_size=0, reserved=0
	ilocEntries := bytes.Join([][]byte{
		// item 1 (the image): one extent, empty.
		be16(1), be16(0), be16(1), be32(0), be32(0),
		// item 3 (EXIF): offset patched below.
		be16(3), be16(0), be16(1), be32(0), be32(uint32(len(exifPayload))),
	}, nil)
	iloc := box("iloc", []byte{0, 0, 0, 0}, []byte{0x44, 0x00}, be16(2), ilocEntries)

	meta := box("meta", []byte{0, 0, 0, 0}, box("hdlr", make([]byte, 4)), iinf, iloc)
	ftyp := box("ftyp", []byte("heic"), be32(0), []byte("mif1heic"))

	head := append(append([]byte{}, ftyp...), meta...)
	mdat := box("mdat", exifPayload)
	exifOffset := uint32(len(head) + 8)

	// Patch the EXIF extent offset now that the header length is known.
	file := append(head, mdat...)
	idx := bytes.LastIndex(file[:len(head)], be32(uint32(len(exifPayload))))
	binary.BigEndian.PutUint32(file[idx-4:idx], exifOffset)
	return file
}

// exifPayload wraps TIFF bytes the way a HEIF EXIF item does: a 4-byte offset
// to the TIFF header, then the "Exif\0\0" marker it skips over.
func exifPayload(tiff []byte) []byte {
	return append(append(be32(6), []byte("Exif\x00\x00")...), tiff...)
}

func TestHeicExifReader(t *testing.T) {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08rest-of-tiff")
	data := buildHEIF(exifPayload(tiff))

	r, err := heicExifReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("heicExifReader() error: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read exif: %v", err)
	}
	if !bytes.Equal(got, tiff) {
		t.Errorf("exif block = %q; want %q", got, tiff)
	}
}

func TestHeicExifReaderNoExifItem(t *testing.T) {
	// A container whose only item is the image itself: no EXIF to find.
	infeImage := box("infe", []byte{2, 0, 0, 0}, be16(1), be16(0), []byte("hvc1"), []byte{0})
	iinf := box("iinf", []byte{0, 0, 0, 0}, be16(1), infeImage)
	iloc := box("iloc", []byte{0, 0, 0, 0}, []byte{0x44, 0x00}, be16(1),
		bytes.Join([][]byte{be16(1), be16(0), be16(1), be32(0), be32(0)}, nil))
	data := append(box("ftyp", []byte("heic"), be32(0), []byte("mif1heic")),
		box("meta", []byte{0, 0, 0, 0}, iinf, iloc)...)

	if _, err := heicExifReader(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("heicExifReader() expected an error for a container without EXIF")
	}
}

// Garbage must be rejected rather than panic or read out of bounds.
func TestHeicExifReaderMalformed(t *testing.T) {
	cases := map[string][]byte{
		"empty":            {},
		"truncated header": []byte("\x00\x00\x00"),
		"no meta box":      box("ftyp", []byte("heic")),
		"truncated meta":   append(box("ftyp", []byte("heic")), []byte("\x00\x00\x10\x00meta")...),
		"random bytes":     bytes.Repeat([]byte{0xff}, 64),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := heicExifReader(bytes.NewReader(data), int64(len(data))); err == nil {
				t.Error("heicExifReader() expected an error, got nil")
			}
		})
	}
}
