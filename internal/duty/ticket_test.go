package duty

import (
	"context"
	"testing"
)

func TestTicketRoundtrip(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	tk := &Ticket{
		Actor:      "tok_abc",
		ActorKind:  "token",
		Source:     "web",
		Question:   "why is api 502?",
		Conclusion: "api shows ERROR upstream 502 in last 200 lines; health 42",
		ToolSummary: []ToolTrace{
			{Tool: "tailLogs", Input: `{"id":"abc123456789"}`, OutputExcerpt: "ERROR upstream 502", Evidence: "abc123456789"},
		},
		RelatedContainer: "abc123456789",
	}
	if err := s.Insert(ctx, tk); err != nil {
		t.Fatal(err)
	}
	if tk.ID == 0 {
		t.Fatal("ID not assigned")
	}

	got, err := s.Get(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Question != tk.Question || got.Conclusion != tk.Conclusion {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if len(got.ToolSummary) != 1 || got.ToolSummary[0].Evidence != "abc123456789" {
		t.Fatalf("tool summary lost: %+v", got.ToolSummary)
	}
	if got.WriteConfirmed {
		t.Fatal("new ticket must not be write-confirmed")
	}

	if err := s.ConfirmWrite(ctx, tk.ID, "restart", "abc123456789", "req_123"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, tk.ID)
	if !got.WriteConfirmed || got.WriteAction != "restart" || got.RequestID != "req_123" {
		t.Fatalf("confirm write failed: %+v", got)
	}

	list, total, err := s.List(ctx, TicketFilter{Container: "api"})
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list failed: total=%d len=%d err=%v", total, len(list), err)
	}
}
