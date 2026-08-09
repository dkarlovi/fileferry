package file

import (
	"encoding/binary"
	"errors"
	"io"
)

// HEIC/HEIF files are ISO-BMFF containers, so their EXIF is not at the head of
// the file the way it is in JPEG or TIFF — goexif cannot find it by streaming.
// It lives in a separate "Exif" item whose position is described by the file's
// metadata boxes: `iinf` names the items, `iloc` says where each one's bytes
// are. This locates that item and hands back a reader over the TIFF block
// inside it, which is exactly what goexif expects.
//
// Only the boxes needed to find that one item are parsed; image data, tiles and
// the rest of the container are ignored.

const (
	// A single metadata box far larger than this is not something we want to
	// buffer or trust; real HEIC meta boxes are a few KB.
	maxMetaBoxSize = 8 << 20
	// Sanity bound on the EXIF payload itself.
	maxExifItemSize = 4 << 20
)

var errNoExifItem = errors.New("no exif item in heif container")

// heicExifReader returns a reader over the TIFF/EXIF block of a HEIC/HEIF file,
// suitable for exif.Decode. It returns an error when the container holds no
// EXIF item or is malformed.
func heicExifReader(ra io.ReaderAt, size int64) (io.Reader, error) {
	meta, err := findBox(ra, 0, size, "meta")
	if err != nil {
		return nil, err
	}
	if meta.size > maxMetaBoxSize {
		return nil, errors.New("heif meta box too large")
	}
	// `meta` is a FullBox: its 4-byte version/flags precede the child boxes.
	childStart, childEnd := meta.payloadStart+4, meta.payloadEnd()
	if childStart > childEnd {
		return nil, errors.New("truncated heif meta box")
	}

	itemID, err := findExifItemID(ra, childStart, childEnd-childStart)
	if err != nil {
		return nil, err
	}
	offset, length, err := findItemLocation(ra, childStart, childEnd-childStart, itemID)
	if err != nil {
		return nil, err
	}
	if length < 4 || length > maxExifItemSize || offset+length > size {
		return nil, errors.New("heif exif item out of range")
	}

	// The item payload starts with a 4-byte offset to the TIFF header, skipping
	// any leading "Exif\0\0" marker.
	var hdr [4]byte
	if _, err := ra.ReadAt(hdr[:], offset); err != nil {
		return nil, err
	}
	skip := 4 + int64(binary.BigEndian.Uint32(hdr[:]))
	if skip >= length {
		return nil, errors.New("heif exif item has no tiff data")
	}
	return io.NewSectionReader(ra, offset+skip, length-skip), nil
}

// boxHeader describes one ISO-BMFF box within the file.
type boxHeader struct {
	boxType      string
	start        int64 // offset of the box header
	size         int64 // total box size, header included
	payloadStart int64
}

func (b boxHeader) payloadEnd() int64 { return b.start + b.size }

// readBoxHeader reads the box starting at off, bounded by end.
func readBoxHeader(ra io.ReaderAt, off, end int64) (boxHeader, error) {
	var hdr [8]byte
	if off+8 > end {
		return boxHeader{}, io.EOF
	}
	if _, err := ra.ReadAt(hdr[:], off); err != nil {
		return boxHeader{}, err
	}
	size := int64(binary.BigEndian.Uint32(hdr[0:4]))
	b := boxHeader{boxType: string(hdr[4:8]), start: off, payloadStart: off + 8}
	switch size {
	case 0:
		// Extends to the end of the enclosing container.
		size = end - off
	case 1:
		// 64-bit size in the 8 bytes following the header.
		var large [8]byte
		if off+16 > end {
			return boxHeader{}, io.ErrUnexpectedEOF
		}
		if _, err := ra.ReadAt(large[:], off+8); err != nil {
			return boxHeader{}, err
		}
		u := binary.BigEndian.Uint64(large[:])
		if u > uint64(end-off) {
			return boxHeader{}, errors.New("box size beyond container")
		}
		size = int64(u)
		b.payloadStart = off + 16
	}
	if size < b.payloadStart-off || off+size > end {
		return boxHeader{}, errors.New("invalid box size")
	}
	b.size = size
	return b, nil
}

// findBox scans the boxes in [start, start+length) for the first one of the
// given type.
func findBox(ra io.ReaderAt, start, length int64, boxType string) (boxHeader, error) {
	end := start + length
	for off := start; off < end; {
		b, err := readBoxHeader(ra, off, end)
		if err != nil {
			return boxHeader{}, err
		}
		if b.boxType == boxType {
			return b, nil
		}
		off += b.size
	}
	return boxHeader{}, errors.New("box " + boxType + " not found")
}

