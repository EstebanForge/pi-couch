# Plan: Provider-limit waiting, surfaced

Source idea: pi-pocket's "Waiting out provider limits". We cannot make
pi retry (pi owns its retry loop); what we can do is *see* the wait,
say what it waits for, and push once when a long wait starts. Today
the app either shows "Working" (spinner lies) or "Waiting for you"
(wrong layer entirely).

Peer review round 1 forced a redesign: `awaiting.go` never emits
status (blocked/working come from herdr), the push watcher blocks on
`agent.wait` with no pane text, ADR-0007 forbids building a pane regex
before checking pi's own event surface, and "Status" in CONTEXT.md is
herdr-owned vocabulary. This version models `limited` as a flag, not
a status, and prefers the bridge over the pane.

## Phase A: event surface check (gate for everything else)

ADR-0007 exists because pane regexes are guesses; the dialog bridge
listens to pi's own `ui_prompt` events instead. Do the same here:
check whether pi's extension API exposes retry / rate-limit / provider
error events (the same surface `couch-pi-bridge.ts` uses). If yes,
extend the bridge: on a limit event it POSTs kind + model + reset
time to a new `/api/bridge/limit` endpoint, same shape as the dialog
bridge. The pane-regex detector below is the fallback only, and if it
ships it ships marked as such.

## Phase B: probe (mandatory, m0 style)

We do not know pi's exact behavior when a provider 429s. Cheapest
first: grep existing session jsonl files and herdr logs for past
429/rate-limit events and capture the real strings plus what herdr
reported during the retry window. Only if nothing real exists,
synthesize a pane capture. Record findings here. Guessed regexes are
forbidden either way.

## Phase C: detector

- If the bridge path exists: the bridge event carries everything;
  no detector.
- Fallback (pane regex, marked): a `limitWaitRe` set built from the
  probed strings, living beside the other detectors in `awaiting.go`.
  Output is a per-path **flag**, e.g. `paneState.limited`, never a
  status value. A dialog detection always wins; a pane match flips
  the flag only while herdr says `working` (the probe must confirm
  what herdr reports during pi's retry sleep — record it).
- Degradation rule: a missed paint (the detector reads the visible
  slice of one source at 1 s poll) means no flag — the app shows
  today's "Working", never a wrong alarm. Fail closed to status quo.

## Wire

- `limited` is a boolean field on the sessions wire, merged in
  `snapshot()` and patched through `sessionPatch` with its own
  last-value gate. It does not enter the `Status` vocabulary; the
  glossary gains "limited (flag): the session's provider is
  rate-limiting; orthogonal to Status". `status.ts` gains no new dot
  color — the yellow waiting spinner (already shipped) is the marker.
- Provider name for the UI comes from `SessionMeta.Model`'s prefix
  (the part before `/`); state in this doc how it is parsed rather
  than guessing at build time.

## Push

The existing watcher cannot do this: it blocks on herdr `agent.wait`
for `idle|blocked|done` and never sees pane or flag state. A `limited`
push is a separate small component in the server package:

- Fed by the detector/bridge with a first-seen time per path.
- One push per wait: fire only when the wait reaches 10 minutes,
  dedupe by (path, first-seen) so flapping `limited`/`working` cannot
  re-push; the key clears when the flag drops for good or the session
  goes idle/done.
- Short waits stay silent by design.

## UI (v1, cut per review)

- The status pseudo-block (Session.tsx:~632) renders "Provider limit
  (model)" in `text-warning` while the flag is set, replacing
  "Working". No reset time, no countdown, no card-row addition — ship
  status plus one push first; the reset time is a follow-up once real
  strings prove it is parseable.
- Home card: the yellow spinner already shipped doubles as the
  limited marker.

## Explicitly not built

- "Retry now": pi owns retry timing.
- Auto-switching models: steering is the human's.
- Reset-time rendering: follows the probe, not the plan.

## Tests

- Go: bridge endpoint or detector table with probed strings as golden
  cases; dialog beats limited; limited never overrides blocked; the
  10-minute push fires once per wait, not for short waits, not on
  flap.
- Web: pseudo-block renders the flag line.
- Fixture + goldens: flag present in rows and stream; push path is
  server-internal (no fixture).

## Ship checklist

`go vet`, `gofmt -l`, `go test ./...`, `tsc`, `biome`, `vitest`;
`CONTEXT.md` gains the flag term; if the bridge path wins,
`extensions/couch-pi-bridge.ts` gains the listener and the symlink
note in AGENTS.md still applies (agents started before the edit run
old bridge code until restarted).

## Risks

- Provider strings change with pi updates; degradation is the status
  quo, not a wrong alarm.
- If neither the bridge nor the probe yields real strings, this plan
  halts at Phase B. A detector built on nothing is the ADR-0007 scar
  repeating.
