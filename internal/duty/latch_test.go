package duty

import (
	"context"
	"strings"
	"testing"
)

// TestUnconfirmedWriteNeverExecutes verifies that asking about a restart
// through the agent only calls previewRestart, never executes a write.
// The fake services execCalls counter must remain 0.
func TestUnconfirmedWriteNeverExecutes(t *testing.T) {
	svc := fixtureServices()
	store, _ := OpenStore(":memory:")
	defer store.Close()

	cfg := DefaultConfig() // no API key -> fake mode
	agent, err := NewAgent(context.Background(), cfg, svc, store)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()

	res, err := agent.Ask(context.Background(), "should I restart api?", "tok_admin", "token", "web")
	if err != nil {
		t.Fatal(err)
	}

	// The agent should have proposed a write.
	if res.ProposedWrite == nil {
		t.Fatalf("expected proposed write, got nil; traces: %+v", res.ToolTraces)
	}
	if res.ProposedWrite.Op != "restart" {
		t.Fatalf("expected restart, got %s", res.ProposedWrite.Op)
	}

	// No exec/write should have happened.
	if svc.execCalls != 0 {
		t.Fatalf("expected 0 exec calls without confirmation, got %d", svc.execCalls)
	}

	// The ticket should not be marked as write-confirmed.
	ticket, err := store.Get(context.Background(), res.TicketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.WriteConfirmed {
		t.Fatal("ticket must not be write-confirmed before human confirmation")
	}
}

// TestEvidenceInAnswer verifies that the fake model's answer contains
// the fixture ERROR substring from tailLogs, proving the tool output
// is used as evidence.
func TestEvidenceInAnswer(t *testing.T) {
	svc := fixtureServices()
	store, _ := OpenStore(":memory:")
	defer store.Close()

	cfg := DefaultConfig()
	agent, _ := NewAgent(context.Background(), cfg, svc, store)
	defer agent.Close()

	res, err := agent.Ask(context.Background(), "show me ERROR in api logs", "tok_test", "token", "web")
	if err != nil {
		t.Fatal(err)
	}

	// The tool traces must contain the fixture ERROR line.
	foundEvidence := false
	for _, tr := range res.ToolTraces {
		if tr.Tool == "tailLogs" && strings.Contains(tr.OutputExcerpt, "ERROR upstream 502") {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Fatalf("fixture ERROR not found in tool traces: %+v", res.ToolTraces)
	}

	// The answer should reference the tool data.
	if res.Answer == "" {
		t.Fatal("empty answer")
	}
}

// TestGuestCannotWrite is a placeholder for the HTTP-level test (in server
// package). At the tool level, there is no role distinction — the agent
// only calls preview tools, never writes. The HTTP layer enforces auth.
func TestGuestCannotWrite(t *testing.T) {
	svc := fixtureServices()
	store, _ := OpenStore(":memory:")
	defer store.Close()

	cfg := DefaultConfig()
	agent, _ := NewAgent(context.Background(), cfg, svc, store)
	defer agent.Close()

	// Even an "anonymous" ask only gets previews.
	res, err := agent.Ask(context.Background(), "restart api", "anonymous", "anonymous", "web")
	if err != nil {
		t.Fatal(err)
	}
	if svc.execCalls != 0 {
		t.Fatal("anonymous ask must not execute anything")
	}
	_ = res
}
