package main

import (
	"strings"
	"testing"
)

// TestViewRendersTheWeComRow covers the one thing the TUI is allowed to do with
// WeCom: show a read-only status row. It never injects, reconfigures, or
// confirms a write.
func TestViewRendersTheWeComRow(t *testing.T) {
	m := &model{
		termWidth: 120,
		wecomLine: func() string { return "wecom: mock  in=1 out=1  last=12:03:44" },
	}

	out := m.View()
	if !strings.Contains(out, "wecom: mock") {
		t.Fatalf("View output does not contain the WeCom row:\n%s", out)
	}
	if !strings.Contains(out, "in=1 out=1") {
		t.Fatalf("View output dropped the counters:\n%s", out)
	}
}

func TestViewHidesTheWeComRowWhenAbsent(t *testing.T) {
	m := &model{termWidth: 120}
	if out := m.View(); strings.Contains(out, "wecom:") {
		t.Fatalf("View should not mention wecom when no line is installed:\n%s", out)
	}

	m.wecomLine = func() string { return "" }
	if out := m.View(); strings.Contains(out, "wecom:") {
		t.Fatalf("View should skip an empty wecom row:\n%s", out)
	}
}
