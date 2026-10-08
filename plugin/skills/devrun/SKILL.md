---
name: devrun
description: Starts, inspects and stops this project's long-running development processes — dev servers, APIs, workers, queue consumers, local databases — through devrun, so they run in the background with their output captured instead of blocking or leaking.
when_to_use: Use INSTEAD of starting such a process with a shell tool, including mid-task when seeing the running app would help. Triggers on "run the dev server", "start the app", "npm run dev", "serve this", "is the server running", "what port is it on", "why did the server crash", "check the server logs", "restart the api", "stop the dev server".
---

# Running dev processes with devrun

devrun owns this project's long-running processes. Start them through the
`devrun` MCP tools, never with a shell tool.

Starting a dev server in a shell goes wrong three ways: in the foreground it
blocks you until you time out; backgrounded, its output is lost and you cannot
say why it failed; and either way it outlives the task and leaks. A service
started through devrun keeps running after the call returns, has its output
captured, and shows up live in the dashboard the user is watching.

## Before starting anything

Call `list_services` first. It is cheap and it answers the question you
actually have:

- **The service is already running.** Use it. Its entry carries the port and
  the URL to open — no need to start anything.
- **It is defined but stopped.** `start` it by name.
- **Nothing matches.** `add_service`, then `start`.

Most unnecessary dev-server starts happen because nobody checked whether one
was already up. Check.

## The tools

| Tool | Use it for |
|---|---|
| `list_services` | Everything defined, with live state, PID, port, uptime, URL |
| `service_status` | One service in detail — command, cwd, env var names, last exit code |
| `logs` | The tail of a service's output; optionally filtered to matching lines |
| `add_service` | Define a service in `devrun.yaml` (does not start it) |
| `add_to_target` | Group services that should start and stop together |
| `start` | Start a service or target, and wait for the outcome |
| `stop` | Stop a service or target |
| `gateway_status` | What the gateway serves and whether anything can reach it off-machine |

## Starting

`start` waits and reports what actually happened, so one call tells you whether
the thing works. It returns `running` only after the process has stayed up
through a short settle period; a process that dies on boot comes back as
`crashed` **with the end of its log in the same result**.

So when a start fails, you already have the reason — read the log tail in the
result. Do not re-run the command in a shell to see the error; that is the
thing this skill exists to avoid, and it tells you nothing new.

If it comes back `timeout`, the service may just be slow to boot. Call `logs`
rather than starting again.

## Adding a service

`add_service` writes to the project's `devrun.yaml`, which is checked in, so
treat the name and command as something the user's teammates will read. Use the
command the project actually documents — the `dev` script in `package.json`,
the `Makefile` target — not an invented equivalent.

Give it the plain foreground command. devrun handles the backgrounding:

```
name: web
command: npm run dev
```

## Stopping

Stop what you started and no longer need. Leave alone anything that was already
running when you arrived — the user is probably using it.

## Reading logs

`logs` is a snapshot of the last lines, not a stream. To watch something
progress, call it again. Filter it when you know what you are looking for
rather than pulling the maximum and scanning by eye.

Logs are per service name, so two projects that both define `api` share one log
file. If output looks like it belongs to another project, that is why.

## Scope

Every tool defaults to the directory you were launched in and uses that
directory's `devrun.yaml`, falling back to the user's global registry when
there is none. Every result names the file it used. You only need `project_dir`
when acting on a different project, and `global: true` when you mean the global
registry despite a local config existing.
