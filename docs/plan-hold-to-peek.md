# Plan: Hold-to-preview on home cards

Variant of pi-pocket's peek tiles, reshaped for a client (not a
runtime): no live strip, no subscriptions. A deliberate long-press on
a home card opens a modal with the session's recent tail; one fetch,
one close, nothing polls in the background.

Named **preview**: "peek" is already CONTEXT.md vocabulary for the
Terminal tab's read-only pane view. The glossary gains "Preview".

Peer review round 1 fixed four real bugs in the original: the
navigating click fires right after the hold, the fired-haptic cue was
indistinguishable from the existing ripple tick, long-press text
selection was assumed away instead of suppressed, and "one cheap file
read" was a full parse. Status was also attributed to the wrong
source.

## Gesture

- `SessionCard` in HomeList.tsx (priority and recents share it, so the
  handler lands once): `onPointerDown` arms a 500 ms timer. Cancel on
  move beyond ~10 px, release, leave, `pointercancel` (Android Chrome
  fires it when scroll takes over), and unmount.
- Fired: `hapticLongPress()` (haptics.ts:35 — the ripple already
  spends `hapticTick` on every pointerdown, so tick is not a signal),
  then open the preview for that session.
- **Click suppression:** the card is a `<button onClick={navigate}>`
  and a long press ends in a click. A `justFired` ref set on fire
  swallows the next click (consume-and-clear in the click handler).
- Text selection: `select-none`, `-webkit-touch-callout: none`, and
  `onContextMenu` preventDefault on the row — up front, not deferred
  to an APK round. `press-scale` does none of this today.

## Server: one endpoint

- `GET /api/sessions/{key}/preview`:
  - `status` from herdr badges, the same `a.herdr.DriveStatus` call
    `snapshot()` makes — the index has no status.
  - `lines`: last ~6 `user`/`text` entries, newest last, raw capped at
    the existing 400 runes per line. Server sends raw text; the client
    truncates by lines (round 1 cut the server-side markdown
    stripping — `Markdown.tsx` already renders client-side, and if the
    preview needs it, it renders there too). No tool-call lines in v1.
  - Cost, stated honestly: `tailEntries` is a full-file parse on a
    cold or non-active session with a single-slot cache. One preview
    is one transcript-open class of work. No server cache added.
  - Closed sessions work (files are ours to read).

## Modal

- Konsta `Popup` if its styling fits a glance modal with a corner
  close; otherwise a hand-built centered card and the rejection reason
  recorded in the commit, per project convention. A new Konsta import
  needs its `@source` line in `web/src/index.css`.
- Content (v1, cut per review): title, status chip (same colors as the
  list), the lines. No re-fetch button, no auto-refresh — the value of
  a preview is a frozen snapshot you compare against the card's live
  marker after closing.
- Close: corner button top-right (`FaXmark`) plus backdrop tap, plus
  back/Esc through the app's sheet-back history pattern — and a test
  that Back closes the modal without leaving the home screen.

## Tests

- Go: preview caps (lines, runes), closed session works, empty thread
  returns `lines: []` not an error, status comes from DriveStatus.
- Web (vitest): hold timer fires once; cancels on move/release/
  `pointercancel`/unmount; the navigating click is swallowed after a
  fire; modal closes on backdrop, corner button, and back.
- Fixture + goldens: `fixture.mjs` gains `/api/sessions/demo/preview`.

## Ship checklist

`go vet`, `gofmt -l`, `go test ./...`, `tsc`, `biome`, `vitest`;
`CONTEXT.md` gains "Preview" and notes the Terminal "peek" stays.

## Risks

- Accidental triggers during fast scrolls: the 10 px threshold is the
  guard; raise the hold to 600 ms before adding options.
- Android WebView long-press selection: suppressed up front (above);
  verify once in the APK round that no copy UI leaks through.
