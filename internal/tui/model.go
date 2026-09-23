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

// serviceRemovedMsg carries the daemon's verdict on a remove request back to the
// model: err set means the daemon refused (or was unreachable) and the config
// file has NOT been touched; err nil means it is safe to persist the deletion.
type serviceRemovedMsg struct {
	name string
	err  error
}

// --- Model ---

type model struct {
	width  int
	height int

	focus     focusKind
	activeTab tabKind

	sidebarC    sidebar
	logsC       logsPanel
	detailsC    detailsPanel
	editC       editPanel
	targetEditC targetEditPanel
	removeC     removeConfirm
	pickerC     targetPicker
	helpC       helpPanel

	searching bool            // the footer's search input has the keyboard
	searchC   textinput.Model // the `/` input; its value is committed to logsC.sb on Enter
	headerC   headerBar
	footerC   footerBar

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
		logsC:       newLogsPanel(),
		searchC:     newSearchInput(),
		editC:       newEditPanel(),
		targetEditC: newTargetEditPanel(),
		socketPath:  socketPath,
		registry:    reg,
		source:      src,
		logDir:      logDir,
		cb:          cb,
		// Init polls straight away, so a request is already in flight on the first
		// frame; both resolve branches clear this.
		spinning: true,
	}
}

func newSearchInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 128
	return ti
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		// Poll immediately rather than arming tickDaemon: the tick only schedules,
		// so waiting for it left the first 2s showing drawn-but-empty chrome even
		// when the daemon answers in milliseconds. Both the response and the error
		// branch re-arm tickDaemon, so this starts the same single poll chain a
		// tick would have — adding tickDaemon() here as well would run two.
		m.pollDaemon(),
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
		scoped := m.scopedServices(msg.payload.Services)
		m.sidebarC.update(scoped, m.buildTargets())
		// The sidebar auto-sizes to the longest service name, so a changed
		// service list can shift the divider — re-flow the log panel.
		m.relayout()
		m.updateLogFile()
		// A poll can add or remove rows (a port appears, the config changes);
		// keep the DETAILS cursor and window inside the new list.
		m.detailsC.scrollToCursor(m.detailLines())
		return m, tickDaemon()

	case serviceRemovedMsg:
		if !m.removeC.open || m.removeC.name != msg.name {
			// A stale reply arriving after the modal already closed (or moved on
			// to another service) — nothing to apply.
			return m, nil
		}
		m.removeC.pending = false
		if msg.err != nil {
			// The daemon refused the eviction or could not be reached — the
			// config file is untouched. Keep the modal open with the reason.
			m.removeC.errMsg = msg.err.Error()
			return m, nil
		}
		// The daemon has evicted the service (or none is configured); now it is
		// safe to delete the definition from disk.
		if err := config.RemoveService(m.source, msg.name); err != nil {
			m.removeC.errMsg = err.Error()
			return m, nil
		}
		if m.registry != nil {
			delete(m.registry.Services, msg.name)
		}
		m.removeC.close()
		m.footerC.showToast("removed " + msg.name)
		return m, m.pollDaemon()

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
		// A modal owns the screen: the log pane under it is hidden, so a click
		// or drag must not move focus there or start a selection.
		if m.modalOpen() {
			return m, nil
		}
		// In the narrow layout the sidebar may be the pane on screen; the log
		// pane is not drawn, so there is nothing under the pointer to select.
		if m.narrow() && m.focus == focusSidebar {
			return m, nil
		}
		if m.activeTab == tabLogs {
			// topOffset: header(1) + the main pane's top border(1) rows sit above
			// the log content. leftOffset: sidebar + the main pane's left border
			// and padding; reserved for future character-level selection.
			_, mainW := m.paneWidths()
			_ = m.logsC.sb.handleMouse(msg, headerRows+1, m.width-mainW+1+mainPadLeft)
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
	// The edit modals are keyboard traps: while open they consume every key.
	if m.editC.open {
		return m.handleEditKey(msg)
	}
	if m.targetEditC.open {
		return m.handleTargetEditKey(msg)
	}
	if m.removeC.open {
		return m.handleRemoveKey(msg)
	}
	if m.pickerC.open {
		return m.handlePickerKey(msg)
	}
	if m.searching {
		return m.handleSearchKey(msg)
	}
	if m.helpC.open {
		// Any of the keys a user would reach for closes it; nothing leaks to
		// the panes behind.
		switch {
		case msg.Type == tea.KeyCtrlC:
			return m, tea.Quit
		case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Help),
			key.Matches(msg, keys.Enter), msg.String() == "q":
			m.helpC.open = false
		}
		return m, nil
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

	case key.Matches(msg, keys.Right):
		m.focus = focusMain

	// Tab toggles focus between the sidebar and the main panel. The main panel
	// is always LOGS — DETAILS is not focusable — so Tabbing in collapses it.
	case key.Matches(msg, keys.Tab):
		if m.focus == focusSidebar {
			m.focus = focusMain
		} else {
			m.focus = focusSidebar
		}

	// Enter toggles LOGS <-> DETAILS for the selected service — the same on
	// every row, and without moving focus: from the sidebar j/k keeps walking
	// services while the view updates live; from the main pane the new view is
	// the one being driven. Ignored mid-selection.
	case key.Matches(msg, keys.Enter):
		switch {
		case m.narrow() && m.focus == focusSidebar:
			// One pane at a time: the view Enter would toggle is not on
			// screen, so Enter opens the selected service instead.
			m.focus = focusMain
		case m.activeTab == tabDetails:
			m.activeTab = tabLogs
		case m.activeTab == tabLogs && !m.logsC.sb.visualMode:
			m.activeTab = tabDetails
			m.detailsC.scrollToCursor(m.detailLines())
		}

	case key.Matches(msg, keys.Up):
		if m.focus == focusSidebar {
			m.sidebarC.moveUp()
			m.updateLogFile()
		} else if m.activeTab == tabLogs {
			m.logsC.sb.moveUp()
		} else {
			m.detailsC.move(-1, m.detailLines())
		}

	case key.Matches(msg, keys.Down):
		if m.focus == focusSidebar {
			m.sidebarC.moveDown()
			m.updateLogFile()
		} else if m.activeTab == tabLogs {
			m.logsC.sb.moveDown()
		} else {
			m.detailsC.move(1, m.detailLines())
		}

	case key.Matches(msg, keys.Top):
		if m.activeTab == tabLogs {
			m.logsC.sb.gotoTop()
		} else if m.focus == focusMain {
			m.detailsC.move(-len(m.detailLines()), m.detailLines())
		}

	case key.Matches(msg, keys.Bottom):
		if m.activeTab == tabLogs {
			m.logsC.sb.gotoBottom()
		} else if m.focus == focusMain {
			m.detailsC.move(len(m.detailLines()), m.detailLines())
		}

	case key.Matches(msg, keys.Follow):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.sb.setFollow(!m.logsC.sb.followMode)
		}

	case key.Matches(msg, keys.Wrap):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.sb.noWrap = !m.logsC.sb.noWrap
		}

	case key.Matches(msg, keys.Visual):
		if m.focus == focusMain && m.activeTab == tabLogs {
			m.logsC.sb.enterVisual()
		}

	// / opens the search input. It always lands in the log pane — searching
	// from the sidebar or from DETAILS means "search this service's log".
	case key.Matches(msg, keys.Search):
		if m.sidebarC.selectedService() == nil {
			break
		}
		m.focus = focusMain
		m.activeTab = tabLogs
		m.searching = true
		m.searchC.SetValue(m.logsC.sb.search.query)
		m.searchC.CursorEnd()
		m.searchC.Focus()
		return m, textinput.Blink

	case key.Matches(msg, keys.Next), key.Matches(msg, keys.Prev):
		if m.activeTab != tabLogs || !m.logsC.sb.search.active() {
			break
		}
		dir := 1
		if key.Matches(msg, keys.Prev) {
			dir = -1
		}
		if !m.logsC.sb.searchStep(dir) {
			m.footerC.showToast("no matches for " + m.logsC.sb.search.query)
		}

	// Esc backs out one level: it cancels an active visual selection first,
	// then clears an active search, otherwise it collapses DETAILS back to LOGS.
	case key.Matches(msg, keys.Escape):
		switch {
		case m.focus == focusMain && m.activeTab == tabLogs && m.logsC.sb.visualMode:
			m.logsC.sb.exitVisual()
		case m.activeTab == tabLogs && m.logsC.sb.search.active():
			m.logsC.sb.setQuery("")
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
		} else if m.focus == focusMain && m.activeTab == tabDetails {
			// In DETAILS y copies the raw value under the cursor — the full
			// command or path even when the pane had to truncate it.
			row := m.detailsC.selected(m.detailLines())
			switch {
			case row == nil:
			case row.copy == "":
				m.footerC.showToast("nothing to copy for " + row.label)
			case !m.cb.Available():
				m.footerC.showToast("No clipboard available")
			default:
				if err := m.cb.Copy(row.copy); err != nil {
					m.footerC.showToastLong("Copy failed")
				} else {
					m.footerC.showToast("Copied " + row.label)
				}
			}
		}

	case key.Matches(msg, keys.Start):
		return m, m.doStart()

	case key.Matches(msg, keys.Stop):
		return m, m.doStop()

	// r restarts the selected service; on one that is not running it is simply
	// a start, so there is no wrong state to press it in.
	case key.Matches(msg, keys.Restart):
		cmd := m.doRestart()
		if cmd != nil {
			m.footerC.showToast("restarting " + m.sidebarC.selectedService().Name)
		}
		return m, cmd

	// S / X act on everything listed: the filtering target when there is one
	// (so the daemon tracks it as an active target), otherwise every service.
	case key.Matches(msg, keys.StartAll):
		return m.runAllListed("start", m.doStartTarget, m.doStartAll)

	case key.Matches(msg, keys.StopAll):
		return m.runAllListed("stop", m.doStopTarget, m.doStopAll)

	case key.Matches(msg, keys.Help):
		m.helpC.open = true

	// t opens the target picker: choose which target filters the service list.
	case key.Matches(msg, keys.Target):
		if len(m.sidebarC.targets) == 0 {
			m.footerC.showToastLong("no targets defined — add a targets: block to the config")
			break
		}
		m.pickerC.openAt(m.sidebarC.targets, m.sidebarC.filterTarget)

	// e opens the editor for the highlighted service.
	case key.Matches(msg, keys.Edit):
		if m.onServiceRow() {
			return m.openEditor()
		}

	// d asks to remove the highlighted service (service rows only).
	case key.Matches(msg, keys.Remove):
		if m.onServiceRow() {
			return m.openRemoveConfirm()
		}
	}

	return m, nil
}

