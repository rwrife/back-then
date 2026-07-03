package exif

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildEXIFJPEG constructs a minimal but structurally valid JPEG whose APP1
// segment carries a TIFF/EXIF block. It writes DateTimeOriginal (0x9003) in an
// Exif sub-IFD when dtOriginal != "", and/or DateTime (0x0132) in IFD0 when
// dt != "". order selects the TIFF byte order. This lets tests exercise the
// parser without committing a binary fixture.
func buildEXIFJPEG(t *testing.T, order binary.ByteOrder, dt, dtOriginal string) []byte {
	t.Helper()

	// --- Build the TIFF payload (what lives after "Exif\0\0"). ---
	// Layout we emit:
	//   [0:8]   TIFF header (byte order, 0x002A, IFD0 offset=8)
	//   IFD0    entries: optional DateTime, optional ExifIFDPointer
	//   ExifIFD entries: optional DateTimeOriginal
	//   string data area (for values > 4 bytes)
	//
	// Offsets are relative to the start of the TIFF payload.

	var tiff []byte
	putU16 := func(v uint16) []byte { b := make([]byte, 2); order.PutUint16(b, v); return b }
	putU32 := func(v uint32) []byte { b := make([]byte, 4); order.PutUint32(b, v); return b }

	// Header.
	if order == binary.LittleEndian {
		tiff = append(tiff, 'I', 'I')
	} else {
		tiff = append(tiff, 'M', 'M')
	}
	tiff = append(tiff, putU16(0x002A)...)
	tiff = append(tiff, putU32(8)...) // IFD0 at offset 8

	// We assemble entries then fix up string/sub-IFD offsets. To keep the math
	// simple we lay out sections in a fixed order and compute their offsets.

	type entry struct {
		tag, typ uint16
		count    uint32
		val      []byte // 4 bytes (inline value or offset placeholder marker)
		strData  []byte // if non-nil, val is an offset to this appended data
		subIFD   bool   // if true, val is an offset to the exif sub-IFD
	}

	asciiEntry := func(tag uint16, s string) entry {
		data := append([]byte(s), 0) // NUL-terminated
		e := entry{tag: tag, typ: 2, count: uint32(len(data))}
		if len(data) <= 4 {
			v := make([]byte, 4)
			copy(v, data)
			e.val = v
		} else {
			e.strData = data // offset filled in later
		}
		return e
	}

	var ifd0 []entry
	if dt != "" {
		ifd0 = append(ifd0, asciiEntry(tagDateTime, dt))
	}
	var subEntries []entry
	if dtOriginal != "" {
		subEntries = append(subEntries, asciiEntry(tagDateTimeOriginal, dtOriginal))
	}
	haveSub := len(subEntries) > 0
	if haveSub {
		ifd0 = append(ifd0, entry{tag: tagExifIFDPointer, typ: 4, count: 1, subIFD: true})
	}

	// Compute section offsets.
	// IFD0: 2 (count) + 12*N + 4 (next-IFD offset, 0).
	ifd0Off := 8
	ifd0Size := 2 + 12*len(ifd0) + 4
	subOff := ifd0Off + ifd0Size
	subSize := 0
	if haveSub {
		subSize = 2 + 12*len(subEntries) + 4
	}
	strOff := subOff + subSize

	// Assign string-data offsets sequentially in the string area.
	cursor := strOff
	assignStr := func(es []entry) {
		for i := range es {
			if es[i].strData != nil {
				es[i].val = putU32(uint32(cursor))
				cursor += len(es[i].strData)
			}
		}
	}
	assignStr(ifd0)
	assignStr(subEntries)

	// Set the sub-IFD pointer value now that subOff is known.
	for i := range ifd0 {
		if ifd0[i].subIFD {
			ifd0[i].val = putU32(uint32(subOff))
		}
	}

	writeIFD := func(dst []byte, es []entry) []byte {
		dst = append(dst, putU16(uint16(len(es)))...)
		for _, e := range es {
			dst = append(dst, putU16(e.tag)...)
			dst = append(dst, putU16(e.typ)...)
			dst = append(dst, putU32(e.count)...)
			v := e.val
			if v == nil {
				v = []byte{0, 0, 0, 0}
			}
			dst = append(dst, v...)
		}
		dst = append(dst, putU32(0)...) // next IFD = none
		return dst
	}

	// Pad tiff up to ifd0Off (already at 8, header is exactly 8 bytes).
	tiff = writeIFD(tiff, ifd0)
	if haveSub {
		tiff = writeIFD(tiff, subEntries)
	}
	// Append string data in the same order offsets were assigned.
	appendStr := func(es []entry) {
		for _, e := range es {
			if e.strData != nil {
				tiff = append(tiff, e.strData...)
			}
		}
	}
	appendStr(ifd0)
	appendStr(subEntries)

	// --- Wrap the TIFF payload in a JPEG APP1 segment. ---
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(payload) + 2 // +2 for the length field itself
	if segLen > 0xFFFF {
		t.Fatalf("EXIF segment too large for test: %d", segLen)
	}

	var jpeg []byte
	jpeg = append(jpeg, 0xFF, 0xD8) // SOI
	jpeg = append(jpeg, 0xFF, 0xE1) // APP1
	jpeg = append(jpeg, byte(segLen>>8), byte(segLen&0xFF))
	jpeg = append(jpeg, payload...)
	// A tiny bit of "image" then EOI so it looks like a real file tail.
	jpeg = append(jpeg, 0xFF, 0xD9) // EOI
	return jpeg
}

