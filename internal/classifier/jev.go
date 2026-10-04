package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// JevURL is TypeSafe's System One endpoint.
const JevURL = "https://api.typesafe.ai/v1/systemone"

// Jev classifies with TypeSafe's Jev decision model. It returns a calibrated
// probability for every label, so the Reviewer can demand real confidence
// instead of trusting a self-reported one.
type Jev struct {
	Key  string
	URL  string // JevURL when empty
	HTTP *http.Client
}

func (Jev) Name() string { return "jev" }

// jevQuestionID names the one question each request asks.
const jevQuestionID = "risk"

// jevRequest and jevResponse follow TypeSafe's documented System One call: the
// text to judge as state, and a map of typed questions. A choice question picks
// one of its criteria and reports a probability for every one.
type jevRequest struct {
	Model     string                 `json:"model"`
	State     string                 `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type jevResponse struct {
	Answers map[string]struct {
		Choice        string             `json:"choice"`
		Confidence    float64            `json:"confidence"`
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"answers"`
}

func (j Jev) Classify(ctx context.Context, req Request) (Result, error) {
	criteria := make(map[string]string, len(labels))
	for _, e := range labels {
		criteria[string(e.Label)] = e.Desc
	}
	body, err := json.Marshal(jevRequest{
		Model: "jev-latest",
		State: req.Facts(),
		Questions: map[string]jevQuestion{
			jevQuestionID: {Type: "choice", Instructions: question, Criteria: criteria},
		},
	})
	if err != nil {
		return Result{}, err
	}
	url := j.URL
	if url == "" {
		url = JevURL
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+j.Key)
	client := j.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(hr)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode/100 != 2 {
		return Result{}, fmt.Errorf("jev: %s: %s", resp.Status, truncate(string(data), 200))
	}
	var out jevResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Result{}, fmt.Errorf("jev: unreadable reply: %w", err)
	}
	ans, ok := out.Answers[jevQuestionID]
	if !ok {
		return Result{}, fmt.Errorf("jev: no answer in reply")
	}
	label := Label(ans.Choice)
	if !Known(label) {
		return Result{}, fmt.Errorf("jev: unknown label %q", ans.Choice)
	}
	if _, ok := ans.Probabilities[ans.Choice]; !ok {
		return Result{}, fmt.Errorf("jev: no probability for %q", ans.Choice)
	}
	probs := make(map[Label]float64, len(ans.Probabilities))
	for l, p := range ans.Probabilities {
		if Known(Label(l)) {
			probs[Label(l)] = p
		}
	}
	return Result{Label: label, Probs: probs}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
