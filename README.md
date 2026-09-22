# devrun

> A lightweight process manager for developers who juggle many services.

Stop opening new terminal tabs. Stop forgetting start commands. Stop wondering what's running.
`devrun` gives you a single place to register, start, and monitor all your development services —
with a live TUI dashboard and persistent logs.

![Screenshot](./assets/screenshot.png)

## Features

- **Register once, run anywhere** — save commands with names, no more muscle memory required
- **Targets** — group a subset of services under a name and start/stop them as a unit
- **Project-local configs** — commit a `devrun.yaml` and every command auto-uses it in that directory
- **Live TUI dashboard** — see all services, CPU/mem, uptime, and tail logs in one view
- **Persistent logs** — every service writes to its own log file; inspect anytime
- **Daemon-backed** — services stay alive after you close the terminal
- **Port detection** — devrun reports which port each service bound to
- **Attach to any service** — bring a running process to your foreground (interactive)
- **Works with AI agents** — Claude Code, Codex and other MCP clients can add, start and stop services and read their logs (`devrun mcp`)

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
# In any directory with a devrun.yaml, every command uses it automatically
devrun up       # register + start all services
devrun list     # check status (scoped to this project)
devrun          # open dashboard
devrun down     # stop all project services

devrun list --global   # ignore devrun.yaml, act on the global registry
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
--group <name>    Assign to a group (ignored when writing to a devrun.yaml)
```

### Lifecycle

| Command | Description |
|---|---|
| `devrun start <name>` | Start a service |
| `devrun start --all` | Start all registered services |
| `devrun start <name> --fg` | Start and attach terminal |
| `devrun stop <name>` | Stop a service |
| `devrun stop --all` | Stop all running services |

### Targets

A target is a named subset of services you can start and stop together — handy
when a config holds many services but you only need a few at a time.

| Command | Description |
|---|---|
| `devrun target create <name>` | Create a new, empty target |
| `devrun target add <name> <service>...` | Add services to a target (creates it if needed) |
| `devrun target rm <name> [service]...` | Remove services, or the whole target when none are given |
| `devrun target list` | List targets, their members, and which are running |
| `devrun target start <name>` | Start every service in the target |
| `devrun target stop <name>` | Stop the target's services, keeping any still held by another running target |

Targets are stored in whichever config is active — the project `devrun.yaml`
when one is present, otherwise the global `services.yaml`. `target stop` uses the
member list captured when the target was started, so editing membership while it
runs doesn't change what a later stop releases. It stops every member in that
snapshot — including any that were already running when the target started —
except services another active target still holds.

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
| `devrun mcp` | MCP server for AI agents — started by the agent, see [Using devrun from an AI agent](#using-devrun-from-an-ai-agent) |

### Project-local Workflow

| Command | Description |
|---|---|
| `devrun up` | Register + start all services from `devrun.yaml` |
| `devrun down` | Stop all services from `devrun.yaml` |

---

## Configuration

### Config resolution

Every command picks its config automatically:

- If the current directory contains a `devrun.yaml`, that file is the config — `list`, `start`, `stop`, `info`, `add`, `remove`, `target`, and the TUI dashboard all operate only on its services, and `add`/`remove`/`target` edit the file in place.
- Otherwise the global registry (`~/.config/devrun/services.yaml`) is used, which holds only services you registered with `devrun add`.
- `--global` / `-g` forces the global registry even when a `devrun.yaml` is present (not valid for `up`/`down`).

Project services are sent to the daemon with their full definition inline and are never written to `services.yaml`. `devrun daemon restart` re-execs in place and hands running services (with their definitions and live log capture) to the replacement, so nothing is lost. But if the daemon instead *crashes* or is stopped with `devrun daemon stop`, a running project service keeps running yet the next daemon no longer knows its command — and log capture is paused — until you re-run `devrun up` in that directory.

### Project-local: `devrun.yaml`

Place this file in your project root and commit it. Running `devrun up` starts every service in the daemon, grouped under the project name. The definitions are sent to the daemon inline for the duration of the run — they are not written to the global `services.yaml` (see [Config resolution](#config-resolution) above).

```yaml
name: myapp      # optional — defaults to directory name

services:
  web:
    command: yarn dev
    cwd: ./frontend  # relative to devrun.yaml; defaults to project root
    env:
      PORT: "3000"
      NODE_ENV: development

  api:
    command: go run ./cmd/api
    env:
      PORT: "4000"

  db:
    command: postgres -D ./pgdata

