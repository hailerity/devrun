package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hailerity/devrun/internal/config"
)

func TestAddService_ProjectFile(t *testing.T) {
	e := newEnv(t)
	dir := e.project("services:\n  web:\n    command: npm start\n")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"project_dir": dir, "name": "api", "command": "go run ./cmd/api", "cwd": "backend",
		"env": map[string]any{"PORT": "8080"},
	}, &out))
	assert.Equal(t, "project", out.Scope)
	assert.Equal(t, filepath.Join(dir, config.ProjectFileName), out.Source)
	// Group is "proj" though none was asked for: the project's name is what a
	// service in a devrun.yaml inherits, and the result is read back from disk
	// rather than echoed, so it reports the group the service actually has.
	assert.Equal(t, ServiceDef{
		Name: "api", Command: "go run ./cmd/api", CWD: filepath.Join(dir, "backend"),
		Group: "proj", EnvKeys: []string{"PORT"},
	}, out.Service, "the result is read back from disk, with cwd resolved")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	require.Contains(t, proj.Services, "api")
	assert.Equal(t, "backend", proj.Services["api"].CWD, "stored relative to the project file")
	assert.Empty(t, proj.Services["api"].Group, "and the file records no group of its own")
	_, err = os.Stat(config.RegistryPath())
	assert.True(t, os.IsNotExist(err), "the global registry is untouched")
}

// The parameter an agent had no way to set: a group, written to whichever
// config is in scope and reported back.
func TestAddService_WritesTheGroup(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n  web:\n    command: npm start\n")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"project_dir": dir, "name": "api", "command": "go run .", "group": "backend",
	}, &out))
	assert.Equal(t, "backend", out.Service.Group, "reported back")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Equal(t, "backend", proj.Services["api"].Group, "and written to the file")
	assert.Empty(t, proj.Services["web"].Group, "its sibling is untouched")
}

// Omitting it leaves the service inheriting, rather than pinning it to the
// project's current name — so renaming the project still carries it along.
func TestAddService_OmittedGroupInherits(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n  web:\n    command: npm start\n")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"project_dir": dir, "name": "api", "command": "go run .",
	}, &out))
	assert.Equal(t, "shop", out.Service.Group, "the inherited group is reported")

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Empty(t, proj.Services["api"].Group, "but nothing is pinned in the file")
}

// In the global registry there is no project to inherit from, so an omitted
// group means ungrouped and a given one is stored as-is.
func TestAddService_GroupInTheGlobalRegistry(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{}, nil)

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"name": "api", "command": "go run .", "group": "backend",
	}, &out))
	assert.Equal(t, "global", out.Scope)
	assert.Equal(t, "backend", out.Service.Group)

	var bare AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"name": "lonely", "command": "go run .",
	}, &bare))
	assert.Empty(t, bare.Service.Group, "nothing to inherit")
}

// An agent must never silently replace a service someone defined.
func TestAddService_RefusesAnExistingName(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "original command"}, nil)

	msg := e.call("add_service", map[string]any{"name": "web", "command": "something else"}, nil)
	assert.Contains(t, msg, `service "web" already registered`)
	reg, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	assert.Equal(t, "original command", reg.Services["web"].Command, "the file is left as it was")

	dir := e.project("services:\n  app:\n    command: x\n")
	assert.Contains(t, e.call("add_service", map[string]any{"project_dir": dir, "name": "app", "command": "y"}, nil), "already defined in devrun.yaml")
}

func TestAddService_Validates(t *testing.T) {
	e := newEnv(t)
	assert.Contains(t, e.call("add_service", map[string]any{"name": "../x", "command": "y"}, nil), "invalid service name")
	assert.Contains(t, e.call("add_service", map[string]any{"name": "ok", "command": "   "}, nil), "command cannot be empty")
	assert.Contains(t, e.call("add_service", map[string]any{"name": "ok", "command": "y", "env": map[string]any{"BAD-KEY": "1"}}, nil), "invalid environment variable name")
	// Missing required fields are rejected by the schema before the handler runs.
	assert.NotEmpty(t, e.call("add_service", map[string]any{"name": "ok"}, nil))
	_, err := os.Stat(config.RegistryPath())
	assert.True(t, os.IsNotExist(err), "nothing was written by any failed call")
}

func TestAddToTarget_CreatesAndMerges(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "x", "api": "y"}, nil)

	var out AddToTargetOutput
	require.Empty(t, e.call("add_to_target", map[string]any{"target": "stack", "services": []string{"web"}}, &out))
	assert.Equal(t, []string{"web"}, out.Members)
	require.Empty(t, e.call("add_to_target", map[string]any{"target": "stack", "services": []string{"api", "web"}}, &out))
	assert.Equal(t, []string{"web", "api"}, out.Members, "existing members kept, duplicates merged")

	msg := e.call("add_to_target", map[string]any{"target": "stack", "services": []string{"ghost"}}, nil)
	assert.Contains(t, msg, `service "ghost" is not defined in this config`)
	assert.Contains(t, msg, "defined services: [api web]")
	assert.Contains(t, e.call("add_to_target", map[string]any{"target": "stack", "services": []string{}}, nil), "at least one service")
}