// onServiceRow reports whether the sidebar has focus, a service is selected, and
// there is a registry to persist an edit to. It gates both the `e` key and the footer's edit hint.
func (m model) onServiceRow() bool {
	return m.focus == focusSidebar &&
		m.registry != nil &&
		m.sidebarC.selectedService() != nil
}

// runAllListed runs the S / X action over everything listed — forTarget with the
// filtering target when there is one, otherwise forAll — and toasts what it is
// doing. The toast only promises an action that was actually dispatched: with no
// daemon socket, or nothing runnable in scope, it says so instead.
func (m model) runAllListed(verb string, forTarget func(string) tea.Cmd, forAll func() tea.Cmd) (tea.Model, tea.Cmd) {
	scope := "all services"
	var cmd tea.Cmd
	if name := m.sidebarC.filterTarget; name != "" {
		scope = "target " + name
		cmd = forTarget(name)
	} else {
		cmd = forAll()
	}
	if cmd == nil {
		m.footerC.showToast("nothing to " + verb + " in " + scope)
		return m, nil
	}
	m.footerC.showToast(verb + "ing " + scope)
	return m, cmd
}

// modalOpen reports whether a modal — an editor, the remove confirm, the target
// picker, or the help overlay — currently owns the screen and all input.
func (m model) modalOpen() bool {
	return m.editC.open || m.targetEditC.open || m.removeC.open || m.pickerC.open || m.helpC.open
}