// findExifItemID walks the item info box (`iinf` → `infe` entries) and returns
// the ID of the item whose type is "Exif".
func findExifItemID(ra io.ReaderAt, start, length int64) (uint32, error) {
	iinf, err := findBox(ra, start, length, "iinf")
	if err != nil {
		return 0, err
	}
	r := &boxReader{ra: ra, off: iinf.payloadStart, end: iinf.payloadEnd()}
	version, err := r.fullBoxVersion()
	if err != nil {
		return 0, err
	}
	// entry_count widens from 16 to 32 bits in version 1.
	if version == 0 {
		if _, err := r.uint(2); err != nil {
			return 0, err
		}
	} else if _, err := r.uint(4); err != nil {
		return 0, err
	}

	for off := r.off; off < r.end; {
		infe, err := readBoxHeader(ra, off, r.end)
		if err != nil {
			return 0, err
		}
		off += infe.size
		if infe.boxType != "infe" {
			continue
		}
		er := &boxReader{ra: ra, off: infe.payloadStart, end: infe.payloadEnd()}
		v, err := er.fullBoxVersion()
		if err != nil {
			return 0, err
		}
		// Only versions 2 and 3 carry a four-character item_type; the older
		// name-based entries cannot identify an EXIF item.
		idWidth := 2
		switch v {
		case 2:
		case 3:
			idWidth = 4
		default:
			continue
		}
		id, err := er.uint(idWidth)
		if err != nil {
			return 0, err
		}
		if _, err := er.uint(2); err != nil { // item_protection_index
			return 0, err
		}
		itemType, err := er.fourCC()
		if err != nil {
			return 0, err
		}
		if itemType == "Exif" {
			return uint32(id), nil
		}
	}
	return 0, errNoExifItem
}

// findItemLocation reads the item location box (`iloc`) and returns the file
// offset and length of the given item's first extent.
func findItemLocation(ra io.ReaderAt, start, length int64, itemID uint32) (offset, size int64, err error) {
	iloc, err := findBox(ra, start, length, "iloc")
	if err != nil {
		return 0, 0, err
	}
	r := &boxReader{ra: ra, off: iloc.payloadStart, end: iloc.payloadEnd()}
	version, err := r.fullBoxVersion()
	if err != nil {
		return 0, 0, err
	}

	// Two packed bytes give the width of every field that follows.
	sizes, err := r.uint(2)
	if err != nil {
		return 0, 0, err
	}
	offsetSize := int((sizes >> 12) & 0xf)
	lengthSize := int((sizes >> 8) & 0xf)
	baseOffsetSize := int((sizes >> 4) & 0xf)
	indexSize := int(sizes & 0xf) // reserved before version 1

	idWidth := 2
	countWidth := 2
	if version >= 2 {
		idWidth = 4
		countWidth = 4
	}
	itemCount, err := r.uint(countWidth)
	if err != nil {
		return 0, 0, err
	}

	for i := uint64(0); i < itemCount; i++ {
		id, err := r.uint(idWidth)
		if err != nil {
			return 0, 0, err
		}
		if version >= 1 {
			if _, err := r.uint(2); err != nil { // construction_method
				return 0, 0, err
			}
		}
		if _, err := r.uint(2); err != nil { // data_reference_index
			return 0, 0, err
		}
		baseOffset, err := r.uint(baseOffsetSize)
		if err != nil {
			return 0, 0, err
		}
		extentCount, err := r.uint(2)
		if err != nil {
			return 0, 0, err
		}
		for e := uint64(0); e < extentCount; e++ {
			if version >= 1 && indexSize > 0 {
				if _, err := r.uint(indexSize); err != nil {
					return 0, 0, err
				}
			}
			extOffset, err := r.uint(offsetSize)
			if err != nil {
				return 0, 0, err
			}
			extLength, err := r.uint(lengthSize)
			if err != nil {
				return 0, 0, err
			}
			// The first extent is enough: EXIF is never split in practice.
			if uint32(id) == itemID && e == 0 {
				return int64(baseOffset + extOffset), int64(extLength), nil
			}
		}
	}
	return 0, 0, errNoExifItem
}

// boxReader reads big-endian fields sequentially from a bounded region.
type boxReader struct {
	ra  io.ReaderAt
	off int64
	end int64
}

// uint reads an n-byte big-endian integer (n may be 0, yielding 0, as ISO-BMFF
// uses zero-width fields to mean "absent").
func (r *boxReader) uint(n int) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if n < 0 || n > 8 || r.off+int64(n) > r.end {
		return 0, io.ErrUnexpectedEOF
	}
	buf := make([]byte, n)
	if _, err := r.ra.ReadAt(buf, r.off); err != nil {
		return 0, err
	}
	r.off += int64(n)
	var v uint64
	for _, b := range buf {
		v = v<<8 | uint64(b)
	}
	return v, nil
}

// fullBoxVersion consumes a FullBox's version byte and 3 flag bytes.
func (r *boxReader) fullBoxVersion() (uint8, error) {
	v, err := r.uint(1)
	if err != nil {
		return 0, err
	}
	if _, err := r.uint(3); err != nil {
		return 0, err
	}
	return uint8(v), nil
}

func (r *boxReader) fourCC() (string, error) {
	if r.off+4 > r.end {
		return "", io.ErrUnexpectedEOF
	}
	var buf [4]byte
	if _, err := r.ra.ReadAt(buf[:], r.off); err != nil {
		return "", err
	}
	r.off += 4
	return string(buf[:]), nil
}
