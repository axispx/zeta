package compact

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/ai"
)

func TestEstimateWithImages(t *testing.T) {
	plain := ai.Message{Role: ai.RoleUser, Text: "hi"}
	with := ai.Message{Role: ai.RoleUser, Text: "hi", Images: []ai.Image{{URL: pngDataURL(t, 1024, 1024)}}}
	if Estimate([]ai.Message{with}) <= Estimate([]ai.Message{plain}) {
		t.Fatal("images should add tokens")
	}
	if got := imageTokens(with.Images[0]); got != 765 {
		t.Fatalf("1024x1024 tokens = %d, want 765", got)
	}
}

// A full-size base64 data URL must not be charged by the byte. Charging the
// encoded length reported ~274k tokens for one screenshot, which tripped
// auto-compaction and made the footer claim hundreds of percent of the window.
func TestImageTokensIgnoreFileSize(t *testing.T) {
	small := ai.Image{URL: pngDataURL(t, 1280, 800), MIME: "image/png"}
	big := ai.Image{URL: small.URL + strings.Repeat("A", 4<<20), MIME: "image/png"}
	// Same header, wildly different length: the tail is beyond the header read.
	if got, want := imageTokens(small), imageTokens(big); got != want {
		t.Fatalf("token estimate tracked payload size: %d vs %d", got, want)
	}
	if imageTokens(small) > 2_000 {
		t.Fatalf("one screenshot should be cheap: %d", imageTokens(small))
	}
}

func TestImageTokens(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		want int
	}{
		// OpenAI high detail: scale into 2048x2048, set the shortest side to
		// 768, then charge 85 + 170 per 512px tile.
		{"canonical square", 1024, 1024, 765}, // 768x768 → 2x2 tiles
		{"small upscales", 512, 512, 765},     // 768x768 → 2x2 tiles
		{"at the cap", 2048, 2048, 765},       // 768x768 → 2x2 tiles
		{"wide screenshot", 1280, 800, 1105},  // 1228x768 → 3x2 tiles
		{"4k", 3840, 2160, 1105},              // 1365x768 → 3x2 tiles
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := imageTokens(ai.Image{URL: pngDataURL(t, tt.w, tt.h), MIME: "image/png"})
			if got != tt.want {
				t.Fatalf("imageTokens(%dx%d) = %d, want %d", tt.w, tt.h, got, tt.want)
			}
		})
	}
}

// pngDataURL builds a minimal PNG header carrying the given dimensions. The
// token estimator reads only the signature and IHDR.
func pngDataURL(t *testing.T, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	b.Write([]byte{0, 0, 0, 13}) // IHDR chunk length
	b.WriteString("IHDR")
	for _, v := range []uint32{uint32(w), uint32(h)} {
		if err := binary.Write(&b, binary.BigEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	b.Write([]byte{8, 6, 0, 0, 0}) // bit depth, color type, compression, filter, interlace
	b.Write([]byte{0, 0, 0, 0})    // CRC (not validated here)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestImageTokensFallback(t *testing.T) {
	for _, tt := range []struct {
		name string
		url  string
	}{
		{"unknown format", "data:image/tiff;base64,QUJD"},
		{"truncated payload", "data:image/png;base64,iVBORw0KGgo"},
		{"not a data url", "https://example.com/a.png"},
		{"empty", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageTokens(ai.Image{URL: tt.url}); got != imageTokensFallback {
				t.Fatalf("imageTokens(%q) = %d, want fallback %d", tt.url, got, imageTokensFallback)
			}
		})
	}
	// A fallback is not free: an unreadable image must never estimate as 0.
	if imageTokensFallback <= 0 {
		t.Fatal("fallback must be positive")
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("empty: %d", got)
	}
	if got := EstimateTokens("ab"); got != 1 { // 2 runes → min 1
		t.Fatalf("short: %d", got)
	}
	if got := EstimateTokens(strings.Repeat("x", 40)); got != 10 {
		t.Fatalf("40 chars: %d", got)
	}
	// multibyte
	if got := EstimateTokens("你好世界"); got != 1 { // 4 runes
		t.Fatalf("unicode: %d", got)
	}
}

func TestNeeded(t *testing.T) {
	// Multi-turn so Select has a freeable head when over budget.
	old := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("word ", 2000)}
	recent := ai.Message{Role: ai.RoleUser, Text: "recent"}
	hist := []ai.Message{old, recent}
	est := Estimate(hist)

	if Needed(nil, Config{ContextWindow: 1000}) {
		t.Fatal("empty history should not need compact")
	}
	if Needed(hist, Config{ContextWindow: 0}) {
		t.Fatal("zero window should not need compact")
	}
	if Needed(hist, Config{ContextWindow: est + DefaultBuffer + 100}) {
		t.Fatalf("under budget should not need compact (est=%d)", est)
	}
	if !Needed(hist, Config{ContextWindow: est + DefaultBuffer - 1, Keep: estimateMsg(recent) + 5}) {
		t.Fatalf("over budget with freeable head should need compact (est=%d)", est)
	}
	// overhead pushes over
	if !Needed(hist, Config{ContextWindow: est + DefaultBuffer + 50, Overhead: 100, Keep: estimateMsg(recent) + 5}) {
		t.Fatal("overhead should push over budget")
	}
	// Single oversized turn: over budget but nothing freeable.
	huge := []ai.Message{{Role: ai.RoleUser, Text: strings.Repeat("word ", 50_000)}}
	if Needed(huge, Config{ContextWindow: 8_000}) {
		t.Fatal("single oversized turn should not need compact")
	}
}

