package walk

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// minimalEXIFJPEG returns a tiny little-endian JPEG carrying a single EXIF
// DateTimeOriginal tag in an Exif sub-IFD. It mirrors the parser's expected
// structure and is just enough to prove the walker populates CaptureTime for
// images. (The exhaustive parser tests live in internal/exif.)
func minimalEXIFJPEG(dtOriginal string) []byte {
	le := binary.LittleEndian
	u16 := func(v uint16) []byte { b := make([]byte, 2); le.PutUint16(b, v); return b }
	u32 := func(v uint32) []byte { b := make([]byte, 4); le.PutUint32(b, v); return b }

	str := append([]byte(dtOriginal), 0) // NUL-terminated ASCII

	// TIFF payload.
	var tiff []byte
	tiff = append(tiff, 'I', 'I')       // little-endian
	tiff = append(tiff, u16(0x002A)...) // magic
	tiff = append(tiff, u32(8)...)      // IFD0 at offset 8

	// IFD0: one entry (ExifIFDPointer 0x8769 -> sub-IFD).
	ifd0Off := 8
	ifd0Size := 2 + 12*1 + 4
	subOff := ifd0Off + ifd0Size
	subSize := 2 + 12*1 + 4
	strOff := subOff + subSize

	// IFD0.
	tiff = append(tiff, u16(1)...)      // entry count
	tiff = append(tiff, u16(0x8769)...) // ExifIFDPointer
	tiff = append(tiff, u16(4)...)      // type LONG
	tiff = append(tiff, u32(1)...)      // count
	tiff = append(tiff, u32(uint32(subOff))...)
	tiff = append(tiff, u32(0)...) // next IFD = none

	// Exif sub-IFD: one entry (DateTimeOriginal 0x9003, ASCII).
	tiff = append(tiff, u16(1)...)
	tiff = append(tiff, u16(0x9003)...)
	tiff = append(tiff, u16(2)...) // ASCII
	tiff = append(tiff, u32(uint32(len(str)))...)
	tiff = append(tiff, u32(uint32(strOff))...)
	tiff = append(tiff, u32(0)...) // next IFD = none

	// String data.
	tiff = append(tiff, str...)

	// Wrap in JPEG APP1.
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(payload) + 2
	var jpeg []byte
	jpeg = append(jpeg, 0xFF, 0xD8) // SOI
	jpeg = append(jpeg, 0xFF, 0xE1) // APP1
	jpeg = append(jpeg, byte(segLen>>8), byte(segLen&0xFF))
	jpeg = append(jpeg, payload...)
	jpeg = append(jpeg, 0xFF, 0xD9) // EOI
	return jpeg
}

// TestWalkPopulatesCaptureTime verifies the walker reads EXIF for image files
// (setting CaptureTime) while leaving it zero for non-images and EXIF-less
// images. This is the M5 "prefer capture date for images" wiring.
func TestWalkPopulatesCaptureTime(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, data []byte) {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("shot.jpg", minimalEXIFJPEG("2022:05:04 13:37:00"))
	write("plain.jpg", []byte{0xFF, 0xD8, 0xFF, 0xD9})       // JPEG, no EXIF
	write("doc.txt", minimalEXIFJPEG("2022:05:04 13:37:00")) // EXIF bytes but non-image ext

	got := collect(t, root, Options{})

	shot, ok := got["shot.jpg"]
	if !ok {
		t.Fatal("shot.jpg missing")
	}
	wantCapture := time.Date(2022, 5, 4, 13, 37, 0, 0, time.Local)
	if shot.CaptureTime.IsZero() {
		t.Fatal("shot.jpg CaptureTime is zero; EXIF was not read")
	}
	if !shot.CaptureTime.Equal(wantCapture) {
		t.Errorf("shot.jpg CaptureTime = %v, want %v", shot.CaptureTime, wantCapture)
	}

	if plain := got["plain.jpg"]; !plain.CaptureTime.IsZero() {
		t.Errorf("plain.jpg CaptureTime = %v, want zero (no EXIF)", plain.CaptureTime)
	}
	if doc := got["doc.txt"]; !doc.CaptureTime.IsZero() {
		t.Errorf("doc.txt CaptureTime = %v, want zero (non-image ext)", doc.CaptureTime)
	}
}
