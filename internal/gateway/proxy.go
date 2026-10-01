package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
)

// ServeHTTP gates a request, then routes it to its service or renders the index.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The gate comes first: resolving reveals whether a name exists, and a
	// caller without a key is not entitled to learn that.
	if !s.authorize(w, r) {
		return
	}

	target, outcome := s.Resolve(r)
	switch outcome {
	case OK:
		s.proxy(w, r, target)

	case NotRunning:
		s.statusPage(w, r, http.StatusServiceUnavailable,
			target.Service+" is not running",
			"Start it with devrun start "+target.Service+", then reload this page.")

	default:
		// The index lives at the root. An explicit catch-all route puts a
		// service there instead, in which case Resolve above already took it.
		if r.URL.Path == "/" {
			s.index(w, r)
			return
		}
		// NotExposed answers exactly as NoSuchRoute does, down to the wording:
		// that a service exists but is withheld is itself something a stranger
		// should not learn.
		s.statusPage(w, r, http.StatusNotFound,
			"No service here",
			"Nothing is served at this address.")
	}
}

// proxy forwards a resolved request to the service's port on loopback.
func (s *Server) proxy(w http.ResponseWriter, r *http.Request, t Target) {
	route, ok := s.route(t.Service)
	if !ok || !route.Reachable() {
		http.Error(w, fmt.Sprintf("%s is not running", t.Service), http.StatusServiceUnavailable)
		return
	}

	// /web has to become /web/ before anything is proxied: a document served at
	// /web resolves its relative links against /, so every asset would be asked
	// for one directory too high.
	if t.Prefix != "" && r.URL.Path == t.Prefix {
		to := t.Prefix + "/"
		if r.URL.RawQuery != "" {
			to += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, to, http.StatusPermanentRedirect)
		return
	}

	upstream := net.JoinHostPort("127.0.0.1", strconv.Itoa(route.Port))
	inHost := r.Host

	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// X-Forwarded-Host carries the name the eyeball used, so an app that
			// needs it can still recover it after the Host rewrite below.
			pr.SetXForwarded()

			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = upstream
			pr.Out.URL.Path = stripPrefix(pr.In.URL.Path, t.Prefix)
			// Path is authoritative now; a stale RawPath would be used instead.
			pr.Out.URL.RawPath = ""

			// Angular CLI, Vite and webpack-dev-server all reject a Host they do
			// not know — "Invalid Host header" — so a request arriving as
			// web.devrun.example.com never reaches the app. Rewriting by default
			// means those work untouched; preserve is for apps that build
			// absolute URLs out of the Host they were asked for.
			if s.cfg.HostHeader == HostPreserve {
				pr.Out.Host = inHost
			} else {
				pr.Out.Host = upstream
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if t.Prefix != "" {
				reprefixLocation(resp, t.Prefix, upstream)
				reprefixCookies(resp, t.Prefix)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			// The service is listed as running but did not answer — it is coming
			// up, or it died between the snapshot and now.
			http.Error(w,
				fmt.Sprintf("%s did not answer on port %d: %v", t.Service, route.Port, err),
				http.StatusBadGateway)
		},
	}

	// ReverseProxy handles a 101 Switching Protocols response itself, so HMR
	// and other WebSockets need nothing special here.
	rp.ServeHTTP(w, r)
}

// stripPrefix removes the gateway's own path segment, since the service knows
// nothing about being mounted under it. The result always starts with "/".
func stripPrefix(path, prefix string) string {
	if prefix == "" {
		return path
	}
	out := strings.TrimPrefix(path, prefix)
	if out == "" {
		return "/"
	}
	if !strings.HasPrefix(out, "/") {
		// The prefix matched a longer segment (/webhook under /web), so it was
		// never this service's to strip.
		return path
	}
	return out
}

// reprefixLocation puts the stripped prefix back on a redirect, so a service
// that answers /login with Location: /dashboard does not send the browser out
// of its own mount point.
func reprefixLocation(resp *http.Response, prefix, upstream string) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return
	}
	u, err := url.Parse(loc)
	if err != nil {
		return
	}
	// An absolute URL pointing back at the upstream is the service talking about
	// itself; anything else is a real elsewhere and is left alone.
	if u.Host != "" && !isUpstream(u.Host, upstream) {
		return
	}
	if !strings.HasPrefix(u.Path, "/") {
		return // relative to the current directory, already correct
	}
	u.Scheme, u.Host = "", ""
	u.Path = prefix + u.Path
	resp.Header.Set("Location", u.String())
}

// reprefixCookies narrows a cookie set for the whole origin to the service's own
// mount point, so two services under one host cannot overwrite each other's
// session.
func reprefixCookies(resp *http.Response, prefix string) {
	values := resp.Header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	rewritten := make([]string, 0, len(values))
	for _, v := range values {
		rewritten = append(rewritten, reprefixCookie(v, prefix))
	}
	resp.Header.Del("Set-Cookie")
	for _, v := range rewritten {
		resp.Header.Add("Set-Cookie", v)
	}
}

func reprefixCookie(cookie, prefix string) string {
	parts := strings.Split(cookie, ";")
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		if !strings.HasPrefix(strings.ToLower(trimmed), "path=") {
			continue
		}
		path := trimmed[len("path="):]
		if !strings.HasPrefix(path, "/") {
			return cookie
		}
		sep := ""
		if i > 0 {
			sep = " "
		}
		parts[i] = sep + "Path=" + prefix + strings.TrimSuffix(path, "/")
		return strings.Join(parts, ";")
	}
	// No Path attribute: the browser would scope it to the request's directory,
	// which is already inside the mount point.
	return cookie
}

// isUpstream reports whether a redirect's host is the service talking about
// itself. The upstream is dialled as 127.0.0.1:<port>, but a service may spell
// the same place "localhost:<port>" — any loopback name on that port is it.
func isUpstream(host, upstream string) bool {
	if strings.EqualFold(host, upstream) {
		return true
	}
	h, hp, err := net.SplitHostPort(host)
	if err != nil {
		return false
	}
	_, up, err := net.SplitHostPort(upstream)
	if err != nil || hp != up {
		return false
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") || hasLocalhostSuffix(h) {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