func TestUsedTokensPrefersMeasurement(t *testing.T) {
	// Two turns so the history has real content to estimate.
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("x", 4_000)}, // ~1000 est tokens
		{Role: ai.RoleAssistant, Text: "ok"},
	}

	// No measurement: estimate everything, envelope included.
	cfg := Config{Overhead: 500}
	if got, want := usedTokens(hist, cfg), 500+Estimate(hist); got != want {
		t.Fatalf("unmeasured usedTokens = %d, want %d", got, want)
	}

	// With a measurement, the prefix it covers is exact and only the tail is
	// estimated.
	cfg.Measured, cfg.MeasuredMsgs = 90_000, len(hist)
	if got, want := usedTokens(hist, cfg), 90_000; got != want {
		t.Fatalf("measured usedTokens = %d, want %d", got, want)
	}

	// Appending after the measurement adds only the new messages.
	grown := append(append([]ai.Message{}, hist...), ai.Message{Role: ai.RoleUser, Text: strings.Repeat("y", 400)})
	cfg.MeasuredMsgs = len(hist)
	if got, want := usedTokens(grown, cfg), 90_000+Estimate(grown[len(hist):]); got != want {
		t.Fatalf("grown usedTokens = %d, want %d", got, want)
	}
	// ...and measuring the grown history instead is exact.
	cfg.MeasuredMsgs = len(grown)
	if got := usedTokens(grown, cfg); got != 90_000 {
		t.Fatalf("re-measured usedTokens = %d, want 90000", got)
	}
}

// compactable returns a history with an old turn that Select can summarize and
// a small recent turn it keeps, plus the Keep budget that produces that split.
// Needed is only true when a head is actually freeable, so every budget-check
// test needs a history shaped like this.
func compactable() (hist []ai.Message, keep int) {
	recent := ai.Message{Role: ai.RoleUser, Text: "recent"}
	hist = []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("old ", 500)},
		{Role: ai.RoleAssistant, Text: "ok"},
		recent,
	}
	return hist, estimateMsg(recent) + 5
}

// The measurement is the provider's count for a prompt that already contained
// the envelope, so adding Overhead again would double-count it.
func TestUsedTokensMeasurementIncludesEnvelope(t *testing.T) {
	hist, keep := compactable()
	budget := 100_000 - DefaultBuffer // 80_000
	cfg := Config{
		ContextWindow: 100_000,
		Overhead:      2_000,
		Keep:          keep,
		Measured:      budget - 1, // just under
		MeasuredMsgs:  len(hist),
	}
	if got := usedTokens(hist, cfg); got != budget-1 {
		t.Fatalf("usedTokens = %d, want %d (Overhead must not be added)", got, budget-1)
	}
	if Needed(hist, cfg) {
		t.Fatal("a measurement just under budget must not compact")
	}
	// One token more and it must.
	cfg.Measured = budget + 1
	if !Needed(hist, cfg) {
		t.Fatal("a measurement over budget must compact")
	}
}

