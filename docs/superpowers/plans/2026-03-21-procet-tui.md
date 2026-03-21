# procet TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a full-screen terminal dashboard launched by `procet` (no args) showing a real-time service sidebar, log tail, and details panel with start/stop control.

**Architecture:** A bubbletea root model (`internal/tui/model.go`) owns a sidebar component, a logs panel (bubbles viewport), and a details panel; a 2s ticker polls the daemon for service state; a 100ms ticker polls the log file; lipgloss composes the layout. The TUI is wired into cobra via `rootCmd.RunE` in `internal/cli/root.go`; all infra (EnsureDaemon, client connect, registry load) happens in root.go before calling `tui.Run(c, reg, logDir)`.

**Tech Stack:** Go 1.22+, `github.com/charmbracelet/bubbletea` (event loop), `github.com/charmbracelet/lipgloss` (layout/styling), `github.com/charmbracelet/bubbles/viewport` (scrollable log panel), existing `internal/client`, `internal/config`, `internal/ipc`, `internal/daemon`.

---

## File Map

| Path | Action | Purpose |
|------|--------|---------|
| `go.mod` / `go.sum` | Modify | Add Charm dependencies |
| `internal/tui/styles.go` | Create | GitHub Dark palette lipgloss constants |
| `internal/tui/keys.go` | Create | bubbletea key.Binding definitions |
| `internal/tui/clipboard.go` | Create | pbcopy/xclip/xsel abstraction |
| `internal/tui/clipboard_test.go` | Create | clipboard detection tests |
| `internal/tui/header.go` | Create | Header bar: title, counts, spinner |
| `internal/tui/footer.go` | Create | Footer bar: hints + toast system |
| `internal/tui/footer_test.go` | Create | Toast timing tests |
| `internal/tui/sidebar.go` | Create | Service list, mini stats, action hints |
| `internal/tui/sidebar_test.go` | Create | Sort order, selection preservation tests |
| `internal/tui/logs.go` | Create | Log file tail, viewport, visual select, copy |
| `internal/tui/logs_test.go` | Create | Colorize, copy, follow mode tests |
| `internal/tui/details.go` | Create | STATUS + CONFIG + ENV rendering |
| `internal/tui/details_test.go` | Create | formatUptime, formatBytes tests |
| `internal/tui/model.go` | Create | Root bubbletea model wiring all components |
| `internal/tui/model_test.go` | Create | WindowSizeMsg sets dimensions |
| `internal/cli/root.go` | Modify | Add RunE: EnsureDaemon → connect → tui.Run |

---

## Task 1: Add Charm Dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the three Charm packages**

```bash
cd /path/to/procet
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbles
```

- [ ] **Step 2: Verify the build compiles**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add bubbletea, lipgloss, bubbles dependencies"
```

---

## Task 2: Styles and Keybindings

**Files:**
- Create: `internal/tui/styles.go`
- Create: `internal/tui/keys.go`

No tests needed (pure constants).

- [ ] **Step 1: Create `internal/tui/styles.go`**

```go
package tui

import "github.com/charmbracelet/lipgloss"

// GitHub Dark palette
var (
	colorBg     = lipgloss.Color("#0d1117")
	colorText   = lipgloss.Color("#c9d1d9")
	colorMuted  = lipgloss.Color("#6e7681")
	colorAccent = lipgloss.Color("#58a6ff")
	colorGreen  = lipgloss.Color("#3fb950")
	colorRed    = lipgloss.Color("#f85149")
	colorYellow = lipgloss.Color("#f0e68c")
	colorBorder = lipgloss.Color("#21262d")
	colorSelBg  = lipgloss.Color("#161b22")
	colorVisBg  = lipgloss.Color("#1f3a5f")
)

var (
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent = lipgloss.NewStyle().Foreground(colorAccent)
	styleGreen  = lipgloss.NewStyle().Foreground(colorGreen)
	styleRed    = lipgloss.NewStyle().Foreground(colorRed)
	styleYellow = lipgloss.NewStyle().Foreground(colorYellow)
	styleText   = lipgloss.NewStyle().Foreground(colorText)

	styleBorderH = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder)

	styleSelectedSidebar = lipgloss.NewStyle().
				Background(colorSelBg).
				BorderLeft(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorAccent)

	styleVisualLine = lipgloss.NewStyle().
			Background(colorVisBg).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorAccent)

	styleSelectedLine = lipgloss.NewStyle().
				Background(colorSelBg)
)
```

- [ ] **Step 2: Create `internal/tui/keys.go`**

```go
package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Tab    key.Binding
	Start  key.Binding
	Stop   key.Binding
	Quit   key.Binding
	Follow key.Binding
	Copy   key.Binding
	Visual key.Binding
	Escape key.Binding
	Top    key.Binding
	Bottom key.Binding
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
	Down:   key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
	Left:   key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "sidebar")),
	Right:  key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "main")),
	Tab:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "switch tab")),
	Start:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start")),
	Stop:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Follow: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "follow")),
	Copy:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy")),
	Visual: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "select")),
	Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "cancel")),
	Top:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
	Bottom: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
}
```

- [ ] **Step 3: Build to confirm no errors**

```bash
go build ./internal/tui/...
```

- [ ] **Step 4: Commit**

```bash
git add internal/tui/styles.go internal/tui/keys.go
git commit -m "feat(tui): styles and keybindings — GitHub Dark palette"
```

---

## Task 3: Clipboard Abstraction

**Files:**
- Create: `internal/tui/clipboard.go`
- Create: `internal/tui/clipboard_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/clipboard_test.go
package tui

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClipboard_NoneAvailable(t *testing.T) {
	cb := detectClipboardWith(func(string) bool { return false })
	assert.False(t, cb.Available())
}

