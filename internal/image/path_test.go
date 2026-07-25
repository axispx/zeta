package image

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", false},
		{"hello world", "", false},
		{"/tmp/shot.png", "/tmp/shot.png", true},
		{`"/tmp/shot.png"`, "/tmp/shot.png", true},
		{"'~/Pictures/a.png'", "~/Pictures/a.png", true},
		{"file:///Users/a/x.png", "/Users/a/x.png", true},
		{"file://localhost/Users/a/x.png", "/Users/a/x.png", true},
		{`/tmp/my\ file.png`, "/tmp/my file.png", true},
		{"https://example.com/a.png", "", false},
		{"line1\nline2", "", false},
		{`C:\Users\a\x.png`, `C:\Users\a\x.png`, true},
		{"file:///C:/Users/a/x.png", "C:/Users/a/x.png", true},
		{"/tmp/a.png\n", "/tmp/a.png", true},
		{"file:///tmp/my%20shot.png\n", "/tmp/my shot.png", true},
	}
	for _, tt := range tests {
		got, ok := NormalizePath(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("NormalizePath(%q) = %q,%v want %q,%v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSniff(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "a.png")
	// minimal PNG header
	hdr := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0, 0, 0, 0, 0}
	if err := os.WriteFile(png, hdr, 0o600); err != nil {
		t.Fatal(err)
	}
	mime, ok := Sniff(png)
	if !ok || mime != MIMEPNG {
		t.Fatalf("png sniff = %q,%v", mime, ok)
	}

	txt := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(txt, []byte("hi"), 0o600)
	if _, ok := Sniff(txt); ok {
		t.Fatal("txt should not sniff as image")
	}

	jpg := filepath.Join(dir, "b.jpg")
	_ = os.WriteFile(jpg, []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}, 0o600)
	mime, ok = Sniff(jpg)
	if !ok || mime != MIMEJPEG {
		t.Fatalf("jpg sniff = %q,%v", mime, ok)
	}
}
