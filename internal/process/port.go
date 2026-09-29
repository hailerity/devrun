package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// DetectPort returns the lowest TCP port a service is listening on, or 0 when it
// is not listening yet. See DetectPorts, which it wraps.
func DetectPort(pid int) (int, error) {
	return DetectPorts([]int{pid})[pid], nil
}

// DetectPorts resolves the listening port of several services at once, keyed by
// the pid devrun tracks. Services with no listener are absent from the result.
//
// Two things make this harder than asking the OS about one pid:
//
// A service's own process usually holds no socket. Commands are spawned as
// `sh -c` (see hupShield), and a `yarn dev` or `npm start` leaves the listener
// in a grandchild, so the whole process tree has to be considered.
//
// And both platforms have a trap that answers about the wrong process rather
// than failing: lsof ORs its selectors unless given -a, and /proc/<pid>/net/tcp
// is per network namespace, listing every socket on the host. Either one returns
// a plausible port belonging to something else entirely.
//
// A service listening on several ports — an app port plus a debugger — has no
// single right answer; the lowest is returned so the value at least stays stable
// between polls. Declare the port in config when that guess is wrong.
func DetectPorts(pids []int) map[int]int {
	if len(pids) == 0 {
		return map[int]int{}
	}

	children := childMap()
	trees := make(map[int][]int, len(pids))
	seen := map[int]bool{}
	for _, pid := range pids {
		tree := descendants(pid, children)
		trees[pid] = tree
		for _, p := range tree {
			seen[p] = true
		}
	}

	all := make([]int, 0, len(seen))
	for p := range seen {
		all = append(all, p)
	}
	sort.Ints(all)

	byPid := listenPortsByPid(all)
	out := make(map[int]int, len(pids))
	for _, pid := range pids {
		var ports []int
		for _, p := range trees[pid] {
			ports = append(ports, byPid[p]...)
		}
		if len(ports) == 0 {
			continue
		}
		sort.Ints(ports)
		out[pid] = ports[0]
	}
	return out
}

func listenPortsByPid(pids []int) map[int][]int {
	if runtime.GOOS == "darwin" {
		return listenPortsLsof(pids)
	}
	return listenPortsProc(pids)
}

// listenPortsLsof asks lsof about exactly these pids.
//
// -a is load-bearing: lsof ORs its selection options, so `lsof -p PID -i` means
// "files of this pid, or any internet file" and answers for the whole machine.
// Without it the first LISTEN line belongs to whatever unrelated process sorts
// first — on a Mac, typically ControlCenter on :7000.
//
// -F asks for field output instead of columns, because the columnar layout
// shifts with the selection flags (-g adds a PGID column, moving NAME).
func listenPortsLsof(pids []int) map[int][]int {
	csv := make([]string, len(pids))
	for i, p := range pids {
		csv[i] = strconv.Itoa(p)
	}
	out, err := exec.Command("lsof",
		"-a", "-p", strings.Join(csv, ","),
		"-i", "-n", "-P", "-FpnT",
	).Output()
	if err != nil {
		// lsof exits non-zero when nothing matches, which is not an error here.
		return map[int][]int{}
	}
	return ParseLsofListenPorts(string(out))
}

// ParseLsofListenPorts reads `lsof -FpnT` field output and returns the ports each
// pid is listening on. Records are one field per line, tagged by their first
// byte: p a pid, f a new file, n its name, T a TCP state such as TST=LISTEN.
func ParseLsofListenPorts(out string) map[int][]int {
	ports := map[int][]int{}
	pid, name, listening := 0, "", false

	flush := func() {
		if pid != 0 && listening && name != "" {
			if p, ok := portFromAddr(name); ok {
				ports[pid] = append(ports[pid], p)
			}
		}
		name, listening = "", false
	}

	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			flush()
			if v, err := strconv.Atoi(line[1:]); err == nil {
				pid = v
			} else {
				pid = 0
			}
		case 'f':
			flush()
		case 'n':
			name = line[1:]
		case 'T':
			if line[1:] == "ST=LISTEN" {
				listening = true
			}
		}
	}
	flush()
	return ports
}

