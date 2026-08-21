package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zsuroy/dockerview-go/internal/docker"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	dockerclient "github.com/docker/docker/client"
)

type model struct {
	mu         sync.RWMutex
	containers []docker.ContainerInfo
	err        error

	cursor       int
	actionMode   bool
	logsMode     bool
	logs         []string
	execMode     bool
	execInput    string
	execResult   *docker.ExecResult
	execErr      error
	execRunning  bool
	statusMsg    string
	statusTimer  *time.Timer
	dockerClient *dockerclient.Client

	termWidth int
}

type tickMsg struct {
	time.Time
}

type opResultMsg struct {
	err error
	op  string
}

type logsMsg struct {
	lines []string
	err   error
}

type execMsg struct {
	result docker.ExecResult
	err    error
}

var (
	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444444")).
			Padding(1, 2)

	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFA500"))

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00D9FF"))

	styleSubtitle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	styleID        = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D9FF")).Width(14)
	styleName      = lipgloss.NewStyle().Width(22)
	styleMemory    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D9FF")).Width(10)
	styleBlkio     = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D9FF")).Width(18)
	styleNetwork   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Width(18)
	styleCPUOk     = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Width(8)
	styleCPUHot    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444")).Width(8)
	styleStatusOk  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Width(24)
	styleStatusBad = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444")).Width(24)

	styleError = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	styleEmpty = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	styleCursor = lipgloss.NewStyle().Background(lipgloss.Color("#005F87")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)

	styleActionBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D9FF")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#333333")).
			Padding(0, 1)

	styleStatus = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFA500")).
			Italic(true)

	styleLogs = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CCCCCC")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#333333")).
			Padding(1)

	styleExecStderr = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	styleExecMeta   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true)
)

func (m *model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg{t}
	})
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		return m, nil

	case tickMsg:
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return tickMsg{t}
		})

	case opResultMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Error: %v", msg.err)
		} else {
			m.statusMsg = fmt.Sprintf("Done: %s", msg.op)
		}
		m.actionMode = false
		m.statusTimer = time.AfterFunc(3*time.Second, func() {
			m.mu.Lock()
			m.statusMsg = ""
			m.mu.Unlock()
		})
		return m, nil

	case logsMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Logs error: %v", msg.err)
			m.statusTimer = time.AfterFunc(3*time.Second, func() {
				m.mu.Lock()
				m.statusMsg = ""
				m.mu.Unlock()
			})
		} else {
			m.logs = msg.lines
			m.logsMode = true
		}
		return m, nil

	case execMsg:
		m.execRunning = false
		if msg.err != nil {
			m.execErr = msg.err
			m.execResult = nil
		} else {
			m.execErr = nil
			m.execResult = &msg.result
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyUp:
			if !m.logsMode && !m.actionMode && !m.execMode {
				if m.cursor > 0 {
					m.cursor--
				}
			}
		case tea.KeyDown:
			if !m.logsMode && !m.actionMode && !m.execMode {
				m.mu.RLock()
				max := len(m.containers)
				m.mu.RUnlock()
				if m.cursor < max-1 {
					m.cursor++
				}
			}
		case tea.KeyEnter:
			if m.execMode {
				if m.execRunning {
					return m, nil
				}
				if m.execResult != nil || m.execErr != nil {
					m.execResult = nil
					m.execErr = nil
					return m, nil
				}
				cmd := strings.TrimSpace(m.execInput)
				if cmd == "" {
					return m, nil
				}
				m.execRunning = true
				return m, m.runExec(cmd)
			}
			if !m.logsMode {
				m.actionMode = !m.actionMode
			}
		case tea.KeyEsc:
			if m.execMode {
				m.execMode = false
				m.execInput = ""
				m.execResult = nil
				m.execErr = nil
				m.execRunning = false
			} else if m.logsMode {
				m.logsMode = false
				m.logs = nil
			} else if m.actionMode {
				m.actionMode = false
			}
		default:
			if m.execMode {
				if m.execRunning {
					return m, nil
				}
				if m.execResult != nil || m.execErr != nil {
					switch msg.String() {
					case "q":
						m.execMode = false
						m.execInput = ""
						m.execResult = nil
						m.execErr = nil
					}
					return m, nil
				}
				switch {
				case isBackspaceKey(msg):
					if len(m.execInput) > 0 {
						m.execInput = m.execInput[:len(m.execInput)-1]
					}
				case msg.Type == tea.KeySpace:
					m.execInput += " "
				case msg.Type == tea.KeyRunes:
					m.execInput += string(msg.Runes)
				default:
					switch msg.String() {
					case "q":
						m.execMode = false
						m.execInput = ""
						m.execResult = nil
						m.execErr = nil
					}
				}
				return m, nil
			}
			if m.logsMode {
				switch msg.String() {
				case "q":
					m.logsMode = false
					m.logs = nil
				}
				return m, nil
			}
			if m.actionMode {
				switch msg.String() {
				case "q":
					m.actionMode = false
				case "s":
					return m, m.doContainerOp("start")
				case "x":
					return m, m.doContainerOp("stop")
				case "r":
					return m, m.doContainerOp("restart")
				case "l":
					return m, m.fetchLogs()
				case "e":
					m.mu.RLock()
					canExec := m.cursor < len(m.containers) &&
						!strings.Contains(strings.ToLower(m.containers[m.cursor].Status), "exit")
					m.mu.RUnlock()
					if !canExec {
						m.statusMsg = "Error: container is not running"
						m.statusTimer = time.AfterFunc(3*time.Second, func() {
							m.mu.Lock()
							m.statusMsg = ""
							m.mu.Unlock()
						})
						return m, nil
					}
					m.actionMode = false
					m.execMode = true
					m.execInput = ""
					m.execResult = nil
					m.execErr = nil
					m.execRunning = false
				}
			}
		}
	}
	return m, nil
}

