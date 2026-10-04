package config_test

import (
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
)

func base(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestExpandEnv(t *testing.T) {
	look := base(map[string]string{
		"DEVRUN_URL_API": "https://api-devrun.example.com",
		"EMPTY":          "",
	})

	for name, tc := range map[string]struct{ in, want string }{
		// The case this exists for.
		"the bridge":    {"${DEVRUN_URL_API}", "https://api-devrun.example.com"},
		"embedded":      {"${DEVRUN_URL_API}/v1", "https://api-devrun.example.com/v1"},
		"twice":         {"${EMPTY}${DEVRUN_URL_API}", "https://api-devrun.example.com"},
		"set but empty": {"[${EMPTY}]", "[]"},

		// A name nobody set is blank, not an error: a service whose URL does
		// not exist yet should still start.
		"unset": {"[${NOPE}]", "[]"},

		// Bare $NAME is left alone. Env values are full of literal dollars —
		// passwords, awk scripts, jq filters — and eating them silently would
		// be worse than not expanding at all.
		"bare dollar name": {"$DEVRUN_URL_API", "$DEVRUN_URL_API"},
		"jq filter":        {`.items[] | .$id`, `.items[] | .$id`},
		"password":         {"p$$w0rd", "p$w0rd"},
		"lone dollar":      {"100$", "100$"},
		"dollar at end":    {"a$", "a$"},

		// $$ is the escape for a value that really wants ${...}.
		"escaped reference": {"$${DEVRUN_URL_API}", "${DEVRUN_URL_API}"},

		// Malformed input stays as written rather than swallowing the rest.
		"unterminated":  {"${DEVRUN_URL_API", "${DEVRUN_URL_API"},
		"empty name":    {"${}", ""},
		"no dollars":    {"plain value", "plain value"},
		"empty string":  {"", ""},
		"brace no sign": {"{NOT_A_REF}", "{NOT_A_REF}"},
	} {
		got := config.ExpandEnv(map[string]string{"K": tc.in}, look)
		assert.Equalf(t, tc.want, got["K"], "%s: %q", name, tc.in)
	}
}

// The input map belongs to the caller — the service's config — and must come
// back unchanged, or a restart would expand an already-expanded value.
func TestExpandEnv_DoesNotMutateTheInput(t *testing.T) {
	in := map[string]string{"VITE_API_URL": "${DEVRUN_URL_API}"}
	out := config.ExpandEnv(in, base(map[string]string{"DEVRUN_URL_API": "https://x"}))

	assert.Equal(t, "${DEVRUN_URL_API}", in["VITE_API_URL"], "the config is not touched")
	assert.Equal(t, "https://x", out["VITE_API_URL"])
}

// Entries cannot see each other. The env is a map, so Go's iteration order is
// random and a sibling reference would resolve differently run to run —
// better to never resolve than to resolve unpredictably.
func TestExpandEnv_SiblingsAreNotVisible(t *testing.T) {
	out := config.ExpandEnv(
		map[string]string{"A": "first", "B": "[${A}]"},
		base(map[string]string{}),
	)
	assert.Equal(t, "[]", out["B"], "A is a sibling, not part of the base environment")
}

func TestExpandEnv_EmptyAndNil(t *testing.T) {
	assert.Nil(t, config.ExpandEnv(nil, base(nil)))
	assert.Empty(t, config.ExpandEnv(map[string]string{}, base(nil)))
}
