package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestIsBackspaceKey(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyMsg
		want bool
	}{
		{name: "backspace del", msg: tea.KeyMsg{Type: tea.KeyBackspace}, want: true},
		{name: "ctrl+h bs", msg: tea.KeyMsg{Type: tea.KeyCtrlH}, want: true},
		{name: "delete key", msg: tea.KeyMsg{Type: tea.KeyDelete}, want: true},
		{name: "del rune", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{127}}, want: true},
		{name: "bs rune", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\b'}}, want: true},
		{name: "regular rune", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, want: false},
		{name: "enter", msg: tea.KeyMsg{Type: tea.KeyEnter}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBackspaceKey(tt.msg); got != tt.want {
				t.Fatalf("isBackspaceKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateANSI(t *testing.T) {
	if got := truncateANSI("hello", 10); got != "hello" {
		t.Fatalf("short string mutated: %q", got)
	}
	if got := truncateANSI("hello world", 8); ansi.StringWidth(got) > 8 || !strings.HasSuffix(got, "…") {
		t.Fatalf("want ellipsized <=8 cells, got %q (%d cells)", got, ansi.StringWidth(got))
	}
	if got := truncateANSI("\x1b[31mred text\x1b[0m", 5); ansi.StringWidth(got) > 5 {
		t.Fatalf("ANSI sequence broken: %q", got)
	}
}

func TestTableColumnsFitWidth(t *testing.T) {
	for _, w := range []int{40, 50, 60, 78, 80, 96, 100, 114, 160} {
		l := tableColumns(w)
		if got := l.total(); got > w {
			t.Fatalf("width=%d: table needs %d cells", w, got)
		}
	}
	// Full-width terminal keeps every column.
	full := tableColumns(120)
	if !full.showStorage || !full.showNetwork {
		t.Fatal("wide terminal dropped columns")
	}
	// Very narrow terminal still renders a usable table.
	tiny := tableColumns(50)
	if tiny.name < 8 {
		t.Fatalf("narrow name column too small: %d", tiny.name)
	}
}
