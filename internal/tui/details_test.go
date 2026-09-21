package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatUptimeFull_Hours(t *testing.T) {
	assert.Equal(t, "2h 14m 32s", formatUptimeFull(2*3600+14*60+32))
}

func TestFormatUptimeFull_Minutes(t *testing.T) {
	assert.Equal(t, "3m 0s", formatUptimeFull(180))
}

func TestFormatUptimeFull_Seconds(t *testing.T) {
	assert.Equal(t, "45s", formatUptimeFull(45))
}

func TestFormatUptimeFull_Zero(t *testing.T) {
	assert.Equal(t, "—", formatUptimeFull(0))
}

func TestRenderStateLabel_Running(t *testing.T) {
	out := renderStateLabel("running")
	assert.Contains(t, out, "running")
	assert.Contains(t, out, "●")
}

func TestRenderStateLabel_Crashed(t *testing.T) {
	out := renderStateLabel("crashed")
	assert.Contains(t, out, "crashed")
	assert.Contains(t, out, "✖")
}

func TestRenderStateLabel_Stopped(t *testing.T) {
	out := renderStateLabel("stopped")
	assert.Contains(t, out, "stopped")
	assert.Contains(t, out, "○")
}

func TestRenderCPUFull_LowUncolored(t *testing.T) {
	out := renderCPUFull(10.0)
	assert.Equal(t, "10.0%", out) // no ANSI codes for low CPU
}

func TestRenderCPUFull_MidYellow(t *testing.T) {
	out := renderCPUFull(60.0)
	assert.Contains(t, out, "60.0%")
	assert.Greater(t, len(out), len("60.0%")) // has ANSI codes
}

func TestRenderCPUFull_HighRed(t *testing.T) {
	out := renderCPUFull(90.0)
	assert.Contains(t, out, "90.0%")
	assert.Greater(t, len(out), len("90.0%")) // has ANSI codes
}

func detailFixture() (*ipc.ServiceInfo, *config.ServiceConfig) {
	return &ipc.ServiceInfo{Name: "api", State: "running", Port: intp(8080), PID: intp(4821), CPUPct: 2.1, UptimeSec: 8040, MemBytes: 1 << 20},
		&config.ServiceConfig{Name: "api", Command: "go run ./cmd/api", CWD: "/w/api", Group: "shop",
			Env: map[string]string{"PORT": "8080", "DATABASE_URL": "postgres://localhost/shop"}}
}

func TestDetailLines_SectionsValuesAndRawCopies(t *testing.T) {
	svc, cfg := detailFixture()
	lines := detailLines(svc, cfg)

	var headings, labels []string
	copies := map[string]string{}
	for _, l := range lines {
		switch {
		case l.value < 0 && plain(l.text) != "":
			headings = append(headings, plain(l.text))
		case l.value >= 0:
			labels = append(labels, l.label)
			copies[l.label] = l.copy
		}
	}
	assert.Equal(t, []string{"STATUS", "CONFIG", "ENV"}, headings)
	assert.Equal(t, []string{"state", "pid", "port", "uptime", "cpu", "mem", "started", "cmd", "cwd", "group", "DATABASE_URL", "PORT"}, labels,
		"value rows are numbered in display order, env keys sorted")
	for i, l := range labels {
		_ = i
		assert.NotContains(t, copies[l], "\x1b", "%s: the clipboard value carries no styling", l)
	}
	assert.Equal(t, "running", copies["state"], "the raw state, not the glyph + label")
	assert.Equal(t, "8080", copies["port"], "the port number, not the display form :8080")
	assert.Equal(t, "go run ./cmd/api", copies["cmd"])
	assert.Equal(t, "postgres://localhost/shop", copies["DATABASE_URL"])
}

func TestDetailLines_NoConfigOrNoService(t *testing.T) {
	svc, _ := detailFixture()
	lines := detailLines(svc, nil)
	assert.Equal(t, 7, valueCount(lines), "status rows only")
	assert.Nil(t, detailLines(nil, nil))

	// Values the daemon has not reported copy as empty, not as the "—" glyph.
	stopped := detailLines(&ipc.ServiceInfo{Name: "web", State: "stopped"}, nil)
	for _, l := range stopped {
		if l.label == "pid" || l.label == "port" {
			assert.Equal(t, "", l.copy, l.label)
		}
	}
}

func TestDetailsPanel_CursorClampsAndWindowFollows(t *testing.T) {
	svc, cfg := detailFixture()
	lines := detailLines(svc, cfg) // 12 values, 16 lines with headings and spacers
	dp := &detailsPanel{}
	dp.setRows(6)

	visible := func() bool {
		first, last := dp.window(lines)
		for i := first; i < last; i++ {
			if lines[i].value == dp.cursor {
				return true
			}
		}
		return false
	}
	dp.move(-1, lines)
	assert.Equal(t, 0, dp.cursor, "clamps at the top, no wrap")
	for i := 0; i < 20; i++ {
		require.True(t, visible(), "moving down: cursor %d top %d", dp.cursor, dp.top)
		dp.move(1, lines)
	}
	assert.Equal(t, 11, dp.cursor, "clamps at the last value")
	first, last := dp.window(lines)
	assert.Equal(t, 6, last-first, "the window stays full at the bottom")

	for i := 0; i < 20; i++ {
		require.True(t, visible(), "moving up: cursor %d top %d", dp.cursor, dp.top)
		dp.move(-1, lines)
	}
	assert.Equal(t, 0, dp.top, "back on the first value the STATUS heading is in view again")
}

// A poll can shrink the list under a parked cursor (the config lost its env).
func TestDetailsPanel_SurvivesAShrinkingList(t *testing.T) {
	svc, cfg := detailFixture()
	dp := &detailsPanel{}
	dp.setRows(5)
	dp.move(99, detailLines(svc, cfg))
	require.Equal(t, 11, dp.cursor)

	short := detailLines(svc, nil)
	dp.scrollToCursor(short)
	assert.Equal(t, 6, dp.cursor)
	require.NotNil(t, dp.selected(short))
	assert.Equal(t, "started", dp.selected(short).label)

	dp.scrollToCursor(nil)
	assert.Nil(t, dp.selected(nil), "no service: nothing selected, no panic")
}

func TestDetailsPanel_RenderFitsWidthAndMarksCursorOnlyWhenFocused(t *testing.T) {
	svc, cfg := detailFixture()
	cfg.Command = strings.Repeat("a-very-long-command ", 12)
	lines := detailLines(svc, cfg)
	dp := detailsPanel{}

	for _, w := range []int{10, 30, 60} {
		for _, focused := range []bool{false, true} {
			for i, row := range strings.Split(dp.render(lines, w, focused), "\n") {
				assert.LessOrEqual(t, lipgloss.Width(row), w, "width %d focused=%v row %d", w, focused, i)
			}
		}
	}
	assert.NotEqual(t, dp.render(lines, 60, false), dp.render(lines, 60, true), "the cursor row is highlighted with focus")
	// Focus changes only the highlight: swap the cursor row's gutter bar back to
	// the blank column it replaces, ignore the background padding, and every
	// row must read the same — same text, same column.
	normalise := func(out string) []string {
		rows := strings.Split(plain(out), "\n")
		for i, r := range rows {
			rows[i] = strings.TrimRight(strings.Replace(r, "│", " ", 1), " ")
		}
		return rows
	}
	assert.Equal(t, normalise(dp.render(lines, 60, false)), normalise(dp.render(lines, 60, true)))
	assert.Contains(t, plain(dp.render(lines, 40, false)), "…", "an over-long value is cut with an ellipsis")
}
