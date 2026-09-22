package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sandbox points devrun's config and data dirs at a temp dir and returns a
// working directory inside it with no devrun.yaml, so the global registry is
// in scope.
func sandbox(t *testing.T) (work string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	work = filepath.Join(tmp, "work")
	require.NoError(t, os.Mkdir(work, 0755))
	return work
}

// project writes a devrun.yaml into a new directory under the sandbox.
func project(t *testing.T, yaml string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "proj")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte(yaml), 0644))
	return dir
}

func TestMergeMembers(t *testing.T) {
	assert.Equal(t, []string{"web", "api"}, MergeMembers(nil, []string{"web", "api"}))
	assert.Equal(t, []string{"web", "api", "db"},
		MergeMembers([]string{"web", "api"}, []string{"api", "db"}))
	assert.Equal(t, []string{"web"}, MergeMembers([]string{"web"}, []string{"web"}))
}

func TestDropMembers(t *testing.T) {
	assert.Equal(t, []string{"web"}, DropMembers([]string{"web", "api"}, []string{"api"}))
	assert.Equal(t, []string{}, DropMembers([]string{"web", "api"}, []string{"web", "api"}))
	assert.Equal(t, []string{"web", "db"},
		DropMembers([]string{"web", "api", "db"}, []string{"api", "missing"}))
}

// EditTargets round-trips through the global registry when no devrun.yaml is in
// scope: it validates members against known services and writes the result back.
func TestEditTargets_GlobalRegistryRoundTrip(t *testing.T) {
	work := sandbox(t)
	reg := &config.Registry{Version: "1", Services: map[string]*config.ServiceConfig{
		"web": {Name: "web", Command: "yarn dev"},
		"api": {Name: "api", Command: "go run ."},
	}}
	require.NoError(t, config.SaveRegistry(config.RegistryPath(), reg))
	s := Scope{Dir: work}

	_, err := AddToTarget(s, "project-1", []string{"ghost"})
	assert.EqualError(t, err, `service "ghost" is not defined in this config`)

	members, err := AddToTarget(s, "project-1", []string{"web", "api"})
	require.NoError(t, err)
	assert.Equal(t, []string{"web", "api"}, members)

	got, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	assert.Equal(t, []string{"web", "api"}, got.Targets["project-1"])
}

func TestTargets_ProjectScopeCreateAddRemove(t *testing.T) {
	sandbox(t)
	dir := project(t, "services:\n  web:\n    command: a\n  api:\n    command: b\n")
	s := Scope{Dir: dir}

	require.NoError(t, CreateTarget(s, "fe"))
	assert.EqualError(t, CreateTarget(s, "fe"), `target "fe" already exists`)

	members, err := AddToTarget(s, "fe", []string{"web", "api", "web"})
	require.NoError(t, err)
	assert.Equal(t, []string{"web", "api"}, members, "duplicates are merged")

	require.NoError(t, RemoveFromTarget(s, "fe", []string{"api"}))
	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"web"}, proj.Targets["fe"])

	require.NoError(t, RemoveFromTarget(s, "fe", nil))
	proj, err = config.LoadProject(dir)
	require.NoError(t, err)
	assert.NotContains(t, proj.Targets, "fe")
	assert.EqualError(t, RemoveFromTarget(s, "fe", nil), `target "fe" does not exist`)

	// The project file was edited; the global registry was not touched.
	_, err = os.Stat(config.RegistryPath())
	assert.True(t, os.IsNotExist(err))
}

// Global on a directory that has a devrun.yaml must still edit the registry.
func TestTargets_GlobalFlagBypassesProjectFile(t *testing.T) {
	sandbox(t)
	dir := project(t, "services:\n  web:\n    command: a\n")
	require.NoError(t, config.SaveRegistry(config.RegistryPath(), &config.Registry{Version: "1",
		Services: map[string]*config.ServiceConfig{"g": {Name: "g", Command: "x"}}}))

	_, err := AddToTarget(Scope{Dir: dir, Global: true}, "t", []string{"g"})
	require.NoError(t, err)
	reg, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	assert.Equal(t, []string{"g"}, reg.Targets["t"])
}

