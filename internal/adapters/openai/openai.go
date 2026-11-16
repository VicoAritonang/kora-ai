// Package openai adapts the OpenAI Responses API to ports.ModelProvider.
//
// It is a thin HTTP client rather than the openai-go SDK: the surface Kora
// needs (function tools, web search, strict JSON schema, streaming text,
// usage) is small, and keeping the wire format here makes the adapter
// verifiable against recorded responses. SDK types would not leave this
// package either way (rule D6).
package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Price is USD per 1M tokens.
type Price struct{ Input, Cached, Output float64 }

// Prices from the OpenAI pricing page, 14 Nov 2026 (TECH_STACK §5). Models
// not listed are metered at the gpt-5-mini price and flagged in logs.
var Prices = map[string]Price{
	"gpt-5-nano":    {0.05, 0.005, 0.40},
	"gpt-5-mini":    {0.25, 0.025, 2.00},
	"gpt-4o-mini":   {0.15, 0.075, 0.60},
	"gpt-4.1-mini":  {0.40, 0.10, 1.60},
	"gpt-6-luna":    {0.10, 0.01, 0.50},
	"gpt-5.6-luna":  {0.20, 0.02, 1.20},
	"gpt-5.4-mini":  {0.75, 0.075, 4.50},
	"gpt-6.1-terra": {1.00, 0.05, 5.00},
}

// WebSearchCallMicros is $10 per 1,000 web search calls.
const WebSearchCallMicros = 10_000

// Client is the OpenAI adapter.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *platform.HTTPClient
}

// New returns a client.
func New(baseURL, apiKey string, timeout time.Duration) *Client {
	h := platform.NewHTTPClient("openai", timeout)
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, HTTP: h}
}

func isReasoning(model string) bool {
	return strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "gpt-6") || strings.HasPrefix(model, "o")
}

// --- wire types -----------------------------------------------------------

type inputItem struct {
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type tool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type request struct {
	Model             string         `json:"model"`
	Instructions      string         `json:"instructions,omitempty"`
	Input             []inputItem    `json:"input"`
	Tools             []tool         `json:"tools,omitempty"`
	ParallelToolCalls *bool          `json:"parallel_tool_calls,omitempty"`
	MaxOutputTokens   int            `json:"max_output_tokens,omitempty"`
	Reasoning         map[string]any `json:"reasoning,omitempty"`
	Text              map[string]any `json:"text,omitempty"`
	Store             bool           `json:"store"`
	PromptCacheKey    string         `json:"prompt_cache_key,omitempty"`
	Stream            bool           `json:"stream,omitempty"`
	Include           []string       `json:"include,omitempty"`
}

type outputContent struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"annotations"`
}

type outputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   []outputContent `json:"content"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Status    string          `json:"status"`
}

