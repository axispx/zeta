package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebfetchValidation(t *testing.T) {
	if _, err := parseFetchURL("https://example.com/doc"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseFetchURL("http://example.com/doc"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseFetchURL("ftp://example.com"); err == nil {
		t.Fatal("expected scheme error")
	}
	if _, err := parseFetchURL("https://localhost/x"); err == nil {
		t.Fatal("expected localhost block")
	}
	if _, err := parseFetchURL("https://127.0.0.1/x"); err == nil {
		t.Fatal("expected loopback block")
	}
	if _, err := parseFetchURL("https://example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestWebfetchFormatBody(t *testing.T) {
	html := `<!doctype html><html><head><title>x</title><style>hide</style></head>` +
		`<body><h1>Title</h1><p>Hello <a href="/doc">Go</a> world</p>` +
		`<script>alert(1)</script></body></html>`

	md := formatBody([]byte(html), "text/html", "markdown", "https://go.dev")
	if !strings.Contains(md, "# Title") {
		t.Fatalf("markdown heading: %q", md)
	}
	if !strings.Contains(md, "[Go](https://go.dev/doc)") {
		t.Fatalf("markdown link (domain resolve): %q", md)
	}
	if !strings.Contains(md, "Hello") || !strings.Contains(md, "world") {
		t.Fatalf("markdown text: %q", md)
	}
	if strings.Contains(md, "alert") {
		t.Fatalf("script leaked: %q", md)
	}

	text := formatBody([]byte(html), "text/html", "text", "")
	if !strings.Contains(text, "Title") || !strings.Contains(text, "Hello") {
		t.Fatalf("text: %q", text)
	}
	if strings.Contains(text, "alert") {
		t.Fatalf("script leaked: %q", text)
	}

	raw := formatBody([]byte(html), "text/html", "html", "")
	if !strings.Contains(raw, "<h1>") {
		t.Fatalf("html: %q", raw)
	}
}

func TestWebfetchBinaryMIME(t *testing.T) {
	if !isBinaryMIME("image/png") || !isBinaryMIME("application/pdf") {
		t.Fatal("expected binary")
	}
	if isBinaryMIME("text/plain") || isBinaryMIME("application/json") || isBinaryMIME("") {
		t.Fatal("expected text")
	}
}

func TestWebfetchRunArgs(t *testing.T) {
	_, err := (webfetchTool{}).Run(context.Background(), "", mustRaw(t, map[string]any{"url": ""}))
	if err == nil {
		t.Fatal("expected empty url error")
	}
	_, err = (webfetchTool{}).Run(context.Background(), "", mustRaw(t, map[string]any{
		"url": "https://example.com", "format": "xml",
	}))
	if err == nil || !strings.Contains(err.Error(), "format") {
		t.Fatalf("format: %v", err)
	}
}

func TestWebfetchSummary(t *testing.T) {
	got := (webfetchTool{}).Summary(mustRaw(t, map[string]any{"url": "https://go.dev/doc"}))
	if got != "webfetch https://go.dev/doc" {
		t.Fatal(got)
	}
}

func TestWebfetchBlocksPrivateDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	_, _, err = fetchURL(context.Background(), "http://"+ln.Addr().String()+"/", defaultFetchTimeout)
	if err == nil {
		t.Fatal("expected private dial block")
	}
	if !strings.Contains(err.Error(), "not allowed") && !strings.Contains(err.Error(), "private") {
		t.Fatalf("unexpected error: %v", err)
	}
}
