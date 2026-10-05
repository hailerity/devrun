package daemon

import (
	"os"
	"testing"

	"github.com/hailerity/devrun/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func svc(name string, status config.ServiceStatus, detected, declared int) *managedService {
	cfg := &config.ServiceConfig{Name: name, Command: "x", Port: declared}
	st := &config.ServiceState{Status: status}
	if detected > 0 {
		p := detected
		st.Port = &p
	}
	return &managedService{cfg: cfg, state: st}
}

func TestURLVarName(t *testing.T) {
	for in, want := range map[string]string{
		"api":         "DEVRUN_URL_API",
		"pimatix-web": "DEVRUN_URL_PIMATIX_WEB",
		"a.b-c":       "DEVRUN_URL_A_B_C",
		"web2":        "DEVRUN_URL_WEB2",
	} {
		assert.Equalf(t, want, URLVarName(in), "%q", in)
	}
}

// Locally a service is reached at its own port, which is what makes the
// variable useful at all: switching back to plain localhost is the default.
func TestServiceURLs_Local(t *testing.T) {
	s := quietSupervisor(t)
	s.services["api"] = svc("api", config.StatusRunning, 3000, 0)
	s.services["web"] = svc("web", config.StatusRunning, 0, 4200) // declared

	urls := s.serviceURLsLocked()
	assert.Equal(t, "http://localhost:3000", urls["DEVRUN_URL_API"])
	assert.Equal(t, "http://localhost:4200", urls["DEVRUN_URL_WEB"], "a declared port counts")
}

// ServiceState.Port outlives the process that was listening on it, so a
// stopped service would otherwise advertise an address nothing answers on.
func TestServiceURLs_StoppedServiceWithAStalePort(t *testing.T) {
	s := quietSupervisor(t)
	s.services["api"] = svc("api", config.StatusStopped, 3000, 0)
	s.services["web"] = svc("web", config.StatusStopped, 0, 4200)

	urls := s.serviceURLsLocked()
	assert.NotContains(t, urls, "DEVRUN_URL_API", "a detected port is only true while it is up")
	assert.Equal(t, "http://localhost:4200", urls["DEVRUN_URL_WEB"],
		"a declared port is config, true whether or not it is running")
}

func TestServiceURLs_NoPortMeansNoVariable(t *testing.T) {
	s := quietSupervisor(t)
	s.services["tauri"] = svc("tauri", config.StatusRunning, 0, 0)

	assert.NotContains(t, s.serviceURLsLocked(), "DEVRUN_URL_TAURI",
		"an absent variable beats a guessed port")
}

// Published, the same variable has to name the public address — that is the
// whole point of it resolving in both worlds.
func TestServiceURLs_PublishedWithAHostnameTemplate(t *testing.T) {
	s := quietSupervisor(t)
	s.services["api"] = svc("api", config.StatusRunning, 3000, 0)
	s.services["admin"] = svc("admin", config.StatusRunning, 9000, 0)
	s.gateway = &gatewayChild{pid: os.Getpid(), addr: "127.0.0.1:7788", cfg: config.GatewayConfig{
		PublicHostname: "{service}-devrun.example.com",
		Expose:         []string{"api"},
	}}
	s.tunnel = &tunnelChild{pid: os.Getpid(), kind: KindNamed, publicURL: "https://devrun.example.com"}

	urls := s.serviceURLsLocked()
	assert.Equal(t, "https://api-devrun.example.com", urls["DEVRUN_URL_API"])
	assert.NotContains(t, urls, "DEVRUN_URL_ADMIN",
		"published means the allowlist applies; a withheld service has no public address")
}

// Without a template, services hang off the tunnel's one hostname as paths.
func TestServiceURLs_PublishedByPath(t *testing.T) {
	s := quietSupervisor(t)
	s.services["api"] = svc("api", config.StatusRunning, 3000, 0)
	s.gateway = &gatewayChild{pid: os.Getpid(), cfg: config.GatewayConfig{Expose: []string{"api"}}}
	s.tunnel = &tunnelChild{pid: os.Getpid(), kind: KindQuick, publicURL: "https://odd-mountain.trycloudflare.com"}

	assert.Equal(t, "https://odd-mountain.trycloudflare.com/api/",
		s.serviceURLsLocked()["DEVRUN_URL_API"])
}

