package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zsuroy/dockerview-go/internal/docker"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	mu         sync.Mutex
	containers []docker.ContainerInfo
	err        error
}

type tickMsg struct {
	time.Time
}

func (m *model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg{t}
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return tickMsg{t}
		})
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *model) View() string {
	m.mu.Lock()
	containers := m.containers
	err := m.err
	m.mu.Unlock()

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#444444")).
		Padding(1, 2)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFA500"))

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00D9FF")).
		Render("DockerView Monitor " + Version)

	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Render("Press Ctrl+C to exit")

	colStyles := []lipgloss.Style{
		headerStyle,
		headerStyle,
		headerStyle,
		headerStyle,
		headerStyle,
		headerStyle,
	}

	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		colStyles[0].Width(14).Render("ID"),
		colStyles[1].Width(20).Render("Name"),
		colStyles[3].Width(8).Render("CPU"),
		colStyles[4].Width(10).Render("Memory"),
		colStyles[5].Width(18).Render("Network"),
		colStyles[2].Width(20).Render("Status"),
	)

	var rows []string
	for _, c := range containers {
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}

		name := c.Name
		if len(name) > 20 {
			name = name[:18] + ".."
		}

		status := c.Status
		if len(status) > 20 {
			status = status[:18] + ".."
		}

		cpuVal, _ := strconv.ParseFloat(strings.TrimSuffix(c.CPU, "%"), 64)

		var statusColor, cpuColor lipgloss.Color
		if strings.Contains(strings.ToLower(c.Status), "exit") {
			statusColor = lipgloss.Color("#FF4444")
		} else {
			statusColor = lipgloss.Color("#00FF00")
		}

		if cpuVal >= 50 {
			cpuColor = lipgloss.Color("#FF4444")
		} else {
			cpuColor = lipgloss.Color("#00FF00")
		}

		row := lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Foreground(lipgloss.Color("#00D9FF")).Width(14).Render(id),
			lipgloss.NewStyle().Width(20).Render(name),
			lipgloss.NewStyle().Foreground(cpuColor).Width(8).Render(c.CPU),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#00D9FF")).Width(10).Render(c.Memory),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#05f846")).Width(16).Render(c.Network),
			lipgloss.NewStyle().Foreground(statusColor).Width(20).Render(status),
		)
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		if err != nil {
			rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444")).Render("Error: "+err.Error()))
		} else {
			rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("No containers running"))
		}
	}

	content := fmt.Sprintf("%s\n%s\n\n%s\n%s\n%s",
		title,
		subtitle,
		header,
		strings.Repeat("─", 94),
		strings.Join(rows, "\n"),
	)

	return border.Render(content)
}
