// Package mcpserver exposes devrun to AI agents as a Model Context Protocol
// server. Claude Code, Codex and other MCP clients launch `devrun mcp` and talk
// to it over stdio.
//
// Every tool is a bounded request/response: nothing follows, attaches or opens
// the TUI, so no call can hang the agent. Each resolves its config afresh from
// an optional project_dir (default: the directory the agent launched the server
// in) and reports which file it used, so an agent that addressed the wrong
// scope sees it in the result. The work itself is internal/ops — the same code
// the CLI runs.
package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hailerity/devrun/internal/ops"
)

// Options configures the server.
type Options struct {
	// Dir is the project directory a call uses when it gives no project_dir —
	// normally the server's working directory, which is where the agent was
	// launched.
	Dir string
}

// instructions is sent to the client at initialization; clients show it to the
// model as guidance for the whole server.
const instructions = `devrun manages long-running development processes (dev servers, workers, databases) for a project, so they run in the background instead of blocking your shell.

Prefer these tools over running a dev server yourself: a service started here keeps running after the call returns, its output is captured, and the user sees it live in the devrun dashboard.

Config: a devrun.yaml in the project directory defines the project's services and targets; without one, the user's global registry is used. Every tool accepts project_dir (default: the directory you were launched in) and every result says which file it used.

Typical flow: list_services → add_service if what you need is missing → start (it waits and tells you whether the service came up, with its log tail if not) → logs to check on it → stop when done.`

// New builds the devrun MCP server.
func New(version string, opts Options) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "devrun", Title: "devrun", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	h := &handlers{defaultDir: opts.Dir}
	registerReadTools(s, h)
	registerWriteTools(s, h)
	return s
}

type handlers struct {
	defaultDir string
}

// Scoped is the part of every tool's input that picks the config.
type Scoped struct {
	ProjectDir string `json:"project_dir,omitempty" jsonschema:"Absolute path of the project to act on. Its devrun.yaml is used when it has one, otherwise the global registry. Defaults to the directory the server was launched in."`
	Global     bool   `json:"global,omitempty" jsonschema:"Use the global registry even when project_dir has a devrun.yaml."`
}

// Origin is the part of every tool's result that says which config was used.
type Origin struct {
	Source string `json:"source" jsonschema:"The config file that was read or written."`
	Scope  string `json:"scope" jsonschema:"project (a devrun.yaml) or global (the user's registry)."`
}

func sourceOf(r *ops.Resolved) Origin {
	return Origin{Source: r.SourcePath(), Scope: r.ScopeName()}
}

// scope turns a call's Scoped input into an ops.Scope, validating project_dir.
func (h *handlers) scope(in Scoped) (ops.Scope, error) {
	dir := in.ProjectDir
	if dir == "" {
		dir = h.defaultDir
	}
	if !filepath.IsAbs(dir) {
		return ops.Scope{}, fmt.Errorf("project_dir must be an absolute path, got %q", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return ops.Scope{}, fmt.Errorf("project_dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return ops.Scope{}, fmt.Errorf("project_dir %q is not a directory", dir)
	}
	return ops.Scope{Dir: filepath.Clean(dir), Global: in.Global}, nil
}

// resolve is scope plus loading the config it points at.
func (h *handlers) resolve(in Scoped) (*ops.Resolved, error) {
	s, err := h.scope(in)
	if err != nil {
		return nil, err
	}
	r, err := ops.Resolve(s)
	if err != nil {
		if r != nil && r.IsLocal() {
			return nil, fmt.Errorf("%s: %w", r.SourcePath(), err)
		}
		return nil, err
	}
	return r, nil
}

// nameRe is what a service or target name may look like. A service's log file
// is named after it, so a name must never be able to leave the logs directory.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func checkName(kind, name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid %s name %q: use 1–64 letters, digits, '.', '_' or '-', starting with a letter or digit", kind, name)
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }
