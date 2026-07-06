package main

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func SetColor() {
	if os.Getenv("TERM") != "xterm-256color" {
		os.Setenv("TERM", "xterm-256color")
	}
}

// isBackspaceKey reports whether a key should delete the previous input character.
// Terminals differ: Linux often sends DEL (127), while Windows clients such as
// MobaXterm over SSH frequently send BS (Ctrl+H, 8) instead.
func isBackspaceKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete:
		return true
	case tea.KeyRunes:
		if len(msg.Runes) == 1 {
			switch msg.Runes[0] {
			case '\b', 127:
				return true
			}
		}
	}
	return false
}