// The dangerous direction: chars/4 under-counts source and tool output, so a
// history that looks like it fits can still blow the window. A provider
// measurement has to win over the estimate in both directions.
func TestNeededTrustsMeasurementOverEstimate(t *testing.T) {
	hist, keep := compactable()

	// Estimate says it fits comfortably...
	if cfg := (Config{ContextWindow: 100_000, Keep: keep}); Needed(hist, cfg) {
		t.Fatal("small history should not need compact")
	}
	// ...but the provider says the last request was nearly the whole window.
	cfg := Config{ContextWindow: 100_000, Keep: keep, Measured: 95_000, MeasuredMsgs: len(hist)}
	if !Needed(hist, cfg) {
		t.Fatal("measurement should override an under-counting estimate")
	}

	// And the reverse: a history that looks huge but measured small.
	big := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("x", 400_000)} // ~100k est tokens
	huge := []ai.Message{big, {Role: ai.RoleAssistant, Text: "ok"}, hist[len(hist)-1]}
	if !Needed(huge, Config{ContextWindow: 100_000, Keep: keep}) {
		t.Fatal("huge estimate should need compact")
	}
	cfg = Config{ContextWindow: 100_000, Keep: keep, Measured: 5_000, MeasuredMsgs: len(huge)}
	if Needed(huge, cfg) {
		t.Fatal("measurement should override an over-counting estimate")
	}
}

// A measurement that predates a shrink (compaction, rewind) describes content
// the history no longer holds, so it must not be trusted.
func TestUsedTokensIgnoresStaleMeasurement(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "short"}}
	cfg := Config{Overhead: 100, Measured: 90_000, MeasuredMsgs: 40} // 40 > len(hist)
	if got, want := usedTokens(hist, cfg), 100+Estimate(hist); got != want {
		t.Fatalf("stale measurement usedTokens = %d, want fallback %d", got, want)
	}

	// A measurement of zero messages is not usable either.
	cfg = Config{Overhead: 100, Measured: 90_000, MeasuredMsgs: 0}
	if got, want := usedTokens(hist, cfg), 100+Estimate(hist); got != want {
		t.Fatalf("empty-span measurement usedTokens = %d, want fallback %d", got, want)
	}

	// Zero tokens means the provider reported nothing.
	cfg = Config{Overhead: 100, Measured: 0, MeasuredMsgs: 1}
	if got, want := usedTokens(hist, cfg), 100+Estimate(hist); got != want {
		t.Fatalf("absent measurement usedTokens = %d, want fallback %d", got, want)
	}
}

