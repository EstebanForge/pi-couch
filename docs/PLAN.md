# Couch Pi plan, v0.2

Rewritten after the grilling session. Decisions are in `docs/adr/`, terms in `CONTEXT.md`. Sources: my requirements, the seven reference screenshots, pi's local docs, and herdr's live API on this machine.

## Product shape

One Android-first client that shows every pi session on this server, drives them through herdr, reviews git changes per repo, and pushes when an agent needs me. The install surface is the thin WebView APK (ADR-0003); the PWA manifest stays for desktop browsers. Chat view is the default surface. Terminal, files, changes, search, and comments are tabs. ssh and herdr at the desk stay the power channels; the UI is a client, not a cage.

## Decisions locked

- Plain pi sessions only. No pi-durable, no RPC children. Sessions resume anywhere pi runs (ADR-0001).
- herdr is the only driver. Everything Couch Pi submits goes through herdr's socket API.
- Steer any session, hers or mine. Hard requirement, met by herdr tracking every live agent.
- pi sessions only. No guests. Working name Couch Pi.
- Changes are repo-level git diffs; repos found by a pruned full scan of the session workdir.
- Android-first PWA. Web push on status transitions. iOS gaps accepted.

## Feature inventory

### View

- V1. Unified session list. Every session under `~/.pi/agent/sessions/**/*.jsonl`, joined live with herdr's agent snapshot (Status, workdir, pane). Sort by last activity, group by workdir. Badges: Status, named or not, driver present.
- V2. Live tail. Transcripts update by push. The scanner watches jsonl files; herdr subscriptions drive Status. Android backgrounds kill SSE: reconnect with last-entry resumption on tab wake is part of this feature, not a follow-up.
- V3. Chat transcript. Owner messages right, assistant left, tool and thinking blocks rendered from jsonl entries. Handles branches: show the active branch; branch picker later.
- V4. Tool-call rows. Collapsed one-liner, success check, expandable output.
- V5. Inline error cards. Provider refusals and connection errors as full-width cards.
- V6. Subagent drill-in. Subagent blocks link to their own session files with a parent breadcrumb.
- V7. Copy buttons on messages and tool output.
- V8. Session header. Session name (pi `--name` or first prompt), workdir, Status, terminal toggle, menu.
- V9. Footer strip. Model, thinking level, cost, context percent. Usage and cost live in the message usage blocks; parser shapes come from real session files at M0, not guesses. Works for every session, driven or not.
- V10. Search. Over the open transcript in M4, across all sessions later.
- V11. Files tab. Browse a Repo's tree, open raw file, download.
- V12. Status badge only. Presence beyond Status is cut with guests.

### Drive

- D1. Home composer. Prompt input, folder picker, herdr target picker: existing workspace and tab, or create new ones from the phone (herdr workspace and tab create commands).
- D2. Model and thinking picker per session: the composer sends pi's own "/model <pattern>" and "/thinking <level>" editor commands through herdr prompts; queued while working, applied at the turn boundary. Catalog search is best-effort (provider env is not in the server unit); the pattern input is free-text. Shipped.
- D3. Steer any session through herdr `agent prompt`. Works on sessions started anywhere, including herdr itself.
- D4. Queue a follow-up: same mechanism; pi's own queue mode governs steer versus follow-up. Documented limit, not a UI toggle (ADR-0001).
- D5. Stop button: Ctrl-C through herdr `send-keys`.
- D6. Attachments: the composer uploads the image (POST /api/upload, sniffed type, 10MB cap) and Send appends the stored /tmp path to the prompt text; pi reads the file and the model sees it, the same flow as herdr's clipboard paste. Shipped.
- D7. Stop a Session: herdr tab or workspace close. Confirmation required. Close (hide) stays separate. The composer stop box carries Close for the session's whole workspace (D7 shipped).
- D8. Resume from the phone: new herdr tab, agent start with `pi --session <id>`.
- D9. Drive from any client. Phone, tablet, desktop: same server, same herdr.
- D10. Voice input: dictate into the composer. Whistle (Apache-2.0, 16.9 MB, seven languages, CPU) compiled to WASM and run on-device in the client; 16 kHz mono capture in the TS island. Fallback: the server execs a vendored `needle` binary on an uploaded clip (ADR-0004). Model and engine pinned; keyword biasing seeded with repo and stack names.

### Review

- R1. Changes tab per Session: the Repos found in its workdir, then per-Repo diff.
- R2. Repo discovery: full scan of the workdir for `.git`, pruned by ignore rules (rg/fd semantics) plus a denylist: node_modules, dist, build, target, vendor, .venv, caches, hidden dirs. Fast search via rg or fdfind, never raw find.
- R3. Repo picker when a workdir contains more than one Repo. Last choice remembered per Session.
- R4. Diff rendering. Line numbers, add and remove colors, wrap toggle, jump prev and next change.
- R5. Line comments: draft, edit, delete before sending. Bound to Repo, file, line.
- R6. Pending tray: count, Discard all, Send all.
- R7. Send all: one formatted prompt through the driver, listing file, line, and text per Comment. Marks Comments sent.
- R8. Comments tab: all Comments for the Session with state and jump to line.
- R9. Raw file view and download from the diff header.

### Terminal

- T1. Terminal tab: read-only peek at the session's herdr pane by default, interactive on explicit tap. The server polls `pane.read` in `ansi` format on a 1 s tick and sends keystrokes via `pane.send_input`. There is no PTY bridge and no cgo: the herdr socket has no attach method (m0-findings, ADR-0001).
- T2. Mobile key row: esc, ctrl, tab, arrows, modifier latch.
- T3. One attach client per pane at a time is herdr's rule, and only the desk CLI attach is exclusive. When the desk holds attach, Couch Pi shows a coexistence banner and stays on read-only peek. No takeover flag exists on the socket.
- T4. Read-only peek mode before attach, one tap to take the wheel.

