// Package exif is a best-effort, dependency-free reader for image capture
// dates (EXIF DateTimeOriginal). It degrades gracefully to "unknown" when no
// EXIF data is present, so callers fall back to the file's mtime.
//
// Implemented in M5 (EXIF + smarter ranking). The parser is intentionally
// minimal: it walks a JPEG's APP1/Exif segment and the embedded TIFF IFD
// structure just far enough to read DateTimeOriginal (tag 0x9003), falling
// back to the base DateTime (tag 0x0132). It handles both little- and
// big-endian TIFF byte orders. Anything malformed, truncated, or unsupported
// yields (zero, false, nil) rather than an error — a missing capture date is
// a normal, expected outcome, not a failure.
//
// Keeping this in pure Go with no third-party dependency preserves back-then's
// cgo-free, single-binary build. Only JPEG is parsed today (the dominant photo
// format that carries EXIF); other container formats can be added later behind
// the same CaptureTime contract.
package exif

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxScan caps how many bytes we will read while hunting for and parsing the
// EXIF segment. EXIF lives in the JPEG APP1 marker near the start of the file;
// a few hundred KiB is far more than enough and bounds work on huge or
// adversarial inputs.
const maxScan = 512 * 1024

// EXIF tag numbers we care about.
const (
	tagDateTime          = 0x0132 // ModifyDate / DateTime (IFD0)
	tagExifIFDPointer    = 0x8769 // pointer from IFD0 to the Exif sub-IFD
	tagDateTimeOriginal  = 0x9003 // DateTimeOriginal (Exif sub-IFD) — when taken
	tagDateTimeDigitized = 0x9004 // DateTimeDigitized (Exif sub-IFD)
)

// imageExts is the set of extensions CaptureTime will attempt to parse. Kept
// small and JPEG-only for now; HasEXIFExt lets callers cheaply gate work.
var imageExts = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".jpe":  {},
}

// HasEXIFExt reports whether path has an extension this package knows how to
// read EXIF from. Callers (e.g. the walker) can use it to skip opening files
// that will never yield a capture date.
func HasEXIFExt(path string) bool {
	_, ok := imageExts[strings.ToLower(filepath.Ext(path))]
	return ok
}

// CaptureTime returns the image's EXIF capture date (DateTimeOriginal, then
// DateTimeDigitized, then the base DateTime) for a supported image file.
//
// The boolean is false (with a nil error) whenever no usable capture date is
// available: an unsupported extension, a file with no EXIF, an unparseable or
// truncated segment, or an EXIF date that is empty/zeroed. A non-nil error is
// returned only for a genuine I/O failure opening/reading the file.
//
// EXIF timestamps carry no timezone; they are interpreted as local wall-clock
// time (time.Local), matching how cameras record them and how filesystem
// mtimes are handled elsewhere.
func CaptureTime(path string) (time.Time, bool, error) {
	if !HasEXIFExt(path) {
		return time.Time{}, false, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false, err
	}
	defer f.Close()

	// Read a bounded prefix; EXIF is always near the front of a JPEG.
	buf, err := io.ReadAll(io.LimitReader(f, maxScan))
	if err != nil {
		return time.Time{}, false, err
	}

	raw := parseJPEGExif(buf)
	if raw == nil {
		return time.Time{}, false, nil
	}
	t, ok := decodeExifDate(raw)
	if !ok {
		return time.Time{}, false, nil
	}
	return t, true, nil
}

// parseJPEGExif locates the APP1 "Exif" segment in a JPEG byte slice and
// returns the TIFF-structured EXIF payload (starting at the byte-order marker),
// or nil if not found / not a JPEG.
func parseJPEGExif(b []byte) []byte {
	// JPEG starts with SOI: 0xFF 0xD8.
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil
	}
	i := 2
	for i+4 <= len(b) {
		// Each marker begins with 0xFF; skip any fill bytes.
		if b[i] != 0xFF {
			i++
			continue
		}
		marker := b[i+1]
		// Standalone markers without a length payload.
		if marker == 0xD8 || marker == 0xD9 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		// SOS (0xDA): start of compressed scan data — no EXIF beyond here.
		if marker == 0xDA {
			return nil
		}
		if i+4 > len(b) {
			return nil
		}
		segLen := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if segLen < 2 {
			return nil
		}
		segStart := i + 4
		segEnd := i + 2 + segLen
		if segEnd > len(b) {
			// Truncated segment (we only read a prefix); give up gracefully.
			return nil
		}
		// APP1 (0xE1) carrying EXIF: payload begins with "Exif\0\0".
		if marker == 0xE1 {
			payload := b[segStart:segEnd]
			const sig = "Exif\x00\x00"
			if len(payload) >= len(sig) && bytes.Equal(payload[:len(sig)], []byte(sig)) {
				return payload[len(sig):]
			}
		}
		i = segEnd
	}
	return nil
}