// handleSearchKey routes a key to the footer's search input. The query is
// matched live as it is typed — the border's match count updates — but the
// cursor only moves on Enter, so abandoning a search with Esc costs nothing.
func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.searching = false
		m.searchC.Blur()
		m.logsC.sb.setQuery("")
		return m, nil
	case tea.KeyEnter:
		m.searching = false
		m.searchC.Blur()
		q := m.logsC.sb.search.query
		if q != "" && !m.logsC.sb.searchConfirm() {
			m.footerC.showToast("no matches for " + q)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.searchC, cmd = m.searchC.Update(msg)
	m.logsC.sb.setQuery(m.searchC.Value())
	return m, cmd
}

// handlePickerKey routes a key to the open target picker: j/k move, Enter
// applies the highlighted row as the service filter ("All services" clears it),
// e opens the target editor for a real target, and Esc / t / q close.
func (m model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	targets := m.sidebarC.targets
	switch {
	case msg.Type == tea.KeyCtrlC:
		return m, tea.Quit
	case key.Matches(msg, keys.Escape), key.Matches(msg, keys.Target), msg.String() == "q":
		m.pickerC.close()
	case key.Matches(msg, keys.Up):
		m.pickerC.move(-1, len(targets))
	case key.Matches(msg, keys.Down):
		m.pickerC.move(1, len(targets))
	case key.Matches(msg, keys.Enter):
		m.sidebarC.setFilter(m.pickerC.selected(targets))
		m.pickerC.close()
		m.focus = focusSidebar
		m.activeTab = tabLogs
		m.updateLogFile()
		m.relayout()
	case key.Matches(msg, keys.Edit):
		if name := m.pickerC.selected(targets); name != "" && m.registry != nil {
			m.pickerC.close()
			return m.openTargetEditor(name)
		}
	}
	return m, nil
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
	if m.registry == nil {
		m.editC.errMsg = "no config loaded"
		return m, nil
	}
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
		//
		// The naive stop-then-start is safe even for a command-only edit (same
		// name): dial blocks until the daemon's stopService -> TerminateGroup
		// has polled the old process dead, startService only rejects an entry in
		// StatusRunning/StatusStarting (a just-stopped one is Stopping/Stopped),
		// and watchExit mutates its captured *managedService rather than
		// s.services[name], so a re-start under the same name is not clobbered.
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

// doRestart stops then starts the selected service under its current
// definition. It shares the editor's restart path, which already tolerates a
// service that is not running — the stop is best-effort, the start decides.
func (m model) doRestart() tea.Cmd {
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return nil
	}
	var cfg *config.ServiceConfig
	if m.registry != nil {
		cfg = m.registry.Services[svc.Name]
	}
	return m.doRestartForEdit(svc.Name, svc.Name, cfg)
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

// --- remove service ---

// serviceRemoveBusy reports whether the last daemon view shows the named service
// running or starting — the states in which the daemon's handleRemove refuses to
// evict it, so deleting its config entry would orphan a live process.
func (m model) serviceRemoveBusy(name string) bool {
	for _, s := range m.sidebarC.allServices {
		if s.Name == name {
			return s.State == string(config.StatusRunning) || s.State == string(config.StatusStarting)
		}
	}
	return false
}

// openRemoveConfirm arms the delete modal for the selected service. A service
// the last poll showed busy is turned away with a toast rather than opening the
// modal; the daemon has the final say at confirm time.
func (m model) openRemoveConfirm() (tea.Model, tea.Cmd) {
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return m, nil
	}
	if m.serviceRemoveBusy(svc.Name) {
		m.footerC.showToast("stop " + svc.Name + " before removing")
		return m, nil
	}
	m.removeC.openFor(svc.Name)
	return m, nil
}