### Platform

- P1. Server binds 127.0.0.1 only, fronted by `tailscale serve`. The tailnet is the boundary; no extra auth.
- P2. Android-first layout in portrait, one-hand reach. Same DOM widens for desktop.
- P3. Dark default, light theme included.
- P4. Installable PWA, standalone.
- P8. Android shell APK (ADR-0003): one Activity, one WebView, opens the configured URL full-screen on launch, back wired to history, no browser chrome, no tabs. First-launch URL field persisted on device, default the tailscale serve URL. Same gradle and Kotlin toolchain as Baluard, sideloaded, no store.
- P5. Web push (VAPID) on Status transitions: working to idle, blocked, or done. Transitions detected by per-agent `agent.wait` watchers over the herdr socket, not by re-reading session files; herdr 0.9.3 streams no subscription events (ADR-0005).
- P6. No lock-in. Every session stays fully usable over ssh and herdr at any moment.

### Out of scope

Guests, other agents' sessions, writing into session files, deleting sessions, sandboxing, iOS parity where Android leads.

## Architecture

One Go binary as a systemd user service, beside herdr's. Stack decision in ADR-0002: templ + Datastar over SSE, stdlib routing, vendored xterm.js, vanilla TS islands only. No web framework, no node_modules, no client SPA.

- Scanner. Walks `~/.pi/agent/sessions/`, parses jsonl tolerantly (unknown entries never crash), tails with fsnotify. Buffers partial lines until the newline before parsing. Indexes the sessions dir by mtime with in-memory metadata instead of repeated linear scans.
- herdr client. Socket at `~/.config/herdr/herdr.sock`, protocol 22. Speaks the request envelope from `herdr api schema` (request, response, event, subscription_event). Actions (prompt, keys, start, close) and subscriptions (Status events). CLI fallback where the socket is fiddly.
- Terminal bridge. herdr socket only: `pane.read` polling in `ansi` format on a 1 s tick into xterm.js, keystrokes out via `pane.send_input`. No PTY bridge, no cgo: the socket has no attach method (m0-findings).
- Git service. Repo discovery via pruned scan (rg/fdfind semantics), diffs via exec git.
- State. Comments and UI prefs in a plain JSON file per session. No database. Sent comments persist server-side; the pending tray is per-device UI state.
- Web client. Mobile-first PWA. Server-rendered HTML, Datastar SSE patches (SDK pinned to one exact version; 0.x churns), one vanilla TS island (composer, terminal) built with esbuild.
- Native door. Views stay separate from streams so a Kotlin client can plug in later with no server rework: commands are JSON POSTs, SSE streams accept a raw-JSON format alongside Datastar patches, the terminal WebSocket is raw bytes plus JSON resize frames.

## Milestones

Shipped through M4 as of 2026-10-12; nothing on the milestone list is open.

- M0 read (2-3d). Go skeleton (templ, Datastar, SSE), scanner, V1-V9, P1. All existing sessions on the phone, live. Shipped 2026-10-10.
- M0.5 shell (1-2d). P8, the icon on the phone. WebView APK pointing at the tailscale serve URL. Includes keystore, icon assets, back-button history wiring, IME and viewport handling for the composer. Shipped 2026-10-11 minus keystore: debug-signed with the Pi Agent adaptive icon and dark splash, page draws under the transparent status bar with injected safe-area insets (ADR-0003), verified live on the the phone over WiFi adb.
- M1 drive (3-4d). D1, D3-D5, D8, D9. Start in a herdr tab, steer, stop, resume. Shipped.
- M2 review (2-3d). R1-R9, V11. Shipped.
- M3 terminal (2-3d). T1-T4. Shipped, on the socket path (no PTY).
- M4 polish (1-2d). V10 in-transcript search, D7 close, P2-P4, plus D2 and D6. Shipped 2026-10-12 (D2 rides pi's /model and /thinking editor commands through herdr prompts; D6 rides image paths in the prompt text). P3 dark default shipped as the dracula theme.
- M5 push (1-2d). P5, webpush-go. Shipped 2026-10-11 with a deviation: transitions come from per-agent `agent.wait` watchers, not socket subscription events, which herdr 0.9.3 does not stream (ADR-0005). Server-side chain E2E-verified; delivery on the phone rides the WebView service worker until FCM is demanded.

About three weeks end to end. Dogfood rule: from M0 on, Couch Pi is built with pi sessions driven through Couch Pi where possible.

## Risks and open questions

- herdr protocol churn. Pin protocol 22, adapt on change; the CLI subcommands are the stable skin.
- Prompt fidelity. Steer versus follow-up rides on pi's queue mode; revisit only if it bites.
- Attach contention. herdr allows one attach client per pane; read-only peek avoids surprise takeovers.
- Big workdirs. Repo scan leans on ignore rules; measure on the Wicket stack before trusting it.
- jsonl tree branches. M0 shows the active branch; branch picker is unscheduled.
- Ephemeral sessions (`--no-session`) are invisible by design.
- Whistle is days old and its WebView WASM speed is unmeasured; D10 carries a server-exec fallback.

## References

- ADR-0001 for the herdr-only decision and the terminal socket path.
- ADR-0003 for the APK install surface.
- ADR-0004 for the voice-input placement.
- ADR-0005 for the push transition source.
- herdr agent guide: https://herdr.dev/agent-guide.md
- Pi Durable announcement (context for the rejected option): https://earendil.com/posts/pi-durable/
