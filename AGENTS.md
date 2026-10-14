# AGENTS.md

Operating guide for coding agents working on Couch Pi.

## Project overview

Couch Pi: phone-first web client for the pi coding agents on this server. Shows every pi session on the machine, drives them through herdr, reviews git changes per repo, collects line comments, pushes on status changes. It is a client, not a runtime: it never forks its own agent runtime. Status: all milestones shipped; the React client rewrite cut over 2026-10-13 — the SPA is the whole site, the templ pages and Datastar are deleted. Plan and decisions live in docs/.

## Read first (sources of truth)

- `docs/PLAN.md`: plan v0.2. Feature inventory (V/D/R/T/P ids), milestones, risks. Feature ids are stable references; use them in tasks and commits.
- `CONTEXT.md`: binding domain glossary. Use these exact words in code, UI, and docs. Avoid the synonyms listed under each term (Session, Workdir, Repo, Changes, Comment, Steering, Driver, Status, Stop, Resume, Close).
- `docs/adr/NNNN-slug.md`: decision records. New architectural decisions go in a new ADR, not as edits to the plan.
- `docs/m0-findings.md`: verified herdr and pi facts from the M0 probes. Trust these over assumptions.
- `docs/herdr-web-ui-learnings.md`: verified facts and porting patterns extracted from the herdr-web-ui codebase (2026-10-12), including the `terminal session observe` correction to m0-findings.

## Locked decisions (do not relitigate)

- Plain pi sessions only: jsonl trees under `~/.pi/agent/sessions/`. No pi-durable, no RPC children, no guests (ADR-0001).
- herdr is the only driver. Every start, steer, stop, resume goes through herdr's socket API. Protocol 22 is pinned; adapt on change. Abort is Ctrl-C via send-keys, not an API verb (ADR-0001).
- Stack: one Go binary, stdlib `net/http` routing, and a React 19 + Vite + Tailwind 4 + Konsta SPA served from `go:embed`. The templ pages, Datastar and the daisyUI CDN died at the stage-7 cutover (ADR-0006 supersedes ADR-0002). No web framework on the server; node is a build-time dependency only.
- Android install surface: thin Kotlin WebView APK. One Activity, no IPC bridge, no bundled assets, no FCM; sideloaded, not the PWA install flow (ADR-0003).
- Security: server binds 127.0.0.1 only, fronted by `tailscale serve`. The tailnet is the boundary; no extra auth layer. Never widen this without a new ADR.

## Commands

Verified (M0):

- `./scripts/build.sh`: builds the React SPA (when npm is present), then `go build` to `bin/couch-pi`.
- `go vet ./...` and `gofmt -l .` must both be clean before a commit; web/ adds `tsc`, `biome` and `vitest` (run via web/node_modules/.bin).
- `go test ./...`: parser and index tests; the real-sessions integration test skips itself when the dir is absent.
- Run: `bin/couch-pi --addr 127.0.0.1:8090` (loopback only). `curl localhost:8090/healthz` expects `ok`.
- herdr probes: `herdr api schema --json`, `herdr api snapshot`, `herdr agent --help`.
- Service install is manual (user unit): copy `systemd/couch-pi.service` to `~/.config/systemd/user/`, then `systemctl --user daemon-reload && systemctl --user enable --now couch-pi`.
- Deploy server updates (the phone hits the service, not `bin/`): `./scripts/build.sh && install -m 755 bin/couch-pi ~/.local/bin/couch-pi && systemctl --user restart couch-pi && curl -s localhost:8090/healthz`. build.sh builds the React SPA (npm ci + npm run build under web/) when npm is on PATH, so a deploy always embeds a fresh SPA; on machines without node the Go-only build still works and a fresh clone serves 404 everywhere until the SPA is built there. The unit runs `%h/.local/bin/couch-pi`, so restarting without the install step keeps serving the previous binary; verify by content (e.g. the served `/manifest.webmanifest`), not just healthz. The unit passes `--origin https://box.example.ts.net:8443`; state-changing POSTs from any other host are rejected as cross-site, so a dev run without the flag only accepts loopback.
- Remote front: `tailscale serve --bg --https=8443 8090` puts the loopback server on `https://box.example.ts.net:8443/` (tailnet-only). Port 8443, not 443: Wicket stacks publish 0.0.0.0:443 machine-wide and would collide. `curl https://box.example.ts.net:8443/healthz` expects `ok`.
- APK: `cd android && ./gradlew assembleDebug` (wrapper copied from Baluard; `android/local.properties` holds `sdk.dir`, gitignored). Install: `adb install -r android/app/build/outputs/apk/debug/app-debug.apk`. Debug-signed, Pi Agent adaptive icon and dark splash; no keystore yet.
- Push slice: VAPID keys and subscriptions live under `~/.local/state/couch-pi/` (`vapid.json` 0600, `push-subs.json`). `GET /api/push/key`, `POST /api/push/subscribe|unsubscribe`; `/sw.js`, `/manifest.webmanifest` and the icons come from the SPA embed (`web/public/`).
- Dialog bridge plugin (needed for dialog titles, per ADR-0007): `ln -sf <repo>/extensions/couch-pi-bridge.ts ~/.pi/agent/extensions/couch-pi-bridge.ts` on every machine whose agents should surface dialogs. The repo file is the source of truth; pi loads it at agent start, so agents started before the install (or before an edit) run the old code until restarted. Without the plugin nothing breaks: herdr still flips to blocked via the pane regex in `internal/server/awaiting.go`, but the phone shows no dialog kind or title (and blocked detection for `ctx.ui` dialogs then rides the regex only). `COUCHPI_URL` overrides the default `http://127.0.0.1:8090` dial target.

