package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
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
	tabLogs tabKind = iota
	tabDetails
)

type focusKind int

const (
	focusSidebar focusKind = iota
	focusMain
)

// --- Message types ---

type daemonTickMsg struct{}
type logTickMsg struct{}
type spinTickMsg struct{}
type daemonRespMsg struct{ payload ipc.ListResponsePayload }
type daemonErrMsg struct{ err error }

// --- Model ---

type model struct {
	width  int
	height int

	focus     focusKind
	activeTab tabKind

	sidebarC       sidebar
	logsC          logsPanel
	detailsC       detailsPanel
	targetDetailsC targetDetailsPanel
	editC          editPanel
	headerC        headerBar
	footerC        footerBar

	socketPath string
	registry   *config.Registry
	source     config.Source // where the registry was resolved from — the file service edits write back to
	logDir     string

	spinFrame int
	spinning  bool

	cb clipboard
}

func newModel(socketPath string, reg *config.Registry, src config.Source, logDir string, cb clipboard) model {
	return model{
		logsC:      newLogsPanel(),
		editC:      newEditPanel(),
		socketPath: socketPath,
		registry:   reg,
		source:     src,
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
		m.relayout()
		return m, nil

	case daemonTickMsg:
		m.spinning = true
		return m, m.pollDaemon()

	case daemonRespMsg:
		m.spinning = false
		m.sidebarC.update(m.scopedServices(msg.payload.Services), m.buildTargets(msg.payload.ActiveTargets))
		// The sidebar auto-sizes to the longest service name, so a changed
		// service list can shift the divider — re-flow the log panel.
		m.relayout()
		if svc := m.sidebarC.selectedService(); svc != nil {
			path := filepath.Join(m.logDir, "logs", svc.Name+".log")
			if path != m.logsC.filePath {
				m.logsC.setFile(path)
			}
		}
		return m, tickDaemon()

	case daemonErrMsg:
		m.spinning = false
		// The first poll resolved (unsuccessfully); stop showing "Loading…" and
		// fall back to the empty-state message alongside the error toast.
		m.sidebarC.loaded = true
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
		// Skip while a target roll-up occupies the main pane — there is no log
		// content under the cursor to select or focus.
		if m.activeTab == tabLogs && m.focusedTarget() == nil {
			// topOffset=4: header(2 rows) + tab-bar label+border(2 rows) = 4 rows above log content.
			// leftOffset: sidebar width + divider(1); reserved for future character-level selection.
			_ = m.logsC.sb.handleMouse(msg, 4, m.sidebarWidth()+1)
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
	// The edit modal is a keyboard trap: while open it consumes every key.
	if m.editC.open {
		return m.handleEditKey(msg)
	}

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

	// A focused target row shows a non-focusable roll-up in the main pane (like
	// DETAILS), so → and Tab-in are inert while the cursor sits on one —
	// otherwise focus would move to a logs pane the user cannot see and the
	// f/v/y shortcuts would run against hidden scrollback.
	case key.Matches(msg, keys.Right):
		if m.focusedTarget() == nil {
			m.focus = focusMain
			m.activeTab = tabLogs
		}

	// Tab toggles focus between the sidebar and the main panel. The main panel
	// is always LOGS — DETAILS is not focusable — so Tabbing in collapses it.
	case key.Matches(msg, keys.Tab):
		switch {
		case m.focusedTarget() != nil:
			m.focus = focusSidebar
		case m.focus == focusSidebar:
			m.focus = focusMain
			m.activeTab = tabLogs
		default:
			m.focus = focusSidebar
		}

	// Enter on a target row selects that target as the service filter (or
	// clears it if already selected) and returns to LOGS. Elsewhere it toggles
	// LOGS <-> DETAILS for the selected service. DETAILS is a read-only overlay,
	// not a focus target: focus stays on the sidebar so j/k keeps walking
	// services and the panel updates live. Ignored mid-selection.
	case key.Matches(msg, keys.Enter):
		switch {
		case m.onTargetRow():
			m.sidebarC.toggleTargetSelection()
			m.activeTab = tabLogs
			m.updateLogFile()
		case m.focusedTarget() != nil:
			// A target roll-up already fills the main pane — nothing to toggle.
		case m.activeTab == tabDetails:
			m.activeTab = tabLogs
		case m.activeTab == tabLogs && !m.logsC.sb.visualMode:
			m.focus = focusSidebar
			m.activeTab = tabDetails
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

	// Esc backs out one level: it cancels an active visual selection first,
	// otherwise it collapses DETAILS back to LOGS.
	case key.Matches(msg, keys.Escape):
		switch {
		case m.focus == focusMain && m.activeTab == tabLogs && m.logsC.sb.visualMode:
			m.logsC.sb.exitVisual()
		case m.activeTab == tabDetails:
			m.activeTab = tabLogs
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
		if t := m.sidebarC.selectedTarget(); t != nil {
			return m, m.doStartTarget()
		}
		return m, m.doStart()

	case key.Matches(msg, keys.Stop):
		if t := m.sidebarC.selectedTarget(); t != nil {
			return m, m.doStopTarget()
		}
		return m, m.doStop()

	// e opens the service editor for the highlighted service row.
	case key.Matches(msg, keys.Edit):
		if m.onServiceRow() {
			return m.openEditor()
		}
	}

	return m, nil
}

// onServiceRow reports whether the sidebar has focus with its cursor on a
// service row (not a target row) and a service is selected.
func (m model) onServiceRow() bool {
	return m.focus == focusSidebar &&
		(!m.sidebarC.hasTargets() || m.sidebarC.section == sectionServices) &&
		m.sidebarC.selectedService() != nil
}

// openEditor prefills the edit modal for the selected service.
func (m model) openEditor() (tea.Model, tea.Cmd) {
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return m, nil
	}
	var cfg *config.ServiceConfig
	if m.registry != nil {
		cfg = m.registry.Services[svc.Name]
	}
	if cfg == nil {
		cfg = &config.ServiceConfig{Name: svc.Name}
	}
	m.editC.openFor(svc.Name, cfg)
	return m, textinput.Blink
}

// handleEditKey routes a key to the open edit modal: Esc cancels, Enter saves,
// Tab / Shift-Tab move between fields, everything else goes to the focused input.
func (m model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.editC.close()
		return m, nil
	case tea.KeyEnter:
		return m.saveEditor()
	case tea.KeyTab:
		m.editC.focusDelta(1)
		return m, textinput.Blink
	case tea.KeyShiftTab:
		m.editC.focusDelta(-1)
		return m, textinput.Blink
	}
	cmd := m.editC.update(msg)
	return m, cmd
}

// serviceNames returns the set of all configured service names.
func (m model) serviceNames() map[string]bool {
	out := make(map[string]bool)
	if m.registry != nil {
		for name := range m.registry.Services {
			out[name] = true
		}
	}
	return out
}

// saveEditor validates the modal, persists the edit, updates the in-memory
// registry, and closes the modal. Validation or persistence errors keep the
// modal open with the error shown.
func (m model) saveEditor() (tea.Model, tea.Cmd) {
	if problem := m.editC.validate(m.serviceNames()); problem != "" {
		m.editC.errMsg = problem
		return m, nil
	}
	oldName := m.editC.origName
	name, command, cwd := m.editC.values()

	if err := config.SaveServiceEdit(m.source, oldName, name, command, cwd); err != nil {
		m.editC.errMsg = err.Error()
		return m, nil
	}
	wasRunning := m.serviceIsRunning(oldName)
	m.applyEditToRegistry(oldName, name, command, cwd)
	m.editC.close()

	if wasRunning {
		// A running service keeps its old definition (and, on rename, its old
		// name) until it is restarted — do that now so the edit takes effect.
		m.footerC.showToast("restarting " + name)
		return m, tea.Sequence(
			m.doRestartForEdit(oldName, name, m.registry.Services[name]),
			m.pollDaemon(),
		)
	}
	m.footerC.showToast("saved " + name)
	return m, m.pollDaemon()
}

// serviceIsRunning reports whether the sidebar's last daemon view shows the
// named service running.
func (m model) serviceIsRunning(name string) bool {
	for _, s := range m.sidebarC.allServices {
		if s.Name == name {
			return s.State == "running"
		}
	}
	return false
}

// doRestartForEdit stops oldName then starts newName with cfg, so an edit (or a
// rename) to a running service takes effect immediately. cfg is shipped inline
// like doStart, so a project service the daemon has not seen still starts.
func (m model) doRestartForEdit(oldName, newName string, cfg *config.ServiceConfig) tea.Cmd {
	if m.socketPath == "" {
		return nil
	}
	sp := m.socketPath
	return func() tea.Msg {
		// Best-effort stop of the old name — the daemon may already consider it
		// stopped (a stale sidebar view). Always proceed to start; a genuinely
		// unreachable daemon still surfaces through the start error below.
		_ = dial(sp, func(c *client.Client) tea.Msg {
			_, _ = c.Send("stop", ipc.StopPayload{Name: oldName})
			return nil
		})
		return dial(sp, func(c *client.Client) tea.Msg {
			resp, err := c.Send("start", ipc.StartPayload{Name: newName, Config: cfg})
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

// applyEditToRegistry mirrors the just-persisted edit into the in-memory
// registry so the sidebar reflects it before the next daemon poll. For a project
// source the cwd is resolved to absolute against the project dir, matching what
// ProjectConfig.ToServiceConfigs would produce on reload (and what the daemon
// needs on restart).
func (m *model) applyEditToRegistry(oldName, newName, command, cwd string) {
	if m.registry == nil {
		return
	}
	if m.source.IsLocal() {
		cwd = resolveProjectCWD(m.source.Dir, cwd)
	}
	cur := m.registry.Services[oldName]
	if cur == nil {
		cur = &config.ServiceConfig{}
	}
	updated := *cur
	updated.Name = newName
	updated.Command = command
	updated.CWD = cwd
	if newName != oldName {
		delete(m.registry.Services, oldName)
	}
	m.registry.Services[newName] = &updated
}

// resolveProjectCWD mirrors ProjectConfig.ToServiceConfigs: a project service's
// cwd is stored absolute in memory, resolved against the project dir (empty
// means the project root).
func resolveProjectCWD(dir, cwd string) string {
	if cwd == "" {
		return dir
	}
	if !filepath.IsAbs(cwd) {
		return filepath.Join(dir, cwd)
	}
	return cwd
}

// scopedServices restricts the daemon's full service list to the active config:
// the local devrun.yaml when one is present, otherwise the services registered
// directly in the global registry (project mirrors are already excluded by
// config.Resolve). Configured services the daemon has not reported are shown as
// "stopped". Only a nil registry (tests, no config context) passes through.
func (m model) scopedServices(all []ipc.ServiceInfo) []ipc.ServiceInfo {
	if m.registry == nil {
		return all
	}

	byName := make(map[string]ipc.ServiceInfo, len(all))
	for _, s := range all {
		byName[s.Name] = s
	}

	names := make([]string, 0, len(m.registry.Services))
	for name := range m.registry.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ipc.ServiceInfo, 0, len(names))
	for _, name := range names {
		s, ok := byName[name]
		if !ok {
			s = ipc.ServiceInfo{Name: name, State: string(config.StatusStopped)}
		}
		if cfg := m.registry.Services[name]; cfg != nil && cfg.Group != "" {
			s.Group = cfg.Group
		}
		out = append(out, s)
	}
	return out
}

// buildTargets turns the registry's target definitions into sidebar rows, with
// the synthetic "All services" row first. Returns nil when no targets are
// defined, which collapses the TARGETS block entirely.
func (m model) buildTargets(active []string) []sidebarTarget {
	if m.registry == nil || len(m.registry.Targets) == 0 {
		return nil
	}
	activeSet := make(map[string]bool, len(active))
	for _, a := range active {
		activeSet[a] = true
	}
	rows := []sidebarTarget{{name: ""}} // "All services"
	for _, name := range config.SortedTargetNames(m.registry.Targets) {
		rows = append(rows, sidebarTarget{
			name:    name,
			members: m.registry.Targets[name],
			active:  activeSet[name],
		})
	}
	return rows
}

// focusedTarget returns the highlighted target when the sidebar cursor sits on a
// real target row. The synthetic "All services" row (empty name) and a cursor in
// the SERVICES section both yield nil, leaving the main pane on logs.
func (m model) focusedTarget() *sidebarTarget {
	t := m.sidebarC.selectedTarget()
	if t == nil || t.name == "" {
		return nil
	}
	return t
}

// targetMemberInfos returns the daemon-reported ServiceInfo for each member of t
// that the daemon currently knows about, drawn from the registry-scoped service
// list (not the active-target filter). Order is unspecified — the sole caller
// keys the result by name.
func (m model) targetMemberInfos(t *sidebarTarget) []ipc.ServiceInfo {
	want := make(map[string]bool, len(t.members))
	for _, name := range t.members {
		want[name] = true
	}
	out := make([]ipc.ServiceInfo, 0, len(t.members))
	for _, svc := range m.sidebarC.allServices {
		if want[svc.Name] {
			out = append(out, svc)
		}
	}
	return out
}

func (m *model) updateLogFile() {
	if svc := m.sidebarC.selectedService(); svc != nil {
		path := filepath.Join(m.logDir, "logs", svc.Name+".log")
		if path != m.logsC.filePath {
			m.logsC.setFile(path)
		}
	}
}

// onTargetRow reports whether the sidebar has focus with its cursor parked on a
// target row — the state in which Enter selects a filter rather than toggling
// DETAILS. The sidebar's section persists after focus leaves it, so the focus
// check is what keeps Enter in the LOGS panel meaning "details".
func (m model) onTargetRow() bool {
	return m.focus == focusSidebar && m.sidebarC.section == sectionTargets && m.sidebarC.hasTargets()
}

const (
	sidebarMinW = 24
	sidebarMaxW = 40
)

// sidebarWidth is the sidebar column count: wide enough for the longest service
// name (plus the dot, a space, and a right margin), clamped to
// [sidebarMinW, sidebarMaxW] and never more than a third of the terminal.
func (m model) sidebarWidth() int {
	w := sidebarMinW
	for _, svc := range m.sidebarC.allServices {
		if n := lipgloss.Width(svc.Name) + 3; n > w {
			w = n
		}
	}
	for _, t := range m.sidebarC.targets {
		label := t.name
		if label == "" {
			label = allServicesLabel
		}
		if n := lipgloss.Width(label) + 4; n > w { // +1 vs services for the filter marker gutter
			w = n
		}
	}
	w = min(w, sidebarMaxW)
	if m.width > 0 {
		w = min(w, m.width/3)
	}
	return max(w, sidebarMinW/2)
}

// relayout recomputes derived geometry after a resize or a sidebar-width change
// and re-flows the log panel to the new main-area width.
func (m *model) relayout() {
	if m.width == 0 {
		return
	}
	mainW := m.width - m.sidebarWidth() - 1
	bodyH := m.height - 4 // header(2) + footer(2) = 4 reserved rows
	m.logsC.sb.resize(mainW, bodyH-2)
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
	// Ship the resolved definition inline so the daemon can start a project
	// devrun.yaml service it has never seen; nil for a registry service.
	var cfg *config.ServiceConfig
	if m.registry != nil {
		cfg = m.registry.Services[name]
	}
	return func() tea.Msg {
		return dial(sp, func(c *client.Client) tea.Msg {
			resp, err := c.Send("start", ipc.StartPayload{Name: name, Config: cfg})
			if err != nil {
				return daemonErrMsg{err}
			}
			if !resp.OK {
				if cfg != nil && strings.Contains(resp.Error, "not registered") {
					return daemonErrMsg{fmt.Errorf("daemon is an older build — run 'devrun daemon restart'")}
				}
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

// doStartTarget starts every service in the highlighted target. The "All
// services" row (empty name) is a no-op, as is a target with no runnable
// members. Member definitions are shipped inline so a project target works
// without a registry entry.
func (m model) doStartTarget() tea.Cmd {
	if m.socketPath == "" || m.registry == nil {
		return nil
	}
	t := m.sidebarC.selectedTarget()
	if t == nil || t.name == "" {
		return nil
	}
	members := m.registry.TargetMemberConfigs(t.name)
	if len(members) == 0 {
		return nil
	}
	sp, name := m.socketPath, t.name
	return func() tea.Msg {
		return dial(sp, func(c *client.Client) tea.Msg {
			resp, err := c.Send("target-start", ipc.TargetStartPayload{Name: name, Services: members})
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

// doStopTarget stops the highlighted target; the daemon keeps any member still
// held by another running target. "All services" is a no-op.
func (m model) doStopTarget() tea.Cmd {
	if m.socketPath == "" {
		return nil
	}
	t := m.sidebarC.selectedTarget()
	if t == nil || t.name == "" {
		return nil
	}
	sp, name := m.socketPath, t.name
	return func() tea.Msg {
		return dial(sp, func(c *client.Client) tea.Msg {
			resp, err := c.Send("target-stop", ipc.TargetStopPayload{Name: name})
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

	sidebarW := m.sidebarWidth()
	mainW := m.width - sidebarW - 1
	bodyH := m.height - 4 // header(2) + footer(2) = 4 reserved rows

	// Header — counts reflect the whole scoped project, not the target filter.
	total := len(m.sidebarC.allServices)
	running := 0
	for _, s := range m.sidebarC.allServices {
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

	// The edit modal takes over the body area while it is open.
	if m.editC.open {
		body = m.editC.view(m.width, bodyH)
	}

	// Footer
	footer := m.footerC.render(m.activeTab, m.focus, m.logsC.sb.visualMode, m.focusedTarget() != nil, m.onTargetRow(), m.onServiceRow(), m.editC.open, m.width)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// Run starts the devrun TUI. Called from cli/root.go.
// The daemon must be running at socketPath; a fresh connection is dialed per request.
// src is the config the registry was resolved from — the file the service editor writes back to.
func Run(socketPath string, reg *config.Registry, src config.Source, logDir string) error {
	cb := detectClipboard()
	m := newModel(socketPath, reg, src, logDir, cb)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

func (m model) renderMain(w, h int) string {
	// A focused target row replaces the LOGS/DETAILS view with a read-only
	// roll-up of that target and its member services.
	if t := m.focusedTarget(); t != nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().
				Width(w).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorBorder).
				Render(styleMuted.Render("TARGET")),
			m.targetDetailsC.render(t, m.targetMemberInfos(t), w, h-2),
		)
	}

	// Tab bar: only the active view's label is shown. LOGS is accented only
	// while the main panel holds focus; DETAILS is never accented — it is a
	// read-only overlay, not a focus target.
	var tabBar string
	if m.activeTab == tabLogs {
		if m.focus == focusMain {
			tabBar = styleAccent.Underline(true).Render("LOGS")
		} else {
			tabBar = styleMuted.Render("LOGS")
		}
		if m.logsC.sb.followMode {
			tabBar += styleMuted.Render("  ● follow")
		}
	} else {
		tabBar = styleMuted.Render("DETAILS")
	}

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
