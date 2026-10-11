# devrun demo — regenerates the README hero, assets/demo.gif.
#
# The still (assets/screenshot.png) is recorded by its own tape, because
# Screenshot does not capture in step with the commands around it: taken here,
# it caught the frame roughly three seconds later, after the next navigation
# had already moved the cursor. A tape that does nothing after the capture
# cannot race it. See assets/screenshot.tape.
#
#     brew install vhs        # pulls in ttyd + ffmpeg
#     make demo
#
# Run it natively. The VHS Docker image is Linux, and `make build` produces a
# binary for *this* machine, so a macOS build cannot execute in there — the
# recording then captures nothing but exec errors. That image also has no zsh,
# and the default Docker Desktop VM (~2 GB) gets ffmpeg OOM-killed on the GIF
# palette pass. To use it anyway:
#
#     GOOS=linux GOARCH=arm64 go build -o bin/devrun ./cmd/devrun
#     docker run --rm -m 6g -v $PWD:/vhs ghcr.io/charmbracelet/vhs assets/demo.tape
#
# ...and raise Docker Desktop's memory limit to match the -m flag.
#
# Driven against ./playground, in place: that is the project the README tells
# people to run, so the recording and the invitation cannot drift apart. Only
# the state directory is a throwaway, so your real registry, daemon and
# services are never touched — nothing is written into the repo.
#
# Keep it short. Frames are held in memory for the GIF palette pass, so a
# minute of 25fps at this size is enough to get ffmpeg OOM-killed.

# Strings here take ", ' or backticks as delimiters, and there is NO backslash
# escape: a \" ends the string early and the rest parses as stray commands. A
# line that needs double quotes inside it is delimited with backticks.

Output assets/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 20
Set Framerate 8
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms

# --- setup, not recorded -----------------------------------------------
Hide
# One assignment per command. `export A=$(mktemp -d) B=$A/data` does NOT work:
# export expands all of its arguments before any of them is assigned, so $A is
# still empty and B becomes "/data" — which on macOS is an uncreatable path on
# the read-only root volume, and devrun then times out waiting for a daemon
# that can never open its socket.
Type "export D=$(mktemp -d)" Enter
Type "export XDG_DATA_HOME=$D/data XDG_CONFIG_HOME=$D/config" Enter
Type "export PATH=$PWD/bin:$PATH" Enter
Type `[ "$XDG_DATA_HOME" = "$D/data" ] || echo "SETUP BROKE: XDG not under $D"` Enter
Type "cd playground" Enter
Type "devrun --version >/dev/null || echo 'BUILD devrun FIRST: make build'" Enter
# A port devrun cannot bind records as a crash, so a demo port already in use
# silently turns the hero into a wall of failures.
Type `for p in 5173 4000 8787 8788 8789 8790 5433 6380; do lsof -nP -iTCP:$p -sTCP:LISTEN >/dev/null 2>&1 && echo "PORT $p IN USE - the demo will record a false crash"; done` Enter
Type "clear" Enter
# `clear` is typed, not yet executed: Show begins capturing before the terminal
# has processed it, and the first second of the GIF was the whole hidden setup
# — the exports, the port check — sitting above `devrun up`. Wait for the
# screen to actually be empty.
Sleep 1.5s
Show

# --- one command starts the project ------------------------------------
Type "devrun up" Sleep 300ms Enter
Sleep 1.6s

# --- the dashboard -----------------------------------------------------
# Port detection was measured at 3-6s after start, so the screenshot waits:
# taken earlier the PORT column is still empty, and the ports are most of what
# makes the frame worth looking at. The wait sits here rather than after
# `devrun up` so it is spent on the dashboard, not on a static shell.
Type "devrun" Sleep 300ms Enter
Sleep 2.4s

# It opens on `admin-api`, the first service row, with its log already running.
Sleep 1.2s

# DETAILS: state, pid, port, uptime, cpu, mem, then the command, directory and
# group it was started from, then its environment. Enter toggles back to LOGS.
Enter Sleep 2.2s
Enter Sleep 600ms

# Down to the crash at the bottom of `platform`. Nine steps: the cursor walks
# headers too and skips the blank rows between groups, and the counts are exact
# because step() is modular — an overshoot wraps to the other end of the list
# rather than stopping at the edge.
Down@260ms 9
Sleep 1.5s

# Up four to that group's header, and fold it away. Ten services is where this
# starts to matter: platform is four of them and none are what you are working
# on right now.
Up@300ms 4
Space Sleep 1.3s
Space Sleep 500ms

# Back onto a service before going on. The cursor was left on a header by the
# fold, and a header selects nothing — so the main pane read "No service
# selected" through the next three scenes. One step down is `events`, the Go
# sample, whose coloured INFO / WARN / ERROR also shows that devrun passes a
# service's ANSI through untouched.
Down@400ms 1
Sleep 1.2s

# The target picker. Row 0 is "All services", then the targets in name order —
# admin, data, payments, storefront — each with its running count, and the
# members of whichever is highlighted listed underneath. Three down is
# `payments`; Enter applies it as the filter.
Type "t" Sleep 1.2s
Down@300ms 3
Sleep 1.1s
Enter Sleep 1.5s

# A target says what you are working on; a query searches inside it. Both chips
# show in the SERVICES title, so the reason a service is missing is on screen.
Type "/" Sleep 400ms
Type@150ms "led"
Sleep 1.3s
Escape Sleep 600ms

Type "q"
Sleep 600ms

# --- teardown, not recorded --------------------------------------------
Hide
Type "devrun down; devrun daemon stop; cd ..; rm -rf $D" Enter
Sleep 2s
