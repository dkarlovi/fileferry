package file

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
)

// A JPEG encoder records the quantization tables it used in the file's DQT
// segments, and those tables are what actually decides how much detail
// survives: the coarser the steps, the more high-frequency information is
// discarded. Two encodes of one shot at identical dimensions are therefore
// ranked by their tables — the difference is invisible in the decoded pixel
// geometry, which is all image.DecodeConfig can see.

// maxJPEGHeader bounds how far into a stream the DQT segments are looked for.
// Everything that matters sits in the header ahead of the first scan; the bound
// keeps a malformed file from being read to its end.
const maxJPEGHeader = 4 << 20

var errNoQuantTables = errors.New("no jpeg quantization tables")

// jpegQuantization returns the mean quantization step across every DQT table in
// a JPEG header. Lower means finer quantization — a higher-quality encode.
//
// The mean rather than the sum keeps two encodes comparable when they carry a
// different number of tables (a greyscale JPEG has one, a colour JPEG usually
// two). Table precision does not affect the scale: the 16-bit form only permits
// values above 255, it does not rescale them, so means are comparable across
// both forms.
func jpegQuantization(r io.Reader) (float64, error) {
	br := bufio.NewReader(io.LimitReader(r, maxJPEGHeader))

	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil {
		return 0, err
	}
	if soi[0] != 0xff || soi[1] != 0xd8 {
		return 0, errNoQuantTables
	}

	var total float64
	var count int
	for {
		marker, err := nextJPEGMarker(br)
		if err != nil {
			break
		}
		switch {
		case marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7):
			// Standalone markers carry no payload.
			continue
		case marker == 0xda || marker == 0xd9:
			// The first scan begins the entropy-coded data; the tables are all
			// declared ahead of it, so there is nothing further to walk.
			return finishQuantization(total, count)
		}

		var lenBuf [2]byte
		if _, err := io.ReadFull(br, lenBuf[:]); err != nil {
			break
		}
		segLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		if segLen < 2 {
			break
		}
		payload := make([]byte, segLen-2)
		if _, err := io.ReadFull(br, payload); err != nil {
			break
		}
		if marker == 0xdb {
			sum, n := sumQuantTables(payload)
			total += sum
			count += n
		}
	}
	return finishQuantization(total, count)
}

func finishQuantization(total float64, count int) (float64, error) {
	if count == 0 {
		return 0, errNoQuantTables
	}
	return total / float64(count), nil
}

// nextJPEGMarker advances to the next marker byte. A marker is 0xff followed by
// a byte that is neither 0x00 (a stuffed literal 0xff in entropy data) nor
// another 0xff (segments may be padded with fill bytes).
func nextJPEGMarker(br *bufio.Reader) (byte, error) {
	for {
		b, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != 0xff {
			continue
		}
		for b == 0xff {
			if b, err = br.ReadByte(); err != nil {
				return 0, err
			}
		}
		if b != 0x00 {
			return b, nil
		}
	}
}

// sumQuantTables totals the entries of every table packed into one DQT payload,
// returning the sum and how many entries it covers. Each table is 64 entries
// behind a header byte whose high nibble selects 1- or 2-byte precision.
func sumQuantTables(payload []byte) (sum float64, n int) {
	const entries = 64
	for off := 0; off < len(payload); {
		width := 1
		if payload[off]>>4 == 1 {
			width = 2
		}
		off++
		if off+entries*width > len(payload) {
			break
		}
		for i := 0; i < entries; i++ {
			v := uint16(payload[off+i])
			if width == 2 {
				v = binary.BigEndian.Uint16(payload[off+i*2:])
			}
			sum += float64(v)
			n++
		}
		off += entries * width
	}
	return sum, n
}
