# Plan: Share to Pi (Android share intent)

Source idea: pi-pocket's "Share to Pi". Theirs lands shared text in the
message box. Ours lands it in the new-agent sheet, since a share is
almost always the seed of a new task, not a follow-up in an open
thread.

Peer review round 1 found three blockers (router location, singleTask
re-entry, nothing opens the sheet) plus a real injection hole in the
existing error page. All are addressed below.

## Constraint: ADR-0003 stays intact

No new IPC bridge. The share rides the URL as a fragment
(`#share=<urlencoded>`); on a warm Activity the same fragment reaches
the already-loaded page as a `hashchange`. Kotlin only touches intent
handling; the web app owns everything else.

## Android side

- `AndroidManifest.xml`: `MainActivity` gains an intent filter for
  `ACTION_SEND` with `text/plain` — including
  `android.intent.category.DEFAULT` and `android:data
  mimeType="text/plain"`, without which the app never appears in share
  menus. `launchMode="singleTask"` so a share re-enters the running
  Activity instead of stacking a second WebView; keep the existing
  `configChanges`.
- `MainActivity.kt`:
  - `onCreate` and `onNewIntent` funnel into `shareFrom(intent)`:
    `EXTRA_TEXT` or `EXTRA_PROCESS_TEXT` (Android's "Process text"
    selection menu). No `EXTRA_SUBJECT` heading — cut in review.
  - Cap at 10 000 chars in Kotlin, before `Uri.encode`, so a
    multi-MB `EXTRA_TEXT` never gets encoded on the main thread.
    Over the cap: drop, log, done.
  - Load with `loadUrl(url + "#share=" + Uri.encode(text))`. If the
    WebView is not attached yet (first run pending), the share waits
    in a field and applies at `attachWeb`. Never silently drop.
- **Fix the existing quote injection while here.**
  `loadErrorHtml` interpolates `req.url` into
  `onclick="location.href='$failedUrl'"` (MainActivity.kt:518), and
  `Uri.encode` leaves `'` unescaped. Once shared text rides the URL
  this is a real hole. Build the reload button from the saved base
  URL (`prefs PREF_URL`) only, never from the failing request URL.
- versionCode 10 -> 11 on the APK build, per the version-axis rule.
  Server deploy alone does not deliver this feature; the sideload
  step is part of the ship checklist.

## Web side

- Boot path (cold start): the router boot is `web/src/nav.ts:90`,
  which copies `location.hash` into history. Reading the hash in
  `main.tsx` is too late and leaves an extra Back entry. Instead:
  `nav.ts` seeds history with the hash already stripped, and stashes
  the decoded share in a module-level `pendingShare` (cap enforced
  again; strip via `history.replaceState`).
- Warm path (singleTask re-entry): the hash change on the live page
  fires `hashchange`, not boot. One `window.addEventListener("hashchange")`
  applies the same decode-and-stash.
- Consumption, and the blocker the review caught: nothing today opens
  the sheet. On a share arrival the app navigates to `/` and opens
  `NewSessionSheet`, pre-filling the first-prompt field only when it
  is empty. `NewSessionSheet` keeps `prompt` across close/reopen
  (only `pickedPath` resets), so the empty-check is the guard against
  clobbering a typed draft; a stale draft from a previous open does
  not block a new share — the share wins only over an *empty* field.
- Home has no toast component (toasts live in Composer/Changes/Files/
  Terminal), so the over-cap drop is silent by design; the cap is
  10k chars, the realistic share is a URL or paragraph.

## Tests

- Web (vitest): hash parser (decode, cap, strip, both boot and
  `hashchange` paths); arrival navigates home and opens the sheet;
  pre-fill only on an empty field.
- Kotlin: `adb shell am start -a android.intent.action.SEND -t
  text/plain --es android.intent.extra.TEXT "https://example.com" <apk>`
  against cold and warm app; verify the error-page reload button after
  a failed load with a hostile URL.

## Risks

- Shares arriving while the WebView shows an error page: the fragment
  survives only if re-applied on the next successful load; consume
  from the same `pendingShare` field.
- WebView history grows a `#share=` entry per warm share; each consume
  strips it with `replaceState`. Verify back-button behavior in the
  APK round.
