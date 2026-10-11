# Push transitions ride per-agent `agent.wait`, not subscriptions or polling

P5 (web push on Status transitions) needs a transition source. PLAN.md's "events from herdr socket subscriptions, not polling" assumed the socket streams events. Probes against live herdr 0.9.3 (2026-10-11) show it does not:

- `events.subscribe` acks and then streams nothing on the socket (M0 findings, reconfirmed).
- `events.wait` matches require both pane_id and one agent_status per call, so watching N panes means 3N concurrent waits re-armed on two axes.
- `agent.wait {target, until[], timeout}` is one blocking call per agent, returns the full Agent object on a transition into any matched state, and returns a clean timeout error otherwise. Probed live: a `--until done --until blocked` wait started before a prompt returned within a second of the turn boundary with `agent_status: done`; an 8 s wait on a `done` agent that stayed `done` timed out, proving the call fires on transitions into a state, not on state equality, so a re-armed watcher cannot spin.

## Decision

One watcher goroutine per live driven agent loops on `agent.wait` with `until: [idle, blocked, done]`. On return, a 2 s stability check (re-read `agent.list`; drop the event if the status moved again) debounces flapping, then the push goes out. A slow reconcile tick over `agent.list` spawns watchers for new agents and stops watchers for gone ones. No polling of statuses, no session-file reads for transitions.

## Considered options

- Short polling of `agent.list` with a status diff: rejected, PLAN.md excludes polling and a verified blocking primitive exists.
- `events.wait` per pane per status: rejected, N x 3 calls and double-axis re-arming for the same information.
- `events.subscribe`: not a transport in 0.9.3; revisit only if herdr starts streaming.

## Consequences

- Detection latency is the herdr event latency plus the 2 s debounce, well under push delivery time.
- One long-lived call per agent sits on herdr's socket; the client already opens one connection per request, so this adds no shared-state risk.
- If herdr changes wait semantics, the watcher degrades to re-arm loops guarded by the reconcile tick; a wrong fire is suppressed by the stability check, a missed fire is no worse than polling.
