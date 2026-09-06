package duty

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestCustomBaseURLHitsTestServer verifies that when a custom BaseURL is
// configured, the agent's model requests go to that URL and NOT to
// api.openai.com. This is the "configurable BaseURL" acceptance gate.
func TestCustomBaseURLHitsTestServer(t *testing.T) {
	var requestCount int32
	var lastPath string
	var lastBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		lastPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)

		// Return a minimal valid OpenAI chat completion with a tool call
		// for listContainers, then a final text response.
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		msgs, _ := req["messages"].([]any)

		hasToolResponse := false
		for _, m := range msgs {
			if mm, ok := m.(map[string]any); ok {
				if mm["role"] == "tool" {
					hasToolResponse = true
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if !hasToolResponse {
			// First turn: ask for listContainers tool call.
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-test",
				"object":  "chat.completion",
				"created": 1234567890,
				"model":   "test-model",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call_test1",
									"type": "function",
									"function": map[string]any{
										"name":      "listContainers",
										"arguments": "{}",
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			})
		} else {
			// Second turn: final answer.
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-test2",
				"object":  "chat.completion",
				"created": 1234567891,
				"model":   "test-model",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "Two containers running: api (health 42) and web (health 91).",
						},
						"finish_reason": "stop",
					},
				},
			})
		}
	}))
	defer ts.Close()

	svc := fixtureServices()
	store, _ := OpenStore(":memory:")
	defer store.Close()

	cfg := DefaultConfig()
	cfg.APIKey = "test-key-not-a-real-key"
	cfg.BaseURL = ts.URL // <-- custom BaseURL points at the test server
	cfg.Model = "test-model"

	agent, err := NewAgent(context.Background(), cfg, svc, store)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	if agent.Mode() != "live" {
		t.Fatalf("want live mode with key, got %s", agent.Mode())
	}

	res, err := agent.Ask(context.Background(), "what containers are running?", "tok_test", "token", "web")
	if err != nil {
		t.Fatal(err)
	}

	if atomic.LoadInt32(&requestCount) == 0 {
		t.Fatal("no request hit the test server; BaseURL was not used")
	}
	if !strings.Contains(lastPath, "/chat/completions") {
		t.Fatalf("expected /chat/completions path, got %s", lastPath)
	}
	// Verify the request did NOT go to api.openai.com (the test server would
	// not have received it if it did).
	if strings.Contains(lastBody, "api.openai.com") {
		t.Fatal("request body references api.openai.com")
	}
	if res.Answer == "" {
		t.Fatal("empty answer")
	}
	// Verify listContainers was called.
	foundList := false
	for _, tr := range res.ToolTraces {
		if tr.Tool == "listContainers" {
			foundList = true
		}
	}
	if !foundList {
		t.Fatal("listContainers tool was not called")
	}
}
