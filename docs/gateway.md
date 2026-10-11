# Local gateway and publishing

Serve every service you are running from one address, and — if you choose to — put that address on the internet.

`devrun gateway up` serves everything you are running from one address, with an index at the root you can click through.

```sh
devrun gateway up          # http://localhost:7788/
```

On loopback it needs no configuration and applies no controls — anything that can open `localhost:7788` can already open `localhost:4200` directly, so there is nothing to gate. The controls switch on the moment it can be reached from somewhere else.

### Four ways to use it

| what you want | config | `devrun tunnel up`? |
|---|---|---|
| local only | nothing | never |
| a phone on the same wifi | `gateway.bind: 0.0.0.0` | never |
| your own tunnel or reverse proxy | `gateway.posture: published` | never |
| devrun runs cloudflared for you | asked on first use, then saved | yes |

cloudflared is not a dependency. It is absent from `go.mod` and from the binary, and never looked for unless you publish.

### Two postures

|  | serves | allowlist | token |
|---|---|---|---|
| **local** | every running service | not applied | not required |
| **published** | only `gateway.expose` | applied | required |

Published is derived, not declared: a devrun-run tunnel, a non-loopback bind, or a request arriving with a `Host` the gateway does not recognise as itself.

**That last check is defence in depth, not a guarantee.** Front the gateway with something that rewrites `Host` to `localhost` — some proxies do — and it will still think it is local while the world can reach it. If you publish it yourself, say so:

```yaml
gateway:
  posture: published
```

On a shared machine, loopback is not a trust boundary either. `auth: always` keeps the token on locally.

### Handing over the key

`devrun gateway status` prints the token, whether or not a tunnel is running; `devrun tunnel up` prints it too, since publishing is what usually starts it being asked for. There are three ways to present it:

| | How | Good for |
|---|---|---|
| **Link** | `https://…/?k=<token>` | Sharing — the link carries the key |
| **Form** | Paste it on the page the gateway answers with | Whoever has the token but no link |
| **Header** | `Authorization: Bearer <token>` | `curl`, and anything that is not a browser |

The first two trade the token for an `HttpOnly` cookie, so it is presented once rather than on every link that follows.

**Prefer the form when you have the token to hand.** A pasted key never enters the address bar, the browser's history, a `Referer` header or the tunnel's request log; `?k=` is in a URL for one round trip before it is traded away. The form is no weaker a gate — the token is 128 bits of randomness, so neither can be guessed, and the page answers a wrong key exactly as it answers a missing one, so it never confirms a near miss.

A key entered in the form lands you on the index rather than the page you asked for. That is deliberate: carrying a destination through the form would make the denial page differ by the path that was requested, and it is identical for every path so that a stranger cannot learn which services exist.

### How a request names a service

Two shapes, and both always resolve whatever the mode says — `mode` only decides which one the index advertises.

```
subdomain   web.localhost:7788          one origin per service
path        localhost:7788/web/         one origin for everything
```

**Locally, prefer subdomain.** It is free: browsers resolve `*.localhost` to loopback with no setup, and it keeps a frontend's relative `/api/...` calls working through its own dev server's proxy, exactly as they do at `localhost:4200`. Reaching the gateway at an **IP** forces path links, because `web.127.0.0.1` resolves nowhere — so open it by name. Every URL devrun prints for a loopback gateway uses `localhost` for that reason, and names each service the way the gateway would link to it.

### Path mode breaks root-absolute asset URLs

An app served at `/web/` that asks for `/@vite/client` is asking the *gateway* for it, not the app, and gets a 404. Give one service the root:

```yaml
gateway:
  routes:
    "/":    web
    "/api": { service: api, strip: false }
```

