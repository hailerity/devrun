# TUI redesign — implementation plan

Interactive mock of the target design: https://claude.ai/artifact/ERuxvnZZx9EHoXy2NrhwpK

**Status:** all four PRs are open as a stack — #36 → #37 → #38 → #39. Each
merges only after the one before it, and is retargeted to `main` at that point
(CI only runs for pull requests into `main`, so the stacked ones show no checks
until then).

The two-pane shape (service list + logs) stays. The redesign fixes information
density, focus cues, and the keyboard model. It lands as four stacked PRs, each
green on its own; every numbered task below is one commit.

## Target layout

```
 ⬡ devrun  shop · devrun.yaml                                      3/5 running  ✖ 1 crashed
╭─ SERVICES · backend ───────╮╭─ api  ● running :8080  up 2h14m ────────── [LOGS] details ─╮
│▌● api        :8080     2.1% ││ 21:39:47 INFO  database query took 312ms                   │
│ ✖ chat       crashed        ││▌21:39:56 ERROR TLS certificate expires in 14 days          │
│ ○ web        stopped        ││ 22:36:52 ERROR rate limit exceeded for IP 10.0.0.1         │
╰─ 1/3 up ───────────────────╯╰─ 1,204 lines ─────────────────────────────────── ↓ 37 new ─╯
 s start  x stop  r restart  ↵ details  t target  / search                   ? help  q quit
```

## Keymap after the redesign

| Key | Action |
|-----|--------|
| `j/k` `↑/↓` | move the cursor in the focused pane |
| `Tab` `←/→` | switch pane (in the narrow layout: swap the visible pane) |
| `↵` | LOGS ⇄ DETAILS — the same on every row |
| `s` / `x` / `r` | start / stop / restart the selected service |
| `S` / `X` | start / stop everything listed (the filtered target, or all services) |
| `t` | target picker: `↵` filter, `e` edit, `Esc` close |
| `/` `n` `N` | search the log, next / previous match |
| `f` `w` `v` `y` `g` `G` | follow, wrap, select, copy, top, end (`G` re-follows) |
| `e` / `d` | edit / remove the selected service |
| `?` | key help overlay |
| `q` | quit |

## PR 1 — targets become a filter (`feat/tui-target-filter`)

Removes the TARGETS rows, the two-list cursor, and every roll-up guard in
`handleKey`. The sidebar becomes one list with one cursor.

1. **Plan document** — goal: this file is in the repo.
2. **Target picker and single-list sidebar** — goal: `t` opens a modal listing
   "All services" and every target with `running/total` counts and the
   highlighted target's members; `j/k` move, `↵` applies the filter, `e` opens
   the target editor, `Esc`/`t` close. `sidebar` holds only services plus
   `filterTarget`; `sectionTargets`, target rows, `sidebarRollup`,
   `targetDetailsPanel` and the SUMMARY/TARGET main-pane views are gone; the
   active filter shows in the section header (`SERVICES · backend`); `↵` always
   toggles LOGS ⇄ DETAILS. (One commit: the picker cannot build against the
   old two-list sidebar.) Done when the picker and rewritten sidebar tests pass.
3. **`S` / `X` act on everything listed** — goal: with a filter they send
   `target-start` / `target-stop`; without one they run the start-all /
   stop-all batch. Done when model tests cover both paths.
4. **README** — goal: the TUI section describes the picker and the new keys.

## PR 2 — visual structure (`feat/tui-redesign-visual`, stacked on PR 1)

5. **Adaptive palette and state glyphs** — goal: colours are
   `lipgloss.AdaptiveColor` pairs readable on light and dark terminals; state
   is `●` running, `◐` starting/detecting, `○` stopped, `✖` crashed.
6. **Service table rows** — goal: each row is glyph · name · port-or-state ·
   CPU (coloured only above 50 %); crashed services sort first; the bottom info
   block and duplicate `s/x` hints are removed.
7. **Bordered panes and one-row header** — goal: both panes draw a rounded
   border, accent-coloured when focused; the main pane title names the service,
   state, port and uptime and shows both tab labels; the log pane's bottom
   border carries the line count and follow / wrap state; the header is one row
   with the config source and a red crashed count. Layout arithmetic and mouse
   offsets are updated.
8. **Modals float over the body** — goal: edit / target / remove / picker
   modals are composited over the dimmed panes instead of replacing them.
9. **Sidebar scroll window** — goal: a list longer than the pane scrolls to
   keep the cursor visible, and the border says `6–15 of 40`. (Moved up from
   PR 4: the bordered pane clips the list, so without scrolling the selected
   row could sit below the fold.)

## PR 3 — footer, help, log tools (`feat/tui-footer-help-logs`, stacked on PR 2)

10. **Priority footer** — goal: hints carry a priority; when the row is too
   narrow whole hints are dropped lowest-priority first; `? help` and `q quit`
   are pinned right.
11. **Help overlay** — goal: `?` shows the full keymap grouped by area.
12. **New-line counter** — goal: with follow off, the log border shows
    `↓ N new`; `G` clears it and re-follows.
13. **Log search** — goal: `/` opens an input in the footer, matches are
    highlighted, `n`/`N` step through them, `Esc` clears; the border shows
    `k/N matches`.
14. **Restart** — goal: `r` stops then starts the selected service.

## PR 4 — details, scrolling, small terminals (`feat/tui-details-narrow`, stacked on PR 3)

15. **Focusable DETAILS** — goal: DETAILS takes focus, has a row cursor, scrolls,
    and `y` copies the value under the cursor.
16. **Narrow single-pane layout** — goal: below 70 columns only the focused
    pane is drawn and `Tab` swaps it.
17. **README and screenshot notes** — goal: README keymap matches the new UI.

## Verification for every task

`go build ./... && go test ./... -count=1 && go vet ./...`. Layout is verified
by tests that render `model.View()` at several terminal sizes and assert the
output is exactly the terminal's rows and columns (an extra row scrolls the
header off a real terminal), and by reading rendered frames during development.

Not covered by the automated checks: running the real binary in a terminal.
Starting `devrun` spawns the daemon against the user's own state directory, so
that pass — true-colour rendering, light-terminal colours, mouse selection,
resize behaviour — is left to the human reviewer of each PR.