func TestStartStop_ValidateBeforeDoingAnything(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "sleep 30"}, map[string][]string{"stack": {"web"}})

	for _, tool := range []string{"start", "stop"} {
		assert.Contains(t, e.call(tool, map[string]any{}, nil), "exactly one of service or target", tool)
		assert.Contains(t, e.call(tool, map[string]any{"service": "web", "target": "stack"}, nil), "exactly one of service or target", tool)
		assert.Contains(t, e.call(tool, map[string]any{"service": "nope"}, nil), `"nope" not found`, tool)
		assert.Contains(t, e.call(tool, map[string]any{"target": "nope"}, nil), `target "nope" not found`, tool)
	}
	assert.Contains(t, e.call("start", map[string]any{"service": "web", "timeout_s": 500}, nil), "timeout_s must be between 1 and 120")
}

// Stopping what is not running is the state the caller wanted: success.
func TestStop_NothingRunningIsANoOp(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{"web": "sleep 30"}, map[string][]string{"stack": {"web"}})

	var out StopOutput
	require.Empty(t, e.call("stop", map[string]any{"service": "web"}, &out))
	assert.Equal(t, []StopState{{Name: "web", State: "stopped"}}, out.Services)

	require.Empty(t, e.call("stop", map[string]any{"target": "stack"}, &out))
	assert.Equal(t, []StopState{{Name: "web", State: "stopped"}}, out.Services)
	assert.Contains(t, out.Note, "was not started as a target")
}

// An agent passing the group it saw in list_services — which for a project
// service is the project's name applied as a default — must not thereby pin the
// service to that name. Nothing in the read output distinguishes the default
// from a group a service set for itself, and the instructions tell the agent to
// match an existing group, so this is the path it is actively steered onto.
func TestAddService_GroupMatchingTheProjectNameStaysInherited(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n  web:\n    command: npm start\n")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"project_dir": dir, "name": "api", "command": "go run .", "group": "shop",
	}, &out))

	// What the agent asked for, honoured: the service is in the shop section.
	assert.Equal(t, "shop", out.Service.Group)

	// But not pinned to it.
	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Empty(t, proj.Services["api"].Group,
		"the project's own name is the default, so it is stored as inherited")

	// Which is what it buys: renaming the project carries both along, instead of
	// leaving api behind under a group of one.
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.ProjectFileName),
		[]byte("name: market\nservices:\n  web:\n    command: npm start\n  api:\n    command: go run .\n"), 0644))
	proj, err = config.LoadProject(dir)
	require.NoError(t, err)
	cfgs := proj.ToServiceConfigs(dir)
	assert.Equal(t, "market", cfgs["web"].Group)
	assert.Equal(t, "market", cfgs["api"].Group)
}

// A group that is not the project's default is stored as given, including one
// that merely resembles it.
func TestAddService_ADifferentGroupIsStoredLiterally(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n  web:\n    command: npm start\n")

	for _, group := range []string{"backend", "Shop", "shop-api"} {
		var out AddServiceOutput
		require.Empty(t, e.call("add_service", map[string]any{
			"project_dir": dir, "name": "svc-" + group, "command": "x", "group": group,
		}, &out))
		proj, err := config.LoadProject(dir)
		require.NoError(t, err)
		assert.Equal(t, group, proj.Services["svc-"+group].Group,
			"%q is not the project's name, so it is the service's own", group)
	}
}

// In global scope there is no project name to collide with, so a group is always
// stored as given.
func TestAddService_GlobalScopeStoresAnyGroupLiterally(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{}, nil)

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"name": "api", "command": "x", "group": filepath.Base(e.root),
	}, &out))
	assert.Equal(t, filepath.Base(e.root), out.Service.Group,
		"nothing is inherited globally, so nothing is normalised away")
}

// The group is the only free-form string this surface writes into a committed
// file, and hardening_test.go's contract is that malformed input is refused
// before anything is written. A newline is the one that matters: the sidebar
// counts one row as one terminal line, and lipgloss.Width measures the widest
// line of a multi-line string — so it would never be truncated and would draw
// two lines for one row.
func TestAddService_RefusesAnUndrawableGroup(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n  web:\n    command: npm start\n")

	for _, tc := range []struct{ name, group, want string }{
		{"newline", "back\nend", "single line"},
		{"carriage return", "back\rend", "single line"},
		{"control character", "back\x07end", "control character"},
		{"too long", strings.Repeat("g", config.MaxGroupLen+1), "over the"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out AddServiceOutput
			errText := e.call("add_service", map[string]any{
				"project_dir": dir, "name": "api", "command": "x", "group": tc.group,
			}, &out)
			require.NotEmpty(t, errText, "must be refused")
			assert.Contains(t, errText, tc.want, "the message names the problem")

			// And nothing was written: the service does not exist.
			proj, err := config.LoadProject(dir)
			require.NoError(t, err)
			assert.NotContains(t, proj.Services, "api", "refused before the write")
		})
	}
}

