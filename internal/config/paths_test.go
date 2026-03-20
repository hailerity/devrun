package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/procet/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestPaths_XDGOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	assert.Equal(t, filepath.Join(tmp, "procet"), config.ConfigDir())
	assert.Equal(t, filepath.Join(tmp, "procet"), config.DataDir())
	assert.Equal(t, filepath.Join(tmp, "procet", "services.yaml"), config.RegistryPath())
	assert.Equal(t, filepath.Join(tmp, "procet", "procet.sock"), config.SocketPath())
	assert.Equal(t, filepath.Join(tmp, "procet", "state.json"), config.StatePath())
	assert.Equal(t, filepath.Join(tmp, "procet", "logs", "web.log"), config.LogPath("web"))
}

func TestPaths_HomeDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".config", "procet"), config.ConfigDir())
	assert.Equal(t, filepath.Join(home, ".local", "share", "procet"), config.DataDir())
	assert.Equal(t, filepath.Join(home, ".config", "procet", "services.yaml"), config.RegistryPath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "procet", "procet.sock"), config.SocketPath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "procet", "state.json"), config.StatePath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "procet", "logs", "web.log"), config.LogPath("web"))
}
