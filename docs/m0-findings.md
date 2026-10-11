# M0 probe findings, verified 2026-10-10; push-slice additions 2026-10-11

All claims below were verified against the live daemons, not docs. herdr 0.9.3, pi session version 3.

## herdr socket API

- Socket at `~/.config/herdr/herdr.sock`. Protocol 22, schema version 1, confirmed.
- Envelope: `{"method": "<name>", "params": {...}}`. Full schema: `herdr api schema --json`. Live snapshot: `herdr api snapshot`.
- Methods this app needs, confirmed present: `agent.list`, `agent.get`, `agent.start`, `agent.prompt`, `agent.send_keys`, `agent.read`, `agent.rename`, `agent.wait`, `workspace.create`, `workspace.close`, `tab.create`, `pane.read`, `pane.split`, `pane.send_input`, `pane.send_keys`, `events.subscribe`.
- `AgentStartParams`: `{name, kind, pane_id, args[], timeout_ms 3000..300000}`. `pi` is a supported kind. Start needs an existing pane at a shell prompt; `workspace.create` returns the root pane in one call.
- `AgentPromptParams`: `{target, text, wait{timeout_ms, until[]}}`. **Text only, no images.** The composer path-note fallback is the only image path (D6).
- `agent.read` / `pane.read`: `{target, source, format, lines, strip_ansi}`. `ReadFormat` enum is `text` and `ansi`. Revisions on `PaneReadResult` support incremental fetch.
- `events.subscribe` accepts workspace/tab/pane lifecycle events (dotted variant names such as `pane.updated`) and returns only a `subscription_started` ack on its socket; nothing streams on it afterwards. `events.wait` accepts only pane agent-status matches in herdr 0.9.3 (`unsupported_event_wait_match` for anything else). There is **no output push**: the terminal polls `pane.read` and gates on the `PaneReadResult` revision. Status badges ride session-file ticks and the 30 s heartbeat.
- There is **no attach method in the socket**. `herdr agent attach` is a CLI thin client only. The terminal tab drives the socket: `pane.read` in `ansi` format polled on a 1 s tick, keystrokes out via `pane.send_input`. No PTY bridge, no cgo.
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

1. `herdr workspace create --cwd <tmp> --label couch-pi-probe --no-focus` returned workspace, tab, and root pane in one call.
2. `herdr agent start couch-pi-probe --kind pi --pane <id>`: pi interactive in 4 s, `interactive_ready: true`.
3. `herdr agent prompt ... "Reply with exactly: PIREMOTE-OK"`: reply rendered in the pane, session file created and linked.
4. `herdr agent read --lines N`: pane tail readable (text format).
5. `herdr agent send-keys ctrl+c`: stopped a running turn, `working -> done`.
6. `herdr workspace close`: clean teardown, zero leftover agents.

Probe artifacts: one junk pi session file under `~/.pi/agent/sessions/--tmp-tmp.FcPRcGk5ah--/`. Left in place; delete at will.

## Push-slice probes, verified 2026-10-11

- `agent.wait` (`AgentWaitParams`: `{target, until[], timeout_ms}`) fires only on transitions INTO a matched state and never on state equality: an 8 s `--until done` on an agent already `done` times out cleanly. One blocking call per agent covers all target statuses; timeouts are errors the caller re-arms on.
- `agent.wait` re-returns on EVERY subsequent agent update while the status still matches the until set (token and cost flushes), not just on status changes. A watcher needs a re-arm pause plus same-status dedupe or it buzzes every flush.
- CLI form: `herdr agent prompt <TARGET> "<msg>" --wait --until done --timeout <MS>`. The message text must come before the flags; `--timeout-ms` is unknown. `--wait` from a non-working state requires an observed working or blocked transition within 5000 ms else `agent_prompt_stalled`.
- `herdr agent start` has no `--cwd`; the agent inherits the pane's cwd. Form: `herdr agent start <NAME> --kind <KIND> --pane <ID> [-- AGENT_ARGS]`.
- `herdr api snapshot` JSON nests all state under `result.snapshot`; top-level keys are only `id` and `result`, and workspaces carry no `id` key (use `workspace_id` from creation results).
- `herdr api` has only `snapshot` and `schema` subcommands. Raw socket probes go through a small python script over the unix socket.
- The `pane.agent_status_changed` wait match requires both `pane_id` and `agent_status`: one call matches one pane and one target status. `agent.wait` matches one agent against repeatable statuses, which is the cheaper shape.