func TestIsCheckpointPrefix(t *testing.T) {
	cp := CheckpointMessage("sum")
	if !IsCheckpoint(cp) {
		t.Fatal("expected checkpoint")
	}
	// Tag only mid-message must not match.
	fake := ai.Message{Role: ai.RoleUser, Text: "see " + checkpointOpen + " later"}
	if IsCheckpoint(fake) {
		t.Fatal("mid-text tag should not be checkpoint")
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	sum := "## Task\n- ship compaction"
	m := CheckpointMessage(sum)
	if m.Role != ai.RoleUser {
		t.Fatalf("role: %s", m.Role)
	}
	if !IsCheckpoint(m) {
		t.Fatal("expected checkpoint")
	}
	got, ok := ParseSummary(m)
	if !ok || got != sum {
		t.Fatalf("parse: ok=%v got=%q", ok, got)
	}
	// empty summary still checkpoint
	m2 := CheckpointMessage("  ")
	if s, ok := ParseSummary(m2); !ok || s != "(no summary available)" {
		t.Fatalf("empty summary: %q ok=%v", s, ok)
	}
}

func TestSelectKeepsRecentTail(t *testing.T) {
	// Three messages with known sizes; keep only the last.
	mk := func(s string) ai.Message {
		return ai.Message{Role: ai.RoleUser, Text: s}
	}
	// Each ~25 tokens of text (+ framing)
	a := mk(strings.Repeat("aaaa ", 20))
	b := mk(strings.Repeat("bbbb ", 20))
	c := mk(strings.Repeat("cccc ", 20))
	hist := []ai.Message{a, b, c}

	// keep budget that fits only c
	keep := estimateMsg(c)
	sp := Select(hist, keep)
	if len(sp.Tail) != 1 || sp.Tail[0].Text != c.Text {
		t.Fatalf("tail=%v", msgsText(sp.Tail))
	}
	if len(sp.Head) != 2 {
		t.Fatalf("head len=%d want 2", len(sp.Head))
	}
}

func TestSelectPeelsPreviousCheckpoint(t *testing.T) {
	cp := CheckpointMessage("old summary")
	u := ai.Message{Role: ai.RoleUser, Text: "hello"}
	a := ai.Message{Role: ai.RoleAssistant, Text: "hi"}
	sp := Select([]ai.Message{cp, u, a}, 1_000_000)
	if sp.PreviousSummary != "old summary" {
		t.Fatalf("prev=%q", sp.PreviousSummary)
	}
	if len(sp.Head) != 0 {
		t.Fatalf("everything should fit tail, head=%d", len(sp.Head))
	}
	if len(sp.Tail) != 2 {
		t.Fatalf("tail=%d", len(sp.Tail))
	}
}

func TestSelectSnapsToUserTurn(t *testing.T) {
	// Budget would only fit the last assistant if we split by message — user-turn
	// snap must pull in the matching user message (and not the prior turn).
	oldU := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("old ", 500)}
	oldA := ai.Message{
		Role: ai.RoleAssistant,
		Text: "calling",
		ToolCalls: []ai.ToolCall{
			{ID: "1", Name: "read", Arguments: `{"path":"a.go"}`},
		},
	}
	oldT := ai.Message{Role: ai.RoleTool, ToolCallID: "1", Text: "file contents here"}
	newU := ai.Message{Role: ai.RoleUser, Text: "recent question"}
	newA := ai.Message{Role: ai.RoleAssistant, Text: "recent answer"}

	keep := estimateMsg(newU) + estimateMsg(newA)
	sp := Select([]ai.Message{oldU, oldA, oldT, newU, newA}, keep)
	if len(sp.Tail) != 2 {
		t.Fatalf("tail len=%d roles=%v", len(sp.Tail), roles(sp.Tail))
	}
	if sp.Tail[0].Role != ai.RoleUser || sp.Tail[0].Text != newU.Text {
		t.Fatalf("tail should start at user turn: %+v", sp.Tail[0])
	}
	if sp.Tail[1].Text != newA.Text {
		t.Fatalf("tail assistant: %+v", sp.Tail[1])
	}
	if len(sp.Head) != 3 || sp.Head[0].Role != ai.RoleUser {
		t.Fatalf("head: %v", roles(sp.Head))
	}
}

func TestSelectKeepsOversizedNewestTurn(t *testing.T) {
	// One user turn larger than keep — still keep it whole (no mid-turn cut).
	u := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("q ", 200)}
	a := ai.Message{
		Role: ai.RoleAssistant,
		ToolCalls: []ai.ToolCall{
			{ID: "1", Name: "read", Arguments: `{}`},
		},
	}
	tool := ai.Message{Role: ai.RoleTool, ToolCallID: "1", Text: strings.Repeat("out ", 200)}
	keep := 10 // tiny
	sp := Select([]ai.Message{u, a, tool}, keep)
	if len(sp.Head) != 0 {
		t.Fatalf("head should be empty, got %v", roles(sp.Head))
	}
	if len(sp.Tail) != 3 || sp.Tail[0].Role != ai.RoleUser {
		t.Fatalf("tail=%v", roles(sp.Tail))
	}
}

func TestSelectNoUserMessages(t *testing.T) {
	a := ai.Message{Role: ai.RoleAssistant, Text: "orphan"}
	sp := Select([]ai.Message{a}, 100)
	if len(sp.Head) != 0 || len(sp.Tail) != 1 {
		t.Fatalf("head=%v tail=%v", roles(sp.Head), roles(sp.Tail))
	}
}