// A tunnel whose URL could not be read is publishing, but devrun cannot say
// where — so it hands out nothing rather than a wrong address.
func TestServiceURLs_PublishedButURLUnknown(t *testing.T) {
	s := quietSupervisor(t)
	s.services["api"] = svc("api", config.StatusRunning, 3000, 0)
	s.gateway = &gatewayChild{pid: os.Getpid(), cfg: config.GatewayConfig{Expose: []string{"api"}}}
	s.tunnel = &tunnelChild{pid: os.Getpid(), kind: KindQuick, publicURL: ""}

	assert.Equal(t, "http://localhost:3000", s.serviceURLsLocked()["DEVRUN_URL_API"],
		"it falls back to the local address the app can still reach")
}

// The case the whole group exists for: Vite only exposes VITE_-prefixed
// variables to client code, so the bridge has to happen in config.
func TestServiceEnv_BridgesIntoAFrameworkPrefix(t *testing.T) {
	s := quietSupervisor(t)
	s.services["api"] = svc("api", config.StatusRunning, 3000, 0)
	web := svc("web", config.StatusRunning, 0, 4200)
	web.cfg.Env = map[string]string{"VITE_API_URL": "${DEVRUN_URL_API}"}
	s.services["web"] = web

	env := s.serviceEnvLocked(web.cfg)
	assert.Equal(t, "http://localhost:3000", env["VITE_API_URL"])
	assert.Equal(t, "http://localhost:3000", env["DEVRUN_URL_API"],
		"the raw variable is there too, for frameworks that do not filter")
	assert.Equal(t, "${DEVRUN_URL_API}", web.cfg.Env["VITE_API_URL"],
		"the config itself is unchanged, so a restart re-expands from the template")
}

// An env value may reference the inherited environment too, not only what
// devrun injects.
func TestServiceEnv_FallsBackToTheInheritedEnvironment(t *testing.T) {
	t.Setenv("DEVRUN_TEST_TOKEN", "sekrit")
	s := quietSupervisor(t)
	cfg := &config.ServiceConfig{Name: "web", Command: "x",
		Env: map[string]string{"TOKEN": "${DEVRUN_TEST_TOKEN}"}}

	assert.Equal(t, "sekrit", s.serviceEnvLocked(cfg)["TOKEN"])
}

// A service's own entry wins over an injected one of the same name, so an
// explicit override is possible.
func TestServiceEnv_ExplicitEntryWins(t *testing.T) {
	s := quietSupervisor(t)
	s.services["api"] = svc("api", config.StatusRunning, 3000, 0)
	cfg := &config.ServiceConfig{Name: "web", Command: "x",
		Env: map[string]string{"DEVRUN_URL_API": "http://somewhere-else"}}

	assert.Equal(t, "http://somewhere-else", s.serviceEnvLocked(cfg)["DEVRUN_URL_API"])
}

func TestAddressablePort_NilSafe(t *testing.T) {
	assert.Zero(t, addressablePort(nil))
	assert.Zero(t, addressablePort(&managedService{}))
}

// The name mapping is not injective — '-', '.' and '_' all become '_' — so
// two services can claim one variable. Letting the last range iteration win
// means a service calls a sibling it was never pointed at, and differently on
// different calls, since Go randomises map order.
func TestServiceURLs_CollidingNamesYieldNoVariable(t *testing.T) {
	s := quietSupervisor(t)
	s.services["a-b"] = svc("a-b", config.StatusRunning, 3000, 0)
	s.services["a.b"] = svc("a.b", config.StatusRunning, 9999, 0)
	s.services["other"] = svc("other", config.StatusRunning, 4200, 0)

	// Repeated, because the bug this guards was intermittent: the same call
	// returned different answers in one process.
	for i := 0; i < 50; i++ {
		urls := s.serviceURLsLocked()
		require.NotContainsf(t, urls, "DEVRUN_URL_A_B",
			"call %d: neither claimant may win; empty breaks visibly, wrong does not", i)
		require.Equal(t, "http://localhost:4200", urls["DEVRUN_URL_OTHER"],
			"an uncontested name is unaffected")
	}
}

