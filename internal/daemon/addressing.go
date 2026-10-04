package daemon

import (
	"os"
	"strconv"
	"strings"

	"github.com/hailerity/devrun/internal/config"
)

// URLVarPrefix starts the variable naming each service's address.
const URLVarPrefix = "DEVRUN_URL_"

// URLVarName is the environment variable carrying a service's address:
// "pimatix-web" becomes DEVRUN_URL_PIMATIX_WEB.
//
// Anything a service name may contain but an environment variable may not —
// '-', '.' — becomes '_'. Names are validated elsewhere to letters, digits,
// '.', '_' and '-', so this covers the set.
func URLVarName(service string) string {
	var b strings.Builder
	b.Grow(len(URLVarPrefix) + len(service))
	b.WriteString(URLVarPrefix)
	for _, r := range strings.ToUpper(service) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// serviceURLsLocked is the address of every service devrun can name, keyed by
// environment variable. Caller holds s.mu.
//
// Some apps cannot use their dev server's proxy and must call an absolute
// URL. One variable that resolves correctly in both worlds beats making the
// app branch on which world it is in:
//
//	DEVRUN_URL_API = http://localhost:3000             # local
//	DEVRUN_URL_API = https://api-devrun.example.com    # published
//
// A service with no known port is omitted rather than given a guess. An
// absent variable expands to empty, which is a visibly broken URL at the
// first request — better than a plausible one pointing at the wrong port.
func (s *supervisor) serviceURLsLocked() map[string]string {
	out := map[string]string{}
	published := s.publishedBaseLocked()

	for name, svc := range s.services {
		var url string
		if published != nil {
			url = published(name)
		} else if port := addressablePort(svc); port > 0 {
			url = "http://localhost:" + strconv.Itoa(port)
		}
		if url != "" {
			out[URLVarName(name)] = url
		}
	}
	return out
}

// publishedBaseLocked returns a function giving each service's public address,
// or nil when nothing devrun manages is publishing.
//
// Only a devrun-run tunnel counts. Someone fronting the gateway with ngrok or
// their own Caddy is publishing it just as truly, but devrun does not know the
// name they are using and will not invent one — a wrong absolute URL is worse
// than a local one the app can still reach.
func (s *supervisor) publishedBaseLocked() func(string) string {
	if s.tunnel == nil || !pidAlive(s.tunnel.pid) || s.gateway == nil {
		return nil
	}
	public := s.tunnel.publicURL
	if public == "" {
		// A quick tunnel whose banner could not be read. It is publishing,
		// but devrun cannot say where, so there is no URL to hand out.
		return nil
	}
	template := s.gateway.cfg.PublicHostname
	exposed := s.gateway.cfg.ExposedSet()

	return func(name string) string {
		// Published means the allowlist applies, so a service that may not
		// leave the machine has no public address to advertise.
		if !containsString(exposed, name) {
			return ""
		}
		if template != "" {
			return "https://" + strings.ReplaceAll(template, config.ServicePlaceholder, name)
		}
		return strings.TrimSuffix(public, "/") + "/" + name + "/"
	}
}

// addressablePort is the port a sibling can usefully be told about.
//
// A declared port always counts: it is config, true whether or not the
// service is up, and it is the answer for one holding several listeners where
// detection takes the lowest and may take the debugger's.
//
// A detected port counts only while the service is live. ServiceState.Port
// outlives the process that was listening on it, so a stopped service would
// otherwise hand out an address nothing answers on — and a plausible wrong
// URL costs more to debug than an absent one.
func addressablePort(svc *managedService) int {
	if svc == nil {
		return 0
	}
	if svc.cfg != nil && svc.cfg.Port > 0 {
		return svc.cfg.Port
	}
	if svc.state != nil && svc.state.Port != nil && svc.state.Status.IsLive() {
		return *svc.state.Port
	}
	return 0
}

// serviceEnvLocked is the environment a service is started with: the sibling
// addresses, then its own entries with ${NAME} resolved against them.
//
// Expansion happens here rather than in config because only the daemon knows
// the addresses, and only at the moment of starting — they depend on which
// services are up and whether anything is publishing.
func (s *supervisor) serviceEnvLocked(cfg *config.ServiceConfig) map[string]string {
	urls := s.serviceURLsLocked()

	expanded := config.ExpandEnv(cfg.Env, func(name string) (string, bool) {
		if v, ok := urls[name]; ok {
			return v, true
		}
		return os.LookupEnv(name)
	})

	// The URLs go in too, so an app can read DEVRUN_URL_API directly when its
	// framework does not filter by prefix.
	out := make(map[string]string, len(urls)+len(expanded))
	for k, v := range urls {
		out[k] = v
	}
	for k, v := range expanded {
		out[k] = v
	}
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
