# Using devrun from an AI agent

Coding agents start dev servers all the time, and do it badly: they block on a foreground `npm run dev`, background it and lose the output, or leave processes behind. `devrun mcp` is a [Model Context Protocol](https://modelcontextprotocol.io) server that lets an agent hand those processes to devrun instead. The agent drives the same daemon you do, so whatever it starts shows up live in your `devrun` dashboard, and the reverse.

### Set up

**Claude Code — plugin (recommended)**

The plugin wires up the MCP server *and* a skill that tells the agent to reach for devrun when it needs a dev server, rather than leaving it to notice the tools on its own:

```bash
claude plugin marketplace add hailerity/devrun
claude plugin install devrun@hailerity
```

or, in a session, `/plugin marketplace add hailerity/devrun` followed by `/plugin install devrun@hailerity`.

To try it without installing, `claude --plugin-dir ./plugin` loads it for one session.

**Claude Code — MCP server only**

```bash
claude mcp add devrun -- devrun mcp              # this project only
claude mcp add -s user devrun -- devrun mcp      # every project
claude mcp add -s project devrun -- devrun mcp   # shared with the team via .mcp.json
```

These give you the tools without the skill. If you have done this *and* then install the plugin, your own entry takes precedence and the plugin's is ignored — both define the same `devrun mcp` command, so nothing breaks, but `claude mcp list` will mention the conflict.

The project scope writes a `.mcp.json` you can commit:

```json
{
  "mcpServers": {
    "devrun": { "command": "devrun", "args": ["mcp"] }
  }
}
```

**Codex**

```bash
codex mcp add devrun -- devrun mcp
```

or in `~/.codex/config.toml`:

```toml
[mcp_servers.devrun]
command = "devrun"
args = ["mcp"]
```

Any other MCP client that can launch a stdio server works the same way: the command is `devrun mcp`. Run by hand in a terminal, `devrun mcp` just prints these instructions.

### Tools

| Tool | What it does |
|---|---|
| `list_services` | Every service and target in scope, with live state, PID, port, uptime, CPU/memory, its group, and the URL it can be opened at |
| `service_status` | One service's state and definition — command, directory, group, env variable **names** (never values), last exit code |
| `gateway_status` | What the gateway serves, its posture, what may leave the machine, and the public URL when a tunnel is running. Never the token |
| `logs` | The end of a service's output as plain text: 100 lines by default, at most 1000 and 64 KB, optionally filtered |
| `add_service` | Define a new service in the project's `devrun.yaml` (or the global registry), optionally in a `group`. Refuses an existing name |
| `add_to_target` | Create a target or add services to it |
| `start` | Start a service or target and **wait for the outcome**: running, or crashed / exited / failed with the end of its log |
| `stop` | Stop a service or target. Stopping something that is not running is fine |

`start` does not report success the moment a process is spawned. It waits until the service has stayed up for a short settle period (2 s), so a crash just after boot comes back as `crashed` with the log lines that explain it — in the same call.

**Which config?** Every tool takes an optional absolute `project_dir` (default: the directory the agent launched `devrun mcp` in) and uses that directory's `devrun.yaml`, or the global registry when it has none. Every result names the file it used. Pass `global: true` to use the global registry even inside a project.

### Permissions

`list_services`, `service_status` and `logs` only read, and are marked read-only. `add_service` followed by `start` runs whatever command the agent chose — no more than a shell tool already allows, but worth a prompt. A reasonable Claude Code setup auto-approves the read-only tools and asks for the rest, in `.claude/settings.json`:

```json
{
  "permissions": {
    "allow": ["mcp__devrun__list_services", "mcp__devrun__service_status", "mcp__devrun__logs"]
  }
}
```

Agents cannot remove or edit services, or control the daemon.

### Good to know

- **Tool timeouts.** `start` waits 15 s by default and accepts up to 120 s (`timeout_s`). Clients cap how long a tool call may take — Codex's default `tool_timeout_sec` is 60 — so raise that setting before asking for longer waits.
- **Log files are named by service.** Two projects that both define `api` share `~/.local/share/devrun/logs/api.log`.
- **Names** of services and targets an agent creates are limited to letters, digits, `.`, `_` and `-`.
