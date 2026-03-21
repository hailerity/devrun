# Design: Sample Services for procet Testing

**Date:** 2026-03-21
**Status:** Approved

---

## Overview

Two standalone Go programs under `testdata/services/` for manually testing procet's process management, log streaming, and TUI dashboard.

---

## Structure

```
testdata/
└── services/
    ├── logemitter/
    │   └── main.go      # Periodic colored log emitter
    └── chat/
        └── main.go      # Interactive fake-AI chat console
```

Both programs live inside the procet module (no separate `go.mod`). They are run via:

```sh
procet add logemitter "go run ./testdata/services/logemitter" --cwd /path/to/procet
procet add chat "go run ./testdata/services/chat" --cwd /path/to/procet
```

---

## Service 1: Log Emitter

**File:** `testdata/services/logemitter/main.go`

### Behaviour

- Infinite loop; sleeps 3 seconds between each burst
- Each burst emits 1–3 log lines at randomly chosen levels: INFO, WARN, ERROR
- Runs until killed; handles SIGTERM/SIGINT with a clean exit message

### Output Format

```
2006-01-02 15:04:05 [INFO]  GET /api/users 200 42ms
2006-01-02 15:04:05 [WARN]  database query took 312ms
2006-01-02 15:04:05 [ERROR] failed to connect to redis: connection refused
```

### Colors (ANSI, inline — no external deps)

| Level | Color  |
|-------|--------|
| INFO  | Green  |
| WARN  | Yellow |
| ERROR | Red    |

### Message Templates (~15–20, web-server flavour)

Examples:
- `GET /api/users 200 42ms`
- `POST /auth/login 201 88ms`
- `database query took 312ms`
- `cache hit ratio: 94%`
- `failed to connect to redis: connection refused`
- `worker pool exhausted, queuing request`
- `health check passed`
- `config reloaded from disk`
- `request rate: 42 req/s`
- `GC pause: 1.2ms`

---

## Service 2: Chat Console

**File:** `testdata/services/chat/main.go`

### Behaviour

- Prints `> ` prompt to stdout and reads a line from stdin
- Waits a random 500ms–1s delay (simulated "thinking")
- Prints a randomly selected response from a preset list
- Loops back to prompt immediately after responding
- Handles EOF (Ctrl+D) and SIGINT with a farewell message

### Response Pool (~15 entries, fake-AI flavour)

Examples:
- `That's an interesting perspective. Tell me more.`
- `I've processed your input. My analysis: 42.`
- `Fascinating. I'm updating my neural weights accordingly.`
- `Based on my training data, I'd recommend caution.`
- `I don't have feelings, but if I did, I'd say: agreed.`
- `Running inference... done. Confidence: 87%.`
- `I've seen 10,000 similar queries. This one is unique.`
- `Noted. Storing in long-term memory.`

---

## Dependencies

None beyond Go stdlib. Both programs use only:
- `fmt`, `os`, `time`, `math/rand`, `bufio`, `os/signal`, `syscall`

---

## Testing Purpose

| Test scenario | Service |
|---|---|
| Log streaming in TUI log panel | logemitter |
| ANSI color stripping in log viewport | logemitter |
| Service restart / crash recovery | logemitter (killable) |
| stdin/stdout passthrough via PTY | chat |
| Interactive attach (`procet attach`) | chat |
| Long-running process lifecycle | both |
