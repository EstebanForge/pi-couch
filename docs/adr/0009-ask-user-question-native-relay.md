# ADR-0009: ask_user_question rides the rpiv native contract

Date: 2026-10-31
Status: Accepted
Supersedes: nothing. Amends ADR-0007 (the bridge is the dialog sensor) with a second sensor channel and a structured answer leg.
Related: ADR-0007, extensions/couch-pi-bridge.ts, internal/server/bridge.go, web/src/components/DialogActions.tsx

## Context

The ask_user_question tool comes from the rpiv-ask-user-question extension, not pi core. It renders a structured questionnaire (questions, options, multiSelect, previews) as a ctx.ui.custom TUI, so ADR-0007's sensor catches only kind=custom with no title. The pane parse cannot rebuild the questionnaire either: it is a multi-question component with tabs, checkbox paint and its own chrome, so the phone never got an answerable card - only the "pi asks: custom - open Terminal" banner, and a phone-first user is stuck.

The rpiv package already ships the contract that makes screen-scraping unnecessary (its events.ts declares a stability policy: rpiv:* channel names are immutable, payloads are JSON-safe and append-only). It emits rpiv:ask-user:prompt carrying every question, header, multiSelect flag and option label/description; it accepts an inbound rpiv:ask-user:answer event with strict all-or-nothing validation (first answer wins, TUI keystrokes included); it reports every attempt on rpiv:ask-user:answer-result. pi's extension event bus carries all three, so the API-only rule holds: no pi objects are touched.

## Decision

The bridge extension gains a second relay: it listens to the three rpiv events, POSTs the open questionnaire to /api/bridge/ask, POSTs closes and verdicts the same way it dials /api/bridge/dialog, and while a questionnaire is open it polls GET /api/bridge/ask/answer on a sub-second tick and re-emits whatever it finds as rpiv:ask-user:answer into pi's bus.

The server stores the ask in the same session-path-keyed bridge map the dialog sensor already uses, serves it on the wire as dialog kind "ask" with the questions attached, and outranks the pane parse with it while blocked. POST /api/sessions/{key}/ask/answer takes the phone's structured answer ({tool_call_id, answers | cancel}), parks it as a pending answer, and returns the rpiv verdict; the extension poll delivers it, so the phone answers without a single keystroke.

The phone renders the questionnaire natively in the existing dialog sheet: per-question options (single-select = tap, multiSelect = toggles), a per-question free-text row for the reserved custom answer, Submit gated on every question being answered, and Cancel which cancels the questionnaire the way Esc does. The pane-parse keystroke card stays untouched for pi's native dialogs; it just never fires for kind "ask".

Rejected: teaching the pane parser to read the rpiv questionnaire and driving it with the verify-then-fire keystroke walk - it would have to chase tabs, per-question cursor state and checkbox paint across a multi-question flow, exactly the fragility ADR-0007's dial exists to avoid.

## Consequences

- Agent machines need @juicesharp/rpiv-ask-user-question installed. Without it nothing regresses: no prompt events fire, and the banner + Terminal path stays as today.
- The bridge extension must be reloaded (agent restart) to gain the relay; the server and SPA ship independently.
- Answer latency is one poll tick (~sub-second) plus pi's own validation; the phone POST returns the real verdict, and refused answers keep the card with the reason.
- First answer wins: a desk TUI answer settles the questionnaire and the phone's attempt comes back refused, same as the keystroke path's id gate.
- Previews do not travel: the prompt event ships hasPreview, not the content, so the phone shows the label and description only.
- The rpiv stability policy is the version contract; a breaking change would arrive as a new channel name (prompt.v2) and the old relay degrades silently.
