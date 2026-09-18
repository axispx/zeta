package image

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

// dataURL wraps raw image bytes the way attach does.
func dataURL(mime string, b []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// pngHeader is a signature plus an IHDR chunk carrying w x h.
func pngHeader(w, h int) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	b.Write([]byte{0, 0, 0, 13})
	b.WriteString("IHDR")
	for _, v := range []uint32{uint32(w), uint32(h)} {
		_ = binary.Write(&b, binary.BigEndian, v)
	}
	b.Write([]byte{8, 6, 0, 0, 0})
	b.Write([]byte{0, 0, 0, 0})
	return b.Bytes()
}

func gifHeader(w, h int) []byte {
	var b bytes.Buffer
	b.WriteString("GIF89a")
	for _, v := range []uint16{uint16(w), uint16(h)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.Write([]byte{0, 0, 0})
	return b.Bytes()
}

// jpegHeader puts an APP0 segment with a dummy payload ahead of the SOF, the
// way a real file's EXIF sits before the frame header.
func jpegHeader(w, h, appLen int) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})
	b.Write([]byte{0xFF, 0xE0})
	_ = binary.Write(&b, binary.BigEndian, uint16(appLen))
	b.Write(bytes.Repeat([]byte{'x'}, appLen-2))
	b.Write([]byte{0xFF, 0xC0})
	_ = binary.Write(&b, binary.BigEndian, uint16(17))
	b.WriteByte(8)
	_ = binary.Write(&b, binary.BigEndian, uint16(h))
	_ = binary.Write(&b, binary.BigEndian, uint16(w))
	b.Write([]byte{3, 1, 0x22, 0})
	return b.Bytes()
}

func TestDimensions(t *testing.T) {
	little, big := 320, 240
	tests := []struct {
		name string
		url  string
		w, h int
	}{
		{"png", dataURL(MIMEPNG, pngHeader(1024, 768)), 1024, 768},
		{"gif", dataURL(MIMEGIF, gifHeader(little, big)), little, big},
		{"jpeg", dataURL(MIMEJPEG, jpegHeader(800, 600, 16)), 800, 600},
		// A big APP segment must be skipped by its length, not by a fixed offset.
		{"jpeg past exif", dataURL(MIMEJPEG, jpegHeader(1920, 1080, 4096)), 1920, 1080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := Dimensions(tt.url)
			if w != tt.w || h != tt.h {
				t.Fatalf("Dimensions() = %dx%d, want %dx%d", w, h, tt.w, tt.h)
			}
		})
	}
}

func TestDimensionsWebP(t *testing.T) {
	// VP8X stores both dimensions as 24-bit size-minus-one.
	vp8x := []byte("RIFF\x00\x00\x00\x00WEBPVP8X\x00\x00\x00\x00\x00\x00\x00\x00")
	vp8x = append(vp8x, 0xFF, 0x03, 0x00) // 1024 - 1
	vp8x = append(vp8x, 0x57, 0x02, 0x00) // 600 - 1
	if w, h := Dimensions(dataURL(MIMEWebP, vp8x)); w != 1024 || h != 600 {
		t.Fatalf("VP8X = %dx%d, want 1024x600", w, h)
	}

	// VP8L packs both dimensions into 14-bit fields of one little-endian word.
	bits := uint32(799) | uint32(599)<<14
	var vp8l bytes.Buffer
	vp8l.WriteString("RIFF\x00\x00\x00\x00WEBPVP8L\x00\x00\x00\x00")
	vp8l.WriteByte(0x2F)
	_ = binary.Write(&vp8l, binary.LittleEndian, bits)
	vp8l.Write(make([]byte, 8))
	if w, h := Dimensions(dataURL(MIMEWebP, vp8l.Bytes())); w != 800 || h != 600 {
		t.Fatalf("VP8L = %dx%d, want 800x600", w, h)
	}

	// VP8 (lossy) has a start code before the 14-bit dimensions.
	var vp8 bytes.Buffer
	vp8.WriteString("RIFF\x00\x00\x00\x00WEBPVP8 \x00\x00\x00\x00")
	vp8.Write([]byte{0, 0, 0, 0x9D, 0x01, 0x2A})
	_ = binary.Write(&vp8, binary.LittleEndian, uint16(640))
	_ = binary.Write(&vp8, binary.LittleEndian, uint16(480))
	if w, h := Dimensions(dataURL(MIMEWebP, vp8.Bytes())); w != 640 || h != 480 {
		t.Fatalf("VP8 = %dx%d, want 640x480", w, h)
	}
}

func TestDimensionsUnknown(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"plain url", "https://example.com/a.png"},
		{"data url without base64", "data:image/png,abc"},
		{"unknown format", dataURL("image/tiff", []byte("II*\x00garbagegarbage"))},
		{"truncated png", dataURL(MIMEPNG, pngHeader(10, 10)[:16])},
		{"garbage", dataURL(MIMEPNG, []byte("not an image at all"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w, h := Dimensions(tt.url); w != 0 || h != 0 {
				t.Fatalf("Dimensions(%q) = %dx%d, want 0x0", tt.url, w, h)
			}
		})
	}
}

// A header beyond the read window must not derail decoding: the estimator only
// needs the front of the image, however large the payload is.
func TestDimensionsLargePayload(t *testing.T) {
	url := dataURL(MIMEPNG, append(pngHeader(640, 480), bytes.Repeat([]byte{0xAB}, 4<<20)...))
	if w, h := Dimensions(url); w != 640 || h != 480 {
		t.Fatalf("Dimensions() = %dx%d, want 640x480", w, h)
	}
}

// A truncated tail is expected — decodePrefix rounds down to whole base64
// quads, so padding complaints must not read as an unparseable header.
func TestDimensionsTruncatedTail(t *testing.T) {
	full := dataURL(MIMEPNG, pngHeader(300, 200))
	for _, n := range []int{1, 2, 3} {
		cut := full[:len(full)-n]
		if w, h := Dimensions(cut); w != 300 || h != 200 {
			t.Fatalf("truncated by %d: got %dx%d, want 300x200", n, w, h)
		}
	}
}

func TestDimensionsIgnoresNonBase64URL(t *testing.T) {
	if w, h := Dimensions(strings.Repeat("data:image/png;base64,", 1) + "@@@@"); w != 0 || h != 0 {
		t.Fatalf("invalid base64 = %dx%d, want 0x0", w, h)
	}
}