func TestClipboard_PbcopyOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	cb := detectClipboardWith(func(name string) bool { return name == "pbcopy" })
	assert.True(t, cb.Available())
}

func TestClipboard_XclipOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("linux only")
	}
	cb := detectClipboardWith(func(name string) bool { return name == "xclip" })
	assert.True(t, cb.Available())
}

func TestClipboard_XselFallbackOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("linux only")
	}
	// xclip not found, xsel found
	cb := detectClipboardWith(func(name string) bool { return name == "xsel" })
	assert.True(t, cb.Available())
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tui/... -run TestClipboard
```

Expected: compilation error (clipboard.go not yet created).

- [ ] **Step 3: Create `internal/tui/clipboard.go`**

```go
package tui

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
)

type clipboardBackend struct {
	cmd  string
	args []string
}

type clipboard struct {
	backend *clipboardBackend
}

// detectClipboard checks for clipboard backends at startup.
func detectClipboard() clipboard {
	return detectClipboardWith(func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
}

// detectClipboardWith is the testable form; lookup returns true if the named
// command exists on PATH.
func detectClipboardWith(lookup func(string) bool) clipboard {
	if runtime.GOOS == "darwin" {
		if lookup("pbcopy") {
			return clipboard{backend: &clipboardBackend{cmd: "pbcopy"}}
		}
		return clipboard{}
	}
	if lookup("xclip") {
		return clipboard{backend: &clipboardBackend{
			cmd:  "xclip",
			args: []string{"-selection", "clipboard"},
		}}
	}
	if lookup("xsel") {
		return clipboard{backend: &clipboardBackend{
			cmd:  "xsel",
			args: []string{"--clipboard", "--input"},
		}}
	}
	return clipboard{}
}

// Available reports whether a clipboard backend was found.
func (c clipboard) Available() bool { return c.backend != nil }

// Copy writes text to the system clipboard.
func (c clipboard) Copy(text string) error {
	if !c.Available() {
		return fmt.Errorf("no clipboard backend")
	}
	cmd := exec.Command(c.backend.cmd, c.backend.args...)
	cmd.Stdin = bytes.NewBufferString(text)
	return cmd.Run()
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
go test ./internal/tui/... -run TestClipboard -v
```

Expected: all 4 tests pass (some skipped by OS).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/clipboard.go internal/tui/clipboard_test.go
git commit -m "feat(tui): clipboard abstraction (pbcopy/xclip/xsel)"
```

---

## Task 4: Header and Footer Components

**Files:**
- Create: `internal/tui/header.go`
- Create: `internal/tui/footer.go`
- Create: `internal/tui/footer_test.go`

- [ ] **Step 1: Write footer tests**

```go
// internal/tui/footer_test.go
package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFooter_ToastAppearsAndClears(t *testing.T) {
	f := &footerBar{}
	f.showToast("Copied!")
	assert.Equal(t, "Copied!", f.toast)

	f.tick(1 * time.Second)
	assert.Equal(t, "Copied!", f.toast) // still visible

	f.tick(600 * time.Millisecond) // total 1.6s > 1.5s threshold
	assert.Equal(t, "", f.toast)   // cleared
}

func TestFooter_ToastNoOpWhenEmpty(t *testing.T) {
	f := &footerBar{}
	f.tick(5 * time.Second) // should not panic
	assert.Equal(t, "", f.toast)
}

func TestFooter_ToastResetOnNew(t *testing.T) {
	f := &footerBar{}
	f.showToast("first")
	f.tick(1 * time.Second)
	f.showToast("second") // resets timer
	f.tick(1 * time.Second)
	assert.Equal(t, "second", f.toast) // 1s < 1.5s, still showing
}
```

- [ ] **Step 2: Run to confirm compilation failure**

```bash
go test ./internal/tui/... -run TestFooter
```

- [ ] **Step 3: Create `internal/tui/header.go`**

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type headerBar struct{}

func (h headerBar) render(total, running, frame int, spinning bool, width int) string {
	left := styleAccent.Bold(true).Render("⬡ procet")

	indicator := "●"
	if spinning {
		indicator = spinFrames[frame%len(spinFrames)]
	}
	right := styleMuted.Render(fmt.Sprintf("%d services · %d running · %s", total, running, indicator))

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().
		Width(width).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Render(line)
}
```

- [ ] **Step 4: Create `internal/tui/footer.go`**

```go
package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const toastDuration = 1500 * time.Millisecond

type footerBar struct {
	toast    string
	toastAge time.Duration
}

func (f *footerBar) showToast(msg string) {
	f.toast = msg
	f.toastAge = 0
}

func (f *footerBar) tick(dt time.Duration) {
	if f.toast == "" {
		return
	}
	f.toastAge += dt
	if f.toastAge >= toastDuration {
		f.toast = ""
		f.toastAge = 0
	}
}

func (f *footerBar) render(activeTab tabKind, focus focusKind, visualMode bool, width int) string {
	base := lipgloss.NewStyle().
		Width(width).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder)

	if f.toast != "" {
		return base.Foreground(colorAccent).Render(f.toast)
	}

	var hints []string
	hints = append(hints, renderHint("Tab", "switch"))
	if focus == focusMain && activeTab == tabLogs {
		hints = append(hints, renderHint("y", "copy"), renderHint("v", "select"), renderHint("f", "follow"))
	}
	if visualMode {
		hints = append(hints, renderHint("Esc", "cancel"))
	}
	hints = append(hints, renderHint("s", "start"), renderHint("x", "stop"), renderHint("q", "quit"))
	return base.Render(strings.Join(hints, "  "))
}

func renderHint(k, label string) string {
	key := lipgloss.NewStyle().
		Background(colorBorder).
		Foreground(colorText).
		Padding(0, 1).
		Render(k)
	return key + styleMuted.Render(" "+label)
}
```

Note: `tabKind` and `focusKind` are defined in `model.go` (Task 9). For now declare them as placeholder types so the package compiles:

```go
// internal/tui/footer.go — add at top of file, below imports:
type tabKind int
type focusKind int

const (
	tabLogs    tabKind  = iota
	tabDetails
)
const (
	focusSidebar focusKind = iota
	focusMain
)
```

- [ ] **Step 5: Run footer tests — expect pass**

```bash
go test ./internal/tui/... -run TestFooter -v
```

- [ ] **Step 6: Confirm package builds**

```bash
go build ./internal/tui/...
```

- [ ] **Step 7: Commit**

```bash
git add internal/tui/header.go internal/tui/footer.go internal/tui/footer_test.go
git commit -m "feat(tui): header and footer components with toast system"
```

---

## Task 5: Sidebar Component

**Files:**
- Create: `internal/tui/sidebar.go`
- Create: `internal/tui/sidebar_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/tui/sidebar_test.go
package tui

import (
	"testing"

	"github.com/hailerity/procet/internal/ipc"
	"github.com/stretchr/testify/assert"
)

func svcNames(sb *sidebar) []string {
	names := make([]string, len(sb.services))
	for i, s := range sb.services {
		names[i] = s.Name
	}
	return names
}

func TestSidebar_AlphabeticalOrder(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{
		{Name: "zoo", State: "running"},
		{Name: "api", State: "stopped"},
		{Name: "web", State: "running"},
	})
	assert.Equal(t, []string{"api", "web", "zoo"}, svcNames(sb))
}

