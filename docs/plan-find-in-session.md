# Plan: Find in session (full history)

Source idea: pi-pocket's "Find in session". Transcript-local search
with jump-to-match.

Peer review round 1 reframed this: in-session search already exists.
The tool strip's Search tab (session-tools.ts:23) opens a filter that
matches `state.items` — the loaded tail only (cap LIMIT=2000). The
real gap is full history, and the naive version of this plan was
wrong about the wire (only `before=` paging), the cost model (no
windowed scan exists; the index does full-file parses with a
single-slot cache) and the data (the 400-rune render cap hides hits).
This version extends the existing filter instead of adding a second
search UI.

## Server

- `GET /api/sessions/{key}/transcript?q=<term>&around=<seq>`:
  - `q`: search the session's branch via the full-item path
    (`branchItems(key, 0)`, not the 400-rune render path). Kinds
    searched: `user`, `text`, and `bash` — not `thinking` or
    `tool_result` (searching pi's private reasoning and raw output is
    noise; a real need can add kinds later). Case-insensitive
    substring, no regex.
  - Response: flat `{matches: [{seq, kind, snippet}]}`, max 50, rune
    offsets for highlight (runes, not bytes — JS strings are UTF-16;
    document which the server emits). No `more` flag: the scan covers
    the whole branch; the index already paid the full-parse cost, and
    an artificial window would lie about coverage.
  - `around`: return the page of items containing `seq` so the client
    can land there directly. This is the piece round 1 flagged as
    hand-waved: reverse Virtuoso pagination means "load the page
    containing seq" must come from the server, not from walking
    `loadOlder` (which is also disabled while a query is active).
- Cost honesty: a search against a cold or non-active session is one
  full parse, same class as opening the transcript. The single-slot
  tail cache can be evicted by a search on another session — accepted
  and stated, not hidden behind a fictional window.

## Web

- Extend the existing Search tab, no new header icon or sheet:
  - While a query is active, results come from the server instead of
    the local tail filter. Below the input, a match list (kind icon,
    snippet with the hit highlighted via the rune offsets).
  - Tap a match: fetch `around=<seq>`, rebuild the list window at that
    offset (`firstItemIndex` counts down from the newest item, so the
    offset is `newestSeq - seq`; the landed item gets no flash
    animation in v1 — cut in review).
  - Clearing the query returns to the live tail (today's behavior).
- Debounce 300 ms like the model picker.

## Tests

- Go: match caps (50), kinds filter (thinking/tool_result excluded),
  rune offsets on multi-hit and multibyte lines, `around` returns the
  page containing seq, q empty falls back to plain `before=` paging,
  tolerant parse on a corrupted tail.
- Web: debounce fires once per pause; tap fetches `around=<seq>` and
  rebuilds at the right offset; clear restores the tail.
- Fixture + goldens: `fixture.mjs` gains `/transcript?q=` and
  `around=` responses; add both to `api.golden.test.ts`.

## Ship checklist

`go vet`, `gofmt -l`, `go test ./...`, `tsc`, `biome`, `vitest`.

## Risks

- First search on a huge cold session pays a full parse; acceptable at
  interactive rates, and the transcript warm cache absorbs repeats.
- `around` + live appends racing: a stream append during the jump
  shifts offsets; v1 re-anchors on the next page fetch rather than
  tracking deltas.
