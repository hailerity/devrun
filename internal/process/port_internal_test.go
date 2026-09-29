package process

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPPIDFromStat(t *testing.T) {
	// Field 2 is the executable name in parentheses and is not escaped, so a
	// name containing spaces or its own brackets shifts every later field.
	assert.Equal(t, 1, mustPPID(t, "42 (node) S 1 42 42 0 -1 4194304 0"))
	assert.Equal(t, 7, mustPPID(t, "42 (my app (dev)) S 7 42 42 0 -1 0 0"))
	assert.Equal(t, 3, mustPPID(t, "42 (weird )name) R 3 42 42"))

	_, ok := ppidFromStat("no parens here")
	assert.False(t, ok)
	_, ok = ppidFromStat("42 (node) S")
	assert.False(t, ok, "truncated line has no ppid field")
}

func mustPPID(t *testing.T, s string) int {
	t.Helper()
	ppid, ok := ppidFromStat(s)
	assert.True(t, ok, "expected a ppid from %q", s)
	return ppid
}

func TestSocketInode(t *testing.T) {
	inode, ok := socketInode("socket:[151147745]")
	assert.True(t, ok)
	assert.Equal(t, "151147745", inode)

	for _, notASocket := range []string{"/dev/null", "pipe:[123]", "socket:[123", "anon_inode:[eventfd]"} {
		_, ok := socketInode(notASocket)
		assert.Falsef(t, ok, "%q is not a socket fd", notASocket)
	}
}

func TestPortFromAddr(t *testing.T) {
	for addr, want := range map[string]int{
		"*:3000":         3000,
		"127.0.0.1:7411": 7411,
		"[::1]:7412":     7412,
		"[::]:80":        80,
	} {
		got, ok := portFromAddr(addr)
		assert.Truef(t, ok, "expected a port from %q", addr)
		assert.Equal(t, want, got, addr)
	}

	for _, bad := range []string{"", "no-colon", "*:0", "*:70000", "*:notaport"} {
		_, ok := portFromAddr(bad)
		assert.Falsef(t, ok, "%q should not yield a port", bad)
	}
}

func TestDescendants(t *testing.T) {
	// 1 -> 2 -> 4, 1 -> 3. A service's listener is often a grandchild.
	children := map[int][]int{1: {2, 3}, 2: {4}, 9: {10}}
	assert.Equal(t, []int{1, 2, 3, 4}, descendants(1, children))
	assert.Equal(t, []int{2, 4}, descendants(2, children))
	assert.Equal(t, []int{5}, descendants(5, children), "a leaf is just itself")
}

func TestDescendants_CycleDoesNotHang(t *testing.T) {
	// The process table is read unlocked and can be self-inconsistent.
	assert.Equal(t, []int{1, 2}, descendants(1, map[int][]int{1: {2}, 2: {1}}))
}
