package ops

import (
	"testing"
	"time"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverall_RunningOnlyWhenEveryMemberIs(t *testing.T) {
	of := func(os ...Outcome) Outcome {
		var s []ServiceOutcome
		for _, o := range os {
			s = append(s, ServiceOutcome{Outcome: o})
		}
		return overall(s)
	}
	assert.Equal(t, OutcomeRunning, of(OutcomeRunning, OutcomeAlreadyRunning))
	assert.Equal(t, OutcomeAlreadyRunning, of(OutcomeAlreadyRunning, OutcomeAlreadyRunning))
	assert.Equal(t, OutcomeCrashed, of(OutcomeRunning, OutcomeCrashed, OutcomeTimeout))
	assert.Equal(t, OutcomeFailed, of(OutcomeCrashed, OutcomeFailed))
	assert.Equal(t, OutcomeTimeout, of(OutcomeRunning, OutcomeTimeout, OutcomeExited))
	assert.Equal(t, OutcomeExited, of(OutcomeRunning, OutcomeExited))
}

func TestOutcome_OK(t *testing.T) {
	for _, o := range []Outcome{OutcomeRunning, OutcomeAlreadyRunning, OutcomeStarted} {
		assert.True(t, o.OK(), o)
	}
	for _, o := range []Outcome{OutcomeCrashed, OutcomeExited, OutcomeStopped, OutcomeFailed, OutcomeTimeout} {
		assert.False(t, o.OK(), o)
	}
}

// spawnedSince reads the StartedAt the daemon records on every spawn: before
// the request → not ours; at or after it → ours. No state at all → not ours.
func TestSpawnedSince_UsesTheRecordedStartTime(t *testing.T) {
	sandbox(t)
	t0 := time.Now()
	assert.False(t, spawnedSince("web", t0), "never started")

	earlier, later := t0.Add(-time.Second), t0.Add(time.Millisecond)
	require.NoError(t, config.SaveState(config.StatePath(), &config.State{Services: map[string]*config.ServiceState{
		"old": {Status: config.StatusCrashed, StartedAt: &earlier},
		"new": {Status: config.StatusCrashed, StartedAt: &later},
	}}))
	assert.False(t, spawnedSince("old", t0), "an old crash is not this start's")
	assert.True(t, spawnedSince("new", t0))
}

func TestStartAndWait_RejectsBadRequestsBeforeTouchingTheDaemon(t *testing.T) {
	work := sandbox(t)
	r, err := Resolve(Scope{Dir: work})
	require.NoError(t, err)

	_, err = StartAndWait(r, "", "", WaitOptions{})
	assert.EqualError(t, err, "give exactly one of a service or a target")
	_, err = StartAndWait(r, "a", "b", WaitOptions{})
	assert.EqualError(t, err, "give exactly one of a service or a target")

	_, err = StartAndWait(r, "nope", "", WaitOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `service "nope" not found in `+config.RegistryPath()+" (global scope); it defines no services")

	_, err = StartAndWait(r, "", "nope", WaitOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "it defines no targets")
}
