# PiRemote plan

Working notes. Temp name; rename later.

## Goal

One web page, tuned for a phone in portrait, over Tailscale, that drives the pi agents on this server.

- Watch ALL pi sessions on the machine, including classic TUI sessions started over ssh.
- Start a session in any folder. Steer, queue follow-ups, stop.
- Optional raw terminal per session. Chat view is the default, terminal is the escape hatch.
- Review the working diff per session, leave line comments, send them to the agent in one shot.
- Retake control on the PC over ssh at any time. The UI is a client, not a cage.

## Non-goals

Multi-tenant, custom auth (the tailnet is the boundary), sandboxing (the machine is trusted), anything like the AgentsInTheCloud workspace product with containers per repo.

## Architecture

Server: one Bun + TypeScript process, run as a systemd user unit. Binds 127.0.0.1. Exposed with `tailscale serve`.

Two session sources:

- Classic pi sessions. Scan `~/.pi/agent/sessions/<project>/*.jsonl`, parse entries, render the chat view, live-tail with fs.watch. Read-only plus "open in terminal" (attach tmux). The jsonl schema is pi-internal, so the parser stays tolerant of unknown entries.
- Durable sessions. A `@earendil-works/pi-durable` harness owned by the server, SQLite storage in `~/.local/share/piremote/`. The UI creates conversations, steers with `submit(..., whenBusy: "steer")`, aborts, and watches through `viewState()` + `subscribe()`. Remote clients use the commit-op stream over WebSocket.

UI: mobile-first. Home = "What should Pi do?" composer with folder picker and model picker. Session view = chat transcript (bubbles, collapsed tool rows, subagent links) with a footer showing model, cost, and context use. Tabs per session: Chat, Terminal, Files, Changes, Search, Comments.

Diff review: `git diff` in the session cwd, per-file view, line comments stored server-side. "Send all" posts the comments to the session as one steer message.

## Milestones

- M0 viewer (2-3d): scan, list, transcript, live tail of classic sessions. This alone delivers "see how the other agents are doing while I am away".
- M1 drive (3-5d): durable harness wired in. New session anywhere, model picker, steer, queue, abort, cost/context footer.
- M2 review (2-3d): Changes tab, line comments, Send all.
- M3 terminal (1-2d): xterm.js to a tmux attach, per session.
- M4 hygiene (1-2d): close/kill a session, resume a classic session in the background from the UI.
- M5 polish: guests (tailscale identity), PWA, notifications.

## Risks and open questions

- Pi Durable is experimental. Pin the version, keep it behind one server module so churn stays contained.
- The classic session jsonl format is not a public contract. Parser must never crash on unknown shapes.
- Steering a classic TUI session from the web is out of scope; those get view + terminal only.
- iOS keyboard behavior (enter-to-send vs newline) needs a decision in M1.
- Build PiRemote using pi sessions driven through PiRemote. Dogfood from M0 onward.

## References

- Pi Durable announcement: https://earendil.com/posts/pi-durable/
- Inspiration: Mario Zechner's private phone UI (not published).
- Local pi docs: session storage and SDK under the installed pi package.
