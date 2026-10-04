// Package cloudflared drives the cloudflared binary. It is deliberately not a
// provider abstraction: there is one provider, and inventing an interface for
// it would be guessing at the shape of a second.
//
// Nothing here imports cloudflared as a library, so it stays out of go.mod and
// out of the binary. If you never publish, it is never even looked for.
package cloudflared

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Binary is the executable devrun shells out to.
const Binary = "cloudflared"

// ErrNotInstalled is returned by Find when the binary is not on PATH.
var ErrNotInstalled = errors.New("cloudflared is not installed")

// NotInstalledHelp is the guidance that goes with ErrNotInstalled. It names
// both ways forward, because installing cloudflared is only one of them:
// publishing is optional, and the gateway is an ordinary HTTP server that
// anything can front.
const NotInstalledHelp = `cloudflared is not installed.

Either install it and let devrun run the tunnel:
    brew install cloudflared          (or see developers.cloudflare.com)

Or publish the gateway yourself — ngrok, tailscale funnel, an SSH remote
forward, a Caddy you already run — and tell devrun it is published, so the
allowlist and token apply:
    gateway:
      posture: published`

// Find locates the cloudflared binary. The error names both forks rather than
// assuming the user wants to install anything.
func Find() (string, error) {
	path, err := exec.LookPath(Binary)
	if err != nil {
		return "", fmt.Errorf("%w\n\n%s", ErrNotInstalled, NotInstalledHelp)
	}
	return path, nil
}

// listTimeout bounds the tunnel list call. It reaches Cloudflare's API, so it
// can hang on a bad network; a name check is not worth waiting on.
const listTimeout = 10 * time.Second

// tunnelRow is one entry of `cloudflared tunnel list --output json`. The
// command returns {id, name, connections} and notably **no DNS routes**, which
// is why devrun cannot derive a hostname from a tunnel name.
type tunnelRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// List returns the names of the account's tunnels.
//
// It makes an authenticated API call using ~/.cloudflared/cert.pem, so it
// fails for anyone who has not run `cloudflared tunnel login`. That is not a
// reason to block: callers use this to catch a typo before cloudflared does,
// and an unverifiable name should go through unchecked rather than stop a
// publish. Every failure is reported as an error for the caller to degrade on.
func List(ctx context.Context, bin string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "tunnel", "list", "--output", "json").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("cloudflared tunnel list: %s", FirstLine(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("cloudflared tunnel list: %w", err)
	}

	rows, err := parseTunnelRows(out)
	if err != nil {
		return nil, fmt.Errorf("cloudflared tunnel list: %w", err)
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	return names, nil
}

// parseTunnelRows reads the tunnel array out of what cloudflared printed.
//
// The whole output first, because that is what the real binary gives:
// --output json puts the array on stdout and its logs on stderr, and Output()
// captures stdout alone. The fallback is for a release that prefixes it
// anyway, and is line-oriented on purpose — scanning for the first '[' byte
// would happily start inside a bracket in a log line and then fail to parse,
// which is the case the tolerance exists for.
func parseTunnelRows(out []byte) ([]tunnelRow, error) {
	var rows []tunnelRow
	if err := json.Unmarshal(out, &rows); err == nil {
		return rows, nil
	}
	// The offset is carried, not searched for. Looking the line up with
	// bytes.Index would find its first occurrence — and a one-character line
	// of "[" occurs inside "[core]" in the preamble, which is the very bug
	// this fallback replaced.
	offset := 0
	for _, line := range bytes.Split(out, []byte("\n")) {
		if bytes.HasPrefix(bytes.TrimSpace(line), []byte("[")) {
			start := offset + bytes.IndexByte(line, '[')
			if err := json.Unmarshal(out[start:], &rows); err == nil {
				return rows, nil
			}
			break
		}
		offset += len(line) + 1 // the newline Split consumed
	}
	return nil, errors.New("unreadable output")
}

// HasTunnel reports whether name is one of the account's tunnels, and whether
// the question could be answered at all. A false "known" means unverified —
// not absent.
func HasTunnel(ctx context.Context, bin, name string) (found, known bool) {
	names, err := List(ctx, bin)
	if err != nil {
		return false, false
	}
	for _, n := range names {
		if n == name {
			return true, true
		}
	}
	return false, true
}

// FirstLine is the opening non-blank line of some output — where a failing
// process says why, ahead of any stack or fallout. Shared because the daemon
// reads cloudflared's log and the gateway child's stderr the same way.
func FirstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			return l
		}
	}
	return strings.TrimSpace(s)
}
