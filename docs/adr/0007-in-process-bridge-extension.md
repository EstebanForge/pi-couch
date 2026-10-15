# ADR-0007: In-process bridge extension as the dialog sensor

Date: 2026-10-14
Status: Accepted
Supersedes: nothing. Amends ADR-0001 (herdr is the only driver).
Related: ADR-0001, docs/m0-findings.md, internal/server/awaiting.go

## Context

pi asks the human things: project trust, extension confirm dialogs (the
pi-me family), MCP prompts, model and theme pickers, custom overlays.
While a blocking dialog is open, herdr keeps reporting the agent as
working or done, so Couch Pi cannot tell "generating" from "waiting on
you". The first fix (internal/server/awaiting.go) matches pi-tui pixels
on the pane: a regex over pane.read output. It works, but it guesses.

pi has a native extension API for this. Every ctx.ui dialog call is
wrapped and announced to extensions as ui_prompt_start / ui_prompt_end
events carrying kind and title (typed in pi 1.2.0 types.d.ts). pi also
has an RPC mode, and pi's RPC sub-protocol can forward dialog requests
to an external client with structured answers.

Two hard constraints from Esteban, 2026-10-14:

1. pi's codebase is never modified. Ever. We interact only through pi
   APIs. This rules out forks, file patches, and runtime mutation of
   pi's objects (patching the shared ctx.ui object is undocumented
   object surgery, not API use - rejected with the same force).
2. herdr stays the only driver (ADR-0001). Couch Pi never spawns or
   owns pi processes.

## Decision

A small bridge extension, repo-owned at extensions/couch-pi-bridge.ts
and installed by symlink into ~/.pi/agent/extensions/, listens to
ui_prompt_start and ui_prompt_end and re-emits them on pi's event bus
as the herdr:blocked event ({active, label}) that herdr's own managed
extension already consumes and forwards to herdr via
pane.report_agent. The extension is a sensor only: it answers nothing,
drives nothing, and owns nothing.

The dialog signal then reaches Couch Pi inside the herdr protocol 22
payloads we already speak (agent list state, verified live in the
slice that shipped this ADR). The phone renders kind and title and
answers through the existing keystroke path (terminal input endpoint,
the same one the quick keys use). The pixel detector
(awaiting.go) stays as the fallback and covers every surface the
events do not reach: model and theme pickers, custom() overlays,
anything drawn by interactive mode directly on the grid.

Rejected options:

- Real RPC (pi --mode rpc or RPC children): exclusive with TUI mode,
  no PTY, invisible to herdr tracking, breaks ADR-0001. Also does
  nothing for the existing TUI sessions where the dialogs live.
- Patching the shared ctx.ui object from the bridge to forward
  structured requests: technically reachable in-process, but it
  mutates undocumented internals of pi's runtime. Violates the
  API-only rule; rejected.
- Direct bridge-to-Couch-Pi HTTP dial for the signal: originally
  rejected as an unnecessary transport.

Amendment 2026-10-15: the live probe falsified the assumption behind
that rejection. herdr's agent.list drops the reported label entirely
(state_labels and title stay empty while agent_status flips to
blocked), so the dialog title cannot travel through herdr at all. The
bridge therefore ALSO fires a fire-and-forget HTTP dial to Couch Pi
(POST /api/bridge/dialog with {phase, kind, title, session_path},
COUCHPI_URL overridable) as the PRIMARY leg for kind and title, and
herdr carries only the blocked status. This shipped in the bridge
extension and internal/server/bridge.go; the ADR text above is kept
for the record of what was originally assumed.

## Consequences

- Coverage of the event path: ctx.ui dialogs only (project trust, the
  pi-me family confirms and editors, MCP prompts). Pickers and
  custom() overlays stay on the regex detector.
- The extension must be installed per machine where dialogs should
  surface. Without it, herdr's blocked status still surfaces (via the
  herdr:blocked mapping) but the phone gets no kind or title, and the
  regex detector is the only signal left.
- pi version coupling is narrow: two event names. The bridge
  feature-detects (no events, no signal) and degrades silently.
- herdr mapping dependency: herdr's managed integration (v9) turns
  herdr:blocked into agent status but drops the label; the title rides
  the bridge's direct dial instead. If herdr changes the status
  mapping, the regex detector is the safety net.
- The phone answer path stays keystrokes. No structured answer channel
  exists under the API-only rule.