func (m *model) doContainerOp(op string) tea.Cmd {
	return func() tea.Msg {
		m.mu.RLock()
		if m.cursor >= len(m.containers) {
			m.mu.RUnlock()
			return opResultMsg{err: fmt.Errorf("no container selected")}
		}
		id := m.containers[m.cursor].FullID
		cli := m.dockerClient
		m.mu.RUnlock()

		if cli == nil {
			return opResultMsg{err: fmt.Errorf("docker client not available")}
		}

		err := docker.ContainerOp(context.Background(), cli, id, op)
		return opResultMsg{err: err, op: op}
	}
}

func (m *model) fetchLogs() tea.Cmd {
	return func() tea.Msg {
		m.mu.RLock()
		if m.cursor >= len(m.containers) {
			m.mu.RUnlock()
			return logsMsg{err: fmt.Errorf("no container selected")}
		}
		id := m.containers[m.cursor].FullID
		cli := m.dockerClient
		m.mu.RUnlock()

		if cli == nil {
			return logsMsg{err: fmt.Errorf("docker client not available")}
		}

		reader, err := docker.GetContainerLogs(context.Background(), cli, id, "100")
		if err != nil {
			return logsMsg{err: err}
		}
		defer reader.Close()

		buf := make([]byte, 64*1024)
		var content []byte
		for {
			n, readErr := reader.Read(buf)
			if n > 0 {
				content = append(content, buf[:n]...)
			}
			if readErr != nil {
				break
			}
		}

		lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
		return logsMsg{lines: lines}
	}
}

func (m *model) runExec(cmd string) tea.Cmd {
	return func() tea.Msg {
		m.mu.RLock()
		if m.cursor >= len(m.containers) {
			m.mu.RUnlock()
			return execMsg{err: fmt.Errorf("no container selected")}
		}
		id := m.containers[m.cursor].FullID
		cli := m.dockerClient
		m.mu.RUnlock()

		if cli == nil {
			return execMsg{err: fmt.Errorf("docker client not available")}
		}

		result, err := docker.ContainerExec(context.Background(), cli, id, []string{"sh", "-c", cmd})
		return execMsg{result: result, err: err}
	}
}