Now `/@vite/client` reaches `web` untouched, `/api/users` reaches `api` still spelled `/api/users`, and both share one origin — so there is no CORS between them either. With two frontends only one can hold `/`; the second needs its own base path (Vite's `base`, Angular's `--base-href`, webpack's `publicPath`).

**A catch-all `"/"` rule is matched before the Host label**, so once you have one, the subdomain URLs all resolve to whatever sits at the root.

### Publishing

```sh
devrun tunnel up web api     # publishes these two, and starts the gateway
devrun tunnel status
devrun tunnel down           # the gateway keeps serving locally
```

Asked once on first use, then remembered:

```
No tunnel configured.

  A hostname gives a stable URL. It needs a Cloudflare account and three
  commands, which devrun will not run for you:

      cloudflared tunnel login
      cloudflared tunnel create devrun
      cloudflared tunnel route dns devrun devrun.example.com

  Blank gives a quick tunnel instead: no account, nothing to set up, and
  a new URL every run.

  hostname (blank for a quick tunnel): devrun.example.com
  cloudflared tunnel [devrun]:

Saved tunnel.name and tunnel.hostname to ./devrun.yaml

Your setup, with the values just saved — skip whatever is already done:

    cloudflared tunnel login
    cloudflared tunnel create devrun
    cloudflared tunnel route dns devrun devrun.example.com

That hostname is the tunnel's own, where the service index is served, and
in path mode the only record there is. Giving each service a hostname adds
one record per published service; devrun resolves them all once the tunnel
is up and names any that is missing.
```

The commands appear above the question because that is the decision they inform — a stable URL costs a Cloudflare account and three commands, a quick one costs nothing. They are repeated underneath with the answers filled in, so they can be pasted.

Off a terminal it never asks — an agent or a script gets a quick tunnel and a printed note. A quick tunnel needs no account, no DNS and no login, and its URL changes every run.

A named one needs three things on your Cloudflare account first, none of which devrun creates:

```sh
cloudflared tunnel login
cloudflared tunnel create devrun
cloudflared tunnel route dns devrun devrun.example.com
```

That third hostname is the tunnel's own — where the service index is served, and in path mode the only record there is.

### What a published subdomain costs

Each service needs a hostname, and **a certificate that covers it**. Cloudflare's Universal SSL covers the apex and one wildcard level, so `web.devrun.example.com` — two labels deep — is refused at the TLS handshake, not merely distrusted.

```yaml
gateway:
  public_hostname: "{service}-devrun.example.com"
```

| shape | DNS | certificate |
|---|---|---|
| `{service}-devrun.example.com` | one CNAME per service | free |
| `{service}.example.com` | one CNAME per service, or `*.example.com` | free |
| `{service}.devrun.example.com` | `*.devrun.example.com` | needs Advanced Certificate Manager |
| *unset* — path mode | none beyond the tunnel's own | free |

Each of those is **on top of** the tunnel's own record, which stays needed in every shape.

The namespaced form is the one to reach for on a domain you use for anything else: without the suffix, a service called `api` quietly claims `api.example.com`.

devrun does not write DNS records — that needs write access to your zone it has no business holding — but it prints the command for any that are missing:

```
3 hostnames do not resolve yet:
    cloudflared tunnel route dns devrun devrun.example.com
    cloudflared tunnel route dns devrun web-devrun.example.com
    cloudflared tunnel route dns devrun api-devrun.example.com
```

### Telling an app where its backend is

Most projects need nothing here: a frontend's dev server proxies `/api` and the relative call keeps working through the gateway. For an app that must call an absolute URL, devrun injects one per service:

```
DEVRUN_URL_API = http://localhost:3000             # local
DEVRUN_URL_API = https://api-devrun.example.com    # published
```

Frameworks filter the environment by prefix — Vite exposes only `VITE_`, Create React App only `REACT_APP_` — so bridge it in config:

```yaml
services:
  web:
    env:
      VITE_API_URL: "${DEVRUN_URL_API}"
```

`${NAME}` only; a bare `$NAME` is left alone so passwords and shell snippets survive, and `$$` is a literal `$`.

Dev servers read their environment once, at startup, so a service already running when you publish still holds the local value:

```sh
devrun tunnel up --restart-exposed
```

### The index is read-only

It lists services and links to them. No start, no stop, no logs. Anything more and a leaked token stops being "someone can see my dev app" and becomes remote process control on your machine.
