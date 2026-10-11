# The README's static hero, assets/screenshot.png.
#
# Separate from assets/demo.tape on purpose. Screenshot does not capture in
# step with the commands around it — in demo.tape it caught a frame about
# three seconds late, after the next navigation had moved the cursor off the
# service it was meant to show. Here nothing happens after the capture, so
# there is nothing for it to race.
#
# Run via `make demo`, which records this and the GIF together.

Output assets/screenshot-unused.gif

Set Shell "bash"
Set FontSize 16
Set Width 1200
Set Height 640
Set Padding 20
Set Framerate 6
Set Theme "Catppuccin Mocha"

Hide
Type "export D=$(mktemp -d)" Enter
Type "export XDG_DATA_HOME=$D/data XDG_CONFIG_HOME=$D/config" Enter
Type "export PATH=$PWD/bin:$PATH" Enter
Type `[ "$XDG_DATA_HOME" = "$D/data" ] || echo "SETUP BROKE: XDG not under $D"` Enter
Type "cd playground" Enter
Type `for p in 5173 4000 8787 8788 8789 8790 5433 6380; do lsof -nP -iTCP:$p -sTCP:LISTEN >/dev/null 2>&1 && echo "PORT $p IN USE - the still will record a false crash"; done` Enter

# Let the services run before the dashboard opens: port detection takes 3-6s,
# and the log pane shows what has accumulated since start, so a frame taken
# too early has an empty PORT column beside an empty pane.
Type "devrun up" Enter
Sleep 10s
Type "clear" Enter
# Wait for clear to actually run; Show/Screenshot do not wait for it.
Sleep 1.5s
Type "devrun" Enter
Sleep 4s

# No navigation. The cursor restores from an empty anchor to the first service
# row — `admin-api` here, since groups sort before services do and `admin` is
# the first group. An earlier version walked the cursor "to be sure" and landed
# somewhere else entirely: the sidebar's step() is modular, so k wraps off the
# top of the list rather than clamping on it.
Show
Sleep 6s

Screenshot assets/screenshot.png

# Screenshot does not capture at this instruction; it flags a later *visible*
# frame. Hiding straight after it produced no file at all, and in demo.tape,
# where the next visible frames came from a keypress, it captured the screen
# roughly three seconds on, after the cursor had moved. So: stay visible, and
# stay still, until it has had its frame.
Sleep 5s

Hide
Type "q" Enter
Sleep 1s
Type "devrun down; devrun daemon stop; cd ..; rm -rf $D" Enter
Sleep 2s
