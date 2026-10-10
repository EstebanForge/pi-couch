# PiRemote plan, draft v0.1

Feature-complete first draft. Sources: my requirements, the seven screenshots of Mario's private UI, his public posts (videos not watchable; topology taken from them), and the Pi Durable announcement. Every feature gets an ID so milestones and issues can reference them.

## Product shape

One web page, phone-first, that shows every pi session on this server and drives the ones it started. Chat view is the default surface. Terminal, files, diffs, and comments are tabs. ssh at the desk stays the power channel; the UI is a client, not a cage.

Topology note from Mario's setup: his agents run on his phone and his tablet connects to it. Same software shape inverted: our agents run on the server and any device connects. The consequence is the same: multi-client by design, viewport-agnostic.

## Feature inventory

### View

- V1. Unified session list. Every pi session on the machine: classic TUI sessions from `~/.pi/agent/sessions/**/*.jsonl` and UI-started durable sessions. Sort by last activity, group by project folder. Badges: running or idle, source (classic or durable), model.
- V2. Live tail. Open transcripts update by push over WebSocket, no refresh needed. Refresh stays as a manual fallback.
- V3. Chat transcript. Owner messages right-aligned, assistant prose left, name and role above messages (guests show display name plus role, as in the screenshots).
- V4. Tool-call rows. One collapsed line with a command preview, success check, expandable output. Detail view for long output.
- V5. Inline error and refusal cards. Red full-width card with the provider message, as in the Codex refusal screenshot.
- V6. Subagent drill-in. A subagent block shows model, parent session link, and tool list, and navigates to the child transcript with a parent breadcrumb.
- V7. Copy buttons on assistant messages and tool output.
- V8. Session header. Title from the first prompt, project folder, participants icon with attached-client count, per-session menu, terminal toggle.
- V9. Footer strip. Model and thinking level, running cost, context percent. Cost needs durable sessions or usage data in the jsonl (checked in M0).
- V10. Search tab. Full text over the open transcript in M2, across all sessions in M4.
- V11. Files tab. Browse the session working tree, open raw file, download.
- V12. Presence. Attached clients per session: count in the header, display names for guests.

### Drive

- D1. Home composer. "What should Pi do?" input, folder picker with recent projects, model chip.
- D2. Model and thinking level picker per session, editable later.
- D3. Steer a running session. Message joins the current work (pi-durable `whenBusy: "steer"`).
- D4. Queue a follow-up while busy. Distinct from steer, chosen per message.
- D5. Stop button while streaming (abort).
- D6. Attachments. Images into the prompt from the phone (camera roll, file picker).
- D7. Close a session. Durable: abort plus archive. Classic: stop its tmux session. Confirmation required.
- D8. Resume a classic session in the background from the UI. Spawns `pi --resume` in tmux, then views and tails it.
- D9. Drive from any client. Steer, queue, and stop work from the phone, the tablet, and the desktop alike.

### Review

- R1. Changes tab per session. Changed-file count badge, per-file diff list.
- R2. Diff rendering. Line numbers, add and remove colors, wrap toggle, jump prev and next change with position indicator.
- R3. Line comments. Draft on any line, edit and delete before sending.
- R4. Pending tray. Comment count, Discard all, Send all.
- R5. Send all posts the tray as one steer message with file and line references. Durable sessions only; classic sessions get view and note only.
- R6. Comments tab. All comments for the session with state (pending, sent) and jump to line.
- R7. Raw file view plus download from the diff header.
- R8. Refresh re-runs `git diff` in the session cwd.

### Terminal

Optional, late, and never the default surface.

- T1. Terminal tab per session. Classic: attach its tmux. Durable: a shell in the session cwd.
- T2. Mobile key row: esc, ctrl, tab, arrows, and a toggleable modifier latch.
- T3. Multiple terminal tabs per session.
- T4. Read-only viewer as the safe default, writable on explicit toggle.

### Platform

- P1. Server binds 127.0.0.1, exposed with `tailscale serve`. No auth layer; the tailnet is the boundary.
- P2. Phone-first layout in portrait. The same DOM widens for tablet and desktop.
- P3. Dark and light themes. Dark is the default.
- P4. PWA. Installs to the home screen, runs standalone.
- P5. Notifications. Push or ntfy when a session needs input or finishes. My addition to the original list; without it, phone-first means babysitting.
- P6. Guests. A second person attaches with a display name and role. Owner and guest permission levels come later (Mario's UI shows a guest mode knob).
- P7. No lock-in. Every session stays fully usable over ssh at any moment.

### Out of scope for this draft

Writing into classic TUI sessions, auth beyond the tailnet, multi-server, sandboxing, anything resembling the AITC workspace product.

## Architecture

One Bun + TypeScript process, run as a systemd user unit on the server. Binds 127.0.0.1. `tailscale serve` fronts it.

Two session sources behind one gateway interface:

- Classic scanner. Walks `~/.pi/agent/sessions/<project>/*.jsonl`, parses entries tolerantly (unknown shapes never crash), serves list and transcript over a per-session WebSocket, tails with fs.watch.
- Durable bridge. Owns one `@earendil-works/pi-durable` harness with SQLite storage under `~/.local/share/piremote/`. Creates conversations, steers, aborts, and streams commit ops to clients. Version pinned; isolated behind this module so API churn stays contained.

Web client: mobile-first SPA, one WebSocket per open session, tabs for Chat, Terminal, Files, Changes, Search, Comments.

## Milestones

- M0 read (2-3d). V1-V9 for classic sessions, P1. Useful the day it ships: all existing sessions on the phone.
- M1 drive (3-5d). Durable harness live: D1-D6, D9, V9 cost, V12 basic presence.
- M2 review (2-3d). R1-R8, V10 transcript search, V11 files tab.
- M3 terminal (1-2d). T1-T4.
- M4 hygiene (1-2d). D7, D8, V10 across all sessions, P2 and P3 polish.
- M5 reach (2-3d). P4, P5, P6.

Core (M0-M3) is about two weeks of focused work. M4 and M5 pad it to three.

Build order rule: PiRemote gets built with pi sessions driven through PiRemote from M0 onward. Dogfood from the first commit.

## Risks and open questions

- pi-durable is experimental. Pin the version, isolate the bridge, upgrade on purpose.
- The classic jsonl schema is pi-internal. Tolerant parser, test against my real session history. Worst case is a degraded view, never lost work.
- Cost and context data for classic sessions depends on what pi writes to the jsonl. Decide in M0; degrade to showing nothing.
- iOS keyboard behavior: enter-to-send versus newline. Decide in M1.
- Guest identity over tailscale: `tailscale whoami` headers or a shared-secret fallback. Decide in M5.
- Title generation for durable sessions: first prompt is the v1 answer; a generated title can come later.

## References

- Pi Durable announcement: https://earendil.com/posts/pi-durable/
- Mario's public posts on phone-first agent work: agents run on his phone, tablet connects to it, diffs read on the phone.
- Local pi docs for session storage and the SDK.
