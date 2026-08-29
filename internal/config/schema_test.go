package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ServiceConfig travels as JSON over the IPC socket and in the daemon re-exec
// handoff. Pin the wire field names so a future yaml-tag rename can't silently
// change them.
func TestServiceConfigJSONFieldNames(t *testing.T) {
	cfg := ServiceConfig{
		Name:    "web",
		Command: "yarn dev",
		CWD:     "/srv/app",
		Group:   "fullstack",
		Env:     map[string]string{"PORT": "3000"},
		Desc:    "frontend",
	}

	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))

	for _, key := range []string{"name", "command", "cwd", "group", "env", "desc"} {
		_, ok := m[key]
		assert.Truef(t, ok, "expected JSON key %q in %s", key, raw)
	}
}

func TestServiceConfigJSONRoundTrip(t *testing.T) {
	in := ServiceConfig{
		Name:    "api",
		Command: "go run ./cmd/api",
		CWD:     "/srv/api",
		Group:   "backend",
		Env:     map[string]string{"PORT": "4000", "LOG": "debug"},
		Desc:    "the api",
	}

	raw, err := json.Marshal(in)
	require.NoError(t, err)

	var out ServiceConfig
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, in, out)
}

func TestServiceConfigJSONOmitsEmpty(t *testing.T) {
	raw, err := json.Marshal(ServiceConfig{Name: "x", Command: "true"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"x","command":"true"}`, string(raw))
}
