# AGENTS.md

Operating guide for coding agents working on Pi Couch.

## Project overview

Pi Couch: phone-first web client for the pi coding agents on this server. Shows every pi session on the machine, drives them through herdr, reviews git changes per repo, collects line comments, pushes on status changes. It is a client, not a runtime: it never forks its own agent runtime. Status: all milestones shipped; the React client rewrite cut over 2026-10-13 — the SPA is the whole site, the templ pages and Datastar are deleted. Plan and decisions live in docs/.

## Read first (sources of truth)

- `docs/PLAN.md`: plan v0.2. Feature inventory (V/D/R/T/P ids), milestones, risks. Feature ids are stable references; use them in tasks and commits.
- `CONTEXT.md`: binding domain glossary. Use these exact words in code, UI, and docs. Avoid the synonyms listed under each term (Session, Workdir, Repo, Changes, Comment, Steering, Driver, Status, Stop, Resume, Close).
- `docs/adr/NNNN-slug.md`: decision records. New architectural decisions go in a new ADR, not as edits to the plan.
- `docs/m0-findings.md`: verified herdr and pi facts from the M0 probes. Trust these over assumptions.
- `docs/hc-findings.md`: verified herdr-core (hc) facts from the 2026-10-30 probes: wire formats, the first-paint send race, status semantics, and which dialogs never report blocked.
- `docs/herdr-web-ui-learnings.md`: verified facts and porting patterns extracted from the herdr-web-ui codebase (2026-10-12), including the `terminal session observe` correction to m0-findings.

## Locked decisions (do not relitigate)

- Plain pi sessions only: jsonl trees under `~/.pi/agent/sessions/`. No pi-durable, no RPC children, no guests (ADR-0001).
- Two drivers behind one seam (ADR-0008, accepted 2026-10-30): `driver = "hc"` (default) or `driver = "herdr"` in the config picks exactly one per server process. hc speaks herdr-core's JSON-RPC plus framed attach, with session identity on the `pi.session` terminal label. Under hc, abort is esc via terminal.interrupt (herdr keeps Ctrl-C). herdr speaks legacy pinned protocol 22. `hc_socket` overrides hc's socket discovery. `docs/hc-findings.md` holds the verified probe facts.
- Stack: one Go binary, stdlib `net/http` routing, and a React 19 + Vite + Tailwind 4 + Konsta SPA served from `go:embed`. The templ pages, Datastar and the daisyUI CDN died at the stage-7 cutover (ADR-0006 supersedes ADR-0002). No web framework on the server; node is a build-time dependency only. ADR-0007 amends ADR-0001 for the dialog-sensor bridge extension.
- Android install surface: thin Kotlin WebView APK. One Activity, no IPC bridge, no bundled assets, no FCM; sideloaded, not the PWA install flow (ADR-0003).
- Security: server binds 127.0.0.1 only, fronted by `tailscale serve`. The tailnet is the boundary; no extra auth layer. Never widen this without a new ADR.

## Commands

### Quick Commands (`just` recipes)

```bash
just            # List all available targets
just build      # Build React SPA and Go binary to bin/couch-pi
just ci         # Run all local CI gates (Go, web checks, unit and mobile browser tests)
just test-go    # Run Go tests only
just check-web  # Run web checks only (tsc + biome)
just test-e2e   # Build the SPA and run real Chromium phone-browser tests
just apk        # Build Android debug APK
```

Verified (M0):

