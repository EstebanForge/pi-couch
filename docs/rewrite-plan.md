# Client rewrite plan: React over the Couch Pi server

Status: accepted by Esteban on 2026-10-12, revised the same day after checking it against the repo (server.go, web.go, main.ts, MainActivity.kt) and after reading the herdr-web-ui source (state, transport, chat, composer, navigation, serving). This plan supersedes ADR-0002 for the web client. A companion ADR (ADR-0006) records the decision; this document is the build plan.

## Why

- The core path (live transcript while steering) was dead from day one because of datastar-specific traps: an ES-module bundle loaded as a classic script, and the v1 rename of `data-on-load` to `data-init`. Fixed, but the cost class remains.
- The UI feels slow on the phone. Main suspect: the Tailwind JIT running in the browser (consequence of the old "compile never" ruling), rescanning styles on every streamed patch. Also: three CDN scripts on cold start. Nobody has profiled yet; stage 1 takes one Chrome trace over `chrome://inspect` before and after the compiled CSS lands.
- Iteration speed is the top priority. Couch Pi will be dogfooded daily for weeks and tweaked constantly. The implementation agent's training data is far deeper in React than in datastar idioms, so each tweak lands first try more often in React.
- "No build step" was already false: templ generate, esbuild, and go build run on every change. The rule worth keeping is "no node on the server". Vite runs at build time and in dev only.

## Verdict on mobile UI kits

Researched 2026-10-12: Framework7 React, Konsta UI, Onsen UI React.

- Onsen: React binding stale (peer dep react 18, last stable 2023). Rejected.
- Framework7: drags its own app shell, router, and page transitions; skipping the router is undocumented. Rejected.
- Konsta UI: accepted, deliberately. Active releases (5.5.0 on 2026-09-28), Tailwind 4 and React 19 native since 5.0, no router, small. Esteban wants it for surfaces beyond chat: settings, file display, diff chrome, folder picker for new agents, toasts and dialogs. Its Material look is rethemed to the dracula palette, budgeted at about a day.

Konsta rules:

- Fixed theme: `material`, set on the provider. Only the S25 and a desktop browser matter; no iOS branch, no auto theme detection.
- Allowed components: Sheet, Popup, Dialog, Toast, List and ListItem, Badge, Button, Segmented, Panel (the home side menu), Navbar (settings and file screens only). Anything else needs a line here first. The session detail header is custom, not Konsta.
- Not used for the transcript, the composer, or the terminal. Those stay custom Tailwind.
- Konsta's own safe-area handling reads `env(safe-area-inset-*)`, which is 0 in this WebView. Turn it off and use our `--safe-*` vars (see Composer spec). The agent must read `node_modules/konsta/react/konsta-react.d.ts` for prop names, not guess them.
- Tailwind 4 does not scan `node_modules`. Konsta's classes must come from `@import "konsta/react/theme.css"` (the package exports `./react/theme.css`) or an explicit `@source`. Check in stage 1 with a production build, not only the dev server: the failure mode is unstyled components in `dist` only.

## Goals

- One Go binary, one deploy step, loopback bind, tailscale serve, no auth layer: all unchanged.
- A React 19 SPA served from the binary via go:embed, with a fast dev loop (Vite HMR proxied to the Go server).
- App-grade fluidity on the phone: compiled CSS, virtualized transcript, keyboard-aware composer, touch rules.
- Keep every server capability: scanner, herdr client, git service, push watcher, terminal stream, upload, model catalog.
- Keep the APK shell contract: `--safe-top` and `--safe-bottom` on `<html>`, set by MainActivity after each page load.

## Layout north star: herdr-web-ui

The layout and wireframe come from herdr-web-ui, the React client Esteban cloned to analyze: https://github.com/devswha/herdr-web-ui (local clone was /tmp/herdr-web-ui; /tmp is ephemeral, re-clone from GitHub when needed). It is the layout of record for visual proportions and interaction patterns. Navigation is the one exception: Esteban ruled on 2026-10-12 for the shape below, not their drawer shell. Couch Pi reimplements that layout in this stack and adds what the project misses (settings, file display, diff comments, folder picker for new agents). When a layout or UX question comes up later, query and review that repo first instead of inventing a pattern.

Source read on 2026-10-12 (React 18 client, Bun server, about 30 agent adapters). It is a bigger and different product: multi-machine, five languages, pairing auth, a PTY bridge. Most of its weight does not apply. What we take, where it lands in this plan:

