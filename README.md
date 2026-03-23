# devrun

> A lightweight process manager for developers who juggle many services.

Stop opening new terminal tabs. Stop forgetting start commands. Stop wondering what's running.
`devrun` gives you a single place to register, start, and monitor all your development services —
with a live TUI dashboard and persistent logs.

---

## Features

- **Register once, run anywhere** — save commands with names, no more muscle memory required
- **Group services** — start/stop your entire stack with one command (`@group`)
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

Requires Go 1.22+. The binary is placed in `$GOPATH/bin` (usually `~/go/bin`).

### Verify

```sh
devrun --version
```

---

## Quick Start

```sh
# 1. Register your services
devrun add web   "yarn dev"           --cwd ~/projects/app --group fullstack
devrun add api   "yarn nx serve api"  --cwd ~/projects/app --group fullstack
devrun add db    "docker run --rm -p 5432:5432 postgres:16" --group fullstack

# 2. Start everything
devrun start @fullstack

# 3. Check what's running
devrun list

# 4. Open the live dashboard
devrun ui

# 5. When you're done
devrun stop @fullstack
```

---

## Commands

### Service Registration

| Command | Description |
|---|---|
| `devrun add <name> <cmd>` | Register a new service |
| `devrun remove <name>` | Remove a service |
| `devrun edit <name>` | Open service config in `$EDITOR` |
| `devrun list` | List all services with status |
| `devrun status` | Compact status table (name, state, pid, port, uptime) |

**`devrun add` options:**

```
--cwd <path>          Working directory (default: current dir)
--env KEY=VALUE       Set environment variable (repeatable)
--env-file <path>     Load env vars from file
--group <name>        Assign to a group
--restart <policy>    Restart policy: never | on-failure | always (default: never)
--desc <text>         Human-readable description
```

### Lifecycle

| Command | Description |
|---|---|
| `devrun start <name\|@group\|--all>` | Start one, a group, or all services |
| `devrun stop <name\|@group\|--all>` | Stop one, a group, or all services |
| `devrun restart <name\|@group>` | Restart one or a group |

### Observability

| Command | Description |
|---|---|
| `devrun logs <name>` | Print last 100 log lines |
| `devrun logs <name> -f` | Follow log output (like `tail -f`) |
| `devrun logs <name> -n 50` | Print last N lines |

### Interaction

| Command | Description |
|---|---|
| `devrun ui` | Open interactive TUI dashboard |
| `devrun attach <name>` | Attach stdin/stdout to a running service |

### Project-local Workflow

| Command | Description |
|---|---|
| `devrun init` | Scaffold `.devrun.yaml` in current directory |
| `devrun up` | Start all services defined in `.devrun.yaml` |
| `devrun down` | Stop all services defined in `.devrun.yaml` |

### Daemon

| Command | Description |
|---|---|
| `devrun daemon start` | Start the background daemon |
| `devrun daemon stop` | Stop the daemon (and all services) |
| `devrun daemon status` | Check daemon health |
| `devrun daemon restart` | Restart the daemon |

---

## Configuration

### Project-local: `.devrun.yaml`

Place this file in your project root and commit it.

```yaml
version: "1"

services:
  web:
    command: yarn dev
    cwd: .
    group: fullstack
    env:
      PORT: "3000"
      NODE_ENV: development

  api:
    command: yarn nx serve api
    cwd: .
    group: fullstack
    depends_on:
      - db
    env:
      PORT: "4000"
    restart: on-failure

  db:
    command: docker run --rm -p 5432:5432 -e POSTGRES_PASSWORD=dev postgres:16
    group: fullstack
    restart: always
```

### Global registry: `~/.config/devrun/services.yaml`

Managed automatically by `devrun add/remove`. Same schema as `.devrun.yaml`.

---

## TUI Dashboard (`devrun ui`)

```
┌─ devrun ──────────────────────────────────────────────────────────┐
│  NAME     GROUP       STATE    PID    PORT   UPTIME    CPU   MEM  │
│  ──────────────────────────────────────────────────────────────── │
│▶ web       fullstack   running  12041  :3000  2h 14m    2%   180M │
│  api       fullstack   running  12055  :4000  2h 14m    0%   240M │
│  db        fullstack   running  11980  :5432  2h 15m    0%    64M │
│  worker    –           stopped  –      –      –         –    –    │
│                                                                    │
│  [s]tart  [x]stop  [r]estart  [l]ogs  [a]ttach  [?]help  [q]quit │
└────────────────────────────────────────────────────────────────────┘
│ Logs: web                                        [j/k scroll] [c]lear│
│  → GET  /api/users    200  12ms                                    │
│  → POST /api/auth     201  45ms                                    │
│  → GET  /api/profile  200   8ms                                    │
└────────────────────────────────────────────────────────────────────┘
```

Keyboard shortcuts:

| Key | Action |
|---|---|
| `j/k` or `↑/↓` | Navigate service list |
| `s` | Start selected service |
| `x` | Stop selected service |
| `r` | Restart selected service |
| `l` | Toggle log panel for selected service |
| `a` | Attach to selected service |
| `f` | Filter by group |
| `?` | Show help |
| `q` / `Ctrl+C` | Quit |

---

## File Locations

| Path | Purpose |
|---|---|
| `~/.config/devrun/services.yaml` | Global service registry |
| `~/.local/share/devrun/pids/` | PID files per service |
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
