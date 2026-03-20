package process_test

import (
	"os"
	"testing"

	"github.com/hailerity/procet/internal/process"
	"github.com/stretchr/testify/assert"
)

func TestStats_SelfProcess(t *testing.T) {
	pid := os.Getpid()
	// Our own process definitely has memory
	mem, err := process.MemBytes(pid)
	assert.NoError(t, err)
	assert.Greater(t, mem, int64(0))

	// CPU% is non-negative (may be 0 for idle process)
	cpu, err := process.CPUPercent(pid)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, cpu, float64(0))
}
