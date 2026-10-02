package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A wildcard bind is announced as 0.0.0.0:<port> — where it listens, but not
// somewhere a person can go. The real address stays untouched everywhere else:
// Posture reads it, and rewriting it to loopback would report a wildcard bind
// as local.
func TestDisplayHost(t *testing.T) {
	for addr, want := range map[string]string{
		"0.0.0.0:7788":   "localhost:7788",
		"[::]:7788":      "localhost:7788",
		"127.0.0.1:7788": "127.0.0.1:7788",
		"192.168.1.8:80": "192.168.1.8:80",
		"localhost:7788": "localhost:7788",
		"garbage":        "garbage",
	} {
		assert.Equalf(t, want, displayHost(addr), "%q", addr)
	}
}
