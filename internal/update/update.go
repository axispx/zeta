// Package update self-updates the zeta binary from GitHub Releases.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultRepo = "axispx/zeta"
	githubAPI   = "https://api.github.com"
	userAgent   = "zeta-update"
	// dialTimeout bounds connect/TLS only. Whole-request Timeout is intentionally
	// unset so large binaries are not cut off mid-download; callers cancel via ctx.
	dialTimeout   = 15 * time.Second
	maxBinarySize = 200 << 20 // 200 MiB safety cap
)

// Options configures an update run. Zero value uses production defaults.
type Options struct {
	// Current is the running version (no leading "v"). "dev" refuses to update.
	Current string
	// Target is the binary path to replace. Empty → os.Executable (resolved).
	Target string
	// GOOS / GOARCH select the release asset. Empty → runtime values.
	GOOS, GOARCH string
	// Repo is "owner/name". Empty → axispx/zeta.
	Repo string
	// Client is used for GitHub API and asset downloads. Empty → default client.
	Client *http.Client
	// BaseAPI overrides the GitHub API root (tests). Empty → api.github.com.
	BaseAPI string
}

// Result is the outcome of Check or Apply.
type Result struct {
	From, To      string
	AlreadyLatest bool
	Path          string
}

// Check reports whether a newer GitHub Release exists. It does not download.
// Dev builds and unsupported platforms return an error (callers may ignore).
func Check(ctx context.Context, opts Options) (Result, error) {
	res, _, err := check(ctx, normalize(opts))
	return res, err
}

// Apply checks GitHub Releases for a newer zeta and replaces the binary in place.
func Apply(ctx context.Context, opts Options) (Result, error) {
	opts = normalize(opts)
	res, rel, err := check(ctx, opts)
	if err != nil || res.AlreadyLatest {
		return res, err
	}

	artifact := assetName(res.To, opts.GOOS, opts.GOARCH)
	binURL, sumURL, err := rel.assetURLs(artifact)
	if err != nil {
		return res, err
	}

	sums, err := download(ctx, opts.Client, sumURL, 1<<20)
	if err != nil {
		return res, fmt.Errorf("download checksums: %w", err)
	}
	want, err := checksumFor(sums, artifact)
	if err != nil {
		return res, err
	}

	body, err := download(ctx, opts.Client, binURL, maxBinarySize)
	if err != nil {
		return res, fmt.Errorf("download binary: %w", err)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return res, fmt.Errorf("checksum mismatch for %s", artifact)
	}

	if err := replaceFile(opts.Target, body, 0o755); err != nil {
		return res, err
	}
	return res, nil
}

// check compares the current version to GitHub's latest release.
// opts must already be normalized.
func check(ctx context.Context, opts Options) (Result, release, error) {
	res := Result{From: opts.Current, Path: opts.Target}

	if err := checkSupported(opts); err != nil {
		return res, release{}, err
	}
	if IsDev(opts.Current) {
		return res, release{}, errors.New("cannot update a dev build; install a release binary")
	}

	rel, err := latestRelease(ctx, opts)
	if err != nil {
		return res, release{}, err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	if latest == "" {
		return res, release{}, errors.New("latest release has empty tag")
	}
	res.To = latest

	cmp, err := compareSemver(opts.Current, latest)
	if err != nil {
		return res, release{}, err
	}
	if cmp >= 0 {
		res.AlreadyLatest = true
		res.To = opts.Current
		return res, rel, nil
	}
	return res, rel, nil
}

func normalize(opts Options) Options {
	if opts.Repo == "" {
		opts.Repo = defaultRepo
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.BaseAPI == "" {
		opts.BaseAPI = githubAPI
	}
	if opts.Client == nil {
		opts.Client = defaultHTTPClient()
	}
	if opts.Target == "" {
		opts.Target, _ = Executable()
	}
	opts.Current = strings.TrimPrefix(strings.TrimSpace(opts.Current), "v")
	return opts
}

// Executable returns the resolved path of the running binary.
func Executable() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}

// IsDev reports whether v is an unreleased/dev build that cannot self-update.
func IsDev(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "" || v == "dev"
}

// defaultHTTPClient has dial/TLS timeouts but no whole-request Timeout so a
// large release binary can finish on a slow link. Cancel via request context.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: dialTimeout}).DialContext,
			TLSHandshakeTimeout:   dialTimeout,
			ResponseHeaderTimeout: dialTimeout,
			ForceAttemptHTTP2:     true,
		},
	}
}