func TestBuildPromptIncludesPrevious(t *testing.T) {
	head := []ai.Message{{Role: ai.RoleUser, Text: "fix the bug"}}
	msgs := BuildPrompt(nil, head, "prior summary text")
	if len(msgs) != 2 {
		t.Fatalf("len=%d", len(msgs))
	}
	// The head goes in verbatim, not serialized: only a byte-identical prefix
	// can be served from the conversation's cache.
	if msgs[0].Role != ai.RoleUser || msgs[0].Text != "fix the bug" {
		t.Fatalf("head message altered: %+v", msgs[0])
	}
	if !strings.Contains(msgs[1].Text, "<previous-summary>") {
		t.Fatal("missing previous-summary")
	}
	if !strings.Contains(msgs[1].Text, "prior summary text") {
		t.Fatal("missing previous body")
	}
	if !strings.Contains(msgs[1].Text, "## Task") {
		t.Fatal("missing template")
	}
}

func TestBuildPromptFresh(t *testing.T) {
	msgs := BuildPrompt(nil, []ai.Message{{Role: ai.RoleUser, Text: "hi"}}, "")
	// The instruction body mentions the tag; only the revision block opens one.
	if strings.Contains(msgs[1].Text, "<previous-summary>\n") {
		t.Fatal("should not include previous-summary")
	}
	if !strings.Contains(msgs[1].Text, "handoff note") {
		t.Fatal("missing instruction")
	}
}

// The summarizer's prefix must be byte-identical to the live conversation's,
// which is what makes the provider serve the head from cache.
func TestBuildPromptReusesPrefix(t *testing.T) {
	prefix := []ai.Message{
		{Role: ai.RoleSystem, Text: "You are zeta."},
		{Role: ai.RoleDeveloper, Text: "<agent_mode>"},
	}
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: "do a thing"},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "again"},
	}
	// A live turn is prefix + history + trailing blocks.
	live := append(append([]ai.Message{}, prefix...), hist...)
	live = append(live, ai.Message{Role: ai.RoleDeveloper, Text: "# Environment"})

	// Compaction summarizes the head only; its request must be a prefix of it.
	head := hist[:2]
	got := BuildPrompt(prefix, head, "")
	if len(got) != len(prefix)+len(head)+1 {
		t.Fatalf("len=%d", len(got))
	}
	for i := range got[:len(prefix)+len(head)] {
		if got[i].Role != live[i].Role || got[i].Text != live[i].Text {
			t.Fatalf("message %d diverges from the live request:\n got %+v\nlive %+v", i, got[i], live[i])
		}
	}
	if got[len(got)-1].Role != ai.RoleUser {
		t.Fatalf("instruction should be a trailing user message: %s", got[len(got)-1].Role)
	}
}

// The Tools on Config.Prefix must reach the completer unchanged: providers
// fold the tool array into the cached prefix, so a rewritten list forfeits it.
func TestRunForcedPassesPrefixTools(t *testing.T) {
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("old ", 400)},
		{Role: ai.RoleUser, Text: "recent"},
	}
	tools := []ai.Tool{{Name: "read", Description: "Read a file"}}
	stub := &stubCompleter{text: "## Task\n- done"}
	_, err := RunForced(context.Background(), stub, hist, Config{
		Keep:   estimateMsg(hist[1]) + 20,
		Prefix: Prefix{Messages: []ai.Message{{Role: ai.RoleSystem, Text: "sys"}}, Tools: tools},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.gotTools) != 1 || stub.gotTools[0].Name != "read" {
		t.Fatalf("tools not forwarded: %+v", stub.gotTools)
	}
	if stub.got[0].Text != "sys" {
		t.Fatalf("prefix messages not forwarded: %+v", stub.got[0])
	}
}

type stubCompleter struct {
	text      string
	err       error
	got       []ai.Message
	gotTools  []ai.Tool
	maxTokens int64
}

func (s *stubCompleter) Complete(_ context.Context, msgs []ai.Message, tools []ai.Tool, maxTokens int64) (string, error) {
	s.got = msgs
	s.gotTools = tools
	s.maxTokens = maxTokens
	return s.text, s.err
}

