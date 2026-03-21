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

func reply(r *rand.Rand) string {
	delay := 500 + r.Intn(500) // 500–999ms
	time.Sleep(time.Duration(delay) * time.Millisecond)
	return responses[r.Intn(len(responses))]
}

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
