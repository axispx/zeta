package image

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataURLFromBytesAndPath(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "a.png")
	data := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4}
	if err := os.WriteFile(png, data, 0o600); err != nil {
		t.Fatal(err)
	}

	url, err := DataURLFromBytes(data, MIMEPNG)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("url=%q", url)
	}

	ref, err := RefFromPath(png)
	if err != nil {
		t.Fatal(err)
	}
	if ref.MIME != MIMEPNG || ref.Name != "a.png" || !strings.HasPrefix(ref.URL, "data:image/png;base64,") {
		t.Fatalf("ref=%+v", ref)
	}

	if _, err := RefFromPath(filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("expected missing error")
	}
}

func TestRefFromBytesRejectsOversized(t *testing.T) {
	big := make([]byte, MaxImageBytes+1)
	if _, err := RefFromBytes(big, MIMEPNG, "x.png"); err == nil {
		t.Fatal("expected size reject")
	}
	if _, err := DataURLFromBytes(nil, MIMEPNG); err == nil {
		t.Fatal("expected empty reject")
	}
}

func TestSniffRejectsOversized(t *testing.T) {
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok.png")
	hdr := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 3}
	if err := os.WriteFile(okPath, hdr, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Sniff(okPath); !ok {
		t.Fatal("expected sniff ok")
	}
}
