# TODOS

Development roadmap for `devrun`. Work top-to-bottom within each phase.

---

## Phase 1 — Foundation

Goal: A working CLI that can register services and communicate with a daemon.

### Module & Project Setup
- [ ] Initialize Go module (`go mod init github.com/hailerity/devrun`)
- [ ] Set up directory structure (`cmd/devrun`, `internal/...`)
- [ ] Add dependencies: cobra, viper, bubbletea, lipgloss, pty, lumberjack
- [ ] Add `Makefile` with `build`, `test`, `lint`, `install` targets
- [ ] Set up `golangci-lint` config (`.golangci.yml`)
- [ ] Set up basic GitHub Actions CI (build + test on push)

### Config & Registry
- [ ] Define `ServiceConfig` struct with all fields (`internal/config/schema.go`)
- [ ] Implement `LoadRegistry` / `SaveRegistry` (global `services.yaml`)
- [ ] Implement `LoadLocal` (walk cwd upward for `.devrun.yaml`)
- [ ] Implement `Merge` (local shadows global for same-named services)
- [ ] Write unit tests for load/save/merge

### CLI Skeleton
- [ ] Root command with `--config`, `--socket`, `--json`, `--no-color` flags
- [ ] `devrun add` — validate args, write to registry, print confirmation
- [ ] `devrun remove` — remove from registry
- [ ] `devrun edit` — open in `$EDITOR`, validate on save
- [ ] `devrun list` — read registry + daemon state, render table
- [ ] `devrun status` — compact alias for list
- [ ] `devrun init` — scaffold `.devrun.yaml`

### IPC Layer
- [ ] Define all request/response structs (`internal/ipc/protocol.go`)
- [ ] Implement client: connect, send, receive, close (`internal/client/client.go`)
- [ ] Implement framing (4-byte length prefix + JSON)
- [ ] Implement streaming client (for logs, state subscription)
- [ ] Write unit tests for framing and marshal/unmarshal

### Daemon — Core
- [ ] Daemon entry point: double-fork, write PID file, bind socket
- [ ] Request router: dispatch by `type` field to handler functions
- [ ] `handleDaemonStatus` — return daemon health info
- [ ] `handleDaemonStop` — graceful shutdown of daemon + children
- [ ] Auto-start daemon from CLI if socket not available

---

## Phase 2 — Process Management

Goal: Services can be started, stopped, restarted, and their logs viewed.

### Process Runner
- [ ] Implement `process.Start` — fork/exec with PTY, capture stdout/stderr
- [ ] Implement PID file write/read (`internal/process/pid.go`)
- [ ] Implement rolling log file writer (lumberjack, 10MB / 3 rotations)
- [ ] Implement `process.Stop` — SIGTERM → wait → SIGKILL
- [ ] Implement port detection (macOS: parse `lsof` output; Linux: `/proc/net/tcp`)
- [ ] Write unit tests for PID file and log writer

### Daemon — Service Lifecycle
- [ ] `handleStart` — look up config, resolve depends_on order, spawn process
- [ ] `handleStop` — send signal, wait for exit, update state
- [ ] `handleRestart` — stop + start
- [ ] Supervisor goroutine — watch for child exits, apply restart policy
- [ ] State persistence — write `state.json` atomically on every change
- [ ] State restore on daemon startup — re-adopt live PIDs

### CLI — Lifecycle Commands
- [ ] `devrun start <name|@group|--all>` — with dependency ordering
- [ ] `devrun stop <name|@group|--all>`
- [ ] `devrun restart <name|@group>`

### Daemon — Logs
- [ ] `handleSubscribeLogs` — tail log file + follow new lines, push as events
- [ ] `handleSubscribeLogs` — support `tail N` (last N lines) before following

### CLI — Logs
- [ ] `devrun logs <name>` — print last N lines
- [ ] `devrun logs <name> -f` — stream via subscribe connection
- [ ] `devrun logs <name> --since` — filter by timestamp

### Project-local Workflow
- [ ] `devrun up` — load `.devrun.yaml`, start all services
- [ ] `devrun down` — load `.devrun.yaml`, stop all services

---

## Phase 3 — TUI

Goal: `devrun ui` opens a full interactive dashboard.

### TUI Foundation
- [ ] Root `Model` struct with service list, log panel, status bar sub-models
- [ ] `Init` — fire initial tick + window size query
- [ ] `WindowSizeMsg` handling — compute panel heights
- [ ] `tickCmd` — poll daemon every 1s via `SubscribeState`
- [ ] `Update` dispatch for all message types

### Service List Component
- [ ] Render service table (name, group, state, pid, port, uptime, cpu, mem)
- [ ] Cursor navigation (j/k, ↑/↓)
- [ ] State color coding (green=running, red=crashed, yellow=starting, gray=stopped)
- [ ] Group filter (f key → cycle through groups)

### Log Panel Component
- [ ] Toggle on/off (l key)
- [ ] Subscribe to logs for selected service
- [ ] Scrollable ring buffer (last 500 lines)
- [ ] j/k scroll within log panel
- [ ] c to clear buffer

### Keybindings
- [ ] `s` → start selected
- [ ] `x` → stop selected
- [ ] `r` → restart selected
- [ ] `l` → toggle log panel
- [ ] `a` → attach to selected (exit TUI, attach, re-enter TUI on detach)
- [ ] `f` → filter by group
- [ ] `?` → toggle help overlay
- [ ] `q` / `Ctrl+C` → quit

### Status Bar
- [ ] Show: `devrun v0.x.x · daemon: ok · N/M running`
- [ ] Show active keybindings contextually
- [ ] Show error banner when daemon unreachable

### Polish
- [ ] Responsive layout (narrow terminal graceful degradation)
- [ ] Lip Gloss theme (colors matching devrun brand)
- [ ] Spinner on "starting" / "restarting" services

---

## Phase 4 — Advanced Features

Goal: Health checks, attach, shell completion, quality of life.

### Health Checks
- [ ] Parse `health_check` config block
- [ ] Daemon polls health URL on interval; transitions state to `healthy`
- [ ] Surface health status in TUI and `devrun list`

### Attach
- [ ] Daemon `handleAttach` — proxy PTY stdin/stdout bidirectionally
- [ ] CLI `devrun attach` — raw terminal mode, Ctrl+P,Q to detach
- [ ] TUI `a` keybinding — suspend TUI, run attach, resume TUI

### Shell Completion
- [ ] `devrun completion bash`
- [ ] `devrun completion zsh`
- [ ] `devrun completion fish`
- [ ] Dynamic completion for service names (query registry)

### Quality of Life
- [ ] `devrun doctor` — check daemon health, port conflicts, stale PIDs
- [ ] `devrun import --procfile <path>` — import from Procfile format
- [ ] `devrun import --compose <path>` — import from docker-compose.yml (commands only)
- [ ] `--json` flag for all commands (machine-readable output)
- [ ] Man page generation (`cobra` doc generation)

---

## Phase 5 — Distribution

- [ ] Goreleaser config (`.goreleaser.yaml`)
- [ ] Homebrew tap formula
- [ ] Install script (`install.sh`)
- [ ] GitHub Actions release workflow
- [ ] Smoke test suite for built binary
- [ ] CHANGELOG.md
