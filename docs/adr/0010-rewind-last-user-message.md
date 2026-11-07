# ADR-0010: Rewind the last user message from the phone

Date: 2026-11-07
Status: Accepted
Amends: ADR-0007 (dialog sensor bridge), ADR-0009 (structured ask answers)

## Context

Pi's terminal offers an escape-escape tree navigator: select a sent user message, choose "No summary", and pi rewinds the session so that message and everything after it leave the model context, returning the text to the editor for correction and resend. Esteban wants the same correction loop on the phone: long-press the last user message while the session is idle, get the text back into the composer, edit, and resend — as if the original was never sent.

In pi's source this whole flow is one call: `navigateTree(entryId, { summarize: false })`. For a user-message target, pi moves the branch leaf to the message's parent (so the message and its response drop out of context), returns the message text, and writes nothing destructive — the abandoned branch stays in the append-only jsonl. No keystroke walking is required to reproduce it; only the entry id.

## Decision

The bridge registers a pi command, `/couchpi-rewind <entryId>`, whose handler validates the target with the read-only session manager and calls `navigateTree(id, { summarize: false })`. The server exposes `POST /api/sessions/{key}/rewind` which gates on session idleness, resolves the transcript seq to an entry id, and fires the command through the existing herdr prompt path — the same channel `/name` uses, proven to dispatch extension commands live (Nov 7 probe: `/glm-tweaks status` on a fresh agent opened its selector and wrote no session file, confirming command dispatch). The bridge reports the outcome to `POST /api/bridge/rewind` (fire-and-forget POST, like the ask verdicts), and the phone prefills its composer from the transcript item it long-pressed.

Constraints carried forward:

- Pi's codebase is never modified; pi performs the branch itself through its own command API. Session jsonl files remain read-only to Pi Couch — the server never writes them.
- The parser renders the parent chain from the file tip. After the resend the new branch is the tip, so the abandoned exchange disappears naturally; pi itself rebuilds the leaf as "last entry" on restart, so both surfaces always agree after a restart.
- Scope is deliberately one target: the last user message on the active chain, idle sessions only. `navigateTree` refuses while streaming and herdr refuses prompts to blocked agents, matching the phone's gate.

## Alternatives considered

- Driving the escape-escape TUI with pane keystrokes: rejected — selection order depends on fold state, filter mode, and the summary dialog; verify-then-fire cannot read the tree selector reliably.
- Mutating the session with pi's SessionManager from a helper script: rejected — the running pi process owns the file, and couch-pi writing session state would break the read-only boundary.
- Fetching a summary branch ("Summarize"): rejected for v1 — the point is correction, and "No summary" keeps the flow deterministic and instant.

## Consequences

- The bridge now senses (ADR-0007), answers (ADR-0009), and drives session state (this ADR). It still only ever acts through pi's own public extension APIs.
- Sessions need a bridge reload before `/couchpi-rewind` exists; older sessions show the phone a clear error from the server.
- hc sessions refuse rewinds (409 unsupported-driver) until hc grows a keys verb: pi's desk refill would leave the rewound text in the pane editor with no way to clear it, and the next typed prompt would append to it.
- Pi's leaf is in-memory only: a rewind followed by quitting without a resend reverts on restart. This is pi's own desk behavior, inherited unchanged.
- If a prompt-delivered command ever stops dispatching in a future pi, the rewind POST fails visibly with the unknown-command path and the phone shows the refusal; no silent corruption is possible because the flow never touches the file directly.
