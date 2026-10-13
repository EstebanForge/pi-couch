# Parity gate

Stage 6 of docs/rewrite-plan.md: every templ feature ticked against the SPA
before cutover. Tick rules: PARITY means the SPA covers the feature; GAP means
the SPA misses it and the fix lands in this gate; STAGE 7 means the plan
deliberately moves it to cutover. The SPA may exceed the templ UI (quick keys,
work-block folding); extras are noted, not ticked.

The legacy UI is the inventory source: internal/web/pages/*.templ plus the
islands in internal/web/static/js/main.ts.

## Home list (list.templ, a.list, streamSessions)

| Feature | Templ | SPA | Note |
|---|---|---|---|
| Session list with status badges | yes | yes | SPA uses status dots; templ uses pill badges. Same signal. |
| Window (latest 15) + Load more (+10) | yes | GAP | SPA renders every session (~600 cards). Fix: client-side slice; the JSON stream already sends the full snapshot and the client slices by design. |
| Active-first grouping (working/blocked float, Earlier divider) | yes | GAP | SPA keeps recency order. Fix: group in the sessions store (working and blocked first, recency within groups). |
| Header count = total sessions | yes | yes | |
| Live refresh (stream) | yes | yes | templ rides the paced datastar stream; SPA rides /api/stream/sessions with hash-gated snapshots. |
| Push bell (permission on tap, subscribe/unsubscribe) | yes | GAP | SPA never registers /sw.js, so push is impossible from /app/. Fix: bell in the header + push.ts (register, subscribe on tap, unsubscribe). |
| PWA install (manifest link, installable on desktop) | yes | yes | /sw.js, the manifest and the icons ship in the SPA embed (web/public) and the worker registers at boot. |

## Session view (transcript.templ, streamTranscript)

| Feature | Templ | SPA | Note |
|---|---|---|---|
| Live transcript (reset/append, append-only patches) | yes | yes | JSON stream with seq; fingerprint semantics carried over. |
| Work blocks (thinking/tool folds) | yes | yes | SPA folds open while working, folded after; templ renders static details. SPA exceeds. |
| Driver badge | yes | yes | status triple incl. model and thinking. |
| Stop button (working/blocked only) | yes | yes | overflow menu. |
| Close session (confirm) | yes | yes | overflow menu + Konsta Dialog. |
| Composer send with pending bubble | yes | yes | reconcile rules per plan; unknown never retries. |
| Draft persistence per session | yes | yes | templ: textarea survives redirect; SPA: localStorage couch-pi.draft.<key>. |
| Attachments (10MB, 10 cap, chips, path-in-prompt) | yes | yes | |
| Model picker (catalog search + free-text pattern) | yes | yes | Konsta Sheet vs templ datalist. |
| Thinking picker (7 levels) | yes | yes | |
| Unconfirmed 8s Esc timeout on model/thinking | yes | yes | |
| Quick keys strip | no | yes | SPA-only (Esc, Enter, arrows, Alt+Enter, Ctrl+D). |
| In-transcript search | yes (cycle with count) | yes (filter) | V10 cycles window.find matches with a counter; the SPA filters items to matches. Both find text; filter is stronger. Keep. |
| Pin-to-bottom scroll | yes | yes | SPA gates follow on a pinned ref; templ pinned unconditionally. |
| Tail limit ?n= | yes | yes | parseLimit server-side; SPA caps the client DOM at 400 blocks. |
| Redirect notices (?sent/?err/?closed) | yes | yes | SPA resolves outcomes in-band over fetch; no page reload. |
| Keyboard dismissal on transcript tap/drag | no | yes | SPA-only (stage 3). |

## Changes (changes.templ, streamChanges)

| Feature | Templ | SPA | Note |
|---|---|---|---|
| Diff list with +N/−M and labels | yes | yes | |
| Per-file unified diff view | yes | yes | |
| Line comment drafts (tray, legacy localStorage keys) | yes | yes | exact couch-pi-cmts:<key> format. |
| Send all (bullets into one prompt) | yes | yes | posts the legacy JSON route until cutover. |
| Live diff refresh | yes | yes | templ: 3s hash-gated poll; SPA: /api/stream/changes reset events. |
| Known-file / staged-only validation | yes | yes | shared apiChanges logic. |

## Files (files.templ)

| Feature | Templ | SPA | Note |
|---|---|---|---|
| Repo browse, dirs first, sizes | yes | yes | |
| File view (256KB cap, escape guard) | yes | yes | |

## Terminal (terminal.templ, streamTerminal)

| Feature | Templ | SPA | Note |
|---|---|---|---|
| Live screen mirror | yes | yes | templ: legacy revision-gated stream (frozen since M3 until the 8443 slice's hash-gate fix; both share the fixed stream). |
| Input gate (peek vs interactive) | yes | yes | templ: explicit interactive tap; SPA: take-the-wheel gate. |
| Raw text + key input | yes | yes | same endpoint. |

## Cross-cutting

| Feature | Templ | SPA | Note |
|---|---|---|---|
| Dark dracula theme | yes | yes | SPA compiles the palette (Tailwind @theme); templ uses the daisyUI CDN. |
| Safe-area insets (APK-injected vars) | yes | yes | both consume var(--safe-*). |
| One-hand sizing (16px inputs, 44px targets) | yes | yes | |
| View transitions / motion | yes | yes | templ: cross-document VT; SPA: startViewTransition slides. |
| Dock navigation | yes | yes | templ: bottom dock; SPA: session tool strip + overflow per the navigation ruling. |
| CSP on served pages | yes | yes | SPA index carries the strict CSP. |
| Deep links (Back seed, cold start) | yes | yes | seedHistory in nav.ts. |

## Verdict

All gaps closed 2026-10-13 (window and Load more, active-first
grouping, push bell, then the cutover moved the PWA pieces into the
embed). Everything is PARITY or an SPA-side excess; the gate is done.
