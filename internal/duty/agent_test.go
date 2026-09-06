package duty

import (
	"context"
	"strings"
	"testing"
)

func TestFakeAgentToolLoop(t *testing.T) {
	svc := fixtureServices()
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// No API key -> fake model.
	cfg := DefaultConfig()
	cfg.APIKey = ""
	agent, err := NewAgent(context.Background(), cfg, svc, store)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	if agent.Mode() != "fake" {
		t.Fatalf("want fake mode, got %s", agent.Mode())
	}

	// Ask about an ERROR -> should call tailLogs.
	res, err := agent.Ask(context.Background(), "show me the ERROR in api logs", "tok_test", "token", "web")
	if err != nil {
		t.Fatal(err)
	}
	if res.TicketID == 0 {
		t.Fatal("ticket not created")
	}

	// Check that tailLogs was called.
	foundTailLogs := false
	for _, tr := range res.ToolTraces {
		if tr.Tool == "tailLogs" {
			foundTailLogs = true
			if !strings.Contains(tr.OutputExcerpt, "ERROR upstream 502") {
				t.Fatalf("expected fixture ERROR in output, got %q", tr.OutputExcerpt)
			}
		}
	}
	if !foundTailLogs {
		t.Fatalf("tailLogs was not called; traces: %+v", res.ToolTraces)
	}

	// Answer should contain evidence.
	if res.Answer == "" {
		t.Fatal("empty answer")
	}
}

func TestFakeAgentListContainers(t *testing.T) {
	svc := fixtureServices()
	store, _ := OpenStore(":memory:")
	defer store.Close()

	cfg := DefaultConfig()
	agent, _ := NewAgent(context.Background(), cfg, svc, store)
	defer agent.Close()

	res, err := agent.Ask(context.Background(), "what containers are running?", "anonymous", "anonymous", "web")
	if err != nil {
		t.Fatal(err)
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
}

func TestFakeAgentPreviewRestart(t *testing.T) {
	svc := fixtureServices()
	store, _ := OpenStore(":memory:")
	defer store.Close()

	cfg := DefaultConfig()
	agent, _ := NewAgent(context.Background(), cfg, svc, store)
	defer agent.Close()

	res, err := agent.Ask(context.Background(), "should I restart api?", "tok_admin", "token", "web")
	if err != nil {
		t.Fatal(err)
	}
	if res.ProposedWrite == nil {
		t.Fatalf("expected proposed write; traces: %+v", res.ToolTraces)
	}
	if res.ProposedWrite.Op != "restart" || res.ProposedWrite.Name != "api" {
		t.Fatalf("bad proposed write: %+v", res.ProposedWrite)
	}
	// Verify nothing was actually executed.
	if svc.execCalls != 0 {
		t.Fatal("preview must not execute")
	}
}
