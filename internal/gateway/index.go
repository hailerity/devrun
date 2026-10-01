package gateway

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

// The page makes no external requests: no webfont, no CDN, no JavaScript. A tool
// serving a page from your own machine should not phone home, and once the
// gateway is published a font CDN would be handed the IP of everyone the link is
// shared with. System font stacks only.
//
// It is also read-only — no start, stop or expose controls. Anything more and a
// leaked token stops being "someone can see my dev app" and becomes remote
// process control on the machine.
var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>devrun</title>
<style>
:root {
  --bg:#fff; --bar:#f6f8fa; --text:#1f2328; --muted:#656d76; --accent:#0969da;
  --green:#1a7f37; --red:#cf222e; --amber:#9a6700; --rule:#d8dee4; --hover:#f6f8fa;
  --mono:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
}
@media (prefers-color-scheme:dark){:root{
  --bg:#0d1117; --bar:#161b22; --text:#c9d1d9; --muted:#8b949e; --accent:#58a6ff;
  --green:#3fb950; --red:#f85149; --amber:#d29922; --rule:#21262d; --hover:#161b22;
}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);
  font:15px/1.5 ui-sans-serif,system-ui,-apple-system,"Helvetica Neue",sans-serif}
.bar{background:var(--bar);border-bottom:1px solid var(--rule)}
.bar.foot{border-bottom:0;border-top:1px solid var(--rule)}
.wrap{max-width:760px;margin:0 auto;padding:0 20px}
.bar .wrap{display:flex;justify-content:space-between;align-items:baseline;
  gap:16px;padding-top:18px;padding-bottom:18px;flex-wrap:wrap}
.bar.foot .wrap{padding-top:14px;padding-bottom:14px;font-size:13px;color:var(--muted)}
.mark{font-size:17px}.mark b{font-weight:600}.mark .hex{color:var(--accent)}
.where{font-family:var(--mono);font-size:13px;color:var(--muted)}
.pub{color:var(--amber)}
main{padding:8px 0 28px}
.row{display:flex;align-items:center;gap:16px;padding:17px 12px;
  border-bottom:1px solid var(--rule);border-radius:6px;min-height:44px;
  text-decoration:none;color:inherit}
