package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeMembers(t *testing.T) {
	assert.Equal(t, []string{"web", "api"}, mergeMembers(nil, []string{"web", "api"}))
	assert.Equal(t, []string{"web", "api", "db"},
		mergeMembers([]string{"web", "api"}, []string{"api", "db"}))
	assert.Equal(t, []string{"web"}, mergeMembers([]string{"web"}, []string{"web"}))
}

func TestDropMembers(t *testing.T) {
	assert.Equal(t, []string{"web"}, dropMembers([]string{"web", "api"}, []string{"api"}))
	assert.Equal(t, []string{}, dropMembers([]string{"web", "api"}, []string{"web", "api"}))
	assert.Equal(t, []string{"web", "db"},
		dropMembers([]string{"web", "api", "db"}, []string{"api", "missing"}))
}

// editTargets round-trips through the global registry when no devrun.yaml is in
// scope: it validates members against known services and writes the result back.
func TestEditTargets_GlobalRegistryRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Work from a directory with no devrun.yaml so Resolve picks the global registry.
	work := filepath.Join(tmp, "work")
	require.NoError(t, os.Mkdir(work, 0755))
	chdir(t, work)

	reg := &config.Registry{Version: "1", Services: map[string]*config.ServiceConfig{
		"web": {Name: "web", Command: "yarn dev"},
		"api": {Name: "api", Command: "go run ."},
	}}
	require.NoError(t, config.SaveRegistry(config.RegistryPath(), reg))

	// Unknown service is rejected.
	err := editTargets(func(targets map[string][]string, known map[string]bool) error {
		if !known["ghost"] {
			return assertUnknown()
		}
		return nil
	})
	require.Error(t, err)

	// Add web + api to a new target.
	require.NoError(t, editTargets(func(targets map[string][]string, _ map[string]bool) error {
		targets["project-1"] = mergeMembers(targets["project-1"], []string{"web", "api"})
		return nil
	}))

	got, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	assert.Equal(t, []string{"web", "api"}, got.Targets["project-1"])
}

func assertUnknown() error { return os.ErrInvalid }

// chdir switches into dir for the duration of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}
