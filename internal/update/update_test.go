package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.11.0", "0.11.0", 0},
		{"v0.11.0", "0.11.0", 0},
		{"0.10.0", "0.11.0", -1},
		{"0.11.0", "0.10.9", 1},
		{"0.11.0", "0.11.0-rc.1", 1},
		{"0.11.0-rc.1", "0.11.0", -1},
		{"0.11.0-rc.1", "0.11.0-rc.2", -1},
		{"1.0.0+build", "1.0.0", 0},
		{"1", "1.0.0", 0},
		{"1.2", "1.2.0", 0},
	}
	for _, tt := range tests {
		got, err := compareSemver(tt.a, tt.b)
		if err != nil {
			t.Fatalf("compareSemver(%q,%q): %v", tt.a, tt.b, err)
		}
		if got != tt.want {
			t.Errorf("compareSemver(%q,%q)=%d want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCompareSemverBad(t *testing.T) {
	if _, err := compareSemver("dev", "1.0.0"); err == nil {
		t.Fatal("expected error for dev")
	}
	if _, err := compareSemver("1.0.0", "nope"); err == nil {
		t.Fatal("expected error for nope")
	}
}

func TestChecksumFor(t *testing.T) {
	sums := []byte("" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  zeta-1.0.0-darwin-arm64\n" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb *zeta-1.0.0-linux-amd64\n")
	got, err := checksumFor(sums, "zeta-1.0.0-darwin-arm64")
	if err != nil || got != strings.Repeat("a", 64) {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = checksumFor(sums, "zeta-1.0.0-linux-amd64")
	if err != nil || got != strings.Repeat("b", 64) {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := checksumFor(sums, "missing"); err == nil {
		t.Fatal("expected missing")
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("0.11.0", "darwin", "arm64"); got != "zeta-0.11.0-darwin-arm64" {
		t.Fatal(got)
	}
}

func TestIsDev(t *testing.T) {
	for _, v := range []string{"", "dev", "DEV", " Dev "} {
		if !IsDev(v) {
			t.Fatalf("IsDev(%q)=false", v)
		}
	}
	if IsDev("0.11.0") {
		t.Fatal("release is not dev")
	}
}

func TestApplyDevRefused(t *testing.T) {
	_, err := Apply(context.Background(), Options{
		Current: "dev",
		Target:  filepath.Join(t.TempDir(), "zeta"),
		GOOS:    "darwin",
		GOARCH:  "arm64",
	})
	if err == nil || !strings.Contains(err.Error(), "dev build") {
		t.Fatalf("got %v", err)
	}
}

func TestApplyUnsupported(t *testing.T) {
	_, err := Apply(context.Background(), Options{
		Current: "0.1.0",
		Target:  filepath.Join(t.TempDir(), "zeta"),
		GOOS:    "windows",
		GOARCH:  "amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported OS") {
		t.Fatalf("got %v", err)
	}
}

func TestCheckAvailable(t *testing.T) {
	srv := newReleaseServer(t, "v0.12.0", "darwin", "arm64", []byte("bin"))
	defer srv.Close()

	res, err := Check(context.Background(), Options{
		Current: "0.11.0",
		Target:  filepath.Join(t.TempDir(), "zeta"),
		GOOS:    "darwin",
		GOARCH:  "arm64",
		Client:  srv.Client(),
		BaseAPI: srv.URL,
		Repo:    "axispx/zeta",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyLatest || res.From != "0.11.0" || res.To != "0.12.0" {
		t.Fatalf("%+v", res)
	}
}

func TestApplyAlreadyLatest(t *testing.T) {
	srv := newReleaseServer(t, "v0.11.0", "darwin", "arm64", []byte("binary-v0.11.0"))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "zeta")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(context.Background(), Options{
		Current: "0.11.0",
		Target:  target,
		GOOS:    "darwin",
		GOARCH:  "arm64",
		Client:  srv.Client(),
		BaseAPI: srv.URL,
		Repo:    "axispx/zeta",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyLatest || res.To != "0.11.0" {
		t.Fatalf("%+v", res)
	}
	// Binary untouched.
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("binary changed: %q", got)
	}
}

func TestApplyUpdates(t *testing.T) {
	payload := []byte("new-zeta-binary-contents")
	srv := newReleaseServer(t, "v0.12.0", "linux", "amd64", payload)
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "zeta")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Apply(context.Background(), Options{
		Current: "0.11.0",
		Target:  target,
		GOOS:    "linux",
		GOARCH:  "amd64",
		Client:  srv.Client(),
		BaseAPI: srv.URL,
		Repo:    "axispx/zeta",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyLatest || res.From != "0.11.0" || res.To != "0.12.0" {
		t.Fatalf("%+v", res)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("not executable: %v", fi.Mode())
	}
}

func TestApplyChecksumMismatch(t *testing.T) {
	payload := []byte("payload")
	// Serve a wrong checksum on purpose.
	mux := http.NewServeMux()
	artifact := "zeta-0.12.0-darwin-arm64"
	mux.HandleFunc("/repos/axispx/zeta/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"tag_name":"v0.12.0",
			"assets":[
				{"name":%q,"browser_download_url":%q},
				{"name":"SHA256SUMS","browser_download_url":%q}
			]
		}`, artifact, "http://"+r.Host+"/bin", "http://"+r.Host+"/sums")
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", strings.Repeat("0", 64), artifact)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "zeta")
	_ = os.WriteFile(target, []byte("old"), 0o755)

	_, err := Apply(context.Background(), Options{
		Current: "0.11.0",
		Target:  target,
		GOOS:    "darwin",
		GOARCH:  "arm64",
		Client:  srv.Client(),
		BaseAPI: srv.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("got %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatal("binary should be unchanged")
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	if err := replaceFile(path, []byte("hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello" {
		t.Fatalf("%q %v", got, err)
	}
	// Overwrite existing.
	if err := replaceFile(path, []byte("world"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "world" {
		t.Fatal(string(got))
	}
}

func TestDownloadExceedsCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789")) // 10 bytes
	}))
	defer srv.Close()

	_, err := download(context.Background(), srv.Client(), srv.URL, 4)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("got %v", err)
	}
}

// newReleaseServer serves a fake GitHub latest release + assets.
func newReleaseServer(t *testing.T, tag, goos, goarch string, payload []byte) *httptest.Server {
	t.Helper()
	ver := strings.TrimPrefix(tag, "v")
	artifact := assetName(ver, goos, goarch)
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/axispx/zeta/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		base := "http://" + r.Host
		fmt.Fprintf(w, `{
			"tag_name":%q,
			"assets":[
				{"name":%q,"browser_download_url":%q},
				{"name":"SHA256SUMS","browser_download_url":%q}
			]
		}`, tag, artifact, base+"/bin/"+artifact, base+"/SHA256SUMS")
	})
	mux.HandleFunc("/bin/"+artifact, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hexSum, artifact)
	})
	return httptest.NewServer(mux)
}
