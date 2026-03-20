package process

import (
	"fmt"

	gops "github.com/shirou/gopsutil/v3/process"
)

// CPUPercent returns the CPU usage percentage for the given PID.
// Returns 0 if the process is not found or measurement fails.
func CPUPercent(pid int) (float64, error) {
	p, err := gops.NewProcess(int32(pid))
	if err != nil {
		return 0, fmt.Errorf("get process %d: %w", pid, err)
	}
	pct, err := p.CPUPercent()
	if err != nil {
		return 0, fmt.Errorf("cpu percent for %d: %w", pid, err)
	}
	return pct, nil
}

// MemBytes returns the RSS memory usage in bytes for the given PID.
func MemBytes(pid int) (int64, error) {
	p, err := gops.NewProcess(int32(pid))
	if err != nil {
		return 0, fmt.Errorf("get process %d: %w", pid, err)
	}
	info, err := p.MemoryInfo()
	if err != nil {
		return 0, fmt.Errorf("memory info for %d: %w", pid, err)
	}
	return int64(info.RSS), nil
}
