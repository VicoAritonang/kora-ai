package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koraai/koraai/internal/ports"
)

// Recorded from typesafe/jev-1.13 on 15 Nov 2026.
const recorded = `{"model":"typesafe/jev-1.13-20260917","answers":{"intent":{"type":"choice","choice":"info","probabilities":{"plan_trip":0.23,"budget_to_destination":0.04,"refine":0,"info":0.73,"chitchat":0,"out_of_scope":0},"confidence":0.68},"has_budget":{"type":"noul","noul":0.97},"quality":{"type":"score","score":0.57,"legend":{"0":"a","1":"b"},"probabilities":{"0":0.43,"1":0.57},"confidence":0.14}},"usage":{"input_tokens":469,"output_tokens":98,"cost":0.000019698},"id":"gen-dec-1"}`

func TestDecide_MapsAnswers(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/decisions" {
			t.Errorf("path %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		fmt.Fprint(w, recorded)
	}))
	defer srv.Close()
	c, err := New(srv.URL, "k", "typesafe/jev-1.13", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Decide(context.Background(), ports.DecideRequest{
		State: map[string]any{"message": "x", "trip_state": map[string]any{"a": 1}},
		Questions: map[string]ports.Question{
			"intent":     {Type: "choice", Criteria: map[string]string{"plan_trip": "p", "info": "i"}},
			"has_budget": {Type: "noul", Instructions: "budget?"},
			"quality":    {Type: "score", Levels: []string{"a", "b"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a := res.Answers["intent"]; a.Choice != "info" || a.Confidence != 0.73 {
		t.Fatalf("intent %+v", a)
	}
	// Two levels: the expected index 0.57 is already on 0..1.
	if res.Answers["has_budget"].Yes != 0.97 || res.Answers["quality"].Score != 0.57 {
		t.Fatalf("%+v", res.Answers)
	}
	if res.Usage.CostMicros != 20 {
		t.Fatalf("cost %d", res.Usage.CostMicros)
	}
	st := got["state"].(map[string]any)
	if st["trip_state"] != `{"a":1}` {
		t.Fatalf("state not stringified: %v", st)
	}
	q := got["questions"].(map[string]any)["quality"].(map[string]any)
	if _, ok := q["criteria"].([]any); !ok {
		t.Fatalf("score criteria must be a list: %v", q)
	}
}

func TestNew_RefusesLatest(t *testing.T) {
	if _, err := New("x", "k", "typesafe/jev-latest", time.Second); err == nil {
		t.Fatal("accepted unpinned model")
	}
}
