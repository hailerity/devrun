# Configuration

## Config resolution

Every command picks its config automatically:

- If the current directory contains a `devrun.yaml`, that file is the config — `list`, `start`, `stop`, `info`, `add`, `remove`, `target`, and the TUI dashboard all operate only on its services, and `add`/`remove`/`target` edit the file in place.
- Otherwise the global registry (`~/.config/devrun/services.yaml`) is used, which holds only services you registered with `devrun add`.
- `--global` / `-g` forces the global registry even when a `devrun.yaml` is present (not valid for `up`/`down`).

Project services are sent to the daemon with their full definition inline and are never written to `services.yaml`. `devrun daemon restart` re-execs in place and hands running services (with their definitions and live log capture) to the replacement, so nothing is lost. But if the daemon instead *crashes* or is stopped with `devrun daemon stop`, a running project service keeps running yet the next daemon no longer knows its command — and log capture is paused — until you re-run `devrun up` in that directory.

## Project-local: `devrun.yaml`

Place this file in your project root and commit it. Running `devrun up` starts every service in the daemon, grouped under the project name unless a service names its own group. The definitions are sent to the daemon inline for the duration of the run — they are not written to the global `services.yaml` (see [Config resolution](configuration.md#config-resolution) above).

```yaml
name: myapp      # optional — defaults to directory name

services:
  web:
    command: yarn dev
    cwd: ./frontend  # relative to devrun.yaml; defaults to project root
    group: frontend  # optional — defaults to the project name
    env:
      PORT: "3000"
      NODE_ENV: development

  api:
    command: go run ./cmd/api
    group: backend
    env:
      PORT: "4000"

  db:
    command: postgres -D ./pgdata
    group: backend

# Optional: named subsets you can start/stop as a unit.
targets:
  frontend: [web]
  backend:  [api, db]
```

`group:` is what the dashboard sections the list by — see [the TUI's groups](dashboard.md). It is a flat label, not a path: an explicit group replaces the project-name default rather than nesting inside it, so a file that groups only some of its services shows the project's name as the header for the rest. A file that says nothing about groups behaves exactly as before, with every service under the project name.

Note that `group:` and `targets:` answer different questions. A group is where a service *lives* in the list — one group per service, so the list can be sectioned and folded. A target is a set you *act on* — a service can be in several, and `devrun target start` runs them as a unit.

## Global registry: `~/.config/devrun/services.yaml`

Managed automatically by `devrun add/remove`. You can also edit it directly.

## File locations

| Path | Purpose |
|---|---|
| `~/.config/devrun/services.yaml` | Global service registry |
| `~/.local/share/devrun/state.json` | Runtime state (PID, status, port) |
| `~/.local/share/devrun/logs/` | Log files per service (`<name>.log`) |
| `~/.local/share/devrun/logs/devrun/` | devrun's own logs — `gateway.log`, `tunnel.log` |
| `~/.local/share/devrun/devrun.sock` | Daemon Unix socket |
| `devrun.yaml` | Project-local service definitions |
