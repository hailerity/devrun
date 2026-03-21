package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFooter_ToastAppearsAndClears(t *testing.T) {
	f := &footerBar{}
	f.showToast("Copied!")
	assert.Equal(t, "Copied!", f.toast)

	f.tick(1 * time.Second)
	assert.Equal(t, "Copied!", f.toast) // still visible

	f.tick(600 * time.Millisecond) // total 1.6s > 1.5s threshold
	assert.Equal(t, "", f.toast)   // cleared
}

func TestFooter_ToastNoOpWhenEmpty(t *testing.T) {
	f := &footerBar{}
	f.tick(5 * time.Second) // should not panic
	assert.Equal(t, "", f.toast)
}

func TestFooter_ToastResetOnNew(t *testing.T) {
	f := &footerBar{}
	f.showToast("first")
	f.tick(1 * time.Second)
	f.showToast("second") // resets timer
	f.tick(1 * time.Second)
	assert.Equal(t, "second", f.toast) // 1s < 1.5s, still showing
}
