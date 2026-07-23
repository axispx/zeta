package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
)

const (
	maxWebFetchDownload = 5 * 1024 * 1024
	maxWebFetchOutput   = 512 * 1024
	defaultFetchTimeout = 30 * time.Second
	maxFetchTimeout     = 120 * time.Second
	webFetchUserAgent   = "Mozilla/5.0 (compatible; zeta/1.0)"
)

type webfetchTool struct{}

func (webfetchTool) Name() string { return "webfetch" }
func (webfetchTool) Description() string {
	return "Fetch content from a URL. Converts HTML to markdown by default. " +
		"Use for docs and pages when you already have the URL; use websearch to discover URLs."
}
func (webfetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Fully-formed http(s) URL to fetch",
			},
			"format": map[string]any{
				"type":        "string",
				"enum":        []string{"markdown", "text", "html"},
				"description": "Output format (default: markdown)",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (default 30, max 120)",
			},
		},
		"required": []string{"url"},
	}
}

func (webfetchTool) Summary(raw json.RawMessage) string {
	var a struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(raw, &a)
	u := strings.TrimSpace(a.URL)
	if u == "" {
		return "webfetch"
	}
	if len(u) > 60 {
		u = u[:57] + "..."
	}
	return "webfetch " + u
}

type webfetchArgs struct {
	URL     string `json:"url"`
	Format  string `json:"format"`
	Timeout int    `json:"timeout"`
}

func (webfetchTool) Run(ctx context.Context, _ string, raw json.RawMessage) (string, error) {
	var args webfetchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	rawURL := strings.TrimSpace(args.URL)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", fmt.Errorf("url must start with http:// or https://")
	}

	u, err := parseFetchURL(rawURL)
	if err != nil {
		return "", err
	}

	format := strings.ToLower(strings.TrimSpace(args.Format))
	if format == "" {
		format = "markdown"
	}
	switch format {
	case "markdown", "text", "html":
	default:
		return "", fmt.Errorf("format must be markdown, text, or html")
	}

	timeout := defaultFetchTimeout
	if args.Timeout > 0 {
		timeout = time.Duration(args.Timeout) * time.Second
		if timeout > maxFetchTimeout {
			timeout = maxFetchTimeout
		}
	}

	body, contentType, err := fetchURL(ctx, u.String(), timeout)
	if err != nil {
		return "", err
	}

	mime := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if isBinaryMIME(mime) {
		return fmt.Sprintf("binary content (%s, %d bytes) — cannot return in text format", mime, len(body)), nil
	}

	out := formatBody(body, mime, format, u.String())
	if len(out) > maxWebFetchOutput {
		out = out[:maxWebFetchOutput] + "\n\n[truncated]"
	}
	return out, nil
}

func parseFetchURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url must use http or https")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("url host is required")
	}
	if err := checkHostAllowed(host); err != nil {
		return nil, err
	}
	return u, nil
}

func checkHostAllowed(host string) error {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || lower == "0.0.0.0" {
		return fmt.Errorf("url host %q is not allowed", host)
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
		return fmt.Errorf("url host %q is not allowed", host)
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate()
}

func fetchURL(ctx context.Context, rawURL string, timeout time.Duration) ([]byte, string, error) {
	client := &http.Client{
		Timeout:   timeout,
		Transport: safeHTTPTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if _, err := parseFetchURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", webFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(cl, 10, 64); err == nil && n > maxWebFetchDownload {
			return nil, "", fmt.Errorf("response too large (exceeds %d bytes)", maxWebFetchDownload)
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWebFetchDownload+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxWebFetchDownload {
		return nil, "", fmt.Errorf("response too large (exceeds %d bytes)", maxWebFetchDownload)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		if snippet == "" {
			return nil, "", fmt.Errorf("http %d", resp.StatusCode)
		}
		return nil, "", fmt.Errorf("http %d: %s", resp.StatusCode, snippet)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// safeHTTPTransport dials only after confirming the resolved IP is public,
// closing the DNS-rebinding TOCTOU gap of a pre-request LookupIP.
func safeHTTPTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if err := checkHostAllowed(host); err != nil {
				return nil, err
			}
			if ip := net.ParseIP(host); ip != nil {
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("could not resolve host %q: %w", host, err)
			}
			var last error
			for _, ipa := range ips {
				if isPrivateIP(ipa.IP) {
					last = fmt.Errorf("url host %q resolves to a private address", host)
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipa.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			if last == nil {
				last = fmt.Errorf("no allowed addresses for %q", host)
			}
			return nil, last
		},
	}
}

func isBinaryMIME(mime string) bool {
	if mime == "" || strings.HasPrefix(mime, "text/") {
		return false
	}
	switch mime {
	case "application/json", "application/ld+json", "application/xml",
		"application/javascript", "application/xhtml+xml":
		return false
	}
	return true
}

func formatBody(body []byte, mime, format, pageURL string) string {
	text := string(body)
	isHTML := strings.Contains(mime, "html") || looksLikeHTML(text)

	switch format {
	case "html":
		return text
	case "text":
		if isHTML {
			return htmlToText(text)
		}
		return text
	default:
		if isHTML {
			return htmlToMarkdown(text, pageURL)
		}
		return text
	}
}

func looksLikeHTML(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html")
}

func htmlToMarkdown(src, pageURL string) string {
	var opts []converter.ConvertOptionFunc
	if pageURL != "" {
		opts = append(opts, converter.WithDomain(pageURL))
	}
	md, err := htmltomarkdown.ConvertString(src, opts...)
	if err != nil {
		return htmlToText(src)
	}
	return strings.TrimSpace(md)
}

func htmlToText(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return strings.TrimSpace(src)
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "iframe", "object", "embed":
				return
			case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6":
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
			}
		}
		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t == "" {
				return
			}
			if b.Len() > 0 {
				if last := b.String()[b.Len()-1]; last != '\n' && last != ' ' {
					b.WriteByte(' ')
				}
			}
			b.WriteString(t)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.TrimSpace(b.String())
}
