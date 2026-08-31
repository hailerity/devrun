package tui

import (
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestEditPanel_OpenForPrefills(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "yarn dev", CWD: "/app"})

	assert.True(t, p.open)
	assert.Equal(t, fieldName, p.focus)
	name, command, cwd := p.values()
	assert.Equal(t, "web", name)
	assert.Equal(t, "yarn dev", command)
	assert.Equal(t, "/app", cwd)
}

func TestEditPanel_FocusDeltaWraps(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "x"})

	p.focusDelta(1)
	assert.Equal(t, fieldCommand, p.focus)
	p.focusDelta(1)
	assert.Equal(t, fieldCWD, p.focus)
	p.focusDelta(1)
	assert.Equal(t, fieldName, p.focus, "wraps to the first field")
	p.focusDelta(-1)
	assert.Equal(t, fieldCWD, p.focus, "wraps backwards")
}

func TestEditPanel_Validate(t *testing.T) {
	existing := map[string]bool{"web": true, "api": true}

	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "run"})
	assert.Empty(t, p.validate(existing), "unchanged name + non-empty command is valid")

	p.inputs[fieldName].SetValue("  ")
	assert.Contains(t, p.validate(existing), "name")

	p.inputs[fieldName].SetValue("web")
	p.inputs[fieldCommand].SetValue("   ")
	assert.Contains(t, p.validate(existing), "command")

	p.inputs[fieldCommand].SetValue("run")
	p.inputs[fieldName].SetValue("api")
	assert.Contains(t, p.validate(existing), "already exists", "rename onto another service is rejected")

	p.inputs[fieldName].SetValue("newname")
	assert.Empty(t, p.validate(existing), "rename to a free name is valid")
}

func TestEditPanel_ValuesTrim(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "run"})
	p.inputs[fieldName].SetValue("  ui  ")
	p.inputs[fieldCWD].SetValue("  /srv  ")
	name, _, cwd := p.values()
	assert.Equal(t, "ui", name)
	assert.Equal(t, "/srv", cwd)
}

func TestEditPanel_CloseBlurs(t *testing.T) {
	p := newEditPanel()
	p.openFor("web", &config.ServiceConfig{Command: "run"})
	p.close()
	assert.False(t, p.open)
	assert.False(t, p.inputs[fieldName].Focused())
}
