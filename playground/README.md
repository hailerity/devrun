# Playground

A project to try devrun on, and the one the README's screenshot and GIF are recorded from. Ten services across four sub-projects, nine of which come up and one of which fails on purpose.

```sh
cd playground
devrun up        # start all ten
devrun           # the dashboard
devrun down      # stop them again
```

Then, in the dashboard:

| | |
|---|---|
| `j` / `k` | walk the list — headers included |
| `Space` on a header | fold that sub-project away |
| `/` then `api` | narrow to matching services across every group; `Esc` clears |
| `t` | filter by target instead — `storefront`, `admin`, `payments`, `data` |
| `↵` on `search` | its log says why it died; `↵` again for DETAILS |
| `e` | edit a service, written straight back to `devrun.yaml` |
| `?` | every key |

`search` is meant to be red. It exits non-zero two seconds in, so there is always one failure on screen — a dashboard where everything is green shows you less about a process manager than one where something is wrong.

## What's in here

| | |
|---|---|
| `devrun.yaml` | the project: four groups, four targets, one deliberate failure |
| `serve.py` | binds a port and narrates plausible traffic. Standard library only |
| `services/logemitter/` | Go sample emitting coloured `INFO` / `WARN` / `ERROR`, run as `events` |
| `services/chat/` | Go sample that reads stdin — for trying `devrun fg`, see below |

`serve.py` takes a port and a *kind* (`web`, `api`, `db`, `cache`, `mail`), so adding a service to `devrun.yaml` needs no new code.

### Trying `devrun fg`

`chat` reads from stdin, which is what `fg` is for — bringing a background service to your terminal. It is not in `devrun.yaml`, because a service waiting on input has nothing to show in a dashboard. Add it when you want it:

```sh
devrun add chat "go run ./services/chat" --group platform
devrun start chat
devrun fg chat          # type at it; Ctrl+P then Q detaches, Ctrl+C stops it
```

## Before you run it

**Service names are global to the daemon, not scoped to this project.** devrun keys running services and their log files by name across every project, so if you already have a service called `web`, `redis` or `postgres`, this project's services of that name will collide with yours. Rename them here first, or run the playground against a scratch state directory:

```sh
XDG_DATA_HOME=$(mktemp -d) XDG_CONFIG_HOME=$(mktemp -d) devrun up
```

**It binds ports** 5173, 4000, 8787, 8788, 8789, 8790, 5433 and 6380. A port already in use makes that service record as a crash, which looks like a devrun failure and is not one.

**It needs `python3` and `go`** — python for the eight port-bound services, Go for `events`. Both are already there if you can build this repo.

## Re-recording the README's images

```sh
brew install vhs     # pulls in ttyd and ffmpeg
make demo
```

That drives this project through `assets/demo.tape` and `assets/screenshot.tape` under a throwaway state directory, and writes `assets/demo.gif` and `assets/screenshot.png`.
