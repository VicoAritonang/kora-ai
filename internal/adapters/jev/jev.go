// Package jev adapts the JEV decision model (TypeSafe AI), reached through
// OpenRouter's decisions endpoint (POST /api/alpha/decisions), to
// ports.Decider. The version is pinned; "latest" is refused.
package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// MaxStateChars keeps the state well under JEV's 32k-token context.
const MaxStateChars = 60_000

// Client is the JEV adapter.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *platform.HTTPClient
}

// New returns a client for a pinned model.
func New(baseURL, apiKey, model string, timeout time.Duration) (*Client, error) {
	if strings.Contains(model, "latest") {
		return nil, fmt.Errorf("jev: model %q must be pinned to a version", model)
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model, HTTP: platform.NewHTTPClient("jev", timeout)}, nil
}

type wireQuestion struct {
	Type         string `json:"type"`
	Criteria     any    `json:"criteria,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

type wireAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
	Noul          *float64           `json:"noul"`
	Score         float64            `json:"score"`
}

// Decide asks every question in one request over one state.
func (c *Client) Decide(ctx context.Context, req ports.DecideRequest) (ports.DecideResult, error) {
	if c.APIKey == "" {
		return ports.DecideResult{}, fmt.Errorf("%w: JEV key not configured", ports.ErrUnavailable)
	}
	state := map[string]string{}
	total := 0
	for k, v := range req.State {
		var s string
		switch x := v.(type) {
		case string:
			s = x
		default:
			b, _ := json.Marshal(x)
			s = string(b)
		}
		if total+len(s) > MaxStateChars {
			s = s[:max(0, MaxStateChars-total)]
		}
		total += len(s)
		state[k] = s
	}
	qs := map[string]wireQuestion{}
	for id, q := range req.Questions {
		switch q.Type {
		case "choice":
			qs[id] = wireQuestion{Type: "choice", Criteria: q.Criteria}
		case "score":
			qs[id] = wireQuestion{Type: "score", Criteria: q.Levels}
		case "noul":
			qs[id] = wireQuestion{Type: "noul", Instructions: q.Instructions}
		default:
			return ports.DecideResult{}, fmt.Errorf("%w: question %s has type %q", ports.ErrInvalidQuery, id, q.Type)
		}
	}
	body, _ := json.Marshal(map[string]any{"model": c.Model, "state": state, "questions": qs})
	start := time.Now()
	out, _, err := c.HTTP.Do(ctx, "POST", c.BaseURL+"/decisions", body, map[string]string{
		"Authorization": "Bearer " + c.APIKey, "Content-Type": "application/json",
	})
	if err != nil {
		var se *platform.StatusError
		switch {
		case errors.As(err, &se) && se.Status == 429:
			return ports.DecideResult{}, fmt.Errorf("%w: %v", ports.ErrRateLimited, err)
		case errors.Is(err, context.DeadlineExceeded):
			return ports.DecideResult{}, fmt.Errorf("%w: %v", ports.ErrTimeout, err)
		}
		return ports.DecideResult{}, fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
	}
	var r struct {
		ID      string                `json:"id"`
		Model   string                `json:"model"`
		Answers map[string]wireAnswer `json:"answers"`
		Usage   struct {
			InputTokens  int64   `json:"input_tokens"`
			OutputTokens int64   `json:"output_tokens"`
			Cost         float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return ports.DecideResult{}, fmt.Errorf("jev: decode: %w", err)
	}
	res := ports.DecideResult{Model: r.Model, ID: r.ID, Answers: map[string]ports.Answer{}, Latency: time.Since(start)}
	res.Usage = ports.Usage{InputTokens: r.Usage.InputTokens, OutputTokens: r.Usage.OutputTokens, CostMicros: int64(r.Usage.Cost*1e6 + 0.999)}
	for id, a := range r.Answers {
		ans := ports.Answer{Type: a.Type, Choice: a.Choice, Probabilities: a.Probabilities, Confidence: a.Confidence, Score: a.Score}
		if a.Noul != nil {
			ans.Yes = *a.Noul
			ans.Confidence = abs(*a.Noul-0.5) * 2
		}
		// JEV's score is the expected level index (0..n-1); normalize to 0..1
		// so thresholds read the same for any number of levels.
		if q, ok := req.Questions[id]; ok && q.Type == "score" && len(q.Levels) > 1 {
			ans.Score = a.Score / float64(len(q.Levels)-1)
		}
		if a.Type == "choice" && a.Probabilities != nil {
			// Confidence for routing is the top probability: the threshold in
			// AI_SYSTEM_DESIGN §2 is calibrated on it.
			ans.Confidence = a.Probabilities[a.Choice]
		}
		res.Answers[id] = ans
	}
	for id := range req.Questions {
		if _, ok := res.Answers[id]; !ok {
			return res, fmt.Errorf("jev: missing answer for %s", id)
		}
	}
	return res, nil
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
