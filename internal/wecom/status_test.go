package wecom

import (
	"strings"
	"testing"
	"time"
)

func TestStatusLineDisabled(t *testing.T) {
	for _, s := range []Snapshot{
		{},
		{Enabled: false, Mode: string(ModeMock)},
		{Enabled: true, Mode: string(ModeOff)},
	} {
		if got := StatusLine(s); got != "wecom: off" {
			t.Errorf("StatusLine(%+v) = %q, want \"wecom: off\"", s, got)
		}
	}
}

func TestStatusLineIdle(t *testing.T) {
	got := StatusLine(Snapshot{Enabled: true, Mode: string(ModeMock), State: string(StateMock)})
	if got != "wecom: mock  idle" {
		t.Fatalf("StatusLine = %q", got)
	}
}

func TestStatusLineWithTraffic(t *testing.T) {
	ts := time.Date(2026, 9, 28, 12, 3, 44, 0, time.Local)
	got := StatusLine(Snapshot{
		Enabled:      true,
		Mode:         string(ModeMock),
		State:        string(StateMock),
		Counts:       Counts{Inbound: 1, Outbound: 2},
		LastActivity: ts,
	})
	for _, want := range []string{"mock", "in=1", "out=2", "last=12:03:44"} {
		if !strings.Contains(got, want) {
			t.Errorf("StatusLine = %q, missing %q", got, want)
		}
	}
}

func TestStatusLineConnected(t *testing.T) {
	got := StatusLine(Snapshot{
		Enabled: true,
		Mode:    string(ModeLive),
		State:   string(StateConnected),
		Counts:  Counts{Inbound: 3, Outbound: 3},
	})
	if !strings.HasPrefix(got, "wecom: connected") {
		t.Fatalf("StatusLine = %q, want it to lead with the connected state", got)
	}
}

func TestStatusLineFallsBackWhenStateIsEmpty(t *testing.T) {
	got := StatusLine(Snapshot{Enabled: true, Mode: string(ModeLive)})
	if !strings.Contains(got, string(StateDisconnected)) {
		t.Fatalf("StatusLine = %q, want the disconnected fallback", got)
	}
}

func TestStatusLineIsOneLine(t *testing.T) {
	got := StatusLine(Snapshot{
		Enabled:      true,
		Mode:         string(ModeLive),
		State:        string(StateConnected),
		Counts:       Counts{Inbound: 9, Outbound: 9},
		LastActivity: time.Now(),
	})
	if strings.Contains(got, "\n") {
		t.Fatalf("StatusLine = %q, must stay on one terminal row", got)
	}
}
