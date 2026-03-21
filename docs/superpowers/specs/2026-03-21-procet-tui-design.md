# procet TUI Design

**Date:** 2026-03-21
**Status:** Approved
**Scope:** `procet ui` — terminal dashboard for monitoring and managing services

---

## Goal

Launch a full-screen TUI when the user runs `procet` with no arguments. The TUI provides real-time monitoring, log tailing, and start/stop control for all registered services — replacing the need to run `procet list`, `procet logs`, and `procet start/stop` separately.

---

## Invocation

```bash
procet        # launches TUI (no arguments = TUI mode)
procet list   # still works as a plain CLI command
```

The TUI implementation adds a `RunE` to `rootCmd` in `internal/cli/root.go` that launches the TUI when no subcommand is provided. This preserves `procet --help` and flag-only invocations correctly (cobra only calls `RunE` when no subcommand is matched). `cmd/procet/main.go` remains unchanged.

---

## Layout — Sidebar + Main (Option B)

```
┌─────────────────────────────────────────────────────────────────────┐
│ ⬡ procet                              4 services · 3 running · ●   │
├──────────────────┬──────────────────────────────────────────────────┤
│ SERVICES         │ LOGS                          DETAILS            │
│                  │                                                   │
│ ● web      :3000 │ [10:14:30] GET /metrics 200 3ms                  │
│ ● api      :8080 │ [10:14:33] GET /users 200 14ms                   │  ← selected
│ ● db    crashed  │ [10:14:35] POST /login 201 8ms                   │
│ ● worker stopped │ [10:14:36] GET /health 200 1ms                   │
│                  │                                                   │
│ ── api ──────── │                                                   │
│ CPU  0.8%        │                                                   │
│ MEM  182M        │                                                   │
│ UP   2h 14m      │                                                   │
│                  │                                                   │
│ [s] start        │                                                   │
│ [x] stop         │                                                   │
├──────────────────┴──────────────────────────────────────────────────┤
│ [Tab] switch  [y] copy  [v] select  [f] follow  [j/k] scroll  [q]  │
└─────────────────────────────────────────────────────────────────────┘
```

**Three regions:**
- **Header** — title, global service count, refresh indicator
- **Body** — sidebar (left, fixed ~20 cols) + main panel (right, fills remaining width)
- **Footer** — context-sensitive keybinding hints

---

## Visual Style — GitHub Dark

| Element | Color |
|---|---|
| Background | `#0d1117` |
| Text | `#c9d1d9` |
| Muted text | `#6e7681` |
| Accent / selection | `#58a6ff` |
| Running indicator | `#3fb950` (green) |
| Crashed indicator | `#f85149` (red) |
| Stopped indicator | `#6e7681` (grey) |
| Warning (CPU high) | `#f0e68c` (yellow) |
| Border / separator | `#21262d` |
| Selected row bg | `#161b22` |
| Visual selection bg | `#1f3a5f` |

---

## Sidebar

Always visible. Lists all registered services in alphabetical order by name. (`Registry.Services` is a map; alphabetical sort ensures deterministic, predictable ordering.)

Each row:
```
● <name>    :<port>
```

**Service states** (from `config.Status*` constants) and their visual treatment:

| State | Dot color | Right-side label |
|---|---|---|
| `running` | `#3fb950` green | `:<port>` if detected, else `"detecting"` |
| `starting` | `#6e7681` grey | `"starting"` |
| `stopping` | `#6e7681` grey | `"stopping"` |
| `stopped` | `#6e7681` grey | `"stopped"` |
| `crashed` | `#f85149` red | `"crashed"` |

- `"detecting"` is shown when `ServiceInfo.Port == 0` and state is `running`; the daemon polls ports every 5s so this resolves automatically; no TUI-side timeout
- Selected service highlighted with left border `│` in accent blue and darker background
- Selected service highlighted with left border `│` in accent blue and darker background

**Mini stats panel** below the service list (for selected service only):
```
── <name> ──
CPU  0.8%
MEM  182M
UP   2h 14m
```

**Action hints** at sidebar bottom:
```
[s] start
[x] stop
```

Actions apply to the currently selected service.

---

## Main Panel

Two tabs switched with `Tab`:

### LOGS tab (default)

- Streams log output from `~/.local/share/procet/logs/<name>.log`
- Reads the file directly (no daemon required) — same as `procet logs`
- **Follow mode** (default on): auto-scrolls to newest line; indicator `● follow` shown in tab bar
- **Scroll mode**: mouse wheel or `j`/`k` disables follow mode automatically; `f` re-enables it
- Log lines rendered in muted text; HTTP status codes colorized using the palette: 2xx → `#3fb950` (green), 4xx → `#f0e68c` (yellow), 5xx → `#f85149` (red)

### DETAILS tab

Two sections separated by a divider:

**STATUS** (live, polled every 2s):
```
state    ● running
pid      12041
port     :8080
uptime   2h 14m 32s
cpu      0.8%
mem      182M
started  10:00:04
```

**CONFIG** (from `services.yaml`, static; maps `ServiceConfig` fields: `Command`→`cmd`, `CWD`→`cwd`, `Group`→`group`):
```
cmd      yarn dev
cwd      ~/projects/api
group    backend
```

**ENV** (from `services.yaml`, static; shows only the explicit `ServiceConfig.Env` map — not inherited shell environment, since `ServiceInfo` carries no inherited env; one entry per line in `KEY=value` format; section is omitted entirely if `Env` is empty or nil):
```
NODE_ENV=development
PORT=8080
```

