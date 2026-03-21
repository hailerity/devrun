package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/procet/internal/client"
	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/ipc"
)

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

	c        *client.Client
	registry *config.Registry
	logDir   string

	spinFrame int
	spinning  bool

	cb clipboard
}

func newModel(c *client.Client, reg *config.Registry, logDir string, cb clipboard) model {
	return model{
		logsC:    newLogsPanel(),
		c:        c,
		registry: reg,
		logDir:   logDir,
		cb:       cb,
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
		sidebarW := 22
		mainW := m.width - sidebarW - 1
		bodyH := m.height - 2
		m.logsC.resize(mainW, bodyH-2)
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
		changed := m.logsC.poll()
		if changed {
			m.logsC.rebuildViewport()
		}
		return m, tickLog()

	case spinTickMsg:
		if m.spinning {
			m.spinFrame++
		}
		m.footerC.tick(100 * time.Millisecond)
		return m, tickSpin()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Left):
		m.focus = focusSidebar

	case key.Matches(msg, keys.Right):
		m.focus = focusMain

	case key.Matches(msg, keys.Tab):
		if m.focus == focusSidebar {
			m.focus = focusMain
		} else {
			if m.activeTab == tabLogs {
				m.activeTab = tabDetails
			} else {
				m.activeTab = tabLogs
			}
		}

	case key.Matches(msg, keys.Up):
		if m.focus == focusSidebar {
			m.sidebarC.moveUp()
			m.updateLogFile()
		} else if m.activeTab == tabLogs {
			m.logsC.moveUp()
			m.logsC.rebuildViewport()
		}

	case key.Matches(msg, keys.Down):
		if m.focus == focusSidebar {
			m.sidebarC.moveDown()
			m.updateLogFile()
		} else if m.activeTab == tabLogs {
			m.logsC.moveDown()
			m.logsC.rebuildViewport()
		}

	case key.Matches(msg, keys.Top):
		if m.activeTab == tabLogs {
			m.logsC.cursor = 0
			m.logsC.followMode = false
			m.logsC.vp.GotoTop()
			m.logsC.rebuildViewport()
		}

	case key.Matches(msg, keys.Bottom):
		if m.activeTab == tabLogs {
			m.logsC.cursor = len(m.logsC.lines) - 1
			m.logsC.followMode = true
			m.logsC.vp.GotoBottom()
			m.logsC.rebuildViewport()
		}

	case key.Matches(msg, keys.Follow):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.followMode = !m.logsC.followMode
		}

	case key.Matches(msg, keys.Visual):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.enterVisual()
			m.logsC.rebuildViewport()
		}

	case key.Matches(msg, keys.Escape):
		m.logsC.exitVisual()
		m.logsC.rebuildViewport()

	case key.Matches(msg, keys.Copy):
		if m.focus == focusMain && m.activeTab == tabLogs {
			var text string
			if m.logsC.visualMode {
				text = m.logsC.copySelection()
				m.logsC.exitVisual()
				m.logsC.rebuildViewport()
			} else {
				text = m.logsC.copyLine()
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
	if m.c == nil {
		return tickDaemon()
	}
	return func() tea.Msg {
		resp, err := m.c.Send("list", struct{}{})
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
	}
}

func (m model) doStart() tea.Cmd {
	if m.c == nil {
		return nil
	}
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return nil
	}
	name := svc.Name
	return func() tea.Msg {
		resp, err := m.c.Send("start", ipc.StartPayload{Name: name})
		if err != nil {
			return daemonErrMsg{err}
		}
		if !resp.OK {
			return daemonErrMsg{fmt.Errorf("%s", resp.Error)}
		}
		return daemonTickMsg{}
	}
}

func (m model) doStop() tea.Cmd {
	if m.c == nil {
		return nil
	}
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return nil
	}
	name := svc.Name
	return func() tea.Msg {
		resp, err := m.c.Send("stop", ipc.StopPayload{Name: name})
		if err != nil {
			return daemonErrMsg{err}
		}
		if !resp.OK {
			return daemonErrMsg{fmt.Errorf("%s", resp.Error)}
		}
		return daemonTickMsg{}
	}
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	sidebarW := 22
	mainW := m.width - sidebarW - 1
	bodyH := m.height - 2

	// Header
	total := len(m.sidebarC.services)
	running := 0
	for _, s := range m.sidebarC.services {
		if s.State == "running" {
			running++
		}
	}
	header := m.headerC.render(total, running, m.spinFrame, m.spinning, m.width)

	// Sidebar — render takes (width, height) only, no focused param
	sb := m.sidebarC.render(sidebarW, bodyH)

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
	footer := m.footerC.render(m.activeTab, m.focus, m.logsC.visualMode, m.width)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// Run starts the procet TUI. Called from cli/root.go.
// c must already be connected; caller owns c.Close().
func Run(c *client.Client, reg *config.Registry, logDir string) error {
	cb := detectClipboard()
	m := newModel(c, reg, logDir, cb)
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
	if m.activeTab == tabLogs && m.logsC.followMode {
		followIndicator = styleMuted.Render("  ● follow")
	}
	tabBar := logsLabel + "  " + detailsLabel + followIndicator

	contentH := h - 2

	var content string
	if m.activeTab == tabLogs {
		content = m.logsC.vp.View()
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
