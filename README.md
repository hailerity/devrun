# devrun

> A lightweight process manager for developers who juggle many services.

Stop opening new terminal tabs. Stop forgetting start commands. Stop wondering what's running.
`devrun` gives you a single place to register, start, and monitor all your development services —
with a live TUI dashboard and persistent logs.

---

## Features

- **Register once, run anywhere** — save commands with names, no more muscle memory required
- **Project-local configs** — commit a `.devrun.yaml` so your team shares the same setup
- **Live TUI dashboard** — see all services, CPU/mem, uptime, and tail logs in one view
- **Persistent logs** — every service writes to its own log file; inspect anytime
- **Daemon-backed** — services stay alive after you close the terminal
- **Port detection** — devrun reports which port each service bound to
- **Attach to any service** — bring a running process to your foreground (interactive)

---

## Installation

### curl installer (macOS / Linux) — recommended

```sh
curl -fsSL https://raw.githubusercontent.com/hailerity/devrun/main/scripts/install.sh | sh
```

Installs to `/usr/local/bin` (or `~/.local/bin` if you don't have sudo).
Pin a specific version with `DEVRUN_VERSION=v1.2.3 curl ... | sh`.

### go install

```sh
go install github.com/hailerity/devrun/cmd/devrun@latest
```

Requires Go 1.25+. The binary is placed in `$GOPATH/bin` (usually `~/go/bin`).

### Verify

```sh
devrun --version
```

---

## Quick Start

### Global services

```sh
# 1. Register your services
devrun add web "yarn dev"         --cwd ~/projects/app --group fullstack
devrun add api "go run ./cmd/api" --cwd ~/projects/app --group fullstack

# 2. Start everything
devrun start --all

# 3. Check what's running
devrun list

# 4. Open the live dashboard
devrun

# 5. When you're done
devrun stop --all
```

### Project-local workflow

```sh
# In any directory with a .devrun.yaml
devrun up       # register + start all services
devrun list     # check status
devrun          # open dashboard
devrun down     # stop all project services
```

---

## Commands

### Service Registration

| Command | Description |
|---|---|
| `devrun add <name> <cmd>` | Register a new service |
| `devrun remove <name>` | Remove a service |
| `devrun list` | List all services with status |

**`devrun add` options:**

```
--cwd <path>      Working directory (default: current dir)
--env KEY=VALUE   Set environment variable (repeatable)
--group <name>    Assign to a group
```

### Lifecycle

| Command | Description |
|---|---|
| `devrun start <name>` | Start a service |
| `devrun start --all` | Start all registered services |
| `devrun start <name> --fg` | Start and attach terminal |
| `devrun stop <name>` | Stop a service |
| `devrun stop --all` | Stop all running services |

### Observability

| Command | Description |
|---|---|
| `devrun logs <name>` | Print last 100 log lines |
| `devrun logs <name> -f` | Follow log output (like `tail -f`) |
| `devrun logs <name> -n 50` | Print last N lines |

### Interaction

| Command | Description |
|---|---|
| `devrun` | Open interactive TUI dashboard |
| `devrun fg <name>` | Attach stdin/stdout to a running service |

### Project-local Workflow

| Command | Description |
|---|---|
| `devrun up` | Register + start all services from `.devrun.yaml` |
| `devrun down` | Stop all services from `.devrun.yaml` |

---

## Configuration

### Project-local: `.devrun.yaml`

Place this file in your project root and commit it. Running `devrun up` registers all services into the global daemon with the project name as their group.

```yaml
name: myapp      # optional — defaults to directory name

services:
  web:
    command: yarn dev
    cwd: ./frontend  # relative to .devrun.yaml; defaults to project root
    env:
      PORT: "3000"
      NODE_ENV: development

  api:
    command: go run ./cmd/api
    env:
      PORT: "4000"
```

### Global registry: `~/.config/devrun/services.yaml`

Managed automatically by `devrun add/remove`. You can also edit it directly.

---

## TUI Dashboard (`devrun`)

```
⬡ devrun  3 running / 4 total
──────────────────────────────────────────────────────────────
SERVICES │ LOGS          DETAILS
─────────│─────────────────────────────────────────────────────
● web    │ → GET  /api/users    200  12ms
● api    │ → POST /api/auth     201  45ms
○ db     │ → GET  /api/profile  200   8ms
○ worker │
─────────────────────────────────────────────────────────────
s start  x stop  q quit
```

**Navigation:**

| Key | Action |
|---|---|
| `k` / `↑` | Move up |
| `j` / `↓` | Move down |
| `←` / `→` | Focus sidebar / main panel |
| `Tab` | Cycle: sidebar → Logs → Details → sidebar |

**Service control (sidebar focused):**

| Key | Action |
|---|---|
| `s` | Start selected service |
| `x` | Stop selected service |

**Log panel (main panel focused, Logs tab):**

| Key | Action |
|---|---|
| `f` | Toggle follow mode |
| `g` / `G` | Jump to top / bottom |
| `v` | Enter visual selection mode |
| `y` / `Ctrl+C` | Copy selection (or current line) |
| `Esc` | Exit visual mode |

**Global:**

| Key | Action |
|---|---|
| `q` / `Ctrl+C` | Quit |

---

## File Locations

| Path | Purpose |
|---|---|
| `~/.config/devrun/services.yaml` | Global service registry |
| `~/.local/share/devrun/state.json` | Runtime state (PID, status, port) |
| `~/.local/share/devrun/logs/` | Log files per service (`<name>.log`) |
| `~/.local/share/devrun/devrun.sock` | Daemon Unix socket |
| `.devrun.yaml` | Project-local service definitions |

---

## Why not PM2 / Overmind / tmux?

| Tool | Problem |
|---|---|
| PM2 | Node.js-only, heavyweight, complex API |
| Overmind / Foreman | Procfile-only, no global registry, no TUI |
| tmux / zellij | Manual setup, no process awareness |
| Docker Compose | Containers only, heavy for local dev |

`devrun` is polyglot, minimal, and built for the developer's local machine — not production.

---

## License

MIT