func TestSidebar_SelectionPreservedByName(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}})
	sb.selected = 1 // "web"

	sb.update([]ipc.ServiceInfo{{Name: "zoo"}, {Name: "web", State: "running"}, {Name: "api"}})
	assert.Equal(t, 1, sb.selected) // still index of "web" after re-sort
}

func TestSidebar_SelectionFallsBackWhenServiceGone(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}, {Name: "zoo"}})
	sb.selected = 2 // "zoo"

	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}})
	assert.Equal(t, 0, sb.selected) // "zoo" gone, falls back to 0
}

func TestSidebar_MoveUpDownClamps(t *testing.T) {
	sb := &sidebar{}
	sb.update([]ipc.ServiceInfo{{Name: "api"}, {Name: "web"}})

	sb.moveUp()
	assert.Equal(t, 0, sb.selected) // can't go above 0

	sb.moveDown()
	assert.Equal(t, 1, sb.selected)

	sb.moveDown()
	assert.Equal(t, 1, sb.selected) // can't go past last
}

func TestStateLabel_RunningWithPort(t *testing.T) {
	port := 8080
	assert.Equal(t, ":8080", stateLabel(ipc.ServiceInfo{State: "running", Port: &port}))
}

func TestStateLabel_RunningNoPort(t *testing.T) {
	assert.Equal(t, "detecting", stateLabel(ipc.ServiceInfo{State: "running", Port: nil}))
}

