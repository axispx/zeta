package image

import (
	"os"
	"path/filepath"
	"strings"
)

// Allowed image MIME types.
const (
	MIMEPNG  = "image/png"
	MIMEJPEG = "image/jpeg"
	MIMEGIF  = "image/gif"
	MIMEWebP = "image/webp"
)

var extMIME = map[string]string{
	".png":  MIMEPNG,
	".jpg":  MIMEJPEG,
	".jpeg": MIMEJPEG,
	".gif":  MIMEGIF,
	".webp": MIMEWebP,
}

var mimeExt = map[string]string{
	MIMEPNG:  ".png",
	MIMEJPEG: ".jpg",
	MIMEGIF:  ".gif",
	MIMEWebP: ".webp",
}

// Sniff reports whether path looks like a supported local image and its MIME type.
// Uses extension allowlist plus magic bytes when the file is readable.
// Rejects files larger than MaxImageBytes so paste/attach fail closed early.
func Sniff(path string) (mime string, ok bool) {
	ext := strings.ToLower(filepath.Ext(path))
	byExt, extOK := extMIME[ext]
	if !extOK {
		return "", false
	}
	if CheckSize(path) != nil {
		return "", false
	}

	f, err := os.Open(path)
	if err != nil {
		// Extension matches but unreadable — fail closed so paste doesn't steal.
		return "", false
	}
	defer f.Close()

	var hdr [16]byte
	n, _ := f.Read(hdr[:])
	if n == 0 {
		return "", false
	}
	if m, magicOK := mimeFromMagic(hdr[:n]); magicOK {
		return m, true
	}
	// Magic unknown but extension allowed and file non-empty.
	return byExt, true
}

func mimeFromMagic(b []byte) (string, bool) {
	if len(b) >= 8 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return MIMEPNG, true
	}
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF {
		return MIMEJPEG, true
	}
	if len(b) >= 6 && b[0] == 'G' && b[1] == 'I' && b[2] == 'F' && b[3] == '8' && (b[4] == '7' || b[4] == '9') && b[5] == 'a' {
		return MIMEGIF, true
	}
	if len(b) >= 12 && b[0] == 'R' && b[1] == 'I' && b[2] == 'F' && b[3] == 'F' &&
		b[8] == 'W' && b[9] == 'E' && b[10] == 'B' && b[11] == 'P' {
		return MIMEWebP, true
	}
	return "", false
}
