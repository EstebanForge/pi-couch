# ADR-0008: Driver abstraction over herdr and herdr-core

Date: 2026-10-30
Status: Proposed
Supersedes: the protocol-22 pin in ADR-0001 (adapt-on-change clause triggers).
Amends: ADR-0001 (herdr-family stays the only driver). Related: ADR-0005, ADR-0007, docs/plan-herdr-core.md.

## Context

herdr's authors released herdr-core (`hc`), the successor core, with an
official desktop app on top. It is a different wire contract from the
one we pinned: JSON-RPC 2.0 over a unix socket (OpenRPC schema from `hc
schema`, generation 1) instead of NDJSON protocol 22; terminals with
labels instead of workspaces, tabs and panes; and program-reported
status via OSC 7501 instead of pane inference.

The old herdr keeps working, but new development happens on herdr-core,
and its desktop app is now the primary viewer people run beside us.
Staying pinned to protocol 22 means pinning to a deprecated backend.

Live probes on a real hc 0.1.14 (2026-10-30, /tmp/hc-probe, sandboxed
XDG dirs) verified the load-bearing facts:

- Plain JSON-RPC per-call dial works with no handshake requirement;
  `hello` answers generation 1 and a feature list. Per-call dial is
  what our Go client already does; replies are sequential per
  connection, so it stays.
- pi 1.4.0 emits OSC 7501 natively: idle at the composer, working
  during a run (session name in msg), done at settle, with `turn`
  events at every settle. pi gates this on the terminal answering the
  `7501;?` query, which hc answers.
- `terminal.run` + `terminal.send` + `terminal.keys` drive a full pi
  turn end to end, and `terminal.read` renders pi's screen as plain
  text our dialog parser can work with.
- Labels round-trip (`terminal.label` + list-by-label), and every
  terminal carries `HC_TERMINAL_ID` plus `HC_TERMINAL_SOCKET` in its
  environment, so our bridge extension can label its own terminal.

Two probe findings shape the design:

1. A send issued during pi's slow first paint pasted the text but lost
   the submit. Clients must wait for the first `status` event (or
   `started: null` on the send) before trusting the first prompt.
2. The rpiv ask-user-question dialog did NOT flip pi's 7501 to
   blocked; the status stayed working with the dialog on screen. pi's
   native blocked covers ctx.ui dialogs only, so the bridge POST
   (ADR-0007) stays the source of dialog kind and title. The pane
   regex layer shrinks to a fallback for pre-bridge agents, as today.

## Decision

Couch Pi grows a small driver interface with two implementations:
`herdr` (today's client, untouched) and `hc` (new). Configuration picks
one at startup; there is no runtime switching and no shimming of one
backend onto the other's types. The SPA consumes driver-shaped
facts (sessions with status, badges, driven) as it does today, so the
web layer stays backend-agnostic.

For the hc driver:

- Session identity rides a `pi.session=<path>` label. The bridge
  extension sets it on `session_start` (fixing its HERDR_ENV gate to
  accept `HC_TERMINAL_ID`), which covers /new, /resume and
  desktop-started terminals; the FAB flow labels at start and lets the
  bridge relabel once the jsonl path exists.
- Status and drive badges come from `terminal.list`/`terminal.get`
  status records; a missing record is the existing `unknown` badge and
  hc's `error` state joins the wire enum.
- Push (ADR-0005) collapses onto one `events.subscribe` connection:
  `turn` events and newly-blocked/error status feed notifications,
  deduped on (address, turn). The 15s reconcile, 5s re-arm and 2s
  stability re-check die.
- The terminal tab moves to a framed `terminal.attach` with
  `observe: true` (phone input stays on `terminal.keys`/`send`; the
  phone never takes a typing attach because that would become the size
  leader and resize the desktop app's view).
- Dialog answering keeps parsePiDialog over `terminal.read` text and
  fires through `terminal.keys`; the verify step re-reads the screen.
- Security: the hc socket grants a same-user process the full API
  (machine.exec, file.write included) with no auth beyond the 0700
  directory. The hc client hard-allowlists its method set, never
  forwards client-supplied params, and rejects any address containing
  `/` (remote mirrors). No `caller` parameter is ever sent, so hc does
  not wrap our sends in `<hc from=...>` tags.
- Timeouts: hc's send waits on human-quiet (~3s default) and settle
  (up to 5s) before returning; client budgets are raised accordingly
  (send >= 30s as today's prompt budget, keys >= 10s).

## Consequences

- New `internal/hc` package beside `internal/herdr`; config gains a
  `driver = "herdr" | "hc"` key (default herdr until hc reaches
  parity). Both may stay installed; only one drives.
- Awaiting, push and the sessions stream lose their herdr-shaped
  internals for hc-driven sessions but keep their wire shapes.
- The pane regex layer and the observe hub remain for the herdr driver
  and die with it when herdr is eventually dropped (its own ADR).
- AGENTS.md's protocol-22 wording is superseded by this file; ADR-0001
  holds with "herdr-family" in place of "herdr".

## Probes still open before implementation

- Attach frame-by-frame capture (CREDIT accounting, resize behavior on
  a phone-sized viewer) against a live pi session.
- pi's native blocked report over a ctx.ui dialog (our own
  confirm-family), to decide how much dialog work stays bridge-only.