func TestStateLabel_RunningZeroPort(t *testing.T) {
	port := 0
	assert.Equal(t, "detecting", stateLabel(ipc.ServiceInfo{State: "running", Port: &port}))
}

func TestStateLabel_Crashed(t *testing.T) {
	assert.Equal(t, "crashed", stateLabel(ipc.ServiceInfo{State: "crashed"}))
}
```

- [ ] **Step 2: Run to confirm compilation failure**

```bash
go test ./internal/tui/... -run TestSidebar -run TestStateLabel
```

- [ ] **Step 3: Create `internal/tui/sidebar.go`**

```go
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/procet/internal/ipc"
)

type sidebar struct {
	services []ipc.ServiceInfo // always sorted by Name
	selected int
}

func (s *sidebar) update(svcs []ipc.ServiceInfo) {
	// Remember current name before replacing
	var cur string
	if s.selected < len(s.services) {
		cur = s.services[s.selected].Name
	}

	sorted := make([]ipc.ServiceInfo, len(svcs))
	copy(sorted, svcs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	s.services = sorted

	// Restore by name, fallback to 0
	s.selected = 0
	for i, svc := range s.services {
		if svc.Name == cur {
			s.selected = i
			break
		}
	}
}

func (s *sidebar) moveUp() {
	if s.selected > 0 {
		s.selected--
	}
}

func (s *sidebar) moveDown() {
	if s.selected < len(s.services)-1 {
		s.selected++
	}
}

func (s *sidebar) selectedService() *ipc.ServiceInfo {
	if len(s.services) == 0 {
		return nil
	}
	return &s.services[s.selected]
}

// stateLabel returns the right-side label for a service row.
func stateLabel(svc ipc.ServiceInfo) string {
	if svc.State == "running" {
		if svc.Port != nil && *svc.Port != 0 {
			return fmt.Sprintf(":%d", *svc.Port)
		}
		return "detecting"
	}
	return svc.State
}

func stateDot(state string) string {
	switch state {
	case "running":
		return styleGreen.Render("●")
	case "crashed":
		return styleRed.Render("●")
	default:
		return styleMuted.Render("●")
	}
}

func (s *sidebar) render(width, height int, focused bool) string {
	if len(s.services) == 0 {
		return styleMuted.Render("No services — run procet add <name>")
	}

	var rows []string
	// Header
	rows = append(rows, styleMuted.Render("SERVICES"))

	for i, svc := range s.services {
		dot := stateDot(svc.State)
		label := stateLabel(svc)

		// Pad name and label to fill width
		nameW := width - 4 - lipgloss.Width(label)
		if nameW < 1 {
			nameW = 1
		}
		name := svc.Name
		if len(name) > nameW {
			name = name[:nameW]
		}
		line := dot + " " + fmt.Sprintf("%-*s", nameW, name) + styleMuted.Render(label)

		if i == s.selected {
			line = styleSelectedSidebar.Width(width).Render(line)
		}
		rows = append(rows, line)
	}

	// Mini stats for selected
	if svc := s.selectedService(); svc != nil {
		rows = append(rows, strings.Repeat("─", width))
		rows = append(rows, styleMuted.Render(svc.Name))
		rows = append(rows, fmt.Sprintf("CPU  %s", renderCPUPct(svc.CPUPct)))
		rows = append(rows, fmt.Sprintf("MEM  %s", formatBytes(svc.MemBytes)))
		rows = append(rows, fmt.Sprintf("UP   %s", formatUptime(svc.UptimeSec)))
	}

	// Action hints at bottom
	rows = append(rows, strings.Repeat("─", width))
	rows = append(rows, renderHint("s", "start"))
	rows = append(rows, renderHint("x", "stop"))

	return strings.Join(rows, "\n")
}

func renderCPUPct(pct float64) string {
	s := fmt.Sprintf("%.1f%%", pct)
	if pct > 80 {
		return styleRed.Render(s)
	}
	if pct > 50 {
		return styleYellow.Render(s)
	}
	return styleYellow.Render(s) // always yellow for visibility in mini stats
}
```

- [ ] **Step 4: Run sidebar tests**

```bash
go test ./internal/tui/... -run "TestSidebar|TestStateLabel" -v
```

Expected: all 8 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/sidebar.go internal/tui/sidebar_test.go
git commit -m "feat(tui): sidebar component with alphabetical sort and mini stats"
```

---

## Task 6: Logs Panel

**Files:**
- Create: `internal/tui/logs.go`
- Create: `internal/tui/logs_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/tui/logs_test.go
package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestColorizeLog_200IsGreen(t *testing.T) {
	out := colorizeLog("[10:14:30] GET /health 200 1ms")
	// The 200 should be wrapped in a lipgloss style — check it still contains 200
	assert.Contains(t, out, "200")
	// And is longer than the raw string (style codes added)
	assert.Greater(t, len(out), len("[10:14:30] GET /health 200 1ms"))
}

func TestColorizeLog_404IsYellow(t *testing.T) {
	out := colorizeLog("GET /missing 404")
	assert.Contains(t, out, "404")
	assert.Greater(t, len(out), len("GET /missing 404"))
}

func TestColorizeLog_500IsRed(t *testing.T) {
	out := colorizeLog("POST /fail 500")
	assert.Contains(t, out, "500")
	assert.Greater(t, len(out), len("POST /fail 500"))
}

func TestColorizeLog_NoStatusUnchanged(t *testing.T) {
	line := "[10:14:30] connected to db"
	assert.Equal(t, line, colorizeLog(line))
}

func TestLogsPanel_CopyLine(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"line 0", "line 1", "line 2"}
	lp.cursor = 1
	assert.Equal(t, "line 1", lp.copyLine())
}

func TestLogsPanel_CopyLineEmpty(t *testing.T) {
	lp := newLogsPanel()
	assert.Equal(t, "", lp.copyLine())
}

func TestLogsPanel_CopySelection(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"line 0", "line 1", "line 2", "line 3"}
	lp.visualMode = true
	lp.selStart = 1
	lp.selEnd = 2
	assert.Equal(t, "line 1\nline 2", lp.copySelection())
}

func TestLogsPanel_MoveUpDisablesFollow(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c"}
	lp.cursor = 2
	lp.followMode = true

	lp.moveUp()
	assert.Equal(t, 1, lp.cursor)
	assert.False(t, lp.followMode)
}

func TestLogsPanel_MoveDownDisablesFollow(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c"}
	lp.cursor = 0
	lp.followMode = true

	lp.moveDown()
	assert.Equal(t, 1, lp.cursor)
	assert.False(t, lp.followMode)
}

func TestLogsPanel_MoveDownDoesNotExceedBounds(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b"}
	lp.cursor = 1
	lp.moveDown()
	assert.Equal(t, 1, lp.cursor)
}

func TestLogsPanel_EnterVisualSetsRange(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c"}
	lp.cursor = 1
	lp.enterVisual()
	assert.True(t, lp.visualMode)
	assert.Equal(t, 1, lp.selStart)
	assert.Equal(t, 1, lp.selEnd)
}

func TestLogsPanel_VisualMoveExtendsSelection(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b", "c", "d"}
	lp.cursor = 1
	lp.enterVisual()
	lp.moveDown()
	assert.Equal(t, 2, lp.selEnd)
	assert.Equal(t, 2, lp.cursor)
}

func TestLogsPanel_ExitVisualClearsMode(t *testing.T) {
	lp := newLogsPanel()
	lp.lines = []string{"a", "b"}
	lp.cursor = 0
	lp.enterVisual()
	lp.exitVisual()
	assert.False(t, lp.visualMode)
}
```

- [ ] **Step 2: Run to confirm compilation failure**

```bash
go test ./internal/tui/... -run "TestColorize|TestLogsPanel"
```

- [ ] **Step 3: Create `internal/tui/logs.go`**

```go
package tui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
)

var httpStatusRe = regexp.MustCompile(`\b([2-5]\d{2})\b`)

type logsPanel struct {
	vp         viewport.Model
	lines      []string
	cursor     int  // line index under cursor (for single-line copy)
	selStart   int  // visual selection start index
	selEnd     int  // visual selection end index
	visualMode bool
	followMode bool

	// file state
	filePath   string
	fileOffset int64
	noLogMsg   string // set when file is missing
}

func newLogsPanel() logsPanel {
	return logsPanel{
		followMode: true,
	}
}

// setFile switches the panel to tail a new log file.
func (lp *logsPanel) setFile(path string) {
	lp.filePath = path
	lp.fileOffset = 0
	lp.lines = nil
	lp.cursor = 0
	lp.visualMode = false
	lp.noLogMsg = ""
}

// poll reads any new lines appended to the log file since the last poll.
// Returns true if new lines were added.
func (lp *logsPanel) poll() bool {
	if lp.filePath == "" {
		return false
	}
	f, err := os.Open(lp.filePath)
	if err != nil {
		lp.noLogMsg = fmt.Sprintf("No logs yet for %s", logName(lp.filePath))
		return false
	}
	defer f.Close()

	f.Seek(lp.fileOffset, io.SeekStart) //nolint:errcheck
	scanner := bufio.NewScanner(f)
	var added bool
	for scanner.Scan() {
		lp.lines = append(lp.lines, scanner.Text())
		added = true
	}
	lp.fileOffset, _ = f.Seek(0, io.SeekCurrent)
	lp.noLogMsg = ""

	if added && lp.followMode {
		lp.cursor = len(lp.lines) - 1
	}
	return added
}

// rebuildViewport refreshes the viewport content from lp.lines.
func (lp *logsPanel) rebuildViewport() {
	if lp.noLogMsg != "" {
		lp.vp.SetContent(styleMuted.Render(lp.noLogMsg))
		return
	}
	var sb strings.Builder
	for i, line := range lp.lines {
		sb.WriteString(lp.renderLine(i, line))
		sb.WriteByte('\n')
	}
	lp.vp.SetContent(sb.String())
	if lp.followMode {
		lp.vp.GotoBottom()
	}
}

func (lp *logsPanel) renderLine(idx int, line string) string {
	colored := colorizeLog(line)
	if lp.visualMode && idx >= lp.selStart && idx <= lp.selEnd {
		return styleVisualLine.Render(colored)
	}
	if idx == lp.cursor {
		return styleSelectedLine.Render(colored)
	}
	return styleMuted.Render(colored)
}

// colorizeLog wraps HTTP status codes (2xx/4xx/5xx) with palette colors.
func colorizeLog(line string) string {
	return httpStatusRe.ReplaceAllStringFunc(line, func(code string) string {
		switch code[0] {
		case '2':
			return styleGreen.Render(code)
		case '4':
			return styleYellow.Render(code)
		case '5':
			return styleRed.Render(code)
		}
		return code
	})
}

func (lp *logsPanel) copyLine() string {
	if len(lp.lines) == 0 || lp.cursor >= len(lp.lines) {
		return ""
	}
	return lp.lines[lp.cursor]
}

func (lp *logsPanel) copySelection() string {
	if !lp.visualMode {
		return ""
	}
	start, end := lp.selStart, lp.selEnd
	if start > end {
		start, end = end, start
	}
	if end >= len(lp.lines) {
		end = len(lp.lines) - 1
	}
	return strings.Join(lp.lines[start:end+1], "\n")
}

func (lp *logsPanel) enterVisual() {
	lp.visualMode = true
	lp.selStart = lp.cursor
	lp.selEnd = lp.cursor
}

func (lp *logsPanel) exitVisual() {
	lp.visualMode = false
}

func (lp *logsPanel) moveUp() {
	if lp.cursor > 0 {
		lp.cursor--
		lp.followMode = false
		if lp.visualMode {
			lp.selEnd = lp.cursor
		}
	}
}

func (lp *logsPanel) moveDown() {
	if lp.cursor < len(lp.lines)-1 {
		lp.cursor++
		lp.followMode = false
		if lp.visualMode {
			lp.selEnd = lp.cursor
		}
	}
}

func (lp *logsPanel) resize(w, h int) {
	lp.vp.Width = w
	lp.vp.Height = h
}

// logName extracts "myservice" from "/path/to/logs/myservice.log".
func logName(path string) string {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	base = strings.TrimSuffix(base, ".log")
	return base
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/tui/... -run "TestColorize|TestLogsPanel" -v
```

Expected: all 12 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/logs.go internal/tui/logs_test.go
git commit -m "feat(tui): logs panel — file tail, visual selection, copy, colorize"
```

---

## Task 7: Details Panel

**Files:**
- Create: `internal/tui/details.go`
- Create: `internal/tui/details_test.go`

- [ ] **Step 1: Write tests for pure helpers**

```go
// internal/tui/details_test.go
package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatUptime_Hours(t *testing.T) {
	assert.Equal(t, "2h 14m 32s", formatUptime(2*3600+14*60+32))
}

func TestFormatUptime_Minutes(t *testing.T) {
	assert.Equal(t, "3m 0s", formatUptime(180))
}

func TestFormatUptime_Seconds(t *testing.T) {
	assert.Equal(t, "45s", formatUptime(45))
}

func TestFormatUptime_Zero(t *testing.T) {
	assert.Equal(t, "—", formatUptime(0))
}

func TestFormatBytes_Megabytes(t *testing.T) {
	assert.Equal(t, "182M", formatBytes(182*1024*1024))
}

func TestFormatBytes_Kilobytes(t *testing.T) {
	assert.Equal(t, "512K", formatBytes(512*1024))
}

func TestFormatBytes_Bytes(t *testing.T) {
	assert.Equal(t, "100B", formatBytes(100))
}
```

- [ ] **Step 2: Run to confirm compilation failure**

```bash
go test ./internal/tui/... -run "TestFormatUptime|TestFormatBytes"
```

- [ ] **Step 3: Create `internal/tui/details.go`**

```go
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/ipc"
)

