# Pi Durable evaluation

Recorded 2026-10-16 from a clone of earendil-works/pi at packages/durable v1.3.0 (commit of that day, sparse checkout). Purpose: a standing analysis so a future re-evaluation starts here instead of from zero. Verdict at the bottom. Two patterns were stolen into this codebase the same day; they are listed under "What we took" with their code locations. This document records an evaluation, not a decision; an adoption would need a new ADR (it would reopen ADR-0001 and ADR-0006).

## What Pi Durable is

@earendil-works/pi-durable is a runtime for building agent applications: conversations, model turns, tool calls, tasks and application documents are committed to storage before anything is shown to the user. If the process dies mid-turn, reopening the storage resumes the work from the last committed state. It ships a harness, a task graph with structured concurrency, an inbox for submissions, documents (per-conversation app state with history and forks), compaction, hooks, tools, code mode, and UI feeds. Around 25k lines of TypeScript across 82 source files, plus @earendil-works/chord for document state and @earendil-works/pi-ai for model access. The README's first line says: experimental, the API changes without notice between releases. The design spec is docs/spec.md in the package, 5361 lines.

Storage backends: memory, JSONL (a write-ahead log of commit markers plus sidecar files, FORMAT_VERSION 1), SQLite. The JSONL format is its own invention. It cannot read pi's session files, and pi cannot read its log.

The examples under test/examples are numbered and each demonstrates one capability. Every claim below names its example.

## How agents run here today

Three processes. pi runs the agent: it owns the model loop, tools, context, compaction, and appends each completed entry to a JSONL session file under ~/.pi/agent/sessions/. herdr drives pi sessions: start, prompt, keys, pane reads, agent status over its socket (protocol 22, pinned). Couch Pi observes: it scans and tails the session files, streams them to the phone as SSE, detects blocked dialogs (bridge extension plus a pane regex), answers them by re-reading the pane and sending keys, and pushes on status changes (ADR-0005). Steering rides on pi's queue mode; the wire has one verb, agent.prompt (ADR-0001, PLAN.md D4).

## What a move would replace

| Today | Pi Durable | Example |
|---|---|---|
| JSONL tail, tolerant parser, fsnotify, mtime index | Storage is the source of truth; conversations, entries, tasks, documents commit before display | test/examples/00-conversation.ts |
| SSE feeds with seq, terminal byte stream | viewState() plus subscribe, or watchEvents: snapshot then deltas; late joiners start from current state, no replay | test/examples/05-watches.ts, 21-late-join.ts |
| Steering via agent.prompt; queue mode decides (documented limit) | Inbox: submit with whenBusy steer, followUp or reject; writes queue too; withdraw until placed | test/examples/20-inbox.ts |
| Approvals: bridge extension, pane regex, verify-then-fire keys | beforeTool hook returning block; per-conversation tool override and wrapping; read-only plan mode | spec.md section 7.2 (Permissions), test/examples/30-tool-override.ts, 27-plan-mode.ts |
| Retries invisible; pane text is the only signal | Durable retry: backoff survives reopen; provider resend after crash without repeating preparation | test/harness-generation-recovery.test.ts lines 74, 164 |
| Compaction: pi internal; we render what lands in the file | Threshold-gated background compaction, manual compact() whose summary queues in the inbox, one compact-and-retry on prompt-too-long | test/examples/25-compaction.ts |
| App state: in-memory index, store files, stream reconciles | Extensions own documents that are the state: versioned, rewindable, fork-carried | test/examples/11-extension-state.ts, 04-chord-state.ts |

The real gain is the middle column's honesty: dispositions, placements and approvals become committed state instead of inference from panes and file tails.

## Crash behavior

Today. A couch-pi crash is a non-event: agents live in pi under herdr, and on restart we rescan the session files and re-arm the push watchers. The dangerous crash is the pi agent mid-turn. The session file holds completed entries only, so the streaming partial is lost, and a tool that was executing leaves effects with no record. Recovery is a human typing continue, and whether the half-run tool runs twice depends on the tool, not on any system.