func checkSupported(opts Options) error {
	switch opts.GOOS {
	case "darwin", "linux":
	default:
		return fmt.Errorf("unsupported OS %q", opts.GOOS)
	}
	switch opts.GOARCH {
	case "amd64", "arm64":
	default:
		return fmt.Errorf("unsupported architecture %q", opts.GOARCH)
	}
	if opts.Target == "" {
		return errors.New("cannot resolve executable path")
	}
	return nil
}

func assetName(version, goos, goarch string) string {
	return fmt.Sprintf("zeta-%s-%s-%s", version, goos, goarch)
}

// release is the GitHub release JSON we care about.
type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r release) assetURLs(artifact string) (binURL, sumURL string, err error) {
	for _, a := range r.Assets {
		switch a.Name {
		case artifact:
			binURL = a.BrowserDownloadURL
		case "SHA256SUMS":
			sumURL = a.BrowserDownloadURL
		}
	}
	if binURL == "" {
		return "", "", fmt.Errorf("release %s has no asset %s", r.TagName, artifact)
	}
	if sumURL == "" {
		return "", "", fmt.Errorf("release %s has no SHA256SUMS", r.TagName)
	}
	return binURL, sumURL, nil
}

func latestRelease(ctx context.Context, opts Options) (release, error) {
	url := strings.TrimRight(opts.BaseAPI, "/") + "/repos/" + opts.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := opts.Client.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return release{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return release{}, fmt.Errorf("github releases: %s", resp.Status)
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return release{}, fmt.Errorf("github releases: %w", err)
	}
	return rel, nil
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	// Read one byte past limit so we can distinguish a full-cap body from truncation.
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("asset exceeds %d byte cap", limit)
	}
	return body, nil
}

// checksumFor parses GNU coreutils-style SHA256SUMS ("<hex>  <name>").
func checksumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// "hash  name" or "hash *name"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		file := strings.TrimPrefix(fields[len(fields)-1], "*")
		if file == name {
			h := fields[0]
			if len(h) != 64 {
				return "", fmt.Errorf("invalid checksum for %s", name)
			}
			return h, nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found in SHA256SUMS", name)
}

// replaceFile writes data to path via a same-dir temp file + rename.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Probe writability early for a clearer error than rename failure.
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	f, err := os.CreateTemp(dir, ".zeta-update-*")
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", dir, err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("cannot replace %s: %w", path, err)
	}
	ok = true
	return nil
}

// compareSemver returns -1 if a<b, 0 if equal, 1 if a>b.
// Accepts optional leading "v" and a trailing pre-release (-suffix) which sorts
// below the matching release. Build metadata (+…) is ignored.
func compareSemver(a, b string) (int, error) {
	ap, err := parseSemver(a)
	if err != nil {
		return 0, fmt.Errorf("current version %q: %w", a, err)
	}
	bp, err := parseSemver(b)
	if err != nil {
		return 0, fmt.Errorf("latest version %q: %w", b, err)
	}
	for i := 0; i < 3; i++ {
		if ap.num[i] < bp.num[i] {
			return -1, nil
		}
		if ap.num[i] > bp.num[i] {
			return 1, nil
		}
	}
	// No pre-release > has pre-release (1.0.0 > 1.0.0-rc.1).
	if ap.pre == "" && bp.pre != "" {
		return 1, nil
	}
	if ap.pre != "" && bp.pre == "" {
		return -1, nil
	}
	if ap.pre < bp.pre {
		return -1, nil
	}
	if ap.pre > bp.pre {
		return 1, nil
	}
	return 0, nil
}

type semver struct {
	num [3]int
	pre string
}

func parseSemver(s string) (semver, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return semver{}, errors.New("empty")
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var out semver
	if i := strings.IndexByte(s, '-'); i >= 0 {
		out.pre = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return semver{}, fmt.Errorf("want major.minor.patch, got %q", s)
	}
	for i, p := range parts {
		if p == "" {
			return semver{}, fmt.Errorf("empty component in %q", s)
		}
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return semver{}, fmt.Errorf("non-numeric %q", p)
			}
			n = n*10 + int(c-'0')
		}
		out.num[i] = n
	}
	return out, nil
}
