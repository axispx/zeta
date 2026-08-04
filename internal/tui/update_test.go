package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/axispx/zeta/internal/update"
)

func TestStartUpdateSetsBusy(t *testing.T) {
	// startUpdate reads version.Version (dev in tests) → note + nil.
	// Exercise the busy/cancel path directly like other exclusive-job tests.
	m := testModel()
	cancelled := false
	m.updateCancel = func() { cancelled = true }
	m.updating = true
	if !m.busy() || !m.exclusiveJob() {
		t.Fatal("should be updating/busy/exclusive")
	}
	m.cancelUpdate()
	if !cancelled || m.updating || m.updateCancel != nil {
		t.Fatal("cancel should clear busy immediately")
	}
	if m.busy() {
		t.Fatal("should not be busy after cancel")
	}
}

func TestStartUpdateNoopWhenBusy(t *testing.T) {
	m := testModel()
	m.compacting = true
	if cmd := m.startUpdate(); cmd != nil {
		t.Fatal("expected nil while busy")
	}
}

func TestStartUpdateDevRefused(t *testing.T) {
	// version.Version is "dev" in tests (no ldflags).
	m := testModel()
	if cmd := m.startUpdate(); cmd != nil {
		t.Fatal("dev build should not start update")
	}
	if m.updating {
		t.Fatal("should not be updating")
	}
	if n := len(m.messages); n == 0 || m.messages[n-1].Text == "" {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestHandleUpdateDoneAlreadyLatest(t *testing.T) {
	m := testModel()
	m.updating = true
	m.handleUpdateDone(updateDoneMsg{
		result: update.Result{From: "0.11.0", To: "0.11.0", AlreadyLatest: true},
	})
	if m.updating {
		t.Fatal("updating still set")
	}
	if n := len(m.messages); n == 0 || m.messages[n-1].Text != "already on 0.11.0" {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestHandleUpdateDoneSuccess(t *testing.T) {
	m := testModel()
	m.updating = true
	m.handleUpdateDone(updateDoneMsg{
		result: update.Result{From: "0.10.0", To: "0.11.0"},
	})
	want := "updated 0.10.0 → 0.11.0 — restart zeta"
	if n := len(m.messages); n == 0 || m.messages[n-1].Text != want {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestHandleUpdateDoneError(t *testing.T) {
	m := testModel()
	m.updating = true
	m.handleUpdateDone(updateDoneMsg{err: errors.New("boom")})
	if n := len(m.messages); n == 0 || m.messages[n-1].Role != RoleError || m.messages[n-1].Text != "boom" {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestHandleUpdateDoneAfterCancelIsNoop(t *testing.T) {
	m := testModel()
	m.updating = true
	m.cancelUpdate()
	m.noteSystem(updateCancelledText) // tryInterrupt notes; done must not double-note
	n := len(m.messages)
	m.handleUpdateDone(updateDoneMsg{err: context.Canceled})
	if m.updating {
		t.Fatal("updating re-set")
	}
	if len(m.messages) != n {
		t.Fatalf("late done should not note again: %+v", m.messages)
	}
}

func TestHandleUpdateDoneCancelled(t *testing.T) {
	// Context canceled without prior cancelUpdate (e.g. parent ctx).
	m := testModel()
	m.updating = true
	m.handleUpdateDone(updateDoneMsg{err: context.Canceled})
	if n := len(m.messages); n == 0 || m.messages[n-1].Text != updateCancelledText {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestRunCommandUpdateDev(t *testing.T) {
	// version.Version is "dev" in tests — /update notes and does not arm busy.
	m := testModel()
	cmd := m.runCommand("/update")
	if cmd != nil {
		t.Fatal("dev /update should not return a network cmd")
	}
	if m.updating {
		t.Fatal("should not be updating")
	}
}

func TestHandleUpdateAvailable(t *testing.T) {
	m := testModel()
	m.handleUpdateAvailable(updateAvailableMsg{from: "0.10.0", to: "0.11.0"})
	want := "zeta 0.11.0 available (you have 0.10.0) — run /update"
	if n := len(m.messages); n == 0 || m.messages[n-1].Text != want {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestHandleUpdateAvailableQuietOnQuit(t *testing.T) {
	m := testModel()
	m.quitting = true
	m.handleUpdateAvailable(updateAvailableMsg{from: "0.10.0", to: "0.11.0"})
	if len(m.messages) != 0 {
		t.Fatalf("messages=%+v", m.messages)
	}
}

func TestCheckUpdateCmdSkipsDev(t *testing.T) {
	// version.Version is "dev" in tests (no ldflags).
	if cmd := checkUpdateCmd(); cmd != nil {
		t.Fatal("dev build should not schedule a check")
	}
}

func TestExclusiveJob(t *testing.T) {
	m := testModel()
	if m.exclusiveJob() {
		t.Fatal("idle")
	}
	m.compacting = true
	if !m.exclusiveJob() || !m.busy() {
		t.Fatal("compacting")
	}
	m.compacting = false
	m.updating = true
	if !m.exclusiveJob() || !m.busy() {
		t.Fatal("updating")
	}
	m.updating = false
	m.authRetrying = true
	if m.exclusiveJob() {
		t.Fatal("auth is busy but not exclusive")
	}
	if !m.busy() {
		t.Fatal("auth should still be busy")
	}
}