// portFromAddr takes the port off an lsof NAME field: "*:3000", "127.0.0.1:3000"
// or "[::1]:3000".
func portFromAddr(s string) (int, bool) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return 0, false
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p <= 0 || p > 65535 {
		return 0, false
	}
	return p, true
}

// listenPortsProc resolves ports on Linux by socket inode.
//
// /proc/<pid>/net/tcp is not the process's sockets — it is the whole network
// namespace, byte-identical for every process in it. The inodes held in
// /proc/<pid>/fd are what tie a row back to the process that owns it.
func listenPortsProc(pids []int) map[int][]int {
	owner := socketInodeOwners(pids)
	if len(owner) == 0 {
		return map[int][]int{}
	}
	ports := map[int][]int{}
	// tcp6 matters as much as tcp: a service bound to :: only appears there.
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for inode, port := range ParseProcNetTCPListen(string(data)) {
			if pid, ok := owner[inode]; ok {
				ports[pid] = append(ports[pid], port)
			}
		}
	}
	return ports
}

// socketInodeOwners maps each socket inode held by these pids back to its pid.
func socketInodeOwners(pids []int) map[string]int {
	owner := map[string]int{}
	for _, pid := range pids {
		dir := fmt.Sprintf("/proc/%d/fd", pid)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			target, err := os.Readlink(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			if inode, ok := socketInode(target); ok {
				owner[inode] = pid
			}
		}
	}
	return owner
}

// socketInode pulls 12345 out of an fd symlink reading "socket:[12345]".
func socketInode(target string) (string, bool) {
	const prefix = "socket:["
	if !strings.HasPrefix(target, prefix) || !strings.HasSuffix(target, "]") {
		return "", false
	}
	return target[len(prefix) : len(target)-1], true
}

// ParseProcNetTCPListen returns the listening port of every row in a
// /proc/net/tcp or tcp6 table, keyed by socket inode. Columns are sl,
// local_address, rem_address, st, ... with inode tenth; local_address is
// hex ADDRESS:PORT and state 0A is TCP_LISTEN.
func ParseProcNetTCPListen(content string) map[string]int {
	ports := map[string]int{}
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return ports
	}
	for _, line := range lines[1:] { // skip the header
		f := strings.Fields(line)
		if len(f) < 10 || f[3] != "0A" {
			continue
		}
		i := strings.LastIndex(f[1], ":")
		if i < 0 {
			continue
		}
		port, err := strconv.ParseInt(f[1][i+1:], 16, 32)
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		ports[f[9]] = int(port)
	}
	return ports
}

// descendants returns pid followed by every process below it.
func descendants(pid int, children map[int][]int) []int {
	seen := map[int]bool{pid: true}
	queue := []int{pid}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			if !seen[c] {
				seen[c] = true
				queue = append(queue, c)
			}
		}
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// childMap reads the whole process table once and indexes it by parent. Building
// it per call would put one `ps` behind the supervisor lock for every service the
// port poller walks, which is why DetectPorts takes a batch.
func childMap() map[int][]int {
	if runtime.GOOS == "darwin" {
		return childMapPS()
	}
	return childMapProc()
}

func childMapPS() map[int][]int {
	m := map[int][]int{}
	out, err := exec.Command("ps", "-ax", "-o", "pid=,ppid=").Output()
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		m[ppid] = append(m[ppid], pid)
	}
	return m
}

func childMapProc() map[int][]int {
	m := map[int][]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return m
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		if ppid, ok := ppidFromStat(string(data)); ok {
			m[ppid] = append(m[ppid], pid)
		}
	}
	return m
}

// ppidFromStat pulls the parent pid out of /proc/<pid>/stat. The second field is
// the executable name in parentheses and may itself contain spaces and brackets,
// so the scan starts after the last ')': state is then first and ppid second.
func ppidFromStat(s string) (int, bool) {
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(f[1])
	return ppid, err == nil
}