Pi Durable. test/examples/13-recovery.ts is the exact story: a task closed after printing tick 2 but before saving its checkpoint. On reopen, nothing to restart by hand; tick 2 runs again because its outcome was never saved, but its memo says it was already printed, so it does not print twice. spec.md section 5.2 names the discipline the effect sandwich: commit intent, perform the external effect, commit the outcome. Reopening in an intent phase means the effect may have happened; the phase handler retries safely, polls an external handle, or records the interruption. Generation-level equivalents live in test/harness-generation-recovery.test.ts: preparation interrupted before its commit reruns; a request interrupted before any partial resends without repeating the preparation; a retry backoff resumes after reopen.

So durable does not make tools idempotent. It makes re-execution legible: the rerun is guaranteed, the dedup is the author's one-line memo, and the state that survives is exactly what was committed.

## Which of our tools are safe to run twice

Inventory of the tool surface this project's agents actually use (the dogfooded agent included):

Safe: edit (exact-text replace, second pass no-ops or errors), write (same bytes over the same path), every read (read, grep, find, codegraph, browser GET), git push after a first success, systemctl --user restart, healthz curls, go build, npm ci.

Not safe: exec_command with side effects (package installs, deploy scripts, POST /api/agents would duplicate an agent), Slack and Asana posts and section moves (duplicates), file moves and deletes, browser form submits, memory_save (harmless but noisy). Under durable every one of these needs the 13-recovery memo treatment or a natural idempotency key before it may sit inside a resumable task. That conversion, not the port, is the hidden migration cost.

## What the move would take

1. A resident node service owning Harness.open(sqlite) per machine; models, registry and settings per test/examples/06-harness.ts and 07-configuration.ts; a systemd user unit; sqlite in the state directory with backups.
2. Port the agent surface: CodingTools plus prompt sections per test/examples/26-coding-agent.ts; our extensions become registry extensions.
3. Replace the dialog bridge with a beforeTool approval hook (spec.md 7.2) and per-conversation tool overrides (30-tool-override.ts). Phone approval cards subscribe to a document instead of scraping titles.
4. Phone commands become submit, steer, withdraw and answer (20-inbox.ts). The UI consumes watch op frames (05, 21); either the Go server re-emits them as SSE or clients learn the op format.
5. The push watcher moves from herdr agent.wait polling to harness busy and idle events (rewrites ADR-0005).
6. History is a cold cutover: durable's JSONL and SQLite formats cannot read pi session files. Archive, do not migrate.
7. herdr retires. The terminal tab has no successor: durable has no pane, no PTY, no send-keys. Either we drop a shipped feature or keep herdr alive next to durable, which defeats the move.

## What is risky

The terminal tab dies; that is a real feature loss on this project. We stop tracking pi: today every pi improvement lands free, and a durable-based stack forks from that river. The API is experimental by its own README across 25k lines plus chord. The harness process becomes a new availability domain, and the memo discipline is a tax on every future tool. The move reopens ADR-0001 (herdr is the only driver, plain pi sessions only) and ADR-0006 (one Go binary).

## What we took (2026-10-16)

The memo pattern: internal/server/dialog_memos.go, claimed in apiDialogAnswer before any key goes out, released when the send provably failed. A double-tap on a dialog card is now refused (409) instead of firing a second Enter into whatever pi shows next. Tests: dialog_memos_test.go and the double-tap and send-failure cases in api_dialog_memo_test.go.

The inbox disposition as the composer contract: POST /api/sessions/{key}/prompt carries whenBusy, and only followUp exists (apiPrompt in internal/server/api_stream.go refuses the rest with 400 instead of guessing pi's queue mode). The composer sends the disposition by session status (web/src/chat/disposition.ts), so a send while working is labeled queued, matching what actually happens, and no steer button is faked (PLAN.md D4 stays true).

## Verdict

Do not move now. The gain is real and sits exactly in our weak spots: legible steering, structured approvals instead of regex on a pane, crash-resumable turns with honest tool rerun semantics. The price is the terminal surface, pi-parity, the just-stabilized integration layer, and an unstable API. Today's couch-pi crash story is fine; only the agent-mid-turn crash is ugly, and it is tolerable for a phone-driven workflow.

Re-evaluate when any of these flips: pi-durable ships a stable release tag; pi itself adopts the durable storage format (which would make session history portable); the terminal need disappears from the product; or steering-via-queue-mode starts biting users in practice.
