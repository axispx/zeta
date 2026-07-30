package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/axispx/zeta/internal/config"
)

func TestUpdatePasteMsgInsertsImage(t *testing.T) {
	isolateZetaHome(t)
	dir := t.TempDir()
	png := filepath.Join(dir, "x.png")
	_ = os.WriteFile(png, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4}, 0o600)

	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.ready = true
	m.width, m.height = 80, 24
	m.layout()

	model, _ := m.Update(tea.PasteMsg{Content: png})
	mm := model.(Model)
	if !strings.Contains(mm.textarea.Value(), "[Image 1]") {
		t.Fatalf("after paste input=%q pending=%d", mm.textarea.Value(), len(mm.pendingImages))
	}
}

func TestIsPasteKey(t *testing.T) {
	if !isPasteKey(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModSuper})) {
		t.Fatal("super+v")
	}
	if !isPasteKey(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModCtrl})) {
		t.Fatal("ctrl+v")
	}
	if isPasteKey(tea.KeyPressMsg(tea.Key{Code: 'v'})) {
		t.Fatal("bare v")
	}
}

func TestUpdateSuperVClipboard(t *testing.T) {
	// ensure super+v is consumed (no panic); may paste text or image
	isolateZetaHome(t)
	m, err := New(config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.ready = true
	m.width, m.height = 80, 24
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModSuper}))
}
