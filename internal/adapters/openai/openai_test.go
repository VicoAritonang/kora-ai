package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/koraai/koraai/internal/ports"
)

// Recorded shape of a Responses API reply with a function call.
const toolCallResponse = `{
 "id": "resp_1", "model": "gpt-5-mini-2025-08-07", "status": "completed",
 "output": [
  {"type": "reasoning", "id": "rs_1", "content": []},
  {"type": "function_call", "call_id": "call_1", "name": "search_flights", "arguments": "{\"origin\":\"JKT\",\"destination\":\"OSA\"}", "status": "completed"}
 ],
 "usage": {"input_tokens": 2000, "input_tokens_details": {"cached_tokens": 1000}, "output_tokens": 300}
}`

func TestGenerate_ToolCallAndCost(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		fmt.Fprint(w, toolCallResponse)
	}))
	defer srv.Close()
	c := New(srv.URL, "k", 5*time.Second)
	res, err := c.Generate(context.Background(), ports.GenerateRequest{
		Model: "gpt-5-mini", Instructions: "sys",
		Input: []ports.InputItem{ports.Message(ports.RoleUser, "hi"), {Type: "function_call", CallID: "c0", Name: "kg_query", Arguments: "{}"}, {Type: "function_call_output", CallID: "c0", Content: "ok"}},
		Tools: []ports.ToolSpec{{Name: "search_flights", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "search_flights" || res.StopReason != "tool_calls" {
		t.Fatalf("%+v", res)
	}
	// (1000 × 0.25) + (1000 × 0.025) + (300 × 2.00) = 875 micro-USD.
	if res.Usage.CostMicros != 875 {
		t.Fatalf("cost %d", res.Usage.CostMicros)
	}
	if got["store"] != false || got["reasoning"] == nil {
		t.Fatalf("request %+v", got)
	}
	in := got["input"].([]any)
	if in[1].(map[string]any)["type"] != "function_call" || in[2].(map[string]any)["output"] != "ok" {
		t.Fatalf("input items %+v", in)
	}
}

func TestGenerate_NonReasoningModel_NoReasoningParam(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		fmt.Fprint(w, `{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"halo"}]}],"usage":{}}`)
	}))
	defer srv.Close()
	res, err := New(srv.URL, "k", time.Second).Generate(context.Background(), ports.GenerateRequest{Model: "gpt-4o-mini", Input: []ports.InputItem{ports.Message(ports.RoleUser, "x")}})
	if err != nil || res.Text != "halo" {
		t.Fatalf("%v %+v", err, res)
	}
	if _, ok := got["reasoning"]; ok {
		t.Fatal("reasoning sent to a non-reasoning model")
	}
}

func TestGenerate_FallbackOnServerError(t *testing.T) {
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		m := req["model"].(string)
		calls[m]++
		if m == "gpt-5-mini" {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, `{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}],"usage":{}}`)
	}))
	defer srv.Close()
	res, err := New(srv.URL, "k", time.Second).Generate(context.Background(), ports.GenerateRequest{Model: "gpt-5-mini", FallbackModel: "gpt-4.1-mini", Input: []ports.InputItem{ports.Message(ports.RoleUser, "x")}})
	if err != nil || !res.UsedFallback || res.Text != "ok" {
		t.Fatalf("%v %+v", err, res)
	}
	if calls["gpt-5-mini"] != 2 { // one retry inside the HTTP client
		t.Fatalf("calls %v", calls)
	}
}

func TestStream_Deltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, d := range []string{"Halo ", "dunia"} {
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", d)
		}
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Halo dunia"}]}],"usage":{"input_tokens":10,"output_tokens":2}}}`+"\n\n")
	}))
	defer srv.Close()
	var parts []string
	res, err := New(srv.URL, "k", time.Second).Stream(context.Background(), ports.GenerateRequest{Model: "gpt-5-mini", Input: []ports.InputItem{ports.Message(ports.RoleUser, "x")}}, func(s string) { parts = append(parts, s) })
	if err != nil || res.Text != "Halo dunia" || strings.Join(parts, "") != "Halo dunia" {
		t.Fatalf("%v %+v %v", err, res, parts)
	}
}

func TestNormalizeEffort(t *testing.T) {
	cases := []struct {
		model, in string
		web       bool
		want      string
	}{
		{"gpt-5-nano", "minimal", false, "minimal"},
		{"gpt-5-mini-2025-08-07", "minimal", false, "minimal"},
		{"gpt-5-mini", "minimal", true, "low"}, // web search needs ≥ low
		{"gpt-5.4-mini", "minimal", false, "low"},
		{"gpt-6-luna", "minimal", false, "low"},
		{"gpt-5-mini", "medium", false, "medium"},
		{"gpt-5-mini", "auto", false, "low"},
	}
	for _, c := range cases {
		if got := NormalizeEffort(c.model, c.in, c.web); got != c.want {
			t.Errorf("%s/%s/%v: %s", c.model, c.in, c.web, got)
		}
	}
}
