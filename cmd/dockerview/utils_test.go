package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
