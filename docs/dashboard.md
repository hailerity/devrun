# The dashboard

Everything `devrun` (no arguments) puts on screen, and every key it listens for.

```
 ⬡ devrun  acme · devrun.yaml                               9/10 running  ✖ 1 crashed
╭─ SERVICES ──────────────────╮╭─ bff  ● running :8787  up 2h 14m ─ [LOGS] details ─╮
│ ▾ admin                2/2  ││ GET  /api/users            200   12ms              │
│ ● admin-api  :8788    0.4%  ││ POST /api/auth             201   45ms              │
│ ● console    :4000    0.9%  ││ GET  /api/products?page=2  200   23ms              │
│                             ││ POST /api/orders           201   61ms              │
│ ▸ payments             2/2  ││ GET  /api/cart             500    8ms              │
│                             ││ DELETE /api/cart/items/3   204   14ms              │
│ ▾ platform             3/4  ││ GET  /api/orders/8821      200    9ms              │
│ ● postgres   :5433    1.1%  ││ GET  /api/users            200   11ms              │
│ ● redis      :6380    0.2%  ││ POST /api/auth             201   39ms              │
│ ✖ search     crashed        ││ GET  /api/cart             200    6ms              │
╰─ 9/10 up ───────────────────╯╰─ 1,204 lines ─────────────────────────── ⇣ follow ─╯
 s start  x stop  r restart  ↵ details  / filter  t target             ? help  q quit
```

Each service row shows a state glyph (`●` running, `◐` starting / stopping, `○` stopped, `✖` crashed), the name, the port or state, and CPU. The list is alphabetical and stays that way, so it never reshuffles under the cursor. The focused pane has the accent-coloured border, and the main pane's border always names the service whose logs or details it shows. Colours adapt to light and dark terminals.

When more than one `group` is in play the list is **sectioned by group**, with a header per group carrying its running count:

```
╭─ SERVICES ──────────────────╮
│ ▾ platform             3/4  │
│ ● postgres   :5433    1.1%  │
│ ● redis      :6380    0.2%  │
│ ✖ search     crashed        │
│                             │
│ ▸ payments             2/2  │
│                             │
│ ▾ (no group)           0/1  │
│ ○ scratch    stopped        │
╰─ 5/7 up ────────────────────╯
```

A blank line sits above every header but the first, and a header is drawn in its own grey — quieter than a service name so it reads as a label rather than as an emphasised service.

`Space` (or `↵`) on a header folds the group shut; the cursor walks headers as it does in a file tree. Groups are alphabetical with the ones with no group last, and a header only appears when there is more than one group — a single project looks exactly as it did before. A group's services keep their place inside it, so a service still never moves because its state changed.

Three things worth knowing about folding:

- **A collapsed group hides what is inside it, including a failure.** The header's count (`1/3`) is the only hint; there is no crash marker on a folded group. That is deliberate — collapsing hides what it hides.
- **An active `/` query suspends every fold**, so a match is never hidden behind one. The folds come back as they were when the query is cleared.
- **Folding is not filtering.** `S` / `X` still act on a folded group's services: a query or a target changes what you are working on, while folding only gets a group out of the way on screen.

Where the group comes from: a service's `group:` in a [`devrun.yaml`](configuration.md#project-local-devrunyaml), or `devrun add --group` in either config. A project service that sets no group inherits the project's name, so a file that never mentions groups is one group — and a single group draws no header.

The sidebar is one list of services, and two things can narrow it:

- **`/` filters by name** — with the sidebar focused, `/` opens a query input and the list narrows as you type (case-insensitive substring, so `web` finds both `web` and `webhook`). `↵` keeps the filter. `Esc` **in the input** cancels, putting back the query it opened on — which is no filter for a fresh one, or the previous query when you reopened `/` to amend an existing filter. `Esc` **on the list**, once the filter is in force, clears it.
- **`t` opens the target picker** when the active config defines targets: every target with its running count and members. `↵` filters the list to that target, `All services` clears the filter, and `e` edits the highlighted target.

Both apply at once — a query searches within the filtering target — and the SERVICES heading names whichever are active (`SERVICES · frontend · /web`), so the reason a service is missing from the list is always on screen.

**Navigation:**

| Key | Action |
|---|---|
| `k` / `↑` | Move up |
| `j` / `↓` | Move down |
| `←` / `→` | Focus sidebar / main panel |
| `Tab` | Toggle focus between sidebar and main panel |
| `↵` | Toggle DETAILS / LOGS for the selected service (on a group header: fold it) |
| `Esc` | Back out of DETAILS to LOGS |

On a terminal narrower than 70 columns only the focused pane is shown, at full width: `Tab` swaps panes, and `↵` on a service opens it.

**Service / target control:**

| Key | Action |
|---|---|
| `s` / `x` | Start / stop the selected service |
| `r` | Restart the selected service (starts it if it is not running) |
| `S` / `X` | Start / stop everything the list is showing — narrowed by `/` and `t`, or every service when neither is active |
| `/` | Filter the service list by name (sidebar focused — `↵` keeps it, `Esc` in the input cancels, `Esc` on the list clears) |
| `Space` | Fold or unfold the group under the cursor (group headers only; `↵` does the same there) |
| `t` | Open the target picker (`↵` filter, `e` edit target, `Esc` close) |
| `p` | Let the selected service leave this machine, or stop it — toggles the gateway's `expose` allowlist and writes it to the active config. It never starts a tunnel |
| `e` | Edit the selected service (sidebar focused) |
| `d` | Remove the selected service (sidebar focused, asks to confirm) |

Under a `/` query, `S` / `X` act on the named services one by one rather than on the target as a unit. That is the point — you narrowed the list to those rows — but it means `X` will stop a service even when another started target still holds it, where `X` on an unqueried target leaves such a member running.

Pressing `e` opens a modal editor. It writes back to the active config — the project `devrun.yaml` when one is in scope, otherwise `~/.config/devrun/services.yaml` — using the same resolution as `devrun add`.

- **On a service** (`e` in the sidebar) the modal edits the service's name, command, working directory and group. Saving refuses an empty name or command, or a name that collides with another service; the group is free-form, since it is only a label the list is sectioned by. Clearing it is a real edit — a service in a `devrun.yaml` goes back to inheriting the project's name. If the edited service is running it is stopped and restarted (under the new name, on a rename) so the change takes effect immediately.
- **On a target** (`e` in the target picker) the modal edits the target's name and members: a name field plus a checklist of every service — `Tab` switches between the two, `space` toggles a service in or out. Saving refuses an empty name or one that collides with another target. A running target keeps its current membership until you stop and start it again.

Pressing `d` on a service asks to confirm, then deletes that service from the active config — the same file `e` writes to, the same effect as `devrun remove`. A running service must be stopped first.

**Log panel:**

| Key | Action |
|---|---|
| `/` | Search the log (main panel focused) — matches highlight as you type, `↵` jumps to the nearest match above the cursor, `Esc` cancels |
| `n` / `N` | Next match down / previous match up (wraps) |
| `f` | Toggle follow mode |
| `g` / `G` | Jump to top / jump to the end and follow |
| `w` | Toggle line wrap |
| `v` | Enter visual selection mode |
| `y` / `Ctrl+C` | Copy selection (or current line) |
| `Esc` | Exit visual mode, then clear the search |

The log pane's bottom border shows the line count, the match position while a search is active (`2/17 matches`), and the follow state. With follow off it counts lines that arrived out of view (`↓ 37 new`); `G` jumps to them.

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

The footer shows the keys for the focused pane. On a narrow terminal it drops whole hints, least useful first; `? help` and `q quit` always stay.
