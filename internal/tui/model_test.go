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
	assert.NotNil(t, cmd)
}