type detailsPanel struct{}

func (dp detailsPanel) render(svc *ipc.ServiceInfo, cfg *config.ServiceConfig, width, height int) string {
	if svc == nil {
		return styleMuted.Render("No service selected")
	}

	var sb strings.Builder

	// STATUS
	sb.WriteString(styleMuted.Render("STATUS") + "\n")
	statusRows := [][]string{
		{"state", renderStateLabel(svc.State)},
		{"pid", renderPID(svc.PID)},
		{"port", renderPort(svc.Port)},
		{"uptime", formatUptime(svc.UptimeSec)},
		{"cpu", renderCPUFull(svc.CPUPct)},
		{"mem", formatBytes(svc.MemBytes)},
		{"started", computeStarted(svc.UptimeSec)},
	}
	sb.WriteString(renderTable(statusRows))

	if cfg == nil {
		return sb.String()
	}

	// CONFIG
	sb.WriteString("\n" + styleMuted.Render("CONFIG") + "\n")
	cfgRows := [][]string{
		{"cmd", styleText.Render(cfg.Command)},
		{"cwd", styleMuted.Render(cfg.CWD)},
	}
	if cfg.Group != "" {
		cfgRows = append(cfgRows, []string{"group", styleMuted.Render(cfg.Group)})
	}
	sb.WriteString(renderTable(cfgRows))

	// ENV (omit section if empty)
	if len(cfg.Env) > 0 {
		sb.WriteString("\n" + styleMuted.Render("ENV") + "\n")
		keys := sortedStringKeys(cfg.Env)
		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("  %s=%s\n",
				styleMuted.Render(k),
				styleAccent.Render(cfg.Env[k]),
			))
		}
	}

	return lipgloss.NewStyle().Width(width).Height(height).Render(sb.String())
}

