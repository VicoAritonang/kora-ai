package openpersona

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Memory is an HTTP client to `openpersona serve` (one internal instance;
// SYSTEM_ARCHITECTURE §14). The scope is the user id.
type Memory struct {
	BaseURL string
	HTTP    *platform.HTTPClient
}

// NewMemory returns a memory client.
func NewMemory(baseURL string) *Memory {
	return &Memory{BaseURL: strings.TrimRight(baseURL, "/"), HTTP: platform.NewHTTPClient("openpersona-memory", 5*time.Second)}
}

// Remember stores already-redacted messages; fact extraction runs
// asynchronously inside the memory service.
func (m *Memory) Remember(ctx context.Context, userID uuid.UUID, msgs []ports.MemoryMessage) error {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var body struct {
		Messages []msg `json:"messages"`
	}
	for _, x := range msgs {
		body.Messages = append(body.Messages, msg{x.Role, x.Content})
	}
	b, _ := json.Marshal(body)
	_, _, err := m.HTTP.Do(ctx, "POST", m.BaseURL+"/v1/memory/"+userID.String()+"/messages", b, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
	}
	return nil
}

// Recall returns a prompt-ready memory block (no LLM).
func (m *Memory) Recall(ctx context.Context, userID uuid.UUID, query string, maxTokens int) (ports.MemoryBlock, error) {
	b, _ := json.Marshal(map[string]any{"query": query, "max_tokens": maxTokens})
	out, _, err := m.HTTP.Do(ctx, "POST", m.BaseURL+"/v1/memory/"+userID.String()+"/search", b, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return ports.MemoryBlock{}, fmt.Errorf("%w: %v", ports.ErrUnavailable, err)
	}
	var r struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return ports.MemoryBlock{}, err
	}
	return ports.MemoryBlock{Text: r.Context, Tokens: platform.EstimateTokens(r.Context)}, nil
}

// List is not available: OpenPersona has no per-scope fact listing yet
// (IMPLEMENTATION_PLAN F0-10).
func (m *Memory) List(context.Context, uuid.UUID) ([]ports.MemoryFact, error) {
	return nil, fmt.Errorf("%w: openpersona has no fact listing per scope yet", ports.ErrNotSupported)
}

// Forget is not available: OpenPersona has no scope deletion yet, which is
// why memory stays disabled for real users (TECHNICAL_SPEC §5).
func (m *Memory) Forget(context.Context, uuid.UUID) error {
	return fmt.Errorf("%w: openpersona has no scope deletion yet", ports.ErrNotSupported)
}

// Disabled is the UserMemory used while MEMORY_ENABLED=false.
type Disabled struct{}

// Remember is a no-op.
func (Disabled) Remember(context.Context, uuid.UUID, []ports.MemoryMessage) error { return nil }

// Recall returns an empty block.
func (Disabled) Recall(context.Context, uuid.UUID, string, int) (ports.MemoryBlock, error) {
	return ports.MemoryBlock{}, nil
}

// List returns nothing.
func (Disabled) List(context.Context, uuid.UUID) ([]ports.MemoryFact, error) { return nil, nil }

// Forget is a no-op (nothing is stored).
func (Disabled) Forget(context.Context, uuid.UUID) error { return nil }

var (
	_ ports.UserMemory = (*Memory)(nil)
	_ ports.UserMemory = Disabled{}
)
