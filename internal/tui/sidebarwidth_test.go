package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sidebar is sized from its longest service name, so that name has to fit.
// It did not: sidebarWidth budgeted the margin, state and CPU columns but not
// the exposed column, which serviceRow subtracts — so the pane came out two
// columns short of what it had just measured and clipped the very name it was
// sized for. Every list whose width that name decided drew it with an ellipsis,
// on every row, permanently.
func TestSidebar_TheLongestNameFitsThePaneItSized(t *testing.T) {
	for _, longest := range []string{"admin-api", "payments-api", "search-indexer"} {
		t.Run(longest, func(t *testing.T) {
			m := newModel("", &config.Registry{}, config.Source{}, "", clipboard{})
			m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			m = m2.(model)
			m.sidebarC.update([]ipc.ServiceInfo{
				{Name: longest, Group: "a", State: "running", CPUPct: 1},
				{Name: "web", Group: "b", State: "running", CPUPct: 1},
			}, nil)
			m.relayout()

			sideW, _ := m.paneWidths()
			w, _ := m.sidebarC.frame(true, sideW).innerSize(sideW, 20)
			out := plain(m.sidebarC.render(w))
			assert.Contains(t, out, longest,
				"the name the pane was sized for is drawn in full")
			assert.NotContains(t, out, "…",
				"nothing in this list is long enough to need truncating")
		})
	}
}

// And the pane still refuses to grow without bound: past sidebarMaxW a very
// long name is clipped, which is the point of the cap.
func TestSidebar_AnOverlongNameIsStillClipped(t *testing.T) {
	m := newModel("", &config.Registry{}, config.Source{}, "", clipboard{})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m = m2.(model)
	m.sidebarC.update([]ipc.ServiceInfo{
		{Name: strings.Repeat("x", 80), Group: "a", State: "running"},
	}, nil)
	m.relayout()

	require.LessOrEqual(t, m.sidebarWidth(), sidebarMaxW, "the cap holds")
	sideW, _ := m.paneWidths()
	w, _ := m.sidebarC.frame(true, sideW).innerSize(sideW, 20)
	assert.Contains(t, plain(m.sidebarC.render(w)), "…")
}
