package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"web", "api-v2", "worker_1", "db.primary", "A", strings.Repeat("x", 64)} {
		assert.NoError(t, ValidateName("service", ok), ok)
	}
	for _, bad := range []string{"", "../x", "a/b", `a\b`, ".hidden", "-flag", "_x", "has space", "tab\there", "ünï", strings.Repeat("x", 65), "a\x00b"} {
		err := ValidateName("service", bad)
		if assert.Error(t, err, "%q", bad) {
			assert.Contains(t, err.Error(), "invalid service name")
		}
	}
	assert.Contains(t, ValidateName("target", "a b").Error(), "invalid target name")
}

// Every name ValidateName accepts must be file-safe — the strict rule is a
// subset of the load-bearing one.
func TestValidNamesAreSafeFileNames(t *testing.T) {
	for _, n := range []string{"web", "a.b", "x_y-z", strings.Repeat("9", 64)} {
		assert.NoError(t, ValidateName("service", n))
		assert.True(t, SafeFileName(n), n)
	}
}

func TestSafeFileName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "../x", "a/b", "/abs", `a\b`, "a\x00b"} {
		assert.False(t, SafeFileName(bad), "%q", bad)
	}
	// Looser than ValidateName on purpose: names that predate the rule and
	// cannot escape a directory still work.
	for _, ok := range []string{"my service", ".hidden", "ünï", "api:v2"} {
		assert.True(t, SafeFileName(ok), "%q", ok)
		assert.Equal(t, filepath.Join("/logs", ok+".log"), filepath.Clean(filepath.Join("/logs", ok+".log")),
			"%q stays inside the directory", ok)
	}
}
