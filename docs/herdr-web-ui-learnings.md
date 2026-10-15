# herdr-web-ui learnings: patterns to port into Couch Pi

Extracted 2026-10-12 from `github.com/devswha/herdr-web-ui` v0.4.8, commit `5a9fe39`, clone at `/tmp/herdr-web-ui`. Three independent reviews (direct, peer via Gemini/antigravity, peer via Claude Code) converged on the facts below. Every claim about their code carries a citation; claims marked [live] were verified against this server's herdr 0.9.3, not just read in source.

Scope: Couch Pi is a single-user internal tool. Nothing here is about competing with them. This doc is a parts bin: verified herdr behavior we did not know, and implementation patterns worth borrowing. Port decisions land in ADRs; this doc is the evidence base.

## 1. herdr facts we did not know (corrects m0-findings)

### 1.1 `herdr terminal session observe` exists [live-verified]

`herdr terminal session observe <paneId> --cols N --rows N` streams the pane's output as NDJSON on stdout. It is a read-only viewport: unlike `terminal attach` it never takes resize ownership and never locks the pane grid (their measurement on herdr 0.9.3, `server/AGENTS.md`). Available since herdr 0.7.2. Our `docs/m0-findings.md` claim "the socket has no output push, poll `pane.read` at 1s" stays true for the socket API but is obsolete as a bridge design: the CLI path pushes. Point the subprocess at the right socket with the `HERDR_SOCKET_PATH` env var (`server/watch.ts`).

Frame format, decoded in `WatchStreamParser` (`server/watch.ts:13-64`):

- `{"type":"terminal.frame","encoding":"ansi","bytes":"<base64 ANSI>"}`: one viewport delta. `encoding` says what the bytes are; `bytes` is always base64.
- `{"type":"terminal.closed"}`: clean end of stream.
- Unknown types and unparsable lines are ignored, never fatal.
- UTF-8 must be decoded incrementally across base64 chunk boundaries (`TextDecoder` in stream mode); a multibyte character can straddle two frames.

Also found by the live probe: `herdr terminal session control` exists and carries a `--takeover` flag. Unexplored; candidate for the interactive path if input latency ever hurts.

### 1.2 Socket client discipline (`server/herdr/client.ts`)

They open one connection per request over `~/.config/herdr/herdr.sock` (Windows: named pipe under `%APPDATA%\herdr`). The comment that justifies it: a reused socket never sees a second reply. Our client should match this one-request-per-connection shape unless we have evidence otherwise.

### 1.3 Pane event surface (`server/AGENTS.md`, `server/collector.ts`)

- Per-pane event `agent_status_changed`, plus global `pane.created` / `pane.closed` / `pane.exited`. Their collector subscribes to every pane in the snapshot plus the global lifecycle stream.
- Streams can die with `events_lost`; the collector reopens with 250 ms to 5 s backoff, then reconciles from a snapshot taken after the new subscription starts (not before: a snapshot taken before re-subscribing races the lost window).
- Status transitions worth copying into our push watcher: `working`/`blocked` are "busy"; `idle` and `done` are both "at rest", and a finish nobody saw is reported by herdr as `idle`.

### 1.4 Terminal input quirks, independently confirmed

Their `server/AGENTS.md` documents the same behaviors our m0 probes found, plus numbers:

- herdr holds a lone ESC on the pty for about 150 ms; a key sent right after an ESC-ending keystroke must wait (`TYPED_SETTLE_MS`), or it overtakes the typing.
- Keys route through `pane.send_keys`; pasted/typed text through `pane.send_text`. Keystrokes wait `KEY_SETTLE_MS` after the pane's last pty keystroke so chords never interleave.
- Attach refusal `attach_held` is a state, not an error to retry through: the desk CLI holds the pane exclusively (matches ADR-0001).
- The layout `rect` is neither the terminal's size nor the border-less grid; it includes the border and lags resizes. Only an attached client's size is real. Never derive pane geometry from the rect.

## 2. Terminal bridge: replace the 1 s poll (our worst UX today)

Their read-only path is exactly what our terminal tab peek should be (`server/watch.ts`):

