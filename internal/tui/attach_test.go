package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/config"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/session"
)

const testPNGDataURL = "data:image/png;base64,iVBORw0KGgo="

func testAttach(name string) image.Ref {
	return image.Ref{URL: testPNGDataURL, MIME: "image/png", Name: name}
}

func TestTryAttachPath(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "shot.png")
	hdr := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 3}
	if err := os.WriteFile(png, hdr, 0o600); err != nil {
		t.Fatal(err)
	}

	a, ok := tryAttachPath(png)
	if !ok {
		t.Fatal("expected attach")
	}
	if a.MIME != "image/png" || a.Name != "shot.png" || !strings.HasPrefix(a.URL, "data:image/png;base64,") {
		t.Fatalf("attach=%+v", a)
	}

	if _, ok := tryAttachPath("hello world"); ok {
		t.Fatal("text should not attach")
	}
	txt := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(txt, []byte("hi"), 0o600)
	if _, ok := tryAttachPath(txt); ok {
		t.Fatal("non-image should not attach")
	}
}

func TestInsertAndParseComposer(t *testing.T) {
	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.textarea.SetValue("look at")
	m.insertImageAttach(testAttach("a.png"))
	if got := m.textarea.Value(); got != "look at [Image 1] " {
		t.Fatalf("input=%q", got)
	}
	text, imgs := m.parseComposer()
	if text != "look at" || len(imgs) != 1 || imgs[0].URL != testPNGDataURL {
		t.Fatalf("parse text=%q imgs=%+v", text, imgs)
	}
}

func TestSubmitWithImages(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	m, err := New(config.Config{
		Active: "x/y",
		Providers: map[string]config.Provider{
			"x": {
				BaseURL: "http://127.0.0.1:1",
				APIKey:  "k",
				Models: map[string]config.ModelDef{
					"y": {ContextWindow: 128000},
				},
			},
		},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.textarea.SetValue("describe")
	m.insertImageAttach(testAttach("a.png"))
	_ = m.submitInput()

	if len(m.pendingImages) != 0 {
		t.Fatal("pending should clear on submit")
	}

	if m.textarea.Value() != "" {
		t.Fatalf("input should clear, got %q", m.textarea.Value())
	}
	if len(m.messages) < 1 || m.messages[0].Role != RoleUser {
		t.Fatalf("messages=%+v", m.messages)
	}
	if !strings.Contains(m.messages[0].Text, "describe") || !strings.Contains(m.messages[0].Text, "[Image 1 · a.png]") {
		t.Fatalf("display=%q", m.messages[0].Text)
	}
	if len(m.history) != 1 || m.history[0].Text != "describe" || len(m.history[0].Images) != 1 || m.history[0].Images[0].URL != testPNGDataURL {
		t.Fatalf("history=%+v", m.history)
	}
}

func TestSubmitImageOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZETA_HOME", dir)

	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.insertImageAttach(testAttach("b.png"))
	_ = m.submitInput()
	if len(m.history) != 1 || m.history[0].Text != "" || len(m.history[0].Images) != 1 {
		t.Fatalf("history=%+v", m.history)
	}
	if m.history[0].Images[0].URL != testPNGDataURL {
		t.Fatalf("url=%q", m.history[0].Images[0].URL)
	}
}

func TestLoadSessionImages(t *testing.T) {
	ui, hist := loadSession([]session.Record{
		{
			Role: session.RoleUser,
			Text: "look",
			Images: []session.ImageRef{
				{URL: testPNGDataURL, MIME: "image/png", Name: "x.png"},
			},
		},
	})
	if len(ui) != 1 || !strings.Contains(ui[0].Text, "[Image 1 · x.png]") {
		t.Fatalf("ui=%+v", ui)
	}
	if len(hist) != 1 || hist[0].Role != ai.RoleUser || len(hist[0].Images) != 1 || hist[0].Images[0].URL != testPNGDataURL {
		t.Fatalf("hist=%+v", hist)
	}
}

func TestSlashCommandRefusesImages(t *testing.T) {
	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.textarea.SetValue("/clear")
	m.insertImageAttach(testAttach("a.png"))
	_ = m.submitInput()
	if len(m.pendingImages) != 1 {
		t.Fatal("pending should remain when slash refused")
	}
	if len(m.messages) == 0 || !strings.Contains(m.messages[len(m.messages)-1].Text, "slash commands") {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestSyncPendingImagesOnDelete(t *testing.T) {
	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.insertImageAttach(testAttach("a.png"))
	m.insertImageAttach(testAttach("b.png"))
	if !strings.Contains(m.textarea.Value(), "[Image 1]") || !strings.Contains(m.textarea.Value(), "[Image 2]") {
		t.Fatalf("input=%q", m.textarea.Value())
	}
	// Delete first token — remaining token keeps its stable id (no renumber rewrite).
	m.textarea.SetValue("[Image 2]")
	m.syncPendingImages()
	if len(m.pendingImages) != 1 || m.pendingImages[2].Name != "b.png" {
		t.Fatalf("pending=%+v", m.pendingImages)
	}
	if _, ok := m.pendingImages[1]; ok {
		t.Fatal("slot 1 should be dropped")
	}
	if got := m.textarea.Value(); got != "[Image 2]" {
		t.Fatalf("token should stay stable, got %q", got)
	}
	// Next insert allocates 3, not reusing 1.
	m.insertImageAttach(testAttach("c.png"))
	if _, ok := m.pendingImages[3]; !ok {
		t.Fatalf("expected slot 3, pending=%+v", m.pendingImages)
	}
}

func TestPathPasteInsertsToken(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "drop.png")
	_ = os.WriteFile(png, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1}, 0o600)

	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !m.handleBracketPaste(png) {
		t.Fatal("expected path paste attach")
	}
	if !strings.Contains(m.textarea.Value(), "[Image 1]") {
		t.Fatalf("input=%q", m.textarea.Value())
	}
	if len(m.pendingImages) != 1 || !strings.HasPrefix(m.pendingImages[1].URL, "data:") {
		t.Fatalf("pending=%+v", m.pendingImages)
	}
}
