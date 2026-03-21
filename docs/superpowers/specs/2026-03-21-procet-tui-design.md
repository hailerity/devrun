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

`cmd/procet/main.go` checks `len(os.Args) == 1` (no subcommand) and delegates to `internal/tui`.

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

Always visible. Lists all registered services in registration order (order in `services.yaml`).

Each row:
```
● <name>    :<port>
```

- Status dot: green (running), red (crashed), grey (stopped/stopping/starting)
- Port shown only when detected; otherwise state label (crashed / stopped / starting)
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
- Log lines rendered in muted text; HTTP status codes colorized (2xx green, 4xx yellow, 5xx red)

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

**CONFIG** (from `services.yaml`, static):
```
cmd      yarn dev
cwd      ~/projects/api
group    backend
```

**ENV** (from `services.yaml`, static):
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
- Detection at startup; if no clipboard backend found, show `"No clipboard available"` in footer

---

## Keybindings

### Global
| Key | Action |
|---|---|
| `j` / `↓` | Move selection down (service list or log lines, whichever is focused) |
| `k` / `↑` | Move selection up |
| `Tab` | Switch between LOGS and DETAILS tabs |
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
- `Tab` switches tabs (not focus); sidebar is always reachable via mouse or `←`/`→` arrows

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
- Refresh spinner `●` in header animates while waiting for daemon response

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

---

## Out of Scope

- Service filtering / search
- Group filtering
- `procet ui` as an explicit subcommand (TUI launches only on bare `procet`)
- Log search / grep within TUI
- Restart button (no restart command in v1)
- Multiple service log tailing simultaneously
- Color theme switching
