package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The config is written before the daemon is asked, so a daemon failure is
// not "nothing happened". The directions are not symmetric: failing to
// publish is harmless, while failing to withhold leaves a service the file
// no longer allows still reachable from outside.
func TestExposureMismatch(t *testing.T) {
	publishing := exposureMismatch([]string{"web"}, true)
	assert.Contains(t, publishing, "Saved to the config")
	assert.Contains(t, publishing, "not published yet")

	withholding := exposureMismatch([]string{"web", "api"}, false)
	assert.Contains(t, withholding, "Saved to the config")
	assert.Contains(t, withholding, "web, api")
	assert.Contains(t, withholding, "MAY STILL BE PUBLISHED",
		"the dangerous direction has to be unmissable")
	assert.Contains(t, withholding, "gateway down", "and say what to do about it")
}
