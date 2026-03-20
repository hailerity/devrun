package process

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// DetectPort returns the first TCP LISTEN port for the given PID.
// Returns 0 and no error if the process has no listening port yet.
func DetectPort(pid int) (int, error) {
	if runtime.GOOS == "darwin" {
		return detectPortMacOS(pid)
	}
	return detectPortLinux(pid)
}

func detectPortMacOS(pid int) (int, error) {
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-i", "-n", "-P").Output()
	if err != nil {
		// lsof exits non-zero if the process has no network connections; treat as no port
		return 0, nil
	}
	port, err := ParseLsofPort(string(out))
	if err != nil {
		return 0, nil // no LISTEN line yet
	}
	return port, nil
}

func detectPortLinux(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/net/tcp", pid))
	if err != nil {
		return 0, nil
	}
	port, err := ParseProcNetTCP(string(data))
	if err != nil {
		return 0, nil
	}
	return port, nil
}

// ParseLsofPort parses lsof -i output and returns the first LISTEN port.
func ParseLsofPort(output string) (int, error) {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "LISTEN") {
			continue
		}
		// NAME column looks like "*:3000" or "0.0.0.0:3000"
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		name := fields[8]
		if idx := strings.LastIndex(name, ":"); idx >= 0 {
			portStr := name[idx+1:]
			if p, err := strconv.Atoi(portStr); err == nil {
				return p, nil
			}
		}
	}
	return 0, fmt.Errorf("no LISTEN port found in lsof output")
}

// ParseProcNetTCP parses /proc/[pid]/net/tcp and returns the first LISTEN port.
// Lines have local_address in hex: HHHHHHHH:PPPP where PPPP is the port in hex.
// State 0A = TCP_LISTEN.
func ParseProcNetTCP(content string) (int, error) {
	lines := strings.Split(content, "\n")
	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		state := fields[3]
		if state != "0A" { // 0A = LISTEN
			continue
		}
		addr := fields[1] // e.g. "00000000:0BB8"
		parts := strings.Split(addr, ":")
		if len(parts) != 2 {
			continue
		}
		port64, err := strconv.ParseInt(parts[1], 16, 32)
		if err != nil {
			continue
		}
		return int(port64), nil
	}
	return 0, fmt.Errorf("no LISTEN port found in /proc/net/tcp")
}