a.row:hover{background:var(--hover)}
a.row:focus-visible{outline:2px solid var(--accent);outline-offset:-2px}
a.row:hover .go{color:var(--accent)}
.dot{font-size:11px;line-height:1}
.up{color:var(--green)}.down{color:var(--muted)}.bad{color:var(--red)}.mid{color:var(--amber)}
.name{flex:1 1 auto;min-width:0;font-family:var(--mono);font-size:16px}
.name.off{color:var(--muted)}
.sub{display:block;font-family:var(--mono);font-size:12px;color:var(--muted);
  margin-top:3px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.port{font-family:var(--mono);font-size:14px;color:var(--muted)}
.note{font-size:13px;color:var(--muted)}.note.warn{color:var(--amber)}
.go{width:14px;color:var(--muted);font-size:15px}
.caveat{margin:10px 12px 14px;padding:10px 12px;background:var(--bar);
  border-radius:6px;font-size:13px;color:var(--muted)}
.empty{padding:56px 12px;text-align:center}
.empty p{margin:0 0 10px}
.empty code{font-family:var(--mono);font-size:13px;background:var(--bar);
  padding:6px 10px;border-radius:5px;color:var(--muted)}
</style>
</head>
<body>
<header class="bar"><div class="wrap">
  <span class="mark"><span class="hex">&#11041;</span> <b>devrun</b></span>
  <span class="where">{{.Host}} &middot; {{if .Published}}<span class="pub">public</span>{{else}}local{{end}}</span>
</div></header>

<main><div class="wrap">
{{- if .PathMode}}
  <p class="caveat">Served under a path. Apps that request assets from the site root may not load &mdash; set a base path, or use a named tunnel for one subdomain per service.</p>
{{- end}}
{{- if .Services}}
  {{- range .Services}}
    {{- if .URL}}
  <a class="row" href="{{.URL}}">
    <span class="dot {{.Class}}" aria-hidden="true">{{.Glyph}}</span>
    <span class="name">{{.Name}}{{if .Sub}}<span class="sub">{{.Sub}}</span>{{end}}</span>
    <span class="port">:{{.Port}}</span>
    <span class="go" aria-hidden="true">&#8594;</span>
  </a>
    {{- else}}
  <div class="row">
    <span class="dot {{.Class}}" aria-hidden="true">{{.Glyph}}</span>
    <span class="name off">{{.Name}}</span>
    <span class="note{{if .Warn}} warn{{end}}">{{.Note}}</span>
    <span class="go"></span>
  </div>
    {{- end}}
  {{- end}}
{{- else if .Published}}
  <div class="empty">
    <p>Nothing is published.</p>
    <p class="note">{{.Total}} service(s) are running &mdash; none of them may leave the machine.</p>
    <code>devrun gateway expose &lt;name&gt;</code>
  </div>
{{- else}}
  <div class="empty">
    <p>Nothing is running.</p>
    <code>devrun start &lt;name&gt;</code>
  </div>
{{- end}}
</div></main>

<footer class="bar foot"><div class="wrap">
  <span>{{.Summary}}</span>
  <span style="font-family:var(--mono)">{{if .Version}}devrun {{.Version}}{{end}}</span>
</div></footer>
</body>
</html>
`))

type indexRow struct {
	Name  string
	Sub   string // the hostname, in subdomain mode
	Port  int
	URL   string // "" when the row must not be a link
	Note  string // why it is not
	Warn  bool   // the note is actionable, not just informational
	Glyph string
	Class string
}

type indexPage struct {
	Host      string
	Published bool
	PathMode  bool
	Version   string
	Services  []indexRow
	Total     int
	Summary   string
}

// index renders the list of services this request is allowed to see.
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	published := s.Posture(r) == Published
	listed := s.Listing(r)
	all := s.Snapshot().Routes

	running := 0
	for _, route := range all {
		if route.State == "running" {
			running++
		}
	}

	page := indexPage{
		Host:      r.Host,
		Published: published,
		PathMode:  s.linkMode() == Path,
		Version:   s.cfg.Version,
		Total:     len(all),
	}
	for _, route := range listed {
		page.Services = append(page.Services, s.row(r, route))
	}
	if published {
		page.Summary = itoa(len(listed)) + " of " + itoa(len(all)) + " published"
	} else {
		page.Summary = plural(len(all), "service", "services") + " · " + itoa(running) + " running"
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Nothing here is cacheable: which services are up changes under the reader.
	w.Header().Set("Cache-Control", "no-store")
	_ = indexTmpl.Execute(w, page)
}

// row turns a route into what the page shows for it. A route that cannot be
// proxied is still listed, with the reason — a link that 503s is worse than a
// row that explains itself.
func (s *Server) row(r *http.Request, route Route) indexRow {
	out := indexRow{Name: route.Name, Port: route.Port}
	out.Glyph, out.Class = stateGlyph(route.State)

	switch {
	case route.State != "running":
		out.Note = route.State
		out.Warn = route.State == "crashed"
	case route.Port == 0:
		out.Note = "port unknown — set port:"
		out.Warn = true
	default:
		out.URL = s.serviceURL(r, route.Name)
		if out.URL == "" {
			// Running and reachable, but the routes table mounts it nowhere.
			out.Note = "not routed"
			out.Warn = true
		} else if s.linkMode() == Subdomain {
			out.Sub = subdomainHost(r.Host, route.Name)
		}
	}
	return out
}

// stateGlyph matches the TUI's vocabulary — ● running, ◐ in transition,
// ✖ crashed, ○ otherwise — so the shape identifies the state without colour.
func stateGlyph(state string) (glyph, class string) {
	switch state {
	case "running":
		return "●", "up"
	case "starting", "stopping":
		return "◐", "mid"
	case "crashed":
		return "✖", "bad"
	default:
		return "○", "down"
	}
}

// linkMode is the shape the index advertises. Both are always accepted by
// Resolve; this only decides what the links look like.
func (s *Server) linkMode() Mode {
	if len(s.cfg.Rules) > 0 {
		// An explicit routes table is already a set of paths.
		return Path
	}
	return s.cfg.Mode
}

// serviceURL builds the link for a service.
func (s *Server) serviceURL(r *http.Request, name string) string {
	if len(s.cfg.Rules) > 0 {
		// The table is the topology; a service it does not mount has no path.
		p, ok := s.rulePath(name)
		if !ok {
			return ""
		}
		return p
	}
	if s.linkMode() == Path {
		// Relative, so it works over http locally and https once published
		// without the gateway having to guess which it is behind.
		return "/" + name + "/"
	}
	scheme := "http"
	if s.Posture(r) == Published {
		scheme = "https"
	}
	link := scheme + "://" + subdomainHost(r.Host, name) + "/"

	// Each service is a different host and the handover cookie is host-only, so
	// a bare link would land somewhere with no cookie and 401. Carry the key:
	// the destination trades it for its own cookie and redirects to the clean
	// address at once. Anyone reading this page already got past the gate, so
	// the key is not exposed to someone who did not have it.
	//
	// A Domain-scoped cookie would avoid the repetition, but it needs the apex
	// hostname, which this package does not know — Config carries the bind
	// address, not the public name.
	if s.NeedsToken(r) {
		link += "?k=" + url.QueryEscape(s.cfg.Token)
	}
	return link
}

// subdomainHost puts the service's label in front of the gateway's own host,
// keeping any port: localhost:7788 becomes web.localhost:7788.
func subdomainHost(gatewayHost, name string) string {
	if gatewayHost == "" {
		return name
	}
	return name + "." + gatewayHost
}

func plural(n int, one, many string) string {
	if n == 1 {
		return itoa(n) + " " + one
	}
	return itoa(n) + " " + many
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// statusPage answers a request that resolved to nothing proxyable, in the same
// shell as the index so a wrong URL does not look like a different site.
func (s *Server) statusPage(w http.ResponseWriter, r *http.Request, code int, headline, detail string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)

	page := strings.NewReplacer(
		"{{HOST}}", template.HTMLEscapeString(r.Host),
		"{{HEADLINE}}", template.HTMLEscapeString(headline),
		"{{DETAIL}}", template.HTMLEscapeString(detail),
		"{{CODE}}", itoa(code),
	).Replace(statusShell)
	_, _ = w.Write([]byte(page))
}

const statusShell = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>devrun</title><style>
:root{--bg:#fff;--bar:#f6f8fa;--text:#1f2328;--muted:#656d76;--accent:#0969da;--rule:#d8dee4;
--mono:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
@media (prefers-color-scheme:dark){:root{--bg:#0d1117;--bar:#161b22;--text:#c9d1d9;
--muted:#8b949e;--accent:#58a6ff;--rule:#21262d}}
body{margin:0;background:var(--bg);color:var(--text);
font:15px/1.5 ui-sans-serif,system-ui,-apple-system,"Helvetica Neue",sans-serif}
.bar{background:var(--bar);border-bottom:1px solid var(--rule)}
.wrap{max-width:760px;margin:0 auto;padding:18px 20px;display:flex;
justify-content:space-between;align-items:baseline;gap:16px;flex-wrap:wrap}
.hex{color:var(--accent)}.mark{font-size:17px}.mark b{font-weight:600}
.where{font-family:var(--mono);font-size:13px;color:var(--muted)}
main{max-width:760px;margin:0 auto;padding:72px 20px;text-align:center}
h1{margin:0 0 12px;font-size:15px;font-weight:400}
p{margin:0;font-size:13px;color:var(--muted)}
a{color:var(--accent)}
.code{margin-top:28px;font-family:var(--mono);font-size:12px;color:var(--muted)}
</style></head><body>
<header class="bar"><div class="wrap">
<span class="mark"><span class="hex">&#11041;</span> <b>devrun</b></span>
<span class="where">{{HOST}}</span>
</div></header>
<main>
<h1>{{HEADLINE}}</h1>
<p>{{DETAIL}}</p>
<p class="code">{{CODE}} &middot; <a href="/">all services</a></p>
</main></body></html>`
