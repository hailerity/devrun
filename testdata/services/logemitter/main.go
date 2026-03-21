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

func emit(r *rand.Rand) {
	count := 1 + r.Intn(3) // 1–3 lines per burst
	for i := 0; i < count; i++ {
		lvl := levels[r.Intn(len(levels))]
		msg := messages[r.Intn(len(messages))]
		ts := time.Now().Format("2006-01-02 15:04:05")
		fmt.Printf("%s [%s%s%s] %s\n", ts, lvl.color, lvl.name, colorReset, msg)
	}
}

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
