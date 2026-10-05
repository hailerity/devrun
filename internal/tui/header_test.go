package tui

import (
	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/ipc"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// The header is exactly one row at every width. lipgloss's Width() style only
// pads a short line — it never caps a long one — and an overlong header wraps
// on the real terminal and scrolls the whole layout, so render() must truncate.

func TestHeaderBar_NeverWrapsAtNarrowWidth(t *testing.T) {
	h := headerBar{}
	out := h.render("a-very-long-project-name · devrun.yaml", 999, 999, 999, 0, true, nil, 15)
	assert.Equal(t, 1, lipgloss.Height(out))
	assert.LessOrEqual(t, lipgloss.Width(out), 15)
}

func TestHeaderBar_ShowsSourceAndCounts(t *testing.T) {
	h := headerBar{}
	out := plain(h.render("shop · devrun.yaml", 5, 3, 0, 0, false, nil, 80))
	assert.Equal(t, 1, lipgloss.Height(out))
	assert.Equal(t, 80, lipgloss.Width(out), "the right-hand counts sit against the right edge")
	assert.Contains(t, out, "devrun")
	assert.Contains(t, out, "shop · devrun.yaml")
	assert.Contains(t, out, "3/5 running")
	assert.NotContains(t, out, "crashed", "no crashed count while nothing is down")
}

func TestHeaderBar_CrashedCountAppearsWhenSomethingIsDown(t *testing.T) {
	h := headerBar{}
	assert.Contains(t, plain(h.render("", 5, 3, 1, 0, false, nil, 80)), "✖ 1 crashed")
}

// The counts outrank the config label: when both cannot fit, the label goes.
func TestHeaderBar_DropsSourceBeforeCounts(t *testing.T) {
	h := headerBar{}
	out := plain(h.render("a-very-long-project-name · devrun.yaml", 5, 3, 1, 0, false, nil, 44))
	assert.Contains(t, out, "3/5 running")
	assert.Contains(t, out, "1 crashed")
	assert.NotContains(t, out, "a-very-long-project-name")
}

// Published is the state worth noticing: something off this machine can
// reach the services. Local is informational, so it stays muted.
func TestHeader_GatewayIndicator(t *testing.T) {
	h := headerBar{}

	assert.NotContains(t, plain(h.render("", 1, 1, 0, 0, false, nil, 80)), "gateway",
		"no gateway, no word about one")

	local := plain(h.render("", 1, 1, 0, 0, false,
		&ipc.GatewayStatusPayload{Running: true, Posture: config.PostureAuto}, 80))
	assert.Contains(t, local, "gateway")
	assert.NotContains(t, local, "published")

	published := plain(h.render("", 1, 1, 0, 0, false,
		&ipc.GatewayStatusPayload{Running: true, Posture: config.PosturePublished}, 80))
	assert.Contains(t, published, "published")

	// A tunnel says so, since "published" alone does not distinguish one
	// devrun runs from a proxy someone else put in front.
	tunnelled := plain(h.render("", 1, 1, 0, 0, false, &ipc.GatewayStatusPayload{
		Running: true, Posture: config.PosturePublished,
		Tunnel: &ipc.TunnelStatusPayload{Running: true, Kind: "named"},
	}, 80))
	assert.Contains(t, tunnelled, "tunnelled")
}

// The bug this guards: the header read the *configured* posture, which
// `devrun tunnel up` does not change — the gateway decides it is published at
// request time from Snapshot().Tunnelled. So a tunnel on a default-posture
// gateway drew the quiet local label while the services were on the internet.
func TestGatewayLabel_ATunnelIsPublishedWhateverThePostureSays(t *testing.T) {
	got := plain(gatewayLabel(&ipc.GatewayStatusPayload{
		Running: true,
		Posture: config.PostureAuto,
		Exposed: []string{"web", "api"},
		Tunnel:  &ipc.TunnelStatusPayload{Running: true, Kind: "named"},
	}))
	assert.Contains(t, got, "tunnelled")
	assert.Contains(t, got, "2", "how much is behind the open door")
}

// Nothing exposed is the state a first tunnel lands in, and it looks like a
// broken tunnel: every service answers 404. The count has to say zero rather
// than go quiet.
func TestGatewayLabel_CountsZeroOutLoud(t *testing.T) {
	got := plain(gatewayLabel(&ipc.GatewayStatusPayload{
		Running: true,
		Tunnel:  &ipc.TunnelStatusPayload{Running: true, Kind: "quick"},
	}))
	assert.Contains(t, got, "tunnelled 0")
}

// A running gateway is something that is on, and grey reads as off.
func TestGatewayLabel_LocalIsGreenNotGrey(t *testing.T) {
	got := gatewayLabel(&ipc.GatewayStatusPayload{Running: true, Posture: config.PostureAuto})
	assert.Equal(t, styleGreen.Render("◎ gateway"), got)
	assert.NotEqual(t, styleMuted.Render("◎ gateway"), got)
}
