package image

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxImageBytes caps a single image read (path attach, clipboard, encode).
const MaxImageBytes = 20 << 20 // 20 MiB

// MaxJSONLLine is the max scanner token size for session JSONL lines that may
// embed data: image URLs. Sized for a few MaxImageBytes images plus framing.
const MaxJSONLLine = 64 << 20 // 64 MiB

// DataURLFromBytes builds a data: URL. mime defaults to image/png.
func DataURLFromBytes(data []byte, mime string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("image: empty data")
	}
	if int64(len(data)) > MaxImageBytes {
		return "", fmt.Errorf("image: exceeds %d MB limit", MaxImageBytes>>20)
	}
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = MIMEPNG
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// RefFromBytes builds a Ref from raw image bytes.
func RefFromBytes(data []byte, mime, name string) (Ref, error) {
	url, err := DataURLFromBytes(data, mime)
	if err != nil {
		return Ref{}, err
	}
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = MIMEPNG
	}
	return Ref{URL: url, MIME: mime, Name: name}, nil
}

// RefFromPath sniffs and reads a local image file into a data: Ref.
func RefFromPath(path string) (Ref, error) {
	mime, ok := Sniff(path)
	if !ok {
		return Ref{}, fmt.Errorf("image %s: not a supported image", path)
	}
	data, err := ReadBytes(path)
	if err != nil {
		return Ref{}, err
	}
	// Prefer magic over extension when available.
	if m, magicOK := mimeFromMagic(data); magicOK {
		mime = m
	}
	return RefFromBytes(data, mime, filepath.Base(path))
}

// ReadBytes reads a local image file, rejecting empty paths and oversized files.
func ReadBytes(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("image: empty path")
	}
	if err := CheckSize(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("image %s: %w", path, err)
	}
	if int64(len(data)) > MaxImageBytes {
		return nil, fmt.Errorf("image %s: exceeds %d MB limit", path, MaxImageBytes>>20)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("image %s: empty file", path)
	}
	return data, nil
}

// CheckSize returns an error if path is missing or larger than MaxImageBytes.
func CheckSize(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("image: empty path")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("image %s: %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("image %s: is a directory", path)
	}
	if fi.Size() > MaxImageBytes {
		return fmt.Errorf("image %s: exceeds %d MB limit", path, MaxImageBytes>>20)
	}
	return nil
}
