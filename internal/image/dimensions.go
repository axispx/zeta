package image

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
)

// Two-stage header reads. Almost every format carries its dimensions in the
// first few bytes, so the cheap read resolves the common case; only a JPEG with
// a large EXIF block (which can embed a thumbnail ahead of the frame header)
// pays for the second. Callers run this on the UI thread whenever they estimate
// the history, so the cost has to stay near zero for ordinary images.
const (
	smallHeaderBytes = 4 << 10
	maxHeaderBytes   = 64 << 10
)

// Dimensions returns the pixel size of a data: URL image. Zeroes mean the
// format or header was not recognized; callers should fall back to a default
// estimate rather than treat it as a zero-cost image.
func Dimensions(url string) (w, h int) {
	raw, truncated := decodePrefix(url, smallHeaderBytes)
	if w, h = dimensionsOf(raw); w > 0 && h > 0 {
		return w, h
	}
	if !truncated {
		return 0, 0 // whole payload decoded; there is nothing more to read
	}
	raw, _ = decodePrefix(url, maxHeaderBytes)
	return dimensionsOf(raw)
}

func dimensionsOf(raw []byte) (w, h int) {
	switch {
	case isPNG(raw):
		return pngSize(raw)
	case isGIF(raw):
		return gifSize(raw)
	case isJPEG(raw):
		return jpegSize(raw)
	case isWebP(raw):
		return webpSize(raw)
	}
	return 0, 0
}

// decodePrefix decodes at most maxBytes of a data: URL's base64 payload and
// reports whether it stopped early. A truncated tail is expected; the encoded
// slice is rounded down to whole base64 quads so it decodes without padding
// complaints.
func decodePrefix(url string, maxBytes int) (raw []byte, truncated bool) {
	i := strings.Index(url, "base64,")
	if i < 0 {
		return nil, false
	}
	enc := url[i+len("base64,"):]
	if maxEnc := (maxBytes/3 + 1) * 4; len(enc) > maxEnc {
		enc, truncated = enc[:maxEnc], true
	}
	if enc = enc[:len(enc)/4*4]; enc == "" {
		return nil, truncated
	}
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, truncated
	}
	return b, truncated
}

func isPNG(b []byte) bool {
	return len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
}

func isGIF(b []byte) bool {
	return len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a")))
}

func isJPEG(b []byte) bool {
	return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

func isWebP(b []byte) bool {
	return len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
}

// pngSize reads the IHDR chunk, which the format requires to come first.
func pngSize(b []byte) (int, int) {
	if len(b) < 24 || !bytes.Equal(b[12:16], []byte("IHDR")) {
		return 0, 0
	}
	return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24]))
}

func gifSize(b []byte) (int, int) {
	if len(b) < 10 {
		return 0, 0
	}
	return int(binary.LittleEndian.Uint16(b[6:8])), int(binary.LittleEndian.Uint16(b[8:10]))
}

// jpegSize walks the marker segments to the start-of-frame, which is where the
// dimensions live. Standalone markers carry no length; every other segment
// does, and that length is how we skip EXIF.
func jpegSize(b []byte) (int, int) {
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			i++
			continue
		}
		marker := b[i+1]
		switch {
		case marker == 0xFF: // fill byte
			i++
			continue
		case marker == 0x01 || marker >= 0xD0 && marker <= 0xD8: // standalone
			i += 2
			continue
		}
		segLen := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if segLen < 2 {
			return 0, 0
		}
		// SOF0..SOF15 minus DHT (C4), JPG (C8), and DAC (CC).
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			if i+9 > len(b) {
				return 0, 0
			}
			// SOF: precision, height, width (all after the length field).
			return int(binary.BigEndian.Uint16(b[i+7 : i+9])), int(binary.BigEndian.Uint16(b[i+5 : i+7]))
		}
		i += 2 + segLen
	}
	return 0, 0
}

// webpSize handles the three chunk layouts: extended (VP8X), lossless (VP8L),
// and lossy (VP8). The last two pack dimensions into bit fields.
func webpSize(b []byte) (int, int) {
	if len(b) < 30 {
		return 0, 0
	}
	switch string(b[12:16]) {
	case "VP8X":
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1 // stored as size-1, 24-bit
	case "VP8L":
		if len(b) < 25 || b[20] != 0x2F {
			return 0, 0
		}
		bits := binary.LittleEndian.Uint32(b[21:25])
		return int(bits&0x3FFF) + 1, int(bits>>14&0x3FFF) + 1
	case "VP8 ":
		if b[23] != 0x9D || b[24] != 0x01 || b[25] != 0x2A {
			return 0, 0
		}
		w := int(binary.LittleEndian.Uint16(b[26:28]) & 0x3FFF)
		h := int(binary.LittleEndian.Uint16(b[28:30]) & 0x3FFF)
		return w, h
	}
	return 0, 0
}
