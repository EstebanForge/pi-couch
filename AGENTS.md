# AGENTS.md

Operating guide for coding agents working on PiRemote.

## Project overview

PiRemote: phone-first web client for the pi coding agents on this server. Shows every pi session on the machine, drives them through herdr, reviews git changes per repo, collects line comments, pushes on status changes. It is a client, not a runtime: it never forks its own agent runtime. Status: M0 in progress; scaffold compiles and serves. Plan and decisions live in docs/.

## Read first (sources of truth)

- `docs/PLAN.md`: plan v0.2. Feature inventory (V/D/R/T/P ids), milestones, risks. Feature ids are stable references; use them in tasks and commits.
- `CONTEXT.md`: binding domain glossary. Use these exact words in code, UI, and docs. Avoid the synonyms listed under each term (Session, Workdir, Repo, Changes, Comment, Steering, Driver, Status, Stop, Resume, Close).
- `docs/adr/NNNN-slug.md`: decision records. New architectural decisions go in a new ADR, not as edits to the plan.
- `docs/m0-findings.md`: verified herdr and pi facts from the M0 probes. Trust these over assumptions.

## Locked decisions (do not relitigate)

- Plain pi sessions only: jsonl trees under `~/.pi/agent/sessions/`. No pi-durable, no RPC children, no guests (ADR-0001).
- herdr is the only driver. Every start, steer, stop, resume goes through herdr's socket API. Protocol 22 is pinned; adapt on change. Abort is Ctrl-C via send-keys, not an API verb (ADR-0001).
- Stack: one Go binary, stdlib `net/http` routing, templ for HTML, Datastar Go SDK over SSE, vendored xterm.js, vanilla TS islands built with esbuild. No web framework, no client SPA, no node_modules on the server (ADR-0002).
- Android install surface: thin Kotlin WebView APK. One Activity, no IPC bridge, no bundled assets, no FCM; sideloaded, not the PWA install flow (ADR-0003).
- Security: server binds 127.0.0.1 only, fronted by `tailscale serve`. The tailnet is the boundary; no extra auth layer. Never widen this without a new ADR.

## Commands

Verified (M0):

- `./scripts/build.sh`: templ generate, esbuild bundle, then `go build` to `bin/piremote`.
- `go tool templ generate ./...` and the esbuild line inside build.sh run standalone when needed.
- `go vet ./...` and `gofmt -l .` must both be clean before a commit.
- Run: `bin/piremote --addr 127.0.0.1:8090` (loopback only). `curl localhost:8090/healthz` expects `ok`.
- herdr probes: `herdr api schema --json`, `herdr api snapshot`, `herdr agent --help`.
- Service install is manual (user unit): copy `systemd/piremote.service` to `~/.config/systemd/user/`, then `systemctl --user daemon-reload && systemctl --user enable --now piremote`.

Pins: templ v0.3.1070 and esbuild v0.28.2 as Go `tool` directives in go.mod. Datastar Go SDK gets its exact pin at first SSE use (M1).

## Architecture (planned, per PLAN.md)

- Scanner: walks `~/.pi/agent/sessions/`, parses jsonl tolerantly, tails with fsnotify. Indexes by mtime with in-memory metadata.
- herdr client: socket at `~/.config/herdr/herdr.sock`, request envelope from `herdr api schema`; CLI fallback where the socket is fiddly.
- Terminal bridge: herdr socket only. `pane.read` in `ansi` format plus `pane_output_changed` events into xterm.js; keystrokes out via `pane.send_input`. No PTY bridge, no cgo: the socket has no attach method (M0 findings).
- Git service: repo discovery by pruned scan, diffs via exec git.
- State: comments and UI prefs in a plain JSON file per session. No database.
- Web client: server-rendered HTML, Datastar SSE patches, one TS island. Mobile-first, dark default.
- Native door: views stay separate from streams (JSON POST commands; SSE accepts raw JSON beside Datastar patches; terminal WS is raw bytes plus JSON resize frames) so a Kotlin client plugs in with no server rework.

Boundaries:

- Session files are read-only to PiRemote. Never write into jsonl.
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
