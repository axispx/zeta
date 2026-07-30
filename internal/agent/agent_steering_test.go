package agent

import (
	"context"
	"testing"
	"time"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/tools"
)

func doneAssistant(text string) <-chan ai.Event {
	ch := make(chan ai.Event, 2)
	ch <- ai.Event{Type: ai.EventDone, Message: ai.Message{Role: ai.RoleAssistant, Text: text}}
	close(ch)
	return ch
}

func TestTrySteerNilChannel(t *testing.T) {
	c := Config{}
	hist, ok := c.trySteer(context.Background(), nil, nil)
	if ok || hist != nil {
		t.Fatalf("ok=%v hist=%v", ok, hist)
	}
}

func TestTrySteerConsumesOne(t *testing.T) {
	steers := make(chan ai.Message, 1)
	steers <- ai.Message{Role: ai.RoleUser, Text: "steer me"}
	c := Config{Steers: steers}
	out := make(chan Event, 2)
	hist, ok := c.trySteer(context.Background(), nil, out)
	if !ok || len(hist) != 1 || hist[0].Text != "steer me" {
		t.Fatalf("hist=%+v ok=%v", hist, ok)
	}
	evt := <-out
	if evt.Kind != KindSteerAccepted || evt.Message.Text != "steer me" {
		t.Fatalf("evt=%+v", evt)
	}
}

func TestRunSteersAfterNoToolResponse(t *testing.T) {
	steers := make(chan ai.Message, 1)
	steers <- ai.Message{Role: ai.RoleUser, Text: "follow-up"}

	calls := 0
	c := Config{
		Steers: steers,
		StreamFn: func(_ context.Context, history []ai.Message, _ []ai.Tool) <-chan ai.Event {
			calls++
			if calls == 1 {
				return doneAssistant("first")
			}
			if len(history) < 3 || history[2].Text != "follow-up" {
				t.Fatalf("second call history=%+v", history)
			}
			return doneAssistant("second")
		},
	}

	ch := c.Run(context.Background(), []ai.Message{{Role: ai.RoleUser, Text: "start"}})
	var kinds []EventKind
	for evt := range ch {
		kinds = append(kinds, evt.Kind)
	}
	want := []EventKind{KindAssistant, KindSteerAccepted, KindAssistant, KindDone}
	if len(kinds) != len(want) {
		t.Fatalf("kinds=%v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds[%d]=%v want %v full=%v", i, kinds[i], want[i], kinds)
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestRunSteersAfterToolBatch(t *testing.T) {
	steers := make(chan ai.Message, 1)
	steers <- ai.Message{Role: ai.RoleUser, Text: "after tool"}

	calls := 0
	c := Config{
		Tools:  tools.Build(),
		Root:   t.TempDir(),
		Steers: steers,
		StreamFn: func(_ context.Context, _ []ai.Message, _ []ai.Tool) <-chan ai.Event {
			calls++
			if calls == 1 {
				ch := make(chan ai.Event, 2)
				ch <- ai.Event{
					Type: ai.EventDone,
					Message: ai.Message{
						Role: ai.RoleAssistant,
						ToolCalls: []ai.ToolCall{{
							ID:        "1",
							Name:      "bash",
							Arguments: `{"command":"true"}`,
						}},
					},
				}
				close(ch)
				return ch
			}
			return doneAssistant("done")
		},
	}

	ch := c.Run(context.Background(), []ai.Message{{Role: ai.RoleUser, Text: "go"}})
	var steerAccepted bool
	for evt := range ch {
		if evt.Kind == KindSteerAccepted {
			steerAccepted = true
			if evt.Message.Text != "after tool" {
				t.Fatalf("steer=%q", evt.Message.Text)
			}
		}
	}
	if !steerAccepted {
		t.Fatal("expected KindSteerAccepted")
	}
	if calls < 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestTrySteerRespectsCancel(t *testing.T) {
	steers := make(chan ai.Message)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := Config{Steers: steers}
	_, ok := c.trySteer(ctx, nil, nil)
	if ok {
		t.Fatal("expected false on cancelled ctx")
	}
}

func TestTrySteerEmptyDefault(t *testing.T) {
	steers := make(chan ai.Message)
	c := Config{Steers: steers}
	done := make(chan struct{})
	go func() {
		_, ok := c.trySteer(context.Background(), nil, nil)
		if ok {
			t.Error("unexpected steer")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("trySteer blocked")
	}
}