func TestAddService_Global(t *testing.T) {
	work := sandbox(t)
	s := Scope{Dir: work}

	res, err := AddService(s, NewService{Name: "web", Command: "yarn dev", Env: map[string]string{"A": "1"}, Group: "g"})
	require.NoError(t, err)
	assert.False(t, res.Local)

	reg, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	web := reg.Services["web"]
	require.NotNil(t, web)
	assert.Equal(t, "yarn dev", web.Command)
	assert.Equal(t, work, web.CWD, "no cwd → the scope's directory")
	assert.Equal(t, "g", web.Group)
	assert.Equal(t, map[string]string{"A": "1"}, web.Env)

	// Without Overwrite a duplicate is refused and the file is left alone.
	_, err = AddService(s, NewService{Name: "web", Command: "other"})
	assert.EqualError(t, err, `service "web" already registered`)
	reg, _ = config.LoadRegistry(config.RegistryPath())
	assert.Equal(t, "yarn dev", reg.Services["web"].Command)

	// With it (the CLI's behaviour) the service is replaced.
	_, err = AddService(s, NewService{Name: "web", Command: "other", Overwrite: true})
	require.NoError(t, err)
	reg, _ = config.LoadRegistry(config.RegistryPath())
	assert.Equal(t, "other", reg.Services["web"].Command)
}

// A relative cwd is taken against the scope's directory, not the process's —
// so it means the same thing from the CLI and from an agent's project_dir.
func TestAddService_RelativeCWDIsUnderTheScope(t *testing.T) {
	work := sandbox(t)
	_, err := AddService(Scope{Dir: work}, NewService{Name: "api", Command: "go run .", CWD: "backend"})
	require.NoError(t, err)
	reg, _ := config.LoadRegistry(config.RegistryPath())
	assert.Equal(t, filepath.Join(work, "backend"), reg.Services["api"].CWD)

	dir := project(t, "services:\n  web:\n    command: a\n")
	_, err = AddService(Scope{Dir: dir}, NewService{Name: "api", Command: "go run .", CWD: "backend"})
	require.NoError(t, err)
	proj, _ := config.LoadProject(dir)
	assert.Equal(t, "backend", proj.Services["api"].CWD, "stored relative to the project file")

	_, err = AddService(Scope{Dir: dir}, NewService{Name: "root", Command: "x", CWD: dir})
	require.NoError(t, err)
	proj, _ = config.LoadProject(dir)
	assert.Equal(t, "", proj.Services["root"].CWD, "the project root is stored as empty")
}

func TestAddService_Project(t *testing.T) {
	sandbox(t)
	dir := project(t, "services:\n  web:\n    command: a\n")
	s := Scope{Dir: dir}

	res, err := AddService(s, NewService{Name: "api", Command: "go run .", Group: "ignored"})
	require.NoError(t, err)
	assert.True(t, res.Local)
	assert.True(t, res.GroupIgnored)

	_, err = AddService(s, NewService{Name: "api", Command: "again", Overwrite: true})
	assert.EqualError(t, err, `service "api" already defined in devrun.yaml`, "a project file never overwrites")

	_, err = os.Stat(config.RegistryPath())
	assert.True(t, os.IsNotExist(err), "nothing was written to the global registry")
}

func TestAddService_RejectsEmptyCommand(t *testing.T) {
	work := sandbox(t)
	_, err := AddService(Scope{Dir: work}, NewService{Name: "x", Command: "   "})
	assert.EqualError(t, err, "command cannot be empty")
}

func TestResolve_SourceAndScope(t *testing.T) {
	work := sandbox(t)
	r, err := Resolve(Scope{Dir: work})
	require.NoError(t, err)
	assert.Equal(t, "global", r.ScopeName())
	assert.Equal(t, config.RegistryPath(), r.SourcePath())
	assert.Nil(t, r.InlineConfig("anything"), "global services are resolved by the daemon")

	dir := project(t, "services:\n  web:\n    command: a\n")
	r, err = Resolve(Scope{Dir: dir})
	require.NoError(t, err)
	assert.Equal(t, "project", r.ScopeName())
	assert.Equal(t, filepath.Join(dir, config.ProjectFileName), r.SourcePath())
	require.NotNil(t, r.InlineConfig("web"), "project services travel inline")
	assert.Equal(t, dir, r.InlineConfig("web").CWD)

	_, err = r.Service("nope")
	assert.EqualError(t, err, `service "nope" not found`)
}

// A devrun.yaml that does not parse is still named, so callers can point at it.
func TestResolve_BrokenProjectFileIsNamed(t *testing.T) {
	sandbox(t)
	dir := project(t, "services: [not, a, map\n")
	r, err := Resolve(Scope{Dir: dir})
	require.Error(t, err)
	assert.Equal(t, filepath.Join(dir, config.ProjectFileName), r.SourcePath())
}