# Optional: named subsets you can start/stop as a unit.
targets:
  frontend: [web]
  backend:  [api, db]
```

### Global registry: `~/.config/devrun/services.yaml`

Managed automatically by `devrun add/remove`. You can also edit it directly.

---

## TUI Dashboard (`devrun`)

```
 ⬡ devrun  shop · devrun.yaml                          2/3 running  ✖ 1 crashed
╭─ SERVICES · frontend ───────╮╭─ web  ● running :5173  up 2h 14m ─ [LOGS] details ─╮
│ ✖ chat      crashed         ││ → GET  /api/users    200  12ms                     │
│ ● api       :8080      2.1% ││ → POST /api/auth     201  45ms                     │
│ ● web       :5173     64.0% ││ → GET  /api/profile  200   8ms                     │
╰─ 2/3 up ────────────────────╯╰─ 1,204 lines ─────────────────────────── ⇣ follow ─╯
 s start  x stop  r restart  ↵ details  t target  / search        ? help  q quit
```

Each service row shows a state glyph (`●` running, `◐` starting / stopping,
`○` stopped, `✖` crashed), the name, the port or state, and CPU. Crashed
services sort to the top. The focused pane has the accent-coloured border, and
the main pane's border always names the service whose logs or details it shows.
Colours adapt to light and dark terminals.

The sidebar is one list of services. When the active config defines targets,
`t` opens the **target picker**: every target with its running count and
members. `↵` filters the list to that target (its name then shows in the
SERVICES heading), `All services` clears the filter, and `e` edits the
highlighted target.

**Navigation:**

| Key | Action |
|---|---|
| `k` / `↑` | Move up |
| `j` / `↓` | Move down |
| `←` / `→` | Focus sidebar / main panel |
| `Tab` | Toggle focus between sidebar and main panel |
| `↵` | Toggle DETAILS / LOGS for the selected service |
| `Esc` | Back out of DETAILS to LOGS |

On a terminal narrower than 70 columns only the focused pane is shown, at full
width: `Tab` swaps panes, and `↵` on a service opens it.

**Service / target control:**

| Key | Action |
|---|---|
| `s` / `x` | Start / stop the selected service |
| `r` | Restart the selected service (starts it if it is not running) |
| `S` / `X` | Start / stop everything listed — the filtering target, or every service when there is no filter |
| `t` | Open the target picker (`↵` filter, `e` edit target, `Esc` close) |
| `e` | Edit the selected service (sidebar focused) |
| `d` | Remove the selected service (sidebar focused, asks to confirm) |

Pressing `e` opens a modal editor. It writes back to the active config — the
project `devrun.yaml` when one is in scope, otherwise `~/.config/devrun/services.yaml` —
using the same resolution as `devrun add`.

- **On a service** (`e` in the sidebar) the modal edits the service's name, command, and working
  directory. Saving refuses an empty name or command, or a name that collides
  with another service. If the edited service is running it is stopped and
  restarted (under the new name, on a rename) so the change takes effect
  immediately.
- **On a target** (`e` in the target picker) the modal edits the target's name and members: a name
  field plus a checklist of every service — `Tab` switches between the two,
  `space` toggles a service in or out. Saving refuses an empty name or one that
  collides with another target. A running target keeps its current membership
  until you stop and start it again.

Pressing `d` on a service asks to confirm, then deletes that service from
the active config — the same file `e` writes to, the same effect as
`devrun remove`. A running service must be stopped first.

**Log panel:**

| Key | Action |
|---|---|
| `/` | Search the log — matches highlight as you type, `↵` jumps to the nearest match above the cursor, `Esc` cancels |
| `n` / `N` | Next match down / previous match up (wraps) |
| `f` | Toggle follow mode |
| `g` / `G` | Jump to top / jump to the end and follow |
| `w` | Toggle line wrap |
| `v` | Enter visual selection mode |
| `y` / `Ctrl+C` | Copy selection (or current line) |
| `Esc` | Exit visual mode, then clear the search |

The log pane's bottom border shows the line count, the match position while a
search is active (`2/17 matches`), and the follow state. With follow off it
counts lines that arrived out of view (`↓ 37 new`); `G` jumps to them.

**Details panel (main panel focused, Details tab):**

| Key | Action |
|---|---|
| `j` / `k` | Move over the values; the list scrolls |
| `g` / `G` | First / last value |
| `y` | Copy the value under the cursor — the full value, even if the pane truncated it |

**Global:**

| Key | Action |
|---|---|
| `?` | Show every key, grouped |
| `q` / `Ctrl+C` | Quit |

The footer shows the keys for the focused pane. On a narrow terminal it drops
whole hints, least useful first; `? help` and `q quit` always stay.

---

## Using devrun from an AI agent

Coding agents start dev servers all the time, and do it badly: they block on a
foreground `npm run dev`, background it and lose the output, or leave
processes behind. `devrun mcp` is a [Model Context Protocol](https://modelcontextprotocol.io)
server that lets an agent hand those processes to devrun instead. The agent
drives the same daemon you do, so whatever it starts shows up live in your
`devrun` dashboard, and the reverse.

### Set up

**Claude Code**

```bash
claude mcp add devrun -- devrun mcp              # this project only
claude mcp add -s user devrun -- devrun mcp      # every project
claude mcp add -s project devrun -- devrun mcp   # shared with the team via .mcp.json
```

The project scope writes a `.mcp.json` you can commit:

```json
{
  "mcpServers": {
    "devrun": { "command": "devrun", "args": ["mcp"] }
  }
}
```

**Codex**

```bash
codex mcp add devrun -- devrun mcp
```

or in `~/.codex/config.toml`:

```toml
[mcp_servers.devrun]
command = "devrun"
args = ["mcp"]
```

Any other MCP client that can launch a stdio server works the same way:
the command is `devrun mcp`. Run by hand in a terminal, `devrun mcp` just
prints these instructions.

### Tools

| Tool | What it does |
|---|---|
| `list_services` | Every service and target in scope, with live state, PID, port, uptime and CPU/memory |
| `service_status` | One service's state and definition — command, directory, env variable **names** (never values), last exit code |
| `logs` | The end of a service's output as plain text: 100 lines by default, at most 1000 and 64 KB, optionally filtered |
| `add_service` | Define a new service in the project's `devrun.yaml` (or the global registry). Refuses an existing name |
| `add_to_target` | Create a target or add services to it |
| `start` | Start a service or target and **wait for the outcome**: running, or crashed / exited / failed with the end of its log |
| `stop` | Stop a service or target. Stopping something that is not running is fine |

`start` does not report success the moment a process is spawned. It waits
until the service has stayed up for a short settle period (2 s), so a crash
just after boot comes back as `crashed` with the log lines that explain it —
in the same call.

**Which config?** Every tool takes an optional absolute `project_dir`
(default: the directory the agent launched `devrun mcp` in) and uses that
directory's `devrun.yaml`, or the global registry when it has none. Every
result names the file it used. Pass `global: true` to use the global registry
even inside a project.

### Permissions

`list_services`, `service_status` and `logs` only read, and are marked
read-only. `add_service` followed by `start` runs whatever command the agent
chose — no more than a shell tool already allows, but worth a prompt. A
reasonable Claude Code setup auto-approves the read-only tools and asks for
the rest, in `.claude/settings.json`:

```json
{
  "permissions": {
    "allow": ["mcp__devrun__list_services", "mcp__devrun__service_status", "mcp__devrun__logs"]
  }
}
```

Agents cannot remove or edit services, or control the daemon.

### Good to know

- **Tool timeouts.** `start` waits 15 s by default and accepts up to 120 s
  (`timeout_s`). Clients cap how long a tool call may take — Codex's default
  `tool_timeout_sec` is 60 — so raise that setting before asking for longer
  waits.
- **Log files are named by service.** Two projects that both define `api`
  share `~/.local/share/devrun/logs/api.log`.
- **Names** of services and targets an agent creates are limited to letters,
  digits, `.`, `_` and `-`.

---

## File Locations

| Path | Purpose |
|---|---|
| `~/.config/devrun/services.yaml` | Global service registry |
| `~/.local/share/devrun/state.json` | Runtime state (PID, status, port) |
| `~/.local/share/devrun/logs/` | Log files per service (`<name>.log`) |
| `~/.local/share/devrun/devrun.sock` | Daemon Unix socket |
| `devrun.yaml` | Project-local service definitions |

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
