# ADR-0011: hc only; Spaces replace workspaces

Date: 2026-11-09
Status: Accepted (Esteban, 2026-11-09: "remove all non-HC support. We will focus only on hc from now on.")
Amends: ADR-0008 (the two-driver seam), ADR-0001 (one live driver), ADR-0005 (push watcher), ADR-0010 (rewind). Related: ADR-0007, docs/hc-findings.md.

## Context

ADR-0008 shipped two drivers behind one seam and kept herdr (protocol 22) as a supported backend while the hc waves landed. The herdr-classic daemon is scheduled for retirement on this server, and every remaining herdr-only code path is maintenance debt: a duplicate dialog implementation, a duplicate create flow, the `herdr observe` terminal hub, and the per-agent push watcher.

Meanwhile hc grew the surface that made the old workspaces make sense. Verified live on hc 0.1.19 (2026-11-09):

- The herdr desktop app keeps each machine's spaces, tabs and pane splits in the core's opaque `herdr-app` document, readable with `doc.get {"name":"herdr-app"}`: `{spaces:[{id,name,folder,tabs:[{id,name,root}]}],version}`. Writes are newest-wins with no merge, so Couch Pi only ever reads it.
- Terminals the app placed carry `herdr.space` and `herdr.tab` labels (machine-local ids) plus a `where` display label; `terminal.list` already returns every label.
- The documented adoption contract (herdr-app `defaults/AGENTS.md`): a terminal started with `--label herdr.space=<id> --label herdr.tab=<id>` shows up in that space's tab, even one an agent starts itself. An id the app does not know becomes a new unnamed space or tab, so an unknown tab id must never be guessed.
- `terminal.keys` exists (the ADR-0010 hc rewind refusal cited its absence; that blocker is gone).

## Decision

1. **One driver.** The `herdr` driver, its client package, the observe hub, the classic push watcher, and the `driver` config key are removed. hc is the only engine. A stale `driver = "herdr"` in a config file refuses to start with a migration hint; `driver = "hc"` stays a no-op. The wire shapes both paths shared (`DrivenInfo`, `Agent`, `PaneRead`, `ErrNotDriven`) move into `internal/hc`.
2. **Spaces replace workspaces.** The old workspaces surface is renamed, not shimmed: `GET /api/spaces` lists the app document's spaces (`{id,name,folder}`; unnamed spaces fold to the folder basename, the name the app itself paints). The create call takes `space_id` instead of `workspace_id`, and the Quick flow's `label` matches a space by name before falling back to the posted cwd.
3. **Spawning into a space adopts via labels.** A `space_id` create spawns pi at the space's folder (jailed under home like any client cwd) with `herdr.space=<id>` plus the doc's first tab id as `herdr.tab`. No tab in the doc means no labels: the spawn stays folder-only rather than minting ids the app would turn into stray spaces.
4. **The document is read-only to Couch Pi.** `doc.set` is never called; the app owns the tree.
5. **Rewind works under hc.** ADR-0010 refused it for the missing keys verb; the dispatch now rides `terminal.send` and the desk refill clears with one `terminal.keys ctrl+c` after the bridge verdict.

## Consequences

- The dual dialog implementation collapses to the hc screen-read path. hc's send appends Enter, so buffer rewriting rides the writer attach instead (kind 6 raw input, per-line key clears, content-matched before submit): see ADR-0012, which restores editor answering that this ADR had recorded as impossible.
- The create flow loses herdr's workspace split semantics; "spawn into a space" is folder + labels, not a pane split in a tree Couch Pi owns.
- `GET /api/workspaces` and the `workspace_id` field are gone with no compatibility alias; the SPA ships in the same binary and the Android APK ships the same SPA.
- Renaming waves: `herdr-clipboard` style names in configs stay untouched; `hc_socket` keeps its name (it names the core's socket, not a driver).