func (m *model) View() string {
	if m.execMode {
		return m.viewExec()
	}
	if m.logsMode {
		return m.viewLogs()
	}

	m.mu.RLock()
	containers := m.containers
	err := m.err
	m.mu.RUnlock()

	title := styleTitle.Render("DockerView Monitor " + Version)
	subtitle := styleSubtitle.Render("Press Ctrl+C to exit | ↑↓ Select | Enter Actions")

	// Column plan adapts to the live terminal width: narrow terminals drop
	// the Storage column first, then Network, so the table never overflows.
	cols := tableColumns(m.contentWidth())
	header := lipgloss.JoinHorizontal(lipgloss.Top, cols.header()...)

	var rows []string
	for i, c := range containers {

		cpuVal, _ := strconv.ParseFloat(strings.TrimSuffix(c.CPU, "%"), 64)

		cpuStyle := styleCPUOk
		if cpuVal >= 50 {
			cpuStyle = styleCPUHot
		}

		statusStyle := styleStatusOk
		if strings.Contains(strings.ToLower(c.Status), "exit") {
			statusStyle = styleStatusBad
		}

		row := lipgloss.JoinHorizontal(lipgloss.Top, cols.row(c, cpuStyle, statusStyle)...)

		if i == m.cursor {
			row = styleCursor.Render(row)
		}

		rows = append(rows, row)
	}

	if len(rows) == 0 {
		if err != nil {
			rows = append(rows, styleError.Render("Error: "+err.Error()))
		} else {
			rows = append(rows, styleEmpty.Render("No containers running"))
		}
	}

	content := fmt.Sprintf("%s\n%s\n\n%s\n%s\n%s",
		title,
		subtitle,
		header,
		strings.Repeat("─", cols.total()),
		strings.Join(rows, "\n"),
	)

	if m.actionMode {
		m.mu.RLock()
		var selectedName string
		if m.cursor < len(m.containers) {
			selectedName = m.containers[m.cursor].Name
		}
		m.mu.RUnlock()

		actionBar := styleActionBar.Render(
			fmt.Sprintf(" %s | [S]tart  [X]Stop  [R]estart  [L]ogs  [E]xec  [Q]uit ",
				lipgloss.NewStyle().Bold(true).Render(selectedName)),
		)
		content += "\n\n" + actionBar
	}

	if m.statusMsg != "" {
		content += "\n" + styleStatus.Render(m.statusMsg)
	}

	return styleBorder.Render(content)
}

func (m *model) viewLogs() string {
	m.mu.RLock()
	var name string
	if m.cursor < len(m.containers) {
		name = m.containers[m.cursor].Name
	}
	m.mu.RUnlock()

	title := styleTitle.Render("Logs: " + name)
	subtitle := styleSubtitle.Render("Press q or Esc to return")

	var logContent string
	if len(m.logs) == 0 {
		logContent = styleEmpty.Render("No log output")
	} else {
		// Show last 40 lines
		start := 0
		if len(m.logs) > 40 {
			start = len(m.logs) - 40
		}
		logContent = strings.Join(m.logs[start:], "\n")
	}

	return styleLogs.Render(
		fmt.Sprintf("%s\n%s\n\n%s", title, subtitle, logContent),
	)
}

func (m *model) viewExec() string {
	m.mu.RLock()
	var name string
	if m.cursor < len(m.containers) {
		name = m.containers[m.cursor].Name
	}
	m.mu.RUnlock()

	title := styleTitle.Render("Exec: " + name)

	var body string
	switch {
	case m.execRunning:
		subtitle := styleSubtitle.Render("Executing command...")
		body = styleEmpty.Render("Please wait")
		return styleLogs.Render(fmt.Sprintf("%s\n%s\n\n%s", title, subtitle, body))
	case m.execErr != nil:
		subtitle := styleSubtitle.Render("Press Enter to retry | q or Esc to return")
		body = styleError.Render(fmt.Sprintf("Error: %v", m.execErr))
		return styleLogs.Render(fmt.Sprintf("%s\n%s\n\n%s", title, subtitle, body))
	case m.execResult != nil:
		subtitle := styleSubtitle.Render(fmt.Sprintf("Exit code: %d | Enter new command | q or Esc to return", m.execResult.ExitCode))
		var parts []string
		parts = append(parts, styleExecMeta.Render(fmt.Sprintf("$ %s", m.execInput)))
		if m.execResult.Stdout != "" {
			parts = append(parts, m.execResult.Stdout)
		}
		if m.execResult.Stderr != "" {
			parts = append(parts, styleExecStderr.Render(m.execResult.Stderr))
		}
		if m.execResult.Stdout == "" && m.execResult.Stderr == "" {
			parts = append(parts, styleEmpty.Render("(no output)"))
		}
		body = strings.Join(parts, "\n\n")
		return styleLogs.Render(fmt.Sprintf("%s\n%s\n\n%s", title, subtitle, body))
	default:
		subtitle := styleSubtitle.Render("Type command and press Enter | q or Esc to cancel")
		prompt := fmt.Sprintf("$ %s_", m.execInput)
		body = prompt
		return styleLogs.Render(fmt.Sprintf("%s\n%s\n\n%s", title, subtitle, body))
	}
}