- Spawn `herdr terminal session observe <paneId> --cols C --rows R` with `HERDR_SOCKET_PATH` set; `stdin: ignore`, pipe both stdout and stderr.
- Parse NDJSON incrementally (buffer until newline, same rule as our jsonl parser); decode `terminal.frame` base64 with a streaming UTF-8 decoder; end on `terminal.closed`.
- Keep a 4 KiB stderr tail while the process runs; on non-zero exit, that tail is the only diagnostic herdr gives.
- Drain both pipes fully before reporting exit: the last frame and the failure reason can arrive after the exit notification.
- Kill the subprocess when the last viewer leaves. One stream per pane per server, fanned out to all viewers (they call this `SocketData.watches`, one observer per pane and connection).

Port sketch for `internal/server/terminal.go`: keep `pane.send_input` for typing, replace the `pane.read` tick with one `exec.Command` observe process per viewed pane, fan frames to xterm.js over the existing terminal WebSocket, and fall back to the old poll if the process dies repeatedly (see the risk note in section 8). No PTY, no cgo, no node-pty sidecar. Their interactive sidecar (node-pty wrapping `terminal attach`, ACK flow control, pause at 256 KiB / resume at 64 KiB / 4008 on stall) is machinery we do not need at single-user scale; `send_input` already covers typing, and `session control --takeover` is the escape hatch if it ever does not.

## 3. pi session files are a tree, not a log (parser rewrite input)

`server/pi-tree.ts` is the highest-value file in the clone. Verified structure of pi's jsonl:

- Append-only entry list; every entry carries `id` and `parentId`. Parents always precede children (verified by them across a 147-file real store), so the last line is the leaf and one forward pass indexes the whole tree.
- `/tree` moves pi's leaf pointer to an earlier entry without rewriting the file; later appends grow a branch beside the abandoned one. The conversation pi shows is the path from the last-written entry back to its root; side branches stay in the file, unread by any linear reader.
- The session header line has no `id` and is not a tree node.
- Entry shape they rely on: `{id, parentId, type, message:{role}, summary}`; `type:"branch_summary"` entries on the live path carry pi's own summary of an abandoned branch.

Their indexing pattern, all worth copying:

- Parse once into an index of entries with `[start,end)` byte ranges, then rebuild the transcript by reading merged ranges, not per-line JSON. Adjacent entries merge, so an unbranched session is one range.
- Extend the index incrementally: only scan past the last line boundary. Incomplete trailing lines wait for their newline (identical rule to our parser's partial-line buffer).
- Chunked reads at 4 MiB with an "open line" buffer: a single record with an image runs to many megabytes; naive per-chunk line joining copies it once per chunk.
- Cache validation without mtime: key by `dev:ino`, remember `scanned` offset and the 64 bytes before it, and treat any tail mismatch as a different file (restore, import, rewrite). mtime lies; the tail does not.
- Cap the active branch at 64 MiB (largest real branch they measured: 23.3 MiB). Over cap: report `branch_unreadable` and fall back to terminal scrollback instead of throwing.

`piAbandonedTurns` gives the UI vocabulary for branches: a branch is a live entry that has a child off the live path (a "head"); turns are counted only inside those subtrees, user/assistant messages only, tool results folded into the turn that asked for them. A session pi never branched costs zero: no walk, no count.

Impact on us: our parser reads linearly, so any session that ever used `/tree` renders tangled or wrong. Port the index + leaf-walk into the Go parser; add "N turns left behind on M branches" to the session header.

## 4. Push watcher upgrades (refines ADR-0005)

`server/push.ts` implements the same VAPID + subscription store we have, with four ideas we lack:

- **Debounce and cancel.** An alert waits before sending: `short` 10 s for blocked/questions, `long` 60 s for a finish, and a finish is only worth the `long` wait if the turn ran at least `longTurn` 60 s. The pane's next status change calls the waiting alert off entirely: a question answered at the desk, or a finish followed by the next prompt, never reaches the phone. This is the single biggest noise reduction over our current same-status dedupe.
- **Focus suppression.** No device gets an alert while any connected page is visible and focused (`inUse` over the WebSocket). Checked when the timer fires, not when scheduled, so a window brought forward during the wait still holds the alert. Test pushes ignore it.
- **Per-device alert prefs.** Each subscription carries: questions on/off; finishes off / always (10 s) / long-only (60 s, turns of 60 s or more). Unknown-length turns (first sighting mid-turn) count as worth telling.
- **Event-loss resync.** After `events_lost`, the snapshot becomes the baseline only for panes with no newer event; panes that vanished have waiting alerts called off; changes that happened unseen are never announced.

Mechanics worth stealing wholesale:

- One notification tag per pane, so a newer push replaces the older one on the lock screen. TTL 12 h (a blocked agent is still blocked when the phone regains signal). Urgency `high` only for blocked.
- Endpoint validation is an SSRF guard: https only, `p256dh` must decode to exactly 65 bytes (uncompressed P-256 point), `auth` to 16. Fetch with `redirect: "error"`: a push service never redirects, and following one would forward alerts to wherever the endpoint points, past the https check.
- State files written atomically (tmp + rename, 0600, dir 0700). A malformed `vapid.json` is an error, never a rotation: regenerating the key silently orphans every subscribed device. 404/410 from the push service prunes the subscription.
- Our push-subs file lives under `~/.local/state/couch-pi/` already; the write and validation patterns apply as-is.

## 5. Approval and question cards (candidate feature D8)

Their `server/prompt.ts` (3,528 lines) detects interactive prompts by screen-scraping the pane's ANSI text with per-agent fingerprint regexes: Claude menus ("enter to select…"), Codex approval headers ("Would you like to run…"), OmO ask forms, omp select hints. The card flow: detect menu shape and options from scrollback, render tappable options, then re-verify the screen still shows the same menu before sending the answer keys. A stale fingerprint refuses to fire.

Two ideas matter for us:

- For pi we do not need ten agents' fingerprints, only pi's own TUI menu shapes. Same technique: fingerprint hint lines + option rows (`❯`/`›`/`>` selection markers, numbered options), card per pending question, re-verify before send, fallback to a plain "type the number" card when no fingerprint matches.
- **Guard regexes are the sharp gotcha.** They explicitly refuse to card Claude's `/model` list and `/effort` slider, because Enter on those saves a default for all future sessions (`CLAUDE_EFFORT_GUARD_RE`, `CLAUDE_MODEL_GUARD_RE` in `server/prompt.ts`). Our model/thinking picker sends `/model` itself; if we ever card pi's menus, a misfire on pi's own settings screens could change persistent state. Anything that can save a default must be excluded from carding.

Related, smaller: their `PendingRequestBook` (`server/pending-requests.ts`) is an idempotency receipt book for chat submits: submit ids carry fingerprints, retired ids cannot replay after eviction, unresolved submits are capped at 256. Direct template for guarding double-taps on our composer and review-submit paths.

## 6. Smaller patterns worth lifting

- **PWA cache discipline** (`server/AGENTS.md`): only fingerprinted build assets get `Cache-Control: immutable, max-age=1y`; `sw.js`, the manifest, and the HTML shell must revalidate on every load, or an installed PWA pins a build the user cannot replace. Check our `sw.js` against this rule.
- **Status collector lifecycle**: stream loss is normal, not exceptional; backoff, re-subscribe, then reconcile from a snapshot taken after re-subscription. `pane.closed` must clear any per-pane state (waiting alerts, watches, title caches) immediately.
- **Tailscale detection** (`server/tailscale.ts`): they read `tailscale status --json` and `tailscale serve status --json` to render ready-made serve commands and connect links, and detect the owner's login on tailnet requests. Our locked model is "the tailnet is the boundary", so we need none of the pairing machinery; the `serve status` read is still a nice source for a "copy link" affordance.
- **Auth order** (only relevant if we ever add a token): token → device cookie → Tailscale identity → loopback, with the gate staying closed once any device is paired, even after all are revoked. Their static-file rule: static stays open, everything under `/api` and `/ws` gated.
- **VAPID/subscription file placement** matches ours (`~/.local/state/...`); no change needed, only the write pattern above.

## 6a. Their frontend stack, verified from source

React 18 SPA in TypeScript built with Vite 6; no router and no state library (state is hand-rolled hooks, `src/App.tsx` 1,614 lines). Styling is hand-rolled CSS with no Tailwind and no component library: one 1,958-line `styles.css` plus 46 per-component CSS files (`ChatView.css` 38 KB, `Composer.css` 37 KB). Icons lucide-react; rich content KaTeX + Mermaid + @tanstack/highlight; PWA is a hand-written `src/pwa.ts` plus a manifest.

Design facts worth copying regardless of stack:

- Viewport discipline for phone-first: `viewport-fit=cover` and `interactive-widget=resizes-content` in the meta tag keep the layout keyboard-aware on Android; `theme-color` matches the palette; `color-scheme: dark`.
- Warm dark palette anchored at `#181613` with per-component CSS files as the visual spec.
- Component anatomy worth using as blueprints when rebuilding our views in templ: session list (`AgentSidebar.css`), transcript (`ChatView.css`), composer with attachments and key bar (`Composer.css`), command palette (`CommandPalette.css`).
- Service worker cache rule already captured in section 6; their `index.html` comments document a real Chrome re-install bug caused by a stale cached manifest URL.

Decision recorded 2026-10-12: borrow the design into our templ + Datastar views (option B), not the React stack (option A). Option A stays rational only if this ever becomes a product face.

## 7. Explicitly not porting

For a single-user, pi-only, one-binary tool these are rejected, not deferred:

- node-pty interactive sidecar with ACK flow control (see section 2; `send_input` + `session control --takeover` cover the need).
- Multi-agent readers (claude/codex/omp/omo/gjc/grok/agy/devin stores), SSH remote machines, machine relay, remote bundle.
- Dictation, telemetry, self-updater with rollback, i18n, worktree creation, file viewer (templ already renders what we need), portal/public-address mode.
- Their 118K LOC React frontend; our templ + Datastar islands stay.

## 8. Open risks and follow-ups

- `observe` is a CLI surface, not a versioned socket method: herdr can change its stdout framing without bumping protocol 22. Mitigation: isolate the stream parser, keep the `pane.read` poll as a fallback path, and re-verify on every herdr upgrade (pin the probe script in `docs/m0-findings.md` style).
- The thin-moat risk from the reviews does not apply at single-user scope; drop it from planning.
- Follow-ups in priority order:
  1. ADR: terminal bridge via `observe` with poll fallback (section 2), superseding the m0 "no output push" conclusion.
  2. Correct `docs/m0-findings.md` with a dated addendum pointing here.
  3. Tree-aware parser port (section 3), then the abandoned-turns header.
  4. Push watcher debounce/cancel + focus suppression (section 4).
  5. Approval cards for pi menus (section 5), guard rules included.
  6. Audit `sw.js` caching against section 6.

## 9. Re-analysis 2026-10-15: the pi adapter (head 6e575a8, post-v0.4.8)

Fresh clone, full read of their pi path: `server/pi.ts`, `server/pi-tree.ts`, `server/pi-models.ts`, `server/prompt.ts` (pi dialog parse + answer), `server/conversation.ts`, `server/transcript-records.ts`, `server/conversation-monitor.ts`, `src/lib/promptAnswer.ts`, `src/lib/keyBar.ts`. Citations are `file(function)`. Focus per Esteban: techniques to mimic for pi, dialogs, and extension compatibility.

Corrections 2026-10-15: an adversarial review pass (Claude session 808275a5, verified against both their source and ours) fixed several overstatements below — the 9.4 per-key claim, the 9.1 parse claim, the fail-closed cap, and the context_clear scope. The corrected text is authoritative.

### 9.1 Session-file engineering (`pi-tree.ts`) — beyond our internal/pi

- The session tree is indexed as byte RANGES (`PiEntry {id, parent, start, end, type, role}`), and the active branch is projected to merged segments (`piBranchSegments`). The INDEX still JSON.parses every line including side branches; it is the conversation, tool-output, and image READS that touch only the branch's bytes. We read everything.
- Cache revalidation is `dev:ino` identity plus a 64-byte tail read before the scan offset (`piEntryIndex`, `readBefore`): appends reuse the index, a rewrite/truncation/same-path replacement rebuilds. We key on path+mtime+size only; dev:ino catches a file swapped in place.
- An LRU of 4 file indexes with budgets (100k entries, 16MB per record, 32MB metadata) that fail CLOSED by throwing, caught to null (`addEntry`, `MAX_PI_*`). We hold exactly one tailCache session. Trap, not feature: a session over 100k entries makes their chat UNAVAILABLE, not partial — wrong trade for a single-user tool; do not copy the caps.
- Incremental extension reads 4MB chunks and carries `open` line fragments across chunk boundaries for multi-MB records (base64 images), joining once (`extendIndex`). Appends never re-scan; `scanned` always sits on a newline.
- Leaf walk uses a `seen` set cycle guard and a byId map where later appends win — our `ActivePath` already overwrites byID last-wins and carries the cycle guard, so nothing to port here.

### 9.2 pi entry semantics we do not yet model (`transcript-records.ts`)

- `piMessage` normalizes pi-family content-block spellings once: `toolCall`/`toolName`/`toolCallId`/`toolInput` vs `name`/`id`/`input`, `toolResult` `output`/`content`/`result`. Providers behind pi spell fields differently; normalize at the edge.
- `display: false` entries (entry or message level) are not conversation. `custom_message` with `display: true` renders as a notice in the user's seat (`piNotice`), with `<system-notice>` envelopes stripped. Our census flattens customs; this is their rule.
- `context_clear` custom entries are the `/clear` boundary: conversation pages AND tool-output reads start after the last clear (`isContextClear`, `activeHistoryText`); a ref that predates the clear must not answer. Scope notes: `context_clear` looks like an omp/OmO/gjc entry and plain pi may never write it (unverified); and `piTranscriptImage` does NOT honor it — only the tool-output read does.
- Tool results carry inline base64 images. The wire carries only a position ref `pi:<callId>:<nth>`; bytes are fetched on demand from the branch (`piTranscriptImage`), never shipped with every poll. Refs validate against strict regexes before any read.
- Whole tool output on demand by ref: branch-only read, 2MB cap, and a substring prefilter (`line.includes(needle)`) so JSON.parse runs only on lines that can hold the answer (`piToolOutput`). context_clear resets the result during the scan.
- pi reports the session path before the file exists (first write lands with the first answer): `unwrittenSession` surfaces an "unwritten" placeholder session with the nearest existing in-store ancestor, so the UI shows the session as not-yet-started instead of missing (`pi.ts`). We discovered the birth-at-first-prompt fact live; they render it.

### 9.3 Dialog detection: the discipline our awaiting.go lacks (`prompt.ts`)

- The screen comes from `herdr paneRead {source: "detection", format: "text"}` (`liveScreen`) — herdr's own detection screen, not our `recent` lines=8. Confirmed present in OUR herdr build: the protocol-22 schema enum reads `["visible","recent","recent_unwrapped","detection"]`. One open question before relying on it: herdr's `screen_detection_skipped` flag suggests detection can be skipped per pane and return empty — a poller that reads empty as "no dialog" would fail quietly, so test that first.
- Four pi responders, each with its own hint: `pi-question` (`↑↓ navigate enter select escape/ctrl+c cancel`), `pi-confirm` (two rows Yes/No), `pi-input` (`enter submit escape/ctrl+c cancel` on a `›` line), `pi-model` (`Enter to select · Ctrl+S to set as default · Escape/Ctrl+C to cancel`). We match one generic hint-bar signature.
- Hints anchor to the END of the screen with a footer allowance (`hintAtEnd`; join span 4 for pi's select/confirm/input, default 3 for `pi-model`): pi keeps its folder+meter footer under every dialog, and a phone-width pane wraps the hint. Anchoring is what keeps an ANSWERED, buried dialog from offering keys. Our match-any-line regex has no such discipline.
- Wrapped rows join before parsing: `PI_WRAPPED_REST_RE` (a line one column in, no cursor) is the rest of the previous option; model-row tails (`[provider] · default` alone on a line) join back. Measured at 46 columns. Our detector never joins.
- Row rules: every block line must be a single-label row (two-column lines void the read — the slash palette would run a command on a click); the cursor must sit on the FIRST row for `pi-question`/`pi-confirm` and only before any keys are sent (`cursor !== 0` = moved by hand, no card; `pi-input` has no cursor rule); `/tree` is deliberately never answered (its `↑/↓ move` hint differs; answering would move branches with no undo). Yes/No reads as a confirmation and "No" is PRESSED, not escaped (`rejectWithEscapeIndex: null`) — Escape leaves pi's confirm unanswered.
- pi never reports itself blocked for a dialog, so cards come from the screen alone. Nuance: pi DOES emit OSC 7501 `blocked`, but herdr does not consume it — same net effect as our ADR-0007 finding.

### 9.4 Answering: stale-card protection, not per-key verification (`prompt.ts` `answerKeys` + the answer route)

- Corrected: the per-keystroke settle polls (`onRow`, 50ms) run only for the LIST responders — Claude model and effort, Codex model, `fallback-gjc-menu`. NOT pi. pi's moves go out as a burst with a 30ms sleep between steps (`prompt.ts:3636`); there is ONE re-read before the first non-move key, and only when the cursor was moved. `pi-question` with zero moves gets no extra re-read; `pi-input` is never re-verified. (An earlier version of this section sold the per-key loop as pi's protection — wrong.)
- What actually makes pi answering safe: prompts are content-hash id'd, a per-pane asking counter serializes answers, the answer route answers 409 `prompt_changed` when the on-screen id differs from the card's, and every irreversible key checks that somebody still waits. That stale-card discipline is the part worth copying.
- `pi-model` answering verifies the MODEL UNDER THE CURSOR by name before Enter (the catalogue scrolls under the cursor, so rows change on the way down; "a wrong model answers every later turn silently"). Even so, our verdict: do NOT copy pi-model answering — the picker can save a default.
- `pi-input`: no Enter before typing (pi's `›` line would submit empty), then `ctrl+k`, `ctrl+u` to clear pi's editor line (their measured 0.87.1 keys) before the text, then Enter. Our verdict: do not copy — it types into pi's editor blind.
- Client side, the composer doubles as an answer surface: a typed number, exact label, or bound letter ("Yes, proceed (y)") answers the pending card; anything else is the custom text when the dialog allows one (`promptAnswer.ts answerFromText`). Our quick keys could adopt the number/letter mapping when a dialog pill is live.

### 9.5 pi model catalog (`pi-models.ts`)

- The context-window ring reads ONLY `~/.pi/agent/models.json` (custom providers), honoring `modelOverrides` and never inheriting a provider-level window; unresolved means no ring is drawn — "guessing a window would draw a percentage the user cannot tell from pi's own". Verified against `pi --list-models` with crafted files. BOM and `//` comments stripped exactly as pi does; cache keyed on mtime+size.
- Deliberately MORE forgiving than pi on bad catalog entries (skip, not refuse): pi's worst case is switching to a nonexistent model; theirs is one missing number. `PI_CODING_AGENT_DIR` / `PI_CODING_AGENT_SESSION_DIR` are honored everywhere.

### 9.6 Smaller techniques worth lifting

- `ConversationMonitor`: one 250ms stat loop for all watched panes, resolution retried on 2s/5s backoff, identity salted with `dev:ino:size:mtimeMs:ctimeMs`; per-pane interest tracking, never a global parser.
- Their key bar supports named keys up to F12 including PageUp/PageDown because their input path is a PTY; herdr send-keys has no page keys, so this stays unportable for us (matches the quick-keys decision).
- `guardRe`: builds hint regexes where every space is optional, so a narrow pane's mid-word wrap cannot kill the match — but it is used only for the CLAUDE hints. The pi hints use `\s+` joins, so a mid-word wrap would not match for pi anyway. Not a portable upgrade.
- Answer-state hygiene: prompts are immutable objects with a parsed-side map (`parsedByPublicPrompt`); answers throw on prompts they were not produced from. Prevents answering a stale card.

### 9.7 Adoption verdicts (post-review 2026-10-15)

The five original candidates were reviewed adversarially and re-scoped:

1. Detector hardening — ADOPTED (narrow): switch to `source:"detection"` where cheap, end-anchored hints with the footer allowance, and pi's real select/confirm/input signatures. Skip wrap-joining and the cursor rule (they only matter for parsing rows to answer). Our `hintBarRe` wants a `·` separator, so pi's space-separated select/confirm/input hints never match today.
2. Answer cards — DEFERRED, confirm dialogs only, behind a stale-card id with 409 and a pre-fire re-read. Never pi-input (blind typing) or pi-model (can save a default).
3. pi-tree engineering — ADOPTED only the cheap defects: the double full-file read in our `parse` path and the tail-cache lock held across IO. Skip LRU(4)/byte budgets/branch projection — they solve multi-agent scale we do not have, and the fail-closed caps would be a trap.
4. Entry semantics — placeholder session ("unwritten") worth doing; `display:false` only after a census of real sessions; images/spellings/context_clear skipped for now.
5. Context ring — DEFERRED. `models.json` covers custom providers only; cosmetic.