func renderStateLabel(state string) string {
	switch state {
	case "running":
		return styleGreen.Render("● running")
	case "crashed":
		return styleRed.Render("● crashed")
	default:
		return styleMuted.Render("● " + state)
	}
}

func renderPID(pid *int) string {
	if pid == nil {
		return styleMuted.Render("—")
	}
	return fmt.Sprintf("%d", *pid)
}

func renderPort(port *int) string {
	if port == nil || *port == 0 {
		return styleMuted.Render("—")
	}
	return styleAccent.Render(fmt.Sprintf(":%d", *port))
}

func renderCPUFull(pct float64) string {
	s := fmt.Sprintf("%.1f%%", pct)
	if pct > 80 {
		return styleRed.Render(s)
	}
	if pct > 50 {
		return styleYellow.Render(s)
	}
	return s
}

func computeStarted(uptimeSec int64) string {
	if uptimeSec <= 0 {
		return styleMuted.Render("—")
	}
	t := time.Now().Add(-time.Duration(uptimeSec) * time.Second)
	return styleMuted.Render(t.Format("15:04:05"))
}

func formatUptime(sec int64) string {
	if sec <= 0 {
		return "—"
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%dM", b/1024/1024)
	case b >= 1024:
		return fmt.Sprintf("%dK", b/1024)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func renderTable(rows [][]string) string {
	maxW := 0
	for _, r := range rows {
		if len(r[0]) > maxW {
			maxW = len(r[0])
		}
	}
	var sb strings.Builder
	for _, r := range rows {
		label := styleMuted.Render(fmt.Sprintf("%-*s", maxW+2, r[0]))
		sb.WriteString("  " + label + r[1] + "\n")
	}
	return sb.String()
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/tui/... -run "TestFormatUptime|TestFormatBytes" -v
```

Expected: all 7 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/details.go internal/tui/details_test.go
git commit -m "feat(tui): details panel — STATUS/CONFIG/ENV sections"
```

---

## Task 8: Root Model

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/model_test.go`

Remove the temporary type declarations from `footer.go` (tabKind, focusKind, tabLogs, tabDetails, focusSidebar, focusMain) and define them canonically here.

- [ ] **Step 1: Write failing test**

```go
// internal/tui/model_test.go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestModel_WindowSizeSetsWidthHeight(t *testing.T) {
	m := model{}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := m2.(model)
	assert.Equal(t, 120, mm.width)
	assert.Equal(t, 40, mm.height)
}

func TestModel_QuitKeyReturnsQuitCmd(t *testing.T) {
	m := model{}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	// tea.Quit is a non-nil command
	assert.NotNil(t, cmd)
}
```

- [ ] **Step 2: Run to confirm compilation failure**

```bash
go test ./internal/tui/... -run TestModel
```

- [ ] **Step 3: Create `internal/tui/model.go`**

```go
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
type toastTickMsg  struct{ dt time.Duration }
type daemonRespMsg struct{ payload ipc.ListResponsePayload }
type daemonErrMsg  struct{ err error }
type logPollMsg    struct{ changed bool }

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
		mainW := m.width - sidebarW - 1 // 1 for divider
		mainH := m.height - 3            // header + footer
		m.logsC.resize(mainW, mainH-2)   // 2 for tab bar
		return m, nil

	case daemonTickMsg:
		m.spinning = true
		return m, m.pollDaemon()

	case daemonRespMsg:
		m.spinning = false
		m.sidebarC.update(msg.payload.Services)
		// Update log file path if selected service changed
		if svc := m.sidebarC.selectedService(); svc != nil {
			path := filepath.Join(m.logDir, "logs", svc.Name+".log")
			if path != m.logsC.filePath {
				m.logsC.setFile(path)
			}
		}
		return m, tickDaemon()

	case daemonErrMsg:
		m.spinning = false
		m.footerC.showToast(fmt.Sprintf("error: %s", msg.err))
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
				m.footerC.showToast("Copy failed")
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
		return daemonTickMsg{} // trigger immediate refresh
	}
}

