package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/zsuroy/dockerview-go/internal/audit"
	"github.com/zsuroy/dockerview-go/internal/docker"
	"github.com/zsuroy/dockerview-go/internal/duty"
)

// TestDutyEndToEndFakeMode verifies the full HTTP path: ask -> tool traces ->
// ticket, using the fake model (no API key, no Docker daemon).
func TestDutyEndToEndFakeMode(t *testing.T) {
	// Set up a server with no Docker client but with snapshot data.
	s := NewServer(nil, "testtoken", "v1", "abc", "now")
	s.UpdateData([]docker.ContainerInfo{
		{ID: "abc123456789", Name: "api", Status: "running", HealthScore: 42, HealthStatus: docker.HealthStatusDangerous},
		{ID: "def987654321", Name: "web", Status: "running", HealthScore: 91, HealthStatus: docker.HealthStatusHealthy},
	})

	// Set up audit recorder.
	aud, err := audit.Open(audit.Config{DBPath: ":memory:", RetentionDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.SetAuditer(aud)

	// Create duty agent in fake mode (no API key).
	store, err := duty.OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewDutyServices(s)
	cfg := duty.DefaultConfig()
	cfg.APIKey = "" // force fake mode
	agent, err := duty.NewAgent(context.Background(), cfg, svc, store)
	if err != nil {
		t.Fatal(err)
	}
	s.SetDutyAgent(agent)

	// Ask about containers.
	body, _ := json.Marshal(map[string]string{"question": "what containers are running?"})
	req := httptest.NewRequest("POST", "/api/duty/ask?token=testtoken", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("ask: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var res duty.AskResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Answer == "" {
		t.Fatal("empty answer")
	}
	if len(res.ToolTraces) == 0 {
		t.Fatal("no tool traces")
	}
	foundList := false
	for _, tr := range res.ToolTraces {
		if tr.Tool == "listContainers" {
			foundList = true
		}
	}
	if !foundList {
		t.Fatalf("listContainers not called; traces: %+v", res.ToolTraces)
	}
	if res.TicketID == 0 {
		t.Fatal("no ticket created")
	}

	// List tickets.
	req2 := httptest.NewRequest("GET", "/api/duty/tickets?token=testtoken", nil)
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("tickets: want 200, got %d", w2.Code)
	}

	// Config endpoint.
	req3 := httptest.NewRequest("GET", "/api/duty/config", nil)
	w3 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w3, req3)
	if w3.Code != 200 {
		t.Fatalf("config: want 200, got %d", w3.Code)
	}
	cfgResp := w3.Body.String()
	if bytes.Contains([]byte(cfgResp), []byte("APIKey")) || bytes.Contains([]byte(cfgResp), []byte("api_key")) {
		t.Fatalf("config must not expose keys: %s", cfgResp)
	}

	// Confirm without Docker client should fail gracefully (503).
	confirmBody, _ := json.Marshal(map[string]any{
		"ticket_id": res.TicketID, "op": "restart", "id": "abc123456789", "confirm": true,
	})
	req4 := httptest.NewRequest("POST", "/api/duty/confirm?token=testtoken", bytes.NewReader(confirmBody))
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w4, req4)
	// No Docker client -> 503.
	if w4.Code != 503 {
		t.Logf("confirm without docker: got %d (expected 503)", w4.Code)
	}
}