---

## Copy Mechanism

The TUI owns the mouse (mouse events captured by bubbletea). OS drag-select is not available inside the TUI; users must use `Option`+drag (macOS) or `Shift`+drag (Linux) to bypass.

### Single line copy
- Navigate to a log line with `j`/`k` or mouse click
- Press `y` — copies the line text to clipboard
- Brief `"Copied!"` toast appears in footer for 1.5s

### Multi-line copy (visual selection)
- Press `v` to enter visual selection mode — current line highlighted
- Extend selection with `j`/`k` (or Shift+click to click-extend)
- Press `y` to copy all selected lines joined with newlines
- Press `Esc` to cancel

Selection highlights lines with `#1f3a5f` background and a blue left border.

### Clipboard backend
- macOS: `pbcopy`
- Linux: `xclip -selection clipboard` (fallback: `xsel --clipboard`)
- Detection runs once at TUI startup (cached for the session); if no backend found, every copy attempt shows `"No clipboard available"` as a 1.5s footer toast

---

## Keybindings

### Global
| Key | Action |
|---|---|
| `j` / `↓` | Move selection down (service list or log lines, whichever is focused) |
| `k` / `↑` | Move selection up |
| `←` | Move focus to sidebar |
| `→` | Move focus to main panel |
| `Tab` | Switch between LOGS and DETAILS tabs (when main panel focused); moves focus to main panel first if sidebar is focused |
| `s` | Start selected service |
| `x` | Stop selected service |
| `q` / `Ctrl+C` | Quit TUI |

### Logs panel (when focused)
| Key | Action |
|---|---|
| `f` | Toggle follow mode |
| `y` | Copy current line to clipboard |
| `v` | Enter visual selection mode |
| `Esc` | Cancel visual selection |
| `g` | Jump to top |
| `G` | Jump to bottom |
| Mouse wheel | Scroll (disables follow mode) |
| Shift+click | Extend selection to clicked line |

### Panel focus
- Clicking the sidebar focuses service navigation
- Clicking the main panel focuses log scroll / line selection
- `Tab` switches tabs (not focus) and only applies when the main panel is focused; pressing `Tab` while the sidebar is focused first moves focus to the main panel, then switches tabs on the next press
- Sidebar is always reachable via mouse click or `←` arrow; `→` moves focus from sidebar to main panel

---

## Start / Stop UX

No confirmation dialog — actions are instant (matching `procet start` / `procet stop` CLI behavior).

- **Start:** sends `StartRequest` to daemon via Unix socket; service transitions to `starting` then `running`; sidebar dot updates within the next refresh cycle (2s)
- **Stop:** sends `StopRequest`; service transitions to `stopping` then `stopped`
- If daemon is not running, `EnsureDaemon` is called automatically before the first action
- Errors shown as a 3s toast in the footer: `"error: web is already running"`

---

## Refresh

- Service list and mini stats: polled every **2 seconds** via `ListRequest` to daemon
- Log file: read continuously via file polling (100ms interval, same as `procet logs -f`)
- Details tab: polled every **2 seconds** (same `ListRequest`)
- Refresh spinner in header animates through frames `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` (Braille dot sequence, 100ms per frame) while waiting for daemon response; shows `●` when idle
- Bubbletea's model-diff rendering means 2s polls re-render only changed cells and do not interrupt log scrolling or visual selection

---

## Architecture

```
internal/
└── tui/
    ├── model.go       # bubbletea root model; Init, Update, View
    ├── sidebar.go     # service list component; navigation, mini stats
    ├── logs.go        # log panel; file tail, scroll, visual selection, copy
    ├── details.go     # details panel; status + config + env rendering
    ├── header.go      # header bar; service counts, refresh indicator
    ├── footer.go      # footer bar; context keybinding hints, toasts
    ├── keys.go        # keybinding definitions (bubbletea key.Binding)
    ├── clipboard.go   # pbcopy / xclip / xsel abstraction
    └── styles.go      # lipgloss color and layout constants (GitHub Dark palette)
```

**Framework:** [bubbletea](https://github.com/charmbracelet/bubbletea) (Charm) for the event loop and component model; [lipgloss](https://github.com/charmbracelet/lipgloss) for layout and styling; [bubbles](https://github.com/charmbracelet/bubbles) viewport for scrollable log panel.

**Data source:**
- Service list / details: `internal/client` → daemon Unix socket (same IPC as CLI)
- Logs: direct file read from `~/.local/share/procet/logs/<name>.log` (no daemon)

The TUI runs in the same process as the CLI; no new binary or daemon involved.

---

## Error States

| Situation | Behavior |
|---|---|
| Daemon not running | Auto-start via `EnsureDaemon`; show `"Starting daemon..."` toast |
| Daemon unreachable after start | Show `"Daemon unavailable"` in header; retry every 5s |
| Log file missing | Show `"No logs yet for <name>"` in log panel |
| No clipboard backend | Show `"No clipboard available"` on copy attempt |
| Start/stop error | Show error in footer toast for 3s |
| No services registered | Sidebar shows `"No services — run procet add <name>"` placeholder; main panel is empty |

---

## Out of Scope

- Service filtering / search
- Group filtering
- `procet ui` as an explicit subcommand (TUI launches only on bare `procet`)
- Log search / grep within TUI
- Restart button (no restart command in v1)
- Multiple service log tailing simultaneously
- Color theme switching