- Navigation shape (Esteban's ruling, not theirs): the home screen is the session list; a tap opens the session detail full screen; Back returns to the list, with a `startViewTransition` slide. A side menu on the home screen holds the less-used features (settings first), and it is never a session switcher. The session detail has a tool header and is the hub for per-session screens (changes, files, terminal, more later). They use a drawer that switches sessions and a lens control; we do not. See Stack, Screens and navigation.
- Chat: stick-to-bottom ref with a 48px threshold, no follow on height-only growth, tri-state fold (`chosenOpen ?? default`), work blocks that open while live and fold after, a render boundary per row, a "New messages" pill. See Transcript spec.
- Composer: text stays in the box until the server acknowledges it, and only the acknowledged part is removed; a duplicate send is worse than a lost one; uploads at 3 in parallel, send blocked while uploading. See Composer spec.
- Transport: their reconnect backoff and wake-on-visible rules, wrapped around EventSource; a generation guard for reads that race a stream. See Server changes and Performance rules.
- Serving: precompressed assets, a strict CSP, hashed assets immutable, index.html `no-cache`, JSON 404 for unmatched API paths, a build-id check that offers a reload. See Embed and serving.
- Types: one wire-types file as the contract, generated for the client with a stale check in CI, as their `generate:types` does. See Server changes.
- Back handling: sheets, the side menu and dialogs push a history entry so Android Back closes them first. See Stack.
- Folder picker for new agents: recent-folder chips (swipe row on a phone, up to 8, from known workdirs) plus a one-directory-at-a-time browser with parent, home, filter, hidden toggle and "Use this folder"; 500 entry cap with a truncated note. See Server changes, new work.

What we leave, with reasons:

- No auth pairing, device roles, tokens or Tailscale identity checks: ADR and AGENTS.md lock "tailnet is the boundary".
- No multi-machine layer, no i18n, no telemetry, no split canvas, no command palette, no themes beyond dracula.
- No ETag cache, LRU, content-hash revisions, `history_id` or 409 reload logic: we push deltas with a `seq`, they invalidate and refetch a file.
- No cross-tab draft leases or storage-event reconciliation: one localStorage draft per session.
- No hand-written Markdown parser and no 30 bundled highlight grammars: use a library, lazy loaded.
- No Mermaid or KaTeX in v1.
- No `App.tsx` with 40 `useState` calls and ref mirrors: zustand stores stay.
- No custom pushState patching for settings and sheets: one small hook (see Stack).
- No PTY attach, flow control or replay buffers: our terminal is a `pane.read` mirror, a mode they only use as a fallback (their `server/mirror.ts` is the reference for stage 5).

## Non-goals

- No Next.js, no SSR, no router library. The app has a home list, a session detail and a few screens under it; the History API plus a small path-to-screen map is enough (herdr-web-ui has no router either).
- No auth, no offline shell, no PWA install changes (manifest and sw.js keep their URLs and content).
- No voice input work. ADR-0004 (Whistle) is not built; the composer leaves room, nothing more.
- No API redesign beyond the JSON surface listed below.

## Stack

Versions checked on npm 2026-10-12. Pin exact versions in package.json and commit `web/package-lock.json`.

- React 19.3, TypeScript, Vite 8 (needs Node 20.19+ or 22.12+; the dev machine runs 26), `@vitejs/plugin-react` 6, Tailwind 4.3 through `@tailwindcss/vite` (compiled, dracula palette as `@theme` tokens).
- `react-virtuoso` 4.18 (the free `Virtuoso` component). Not `VirtuosoMessageList`: it needs a commercial license.
- Konsta UI 5.5 for chrome components, under the rules above.
- Bottom sheets (model and thinking pickers): Konsta Sheet first. Add `vaul` only if swipe-to-dismiss turns out to matter. Do not run two sheet systems.
- zustand 5 for the small client state (sessions cache, transcript store, collapse state, connection status, ui prefs). No Redux, no react-query.
- `@xterm/xterm` and its fit addon from npm, replacing the vendored copy.
- Transport: fetch (JSON) for commands, EventSource for the live streams, EventSource plus POST for the terminal (that is what the server does today; it is SSE, not a WebSocket). No datastar, no templ in the client path.
- Stream helper (`api/stream.ts`): bare EventSource is not enough. It retries network drops itself, but after an HTTP error (a 502 while the server restarts behind `tailscale serve`) it goes CLOSED and never retries. The helper owns the EventSource: on error it closes and reconnects with `min(5000, 250 * 2^n)` ms (reset on the first event), reconnects at once on `online`, `pageshow` and `visibilitychange` to visible, and treats 45 s without any event as dead (the server `ping` is 15 s). Every reconnect starts with a server `reset`, so there is no resume state.
- Navigation: real URLs through `history.pushState` and `popstate`, so the APK Back button (`canGoBack`) and push deep links work. The screen map and the history rules are in "Screens and navigation" below. Paths, not query strings, so push URLs (`/sessions/{key}`) stay valid.
- Overlays and Back: one hook, `useBackClose(open, close)`. On open it pushes a marker entry; on `popstate` it closes; when closed in code it calls `history.back()` if its entry is still on top. Used by every sheet, popup and dialog and by the home side menu. Do not patch `history.pushState` globally (herdr-web-ui does; it is its most fragile code).
- Side menu: Konsta `Panel`, opened by a hamburger in the home header only. No edge swipe: on Android gesture navigation a swipe from the left edge is the system Back gesture and would fight it.

### Screens and navigation

Screen map (paths are real URLs; during stages 1 to 6 they sit under `/app/`):

| Path | Screen | Reached from | Depth |
|---|---|---|---|
| `/` | Home: session list, active sessions first, "load more" window, status badges | app start, Back | 1 |
| `/settings` and later menu items | Settings and other less-used features | home side menu | 2 |
| `/sessions/{key}` | Session detail: chat, composer, tool header | tap a session | 2 |
| `/sessions/{key}/changes` | Changes: diff, line comments, send-all | session header tool | 3 |
| `/sessions/{key}/files` (view: `?file=`) | Project file browser and viewer | session header tool | 3 |
| `/sessions/{key}/terminal` | Terminal | session header tool | 3 |

- Sessions are chosen only on the home list. Nothing else switches sessions; to change session, go Back to the list.
- Session header (custom, top of the detail screen, safe-area aware). Row 1: back arrow, session title, a subtitle with workdir and branch, the driver status badge, an overflow button. Row 2: a horizontally scrollable tool strip with Changes, Files, Terminal and Search, in that order. At 360px the strip scrolls; it never wraps. The overflow button holds the rare and risky actions: Stop and Close session. Model and Thinking are not header tools; they belong to the composer card (see Composer spec).
- A tool is either an action or a screen. Actions act or open a sheet on the detail screen: Search in chat (strip), Stop and Close session (overflow). Screens are routes under the session: Changes, Files, Terminal (strip). Both kinds come from one registry, `web/src/session-tools.ts` (id, label, icon, kind, `placement` of `strip` or `overflow`, path suffix or handler, optional badge); the strip order is the registry order. Adding a tool is one registry entry plus its screen component. The agent adds features that way, and the header does not grow by hand-edited markup.
- Tool screens use a compact header: back arrow, session title, the same tool strip with the current tool marked. Hopping from one tool screen to another is sideways, so it uses `replaceState` and a cross-fade; the history stays at depth 3, and Back goes to the chat.
- History rules: opening a session pushes; opening a tool screen from the detail pushes; a sideways tool hop replaces; opening settings from the side menu pushes; Back pops one. The native Back button, a header back arrow and a swipe-back all call `history.back()`. At the list, `canGoBack` is false and Back exits the app, as the APK does today.
- Deep links: a push tap or bookmark opens `/sessions/{key}` or a tool path as the first entry, so Back would leave the app. On boot, `seedHistory(path)` rewrites the first entry to `/` and pushes the missing levels (list, then session, then tool), so Back always walks up to the list. This is plain `replaceState` plus `pushState` at start, nothing patched.
- Transition: `document.startViewTransition(() => flushSync(...))` around every screen change. The router sets `document.documentElement.dataset.nav` to `forward`, `back` or `side` first (forward and back slide, side cross-fades), and `popstate` goes through the same path, so the native Back animates too. `prefers-reduced-motion` disables it, and a browser without the API changes screens with no animation. CSS uses transform and opacity only.
- Screens stay mounted while covered: the router keeps the stack (at most 3) and renders every entry, only the top one visible and interactive (`inert` on the rest). Back to the chat is instant and keeps the Virtuoso position and the stream. Only the top screen runs polling streams: the terminal and changes streams run only while their screen is on top; the chat stream stays open under a tool screen, since it is cheap deltas and drives the status badge. The home list stream pauses while a session is open and resyncs with a `reset` on return. The home list also saves its scroll offset.
- Desktop: the same screens in a centered column (about 720px for chat and list, wider for changes and terminal). No persistent sidebar and no split view; revisit only if desktop use grows.
- Lint: `tsc --noEmit` and `biome check` in `web/`.
- Wire types: Go response structs live in one file, `internal/server/wire.go`, and are the contract. `tygo` generates `web/src/api/types.gen.ts` from it (add as a Go `tool`; if it does not build with the current Go, fall back to a hand-written `types.ts` guarded by golden JSON fixtures decoded in a Go test). The generated file is never edited by hand, and a build step fails when it is stale (regenerate, then `git diff --exit-code`). This is their `generate:types` and `--check`, minus the schema detour, because the Go side is ours. Client calls stay hand-written typed `fetch` wrappers with one `ApiError` (`status`, `code`); no runtime validation library.
- Markdown (increment after parity, see Transcript spec): `react-markdown` with `remark-gfm`, no raw HTML plugin; code highlighting lazy loaded.

## Server changes

Existing endpoints, all kept and consumed as-is:

- GET /api/models, GET /api/push/key, POST /api/push/subscribe, POST /api/push/unsubscribe, POST /api/upload
- POST /sessions/{key}/comments (JSON in, JSON out), POST /sessions/{key}/terminal/input (JSON in, 204)
- GET /stream/terminal/{key}: raw SSE with `event: term` and a JSON payload `{text, revision}`, plus `event: gone`. Already client-ready.
- GET /sw.js, /manifest.webmanifest, /healthz

Streams: new JSON SSE endpoints, not the datastar ones.

- The datastar streams (`/stream/sessions`, `/stream/transcript/{key}`, `/stream/changes/{key}`) call `PatchElementTempl` with selectors. Their frames are HTML, tied to element ids. Reusing them means parsing HTML in the SPA. Rejected.
- The server already writes raw SSE next to datastar (`writeSSE` in terminal.go), so the pattern exists. Add `GET /api/stream/sessions`, `/api/stream/transcript/{key}`, `/api/stream/changes/{key}`, each writing named JSON events through `writeSSE`.
- Reuse the loop logic (watcher ticks, `pacer`, `itemFps`, `chatAppend`, status compare, keepalive). Pull the change detection into a helper both handlers call. The datastar handlers die at cutover; do not refactor beyond that.
- Transcript events: `reset` (`{items, status, model, thinking?, firstSeq}`), `append` (`{items}`), `status` (`{status, model, thinking?}`), `ping` (keepalive, every 15 s like today). The first event on every connection is a `reset`, so EventSource auto-reconnect needs no Last-Event-ID and no separate snapshot fetch. A `reset` is also sent whenever `chatAppend` says the prefix changed (branch switch, edit, shrink).
- Items get `seq`, the absolute index in the active branch. `pi.ViewItem` has no id today, and the old plan's "stable server id" did not exist. The client store keys by `seq` and dedupes on it.
- Sessions events: `reset` with `{sessions, badges, total, size}`, honoring the `w` window and active-first grouping from `listWindow`. Hash-gate it like the changes stream: today the stream re-renders on every tick even when nothing moved, and the client store also skips a `set` when the payload equals the held one (their `sameData` guard). Changes events: `reset` with `{files}`, hash-gated as today.
- Keep `ping` as a named event, not an SSE comment line (their heartbeat is a comment). A comment never reaches the page, so the client watchdog could not tell a live idle stream from a silently dead WebView socket.
- Slow consumers: the server has no write deadline, so a phone that vanished without a FIN holds a goroutine and a watcher slot until TCP gives up. Set a per-frame write deadline (about 10 s) with `http.NewResponseController(w).SetWriteDeadline` in `writeSSE` and the new loops, and drop the stream on error. Their server does the same with a backpressure check.
- Races: a REST read that can overlap a stream (older history against a `reset`, the first sessions read against the first stream event) carries the client's current stream generation. A `reset` bumps it, and a late response from an older generation is dropped. Their invalidate-in-flight counter, same idea.
- Cadence: pi writes whole entries to the jsonl, not tokens, so the transcript moves when pi flushes an entry. The 1 s `streamMinInterval` is a floor on top of that. JSON deltas are cheap: use 250 ms on the new streams. The terminal tab remains the only live typing view.

Commands: JSON twins of the steering POSTs.

- `steerPrompt`, `steerStop`, `closeSession`, `modelSet`, `thinkingSet` answer with 303 redirects and a `?err=` flag. A fetch cannot read that (main.ts treats `opaqueredirect` as success and cannot show the error).
- Add `POST /api/sessions/{key}/prompt|stop|close|model|thinking`. JSON body in (`{text}`, `{model}`, `{level}`; keep the 1 MB cap), `{ok:true}` out, or `{error:"notdriven"|"empty"|"badmodel"|...}` with a 4xx or 5xx status. The error codes are the same strings `noticeText` maps today. Split each handler into a function that returns a sentinel error and let the redirect and JSON wrappers share it. Delete the redirect wrappers at cutover.

Reads: JSON, no HTML.

- GET /api/sessions?w=N: windowed list plus badges (same snapshot the stream sends).
- GET /api/sessions/{key}/transcript?n=N: older history. N is the tail size (cap 1000 today). The client prepends the part it has not seen.
- GET /api/sessions/{key}/changes?file=...: file list, or one file's diff text with the `truncated` flag.
- GET /api/sessions/{key}/files?dir=... and /files/view?file=...: listing and `{content, kind}`.

Not a port, new work: "new agent" (folder picker plus herdr workspace create). No server code exists for it; the herdr client has `WorkspaceClose` and no create. Needs `GET /api/dirs` (browse outside repos: restrict to a root allowlist, reject `..` and symlink escapes) and `POST /api/agents`. Check `herdr api schema --json` and m0-findings first. Own stage, after cutover.

- Shape, from herdr-web-ui's `NewSessionDialog` and `DirectoryBrowser`: `GET /api/dirs?path=&hidden=1` returns `{path, parent|null, home, directories[], truncated}`, sorted naturally, capped at 500, one level at a time. The create call takes `{cwd, label, agent}` and answers `{workspace_id, pane_id, agent_started, error?}`. Starting an agent can take up to a minute: the dialog stays open with a status line and offers "Open" if the workspace exists but the agent did not start.
- UI: recent-folder chips first (one swipeable row, up to 8, built from the workdirs the scanner already knows, minus `/tmp`), then a Browse sheet with parent and home buttons, a filter box that is not auto-focused on touch, a hidden toggle and "Use this folder". Konsta Sheet and List fit this.
- Their server only checks that the path is a readable directory: no root jail, and `..` is simply resolved. That is not enough for us. We keep the allowlist and symlink rule above, as the browse endpoint is a new read surface.

Guard hardening, found while reading their `crossSiteFileRead` and origin rules: our `guard` applies the Host allowlist only to non-GET requests. The new JSON GETs return transcripts and file contents, so a DNS-rebinding page open in the desk browser could read them from `127.0.0.1:8090`. Apply the Host check to every method on `/api/`, `/stream/` and the legacy read routes (the curl and healthz paths use `localhost` and keep working). Add a test with a foreign Host on a GET.

Unmatched `/api/*` and `/stream/*` paths answer a JSON 404 `{"error":"not_found"}`, never index.html. Error bodies keep today's plain `{"error":"code"}` shape (their `{error:{code,message}}` envelope would break comments.go and upload.go for no gain).

### Embed and serving

- Vite project in `web/`. `build.outDir` is `../internal/web/app` with `emptyOutDir: true` (required: the output is outside the Vite root). Go's embed cannot reach outside its package directory, so the output must live under `internal/web`.
- Not named `dist`: `.gitignore` ignores every `dist/`, and an ignored directory is missing on a fresh clone, which breaks `go vet` and `go test` on the embed. Track `internal/web/app/.gitkeep` and ignore the rest (`internal/web/app/*`, `!internal/web/app/.gitkeep`). Embed with `//go:embed all:app` (the `all:` prefix includes the dotfile, so the pattern always matches).
- web.go gets a second embed beside `static`. `static` stays until cutover (templ pages, vendored datastar, daisyUI CSS, xterm css).
- SPA handler rules: a file that exists in the embed is served as is. Under `/assets/`, a miss is a 404, never index.html (a JS request answered with HTML fails silently). Any other unknown path gets index.html with 200. API and stream prefixes never fall back. If index.html is missing (client not built), answer 503 with a plain text line.
- Cache headers: `/assets/*` gets `public, max-age=31536000, immutable` (content-hashed names). index.html, `/sw.js` and the manifest get `no-cache`. index.html with `no-cache` is what busts the WebView cache after a deploy. herdr-web-ui uses the same split; it adds one day for other files, we use `no-cache` for them (few and small).
- Precompression: a Vite build step writes `.br` and `.gz` next to every js and css file (Node `zlib`, brotli quality 5 and gzip level 9, as they do). The Go handler picks the sidecar from `Accept-Encoding` (br, then gzip, else the plain file), sets `Content-Encoding` and `Vary: Accept-Encoding`, and never compresses SSE. Nothing in the tailnet path compresses for us. Measure first: if the whole app transfers under about 150 KB compressed, skip it. The Go side is about 40 lines on the embed FS.
- CSP: send `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'` with index.html and assets. The transcript renders agent-written text, so this is the second wall behind the Markdown rules. `unsafe-inline` for styles only, because React sets style attributes. Stage 1 checks that the production build needs no inline script; Konsta and Virtuoso must run under it.
- Build id: Vite defines `__BUILD_ID__` from `git rev-parse HEAD` (empty outside git). The first `reset` of every stream carries the server's build id (set at link time with `-ldflags -X`); `/healthz` stays plain `ok`. A client that sees a different id than its own shows a Konsta toast "New version, reload" and reloads only on tap, so a draft is never lost. This is their `useUpdates` idea. It matters here because a deploy swaps the embedded assets: an open page that lazy-loads a chunk (highlighting, xterm) asks for a hashed file the new binary no longer has. Also reload on Vite's `vite:preloadError` event.
- AssetPath: it hashes `app.css` and `js/main.js` into `?v=` for the templ heads, and its `init` panics if those files are missing. Do not touch it until cutover; then delete it, `assetVer`, and the `static` embed together. Vite's hashed filenames replace it.
- Guard middleware: the cross-site and Origin checks apply to non-GET requests only, so SPA page loads and EventSource are unaffected by them. The Host allowlist now covers GETs on the API and stream prefixes too (see Server changes). SPA POSTs are same-origin fetches (Sec-Fetch-Site `same-origin`, Origin equals Host), so they pass. Keep `--origin`. Their cookie rule ("no Origin plus a cookie is rejected") does not apply: we have no cookies and no auth.
- Dev loop: Vite proxies `/api`, `/stream` and `/sessions` to 127.0.0.1:8090 with `changeOrigin: false`, so Host stays what the browser sent. On the phone through the tailnet, set `server.allowedHosts` and make sure the Host name matches the `--origin` hostname, or the first POST gets 403 "unknown host". Test one POST on day one. The proxy must not buffer SSE; check an event arrives live through it.
- sw.js, manifest and icons move to `web/public/` so Vite copies them to the output root unhashed. `/sw.js` and `/manifest.webmanifest` keep their URLs (the existing push registration stays valid) and read from the new embed at cutover; before that they keep reading from `static`. Manifest icon paths change to `/icons/...`.
- URLs: during stages 1 to 6 the SPA lives under `/app/` (Vite `base: '/app/'`, app paths derived from `import.meta.env.BASE_URL`) and the templ UI keeps every current route, `/` included. At cutover `base` becomes `/`, the server drops the templ routes, and the `GET /` handler becomes the SPA handler. After cutover the SPA paths equal today's page URLs (`/`, `/sessions/{key}`, `/changes`, `/files`, `/terminal`), so push deep links (`/sessions/{key}`) and bookmarks survive.
- build.sh runs, in order: `npm ci --prefix web`, `npm run build --prefix web`, then templ generate (until cutover), then `go build`. Vite must run before go build or the embed ships stale files.

## Client structure

```
web/                  new Vite project root (build-time node only)
  index.html
  vite.config.ts
  package.json, package-lock.json
  public/             sw.js, manifest.webmanifest, icons/ (copied unhashed)
  src/
    api/              typed fetch, stream.ts (reconnecting EventSource), types.gen.ts (generated), one module per server surface
    state/            zustand stores
    nav/              router (path map, history stack, seedHistory, view transitions), SideMenu, useBackClose
    session-tools.ts  registry of per-session tools (actions and screens)
    screens/          Home, Session (with SessionHeader), Changes, Files, Terminal, Settings
    chat/             transcript, work blocks, item renderers, composer, pickers
    ui/               konsta wrappers, dracula theme tokens
  dev/                fixture server: replays a recorded session over the same JSON and SSE shapes
internal/web/app/     Vite output, embedded by Go (ignored by git except .gitkeep)
```

The APK shell keeps loading https://box.example.ts.net/. Planned Kotlin change: none. The keyboard spike below may force one small change.

## Transcript spec

`Virtuoso` from react-virtuoso, with these props:

- `data={items}`, `computeItemKey={(_, item) => item.seq}`, `itemContent`.
- `initialTopMostItemIndex={{ index: 'LAST', align: 'end' }}` to open on the latest entry.
- `alignToBottom` so a short transcript sits at the bottom.
- `followOutput={(atBottom) => (atBottom ? 'auto' : false)}`. Use `'auto'`, not `'smooth'`: smooth scrolling fights rapid appends. It only fires when the data length changes while at bottom, matching the current rule: pinned at the bottom, free when the user scrolls up.
- `atBottomThreshold={90}` (default is 4; 90 matches `initChatPin`; herdr-web-ui uses 48, and phones have fat-finger drift, so 90 stays) and `atBottomStateChange` writing `pinned` to the store.
- Older history: `firstItemIndex={firstSeq}` and `startReached` to fetch more. Prepending lowers `firstSeq`, which keeps the scroll position.
- `increaseViewportBy` and `defaultItemHeight` tuned on the real 22MB session, not guessed.
- A "Jump to latest" button when `pinned` is false, calling the list ref's `scrollToIndex({ index: 'LAST', align: 'end' })`.
- There is no "reversed" mode in Virtuoso; the old plan's wording is dropped.
- Conflict with herdr-web-ui, and which side wins: it does not virtualize. It renders every loaded turn, caps a page at 100 turns, and keeps the scroll position on prepend with a layout effect (`scrollTop = oldTop + (newHeight - oldHeight)`). Virtuoso stays here, as accepted: our rows are single items (one tool run is dozens of rows), a session can be 22MB, and Virtuoso gives the prepend anchor and the bottom follow for free. Their approach is the named fallback if Virtuoso fights the folds: render the last 100 groups, a "Load earlier" button, the same layout-effect anchor, and a `stickToBottom` ref set from `onScroll` (distance from bottom at or under the threshold).

Rules:

- Items stream as `append` and `reset` events (see Server changes). Dedupe by `seq`.
- Collapse state for thinking and tool cards lives in the zustand store, keyed by `seq`. Virtuoso unmounts rows that scroll away, so local component state would reset on every scroll.
- No `content-visibility: auto` on rows. Virtuoso already virtualizes and measures row heights; the property breaks that sizing.
- Expanding a card changes row height without changing the data length, so `followOutput` does not fire. Do not chase it: a reader who opens a card wants to read it from its top. herdr-web-ui follows the bottom only when the width or font size changed, never on plain growth, for the same reason. This replaces the earlier "scroll to the end after a toggle" rule. After the toggle `pinned` may turn false; that is correct.
- Work blocks: the client groups consecutive `thinking`, `tool_call` and `tool_result` items between two `user` or `text` items into one row, a fold labelled with the step count and the last tool name. The group key is the `seq` of its first item. Default: open while the agent is working and the block is the last row, folded once the turn is over. A user tap sets `chosenOpen`, and `chosenOpen ?? defaultOpen` decides, stored in the zustand map by group key. Inside a block each step is one closed line, detail mounted only when opened. This is their `WorkBlockView`, and on a 360px screen it cuts scroll length more than any other change. The server stays flat.
- Grouping changes the Virtuoso `data` to groups, so an `append` that lands in the last group does not change the row count and `followOutput` does not fire. When `pinned` and the last group grew, call `scrollToIndex({ index: 'LAST', align: 'end' })` from a layout effect after the store update. Test it with a long tool run.
- Tool text stays capped at 400 runes server-side. If that cap hurts, the fix is theirs: cap higher (they use 4000 chars) and add `GET /api/sessions/{key}/items/{seq}` for "Show the whole output (N chars)" on demand. Not in the first cut.
- Each row renders inside an error boundary keyed by `seq` and text length, so one malformed item shows a stub line instead of blanking the transcript (their `RenderBoundary`; it matches the tolerant-parsing rule on the server).
- When the reader is scrolled away and new items arrive, show a "New messages" pill with a count that jumps to the latest (same button as "Jump to latest", different label).
- Opening a session: show the last page (the `reset` tail) and load older items when the reader scrolls within 400px of the top, with a button in the same slot so the height does not shift. The 400px trigger and the fixed slot are theirs.
- Plain text first. Today the transcript shows `it.Text` in `<pre>` with no Markdown. Reach parity in stage 2. Then add Markdown as its own increment: `react-markdown` plus `remark-gfm`, no raw HTML, links limited to `https:`, `http:` and `mailto:` with `rel="noopener noreferrer"`, the render memoized by `seq` and text length, highlighting lazy loaded and skipped past about 100 KB or 20,000 lines, code fences over 30 lines folded to 20 with "Show more". Their parser is hand-written and ours should not be.
- The driver badge and the Stop and Close items in the header overflow are separate small subscriptions fed by `status` events, not part of the list. Stop is enabled only while the driver reports working or blocked, as `StopForm` does today (Ctrl-C on an idle pi prompt can exit pi). Close asks for confirmation in a Konsta Dialog.
- Search (today `window.find` and `innerText` of `#chat`) cannot work on a virtualized list: most rows are not in the DOM. Search the store items instead: count matches, previous and next call `scrollToIndex`, highlight in the rendered row.
- Cap the client store (for example 2000 items) and drop from the top.

## Composer spec

- Pending bubble renders at submit (loading indicator, not an optimistic claim). Same reconcile rule as main.ts: the bubble clears when a new `user` item whose text starts with the first 60 normalized characters of the sent text arrives, when a `reset` arrives, or after 10 minutes (pi holds mid-turn prompts until the turn ends, so the match can be late). Queue semantics stay visible.
- Conflict with herdr-web-ui, and which side wins: it shows no bubble at all. The text stays in the box with a sending flag until the server acknowledges, and a user turn appears only when the transcript file holds it. We keep the bubble: Esteban's commit 404e6e1 chose it on purpose, and pi holds a mid-turn prompt for minutes, so a silent box reads as a dead send. We take their other half:
  - Draft: stored in localStorage per session key. On submit the whole text is held as "in flight"; on success only that acknowledged prefix is removed from the box, so anything typed during the call survives (with a one-line note that the new text was not sent).
  - Definite failure (the JSON `{error}` answer, a 4xx or 5xx): remove the bubble and restore the draft, with the error text from the code.
  - Unknown outcome (network error, timeout, or the stream drops during the call): do not restore the draft and do not retry. Keep the bubble, mark it "Not confirmed. Check the transcript before sending again.", and clear it if the matching `user` item arrives. A duplicate prompt to an agent is worse than a lost one.
- Enter key: a per-device setting `enterSends`. Default on for a fine pointer (Enter sends, Shift+Enter newline), default off on a coarse pointer (Enter is a newline, the Send button sends), because a soft-keyboard Enter that fires a half-written prompt costs more than a tap. Ignore Enter while an IME composition is active (`isComposing` or `keyCode === 229`). herdr-web-ui defaults to send everywhere.
- Textarea grows with content (set height to `auto`, then to `scrollHeight`, capped at half of `visualViewport.height`), stays 16 px (no focus zoom), has `autoCorrect="off"`, `autoCapitalize="off"` and `spellCheck={false}` (these are prompts about code), and sits in a flex column, never `position: fixed`.
- Keyboard dismiss: a tap on the transcript, or a drag of 32px or more downward on it, blurs the textarea. A reader should not have to find the back gesture to see the chat again.
- Viewport meta: `width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content`. Also `theme-color` `#0f1115`. No `maximum-scale`.
- Layout CSS:

```css
html, body { height: 100%; margin: 0; overflow: hidden; overscroll-behavior: none; }
.app {
  height: var(--app-height, 100dvh);
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  padding-top: calc(var(--safe-top, env(safe-area-inset-top)) / 2);
  padding-bottom: var(--safe-bottom, env(safe-area-inset-bottom));
}
.chat-pane { flex: 1; min-height: 0; }
/* keyboard open: the nav-bar inset is under the keyboard, do not add it twice */
.app:has(textarea:focus) { padding-bottom: 0; }
```

- Safe-area contract: the halved top padding and the `--safe-*` names are copied from app.css and must not change. MainActivity sets the vars once in `onPageFinished`, after React has mounted, so all CSS must read them lazily through `var()` (a JS read at mount would see nothing). SPA navigation does not reload the page, so the vars persist. The app must never replace the `style` attribute of `<html>`.
- Keyboard spike (stage 1, on the S25): `interactive-widget` only helps if the WebView itself is resized. At targetSdk 35 the shell is edge-to-edge, and `adjustResize` is inert there, so the WebView may not resize at all. Test a bottom-pinned textarea page and pick, in order: (a) the meta works, done; (b) the WebView does not resize but `visualViewport` reports it: publish `--app-height` from `visualViewport.height` only while a keyboard is up (a drop of more than about 20% of the layout height) and size the shell as `height: var(--app-height, 100dvh)`; this is exactly what herdr-web-ui ships in `src/lib/viewport.ts` and `styles.css`. Their file grew to about 90 lines of iOS standalone and split-view special cases, each with a regression script behind it. Start with the 15-line version and add a case only when the S25 shows it; (c) neither: MainActivity applies the IME inset as WebView bottom padding (a few lines in a window-insets listener; still no IPC bridge, so ADR-0003 holds). Record the result in the ADR.
- Attachments keep the 10MB client guard and the /api/upload flow. A pick uploads at once, 3 files in parallel, up to 10 files per prompt (extra files are dropped with a note). Each shows as a chip with a remove button, and Send is disabled while any upload runs. On submit the returned paths join the prompt text, one per line at the start, in pick order (pi reads an image when its path is in the text; herdr-web-ui puts its `@path` mentions at the start too). Removing a chip before submit needs no text edit, since nothing was inserted yet.
- Model and Thinking controls belong to the composer card, not the header (Esteban's ruling). They keep the placement idea of today's `ModelPicker`, a strip attached to the top of the composer card, above the input. Exact styling is ours to design.
  - Collapsed state, one line: two chips, the current model (`provider/id`) and the thinking level. The line is small and never pushes the input off screen.
  - Tapping the model chip opens a Konsta Sheet: search over /api/models with a 300ms debounce (the endpoint caps rows at 25), free-text pattern always allowed (`provider/model[:thinking]`, as `/model` takes it today). Tapping the thinking chip opens a Sheet with the levels `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max` (the server's `thinkingLevels`).
  - Both send through the JSON `model` and `thinking` endpoints and show the answer text from today's `noticeText`: "Queued if the agent is working", since pi holds the command until the turn ends. A chip shows a small pending mark until the stream confirms the change.
  - Current values come from the stream. `meta.Model` is set from the jsonl today; the transcript `reset` and `status` events carry `model` so the chip follows a switch without a reload. The current thinking level is not in `SessionMeta` today, and the old page hard-codes "medium" as selected. pi writes a `thinking_level_change` entry to the jsonl (verified live on a probe agent during the D2 model-picker E2E, next to `model_change`). Parse it into `SessionMeta.thinking` beside `model`, last entry on the active branch wins, and send it in `reset` and `status`. No fallback; before any entry exists, show the level as unknown.
  - The controls stay usable while the keyboard is up (they sit inside the composer card, which already rides the keyboard).

### Quick keys strip

Modeled on the muqun client: one horizontal row of key-cap buttons directly above the composer card (the card, with its model and thinking line, sits below it; the strip rides the keyboard with the card). Esteban's ruling, 2026-10-12.

Keys, in this order: Esc, Enter, the four arrows (left, up, down, right), Alt+Enter, Ctrl+D.

| Key | What it does | Enabled when |
|---|---|---|
| Esc | Interrupts the agent (pi's soft interrupt). The overflow Stop item keeps the Ctrl-C path from ADR-0001; the two coexist. | status is working or blocked |
| Enter | Raw Enter into pi's TUI: confirms a menu choice or a permission prompt. It is not the composer Send (that goes through the prompt endpoint). | a driver exists; most useful while blocked |
| Arrows | Move in pi's menus, selectors and history. One key per tap in v1; no hold-to-repeat. | a driver exists; most useful while blocked |
| Alt+Enter | pi's "queue this message for the end of the current turn". | the composer holds text AND status is working |
| Ctrl+D | Exits the agent and closes the session. It is labelled like a key cap but does not send a keystroke: it opens the same confirm Dialog as the overflow Close item and then calls the existing JSON close endpoint (the herdr workspace closes, the transcript stays readable). | a driver exists; always confirms |

When no herdr driver runs the session (status empty), every key is disabled, with the same "no driver" text the terminal uses.

Transport: raw keystrokes into the pi TUI through the existing terminal input path. No new endpoints.

- Esc, Enter and the arrows: `POST /sessions/{key}/terminal/input` with `{"keys":["esc"]}`, `["enter"]`, `["up"]` and so on, the key in the path serialized with `encodeURIComponent(key)`, exactly as the terminal island does. The endpoint is JSON in, 204 out, 64 KB cap, 5 s timeout. The key names are herdr's (`esc` is canonical).
- Sends go through one promise chain per session, as main.ts does, so taps arrive in order. herdr holds a lone Esc on the pty for about 150 ms (their finding, `docs/herdr-web-ui-learnings.md`), so wait that long after an Esc before the next key.
- Alt+Enter acts on pi's own editor buffer, not on our composer textarea. So the button first sends the draft as `{"text": draft}` and then `{"keys":["alt+enter"]}`, in that order on the chain, then clears the composer and shows the pending bubble marked "queued for the end of the turn". Spike on a probe agent before building: (1) herdr must accept `alt+enter` as a key name (the schema does not list the key grammar; if it refuses, send the text `\x1b\r` through the same `text` field, which is Alt+Enter in a terminal); (2) a draft with newlines must not submit early in pi's editor; if it does, send multi-line drafts through the normal prompt path and allow Alt+Enter for single-line text only; (3) leftover text already in pi's editor must not mix with the draft.
- The terminal screen keeps its own key row and its "take the wheel" gate. The chat strip has no such gate: each tap is one deliberate key.

Feedback: through the live stream, no per-key response. Esc shows up as the status flipping from working, a queued message as a new `user` item, Ctrl+D as the session closing. The only local feedback is a pressed state on the cap and, if the POST fails (409 no driver, 502), a Konsta Toast "Key not sent".

Skipped on purpose: Shift+Tab. The composer thinking chip already changes the thinking level, which is what Shift+Tab does in pi, so a second route would duplicate it and take space in a strip that scrolls.

Style and behavior:

- Key-cap look as in the screenshot: a rounded rectangle with a lighter top and a darker bottom edge, a monospace label, "Ctrl D" and "Alt+Enter" as written labels, arrows as glyphs. Dracula tokens, no color literals.
- Each cap is at least 44px high and 44px wide, with an 8px gap. The strip is `overflow-x: auto` with `overscroll-behavior-x: contain`, no scrollbar, and a fade at the right edge so the user sees there is more. It hides nothing critical: Esc and Enter come first, Ctrl+D is last and is reached by scrolling; at 360px about six caps are visible.
- No page zoom: `touch-action: manipulation` on the strip. No text-selection or long-press menu: `user-select: none`.
- A tap must not blur the textarea or drop the soft keyboard: call `preventDefault` on `pointerdown` and `mousedown` (herdr-web-ui's `KeyBar` does the same).
- A setting `showQuickKeys`, on by default, lets Esteban hide the strip if the 52px of height is not worth it.
- Keys are defined in one array (id, label, keys or handler, enabled rule) in `web/src/chat/quick-keys.ts`, so adding a key is one entry.

## Performance rules

- Compiled Tailwind 4 only; the browser JIT and all CDN script tags die with the templ pages.
- Touch: `touch-action: manipulation`, `-webkit-tap-highlight-color: transparent`, `overscroll-behavior: none` on the shell and `contain` on scroll panes, 44px+ targets (herdr-web-ui uses 40px; ours stays at 44).
- Styling: components use theme tokens, no color literals and no `!important` (their rule, and it fits Tailwind `@theme` tokens).
- Motion: transform and opacity only, `prefers-reduced-motion` respected.
- No layout thrash on stream ticks: appends touch the transcript store only.
- Close each stream on unmount. On `visibilitychange` to hidden, close the streams; on visible, reopen at once (the stream helper's rules, see Stack). The first `reset` re-syncs, and a backgrounded WebView does not hold sockets open on battery. Show connection status in the UI.
- Do not poll as a backstop. herdr-web-ui polls every 5 s next to its stream because its stream can miss events; ours resyncs with a `reset` on every connect and has a 45 s watchdog.

## Migration stages

Each stage ends with a working app and a deploy. The templ UI keeps every current route, `/` included, until stage 7. The SPA ships under `/app/`, so nothing the phone uses today can break before cutover.

1. Scaffold and spikes: Vite 8, React, Tailwind 4, Konsta theme proof (production build styled), zustand, embed wiring, SPA handler on `/app/`, dev proxy, build.sh update, `.gitkeep`, `wire.go` plus tygo generation with the stale check, the `api/stream.ts` helper, the CSP, the Host check on GETs, a write deadline in `writeSSE`. App shell: the path router with history stack, `seedHistory` and view transitions, the home side menu (empty but for settings), and the home list read-only from `GET /api/sessions` plus `/api/stream/sessions`; a stub session detail proves the push, Back and slide on the S25. Run the keyboard spike and the first Chrome trace. Gate: fresh clone passes `go vet` and `go test`; `/app/` loads on the phone and runs under the CSP; asset cache headers are right; one POST works through the dev proxy; killing and restarting the server reconnects the stream by itself.
   Shipped 2026-10-12 in 87ba3e6 and 2350e17, narrower than this bullet: the side menu, `seedHistory`, the CSP, `wire.go` with tygo, the keyboard spike and the Chrome trace are still open and ride with stage 3. The stream helper, the Host check on GETs and the write deadline landed with stage 2 instead.
2. Session view, read side: the session header with its tool registry (Search live, Stop and Close in the overflow; Changes, Files, Terminal listed but disabled until their stages), `/api/stream/transcript/{key}` with `seq`, Virtuoso, item renderers, work blocks, driver badge, stop and close through the new JSON endpoints. Steering still happens on the templ page. Record one real session (sanitized) into `web/dev/` and replay it from the fixture server, including a long one, so UI work and 360px screenshots do not need a live agent. Markdown comes after parity, as its own small step.
   Shipped 2026-10-12 in 8a4138c and 58cbfb3. The `reset` event carries a session header (name, first_user, cwd), so a deep link shows the real title with no list snapshot. The fixture server moved to the stage 3 kickoff, where composer work first needs it; the steer-a-probe gate moves with it, because stage 2 changed no steering path. The 22MB session streams and renders at 360px (couchpi-stage2-session-360.png), and the ts.net probe shows reset frames through tailscale serve. Konsta facts the later screens build on: Dialog takes title, content and buttons props with onBackdropClick and has no DialogContent or DialogActions exports; Navbar takes left, right and title with no backLink prop; Virtuoso has no emptyComponent prop.
3. Composer: JSON prompt, pending bubble, the model and thinking controls on the composer card with their sheets, attachments, the quick keys strip (after the Alt+Enter spike). Stage 3 also carries the stage 1 leftovers: the side menu, `seedHistory`, the CSP, `wire.go` with tygo, the keyboard spike, the Chrome trace, and the `web/dev/` fixture server.
   Shipped 2026-10-12 in ee4c787 and 375c5d5: the JSON twins for prompt, model and thinking (in-band codes, since fetch cannot read the templ 303 redirects), the pending bubble with its reconcile rules (an unknown outcome never restores or retries), attachments over `/api/upload`, the model and thinking sheets over the real catalog, and the quick keys strip. The Alt+Enter spike ran live first: herdr takes the key name, draft text lands in pi's editor without submitting, and alt+enter queues pi's follow-up while working. Two wedge classes surfaced and were fixed live: a model pattern with no exact match leaves pi sitting in its interactive picker, and a thinking level the current model does not support is rejected without a jsonl entry — both leave a chip pending forever, so both chips now carry an 8 s unconfirmed timeout that sends Esc and toasts. `web/dev/fixture.mjs` replays a synthetic session for UI work without a live agent; `COUCHPI_PROXY` points the dev server at it. tygo is dropped: unverified on this toolchain, so golden JSON fixtures remain the contract check and are still open. Still open from the leftovers: the side menu, `seedHistory`, the CSP, the keyboard spike and the Chrome trace; they ride with stage 4.
4. Changes and files: diff view, line comments (keep the `trayKey` localStorage keys so drafts survive), send-all, files listing and view.
5. Terminal: reuse the xterm.js logic over the existing SSE and input endpoints.
6. Parity gate: write the list of every templ feature (list window and "load more", active-first grouping, search, push toggle, comment tray and draft sheet, model picker, upload) and tick each against the SPA. Dogfood `/app/` for a few days. Fix gaps before cutover.
7. Cutover: Vite `base` to `/`, SPA handler on `GET /`, templ routes and redirect wrappers removed, `/sw.js` and the manifest served from the new embed, then delete templ pages, datastar bundle, daisyUI CDN, main.ts and its output, vendored xterm, `static`, AssetPath and assetVer. `go mod tidy` drops templ and datastar-go; drop the templ and esbuild `tool` directives. Update build.sh, AGENTS.md (stack, commands, "no node_modules" line) and docs. Gate: a push notification tap opens the right SPA path, APK Back walks history, a cold start loads with no stale index.html, the old service worker upgrades cleanly.
8. After cutover: settings screen, "new agent" folder picker and herdr create (new server work, see Server changes), optional "show whole output" fetch.

## Verification

- Server: go vet, gofmt, go test stay green. New JSON endpoints get tests, including error codes. Extend stream_test.go for the new events (`reset` first, `append` after growth, `reset` on prefix change). Test the SPA handler: existing file, missing asset 404, unknown path gets index.html, missing index gives 503.
- Contract: the tygo output is regenerated and diffed in CI and in build.sh; golden JSON fixtures for each wire struct and each stream event are decoded by a Go test (their `api.contract.test.ts` plays the same role). The generated types are never edited by hand.
- Client: `npm run build`, `tsc --noEmit`, `biome check`; pure logic (work-block grouping, `reset` and `append` reduction, draft acknowledge-prefix, pending reconcile, stream backoff) gets unit tests, with no DOM, like their `lib/*.test.ts`; 360px screenshots against the fixture server for layout checks; phone E2E over adb on the S25 (100.100.10.x, port rotates), 360px screenshots to ~/Dev/Screenshots with the couchpi- prefix.
- Every stage: steer a probe agent end to end (workspace create, prompt, stop) before deploy, then the canonical install+restart pipeline and healthz plus a content check (fetch the served index.html and confirm it names the new hashed asset).

## Risks

- WebView quirks: dvh, interactive-widget, safe-area insets. MainActivity injects --safe-top/--safe-bottom once per page load and returns early if `rootWindowInsets` is null, in which case the vars never appear and content sits under the status bar. Keep the contract; the keyboard spike decides whether the shell needs one more listener.
- Back button: MainActivity uses `canGoBack`. State-only navigation would exit the app on Back. Use `pushState`.
- Konsta retheme cost, prop-guessing by the agent, Tailwind not scanning Konsta in production builds, and its `env()` safe areas reading 0. Keep usage to the listed components.
- Virtuoso plus collapsible cards and prepend (measure with the real transcript, including the 22MB+ session). Expansion at the bottom needs a manual scroll.
- Server cost per tick: the stream handler calls `Transcript(key, 0)` and fingerprints the whole branch on every tick. On the 22MB session that may dominate the lag the SPA is meant to fix. Measure in stage 2; cache the fingerprints per session if needed.
- No item ids today: `seq` plus `reset` covers it, but a branch switch that keeps the same length and prefix hash is a correctness edge. Test it.
- Transcript granularity: entries land whole, so there is no token typing effect. Say so in the UI copy if it feels dead.
- Cache and service worker: a stale index.html in the WebView, or a stale sw.js, serves old assets forever. Both stay `no-cache`; verify by content after every deploy.
- Backgrounded WebView drops streams: reconnect on visible, show status.
- Native EventSource stops for good after an HTTP error (a 502 while the server restarts). The stream helper replaces it; without that, every deploy leaves open pages dead until reload.
- Version skew: an open page outlives a deploy and then lazy-loads a chunk that the new binary does not embed, or speaks to changed JSON. The build id toast and the `vite:preloadError` reload cover it; test by deploying while a page is open.
- DNS rebinding on the new JSON GETs (transcripts, file contents) from a page in the desk browser. The Host check on GETs closes it; keep the test.
- Android IME in the terminal: xterm 5.5 has composition bugs on Android keyboards, and herdr-web-ui carries a source patch for an IME backport (`patches/@xterm%2Fxterm@5.5.0.patch`). Our terminal types through a key row and an input box, not xterm's own textarea, so we may be safe; verify on the S25 in stage 5, and read their patch before writing our own.
- Quick keys: `alt+enter` as a herdr key name, newline handling in pi's editor and the lone-Esc delay are unverified. The spike in the Quick keys strip section settles them before the strip ships. A wrong Alt+Enter can submit a half-typed draft as an immediate steer instead of queueing it.
- Back handling is their most fragile area. Keep to the rules in Screens and navigation (push on open, replace on a sideways tool hop, one overlay hook, `seedHistory` at boot) and test Back on the S25 with a sheet open, the side menu open, a tool screen open, and after a push deep link.
- Screens stay mounted under the top one: with the chat plus a terminal on top, memory and stream load rise. Only the top screen polls; check battery and heap on the S25 in stage 5.
- View transitions on `popstate` can show a wrong direction after a fast double Back or a deep-link seed; the router sets the direction from the history index it stores in `history.state`, not from a guess. Test it.
- Header growth: every new tool lands in a 360px strip. The registry caps visible tools and moves the rest to the overflow button; decide the order of tools by use, not by who was added first.
- Node on the dev machine is now a build dependency (Node 20.19+). The deploy command in AGENTS.md grows an `npm ci` step.
- React 19 idiom drift in training data; pin exact versions in package.json.
