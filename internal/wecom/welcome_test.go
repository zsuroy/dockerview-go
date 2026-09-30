package wecom

import (
	"strings"
	"testing"
)

// welcomeEntries returns the outbound entries sent on the welcome channel.
func welcomeEntries(b *Bridge) []Entry {
	var out []Entry
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindOutbound && e.Channel == ChannelWelcome {
			out = append(out, e)
		}
	}
	return out
}

func TestEnterChatProducesTheWelcome(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{})

	if err := b.InjectEnterChat("carol"); err != nil {
		t.Fatalf("InjectEnterChat: %v", err)
	}

	got := welcomeEntries(b)
	if len(got) != 1 {
		t.Fatalf("got %d welcome entries, want 1", len(got))
	}
	if got[0].Text != DefaultWelcome {
		t.Fatalf("welcome = %q, want the configured default", got[0].Text)
	}
}

func TestEnterChatUsesTheConfiguredWelcome(t *testing.T) {
	cfg := mockConfig(t)
	cfg.Welcome = "自定义欢迎语"
	b, err := New(cfg, &fakeAsker{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Stop()

	if err := b.InjectEnterChat("dave"); err != nil {
		t.Fatalf("InjectEnterChat: %v", err)
	}
	got := welcomeEntries(b)
	if len(got) != 1 || got[0].Text != "自定义欢迎语" {
		t.Fatalf("welcome entries = %+v, want the override", got)
	}
}

func TestEnterChatRecordsAnEvent(t *testing.T) {
	b := newMockBridge(t, &fakeAsker{})
	if err := b.InjectEnterChat("erin"); err != nil {
		t.Fatalf("InjectEnterChat: %v", err)
	}

	var sawEvent bool
	for _, e := range b.Transcript().Entries() {
		if e.Kind == KindEvent && strings.Contains(e.Text, "enter_chat") {
			sawEvent = true
		}
	}
	if !sawEvent {
		t.Fatal("enter_chat should leave an event line in the transcript")
	}
}

func TestEnterChatStillAnswersWhenAskIsDisabled(t *testing.T) {
	// The welcome is a fixed local string; it must not depend on the agent.
	b := newMockBridge(t, nil)
	if err := b.InjectEnterChat("frank"); err != nil {
		t.Fatalf("InjectEnterChat: %v", err)
	}
	if got := welcomeEntries(b); len(got) != 1 {
		t.Fatalf("got %d welcome entries, want 1 even with no Asker", len(got))
	}
}

func TestEnterChatRefusedWhenDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	b, err := New(cfg, &fakeAsker{}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.InjectEnterChat("u"); err == nil {
		t.Fatal("enter_chat should be refused when the integration is off")
	}
}

func TestWelcomeNeverCallsAsk(t *testing.T) {
	ask := &fakeAsker{}
	b := newMockBridge(t, ask)

	if err := b.InjectEnterChat("gina"); err != nil {
		t.Fatalf("InjectEnterChat: %v", err)
	}
	waitIdle(t, b)

	if ask.callCount() != 0 {
		t.Fatalf("welcome path called Ask %d times; it must be a local string (5s deadline, 101463)", ask.callCount())
	}
}

func TestWelcomeTextFitsTheFiveSecondBudget(t *testing.T) {
	// A guard against someone wiring the welcome through the model later: the
	// text is assembled locally and must stay short enough to send immediately.
	if len(DefaultWelcome) > 300 {
		t.Fatalf("default welcome is %d chars; keep the enter_chat reply short", len(DefaultWelcome))
	}
}
