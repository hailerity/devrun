package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	// Force lipgloss to emit ANSI escape codes in non-TTY test environments.
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}
