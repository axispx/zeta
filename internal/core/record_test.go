package core

import (
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/session"
)

func TestRecordFromAPIUser(t *testing.T) {
	img := image.Ref{URL: "data:image/png;base64,AAAA", MIME: "image/png"}
	rec := RecordFromAPI(ai.Message{Role: ai.RoleUser, Text: "hi", Images: []image.Ref{img}})
	if rec.Role != session.RoleUser || rec.Text != "hi" {
		t.Fatalf("rec=%+v", rec)
	}
	if len(rec.Images) != 1 || rec.Images[0].URL != img.URL {
		t.Fatalf("images=%+v", rec.Images)
	}
}

func TestRecordFromAPIAssistantCarriesToolCalls(t *testing.T) {
	rec := RecordFromAPI(ai.Message{
		Role: ai.RoleAssistant,
		Text: "calling",
		ToolCalls: []ai.ToolCall{
			{ID: "c1", Name: "bash", Arguments: `{"command":"ls"}`},
			{ID: "c2", Name: "read", Arguments: `{"path":"a"}`},
		},
	})
	if rec.Role != session.RoleAgent || rec.Text != "calling" {
		t.Fatalf("rec=%+v", rec)
	}
	if len(rec.ToolCalls) != 2 {
		t.Fatalf("toolcalls=%+v", rec.ToolCalls)
	}
	if rec.ToolCalls[0].ID != "c1" || rec.ToolCalls[0].Name != "bash" || rec.ToolCalls[0].Arguments != `{"command":"ls"}` {
		t.Fatalf("call[0]=%+v", rec.ToolCalls[0])
	}
	if rec.ToolCalls[1].ID != "c2" {
		t.Fatalf("call[1]=%+v", rec.ToolCalls[1])
	}
}

func TestRecordFromAPIToolKeepsCallID(t *testing.T) {
	rec := RecordFromAPI(ai.Message{Role: ai.RoleTool, Text: "ok", ToolCallID: "c1"})
	if rec.Role != session.RoleTool || rec.Text != "ok" || rec.ToolCallID != "c1" {
		t.Fatalf("rec=%+v", rec)
	}
}

func TestRecordFromAPIUnknownRoleIsError(t *testing.T) {
	rec := RecordFromAPI(ai.Message{Role: ai.RoleSystem, Text: "sys"})
	if rec.Role != session.RoleError || rec.Text != "sys" {
		t.Fatalf("rec=%+v", rec)
	}
}

func TestToolRecordAddsRowFields(t *testing.T) {
	rec := ToolRecord(ai.Message{Role: ai.RoleTool, Text: "out", ToolCallID: "c9"}, "bash ls", "bash", true)
	if rec.Label != "bash ls" || rec.Tool != "bash" || !rec.Denied {
		t.Fatalf("rec=%+v", rec)
	}
	// The wrapped mapping is unchanged.
	if rec.Role != session.RoleTool || rec.Text != "out" || rec.ToolCallID != "c9" {
		t.Fatalf("rec=%+v", rec)
	}
}