// One service owning a name is the ordinary case and must still work, even
// when that name contains the characters that make collisions possible.
func TestServiceURLs_PunctuatedNameAlone(t *testing.T) {
	s := quietSupervisor(t)
	s.services["a-b"] = svc("a-b", config.StatusRunning, 3000, 0)

	assert.Equal(t, "http://localhost:3000", s.serviceURLsLocked()["DEVRUN_URL_A_B"])
}

// A service is reachable locally and publicly at the same time, and devrun
// used to report only the public address — taking away the one being
// developed against the moment a tunnel started.
func TestServiceAddresses_LocalAndPublicTogether(t *testing.T) {
	s := addressSupervisor(t, config.GatewayConfig{
		Mode:           config.ModeSubdomain,
		PublicHostname: "{service}-devrun.example.com",
		Expose:         []string{"web"},
	}, true)

	got := s.serviceAddressesLocked()
	assert.Equal(t, "http://web.localhost:7788/", got["web"].local)
	assert.Equal(t, "https://web-devrun.example.com", got["web"].public)
}

// Publishing takes a withheld service off the internet, not off this
// machine: the gateway judges a request by its Host, and a browser at
// localhost is not the tunnel. So db keeps its local address throughout, and
// never gains a public one.
//
// This is the assertion an earlier version of this test had backwards, from
// measuring a gateway with Snapshot.Tunnelled set — which nothing outside a
// test ever does.
func TestServiceAddresses_AWithheldServiceKeepsItsLocalURL(t *testing.T) {
	cfg := config.GatewayConfig{Mode: config.ModeSubdomain, Expose: []string{"web"}}

	withTunnel := addressSupervisor(t, cfg, true).serviceAddressesLocked()
	assert.Equal(t, "http://db.localhost:7788/", withTunnel["db"].local)
	assert.Empty(t, withTunnel["db"].public, "not exposed, so nowhere public to point")

	noTunnel := addressSupervisor(t, cfg, false).serviceAddressesLocked()
	assert.Equal(t, "http://db.localhost:7788/", noTunnel["db"].local)
	assert.Empty(t, noTunnel["web"].public, "no tunnel, nothing published")
}

// Binding off loopback is the case that does withhold locally: the gateway
// treats every request as published, because the machine itself is reachable
// from elsewhere.
func TestServiceAddresses_ALANBindWithholdsLocallyToo(t *testing.T) {
	got := addressSupervisor(t, config.GatewayConfig{
		Bind: "0.0.0.0", Mode: config.ModeSubdomain, Expose: []string{"web"},
	}, false).serviceAddressesLocked()

	_, listed := got["db"]
	assert.False(t, listed, "the gateway would 404 it, so there is no address to give")
	assert.NotEmpty(t, got["web"].local)
}

// addressSupervisor is a supervisor with a live gateway and two services, one
// exposed and one not, and optionally a tunnel over it. The pids are this
// test process: serviceAddressesLocked checks they are alive, and nothing
// here spawns anything.
func addressSupervisor(t *testing.T, gcfg config.GatewayConfig, tunnelled bool) *supervisor {
	t.Helper()
	self := os.Getpid()
	s := quietSupervisor(t)
	s.gateway = &gatewayChild{pid: self, addr: "127.0.0.1:7788", cfg: gcfg.Defaults()}
	for _, name := range []string{"web", "db"} {
		s.services[name] = &managedService{cfg: &config.ServiceConfig{Port: 4200}}
	}
	if tunnelled {
		s.tunnel = &tunnelChild{pid: self, kind: KindNamed, publicURL: "https://devrun.example.com"}
	}
	return s
}
