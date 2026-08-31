package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTargetEditPanel_OpenForPrefills(t *testing.T) {
	p := newTargetEditPanel()
	p.openFor("stack", []string{"web", "api", "db"}, []string{"api"})

	assert.True(t, p.open)
	assert.True(t, p.focusName)
	name, members := p.values()
	assert.Equal(t, "stack", name)
	assert.Equal(t, []string{"api"}, members)
	assert.Equal(t, []string{"api", "db", "web"}, p.services, "services listed sorted")
}

func TestTargetEditPanel_ToggleAndMove(t *testing.T) {
	p := newTargetEditPanel()
	p.openFor("t", []string{"web", "api", "db"}, nil) // sorted: api, db, web
	p.focusSwap()                                     // focus the list

	p.toggleAtCursor() // api on
	p.moveCursor(2)    // -> web
	p.toggleAtCursor() // web on

	_, members := p.values()
	assert.Equal(t, []string{"api", "web"}, members)

	p.moveCursor(-1)   // -> db
	p.moveCursor(-1)   // -> api
	p.toggleAtCursor() // api off
	_, members = p.values()
	assert.Equal(t, []string{"web"}, members)
}

func TestTargetEditPanel_MoveCursorClamps(t *testing.T) {
	p := newTargetEditPanel()
	p.openFor("t", []string{"a", "b"}, nil)
	p.focusSwap()
	p.moveCursor(-5)
	assert.Equal(t, 0, p.cursor)
	p.moveCursor(99)
	assert.Equal(t, 1, p.cursor)
}

func TestTargetEditPanel_WindowScrolls(t *testing.T) {
	var svcs []string
	for i := 'a'; i <= 'z'; i++ {
		svcs = append(svcs, string(i))
	}
	p := newTargetEditPanel()
	p.openFor("t", svcs, nil)
	p.focusSwap()
	p.moveCursor(targetEditRows + 2)
	assert.GreaterOrEqual(t, p.cursor, p.top, "cursor at or below the window top")
	assert.Less(t, p.cursor, p.top+targetEditRows, "cursor within the visible window")
}

func TestTargetEditPanel_Validate(t *testing.T) {
	existing := map[string]bool{"stack": true, "fe": true}

	p := newTargetEditPanel()
	p.openFor("stack", []string{"web"}, []string{"web"})
	assert.Empty(t, p.validate(existing), "unchanged name is valid")

	p.nameInput.SetValue("  ")
	assert.Contains(t, p.validate(existing), "name")

	p.nameInput.SetValue("fe")
	assert.Contains(t, p.validate(existing), "already exists")

	p.nameInput.SetValue("brand-new")
	assert.Empty(t, p.validate(existing))
}

func TestTargetEditPanel_CloseBlurs(t *testing.T) {
	p := newTargetEditPanel()
	p.openFor("t", []string{"a"}, nil)
	p.close()
	assert.False(t, p.open)
	assert.False(t, p.nameInput.Focused())
}