// decodeExifDate parses a TIFF-structured EXIF payload and returns the best
// available capture date: DateTimeOriginal, then DateTimeDigitized (both in
// the Exif sub-IFD), then DateTime from IFD0.
func decodeExifDate(tiff []byte) (time.Time, bool) {
	if len(tiff) < 8 {
		return time.Time{}, false
	}
	var order binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		order = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		order = binary.BigEndian
	default:
		return time.Time{}, false
	}
	// Bytes 2-3 are the fixed 0x002A magic; byte 4-7 is the IFD0 offset.
	ifd0Off := int(order.Uint32(tiff[4:8]))

	ifd0, err := readIFD(tiff, ifd0Off, order)
	if err != nil {
		return time.Time{}, false
	}

	// Prefer the Exif sub-IFD dates (DateTimeOriginal is "when taken").
	if p, ok := ifd0.longPointer(tagExifIFDPointer, order); ok {
		if sub, err := readIFD(tiff, p, order); err == nil {
			for _, tag := range []uint16{tagDateTimeOriginal, tagDateTimeDigitized} {
				if s, ok := sub.ascii(tag, tiff); ok {
					if t, ok := parseExifTimestamp(s); ok {
						return t, true
					}
				}
			}
		}
	}
	// Fall back to IFD0 DateTime.
	if s, ok := ifd0.ascii(tagDateTime, tiff); ok {
		if t, ok := parseExifTimestamp(s); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// ifdEntry is a single parsed IFD entry (tag, type, count, and the raw 4-byte
// value/offset field), enough to resolve ASCII strings and long pointers.
type ifdEntry struct {
	tag   uint16
	typ   uint16
	count uint32
	value []byte // exactly 4 bytes: inline value or an offset
}

// ifd is a parsed image file directory: its entries plus the backing tiff
// slice bounds are supplied per-lookup.
type ifd struct {
	entries map[uint16]ifdEntry
}

// errBadIFD signals a structurally invalid or out-of-bounds IFD.
var errBadIFD = errors.New("exif: bad ifd")

// readIFD parses the IFD at offset off within the tiff payload.
func readIFD(tiff []byte, off int, order binary.ByteOrder) (ifd, error) {
	if off < 0 || off+2 > len(tiff) {
		return ifd{}, errBadIFD
	}
	n := int(order.Uint16(tiff[off : off+2]))
	base := off + 2
	// Each entry is 12 bytes; guard against overruns.
	if n < 0 || base+n*12 > len(tiff) {
		return ifd{}, errBadIFD
	}
	m := make(map[uint16]ifdEntry, n)
	for k := 0; k < n; k++ {
		e := base + k*12
		entry := ifdEntry{
			tag:   order.Uint16(tiff[e : e+2]),
			typ:   order.Uint16(tiff[e+2 : e+4]),
			count: order.Uint32(tiff[e+4 : e+8]),
			value: tiff[e+8 : e+12],
		}
		m[entry.tag] = entry
	}
	return ifd{entries: m}, nil
}

// longPointer returns the uint32 value of a LONG-typed entry (e.g. a sub-IFD
// offset).
func (d ifd) longPointer(tag uint16, order binary.ByteOrder) (int, bool) {
	e, ok := d.entries[tag]
	if !ok || len(e.value) != 4 {
		return 0, false
	}
	return int(order.Uint32(e.value)), true
}

// ascii returns the NUL-trimmed ASCII string for an ASCII-typed entry (type 2).
// Short strings (<=4 bytes incl. NUL) are stored inline in the value field;
// longer ones are referenced by an offset into the tiff payload.
func (d ifd) ascii(tag uint16, tiff []byte) (string, bool) {
	e, ok := d.entries[tag]
	if !ok || e.typ != 2 || e.count == 0 {
		return "", false
	}
	n := int(e.count)
	var raw []byte
	if n <= 4 {
		raw = e.value[:n]
	} else {
		// The 4-byte value field holds an offset to the string data. Byte
		// order for that offset matches the TIFF order; recover it from the
		// value bytes using the same order the caller parsed with.
		off := int(byteOrderOf(e.value, tiff))
		if off < 0 || off+n > len(tiff) {
			return "", false
		}
		raw = tiff[off : off+n]
	}
	return strings.TrimRight(string(raw), "\x00 "), true
}

// byteOrderOf reconstructs the uint32 offset stored in a value field. The TIFF
// byte order is embedded at the start of tiff; reuse it so inline offsets are
// decoded consistently regardless of host endianness.
func byteOrderOf(value, tiff []byte) uint32 {
	if len(tiff) >= 2 && tiff[0] == 'I' && tiff[1] == 'I' {
		return binary.LittleEndian.Uint32(value)
	}
	return binary.BigEndian.Uint32(value)
}

// parseExifTimestamp parses the canonical EXIF datetime "YYYY:MM:DD HH:MM:SS"
// (and tolerates a few common variants) into local time. A zeroed date such as
// "0000:00:00 00:00:00" is treated as "no date" (ok=false).
func parseExifTimestamp(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "0000") {
		return time.Time{}, false
	}
	layouts := []string{
		"2006:01:02 15:04:05",
		"2006:01:02 15:04",
		"2006-01-02 15:04:05",
		"2006:01:02",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			if t.IsZero() {
				return time.Time{}, false
			}
			return t, true
		}
	}
	return time.Time{}, false
}