- `./scripts/build.sh`: builds the React SPA (when npm is present), then `go build` to `bin/couch-pi`.
- `go vet ./...` and `gofmt -l .` must both be clean before a commit; `just ci` also runs `go test -race ./internal/pi ./internal/server ./internal/update`. Web adds `tsc` (including `extensions/tsconfig.json` through `npm run check:extension`), `biome`, `vitest` (run via web/node_modules/.bin), and real Chromium mobile E2E (`just test-e2e`). The E2E harness needs Chrome/Chromium or `CHROME_BIN`, uses no extra npm dependency, and writes screenshots to `web/test-results/mobile/`; see `web/e2e/README.md`.
- `go test ./...`: parser and index tests; the real-sessions integration test skips itself when the dir is absent.
- Run: `bin/couch-pi --addr 127.0.0.1:8090` (loopback only). `curl localhost:8090/healthz` expects `ok`.
- herdr probes: `herdr api schema --json`, `herdr api snapshot`, `herdr agent --help`.
- Service install is manual (user unit): copy `systemd/couch-pi.service` to `~/.config/systemd/user/`, then `systemctl --user daemon-reload && systemctl --user enable --now couch-pi`.
- Deploy server updates (the phone hits the service, not `bin/`): `./scripts/build.sh && install -m 755 bin/couch-pi ~/.local/bin/couch-pi && systemctl --user restart couch-pi && curl -s localhost:8090/healthz`. build.sh builds the React SPA (npm ci + npm run build under web/) when npm is on PATH, so a deploy always embeds a fresh SPA; on machines without node the Go-only build still works and a fresh clone serves 404 everywhere until the SPA is built there. The unit runs `%h/.local/bin/couch-pi`, so restarting without the install step keeps serving the previous binary; verify by content (e.g. the served `/manifest.webmanifest`), not just healthz. The server reads `~/.config/pi-couch/config.toml` (see `pi-couch.example.toml`); `origin` there is the public https origin the cross-site guard accepts, so a dev run without it only accepts loopback. Flags override config. `/api` and `/stream` sit behind an access token: `PICOUCH_ACCESS_TOKEN` (env var or `access_token` in config.toml; on this machine the unit's `EnvironmentFile` points at `~/.config/couch-pi/env`), so scripted probes must send it as `Authorization: Bearer` or the `picouch_token` cookie or they get 401; `healthz` and static surfaces stay open.
- Remote front: `tailscale serve --bg --https=8443 8090` puts the loopback server on the tailnet at port 8443 (not 443: other stacks on this machine publish 0.0.0.0:443 and would collide). `curl https://<machine>.<tailnet>.ts.net:8443/healthz` expects `ok`.
- APK: `cd android && ./gradlew assembleDebug` (wrapper copied from Baluard; `android/local.properties` holds `sdk.dir`, gitignored). Install: `adb install -r android/app/build/outputs/apk/debug/app-debug.apk`. Debug-signed, Pi Agent adaptive icon and dark splash; no keystore yet.
- Push slice: VAPID keys and subscriptions live under `~/.local/state/couch-pi/` (`vapid.json` 0600, `push-subs.json`). `GET /api/push/key`, `POST /api/push/subscribe|unsubscribe`; `/sw.js`, `/manifest.webmanifest` and the icons come from the SPA embed (`web/public/`).
- Dialog bridge plugin (needed for dialog titles, per ADR-0007): `ln -sf <repo>/extensions/couch-pi-bridge.ts ~/.pi/agent/extensions/couch-pi-bridge.ts` on every machine whose agents should surface dialogs. The repo file is the source of truth; pi loads it at agent start, so agents started before the install (or before an edit) run the old code until restarted. Without the plugin nothing breaks: herdr still flips to blocked via the pane regex in `internal/server/awaiting.go`, but the phone shows no dialog kind or title (and blocked detection for `ctx.ui` dialogs then rides the regex only). `COUCHPI_URL` overrides the default `http://127.0.0.1:8090` dial target. The ask_user_question relay (ADR-0009) additionally needs `@juicesharp/rpiv-ask-user-question` installed on the agent machine; without it the phone falls back to the "open Terminal" banner for questionnaires.

Pins: fsnotify v1.10.1, webpush-go v1.4.0; the web stack pins live in web/package.json (React 19.3, Vite 8.3, Tailwind 4.3, Konsta 5.5, xterm 5.5, react-markdown 10.1, remark-gfm 4.0, highlight.js 11.12). herdr socket framing is NDJSON: `{"id","method","params"}` in, one compact JSON line out.

## Architecture (planned, per PLAN.md)

- Scanner: walks `~/.pi/agent/sessions/`, parses jsonl tolerantly, tails with fsnotify. Indexes by mtime with in-memory metadata.
- herdr client: socket at `~/.config/herdr/herdr.sock`, request envelope from `herdr api schema`; CLI fallback where the socket is fiddly. The hc client lives beside it in `internal/hc` (plain JSON-RPC per call, one events connection, framed attach); `internal/hc/driver.go` maps hc terminals onto the same session/status/command surface.
- Terminal bridge: herdr socket only. `pane.read` in `ansi` format polled on a 1 s tick into xterm.js; keystrokes out via `pane.send_input`. No PTY bridge, no cgo: the socket has no attach method and no output push (m0-findings).
- Git service: repo discovery by pruned scan, diffs via exec git.
- State: comments in a plain JSON file per session; UI preferences (enter-send behavior, quick keys) in browser localStorage per device. Push subscriptions and the VAPID key live in the store state dir. No database.
- Push watcher: one `agent.wait` goroutine per live herdr agent (until idle, blocked, done), 15 s reconcile, 5 s re-arm pause, 2 s stability re-check, same-status dedupe (ADR-0005).
- Dialog sensor: extensions/couch-pi-bridge.ts (symlink into ~/.pi/agent/extensions) listens to pi's ui_prompt events, flips herdr to blocked, and POSTs kind + title + id to /api/bridge/dialog so the phone shows which dialog waits and can answer it (ADR-0007). The answer endpoint re-reads the pane before any key: confirms take yes/no, selects take an option's text with a verify-then-fire cursor walk, escape dismisses. pi's codebase is never modified; APIs only.
 - Ask relay: the same bridge also relays rpiv-ask-user-question's `rpiv:ask-user:prompt` / `:answer` / `:answer-result` events (ADR-0009): the questionnaire rides the wire as dialog kind `ask` with the questions attached, `POST /api/sessions/{key}/ask/answer` takes structured answers, and the phone card (web/src/components/AskCard.tsx) renders them natively - no pane parse, no keystrokes. The bridge polls `/api/bridge/ask/answer` while a questionnaire is open, so answers cost one poll tick; rpiv's own TUI keeps first-answer-wins over the phone. - Rewind: long-pressing the last user message on the phone (idle, driven sessions) rewinds the conversation to before it (ADR-0010). The bridge registers a `/couchpi-rewind <entryId>` command that calls pi's own `navigateTree` (branch pointer only, jsonl untouched, `summarize: false`); the server fires it through the herdr prompt path and the bridge reports the verdict to `/api/bridge/rewind`. pi's desk refill puts the rewound text back in the pane editor, so the server sends one pane ctrl+c on the verdict to keep the next typed prompt clean; exiting pi would need two presses. The transcript renders the parent chain from the file tip, so the abandoned branch disappears on the next resend. hc sessions refuse rewinds: hc has no keys verb to clear pi's refilled editor buffer. Running sessions need a bridge reload before the command exists.- Web client: the React SPA is the whole site. JSON feeds live under `/api` and `/api/stream/*` (sessions, transcript with `seq`, changes; the raw terminal byte stream stays under `/stream/terminal/{key}`), and every command is a JSON POST under `/api`. Assistant text renders markdown: link allowlist (http, https, mailto), code fences over 12 lines folded, highlight.js 11.12 lazy-loaded as its own chunk. Tool text caps at 400 runes with `GET /api/sessions/{key}/items/{seq}` fetching the whole item. New agents start from the phone: the home FAB opens a sheet over `GET /api/dirs` (browser jailed to the user's home, symlinks rejected on escape) and `POST /api/agents` (herdr workspace create, agent start, launch wait, first prompt; answers `{key}`). The settings screen stores enter-send and quick-keys preferences (including the key order) in localStorage, and the composer dictates through the browser's own speech recognition (no key; the mic hides where the API is missing).
