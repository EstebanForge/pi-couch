# HANDOFF: Couch Pi, post-rename, post-daisyUI

Written 2026-10-11 by the pi session that started in the now-dead `PiRemote` folder. Restart pi inside `/home/you/Dev/EstebanForge/couch-pi`, then read this file top to bottom.

## 1. Active Goal

Couch Pi (slug `couch-pi`, renamed from PiRemote) is a phone-first web client for pi sessions over herdr. M0 (read surface: list, transcript) and M1 (steer, terminal, changes, files, comments) are built and committed. The daisyUI 5 conversion just landed (commit `f88c943`). Acceptance for the conversion was: every screen renders on daisyUI coffee at 360 px dark, verified by screenshots in `~/Dev/Screenshots/couchpi-*.png`. Done.

Remaining backlog, in the user's stated order:

1. Replace the daisyUI CDN with a compiled Tailwind v4 + daisyUI build (`@tailwindcss/cli` + `daisyui` as devDependencies, `internal/web/static/css/input.css`, compile in `scripts/build.sh` before `templ generate`, compiled output committed so the Go binary stays node-free at runtime). Needs a short ADR-0005 recording the build concession against ADR-0002. User deferred this: "then we worry about compilation" — ask for the go before starting.
2. ADR-0002 wording update for the same concession.
3. Notifications slice (docs/ui/10-notifications.md), then the sideloaded APK shell (ADR-0003).

## 2. Verified Facts

- Repo: `/home/you/Dev/EstebanForge/couch-pi`, module `github.com/EstebanForge/couch-pi`, main pushed through `f88c943` (17 commits).
- Old path `/home/you/Dev/EstebanForge/PiRemote` no longer exists. This handoff was written from a session whose default cwd is that dead path.
- Verified commands live in `AGENTS.md` at the repo root; it is the first read. Short form: `./scripts/build.sh`, `go vet ./...`, `gofmt -l .`, `go test ./...`, smoke run `./bin/couch-pi -addr 127.0.0.1:18112` then `curl 127.0.0.1:18112/healthz` expects `ok`.
- Frontend stack of record: daisyUI 5 CDN (`daisyui.css` + `theme/coffee.css`) plus `@tailwindcss/browser@4` script in all seven page heads, loaded before `app.css`. `app.css` is pruned to 75 lines: diff renderer (`.diff`/`.dl`), `#term`, `#keyrow`, `#wheel-btn.on`, `.stopbox:empty`.
- `herdr` 0.9.3 ground truth, the pi jsonl format notes, and every probe result are in `docs/m0-findings.md`.
- Sessions whose recorded cwd still points at the old `PiRemote` path render "not a git repository" on Changes and Files. Self-heals when the user starts new sessions from the renamed folder.
- Untracked `.design-ref/` (six design-reference screenshots) is deliberately uncommitted and unignored. Never commit it. Never mention the reference app by name in code, docs, or commits; the theme name is `coffee`, a daisyUI built-in.
- Screenshot directive: shot sets land in `~/Dev/Screenshots`, prefix `couchpi-`, leave pre-existing files untouched.
- agent-browser pi extension is broken for this session (stored launch dir is the dead path). Workaround: run the `agent-browser` CLI via exec with the repo workdir. Fresh session after restart should retest the native tool.

## 3. Rejected Paths

- CSS frameworks: Agy ranking runs 1 and 2 (custom tokens, Open Props, Halfmoon) are superseded; the user picked daisyUI 5 (run 3, approved). Do not reopen.
- MUI, Ionic core, Web Awesome, @material/web: rejected for React coupling, Shadow DOM clashes with Datastar fragment swaps, and frozen maintenance respectively.
- Patching layer conflicts with higher-specificity overrides: wrong tool. daisyUI 5 styles live in CSS cascade layers; when a daisyUI class renders wrong, hunt `app.css` for an unlayered rule and prune it.
- PTY bridge (creack/pty) for the terminal: rejected. Terminal runs over the herdr socket (`pane.read` polling + `pane.send_input`), no cgo.

## 4. Active Files

Working tree is clean except untracked `.design-ref/`. Nothing in flight.

Files touched by the last commit `f88c943` (daisyUI conversion): `internal/web/pages/{tabs,list,transcript,changes,files,terminal,comments}.templ`, `internal/web/static/app.css`, `internal/web/static/js/main.ts`.

Expected first files of the compile step: `scripts/build.sh`, `internal/web/static/css/input.css` (new), page head edits to drop the CDN links, `docs/adr/0005-*` (new).

## 5. Next Action

Orientation in the fresh session: `cd /home/you/Dev/EstebanForge/couch-pi && git status --short && ./scripts/build.sh && go test ./...` — expect a clean tree, `BUILD-OK`, all suites green. Then ask Esteban for the go on the compile step (backlog item 1); do not start it unsolicited.

## 6. Suggested Skills

- `commit`: mandatory before every commit; repo conventions in it win over defaults.
- `esteban-voice`: any text authored under Esteban's name (PRs, comments, issues).
- `wicket-simplified-english` or `ste-writing`: docs and commit-message tone.
- `test-audit`: when adding tests for the compile step or new slices.
