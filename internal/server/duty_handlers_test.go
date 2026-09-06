package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zsuroy/dockerview-go/internal/duty"
)

// fakeDutyAgent implements dutyAgent for testing the HTTP layer.
type fakeDutyAgent struct {
	askCalled int32
	mode      string
	store     *duty.Store
	proposed  *duty.PreviewResult
}

func (f *fakeDutyAgent) Ask(ctx context.Context, question, actor, kind, source string) (*duty.AskResult, error) {
	atomic.AddInt32(&f.askCalled, 1)
	return &duty.AskResult{
		Answer:        "test answer",
		ToolTraces:    []duty.ToolTrace{{Tool: "listContainers", Input: "{}", OutputExcerpt: "api"}},
		TicketID:      1,
		ProposedWrite: f.proposed,
	}, nil
}
func (f *fakeDutyAgent) Mode() string       { return f.mode }
func (f *fakeDutyAgent) ModelName() string  { return "dockerview/fake" }
func (f *fakeDutyAgent) BaseURL() string    { return "https://api.openai.com/v1" }
func (f *fakeDutyAgent) Store() *duty.Store { return f.store }
func (f *fakeDutyAgent) Close() error       { return nil }

func TestDutyConfirmRequiresAuth(t *testing.T) {
	s := NewServer(nil, "secret-token", "v1", "abc", "now")
	s.SetDutyAgent(&fakeDutyAgent{mode: "fake", store: mustStore(t)})

	body, _ := json.Marshal(map[string]any{"ticket_id": 1, "op": "restart", "id": "abc123", "confirm": true})
	req := httptest.NewRequest("POST", "/api/duty/confirm", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", w.Code)
	}
}

func TestDutyConfirmRejectsInvalidOp(t *testing.T) {
	s := NewServer(nil, "secret-token", "v1", "abc", "now")
	s.SetDutyAgent(&fakeDutyAgent{mode: "fake", store: mustStore(t)})

	body, _ := json.Marshal(map[string]any{"ticket_id": 1, "op": "rm -rf /", "id": "abc123", "confirm": true})
	req := httptest.NewRequest("POST", "/api/duty/confirm?token=secret-token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid op, got %d", w.Code)
	}
}

func TestDutyConfirmRejectsWithoutConfirm(t *testing.T) {
	s := NewServer(nil, "secret-token", "v1", "abc", "now")
	s.SetDutyAgent(&fakeDutyAgent{mode: "fake", store: mustStore(t)})

	body, _ := json.Marshal(map[string]any{"ticket_id": 1, "op": "restart", "id": "abc123"})
	req := httptest.NewRequest("POST", "/api/duty/confirm?token=secret-token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 without confirm flag, got %d", w.Code)
	}
}

func TestDutyConfigNoKeyExposed(t *testing.T) {
	s := NewServer(nil, "", "v1", "abc", "now")
	s.SetDutyAgent(&fakeDutyAgent{mode: "fake"})

	req := httptest.NewRequest("GET", "/api/duty/config", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "secret") || strings.Contains(body, "api_key") || strings.Contains(body, "API_KEY") {
		t.Fatalf("config response must not expose keys: %s", body)
	}
}

func TestDutyAskDisabled(t *testing.T) {
	s := NewServer(nil, "", "v1", "abc", "now")
	// no duty agent set

	body, _ := json.Marshal(map[string]any{"question": "hello"})
	req := httptest.NewRequest("POST", "/api/duty/ask", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when disabled, got %d", w.Code)
	}
}

func mustStore(t *testing.T) *duty.Store {
	t.Helper()
	s, err := duty.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
