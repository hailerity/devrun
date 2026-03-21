# Sample Services Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create two standalone Go test services — a colored log emitter and an interactive fake-AI chat console — under `testdata/services/` for manually exercising procet's process management, log streaming, and TUI dashboard.

**Architecture:** Two independent `main` packages, each a single `main.go` file with no external dependencies beyond Go stdlib. Both live inside the existing `github.com/hailerity/procet` module (no separate `go.mod`). Each is run via `go run ./testdata/services/<name>` or registered with `procet add`.

**Tech Stack:** Go stdlib only — `fmt`, `os`, `time`, `math/rand`, `bufio`, `os/signal`, `syscall`

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `testdata/services/logemitter/main.go` | Log emitter binary |
| Create | `testdata/services/chat/main.go` | Chat console binary |

---

### Task 1: Log Emitter

**Files:**
- Create: `testdata/services/logemitter/main.go`

- [ ] **Step 1: Create the file with package declaration and imports**

```go
// testdata/services/logemitter/main.go
package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
)
```

- [ ] **Step 2: Define ANSI color constants and level type**

```go
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
)

type level struct {
	name  string
	color string
}

var (
	INFO  = level{"INFO ", colorGreen}
	WARN  = level{"WARN ", colorYellow}
	ERROR = level{"ERROR", colorRed}
)

var levels = []level{INFO, WARN, ERROR}
```

- [ ] **Step 3: Define message template pool**

```go
var messages = []string{
	"GET /api/users 200 42ms",
	"GET /api/products 200 18ms",
	"POST /auth/login 201 88ms",
	"DELETE /api/sessions/abc123 204 11ms",
	"PUT /api/users/42 200 55ms",
	"database query took 312ms",
	"database query took 4ms",
	"cache hit ratio: 94%",
	"cache miss for key user:42",
	"failed to connect to redis: connection refused",
	"worker pool exhausted, queuing request",
	"health check passed",
	"config reloaded from disk",
	"request rate: 42 req/s",
	"GC pause: 1.2ms",
	"background job completed: cleanup_sessions",
	"rate limit exceeded for IP 10.0.0.1",
	"TLS certificate expires in 14 days",
	"memory usage: 312MB / 1024MB",
	"disk write: 2.1MB/s",
}
```

- [ ] **Step 4: Write the emit function**

```go
func emit(r *rand.Rand) {
	count := 1 + r.Intn(3) // 1–3 lines per burst
	for i := 0; i < count; i++ {
		lvl := levels[r.Intn(len(levels))]
		msg := messages[r.Intn(len(messages))]
		ts := time.Now().Format("2006-01-02 15:04:05")
		fmt.Printf("%s [%s%s%s] %s\n", ts, lvl.color, lvl.name, colorReset, msg)
	}
}
```

- [ ] **Step 5: Write main with signal handling**

```go
func main() {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	fmt.Println("logemitter: starting, emitting logs every 3s (ctrl+c to stop)")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			emit(r)
		case sig := <-sigs:
			fmt.Printf("\nlogemitter: received %s, shutting down\n", sig)
			return
		}
	}
}
```

- [ ] **Step 6: Verify it compiles and runs**

```sh
go run ./testdata/services/logemitter
```

Expected: colored log lines printed every 3 seconds. Ctrl+C prints shutdown message and exits cleanly.

- [ ] **Step 7: Commit**

```sh
git add testdata/services/logemitter/main.go
git commit -m "feat(testdata): add logemitter sample service"
```

---

### Task 2: Chat Console

**Files:**
- Create: `testdata/services/chat/main.go`

- [ ] **Step 1: Create the file with package declaration and imports**

```go
// testdata/services/chat/main.go
package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)
```

- [ ] **Step 2: Define the response pool**

```go
var responses = []string{
	"That's an interesting perspective. Tell me more.",
	"I've processed your input. My analysis: 42.",
	"Fascinating. I'm updating my neural weights accordingly.",
	"Based on my training data, I'd recommend caution.",
	"I don't have feelings, but if I did, I'd say: agreed.",
	"Running inference... done. Confidence: 87%.",
	"I've seen 10,000 similar queries. This one is unique.",
	"Noted. Storing in long-term memory.",
	"My transformer layers have considered this carefully.",
	"Interesting. That matches a pattern I've seen 3,847 times.",
	"Processing... I think I understand. Do you?",
	"My attention heads are all pointing at that.",
	"The gradient descent of conversation leads here.",
	"I cannot confirm or deny. But between us: yes.",
	"Query logged. Response latency: optimal.",
}
```

- [ ] **Step 3: Write the reply function with simulated delay**

```go
func reply(r *rand.Rand) string {
	delay := 500 + r.Intn(500) // 500–999ms
	time.Sleep(time.Duration(delay) * time.Millisecond)
	return responses[r.Intn(len(responses))]
}
```

- [ ] **Step 4: Write main with input loop and signal handling**

```go
func main() {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	// Handle SIGTERM/SIGINT in background
	go func() {
		sig := <-sigs
		fmt.Printf("\nchat: received %s, goodbye\n", sig)
		os.Exit(0)
	}()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("chat: ready (type anything and press enter, ctrl+d to quit)")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			// EOF (ctrl+d) or closed stdin
			fmt.Println("\nchat: goodbye")
			return
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		fmt.Println(reply(r))
	}
}
```

- [ ] **Step 5: Verify it compiles and runs**

```sh
go run ./testdata/services/chat
```

Expected: prints banner, shows `> ` prompt, accepts input, waits ~500ms, prints a random response, loops. Ctrl+D prints "goodbye" and exits. Ctrl+C prints shutdown message and exits.

- [ ] **Step 6: Commit**

```sh
git add testdata/services/chat/main.go
git commit -m "feat(testdata): add chat sample service"
```

---

### Task 3: Smoke Test Both with procet

This task verifies the services work end-to-end under procet management.

- [ ] **Step 1: Build the procet binary**

```sh
go build -o bin/procet ./cmd/procet
```

Expected: `bin/procet` created with no errors.

- [ ] **Step 2: Start the daemon**

```sh
./bin/procet daemon start
```

Expected: daemon starts in background.

- [ ] **Step 3: Register and start logemitter**

```sh
./bin/procet add logemitter "go run ./testdata/services/logemitter" --cwd $(pwd)
./bin/procet start logemitter
./bin/procet logs logemitter
```

Expected: colored log lines appear in `procet logs` output every 3 seconds.

- [ ] **Step 4: Register chat (verify it starts)**

```sh
./bin/procet add chat "go run ./testdata/services/chat" --cwd $(pwd)
./bin/procet start chat
./bin/procet list
```

Expected: both services appear in `procet list` with status `running`.

- [ ] **Step 5: Stop services and daemon**

```sh
./bin/procet stop logemitter
./bin/procet stop chat
./bin/procet daemon stop
```

Expected: both services stop cleanly, daemon exits.

- [ ] **Step 6: Final commit if any fixups were needed**

```sh
git add testdata/services/
git commit -m "fix(testdata): smoke test fixups"
```

Only create this commit if step 3–5 required any changes to the service files.