func TestRunForcedCompacts(t *testing.T) {
	// RunForced even when under budget; window optional.
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("old stuff ", 400)},
		{Role: ai.RoleAssistant, Text: "ok"},
		{Role: ai.RoleUser, Text: "recent"},
	}
	stub := &stubCompleter{text: "## Task\n- done"}
	res, err := RunForced(context.Background(), stub, hist, Config{
		Keep: estimateMsg(hist[2]) + 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compacted {
		t.Fatal("expected compacted")
	}
	if res.TailCount != len(res.History)-1 {
		t.Fatalf("TailCount=%d want %d", res.TailCount, len(res.History)-1)
	}
	if !IsCheckpoint(res.History[0]) {
		t.Fatalf("first msg not checkpoint: %q", res.History[0].Text)
	}
	if sum, ok := ParseSummary(res.History[0]); !ok || !strings.Contains(sum, "Task") {
		t.Fatalf("summary: %q", sum)
	}
	// tail should keep the recent user message
	found := false
	for _, m := range res.History[1:] {
		if m.Text == "recent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recent missing from tail: %+v", res.History)
	}
	if stub.got == nil {
		t.Fatal("completer not called")
	}
	if stub.maxTokens != int64(SummaryMaxTokens) {
		t.Fatalf("maxTokens=%d want %d", stub.maxTokens, SummaryMaxTokens)
	}
}

func TestRunIfNeededSkipsWhenNotNeeded(t *testing.T) {
	hist := []ai.Message{{Role: ai.RoleUser, Text: "hi"}}
	stub := &stubCompleter{text: "should not run"}
	res, err := RunIfNeeded(context.Background(), stub, hist, Config{ContextWindow: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	if res.Compacted {
		t.Fatal("should not compact")
	}
	if stub.got != nil {
		t.Fatal("completer should not be called")
	}
	if len(res.History) != 1 || res.History[0].Text != "hi" {
		t.Fatalf("history mutated: %+v", res.History)
	}
}

func TestRunForcedPropagatesCompleterError(t *testing.T) {
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("x", 200)},
		{Role: ai.RoleUser, Text: "tail"},
	}
	stub := &stubCompleter{err: errors.New("boom")}
	_, err := RunForced(context.Background(), stub, hist, Config{Keep: 10})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunForcedEmptySummary(t *testing.T) {
	hist := []ai.Message{
		{Role: ai.RoleUser, Text: strings.Repeat("x", 200)},
		{Role: ai.RoleUser, Text: "tail"},
	}
	stub := &stubCompleter{text: "   "}
	_, err := RunForced(context.Background(), stub, hist, Config{Keep: 10})
	if err == nil || !strings.Contains(err.Error(), "empty summary") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunForcedNothingToCompact(t *testing.T) {
	// everything fits in keep budget
	hist := []ai.Message{{Role: ai.RoleUser, Text: "tiny"}}
	stub := &stubCompleter{text: "sum"}
	res, err := RunForced(context.Background(), stub, hist, Config{Keep: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	if res.Compacted {
		t.Fatal("no head → no compact")
	}
}

func TestRunForcedMergesPreviousSummary(t *testing.T) {
	cp := CheckpointMessage("## Task\n- old goal")
	// force head non-empty: large middle message, tiny tail keep
	mid := ai.Message{Role: ai.RoleUser, Text: strings.Repeat("middle ", 300)}
	tail := ai.Message{Role: ai.RoleUser, Text: "now"}
	hist := []ai.Message{cp, mid, tail}
	stub := &stubCompleter{text: "## Task\n- new goal"}
	res, err := RunForced(context.Background(), stub, hist, Config{
		Keep: estimateMsg(tail) + 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Compacted {
		t.Fatal("expected compact")
	}
	if stub.got == nil || !strings.Contains(stub.got[1].Text, "old goal") {
		t.Fatalf("prompt should include previous summary: %v", stub.got)
	}
}

func msgsText(msgs []ai.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text
	}
	return out
}

func roles(msgs []ai.Message) []ai.Role {
	out := make([]ai.Role, len(msgs))
	for i, m := range msgs {
		out[i] = m.Role
	}
	return out
}
