#!/usr/bin/env python3
"""A demo service for the devrun recordings.

Binds a port and narrates plausible traffic. Both halves matter to the
recording: the open port is what devrun's detection reports in the sidebar, and
the output is what fills the log pane. `python3 -m http.server` gave the first
and not the second, so the hero frame had a correct sidebar beside an empty box.

    python3 serve.py <port> <kind>

`kind` picks the flavour of chatter, not the service name — a dozen services
share five kinds, so adding one to devrun.yaml needs nothing here.

Standard library only, so the demo has no setup beyond python3.
"""
import http.server
import random
import socketserver
import sys
import threading
import time

PORT = int(sys.argv[1])
KIND = sys.argv[2] if len(sys.argv) > 2 else "api"

# Seeded from the port, so each service reads differently but every run of a
# service reads the same. The README tells people they can regenerate the
# screenshot and GIF; unseeded, every recording drew a different log and one of
# them happened to put five 500s in the twenty lines the hero shows, which made
# a working demo look like a failing one.
random.seed(PORT)

# Keyed by kind so each pane reads like the thing it claims to be.
# (text, typical milliseconds) — the latency is jittered per line, because five
# canned entries cycling with identical timings read as a loop the moment
# anyone looks closely at the recording.
CHATTER = {
    "web": [
        ("GET  /", 4),
        ("GET  /@vite/client", 2),
        ("GET  /src/App.tsx", 7),
        ("GET  /src/Checkout.tsx", 6),
        ("GET  /assets/logo.svg", 1),
        ("hmr  update /src/Cart.tsx", 18),
    ],
    "api": [
        ("GET  /api/users", 12),
        ("POST /api/auth", 45),
        ("GET  /api/products?page=2", 23),
        ("GET  /api/cart", 8),
        ("POST /api/orders", 61),
        ("GET  /api/orders/8821", 9),
        ("DELETE /api/cart/items/3", 14),
    ],
    "db": [
        ("SELECT * FROM users", 4),
        ("SELECT * FROM products", 11),
        ("INSERT INTO orders", 6),
        ("UPDATE carts SET total", 3),
        ("checkpoint complete", 118),
    ],
    "cache": [
        ("GET  session:8f21c", 1),
        ("SETEX cart:1174 900", 1),
        ("HGET product:33 price", 1),
        ("EXPIRE session:8f21c", 1),
        ("INFO replication", 2),
    ],
    "mail": [
        ("SMTP accepted <receipt@acme.test>", 7),
        ("SMTP accepted <welcome@acme.test>", 6),
        ("stored message 414", 2),
        ("stored message 415", 2),
    ],
}
LINES = CHATTER.get(KIND, CHATTER["api"])
STATUS = {"GET": 200, "POST": 201, "DELETE": 204}

# Some red, not a lot: enough that the log is clearly a real one, few enough
# that the service does not look broken.
ERROR_RATE = 0.03


def line():
    text, typical = random.choice(LINES)
    ms = max(1, int(random.gauss(typical, typical * 0.25)))
    verb = text.split()[0]
    if KIND in ("web", "api") and verb in STATUS:
        # Occasionally something is not fine, which is what a log is for.
        code = 500 if random.random() < ERROR_RATE else STATUS[verb]
        return f"{text:<28}{code:>5}{ms:>6}ms"
    return f"{text:<28}{ms:>11}ms"


def narrate():
    previous = None
    while True:
        # ~3 lines a second: fast enough that the log pane fills and clearly
        # moves, slow enough that the GIF compresses. Every line that scrolls
        # makes a frame differ from the one before it, and at 5/s the recording
        # came out half again as large for no more information.
        time.sleep(random.uniform(0.22, 0.45))
        out = line()
        while out == previous:
            out = line()
        previous = out
        print(out, flush=True)


threading.Thread(target=narrate, daemon=True).start()

socketserver.TCPServer.allow_reuse_address = True
with socketserver.TCPServer(("", PORT), http.server.SimpleHTTPRequestHandler) as srv:
    srv.serve_forever()
