# ADR-0013: Phone spawns adopt by folder

Date: 2026-11-18
Status: Accepted (Esteban, 2026-11-18: deepest parent space wins; peer-reviewed change-first, blockers folded in).
Amends: ADR-0011 (clause 3, the space-only adoption).

## Context

ADR-0011 taught Couch Pi the herdr app's adoption contract: a terminal spawned with `herdr.space` + `herdr.tab` labels lands in that space's tab, and unlabeled terminals render nowhere in the app. Only two spawn paths attach labels — an explicit `space_id` create and a Quick flow whose label matches a space by name. The other paths spawn unplaced: a folder-only create (`resolveDir` default branch) and every session resume (`resumeSessionHC` passes nil). Live evidence from 2026-11-18 (`~/.local/state/herdr-core/default/terminals.json`): the phone's Couch list shows every session because it reads hc directly, but the desktop app showed only the labeled spawns. A phone-made session was invisible on the desk.

## Decision

1. **Folder inference.** Folder-only creates, unmatched Quick labels, and resumes derive the adoption labels from the app document instead of spawning unplaced: the space whose folder is the spawn folder, else the deepest ancestor folder, feeds the same `spaceLabels` the `space_id` path uses. No owning space behaves exactly as before — the plain folder spawn.
2. **Deterministic match.** Skip spaces with an empty folder and remote-machine spaces (ids carry a `/` prefix; their folders are another machine's paths). A deeper folder always wins over a shallower one — a tabless deepest match wins the match and then labels nil (labels need a tab; falling through to a shallower parent would misplace the session). Duplicate folders prefer the space with a tab, then document order. Prefix checks are separator-aware (`~/Dev/foo` never matches `~/Dev/foobar`).
3. **Path disagreement.** The spawn folder arrives from three sources (client cwd, pi's session header, the app document) and none is canonicalised. Compare clean paths first, then retry once with `EvalSymlinks` on both sides.
4. **Fail open, cheaply.** Inference serves an optimization, so its document read rides a short budget (`inferBudget`, 3s) independent of the create budget; an unreadable or slow document costs the labels, never the spawn. The document is fetched once per request. The explicit `space_id` lookup keeps its loud 502 (ADR-0011: an action surface fails loudly).
5. **Resume caveat.** A resume whose project folder is gone falls back to `$HOME`, and the fallback never infers: a catch-all space at `$HOME` would swallow every orphaned session. Placement stays a guess by construction — the app owns the tree after spawn, and the first tab at spawn time is the target, exactly as for `space_id` creates.

## Consequences

- Every phone create and resume lands in the desk app when its folder (or an ancestor) is a space folder; the Quick space at `~/Dev` is the working catch-all Esteban runs, and it inherits anything under `~/Dev` that no deeper space owns.
- A stale document can now misplace on every path instead of only `space_id` creates: deleted spaces still listed in the doc adopt into unknown ids and the app mints strays. Accepted; the doc is the app's own persisted truth and ids are stable across app restarts.
- The create sheet's folder picker no longer implies "unplaced": it implies "the space that owns this folder".