// A group at the limit, and one with punctuation and non-ASCII, are fine — the
// bar is one printable line, not a name rule.
func TestAddService_AcceptsAFreeFormGroup(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{}, nil)

	for i, group := range []string{
		strings.Repeat("g", config.MaxGroupLen),
		"back end / api",
		"сервисы",
	} {
		var out AddServiceOutput
		require.Empty(t, e.call("add_service", map[string]any{
			"name": fmt.Sprintf("svc%d", i), "command": "x", "group": group,
		}, &out), "group %q should be accepted", group)
		assert.Equal(t, group, out.Service.Group)
	}
}

// The normalisation keys on whether the config is a project file, not on the
// `global` flag: a directory with no devrun.yaml resolves to the global registry
// with the flag unset, and there a group matching the directory's name is an
// ordinary group — nothing inherits, so there is nothing to normalise away.
func TestAddService_NoProjectFileMeansNoInheritance(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{}, nil)
	require.NoFileExists(t, filepath.Join(e.root, config.ProjectFileName),
		"the default dir has no project file")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		// No project_dir and no global flag: the default dir, which has no
		// devrun.yaml, so this lands in the global registry.
		"name": "api", "command": "x", "group": filepath.Base(e.root),
	}, &out))
	assert.Equal(t, "global", out.Scope)
	assert.Equal(t, filepath.Base(e.root), out.Service.Group,
		"kept: there is no project to inherit from")
}

// The limit is characters, not bytes, and the TUI editor's input cap counts
// runes — so a byte comparison here made 128 Cyrillic characters, which are 256
// bytes, pass in the editor and be refused through this tool. The same field
// cannot have two different limits depending on who writes it.
func TestAddService_TheGroupLimitCountsCharactersNotBytes(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{}, nil)

	atLimit := strings.Repeat("ф", config.MaxGroupLen) // 2 bytes each
	require.Greater(t, len(atLimit), config.MaxGroupLen,
		"the fixture is only interesting if it is over the limit in bytes")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"name": "api", "command": "x", "group": atLimit,
	}, &out), "%d characters is at the limit, whatever they weigh", config.MaxGroupLen)
	assert.Equal(t, atLimit, out.Service.Group)

	// One character past it is still refused, and the count in the message is in
	// the same unit as the limit it is compared against.
	errText := e.call("add_service", map[string]any{
		"name": "api2", "command": "x", "group": atLimit + "ф",
	}, &out)
	require.NotEmpty(t, errText)
	assert.Contains(t, errText, fmt.Sprintf("%d characters", config.MaxGroupLen+1))
}

// Validated after trimming, because trimming is what decides what gets stored.
// Checking the raw string instead refused a group that would have been written
// exactly at the limit, for being over it by its surrounding spaces.
func TestAddService_TheGroupIsTrimmedBeforeItIsChecked(t *testing.T) {
	e := newEnv(t)
	e.registry(map[string]string{}, nil)

	atLimit := strings.Repeat("g", config.MaxGroupLen)
	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"name": "api", "command": "x", "group": "  " + atLimit + "  ",
	}, &out), "what would be stored is at the limit, so it is accepted")
	assert.Equal(t, atLimit, out.Service.Group, "stored and echoed trimmed")

	reg, err := config.LoadRegistry(config.RegistryPath())
	require.NoError(t, err)
	assert.Equal(t, atLimit, reg.Services["api"].Group)
}

// The other half of trimming first: the inherit check compares the group
// against the project's name, so it has to compare the stored form. Padded, it
// matched nothing and was stored as an explicit group — the very pinning the
// normalisation exists to avoid, and invisible in the result, since a pinned
// group and an inherited one read back identically.
func TestAddService_APaddedGroupMatchingTheProjectNameStillInherits(t *testing.T) {
	e := newEnv(t)
	dir := e.project("name: shop\nservices:\n  web:\n    command: npm start\n")

	var out AddServiceOutput
	require.Empty(t, e.call("add_service", map[string]any{
		"project_dir": dir, "name": "api", "command": "x", "group": "  shop  ",
	}, &out))
	assert.Equal(t, "shop", out.Service.Group)

	proj, err := config.LoadProject(dir)
	require.NoError(t, err)
	assert.Empty(t, proj.Services["api"].Group,
		"no group line, so a rename of the project carries it along with its siblings")
}
