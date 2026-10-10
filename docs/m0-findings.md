# M0 probe findings, verified 2026-10-10

All claims below were verified against the live daemons, not docs. herdr 0.9.3, pi session version 3.

## herdr socket API

- Socket at `~/.config/herdr/herdr.sock`. Protocol 22, schema version 1, confirmed.
- Envelope: `{"method": "<name>", "params": {...}}`. Full schema: `herdr api schema --json`. Live snapshot: `herdr api snapshot`.
- Methods this app needs, confirmed present: `agent.list`, `agent.get`, `agent.start`, `agent.prompt`, `agent.send_keys`, `agent.read`, `agent.rename`, `agent.wait`, `workspace.create`, `workspace.close`, `tab.create`, `pane.read`, `pane.split`, `pane.send_input`, `pane.send_keys`, `events.subscribe`.
- `AgentStartParams`: `{name, kind, pane_id, args[], timeout_ms 3000..300000}`. `pi` is a supported kind. Start needs an existing pane at a shell prompt; `workspace.create` returns the root pane in one call.
- `AgentPromptParams`: `{target, text, wait{timeout_ms, until[]}}`. **Text only, no images.** The composer path-note fallback is the only image path (D6).
- `agent.read` / `pane.read`: `{target, source, format, lines, strip_ansi}`. `ReadFormat` enum is `text` and `ansi`. Revisions on `PaneReadResult` support incremental fetch.
- `events.subscribe` delivers `pane_output_changed`, `pane_agent_status_changed`, workspace/tab/pane lifecycle events. This is the live-update channel for Status badges and the terminal.
- There is **no attach method in the socket**. `herdr agent attach` is a CLI thin client only. The terminal tab drives the socket: `pane.read` in `ansi` format plus `pane_output_changed` events into xterm.js, keystrokes out via `pane.send_input`. No PTY bridge, no cgo.
- Panes expose `agent_session.value`: the full pi session file path, updated live (null until the first prompt creates the file). This is the Driver-to-session mapping for the session list.

## pi session files

- Location: `~/.pi/agent/sessions/--<workdir-slug>--/<timestamp>_<uuid>.jsonl`. 65 dirs on this machine.
- Header line: `{"type":"session","version":3,"id":uuid,"cwd":...}` plus optional `name`, `parentSession`.
- Entry types in the wild: `message` (roles `system`, `user`, `assistant`, `toolResult`), `custom` (`{customType, data}`, e.g. `om.*` memory entries), `compaction`, `model_change`, `thinking_level_change`.
- Content blocks confirmed: `thinking`, `text`, `toolCall` on assistant; `text` on user and toolResult.
- toolResult carries `toolCallId`, `toolName`, `isError`, `durationMs`, `details` — enough for V4 tool rows without heuristics.
- Assistant messages carry `usage` with `totalTokens` and `cost.total` — enough for the V9 footer.
- No dedicated subagent entry type found in this session. Subagent linkage stays an M0-verify item (V6).

## Status semantics (spec delta)

- Verified transition: pi finishes a turn and herdr reports **`done`**, not `idle`. `idle` was seen only before the first prompt. The UI docs that say "working then idle" mean "working then done" in practice.
- Consequences: session-list badge text, push triggers (P5 fires on `working -> done` for completed replies), and prompt `--until` state sets (`done, idle, blocked`, never `idle` alone).
- A working turn interrupted with `send_keys: ["ctrl+c"]` flipped `working -> done` within seconds. ADR-0001 abort path confirmed.

## Lifecycle probe (all green)

1. `herdr workspace create --cwd <tmp> --label piremote-probe --no-focus` returned workspace, tab, and root pane in one call.
2. `herdr agent start piremote-probe --kind pi --pane <id>`: pi interactive in 4 s, `interactive_ready: true`.
3. `herdr agent prompt ... "Reply with exactly: PIREMOTE-OK"`: reply rendered in the pane, session file created and linked.
4. `herdr agent read --lines N`: pane tail readable (text format).
5. `herdr agent send-keys ctrl+c`: stopped a running turn, `working -> done`.
6. `herdr workspace close`: clean teardown, zero leftover agents.

Probe artifacts: one junk pi session file under `~/.pi/agent/sessions/--tmp-tmp.FcPRcGk5ah--/`. Left in place; delete at will.