// handleRemoveKey routes a key to the open delete modal: y confirms, n / Esc
// cancel, everything else is swallowed. Keys are ignored while a confirm is
// already in flight so a double-tap cannot fire two requests.
func (m model) handleRemoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.removeC.pending {
		return m, nil
	}
	switch msg.String() {
	case "y", "Y":
		return m.confirmRemove()
	case "n", "N", "esc":
		m.removeC.close()
	}
	return m, nil
}

// confirmRemove asks the daemon to evict the service first; only once it agrees
// (serviceRemovedMsg with no error) is the config file rewritten. This makes the
// daemon — which refuses a running or starting service — the single authority,
// so a stale sidebar snapshot can never delete the definition of a live
// process. The pre-poll snapshot check is kept as an offline fast-path.
func (m model) confirmRemove() (tea.Model, tea.Cmd) {
	if m.registry == nil {
		m.removeC.errMsg = "no config loaded"
		return m, nil
	}
	name := m.removeC.name
	if m.serviceRemoveBusy(name) {
		m.removeC.errMsg = name + " is running — stop it first"
		return m, nil
	}
	m.removeC.pending = true
	m.removeC.errMsg = ""
	return m, m.doRemove(name)
}

// doRemove asks the daemon to drop the service from its in-memory map and
// reports the verdict as a serviceRemovedMsg. The config file is persisted only
// on a clean acceptance (err nil): a rejection (!resp.OK — running or starting)
// and any transport failure both come back as an error and leave the file
// alone, since a reachable-but-hiccuping daemon may well be supervising the
// process. Only the test/embedded case with no socket configured proceeds
// unconditionally.
func (m model) doRemove(name string) tea.Cmd {
	sp := m.socketPath
	if sp == "" {
		return func() tea.Msg { return serviceRemovedMsg{name: name} }
	}
	return func() tea.Msg {
		c, err := client.Connect(sp)
		if err != nil {
			return serviceRemovedMsg{name: name, err: fmt.Errorf("daemon unreachable: %w", err)}
		}
		defer c.Close()
		resp, err := c.Send("remove", ipc.RemovePayload{Name: name})
		if err != nil {
			return serviceRemovedMsg{name: name, err: fmt.Errorf("daemon unreachable: %w", err)}
		}
		if !resp.OK {
			reason := resp.Error
			if reason == "" {
				reason = "daemon refused the removal"
			}
			return serviceRemovedMsg{name: name, err: fmt.Errorf("%s", reason)}
		}
		return serviceRemovedMsg{name: name}
	}
}

