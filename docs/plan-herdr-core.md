# Plan: herdr-core (`hc`) driver support

Status: planned, 2026-10-30. Ground truth: probes on hc 0.1.14
(/tmp/hc-probe, sandboxed), Claude adversarial review of this analysis
(resumable session 22f23c6c-be59-4dbf-90dd-340a079990ad), and the
schema at herdr-core schema/hc.openrpc.json. Decision: ADR-0008.

## Verified facts (probes, 2026-10-30)

- Plain JSON-RPC 2.0 per-call dial over the unix socket; `hello`
  optional, generation 1. Socket: `$XDG_RUNTIME_DIR/herdr-core/<session>.sock`,
  `HC_SOCKET` override. The CLI autostarts the core; a raw client must
  treat dial failure as degraded, as today.
- pi under hc reports full status: idle -> working (session name in
  msg) -> done, plus `turn` events at settles. `turn` is the push
  trigger.
- `terminal.send` pastes and submits; during pi's slow first paint the
  submit got lost (text sat in the composer). After settle, sends work;
  `started: true` came back and the turn completed. Client rule: gate
  the first send on the first `status` event, and treat
  `started: null` as needs-retry.
- `terminal.read` renders pi's screen as plain text (how: screen |
  history | stitched); the rpiv selector, its hint bar and pi's status
  line all read cleanly. Dialog parsing stays feasible.
- `terminal.label` set + list-by-label round-trips. Terminals carry
  `HC_TERMINAL_ID`, `HC_TERMINAL_SOCKET` (+ `HC_SCOPE`) in their env.
- The rpiv ask-user-question dialog did NOT flip status to blocked
  (stayed working with the dialog up). pi's native blocked covers
  ctx.ui dialogs only. The bridge POST stays the dialog source.
- hc's send path waits ~3s human-quiet + 200ms paste gap + up to 5s
  settle; `terminal.keys` also waits for human quiet. Budgets must
  absorb that.
- Sending `caller` makes hc wrap text in `<hc from=...>` tags. Never
  send caller.
- `terminal.interrupt` sends esc to status-reporting programs: the
  correct pi abort (today we send ctrl+c for stop, esc only in the
  composer stall guard).
- hc API from a same-user process is unrestricted (machine.exec,
  file.write...). Socket auth is the 0700 dir only.

## Work waves (each ships on its own; tests first where feasible)

1. `internal/hc`: JSON-RPC client (per-call dial), typed Terminal/
   status/event structs, error-kind mapping (blocked, unavailable),
   socket discovery + `driver`/`hc_socket` config, a fake hc server
   for tests beside `herdrFake`.
2. Bridge: accept `HC_TERMINAL_ID`/`HC_TERMINAL_SOCKET` (keep the
   herdr gate for the old driver), POST dialogs as today, and set the
   `pi.session=<path>` label on `session_start` (covers /new, /resume,
   desktop starts).
3. Session map: label-driven path <-> address store, fed by
   `events.subscribe` (labels + status + turn), replacing
   TargetForPath/WorkspaceForPath and the per-tick Agents dial.
4. Commands onto hc: prompt (send, wait for reported), keys, stop
   (interrupt = esc), close (kill [+rm]), with raised budgets.
5. Awaiting/dialogs: blocked from status records merged with bridge
   entries (ADR-0007 shape survives); parsePiDialog over terminal.read
   text; answer verify re-reads the screen. Retire pane regexes for hc
   sessions only.
6. Push onto events (ADR-0005 rewrite): turn + newly-blocked/error,
   dedupe (address, turn); delete reconcile/re-arm/stability.
7. FAB create flow: terminal.run (argv pi, nonce label, cwd), wait for
   the first status event, first send, bridge relabels with the path.
8. Terminal tab: framed `terminal.attach` observe:true; CREDIT
   accounting; phone input stays on keys/send. Re-render probe of
   xterm against attach bytes happens here.
9. SPA: `error` status color, workspace selector removal behind the hc
   driver, driver-aware "no driver" copy.
10. Docs: AGENTS.md driver wording, m0-findings addendum with the
    probe transcripts.

## Accepted limitations

- rpiv-style extension widgets that bypass ctx.ui never show blocked;
  the phone sees working until the bridge POST lands (same as the
  current regex gap, no regression).
- hc's headless grid is 200x120; pi wraps differently than under the
  desktop app. Dialog parsing asserts on text, not layout.
- `hc status` has no per-dialog kinds beyond permission/question/auth;
  the phone's answer sheet keeps using bridge kind detail.

## Out of scope

- Dropping the herdr driver (its own ADR, later).
- machine.* / remote terminals: addresses containing "/" are rejected.
