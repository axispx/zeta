package image

import (
	"errors"
	"fmt"
)

// ErrNoImage means the clipboard has no image (soft miss — callers may paste text).
var ErrNoImage = errors.New("clipboard has no image")

// ReadClipboardImage reads an OS clipboard image (if any) and returns a data: Ref.
// Platform bridges may use OS temp briefly; nothing is kept under ZETA_HOME.
// Returns ErrNoImage when no image is available; other errors are hard failures.
func ReadClipboardImage() (Ref, error) {
	data, err := readClipboardImageBytes()
	if err != nil {
		return Ref{}, err
	}
	if len(data) == 0 {
		return Ref{}, ErrNoImage
	}
	if int64(len(data)) > MaxImageBytes {
		return Ref{}, fmt.Errorf("clipboard image: exceeds %d MB limit", MaxImageBytes>>20)
	}
	mime := MIMEPNG
	if m, ok := mimeFromMagic(data); ok {
		mime = m
	}
	ext := mimeExt[mime]
	if ext == "" {
		ext = ".png"
	}
	return RefFromBytes(data, mime, "clipboard"+ext)
}
