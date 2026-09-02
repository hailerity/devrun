package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveConfirm_OpenForAndClose(t *testing.T) {
	var c removeConfirm
	c.openFor("web")
	assert.True(t, c.open)
	assert.Equal(t, "web", c.name)

	c.errMsg = "boom"
	c.pending = true
	c.close()
	assert.False(t, c.open)
	assert.Empty(t, c.errMsg, "close clears a stale error")
	assert.False(t, c.pending, "close clears the in-flight flag")
}

func TestRemoveConfirm_ViewMentionsServiceAndKeys(t *testing.T) {
	var c removeConfirm
	c.openFor("api")
	out := c.view(80, 24)
	assert.Contains(t, out, "api")
	assert.Contains(t, out, "y remove")
	assert.Contains(t, out, "Esc")

	c.errMsg = "service \"api\" not found"
	assert.Contains(t, c.view(80, 24), "not found")
}

func TestRemoveConfirm_ViewGrowsToFitBox(t *testing.T) {
	var c removeConfirm
	c.openFor("web")
	// A height smaller than the rendered box must not clip it.
	out := c.view(80, 1)
	assert.GreaterOrEqual(t, strings.Count(out, "\n"), 6)
}
