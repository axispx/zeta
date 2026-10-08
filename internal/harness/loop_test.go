package harness

import (
	"context"
	"encoding/json"
	"path/filepath"
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

func alwaysGate(string, json.RawMessage) bool { return true }

func TestGateReceivesArgs(t *testing.T) {
	replies := make(chan Reply, 1)
	var gotName string
	var gotArgs json.RawMessage
	c := Config{
		Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies),
		Gate: func(name string, args json.RawMessage) bool {
			gotName, gotArgs = name, args
			return true
		},
	}
	ev := make(chan Event, 4)
	go replyStart(t, ev, replies, false)
	_, _, _ = c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"echo hi"}`,
	}, ev)
	if gotName != tools.Bash || !json.Valid(gotArgs) || tools.ArgCommand(gotArgs) != "echo hi" {
		t.Fatalf("gate name=%q args=%s", gotName, gotArgs)
	}
}

func TestExecToolPolicyDenyReason(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies), Gate: alwaysGate}
	ev := make(chan Event, 4)
	go func() {
		_ = recvStart(t, ev)
		replies <- DenyToolReason("denied by permission policy")
	}()
	_, result, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"echo hi"}`,
	}, ev)
	if !denied || result.Text != "rejected: denied by permission policy" {
		t.Errorf("denied=%v text=%q", denied, result.Text)
	}
}

func TestExecToolGateDeny(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies), Gate: alwaysGate}
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
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies), Gate: alwaysGate}
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
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies), Gate: alwaysGate}
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
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies), Gate: alwaysGate}
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

func TestExecToolNilDeciderRuns(t *testing.T) {
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Gate: alwaysGate}
	ev := make(chan Event, 4)
	_, _, denied := c.execTool(t.Context(), ai.ToolCall{
		ID: "c1", Name: tools.Bash, Arguments: `{"command":"true"}`,
	}, ev)
	if denied {
		t.Fatal("nil Decider should skip gate")
	}
	recvKind(t, ev, KindToolStart)
}

func TestExecToolNilGateRuns(t *testing.T) {
	replies := make(chan Reply, 1)
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies)}
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
		Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies),
		Gate: func(string, json.RawMessage) bool { return false },
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
	c := Config{Tools: tools.Build(), Root: t.TempDir(), Decider: chanDecider(replies), Gate: alwaysGate}
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

// chanDecider turns a reply channel into a Decider for tests.
type chanDecider chan Reply

func (d chanDecider) Decide(ctx context.Context, _ Request) (Reply, error) {
	select {
	case r := <-d:
		return r, nil
	case <-ctx.Done():
		return Reply{}, ctx.Err()
	}
}

// scriptedStream answers each request with the next assistant message and
// records the request it saw.
func scriptedStream(replies []ai.Message, seen *[][]ai.Message) func(context.Context, []ai.Message, []ai.Tool) <-chan ai.Event {
	i := 0
	return func(_ context.Context, history []ai.Message, _ []ai.Tool) <-chan ai.Event {
		*seen = append(*seen, append([]ai.Message(nil), history...))
		ch := make(chan ai.Event, 1)
		ch <- ai.Event{Type: ai.EventDone, Message: replies[i]}
		i++
		close(ch)
		return ch
	}
}

func collect(c Config, history []ai.Message) []Event {
	var got []Event
	for e := range c.Run(context.Background(), history) {
		got = append(got, e)
	}
	return got
}

func TestSteerJoinsTurnAtToolBoundary(t *testing.T) {
	var seen [][]ai.Message
	steers := [][]ai.Message{{{Role: ai.RoleUser, Text: "nudge"}}}
	c := Config{
		Tools: tools.Build(), Root: t.TempDir(),
		StreamFn: scriptedStream([]ai.Message{
			{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "c1", Name: tools.Bash, Arguments: `{"command":"true"}`}}},
			{Role: ai.RoleAssistant, Text: "done"},
		}, &seen),
		Steer: func() []ai.Message {
			if len(steers) == 0 {
				return nil
			}
			s := steers[0]
			steers = steers[1:]
			return s
		},
	}
	var kinds []EventKind
	for _, e := range collect(c, []ai.Message{{Role: ai.RoleUser, Text: "go"}}) {
		kinds = append(kinds, e.Kind)
	}
	if len(seen) != 2 {
		t.Fatalf("requests=%d", len(seen))
	}
	last := seen[1][len(seen[1])-1]
	if last.Role != ai.RoleUser || last.Text != "nudge" {
		t.Fatalf("second request tail = %+v", last)
	}
	steerAt := -1
	for i, k := range kinds {
		if k == KindSteer {
			steerAt = i
		}
	}
	if steerAt < 0 || kinds[steerAt-1] != KindTool {
		t.Fatalf("kinds=%v", kinds)
	}
}

func TestSteerKeepsTurnGoingAfterFinalAnswer(t *testing.T) {
	var seen [][]ai.Message
	steers := [][]ai.Message{{{Role: ai.RoleUser, Text: "one more thing"}}}
	c := Config{
		StreamFn: scriptedStream([]ai.Message{
			{Role: ai.RoleAssistant, Text: "first"},
			{Role: ai.RoleAssistant, Text: "second"},
		}, &seen),
		Steer: func() []ai.Message {
			if len(steers) == 0 {
				return nil
			}
			s := steers[0]
			steers = steers[1:]
			return s
		},
	}
	collect(c, []ai.Message{{Role: ai.RoleUser, Text: "go"}})
	if len(seen) != 2 {
		t.Fatalf("requests=%d, want 2", len(seen))
	}
}

func TestToolLabelEditShowsWorkspacePath(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "internal", "x.go")
	cases := []struct {
		name, args, want string
	}{
		{tools.Edit, `{"path":"` + abs + `","old_string":"a","new_string":"b"}`, "edit internal/x.go"},
		{tools.Edit, `{"path":"internal/x.go","old_string":"","new_string":"b"}`, "create internal/x.go"},
		{tools.Write, `{"path":"` + abs + `","content":"b"}`, "write internal/x.go"},
		{tools.Edit, `{"path":"/etc/hosts","old_string":"a","new_string":"b"}`, "edit /etc/hosts"},
	}
	for _, tc := range cases {
		if got := toolLabel(tools.Build(), root, tc.name, json.RawMessage(tc.args)); got != tc.want {
			t.Errorf("toolLabel(%s) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
