# Plan: Session tags

Source idea: pi-pocket's session tags, cut to our shape: labels on
sessions, chips on rows, filter from search. No multi-select batch
edit (YAGNI); one session at a time.

Peer review round 1 killed the original storage design: the
per-session state file is a bare comment array (store.go:133-141), so
"add a field" was a forbidden migration, and N per-session file reads
per stream tick was a real cost. This version uses one tags map.

## Rules

- Lowercase, `[a-z0-9_+-]` (dash last; the round-1 regex `[a-z0-9+-_]`
  was a range bug admitting `A-Z` and punctuation), 32 chars, 10 per
  session. Server lowercases before validating; the client mirrors the
  rules for instant feedback.

## Server

- Storage: one `tags.json` in the state dir — `{ "<sessionKey>":
  ["tag", ...] }`, loaded into memory at boot, mutated in memory and
  written atomically on change. No per-session file, no shape change,
  one bad write cannot take comments down with it. Corrupt file at
  boot: log and start empty (tags are additive UI).
- Merge point: `snapshot()` in server.go adds `tags` to each row it
  already builds, for both `GET /api/sessions` and the stream
  snapshot. The merge does not touch `pi.SessionMeta` (wrong layer).
- API: `POST /api/sessions/{key}/tags` with `{tags: [...]}`; validates
  shape (same rules), 400 on violations. After the write it signals
  the sessions stream wake channel (the same mechanism `wakeDialog`
  uses) — a tag edit touches no jsonl, so without the wake chips lag
  behind the heartbeat.
- Search: the home needle filters server-side (`apiSessions` matches
  name, first_user, cwd). Tags join that match, so the search window
  sees them.

## Web

- `api.ts`: `tags?: string[]` on the row type; `setTags(key, tags)`
  wrapper. `state/sessions.ts` merge carries tags through the stream
  upsert (extend the merge tests).
- `HomeList.tsx`: chips under the title, max 3 plus `+N`. The card is
  a `<button>`, so a tappable chip inside it is invalid HTML — chips
  are `span role="button"` with `stopPropagation`, and `setQuery`
  flows down from HomeList to fill the search box. Chips render from
  the same snapshot data, no extra fetch.
- Edit entry: the session header menu gains "Edit tags": a sheet with
  current chips, an add field enforcing the rules client-side, tap to
  remove. One entry point; no long-press.
- Konsta ships `Chip`; use it or record the rejection reason in the
  commit. A new Konsta import needs its `@source` line in
  `web/src/index.css` or `konsta-css.test.ts` fails the build.

## Tests

- Go: tags round-trip; corrupt `tags.json` boots empty; POST
  validation table (uppercase input lowercased, bad chars, 11th tag,
  32+ chars); `snapshot()` rows carry tags; search matches tags;
  POST wakes the stream.
- Web: chip cap render; chip tap fills search; edit sheet blocks bad
  input before POST; stream upsert updates chips.
- Fixture + goldens: `web/dev/fixture.mjs` serves rows with tags and
  grows a POST route; update `web/dev/golden/sessions.json` and
  `api.golden.test.ts`.

## Ship checklist

`go vet`, `gofmt -l`, `go test ./...`, `tsc`, `biome`, `vitest` all
clean; `CONTEXT.md` gains the "Tag" term; new Konsta import has its
`@source` line.

## Risks

- Tag sprawl: no autocomplete, phones breed `wip`, `wip2`. Accept;
  literal-text filtering keeps messy tags useful.
- Stream payload growth: 10 short strings per row worst case on a
  snapshot that already batches; noise, not cost.
