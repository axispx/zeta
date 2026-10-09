package compact

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/session"
)

type nativeStub struct {
	item  ai.Compaction
	err   error
	calls int
	got   []ai.Message
	tools []ai.Tool
}

func (n *nativeStub) CompactNative(_ context.Context, msgs []ai.Message, tools []ai.Tool) (ai.Compaction, ai.Usage, error) {
	n.calls++
	n.got, n.tools = msgs, tools
	return n.item, ai.Usage{TotalTokens: 1_234}, n.err
}

func nativeHistory() []ai.Message {
	h := []ai.Message{{Role: ai.RoleUser, Text: "refactor the loop"}}
	h = append(h, longTurn(6)[1:]...)
	return append(h,
		ai.Message{Role: ai.RoleUser, Text: "and add tests"},
		ai.Message{Role: ai.RoleAssistant, Text: "on it"},
	)
}

// The provider's checkpoint stands for everything but the user's own words.
func TestRunNativeReplacesHistory(t *testing.T) {
	stub := &nativeStub{item: ai.Compaction{Content: "ENC", Model: "m"}}
	prefix := []ai.Message{{Role: ai.RoleSystem, Text: "sys"}}
	tools := []ai.Tool{{Name: "read"}}
	hist := nativeHistory()

	res, err := RunNative(context.Background(), stub, hist, Config{Prefix: Prefix{Messages: prefix, Tools: tools}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compacted || res.Native == nil || res.Native.Content != "ENC" {
		t.Fatalf("compacted=%v native=%+v", res.Compacted, res.Native)
	}
	if res.Usage.TotalTokens != 1_234 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	want := []string{"user|refactor the loop", "user|and add tests", "compaction|"}
	var got []string
	for _, m := range res.History {
		got = append(got, string(m.Role)+"|"+m.Text)
	}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("history = %v, want %v", got, want)
	}
	// Live-shaped: the conversation's own prefix and tools, then the history.
	if stub.got[0].Text != "sys" || len(stub.got) != 1+len(hist) || len(stub.tools) != 1 {
		t.Fatalf("request = %d messages, %d tools", len(stub.got), len(stub.tools))
	}
}

func TestRunNativeSkipsWhenWithinBudget(t *testing.T) {
	stub := &nativeStub{item: ai.Compaction{Content: "ENC", Model: "m"}}
	hist := []ai.Message{{Role: ai.RoleUser, Text: "hi"}, {Role: ai.RoleAssistant, Text: "yo"}}
	res, err := RunNative(context.Background(), stub, hist, Config{ContextWindow: 128_000}, false)
	if err != nil || res.Compacted || stub.calls != 0 {
		t.Fatalf("compacted=%v err=%v calls=%d", res.Compacted, err, stub.calls)
	}
	// A history over the budget compacts with no head to find: one long turn.
	big := longTurn(30)
	res, err = RunNative(context.Background(), stub, big, Config{ContextWindow: 30_000, Buffer: 2_000}, false)
	if err != nil || !res.Compacted || stub.calls != 1 {
		t.Fatalf("compacted=%v err=%v calls=%d", res.Compacted, err, stub.calls)
	}
}

// A history past the window is sent with its oldest big tool results elided;
// the durable history is left alone.
func TestRunNativeElidesOldToolOutputToFit(t *testing.T) {
	stub := &nativeStub{item: ai.Compaction{Content: "ENC", Model: "m"}}
	hist := longTurn(30) // ~38k tokens
	before := Estimate(hist)

	_, err := RunNative(context.Background(), stub, hist, Config{ContextWindow: 24_000, Buffer: 2_000}, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := Estimate(stub.got); got > 24_000 {
		t.Fatalf("request is %d tokens, over the 24000 window", got)
	}
	var elided, intact int
	firstIntact := -1
	for i, m := range stub.got {
		if m.Role != ai.RoleTool {
			continue
		}
		if m.Text == elidedOutput {
			elided++
			if firstIntact >= 0 {
				t.Fatalf("message %d was elided after an intact one: elision must go oldest-first", i)
			}
		} else {
			intact++
			if firstIntact < 0 {
				firstIntact = i
			}
		}
	}
	if elided == 0 || intact == 0 {
		t.Fatalf("elided=%d intact=%d, want some of each", elided, intact)
	}
	if Estimate(hist) != before {
		t.Fatal("the durable history was modified")
	}
}

// A checkpoint that does not shrink the history is not worth installing.
func TestRunNativeDiscardsNonShrinkingResult(t *testing.T) {
	stub := &nativeStub{item: ai.Compaction{Content: strings.Repeat("x", 4_000), Model: "m"}}
	hist := []ai.Message{{Role: ai.RoleUser, Text: "short"}, {Role: ai.RoleAssistant, Text: "ok"}}
	res, err := RunNative(context.Background(), stub, hist, Config{}, true)
	if err != nil || res.Compacted || len(res.History) != len(hist) {
		t.Fatalf("compacted=%v len=%d err=%v", res.Compacted, len(res.History), err)
	}
}

func TestRunNativeErrors(t *testing.T) {
	boom := errors.New("boom")
	_, err := RunNative(context.Background(), &nativeStub{err: boom}, nativeHistory(), Config{}, true)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	_, err = RunNative(context.Background(), &nativeStub{}, nativeHistory(), Config{}, true)
	if err == nil || !strings.Contains(err.Error(), "empty checkpoint") {
		t.Fatalf("err = %v", err)
	}
}

func TestRetainUsers(t *testing.T) {
	hist := []ai.Message{
		CheckpointMessage("## Task\n- earlier"),
		{Role: ai.RoleUser, Text: strings.Repeat("old ", 30_000)}, // ~30k, past the budget on its own
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "recent one"},
		{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{ID: "1", Name: "read", Arguments: `{}`}}},
		{Role: ai.RoleTool, ToolCallID: "1", Text: "out"},
		{Role: ai.RoleUser, Text: "recent two"},
	}
	got := RetainUsers(hist)
	if len(got) != 2 || got[0].Text != "recent one" || got[1].Text != "recent two" {
		t.Fatalf("retained = %+v", got)
	}
	// The newest message stays even when it alone is over the budget.
	huge := []ai.Message{{Role: ai.RoleUser, Text: strings.Repeat("big ", 40_000)}}
	if got := RetainUsers(huge); len(got) != 1 {
		t.Fatalf("retained = %d", len(got))
	}
}

