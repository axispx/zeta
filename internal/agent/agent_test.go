package agent

import (
	"context"
	"testing"
	"time"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/tools"
)

func TestEmitToolOutNonBlocking(t *testing.T) {
	ev := make(chan Event) // unbuffered: send blocks unless select/default
	done := make(chan struct{})
	go func() {
		defer close(done)
		emitToolOut(ev, tools.Bash, "line")
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emitToolOut blocked")
	}
}

func alwaysGate(string) bool { return true }

func TestExecToolGateDeny(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Replies: replies, Gate: alwaysGate}
	ev := make(chan Event, 4)
	go replyStart(t, ev, replies, false)

	_, result, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"echo hi"}`,
	}, ev)
	if !denied || result.Text != "rejected: the user denied this call" {
		t.Errorf("denied=%v text=%q", denied, result.Text)
	}
}

func TestExecToolGatePathDetail(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Replies: replies, Gate: alwaysGate}
	ev := make(chan Event, 4)
	got := make(chan Event, 1)
	go func() {
		start := recvStart(t, ev)
		got <- start
		replies <- DenyTool()
	}()

	_, _, _ = c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Write, Arguments: `{"path":"a.txt","content":"x"}`,
	}, ev)
	start := <-got
	if start.Path != "a.txt" || start.Name != "write" || start.Detail == "" {
		t.Fatalf("start=%+v", start)
	}
}

func TestExecToolGateAllow(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Replies: replies, Gate: alwaysGate}
	ev := make(chan Event, 8)
	go replyStart(t, ev, replies, true)
	_, result, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"echo first"}`,
	}, ev)
	if denied {
		t.Errorf("expected allowed, got %q", result.Text)
	}
}

func TestExecToolGateCancelled(t *testing.T) {
	replies := make(chan Reply) // unbuffered; leave unanswered
	ctx, cancel := context.WithCancel(t.Context())
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Replies: replies, Gate: alwaysGate}
	ev := make(chan Event, 4)

	done := make(chan struct {
		result ai.Message
		denied bool
	}, 1)
	go func() {
		_, result, denied := c.execTool(ctx, ai.ToolCall{
			ID: "c1", Name: tools.Bash, Arguments: `{"command":"echo hi"}`,
		}, ev)
		done <- struct {
			result ai.Message
			denied bool
		}{result, denied}
	}()

	_ = recvStart(t, ev)
	cancel()

	select {
	case got := <-done:
		if !got.denied || got.result.Text != "rejected: cancelled" {
			t.Errorf("denied=%v text=%q", got.denied, got.result.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestExecToolNilRepliesRuns(t *testing.T) {
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Gate: alwaysGate}
	ev := make(chan Event, 4)
	_, _, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"true"}`,
	}, ev)
	if denied {
		t.Fatal("nil Replies should skip gate")
	}
	recvKind(t, ev, KindToolStart)
}

func TestExecToolNilGateRuns(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Replies: replies}
	ev := make(chan Event, 4)
	_, _, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"true"}`,
	}, ev)
	if denied {
		t.Fatal("nil Gate should skip wait")
	}
	select {
	case <-replies:
		t.Fatal("should not consume reply")
	default:
	}
}

func TestExecToolGateFalseSkipsWait(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{
		Tools: tools.Build(), Root: t.TempDir(), Replies: replies,
		Gate: func(string) bool { return false },
	}
	ev := make(chan Event, 4)
	_, _, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"true"}`,
	}, ev)
	if denied {
		t.Fatal("Gate false should skip wait")
	}
	select {
	case <-replies:
		t.Fatal("should not consume reply")
	default:
	}
}

func TestExecToolReadOnlyStarts(t *testing.T) {
	c := Config{Tools: tools.Build(), Root: t.TempDir()}
	ev := make(chan Event, 4)
	_, _, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Read, Arguments: `{"path":"."}`,
	}, ev)
	if denied {
		t.Fatal("read should run")
	}
	recvKind(t, ev, KindToolStart)
}

func replyStart(t *testing.T, ev <-chan Event, replies chan<- Reply, allow bool) {
	t.Helper()
	_ = recvStart(t, ev)
	if allow {
		replies <- RunTool()
	} else {
		replies <- DenyTool()
	}
}

func recvStart(t *testing.T, ev <-chan Event) Event {
	t.Helper()
	select {
	case got := <-ev:
		if got.Kind != KindToolStart {
			t.Fatalf("start: %+v", got)
		}
		return got
	case <-time.After(time.Second):
		t.Fatal("expected KindToolStart")
	}
	return Event{}
}

func recvKind(t *testing.T, ev <-chan Event, want EventKind) {
	t.Helper()
	select {
	case got := <-ev:
		if got.Kind != want {
			t.Fatalf("kind=%v want %v", got.Kind, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected kind %v", want)
	}
}

func TestExecToolReplyResult(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Replies: replies, Gate: alwaysGate}
	ev := make(chan Event, 8)
	go func() {
		_ = recvStart(t, ev)
		replies <- InjectResult(`{"answers":{"q":"yes"}}`)
	}()
	_, result, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.AskUser, Arguments: `{"questions":[{"id":"q","header":"H","question":"Q?","options":[{"label":"a","description":"a"},{"label":"b","description":"b"}]}]}`,
	}, ev)
	if denied {
		t.Fatalf("denied: %q", result.Text)
	}
	if result.Text != `{"answers":{"q":"yes"}}` {
		t.Fatalf("result=%q", result.Text)
	}
}
