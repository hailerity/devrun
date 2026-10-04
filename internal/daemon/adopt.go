package daemon

import (
	"path/filepath"
	"strings"

	"github.com/hailerity/devrun/internal/config"
	"github.com/hailerity/devrun/internal/process"
)

// recogniseService reports whether the pid recorded for a service is still
// running what devrun started there.
//
// kill(pid, 0) alone says only that *something* holds that number. After a
// reboot or pid wraparound that something is a stranger, and devrun would
// then list it as the service, report its CPU and memory, and send it SIGTERM
// on `devrun stop` — the same hazard the gateway and the tunnel check argv to
// avoid.
//
// Only a definite mismatch is a no. An argv that cannot be read comes back
// true, because the two mistakes are not equally priced: adopting a stranger
// is bad, but reporting a running dev server as crashed invites the user to
// start a second copy of it, and two servers fighting over one port is worse
// and harder to see.
//
// The comparison is the program name alone, not the command. Services are
// spawned as `sh -c "trap ” HUP\n<command>"`, and a shell given a simple
// command exec-replaces itself with it — measured: a service recorded as
// "sleep 34 # …" shows an argv of exactly "sleep 34". So the only part of a
// recorded command reliably present in the live argv is the program it runs.
//
// That makes this a weak test, and deliberately so: it rejects the stranger
// that pid reuse actually produces — an unrelated binary — while never
// rejecting a service that is genuinely up. Two devrun services both running
// npm would not be told apart, which is the same outcome as today rather
// than a new failure. Comparing process start times against StartedAt would
// discriminate properly and is the obvious next step if that is ever worth
// the per-platform code.
func recogniseService(reg *config.Registry) func(string, int) bool {
	return func(name string, pid int) bool {
		if reg == nil || reg.Services[name] == nil {
			return true // nothing recorded to compare against
		}
		want := programName(reg.Services[name].Command)
		if want == "" {
			return true
		}
		argv := process.CommandLine(pid)
		if argv == "" {
			return true // cannot tell, so do not guess against the user
		}
		return strings.Contains(argv, want)
	}
}

// programName is the executable a command runs, reduced to its base name so
// a recorded "./scripts/dev.sh" still matches an argv showing the absolute
// path. Empty when there is nothing to take.
func programName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}