- Native door: JSON commands, JSON SSE feeds, and the raw terminal byte stream stay separate surfaces so a Kotlin client plugs in with no server rework.

Boundaries:

- Session files are read-only to Pi Couch. Never write into jsonl.
- herdr's own words (workspace, tab, pane, agent) stay at the integration boundary. They never enter the glossary or the UI copy.

## Conventions and gotchas

- Before building any UI element, check whether Konsta already ships a component for it (web/node_modules/konsta/react/components/). Use the Konsta component when one fits the screen; when rejecting one, record the reason in the commit or report. Never hand-build what the framework already provides.
- Konsta CSS is pruned per component: web/src/index.css carries explicit `@source` lines for every konsta/react import, and konsta-css.test.ts fails the build when an import has no entry. Tailwind generates nothing for class strings it never scans, so a missing entry means the component renders unstyled. Nested dependencies count: DialogButton wraps Button, NavbarBackLink uses Icon.
- Tolerant jsonl parsing: unknown entry types never crash the parser. Buffer partial lines until the newline before parsing.
- herdr's create readiness is two-step on current pi: agent_status flips to idle first and the plugin binds the session file a beat later, and agent.prompt refuses with "not an active named agent" until the bind. waitAgentActive therefore requires BOTH a non-unknown status and a bound agent_session, and the first prompt retries ~10x600ms to ride out residual gate gaps (agents_create.go). The desk CLI's agent start waits for full readiness itself; the socket agent.start returns instantly, which is why the socket path needs this poll.
- Verify parser shapes against real session files at M0, not guesses.
- Repo discovery: scan for `.git` with rg/fd semantics plus a denylist: node_modules, dist, build, target, vendor, .venv, caches, hidden dirs. Never raw `find`.
- Steer versus follow-up rides on pi's queue mode. Not a UI toggle; documented limit (ADR-0001).
- The herdr socket has no attach lock and no `--takeover`; only the desk CLI attach is exclusive. Terminal tab: read-only peek by default, interactive on explicit tap, coexistence banner when the desk holds attach (M0 findings).
- Compression scope: the Vite build emits `.gz` sidecars for hashed assets and the Go embed serves them only when the client sends `Accept-Encoding: gzip` (curl and the native door get raw bytes); plain `/api` JSON responses and the transcript SSE compress (buffered vs per-frame flush respectively), while the terminal and sessions SSE streams stay raw on purpose: tiny frequent frames lose more to flush overhead than compression saves. Any change to response writing must keep the `WriteHeader`-before-body discipline in mind, since the gzip wrappers stamp headers at `WriteHeader`.
- Ephemeral sessions (`--no-session`) are invisible by design.
- No backward-compat layers or fallback paths; remove obsolete code instead (project rule, ADR consequences included).

