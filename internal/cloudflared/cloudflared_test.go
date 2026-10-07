package cloudflared

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fake writes a stand-in cloudflared that prints what the test wants and exits
// with the given status. Nothing in this package's tests may reach the real
// binary: it makes authenticated calls to someone's Cloudflare account.
func fake(t *testing.T, stdout, stderr string, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	path := filepath.Join(t.TempDir(), "cloudflared")
	script := "#!/bin/sh\n"
	if stdout != "" {
		script += "cat <<'EOF'\n" + stdout + "\nEOF\n"
	}
	if stderr != "" {
		script += "cat >&2 <<'EOF'\n" + stderr + "\nEOF\n"
	}
	script += "exit " + strconv.Itoa(code) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))
	return path
}

const listJSON = `[
  {"id":"1ca7edfd-5206-4888-8f01-c419ffb2270e","name":"shop","connections":[]},
  {"id":"11f84736-3e49-40b4-b4df-066bddc198f2","name":"docs","connections":[]}
]`

func TestList(t *testing.T) {
	names, err := List(context.Background(), fake(t, listJSON, "", 0))
	require.NoError(t, err)
	assert.Equal(t, []string{"shop", "docs"}, names)
}

// cloudflared logs to stderr and has been known to put a line on stdout ahead
// of the payload, so the array is found rather than assumed to start at byte 0.
func TestList_IgnoresNoiseBeforeTheJSON(t *testing.T) {
	noisy := "2026-10-04T00:00:00Z INF some banner\n" + listJSON
	names, err := List(context.Background(), fake(t, noisy, "", 0))
	require.NoError(t, err)
	assert.Equal(t, []string{"shop", "docs"}, names)
}

func TestList_EmptyAccount(t *testing.T) {
	names, err := List(context.Background(), fake(t, "[]", "", 0))
	require.NoError(t, err)
	assert.Empty(t, names)
}

// The call is authenticated with cert.pem, so it fails for anyone who has not
// run `cloudflared tunnel login`. The error carries what cloudflared said.
func TestList_ReportsWhyItFailed(t *testing.T) {
	_, err := List(context.Background(), fake(t, "", "Error: cannot determine default origin certificate path", 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "origin certificate")
}

func TestList_UnreadableOutput(t *testing.T) {
	_, err := List(context.Background(), fake(t, "not json at all", "", 0))
	assert.ErrorContains(t, err, "unreadable output")
}

func TestList_MissingBinary(t *testing.T) {
	_, err := List(context.Background(), filepath.Join(t.TempDir(), "nope"))
	assert.Error(t, err)
}

// Unverified is not the same answer as absent: a name that cannot be checked
// must go through rather than block a publish.
func TestHasTunnel(t *testing.T) {
	bin := fake(t, listJSON, "", 0)

	found, known := HasTunnel(context.Background(), bin, "docs")
	assert.True(t, found)
	assert.True(t, known)

	found, known = HasTunnel(context.Background(), bin, "typo")
	assert.False(t, found)
	assert.True(t, known, "the account was readable; this name is genuinely absent")

	found, known = HasTunnel(context.Background(), fake(t, "", "not logged in", 1), "docs")
	assert.False(t, found)
	assert.False(t, known, "unverifiable, which callers must not read as absent")
}

// The error has to name both ways forward: installing cloudflared is only one
// of them, since the gateway is an ordinary HTTP server anything can front.
func TestFind_MissingNamesBothForks(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Find()
	require.ErrorIs(t, err, ErrNotInstalled)
	assert.Contains(t, err.Error(), "brew install cloudflared")
	assert.Contains(t, err.Error(), "posture: published")
}

func TestFind_OnPath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, Binary), []byte("#!/bin/sh\n"), 0o700))
	t.Setenv("PATH", dir)

	got, err := Find()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, Binary), got)
}

// Tolerance for a preamble has to survive a bracket in it. Scanning for the
// first '[' byte would start inside the log line and fail to parse, which is
// the case the tolerance exists for.
func TestList_PreambleContainingBrackets(t *testing.T) {
	noisy := "2026-10-04T00:00:00Z INF [core] starting up [v2026.8.3]\n" + listJSON
	names, err := List(context.Background(), fake(t, noisy, "", 0))
	require.NoError(t, err)
	assert.Equal(t, []string{"shop", "docs"}, names)
}

// And genuinely unreadable output is still an error, not an empty list.
func TestList_BracketsButNoArray(t *testing.T) {
	_, err := List(context.Background(), fake(t, "INF [core] nothing to report", "", 0))
	assert.ErrorContains(t, err, "unreadable output")
}

// A log line of its own that starts with a bracket must not end the search
// while the array is still to come.
func TestList_BracketStartingLineBeforeTheArray(t *testing.T) {
	noisy := "[core] starting up\n[warn] something\n" + listJSON
	names, err := List(context.Background(), fake(t, noisy, "", 0))
	require.NoError(t, err)
	assert.Equal(t, []string{"shop", "docs"}, names)
}