func (m model) doStop() tea.Cmd {
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
	bodyH := m.height - 2 // header(1) + footer(1) + borders(2) ≈ header+footer

	// Header
	total := len(m.sidebarC.services)
	running := 0
	for _, s := range m.sidebarC.services {
		if s.State == "running" {
			running++
		}
	}
	header := m.headerC.render(total, running, m.spinFrame, m.spinning, m.width)

	// Sidebar
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
	footer := m.footerC.render(m.activeTab, m.focus, m.logsC.visualMode, m.width)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
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

	contentH := h - 2 // subtract tab bar + border

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
```

- [ ] **Step 4: Remove temporary type declarations from `footer.go`**

Delete the lines in `footer.go` that declared `tabKind`, `focusKind`, `tabLogs`, `tabDetails`, `focusSidebar`, `focusMain` (added as placeholders in Task 4). They now live in `model.go`.

- [ ] **Step 5: Run all TUI tests**

```bash
go test ./internal/tui/... -v
```

Expected: all tests pass, no compilation errors.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go internal/tui/footer.go
git commit -m "feat(tui): root bubbletea model — wires all components, handles all events"
```

---

## Task 9: CLI Integration

**Files:**
- Modify: `internal/cli/root.go`

This is the final wiring: add `RunE` to `rootCmd` and a `tui.Run()` entry point.

- [ ] **Step 1: Add `Run` function to `internal/tui/model.go`**

Append at the bottom of `model.go`:

```go
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
```

- [ ] **Step 2: Modify `internal/cli/root.go`**

Replace the full file content:

```go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hailerity/procet/internal/client"
	"github.com/hailerity/procet/internal/config"
	"github.com/hailerity/procet/internal/daemon"
	"github.com/hailerity/procet/internal/tui"
)

var rootCmd = &cobra.Command{
	Use:   "procet",
	Short: "A lightweight process manager for developers",
	// RunE is called when no subcommand is given — launches the TUI.
	RunE: func(cmd *cobra.Command, args []string) error {
		socketPath := config.SocketPath()
		if err := daemon.EnsureDaemon(socketPath); err != nil {
			return fmt.Errorf("start daemon: %w", err)
		}
		c, err := client.Connect(socketPath)
		if err != nil {
			return fmt.Errorf("connect to daemon: %w", err)
		}
		defer c.Close()

		reg, err := config.LoadRegistry(config.RegistryPath())
		if err != nil {
			// Empty registry is fine — TUI shows "No services" placeholder
			reg = &config.Registry{Services: map[string]*config.ServiceConfig{}}
		}

		logDir := config.DataDir()
		return tui.Run(c, reg, logDir)
	},
}

// Execute is the CLI entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		addCmd,
		removeCmd,
		startCmd,
		stopCmd,
		listCmd,
		logsCmd,
		fgCmd,
	)
}
```

Note: `config.LoadRegistry` is the function in `internal/config/registry.go` that reads `services.yaml`. Check the actual function signature in that file before using — it may be `config.LoadRegistry(path string) (*config.Registry, error)`.

- [ ] **Step 3: Build the binary**

```bash
go build -o bin/procet ./cmd/procet/
```

Expected: compiles with no errors.

- [ ] **Step 4: Run all unit tests**

```bash
go test ./internal/...
```

Expected: all unit tests pass.

- [ ] **Step 5: Smoke test — run TUI**

```bash
./bin/procet
```

Expected: full-screen TUI launches. Press `q` to quit.

If no services are registered yet:
```bash
./bin/procet add test-svc --command "echo hello" && ./bin/procet start test-svc
./bin/procet
```

Expected: TUI shows `test-svc` in the sidebar with a green dot.

- [ ] **Step 6: Test that existing subcommands still work**

```bash
./bin/procet list
./bin/procet --help
```

Expected: both work as before; TUI is not launched.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/root.go internal/tui/model.go
git commit -m "feat(tui): wire TUI into rootCmd.RunE — procet with no args launches dashboard"
```

---

## Verification Checklist

Run these before marking the feature complete:

```bash
# All unit tests
go test ./internal/...

# Integration tests (requires built binary)
go test -tags integration ./internal/integration/... -v

# Build
go build -o bin/procet ./cmd/procet/

# Manual TUI smoke test
./bin/procet

# Existing CLI unaffected
./bin/procet list
./bin/procet --help
./bin/procet start --help
```
