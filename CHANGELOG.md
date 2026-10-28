# Changelog

Started 2026-10-09 as a plan and a domain glossary. It grew into the phone I drive my work through. Dates below are approximate; the git log holds the precise trail.

## 2026-10-28

- Stuck dialog fix: pi shows the same dialog twice (a commit preview re-presented an hour later) and every answer bounced off a dedupe claim that never expired. Claims now expire after a minute, so the second showing answers instead of refusing forever, Cancel included.
- The model catalog learns to refresh: `POST /api/models/refresh` re-runs `pi --list-models` past the cache (memory one hour, file seven days) and the phone gets a refresh button in the model sheet and in Settings, so provider or scope changes show at once. Waiting-on-you spinners turn yellow (`text-warning`, constant across schemes) instead of the theme's accent teal.

## 2026-10-25 → 2026-10-27

- Went public and stayed that way: tag a version and CI builds and publishes the binaries, and the server updates itself from GitHub releases on a 12-hour check (atomic swap, rolls back if a boot fails). A shared token gates the API by default; the first-run screen asks for it.
- Session resume from the phone: closed sessions come back with one tap. The terminal went properly live (herdr observe deltas instead of snapshot slides) with a Wide mode you can actually read.
- The quick-actions panel: pi skills, slash commands, prompt templates and bash one-liners, filterable, filled or run right there. Sessions rename from the chat, queued follow-ups show as a strip, thinking and skill expansions fold.
- Antigravity bridge tool activity renders in the chat: native ACP rows with diffs, thinking stripped of terminal colors. Quotas got honest, cached behind a five-minute cap and read from the agy CLI's own report because the summary endpoint hides the Gemini pools, and the Claude fetcher stopped rotating Claude Code's tokens.
- Under the hood: the transcript stream sends appends instead of full resets on long threads, every dialog answer outcome lands in the journal, and the model catalog stopped truncating rows past an old cap. Versions moved to dates too: YYYYMMDD, with .1, .2 for same-day re-releases, a shape the self-updater's dotted compare already understands.

## 2026-10-24

- Machine settings moved into `~/.config/pi-couch/config.toml` (TOML, commented, optional). The repo carries none of my server's identity anymore, and the history got scrubbed to match. README rewrite, MIT license, this changelog: the first pass at making this something other people can run.

## 2026-10-22 → 2026-10-23

- Quick agent create rebuilt: pick a prompt, model and thinking level at creation (pi launch flags, no TUI automation), align the new-agent sheet with it, and remove the promptless pending-screen experiment. Create reliability got real: pi ready-gate, first-turn recovery, no duplicate prompts, a client watchdog for dead streams.

## 2026-10-19 → 2026-10-21

- The dialog arc finished: multi-question forms with tab navigation and auto-submit, free-text answers, editable commit messages from the phone even when pi's editor clips long drafts (the answer reads the session file instead of scraping the screen). Color schemes arrived (Cobalt Next Dark and Dracula, live-switching, terminal included), the app became Pi Couch, and stale queued bubbles learned to expire.

## 2026-10-18

- Big phone-polish day: composer streamline with the context line, the memory chip, priority sessions on home, Font Awesome icons everywhere, images rendered and zoomable in chat, live todos with retirement, API errors shown instead of swallowed.

## 2026-10-16 → 2026-10-17

- The pi-durable evaluation: verdict no, but two patterns stolen (dialog answer memos, busy-send dispositions). pi-pocket analyzed the same way. FAB speed dial with quick agents, promptless pending chat, directory picker, history paging, the model catalog cache. First commits shipped from the phone itself.

## 2026-10-13 → 2026-10-15

- The React SPA became the whole site; the templ pages died. Session screen, terminal peek, changes review with line comments, push notifications, back-gesture history markers. Deployed as a systemd user service behind tailscale serve, in daily use from here on.

## 2026-10-09 → 2026-10-12

- Plan, glossary, ADRs, and the M0 probes: what pi's session files really look like, what herdr's socket actually speaks, what herdr-web-ui taught us. No product yet, all ground truth.