// --- target editor ---

// openTargetEditor prefills the target modal for the focused target: every
// registry service is listed, the target's current members checked.
func (m model) openTargetEditor(name string) (tea.Model, tea.Cmd) {
	t := m.sidebarC.target(name)
	if t == nil || m.registry == nil {
		return m, nil
	}
	all := make([]string, 0, len(m.registry.Services))
	for name := range m.registry.Services {
		all = append(all, name)
	}
	m.targetEditC.openFor(t.name, all, m.registry.Targets[t.name])
	return m, textinput.Blink
}

// handleTargetEditKey routes a key to the open target modal: Esc cancels, Enter
// saves, Tab switches focus between the name field and the list; in the list,
// up/down move the cursor and space toggles membership.
func (m model) handleTargetEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.targetEditC.close()
		return m, nil
	case tea.KeyEnter:
		return m.saveTargetEditor()
	case tea.KeyTab, tea.KeyShiftTab:
		m.targetEditC.focusSwap()
		return m, textinput.Blink
	}
	if m.targetEditC.focusName {
		return m, m.targetEditC.update(msg)
	}
	switch {
	case key.Matches(msg, keys.Up):
		m.targetEditC.moveCursor(-1)
	case key.Matches(msg, keys.Down):
		m.targetEditC.moveCursor(1)
	case msg.Type == tea.KeySpace:
		m.targetEditC.toggleAtCursor()
	}
	return m, nil
}

// targetNames returns the set of all configured target names.
func (m model) targetNames() map[string]bool {
	out := make(map[string]bool)
	if m.registry != nil {
		for name := range m.registry.Targets {
			out[name] = true
		}
	}
	return out
}

// saveTargetEditor validates the modal, persists the target edit, mirrors it
// into the in-memory registry, and closes the modal. A running target keeps its
// old membership in the daemon until it is restarted.
func (m model) saveTargetEditor() (tea.Model, tea.Cmd) {
	if m.registry == nil {
		m.targetEditC.errMsg = "no config loaded"
		return m, nil
	}
	if problem := m.targetEditC.validate(m.targetNames()); problem != "" {
		m.targetEditC.errMsg = problem
		return m, nil
	}
	oldName := m.targetEditC.origName
	name, members := m.targetEditC.values()
	sort.Strings(members) // match what SaveTargetEdit persists, so the mirror agrees

	if err := config.SaveTargetEdit(m.source, oldName, name, members); err != nil {
		m.targetEditC.errMsg = err.Error()
		return m, nil
	}
	m.applyTargetEditToRegistry(oldName, name, members)
	m.targetEditC.close()
	m.footerC.showToast("saved " + name)
	return m, m.pollDaemon()
}

