package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/client"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
)

// dial opens a fresh connection to the daemon and executes fn, closing on return.
// The daemon handles one request per connection, so callers must not reuse connections.
func dial(socketPath string, fn func(*client.Client) tea.Msg) tea.Msg {
	c, err := client.Connect(socketPath)
	if err != nil {
		return daemonErrMsg{err}
	}
	defer c.Close()
	return fn(c)
}

type tabKind int

const (
	tabLogs    tabKind = iota
	tabDetails
)

type focusKind int

const (
	focusSidebar focusKind = iota
	focusMain
)

// --- Message types ---

type daemonTickMsg struct{}
type logTickMsg    struct{}
type spinTickMsg   struct{}
type daemonRespMsg struct{ payload ipc.ListResponsePayload }
type daemonErrMsg  struct{ err error }

// --- Model ---

type model struct {
	width  int
	height int

	focus     focusKind
	activeTab tabKind

	sidebarC sidebar
	logsC    logsPanel
	detailsC detailsPanel
	headerC  headerBar
	footerC  footerBar

	socketPath string
	registry   *config.Registry
	logDir     string

	spinFrame int
	spinning  bool

	cb clipboard
}

func newModel(socketPath string, reg *config.Registry, logDir string, cb clipboard) model {
	return model{
		logsC:      newLogsPanel(),
		socketPath: socketPath,
		registry:   reg,
		logDir:     logDir,
		cb:         cb,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickDaemon(),
		tickLog(),
		tickSpin(),
	)
}

func tickDaemon() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return daemonTickMsg{} })
}

func tickLog() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return logTickMsg{} })
}

func tickSpin() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinTickMsg{} })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		sidebarW := 26
		mainW := m.width - sidebarW - 1
		bodyH := m.height - 4 // header(2) + footer(2) = 4 reserved rows
		m.logsC.sb.resize(mainW, bodyH-2)
		return m, nil

	case daemonTickMsg:
		m.spinning = true
		return m, m.pollDaemon()

	case daemonRespMsg:
		m.spinning = false
		m.sidebarC.update(msg.payload.Services)
		if svc := m.sidebarC.selectedService(); svc != nil {
			path := filepath.Join(m.logDir, "logs", svc.Name+".log")
			if path != m.logsC.filePath {
				m.logsC.setFile(path)
			}
		}
		return m, tickDaemon()

	case daemonErrMsg:
		m.spinning = false
		m.footerC.showToastLong(fmt.Sprintf("error: %s", msg.err))
		return m, tickDaemon()

	case logTickMsg:
		m.logsC.poll()
		return m, tickLog()

	case spinTickMsg:
		if m.spinning {
			m.spinFrame++
		}
		m.footerC.tick(100 * time.Millisecond)
		return m, tickSpin()

	case tea.MouseMsg:
		if m.activeTab == tabLogs {
			// topOffset=4: header(2 rows) + tab-bar label+border(2 rows) = 4 rows above log content.
			// leftOffset=27: sidebarW(26) + divider(1); reserved for future character-level selection.
			_ = m.logsC.sb.handleMouse(msg, 4, 27)
			// A left-click in the log area auto-focuses the main panel so that
			// keyboard shortcuts (y to copy, v to select, f to follow) work immediately.
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				m.focus = focusMain
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	// ctrl+c with an active visual selection copies instead of quitting.
	// This handles Cmd+C on macOS and Ctrl+Shift+C on Ubuntu when the
	// terminal forwards them as ctrl+c to the running process.
	case msg.Type == tea.KeyCtrlC &&
		m.focus == focusMain &&
		m.activeTab == tabLogs &&
		m.logsC.sb.visualMode:
		text := m.logsC.sb.copySelection()
		m.logsC.sb.exitVisual()
		if !m.cb.Available() {
			m.footerC.showToast("No clipboard available")
		} else if err := m.cb.Copy(text); err != nil {
			m.footerC.showToastLong("Copy failed")
		} else {
			m.footerC.showToast("Copied!")
		}

	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Left):
		m.focus = focusSidebar

	case key.Matches(msg, keys.Right):
		m.focus = focusMain

	case key.Matches(msg, keys.Tab):
		switch {
		case m.focus == focusSidebar:
			m.focus = focusMain
			m.activeTab = tabLogs
		case m.focus == focusMain && m.activeTab == tabLogs:
			m.activeTab = tabDetails
		case m.focus == focusMain && m.activeTab == tabDetails:
			m.focus = focusSidebar
		}

	case key.Matches(msg, keys.Up):
		if m.focus == focusSidebar {
			m.sidebarC.moveUp()
			m.updateLogFile()
		} else if m.activeTab == tabLogs {
			m.logsC.sb.moveUp()
		}

	case key.Matches(msg, keys.Down):
		if m.focus == focusSidebar {
			m.sidebarC.moveDown()
			m.updateLogFile()
		} else if m.activeTab == tabLogs {
			m.logsC.sb.moveDown()
		}

	case key.Matches(msg, keys.Top):
		if m.activeTab == tabLogs {
			m.logsC.sb.gotoTop()
		}

	case key.Matches(msg, keys.Bottom):
		if m.activeTab == tabLogs {
			m.logsC.sb.gotoBottom()
		}

	case key.Matches(msg, keys.Follow):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.sb.followMode = !m.logsC.sb.followMode
		}

	case key.Matches(msg, keys.Visual):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.sb.enterVisual()
		}

	case key.Matches(msg, keys.Escape):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.sb.exitVisual()
		}

	case key.Matches(msg, keys.Copy):
		if m.focus == focusMain && m.activeTab == tabLogs {
			var text string
			if m.logsC.sb.visualMode {
				text = m.logsC.sb.copySelection()
				m.logsC.sb.exitVisual()
			} else {
				text = m.logsC.sb.copyLine()
			}
			if !m.cb.Available() {
				m.footerC.showToast("No clipboard available")
			} else if err := m.cb.Copy(text); err != nil {
				m.footerC.showToastLong("Copy failed")
			} else {
				m.footerC.showToast("Copied!")
			}
		}

	case key.Matches(msg, keys.Start):
		return m, m.doStart()

	case key.Matches(msg, keys.Stop):
		return m, m.doStop()
	}

	return m, nil
}

