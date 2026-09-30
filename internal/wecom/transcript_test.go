package wecom

import (
	"strings"
	"testing"
)

func TestTranscriptRingBufferDropsOldest(t *testing.T) {
	tr := NewTranscript(3)
	for _, s := range []string{"a", "b", "c", "d"} {
		tr.AddInbound(SourceWeCom, "u1", s)
	}
	entries := tr.Entries()
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	if entries[0].Text != "b" || entries[2].Text != "d" {
		t.Fatalf("ring kept %q..%q, want b..d", entries[0].Text, entries[2].Text)
	}
}

func TestTranscriptCapacityFloor(t *testing.T) {
	tr := NewTranscript(0)
	tr.AddInbound(SourceWeCom, "u", "x")
	if got := len(tr.Entries()); got != 1 {
		t.Fatalf("len = %d, want the default capacity to accept an entry", got)
	}
}

func TestTranscriptCounts(t *testing.T) {
	tr := NewTranscript(10)
	tr.AddInbound(SourceWeCom, "u", "q1")
	tr.AddInbound(SourceInject, "u", "q2")
	tr.AddOutbound(ChannelStream, "a1")
	tr.AddEvent("card")

	tr.mu.RLock()
	c := tr.countsLocked()
	tr.mu.RUnlock()

	if c.Inbound != 2 || c.Outbound != 1 || c.Event != 1 {
		t.Fatalf("counts = %+v, want inbound=2 outbound=1 event=1", c)
	}
}

func TestTranscriptStateTransitions(t *testing.T) {
	tr := NewTranscript(5)
	if got := tr.State(); got != StateDisconnected {
		t.Fatalf("initial state = %q, want disconnected", got)
	}
	tr.SetState(StateMock, "no credentials")
	if got := tr.State(); got != StateMock {
		t.Fatalf("state = %q, want mock", got)
	}
}

func TestTranscriptTruncatesLongText(t *testing.T) {
	tr := NewTranscript(2)
	long := strings.Repeat("x", MaxTextInEntry+500)
	tr.AddOutbound(ChannelStream, long)

	e := tr.Entries()[0]
	if len(e.Text) <= MaxTextInEntry {
		t.Fatalf("entry text length = %d, expected truncation above %d", len(e.Text), MaxTextInEntry)
	}
	if !strings.HasSuffix(e.Text, "…") {
		t.Fatal("truncated entry should end with an ellipsis")
	}
}

func TestTranscriptEntriesIsACopy(t *testing.T) {
	tr := NewTranscript(4)
	tr.AddInbound(SourceWeCom, "u", "first")
	got := tr.Entries()
	got[0].Text = "mutated"
	if tr.Entries()[0].Text != "first" {
		t.Fatal("Entries returned a slice aliasing internal state")
	}
}

func TestTranscriptAddStampsTime(t *testing.T) {
	tr := NewTranscript(4)
	e := tr.AddInbound(SourceWeCom, "u", "q")
	if e.Time.IsZero() {
		t.Fatal("Add should stamp a time when the entry has none")
	}
}