type response struct {
	ID                string       `json:"id"`
	Model             string       `json:"model"`
	Status            string       `json:"status"`
	Output            []outputItem `json:"output"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
	Usage struct {
		InputTokens        int64 `json:"input_tokens"`
		OutputTokens       int64 `json:"output_tokens"`
		InputTokensDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

func (c *Client) build(req ports.GenerateRequest, model string, stream bool) request {
	r := request{Model: model, Instructions: req.Instructions, MaxOutputTokens: req.MaxOutputTokens, Store: false, PromptCacheKey: req.CacheKey, Stream: stream}
	for _, it := range req.Input {
		switch it.Type {
		case "function_call":
			r.Input = append(r.Input, inputItem{Type: "function_call", CallID: it.CallID, Name: it.Name, Arguments: it.Arguments})
		case "function_call_output":
			r.Input = append(r.Input, inputItem{Type: "function_call_output", CallID: it.CallID, Output: it.Content})
		default:
			r.Input = append(r.Input, inputItem{Type: "message", Role: string(it.Role), Content: it.Content})
		}
	}
	f := false
	for _, t := range req.Tools {
		r.Tools = append(r.Tools, tool{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Parameters, Strict: &f})
	}
	if req.WebSearch {
		r.Tools = append(r.Tools, tool{Type: "web_search"})
		r.Include = []string{"web_search_call.action.sources"}
	}
	if len(req.Tools) > 0 {
		p := req.ParallelTools
		r.ParallelToolCalls = &p
	}
	if isReasoning(model) {
		r.Reasoning = map[string]any{"effort": NormalizeEffort(model, req.ReasoningEffort, req.WebSearch)}
	}
	if req.Schema != nil {
		r.Text = map[string]any{"format": map[string]any{"type": "json_schema", "name": req.Schema.Name, "schema": req.Schema.Schema, "strict": true}}
	}
	if isReasoning(model) && req.Schema == nil {
		if r.Text == nil {
			r.Text = map[string]any{}
		}
		r.Text["verbosity"] = "medium"
	}
	return r
}

// NormalizeEffort maps a neutral effort to a level the model accepts.
// The original GPT-5 family (gpt-5, -mini, -nano and dated snapshots)
// accepts minimal|low|medium|high. Later reasoning models (gpt-5.1+, gpt-6)
// drop "minimal"; OpenAI's migration guidance is to move minimal/none
// workloads to low and re-test. Web search does not run at minimal.
func NormalizeEffort(model, effort string, webSearch bool) string {
	switch effort {
	case "minimal", "low", "medium", "high":
	default:
		effort = "low"
	}
	original := model == "gpt-5" || strings.HasPrefix(model, "gpt-5-")
	if effort == "minimal" && (!original || webSearch) {
		effort = "low"
	}
	return effort
}

func (c *Client) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.APIKey, "Content-Type": "application/json"}
}

// Generate runs one non-streaming call, falling back to req.FallbackModel
// once when the primary model fails (SYSTEM_ARCHITECTURE §9).
func (c *Client) Generate(ctx context.Context, req ports.GenerateRequest) (ports.GenerateResult, error) {
	res, err := c.generate(ctx, req, req.Model)
	if err != nil && req.FallbackModel != "" && req.FallbackModel != req.Model && ctx.Err() == nil && retryable(err) {
		res, err = c.generate(ctx, req, req.FallbackModel)
		res.UsedFallback = err == nil
	}
	return res, err
}

func retryable(err error) bool {
	var se *platform.StatusError
	if errors.As(err, &se) {
		return se.Transient() || se.Status == 404 || se.Status == 400 && strings.Contains(se.Body, "model")
	}
	return true // network errors, timeouts
}

func (c *Client) generate(ctx context.Context, req ports.GenerateRequest, model string) (ports.GenerateResult, error) {
	start := time.Now()
	body, _ := json.Marshal(c.build(req, model, false))
	out, _, err := c.HTTP.Do(ctx, "POST", c.BaseURL+"/responses", body, c.headers())
	if err != nil {
		return ports.GenerateResult{Model: model}, mapErr(err)
	}
	var r response
	if err := json.Unmarshal(out, &r); err != nil {
		return ports.GenerateResult{Model: model}, fmt.Errorf("openai: decode: %w", err)
	}
	res := convert(r, model, req.WebSearch)
	res.Latency = time.Since(start)
	if r.Error != nil {
		return res, fmt.Errorf("openai: %s", r.Error.Message)
	}
	return res, nil
}

func mapErr(err error) error {
	var se *platform.StatusError
	switch {
	case errors.As(err, &se) && se.Status == 429:
		return fmt.Errorf("%w: %v", ports.ErrRateLimited, err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", ports.ErrTimeout, err)
	case errors.Is(err, platform.ErrCircuitOpen):
		return fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
	}
	return err
}

func convert(r response, model string, webSearch bool) ports.GenerateResult {
	res := ports.GenerateResult{Model: r.Model, ResponseID: r.ID, StopReason: r.Status}
	if res.Model == "" {
		res.Model = model
	}
	var text strings.Builder
	searches := 0
	for _, o := range r.Output {
		switch o.Type {
		case "message":
			for _, c := range o.Content {
				if c.Type == "output_text" {
					text.WriteString(c.Text)
					for _, a := range c.Annotations {
						if a.Type == "url_citation" && a.URL != "" {
							res.Citations = append(res.Citations, ports.Citation{URL: a.URL, Title: a.Title})
						}
					}
				}
			}
		case "function_call":
			res.ToolCalls = append(res.ToolCalls, ports.ToolCall{CallID: o.CallID, Name: o.Name, Arguments: json.RawMessage(o.Arguments)})
		case "web_search_call":
			searches++
		}
	}
	res.Text = text.String()
	if len(res.ToolCalls) > 0 {
		res.StopReason = "tool_calls"
	}
	if r.Status == "incomplete" && r.IncompleteDetails != nil {
		res.StopReason = "incomplete:" + r.IncompleteDetails.Reason
	}
	res.Usage = ports.Usage{InputTokens: r.Usage.InputTokens, CachedTokens: r.Usage.InputTokensDetails.CachedTokens, OutputTokens: r.Usage.OutputTokens}
	res.Usage.CostMicros = Cost(model, res.Usage) + int64(searches)*WebSearchCallMicros
	return res
}

// Cost returns the call cost in micro-USD, rounded up.
func Cost(model string, u ports.Usage) int64 {
	p, ok := Prices[model]
	if !ok {
		// Dated snapshots ("gpt-5-mini-2025-08-07") use the base price.
		for name, price := range Prices {
			if strings.HasPrefix(model, name+"-") {
				p, ok = price, true
				break
			}
		}
		if !ok {
			p = Prices["gpt-5-mini"]
		}
	}
	uncached := u.InputTokens - u.CachedTokens
	if uncached < 0 {
		uncached = 0
	}
	// tokens × USD/1M = micro-USD.
	v := float64(uncached)*p.Input + float64(u.CachedTokens)*p.Cached + float64(u.OutputTokens)*p.Output
	return int64(v + 0.999)
}

// Stream runs a streaming call and reports text deltas as they arrive.
func (c *Client) Stream(ctx context.Context, req ports.GenerateRequest, onDelta func(string)) (ports.GenerateResult, error) {
	res, err := c.stream(ctx, req, req.Model, onDelta)
	if err != nil && !res.UsedFallback && res.Text == "" && req.FallbackModel != "" && req.FallbackModel != req.Model && ctx.Err() == nil && retryable(err) {
		res, err = c.stream(ctx, req, req.FallbackModel, onDelta)
		res.UsedFallback = err == nil
	}
	return res, err
}

func (c *Client) stream(ctx context.Context, req ports.GenerateRequest, model string, onDelta func(string)) (ports.GenerateResult, error) {
	start := time.Now()
	body, _ := json.Marshal(c.build(req, model, true))
	h := c.headers()
	h["Accept"] = "text/event-stream"
	resp, err := c.HTTP.Stream(ctx, "POST", c.BaseURL+"/responses", body, h)
	if err != nil {
		return ports.GenerateResult{Model: model}, mapErr(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	var final *response
	var text strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			Type     string    `json:"type"`
			Delta    string    `json:"delta"`
			Response *response `json:"response"`
			Message  string    `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			text.WriteString(ev.Delta)
			if onDelta != nil {
				onDelta(ev.Delta)
			}
		case "response.completed", "response.incomplete":
			final = ev.Response
		case "response.failed":
			msg := "response failed"
			if ev.Response != nil && ev.Response.Error != nil {
				msg = ev.Response.Error.Message
			}
			return ports.GenerateResult{Model: model, Text: text.String()}, fmt.Errorf("openai: %s", msg)
		case "error":
			return ports.GenerateResult{Model: model, Text: text.String()}, fmt.Errorf("openai stream error: %s", ev.Message)
		}
	}
	if err := sc.Err(); err != nil {
		return ports.GenerateResult{Model: model, Text: text.String()}, mapErr(err)
	}
	if final == nil {
		return ports.GenerateResult{Model: model, Text: text.String()}, errors.New("openai: stream ended without a completed response")
	}
	res := convert(*final, model, req.WebSearch)
	if res.Text == "" {
		res.Text = text.String()
	}
	res.Latency = time.Since(start)
	return res, nil
}
