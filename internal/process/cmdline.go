package process

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// CommandLine returns the argv of a running process as one space-joined string,
// or "" when it cannot be read — the process is gone, or is not ours to inspect.
//
// It exists so a pid recorded on disk can be checked against what is running
// there now. kill(pid, 0) only reports that *something* is alive at that
// number; after a reboot or pid wraparound that something is a stranger, and
// acting on it — signalling it, reporting it as a devrun process — is worse
// than concluding the original is gone.
//
// "" therefore means "cannot confirm", and callers deciding whether to signal a
// process should treat it as a no.
func CommandLine(pid int) string {
	if pid <= 0 {
		return ""
	}
	if runtime.GOOS == "darwin" {
		// As with lsof and ps elsewhere here, read the output rather than trust
		// the status: ps exits non-zero for a pid that has just gone.
		// -ww: without it ps may cut the line to the terminal width, and a
		// truncated argv reads as a different process to anything matching on
		// it.
		out, err := exec.Command("ps", "-ww", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	// argv there is NUL-separated with a trailing NUL. A kernel thread has an
	// empty cmdline, which correctly reads as "cannot confirm".
	return strings.TrimSpace(strings.ReplaceAll(strings.TrimRight(string(data), "\x00"), "\x00", " "))
}