// The log rebuilds a native compaction as the live history was built, for the
// model that made it; for any other it leaves the covered turns raw.
func TestRebuildNativeCompaction(t *testing.T) {
	log := []session.Record{
		{Role: session.RoleUser, Text: "first"},
		{Role: session.RoleAgent, Text: "ok"},
		{Role: session.RoleUser, Text: "second"},
		{Role: session.RoleCompact, Native: "ENC", NativeModel: "gpt-5.5"},
		{Role: session.RoleUser, Text: "third"},
	}
	own := RebuildAPIHistory(log, "gpt-5.5")
	var roles []string
	for _, m := range own {
		roles = append(roles, string(m.Role))
	}
	if strings.Join(roles, ",") != "user,user,compaction,user" {
		t.Fatalf("own = %v", roles)
	}
	if c := own[2].Compaction; c == nil || c.Content != "ENC" || c.Model != "gpt-5.5" {
		t.Fatalf("checkpoint = %+v", c)
	}

	for _, model := range []string{"", "other-model"} {
		raw := RebuildAPIHistory(log, model)
		if len(raw) != 4 || raw[0].Text != "first" || raw[3].Text != "third" {
			t.Fatalf("model %q: history = %+v", model, raw)
		}
		for _, m := range raw {
			if m.Role == ai.RoleCompaction {
				t.Fatalf("model %q was handed a checkpoint it cannot read", model)
			}
		}
	}
}

// An opaque checkpoint is still counted: the next budget check must not read a
// 40k-token item as free.
func TestEstimateCountsCheckpointContent(t *testing.T) {
	m := ai.Message{Role: ai.RoleCompaction, Compaction: &ai.Compaction{Content: strings.Repeat("x", 40_000)}}
	if got := estimateMsg(m); got < 9_000 {
		t.Fatalf("estimate = %d", got)
	}
}
