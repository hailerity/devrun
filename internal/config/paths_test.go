package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaths_XDGOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	assert.Equal(t, filepath.Join(tmp, "devrun"), config.ConfigDir())
	assert.Equal(t, filepath.Join(tmp, "devrun"), config.DataDir())
	assert.Equal(t, filepath.Join(tmp, "devrun", "services.yaml"), config.RegistryPath())
	assert.Equal(t, filepath.Join(tmp, "devrun", "devrun.sock"), config.SocketPath())
	assert.Equal(t, filepath.Join(tmp, "devrun", "state.json"), config.StatePath())
	assert.Equal(t, filepath.Join(tmp, "devrun", "logs", "web.log"), config.LogPath("web"))
}

func TestPaths_HomeDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".config", "devrun"), config.ConfigDir())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun"), config.DataDir())
	assert.Equal(t, filepath.Join(home, ".config", "devrun", "services.yaml"), config.RegistryPath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun", "devrun.sock"), config.SocketPath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun", "state.json"), config.StatePath())
	assert.Equal(t, filepath.Join(home, ".local", "share", "devrun", "logs", "web.log"), config.LogPath("web"))
}

// A log devrun owns must never land on a log a service owns. Both were
// spelled LogPath("gateway") / LogPath("tunnel"), which are in the user's
// namespace, so a service of that name shared the file: its output was quoted
// back as the gateway's reason for failing to start.
//
// An earlier fix used a "_" prefix and asserted that ValidateName rejects it.
// That proved nothing: the check applied before a name becomes a path is
// SafeFileName, which accepts "_gateway" — so the invariant was false while
// this test passed. It asserts the load-bearing check now.
func TestInternalLogPath_CannotCollideWithAService(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	internalDir := filepath.Dir(config.InternalLogPath("gateway"))
	for _, name := range []string{"gateway", "tunnel", "_gateway", "_tunnel", "devrun"} {
		require.True(t, config.SafeFileName(name),
			"%q passes the only check standing between a name and a path", name)
		assert.Equal(t, filepath.Dir(config.LogPath(name)), filepath.Dir(internalDir),
			"a service log lands in logs/, never deeper")
		assert.NotEqual(t, config.LogPath(name), config.InternalLogPath("gateway"))
		assert.NotEqual(t, config.LogPath(name), config.InternalLogPath("tunnel"))
	}

	// The reason it holds: a name that could descend into the subdirectory is
	// refused before it ever reaches LogPath.
	for _, escape := range []string{"devrun/gateway", "devrun\\gateway", "..", "."} {
		assert.False(t, config.SafeFileName(escape), "%q must be refused", escape)
	}
}
