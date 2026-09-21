package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The overlay claims to be the whole keymap. Walk keyMap by reflection so a
// binding added later and never listed in helpGroups fails here instead of
// silently going undocumented.
func TestHelp_ListsEveryBinding(t *testing.T) {
	listed := map[string]bool{}
	for _, col := range helpGroups() {
		for _, g := range col {
			for _, b := range g.bindings {
				listed[b.Help().Key] = true
			}
		}
	}
	v := reflect.ValueOf(keys)
	for i := 0; i < v.NumField(); i++ {
		b := v.Field(i).Interface().(key.Binding)
		name := v.Type().Field(i).Name
		require.NotEmpty(t, b.Help().Key, "%s has no help text", name)
		assert.True(t, listed[b.Help().Key], "%s (%q) is missing from the help overlay", name, b.Help().Key)
	}
}

func TestHelp_ViewShowsGroupsAndDescriptions(t *testing.T) {
	out := plain(helpPanel{}.view())
	for _, want := range []string{"Keys", "MOVE", "SERVICES", "LOGS", "OTHER", "start everything listed", "filter by target", "Esc or ? to close"} {
		assert.Contains(t, out, want)
	}
}

func TestModel_HelpOpensTrapsKeysAndCloses(t *testing.T) {
	m := targetFilterModel(t)

	m = pressKey(m, '?')
	require.True(t, m.helpC.open)
	assert.Contains(t, plain(m.View()), "show this help")
	assert.Contains(t, plain(m.View()), "SERVICES", "the panes stay drawn behind the overlay")

	// Keys do not reach the panes behind it.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(model)
	assert.Equal(t, focusSidebar, m.focus)
	m = pressKey(m, 't')
	assert.False(t, m.pickerC.open, "t must not open the picker under the help overlay")
	assert.True(t, m.helpC.open)

	// q closes the overlay — it must not quit the app from inside help.
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = m2.(model)
	assert.False(t, m.helpC.open)
	assert.Nil(t, cmd, "q inside help closes it rather than quitting")

	for _, closer := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'?'}}} {
		m = pressKey(m, '?')
		require.True(t, m.helpC.open)
		m2, _ = m.Update(closer)
		m = m2.(model)
		assert.False(t, m.helpC.open, "%v closes help", closer)
	}
}

func TestModel_HelpFitsSmallTerminal(t *testing.T) {
	m := targetFilterModel(t)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m = pressKey(m2.(model), '?')
	assertViewFits(t, m, 40, 10)
}