## Versions

- `const Version` in internal/server/version.go is the single source of truth. Releases are date-based since the 0.1.x ladder ended at 0.1.58: `YYYYMMDD` for the first release of a day, `YYYYMMDD.1`, `.2`, ... for same-day re-releases. The first date release of a day is bare regardless of how many 0.1.x releases shipped earlier that day.
- Releases are built and published locally: `scripts/release.sh` runs `scripts/ci.sh` (go vet, gofmt, go test, targeted race tests, npm ci, tsc, biome, vitest, mobile browser E2E), cross-builds the four static binaries plus a checksums.txt the self-updater verifies, creates the GitHub release at the version version.go carries, and dispatches the homebrew-tap formula update (bare assets, date tag accepted there since 022f273). `release.yml` is a manual `workflow_dispatch` fallback and never runs on push; its update-tap job needs the `TAP_GITHUB_TOKEN` secret (not yet set in this repo - only the fallback path uses it). The APK attaches to the release separately, only when an APK round actually happens.
- The binary self-updater compares versions part-wise with any part count, so the date scheme orders correctly against old semver (`20261028.3` < `20261029`); unparsable tags sort oldest so a junk tag can never trigger a downgrade (this is why the same-day suffix is `.1` and never `.A`). Before anything executes a download, the updater fetches the release's checksums.txt and verifies it; releases without checksums fail closed. A tag that failed two boots lands on a reject list (state dir, `update-rejected.json`) and is never chased again, and the boot marker clears when the server actually binds, not on a timer.
- APK versions are a separate axis: versionCode is a strictly increasing int (the only number in-place upgrades and Play care about; bump on every APK build) while versionName mirrors version.go cosmetically.

## Commits

History uses Conventional Commits, lowercase, imperative, no scope: `docs: lock Go and Datastar stack in ADR-0002`. Single branch (main). No CONTRIBUTING.md, CI, or PR templates yet.
