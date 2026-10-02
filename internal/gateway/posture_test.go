package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func req(host string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = host
	return r
}

func TestPosture(t *testing.T) {
	tests := []struct {
		name      string
		bind      string
		forced    string
		tunnelled bool
		host      string
		want      Posture
	}{
		{"loopback bind, local host", "127.0.0.1:7788", PostureAuto, false, "localhost:7788", Local},
		{"loopback bind, loopback ip host", "127.0.0.1:7788", PostureAuto, false, "127.0.0.1:7788", Local},
		{"loopback bind, ipv6 loopback host", "127.0.0.1:7788", PostureAuto, false, "[::1]:7788", Local},
		{"bind host echoed back", "127.0.0.1:7788", PostureAuto, false, "127.0.0.1", Local},

		// RFC 6761 reserves .localhost for loopback, and browsers resolve it —
		// it is how subdomain routing is reached locally, so it must stay local.
		{"subdomain of localhost", "127.0.0.1:7788", PostureAuto, false, "web.localhost:7788", Local},

		// The hole this check exists for: someone else's ngrok in front of a
		// loopback gateway. devrun's own state says nothing; the Host does.
		{"foreign host on a loopback bind", "127.0.0.1:7788", PostureAuto, false, "a1b2.ngrok-free.app", Published},
		{"no host at all", "127.0.0.1:7788", PostureAuto, false, "", Published},

		{"devrun's own tunnel", "127.0.0.1:7788", PostureAuto, true, "localhost:7788", Published},
		{"wildcard bind", "0.0.0.0:7788", PostureAuto, false, "localhost:7788", Published},
		{"ipv6 wildcard bind", "[::]:7788", PostureAuto, false, "localhost:7788", Published},
		{"lan bind", "192.168.1.8:7788", PostureAuto, false, "localhost:7788", Published},
		{"forced, everything else local", "127.0.0.1:7788", PostureForced, false, "localhost:7788", Published},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Config{Bind: tc.bind, Posture: tc.forced})
			s.SetSnapshot(Snapshot{Tunnelled: tc.tunnelled})
			assert.Equal(t, tc.want, s.Posture(req(tc.host)))
		})
	}
}

func TestNeedsToken(t *testing.T) {
	local := req("localhost:7788")
	foreign := req("devrun.example.com")

	tests := []struct {
		name  string
		auth  string
		token string
		r     *http.Request
		want  bool
	}{
		{"auto, local", AuthAuto, "k", local, false},
		{"auto, published", AuthAuto, "k", foreign, true},
		{"always, local", AuthAlways, "k", local, true},
		{"none, published", AuthNone, "k", foreign, false},
		// A gateway with no token cannot ask for one, whatever the mode says.
		{"auto, published, no token minted", AuthAuto, "", foreign, false},
		{"always, no token minted", AuthAlways, "", local, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New(Config{Bind: "127.0.0.1:7788", Auth: tc.auth, Token: tc.token})
			assert.Equal(t, tc.want, s.NeedsToken(tc.r))
		})
	}
}

func TestHostOnly(t *testing.T) {
	for in, want := range map[string]string{
		"localhost:7788":         "localhost",
		"localhost":              "localhost",
		"[::1]:7788":             "::1",
		"[::1]":                  "::1",
		"web.devrun.example.com": "web.devrun.example.com",
		"":                       "",
	} {
		assert.Equal(t, want, hostOnly(in), in)
	}
}

func TestBindIsLoopback(t *testing.T) {
	for bind, want := range map[string]bool{
		"127.0.0.1:7788":   true,
		"127.0.0.53:7788":  true,
		"[::1]:7788":       true,
		"localhost:7788":   true,
		"0.0.0.0:7788":     false,
		"[::]:7788":        false,
		"192.168.1.8:7788": false,
		"":                 false,
		":7788":            false,
	} {
		assert.Equal(t, want, bindIsLoopback(bind), bind)
	}
}
