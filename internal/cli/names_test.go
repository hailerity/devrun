package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
)

// The argument checks run before any config is touched, so a bad name never
// reaches a file.
func TestAddArgs_RejectsUnsafeNames(t *testing.T) {
	for _, bad := range []string{"../x", "a/b", ".hidden", "with space"} {
		err := addCmd.Args(addCmd, []string{bad, "sleep 1"})
		if assert.Error(t, err, bad) {
			assert.Contains(t, err.Error(), "invalid service name")
		}
	}
	assert.NoError(t, addCmd.Args(addCmd, []string{"web", "yarn dev"}))
	assert.Error(t, addCmd.Args(addCmd, []string{"web"}), "the argument count is still checked")
}

func TestTargetArgs_NewNamesFollowTheRuleExistingOnesKeepWorking(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	work := filepath.Join(tmp, "work")
	require.NoError(t, os.Mkdir(work, 0755))
	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(work))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	require.NoError(t, config.SaveRegistry(config.RegistryPath(), &config.Registry{Version: "1",
		Services: map[string]*config.ServiceConfig{"web": {Name: "web", Command: "x"}},
		Targets:  map[string][]string{"front end": {"web"}}}))

	assert.Contains(t, targetCreateCmd.Args(targetCreateCmd, []string{"a/b"}).Error(), "invalid target name")
	assert.NoError(t, targetCreateCmd.Args(targetCreateCmd, []string{"frontend"}))

	assert.Contains(t, targetAddCmd.Args(targetAddCmd, []string{"../t", "web"}).Error(), "invalid target name")
	assert.NoError(t, targetAddCmd.Args(targetAddCmd, []string{"front end", "web"}),
		"an existing target predating the rule can still be added to")
}
