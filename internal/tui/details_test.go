package tui

import (
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
}

func TestRenderStateLabel_Stopped(t *testing.T) {
	out := renderStateLabel("stopped")
	assert.Contains(t, out, "stopped")
}