Pins: fsnotify v1.10.1, webpush-go v1.4.0; the web stack pins live in web/package.json (React 19.3, Vite 8.3, Tailwind 4.3, Konsta 5.5, xterm 5.5). herdr socket framing is NDJSON: `{"id","method","params"}` in, one compact JSON line out.

## Architecture (planned, per PLAN.md)

- Scanner: walks `~/.pi/agent/sessions/`, parses jsonl tolerantly, tails with fsnotify. Indexes by mtime with in-memory metadata.
- herdr client: socket at `~/.config/herdr/herdr.sock`, request envelope from `herdr api schema`; CLI fallback where the socket is fiddly.
- Terminal bridge: herdr socket only. `pane.read` in `ansi` format polled on a 1 s tick into xterm.js; keystrokes out via `pane.send_input`. No PTY bridge, no cgo: the socket has no attach method and no output push (m0-findings).
- Git service: repo discovery by pruned scan, diffs via exec git.
- State: comments and UI prefs in a plain JSON file per session. Push subscriptions and the VAPID key live in the store state dir. No database.
- Push watcher: one `agent.wait` goroutine per live herdr agent (until idle, blocked, done), 15 s reconcile, 5 s re-arm pause, 2 s stability re-check, same-status dedupe (ADR-0005).
- Dialog sensor: extensions/couch-pi-bridge.ts (symlink into ~/.pi/agent/extensions) listens to pi's ui_prompt events, flips herdr to blocked, and POSTs kind + title to /api/bridge/dialog so the phone shows which dialog waits (ADR-0007). pi's codebase is never modified; APIs only.
- Web client: server-rendered HTML, Datastar SSE patches, one TS island. Mobile-first, dark default.
- Native door: views stay separate from streams (JSON POST commands; SSE accepts raw JSON beside Datastar patches; terminal WS is raw bytes plus JSON resize frames) so a Kotlin client plugs in with no server rework.

Boundaries:

- Session files are read-only to Couch Pi. Never write into jsonl.
- herdr's own words (workspace, tab, pane, agent) stay at the integration boundary. They never enter the glossary or the UI copy.

## Conventions and gotchas

- Tolerant jsonl parsing: unknown entry types never crash the parser. Buffer partial lines until the newline before parsing.
- Verify parser shapes against real session files at M0, not guesses.
- Repo discovery: scan for `.git` with rg/fd semantics plus a denylist: node_modules, dist, build, target, vendor, .venv, caches, hidden dirs. Never raw `find`.
- Steer versus follow-up rides on pi's queue mode. Not a UI toggle; documented limit (ADR-0001).
- The herdr socket has no attach lock and no `--takeover`; only the desk CLI attach is exclusive. Terminal tab: read-only peek by default, interactive on explicit tap, coexistence banner when the desk holds attach (M0 findings).
- Ephemeral sessions (`--no-session`) are invisible by design.
- No backward-compat layers or fallback paths; remove obsolete code instead (project rule, ADR consequences included).

## Commits

History uses Conventional Commits, lowercase, imperative, no scope: `docs: lock Go and Datastar stack in ADR-0002`. Single branch (main). No CONTRIBUTING.md, CI, or PR templates yet.
