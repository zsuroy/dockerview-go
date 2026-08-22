package main

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/zsuroy/dockerview-go/internal/docker"
)

// contentWidth is the usable width inside the border and its padding.
func (m *model) contentWidth() int {
	const chrome = 6 // rounded border (2) + horizontal padding (4)
	w := m.termWidth - chrome
	if w < 40 {
		w = 40
	}
	return w
}

// truncateANSI cuts s to at most width display cells without breaking
// ANSI escape sequences or multi-byte runes.
func truncateANSI(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	if width <= 1 {
		return ""
	}
	return ansi.Truncate(s, width-1, "…")
}

// tableLayout is the resolved column plan for one render pass. Widths are
// display cells; showStorage/showNetwork let narrow terminals drop the
// least-critical columns instead of overflowing.
type tableLayout struct {
	id, name, cpu, memory, storage, network, status int
	showStorage, showNetwork                        bool
}

// tableColumns fits the container table into width cells. Full layout needs
// 114; below that Storage goes first, then Network; ID shrinks last.
func tableColumns(width int) tableLayout {
	l := tableLayout{id: 14, name: 22, cpu: 8, memory: 10, storage: 18, network: 18, status: 24}
	l.showStorage, l.showNetwork = true, true
	switch {
	case width >= 114:
	case width >= 96:
		l.showStorage = false
	case width >= 78:
		l.showStorage = false
		l.showNetwork = false
	default:
		l.showStorage = false
		l.showNetwork = false
		l.name = width - l.id - l.cpu - l.memory - l.status
		if l.name < 8 {
			// Squeeze the roomy columns before giving up on width.
			l.cpu, l.memory, l.status = 6, 8, 12
			l.name = width - l.id - l.cpu - l.memory - l.status
		}
		if l.name < 6 {
			l.name = 6
		}
		for l.total() > width && l.id > 8 {
			l.id--
		}
	}
	return l
}

// total is the rendered row width in cells (separator length).
func (l tableLayout) total() int {
	w := l.id + l.name + l.cpu + l.memory + l.status
	if l.showStorage {
		w += l.storage
	}
	if l.showNetwork {
		w += l.network
	}
	return w
}

// header renders the column titles with per-column styling.
func (l tableLayout) header() []string {
	parts := []string{
		styleHeader.Width(l.id).Render("ID"),
		styleHeader.Width(l.name).Render("Name"),
		styleHeader.Width(l.cpu).Render("CPU"),
		styleHeader.Width(l.memory).Render("Memory"),
	}
	if l.showStorage {
		parts = append(parts, styleHeader.Width(l.storage).Render("Storage"))
	}
	if l.showNetwork {
		parts = append(parts, styleHeader.Width(l.network).Render("Network"))
	}
	return append(parts, styleHeader.Width(l.status).Render("Status"))
}

// row renders one container line using the same plan as header().
func (l tableLayout) row(c docker.ContainerInfo, cpuStyle, statusStyle lipgloss.Style) []string {
	parts := []string{
		styleID.Width(l.id).Render(c.ID),
		styleName.Width(l.name).Render(truncateANSI(c.Name, l.name)),
		cpuStyle.Render(c.CPU),
		styleMemory.Render(c.Memory),
	}
	if l.showStorage {
		parts = append(parts, styleBlkio.Render(c.Blkio))
	}
	if l.showNetwork {
		parts = append(parts, styleNetwork.Render(c.Network))
	}
	return append(parts, statusStyle.Render(truncateANSI(c.Status, l.status)))
}

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
