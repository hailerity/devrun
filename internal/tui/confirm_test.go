package tui

import (
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
	out := c.view()
	assert.Contains(t, out, "api")
	assert.Contains(t, out, "y remove")
	assert.Contains(t, out, "Esc")

	c.errMsg = "service \"api\" not found"
	assert.Contains(t, c.view(), "not found")
}
