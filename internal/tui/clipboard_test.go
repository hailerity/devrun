package tui

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClipboard_NoneAvailable(t *testing.T) {
	cb := detectClipboardWith(func(string) bool { return false })
	assert.False(t, cb.Available())
}

func TestClipboard_PbcopyOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	cb := detectClipboardWith(func(name string) bool { return name == "pbcopy" })
	assert.True(t, cb.Available())
}

func TestClipboard_XclipOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("linux only")
	}
	cb := detectClipboardWith(func(name string) bool { return name == "xclip" })
	assert.True(t, cb.Available())
}

func TestClipboard_XselFallbackOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("linux only")
	}
	// xclip not found, xsel found
	cb := detectClipboardWith(func(name string) bool { return name == "xsel" })
	assert.True(t, cb.Available())
}
