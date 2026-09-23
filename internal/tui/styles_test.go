package tui

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// relLuminance is WCAG 2.1's relative luminance for an #rrggbb colour.
func relLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	h := strings.TrimPrefix(hex, "#")
	require.Len(t, h, 6, "expected #rrggbb, got %q", hex)

	chan_ := func(i int) float64 {
		v, err := strconv.ParseUint(h[i:i+2], 16, 8)
		require.NoError(t, err)
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*chan_(0) + 0.7152*chan_(2) + 0.0722*chan_(4)
}

// contrastRatio is WCAG 2.1's ratio between two colours, 1:1 to 21:1.
func contrastRatio(t *testing.T, a, b string) float64 {
	t.Helper()
	la, lb := relLuminance(t, a), relLuminance(t, b)
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

// A terminal's background is whatever the user set, not the one a web palette
// assumes — GitHub's own border tone measured 1.09–1.38:1 against these, which
// does not read as a line at all.
func TestColorBorder_ReadsAsALineOnCommonTerminals(t *testing.T) {
	const minUIContrast = 3.0

	dark := map[string]string{
		"GitHub Dark":  "#0d1117",
		"true black":   "#000000",
		"VS Code dark": "#1e1e1e",
		"One Dark":     "#282c34",
		"Nord":         "#2e3440",
	}
	for name, bg := range dark {
		got := contrastRatio(t, colorBorder.Dark, bg)
		assert.GreaterOrEqualf(t, got, minUIContrast,
			"unfocused border %s on %s (%s) is %.2f:1, below %.1f:1 — it stops reading as an edge",
			colorBorder.Dark, name, bg, got, minUIContrast)
	}

	light := map[string]string{
		"white":        "#ffffff",
		"GitHub Light": "#f6f8fa",
		"off-white":    "#fafafa",
	}
	for name, bg := range light {
		got := contrastRatio(t, colorBorder.Light, bg)
		assert.GreaterOrEqualf(t, got, minUIContrast,
			"unfocused border %s on %s (%s) is %.2f:1, below %.1f:1 — it stops reading as an edge",
			colorBorder.Light, name, bg, got, minUIContrast)
	}
}

// Focus is signalled by the edge changing colour, so the two tones have to be
// told apart — by hue as well as brightness, since they sit on the same
// background.
func TestColorBorder_FocusedEdgeIsDistinctFromIdle(t *testing.T) {
	assert.NotEqual(t, colorBorder.Dark, colorAccent.Dark)
	assert.NotEqual(t, colorBorder.Light, colorAccent.Light)

	got := contrastRatio(t, colorAccent.Dark, colorBorder.Dark)
	assert.Greaterf(t, got, 1.5,
		"focused (%s) and idle (%s) edges are only %.2f:1 apart", colorAccent.Dark, colorBorder.Dark, got)
}

// A focused pane's edge has to clear the same bar as an idle one — it is the
// brighter of the two, so this is a floor, not a coincidence.
func TestColorAccent_ReadsOnDarkTerminals(t *testing.T) {
	for _, bg := range []string{"#0d1117", "#000000", "#1e1e1e", "#282c34", "#2e3440"} {
		assert.GreaterOrEqual(t, contrastRatio(t, colorAccent.Dark, bg), 3.0)
	}
}

// lipgloss resolves an AdaptiveColor by terminal background, so both sides must
// be set — a blank one silently renders as the terminal default.
func TestPalette_BothSidesSet(t *testing.T) {
	for name, c := range map[string]lipgloss.AdaptiveColor{
		"text": colorText, "muted": colorMuted, "accent": colorAccent,
		"green": colorGreen, "red": colorRed, "yellow": colorYellow,
		"border": colorBorder, "bar": colorBar, "chip": colorChip,
	} {
		assert.NotEmptyf(t, c.Light, "%s has no light side", name)
		assert.NotEmptyf(t, c.Dark, "%s has no dark side", name)
	}
}