func (m *model) updateLogFile() {
	if svc := m.sidebarC.selectedService(); svc != nil {
		path := filepath.Join(m.logDir, "logs", svc.Name+".log")
		if path != m.logsC.filePath {
			m.logsC.setFile(path)
		}
	}
}

func (m model) pollDaemon() tea.Cmd {
	if m.socketPath == "" {
		return tickDaemon()
	}
	sp := m.socketPath
	return func() tea.Msg {
		return dial(sp, func(c *client.Client) tea.Msg {
			resp, err := c.Send("list", struct{}{})
			if err != nil {
				return daemonErrMsg{err}
			}
			if !resp.OK {
				return daemonErrMsg{fmt.Errorf("%s", resp.Error)}
			}
			var payload ipc.ListResponsePayload
			if err := json.Unmarshal(resp.Payload, &payload); err != nil {
				return daemonErrMsg{err}
			}
			return daemonRespMsg{payload}
		})
	}
}

func (m model) doStart() tea.Cmd {
	if m.socketPath == "" {
		return nil
	}
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return nil
	}
	sp, name := m.socketPath, svc.Name
	return func() tea.Msg {
		return dial(sp, func(c *client.Client) tea.Msg {
			resp, err := c.Send("start", ipc.StartPayload{Name: name})
			if err != nil {
				return daemonErrMsg{err}
			}
			if !resp.OK {
				return daemonErrMsg{fmt.Errorf("%s", resp.Error)}
			}
			return daemonTickMsg{}
		})
	}
}

func (m model) doStop() tea.Cmd {
	if m.socketPath == "" {
		return nil
	}
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return nil
	}
	sp, name := m.socketPath, svc.Name
	return func() tea.Msg {
		return dial(sp, func(c *client.Client) tea.Msg {
			resp, err := c.Send("stop", ipc.StopPayload{Name: name})
			if err != nil {
				return daemonErrMsg{err}
			}
			if !resp.OK {
				return daemonErrMsg{fmt.Errorf("%s", resp.Error)}
			}
			return daemonTickMsg{}
		})
	}
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	sidebarW := 26
	mainW := m.width - sidebarW - 1
	bodyH := m.height - 4 // header(2) + footer(2) = 4 reserved rows

	// Header
	total := len(m.sidebarC.services)
	running := 0
	for _, s := range m.sidebarC.services {
		if s.State == "running" {
			running++
		}
	}
	header := m.headerC.render(total, running, m.spinFrame, m.spinning, m.width)

	sb := m.sidebarC.render(sidebarW, bodyH, m.focus == focusSidebar)

	// Main panel (tabs + content)
	main := m.renderMain(mainW, bodyH)

	// Body: sidebar | divider | main
	divider := lipgloss.NewStyle().
		Width(1).
		Height(bodyH).
		BorderLeft(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Render("")

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(sidebarW).Height(bodyH).Render(sb),
		divider,
		lipgloss.NewStyle().Width(mainW).Height(bodyH).Render(main),
	)

	// Footer
	footer := m.footerC.render(m.activeTab, m.focus, m.logsC.sb.visualMode, m.width)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// Run starts the devrun TUI. Called from cli/root.go.
// The daemon must be running at socketPath; a fresh connection is dialed per request.
func Run(socketPath string, reg *config.Registry, logDir string) error {
	cb := detectClipboard()
	m := newModel(socketPath, reg, logDir, cb)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

func (m model) renderMain(w, h int) string {
	// Tab bar
	logsLabel := styleMuted.Render("LOGS")
	detailsLabel := styleMuted.Render("DETAILS")
	if m.activeTab == tabLogs {
		logsLabel = styleAccent.Underline(true).Render("LOGS")
	} else {
		detailsLabel = styleAccent.Underline(true).Render("DETAILS")
	}

	followIndicator := ""
	if m.activeTab == tabLogs && m.logsC.sb.followMode {
		followIndicator = styleMuted.Render("  ● follow")
	}
	tabBar := logsLabel + "  " + detailsLabel + followIndicator

	contentH := h - 2

	var content string
	if m.activeTab == tabLogs {
		content = m.logsC.view()
	} else {
		svc := m.sidebarC.selectedService()
		var cfg *config.ServiceConfig
		if svc != nil && m.registry != nil {
			cfg = m.registry.Services[svc.Name]
		}
		content = m.detailsC.render(svc, cfg, w, contentH)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().
			Width(w).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Render(tabBar),
		content,
	)
}
