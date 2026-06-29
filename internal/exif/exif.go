// Package exif is a best-effort reader for image capture dates
// (EXIF DateTimeOriginal). It degrades gracefully to the file's mtime when no
// EXIF data is present.
//
// Implemented in M5 (EXIF + smarter ranking).
package exif