// applyTargetEditToRegistry mirrors the just-persisted target edit into the
// in-memory registry so the sidebar reflects it before the next daemon poll.
func (m *model) applyTargetEditToRegistry(oldName, newName string, members []string) {
	if m.registry == nil {
		return
	}
	if m.registry.Targets == nil {
		m.registry.Targets = map[string][]string{}
	}
	if newName != oldName {
		delete(m.registry.Targets, oldName)
	}
	m.registry.Targets[newName] = members
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

// buildTargets turns the registry's target definitions into the sorted list the
// target picker and the service filter work from. Returns nil with no registry
// or no targets.
func (m model) buildTargets() []sidebarTarget {
	if m.registry == nil || len(m.registry.Targets) == 0 {
		return nil
	}
	var rows []sidebarTarget
	for _, name := range config.SortedTargetNames(m.registry.Targets) {
		rows = append(rows, sidebarTarget{name: name, members: m.registry.Targets[name]})
	}
	return rows
}

// updateLogFile points the log pane at the selected service's file. A changed
// path means a different service is selected, so DETAILS returns to its top too
// — a cursor parked on row 12 of one service means nothing on the next.
func (m *model) updateLogFile() {
	if svc := m.sidebarC.selectedService(); svc != nil {
		path := filepath.Join(m.logDir, "logs", svc.Name+".log")
		if path != m.logsC.filePath {
			m.logsC.setFile(path)
			m.detailsC.reset()
		}
	}
}

// detailLines builds the DETAILS rows for the selected service.
func (m model) detailLines() []detailLine {
	svc := m.sidebarC.selectedService()
	if svc == nil {
		return nil
	}
	var cfg *config.ServiceConfig
	if m.registry != nil {
		cfg = m.registry.Services[svc.Name]
	}
	return detailLines(svc, cfg)
}

const (
	sidebarMinW = 31
	sidebarMaxW = 47

	headerRows  = 1
	footerRows  = 1
	mainPadLeft = 1 // blank column between the main pane's border and its content
)

// sidebarWidth is the sidebar pane's outer column count: wide enough for the
// longest service name plus the row's margin, glyph, state and CPU columns and
// the pane border, clamped to [sidebarMinW, sidebarMaxW] and never more than
// two fifths of the terminal.
func (m model) sidebarWidth() int {
	w := sidebarMinW
	for _, svc := range m.sidebarC.allServices {
		if n := lipgloss.Width(svc.Name) + 3 + 1 + rowStateW + 1 + rowCPUW + paneChrome; n > w {
			w = n
		}
	}
	w = min(w, sidebarMaxW)
	if m.width > 0 {
		w = min(w, m.width*2/5)
	}
	return max(w, sidebarMinW/2)
}

// narrowBelow is the terminal width under which the two panes no longer fit
// side by side with a usable log column; below it only the focused pane is
// drawn, at full width, and Tab swaps which one that is.
const narrowBelow = 70

func (m model) narrow() bool { return m.width > 0 && m.width < narrowBelow }

// paneWidths returns the outer widths of the sidebar and the main pane. Side by
// side they split the terminal; in the narrow layout each gets all of it, since
// only one is on screen at a time.
func (m model) paneWidths() (side, main int) {
	if m.narrow() {
		return m.width, m.width
	}
	side = m.sidebarWidth()
	return side, m.width - side
}

// bodyHeight is the row count left for the panes between header and footer.
func (m model) bodyHeight() int { return max(0, m.height-headerRows-footerRows) }

// mainFrame is the main pane's border, minus the labels renderMain sets.
func (m model) mainFrame() paneFrame {
	return paneFrame{focused: m.focus == focusMain, padLeft: mainPadLeft}
}

// relayout recomputes derived geometry after a resize or a sidebar-width change
// and re-flows the log panel to the main pane's content area.
func (m *model) relayout() {
	if m.width == 0 {
		return
	}
	sideW, mainW := m.paneWidths()
	w, h := m.mainFrame().innerSize(mainW, m.bodyHeight())
	m.logsC.sb.resize(w, h)
	_, sideRows := paneFrame{}.innerSize(sideW, m.bodyHeight())
	m.sidebarC.setRows(sideRows)
	m.detailsC.setRows(h)
	m.detailsC.scrollToCursor(m.detailLines())
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

// doStartTarget starts every service in the target called name. An unknown
// target is a no-op, as is one with no runnable members. Member definitions are shipped inline so a project target works
// without a registry entry.
func (m model) doStartTarget(name string) tea.Cmd {
	if m.socketPath == "" || m.registry == nil {
		return nil
	}
	t := m.sidebarC.target(name)
	if t == nil {
		return nil
	}
	members := m.registry.TargetMemberConfigs(t.name)
	if len(members) == 0 {
		return nil
	}
	sp := m.socketPath
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

// doStopTarget stops the target called name; the daemon keeps any member still
// held by another running target. An unknown target is a no-op.
func (m model) doStopTarget(name string) tea.Cmd {
	if m.socketPath == "" || m.sidebarC.target(name) == nil {
		return nil
	}
	sp := m.socketPath
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

// scopedServiceNames lists the names of every service in the sidebar's scoped
// view (the unfiltered "All services" set), in the sidebar's sorted order.
func (m model) scopedServiceNames() []string {
	names := make([]string, len(m.sidebarC.allServices))
	for i, s := range m.sidebarC.allServices {
		names[i] = s.Name
	}
	return names
}

// doStartAll starts every scoped service — the action behind `S` with no target
// filter, the TUI equivalent of `devrun start --all`. It
// dials once per service (the daemon serves one request per connection),
// shipping each definition inline so a project service the daemon has not seen
// still starts. A service already running is left alone; a member that fails
// does not abort the rest, and the combined failures surface as one error.
func (m model) doStartAll() tea.Cmd {
	if m.socketPath == "" {
		return nil
	}
	names := m.scopedServiceNames()
	if len(names) == 0 {
		return nil
	}
	sp, reg := m.socketPath, m.registry
	return func() tea.Msg {
		var failures []string
		for _, name := range names {
			var cfg *config.ServiceConfig
			if reg != nil {
				cfg = reg.Services[name]
			}
			if f := batchStep(sp, "start", ipc.StartPayload{Name: name, Config: cfg}, name, "is already running"); f != "" {
				failures = append(failures, f)
			}
		}
		if len(failures) > 0 {
			return daemonErrMsg{fmt.Errorf("start all: %s", strings.Join(failures, "; "))}
		}
		return daemonTickMsg{}
	}
}

// doStopAll stops every scoped service — the action behind `X` with no target
// filter, the TUI equivalent of `devrun stop --all`. Like
// doStartAll it dials once per service; a service already stopped is not an
// error, and per-service failures are collected into one message.
func (m model) doStopAll() tea.Cmd {
	if m.socketPath == "" {
		return nil
	}
	names := m.scopedServiceNames()
	if len(names) == 0 {
		return nil
	}
	sp := m.socketPath
	return func() tea.Msg {
		var failures []string
		for _, name := range names {
			if f := batchStep(sp, "stop", ipc.StopPayload{Name: name}, name, "is not running"); f != "" {
				failures = append(failures, f)
			}
		}
		if len(failures) > 0 {
			return daemonErrMsg{fmt.Errorf("stop all: %s", strings.Join(failures, "; "))}
		}
		return daemonTickMsg{}
	}
}

// batchStep runs one request of an "all services" batch and returns a
// "<name>: <reason>" failure string, or "" when the member succeeded or was
// already in the wanted state.
func batchStep(socketPath, reqType string, payload interface{}, name, benignErr string) string {
	resp, err := sendOnce(socketPath, reqType, payload)
	return classifyBatchResp(resp, err, name, benignErr)
}

// classifyBatchResp turns one daemon start/stop reply into a "<name>: <reason>"
// failure string, or "" when the member succeeded or was already in the wanted
// state. benignErr is the daemon's wording for that already-in-state case: the
// single-service "start" / "stop" handlers reply "<name> is already running" /
// "<name> is not running" (daemon.startService / stopService), and the same
// wording is what internal/daemon's isAlreadyRunning / isNotRunning match for
// the target paths that wrap those methods. A missing response (transport
// error, or a contract-breaking nil/nil) is itself a failure, never a success;
// so is a not-OK reply with no error text.
func classifyBatchResp(resp *ipc.Response, err error, name, benignErr string) string {
	switch {
	case err != nil:
		return fmt.Sprintf("%s: %v", name, err)
	case resp == nil:
		return fmt.Sprintf("%s: no response from daemon", name)
	case !resp.OK && strings.Contains(resp.Error, benignErr):
		return "" // already in the wanted state — not a failure
	case !resp.OK:
		reason := resp.Error
		if reason == "" {
			reason = "request failed"
		}
		return fmt.Sprintf("%s: %s", name, reason)
	}
	return ""
}

// sendOnce opens a fresh daemon connection, sends a single request, and returns
// the response. The daemon serves one request per connection, so batch callers
// loop with a new call per request rather than reusing a client.
func sendOnce(socketPath, reqType string, payload interface{}) (*ipc.Response, error) {
	c, err := client.Connect(socketPath)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Send(reqType, payload)
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	sidebarW, mainW := m.paneWidths()
	bodyH := m.bodyHeight()

	// Header — counts reflect the whole scoped project, not the target filter.
	total := len(m.sidebarC.allServices)
	running, crashed := 0, 0
	for _, s := range m.sidebarC.allServices {
		switch s.State {
		case "running":
			running++
		case "crashed":
			crashed++
		}
	}
	header := m.headerC.render(m.sourceLabel(), total, running, crashed, m.spinFrame, m.spinning, m.width)

	// Body: two bordered panes side by side; the focused one takes the accent.
	sideFrame := m.sidebarC.frame(m.focus == focusSidebar)
	sideW, _ := sideFrame.innerSize(sidebarW, bodyH)
	var body string
	switch {
	case !m.narrow():
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			sideFrame.render(m.sidebarC.render(sideW), sidebarW, bodyH),
			m.renderMain(mainW, bodyH),
		)
	case m.focus == focusSidebar:
		body = sideFrame.render(m.sidebarC.render(sideW), sidebarW, bodyH)
	default:
		body = m.renderMain(mainW, bodyH)
	}

	// An open modal floats over the dimmed panes rather than replacing them.
	var modal string
	switch {
	case m.editC.open:
		modal = m.editC.view()
	case m.targetEditC.open:
		modal = m.targetEditC.view()
	case m.removeC.open:
		modal = m.removeC.view()
	case m.pickerC.open:
		modal = m.pickerC.view(m.sidebarC.targets, m.sidebarC.allServices, m.sidebarC.filterTarget)
	case m.helpC.open:
		modal = m.helpC.view()
	}
	if modal != "" {
		body = overlay(body, modal, m.width, bodyH)
	}

	footer := m.footerC.render(footerCtx{
		tab:          m.activeTab,
		focus:        m.focus,
		visual:       m.logsC.sb.visualMode,
		onServiceRow: m.onServiceRow(),
		editing:      m.editC.open || m.targetEditC.open,
		confirming:   m.removeC.open,
		picking:      m.pickerC.open,
		helping:      m.helpC.open,
		searching:    m.searching,
		hasQuery:     m.logsC.sb.search.active(),
		searchInput:  m.searchC.View(),
		narrow:       m.narrow(),
	}, m.width)

	// The header and footer are full-width bars. A terminal has no half rows,
	// so blank-row padding can only be lopsided (none above the header, a whole
	// row below); a background band separates them from the panes instead, with
	// the text centred in it by construction.
	return lipgloss.JoinVertical(lipgloss.Left,
		barBackground(header, m.width), body, barBackground(footer, m.width))
}

// sourceLabel names the config in scope for the header: "<project dir> ·
// devrun.yaml" for a project file, the global registry otherwise.
func (m model) sourceLabel() string {
	if m.registry == nil {
		return ""
	}
	if m.source.IsLocal() {
		return filepath.Base(m.source.Dir) + " · " + filepath.Base(m.source.Local)
	}
	return "global · services.yaml"
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

// renderMain draws the main pane at outer size w×h. Its top border names the
// selected service — state, port, uptime — and shows both view labels with the
// active one bracketed; for LOGS the bottom border carries the line count and
// the follow / wrap state.
func (m model) renderMain(w, h int) string {
	frame := m.mainFrame()
	iw, _ := frame.innerSize(w, h)
	svc := m.sidebarC.selectedService()

	if svc == nil {
		frame.title = styleMuted.Render("LOGS")
		return frame.render(styleMuted.Render("No service selected"), w, h)
	}

	title := styleText.Bold(true).Render(svc.Name) + "  " + renderStateLabel(svc.State)
	if svc.State == "running" {
		if svc.Port != nil && *svc.Port != 0 {
			title += " " + styleAccent.Render(fmt.Sprintf(":%d", *svc.Port))
		}
		if svc.UptimeSec > 0 {
			title += styleMuted.Render("  up " + formatUptime(svc.UptimeSec))
		}
	}
	frame.title = title

	tab := func(kind tabKind, label string) string {
		switch {
		case m.activeTab != kind:
			return styleMuted.Render(strings.ToLower(label))
		case m.focus == focusMain:
			return styleAccent.Bold(true).Render("[" + label + "]")
		default:
			return styleText.Render("[" + label + "]")
		}
	}
	frame.titleRight = tab(tabLogs, "LOGS") + " " + tab(tabDetails, "DETAILS")

	if m.activeTab == tabDetails {
		lines := m.detailLines()
		if first, last := m.detailsC.window(lines); last-first < len(lines) {
			frame.footRight = styleMuted.Render(fmt.Sprintf("%d–%d of %d", first+1, last, len(lines)))
		}
		// The details list draws its own one-column gutter (the cursor bar),
		// so it takes the pane's padding column rather than adding to it.
		frame.padLeft = 0
		return frame.render(m.detailsC.render(lines, iw+mainPadLeft, m.focus == focusMain), w, h)
	}

	sb := &m.logsC.sb
	if n := len(sb.lines); n > 0 {
		frame.footLeft = styleMuted.Render(formatCount(n) + " lines")
	}
	if sb.search.active() {
		// "3/17 matches" while the cursor sits on a match, "17 matches" when it
		// does not, and a plain statement when there is nothing to step through.
		found := styleYellow.Render("no matches")
		if total := len(sb.search.matches); total > 0 {
			found = formatCount(total) + " matches"
			if at := sb.search.position(sb.cursor); at > 0 {
				found = formatCount(at) + "/" + found
			}
			found = styleAccent.Render(found)
		}
		if frame.footLeft != "" {
			frame.footLeft += styleMuted.Render(" · ")
		}
		frame.footLeft += found
	}
	// Follow off is only interesting if something arrived meanwhile: say how
	// much, in the warning colour, so it is clear the view is behind.
	status := styleMuted.Render("follow off")
	switch {
	case sb.followMode:
		status = styleGreen.Render("⇣ follow")
	case sb.unseen > 0:
		status = styleYellow.Render("↓ " + formatCount(sb.unseen) + " new")
	}
	if sb.noWrap {
		status = styleMuted.Render("no-wrap · ") + status
	}
	frame.footRight = status
	return frame.render(m.logsC.view(), w, h)
}