func TestCaptureTimeDateTimeOriginal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		order binary.ByteOrder
	}{
		{"little-endian", binary.LittleEndian},
		{"big-endian", binary.BigEndian},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := buildEXIFJPEG(t, tc.order, "", "2023:07:14 16:45:09")
			got, ok := decodeExifDate(mustExif(t, b))
			if !ok {
				t.Fatal("expected a capture date, got none")
			}
			want := time.Date(2023, 7, 14, 16, 45, 9, 0, time.Local)
			if !got.Equal(want) {
				t.Errorf("capture = %v, want %v", got, want)
			}
		})
	}
}

// mustExif extracts the TIFF payload from a JPEG the test just built, asserting
// the JPEG-segment locator works too.
func mustExif(t *testing.T, jpeg []byte) []byte {
	t.Helper()
	tiff := parseJPEGExif(jpeg)
	if tiff == nil {
		t.Fatal("parseJPEGExif returned nil for a JPEG that has EXIF")
	}
	return tiff
}

func TestCaptureTimeFallsBackToIFD0DateTime(t *testing.T) {
	// Only IFD0 DateTime present (no Exif sub-IFD / DateTimeOriginal).
	b := buildEXIFJPEG(t, binary.LittleEndian, "2020:01:02 03:04:05", "")
	got, ok := decodeExifDate(mustExif(t, b))
	if !ok {
		t.Fatal("expected IFD0 DateTime fallback, got none")
	}
	want := time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("fallback capture = %v, want %v", got, want)
	}
}

func TestCaptureTimePrefersOriginalOverDateTime(t *testing.T) {
	// Both present: DateTimeOriginal must win.
	b := buildEXIFJPEG(t, binary.LittleEndian, "2020:01:02 03:04:05", "2019:06:06 06:06:06")
	got, ok := decodeExifDate(mustExif(t, b))
	if !ok {
		t.Fatal("expected a capture date, got none")
	}
	want := time.Date(2019, 6, 6, 6, 6, 6, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("capture = %v, want DateTimeOriginal %v", got, want)
	}
}

func TestCaptureTimeZeroedDateIsAbsent(t *testing.T) {
	b := buildEXIFJPEG(t, binary.LittleEndian, "", "0000:00:00 00:00:00")
	if _, ok := decodeExifDate(mustExif(t, b)); ok {
		t.Error("zeroed EXIF date should be treated as absent")
	}
}

func TestParseJPEGExifNoAPP1(t *testing.T) {
	// A JPEG with an unrelated APP0 (JFIF) marker but no EXIF APP1.
	var b []byte
	b = append(b, 0xFF, 0xD8) // SOI
	// APP0 JFIF segment (length 16).
	b = append(b, 0xFF, 0xE0, 0x00, 0x10)
	b = append(b, []byte("JFIF\x00")...)
	b = append(b, make([]byte, 11)...) // pad to declared length
	b = append(b, 0xFF, 0xD9)          // EOI
	if tiff := parseJPEGExif(b); tiff != nil {
		t.Errorf("expected nil EXIF for a JFIF-only JPEG, got %d bytes", len(tiff))
	}
}

func TestCaptureTimeUnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	// Even if the bytes were a valid EXIF JPEG, a non-image extension must be
	// skipped without opening/parsing.
	p := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(p, buildEXIFJPEG(t, binary.LittleEndian, "", "2023:07:14 16:45:09"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := CaptureTime(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || !got.IsZero() {
		t.Errorf("unsupported extension should yield (zero,false); got (%v,%v)", got, ok)
	}
}

func TestCaptureTimeEndToEnd(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(p, buildEXIFJPEG(t, binary.BigEndian, "", "2024:11:23 08:15:00"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := CaptureTime(p)
	if err != nil {
		t.Fatalf("CaptureTime error: %v", err)
	}
	if !ok {
		t.Fatal("expected a capture date from the on-disk JPEG")
	}
	want := time.Date(2024, 11, 23, 8, 15, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("capture = %v, want %v", got, want)
	}
}

func TestCaptureTimeAbsentEXIFOnDisk(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.jpg")
	// JPEG bytes with no EXIF at all.
	if err := os.WriteFile(p, []byte{0xFF, 0xD8, 0xFF, 0xD9}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := CaptureTime(p)
	if err != nil {
		t.Fatalf("CaptureTime error: %v", err)
	}
	if ok || !got.IsZero() {
		t.Errorf("no-EXIF JPEG should yield (zero,false); got (%v,%v)", got, ok)
	}
}

func TestHasEXIFExt(t *testing.T) {
	yes := []string{"a.jpg", "A.JPG", "b.jpeg", "c.JPE"}
	no := []string{"a.png", "b.txt", "c.mov", "noext"}
	for _, p := range yes {
		if !HasEXIFExt(p) {
			t.Errorf("HasEXIFExt(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if HasEXIFExt(p) {
			t.Errorf("HasEXIFExt(%q) = true, want false", p)
		}
	}
}
