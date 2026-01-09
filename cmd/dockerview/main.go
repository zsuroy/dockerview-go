package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"dockerview-go/internal/docker"

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

func main() {
	client, err := docker.NewClient()
	if err != nil {
		fmt.Printf("Failed to connect to Docker: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	m := &model{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				containers, err := docker.GetContainerStats(ctx, client)
				m.mu.Lock()
				m.containers = containers
				m.err = err
				m.mu.Unlock()
			}
		}
	}()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
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
		Render("DockerView Monitor")

	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Render("Press Ctrl+C to exit")

	colStyles := []lipgloss.Style{
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
		colStyles[2].Width(14).Render("Status"),
		colStyles[3].Width(8).Render("CPU"),
		colStyles[4].Width(10).Render("Memory"),
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
		if len(status) > 14 {
			status = status[:12] + ".."
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
			lipgloss.NewStyle().Foreground(statusColor).Width(14).Render(status),
			lipgloss.NewStyle().Foreground(cpuColor).Width(8).Render(c.CPU),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#00D9FF")).Width(10).Render(c.Memory),
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
		strings.Repeat("─", 64),
		strings.Join(rows, "\n"),
	)

	return border.Render(content)
}
