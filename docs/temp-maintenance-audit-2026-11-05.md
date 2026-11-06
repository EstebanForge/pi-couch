# Temporary maintenance audit tracker

Date: 2026-11-05

Scope: read-only review of correctness, technical debt, KISS, DRY, YAGNI, maintenance, security, and performance. This is a temporary tracking document. Remove it after every actionable item is completed, rejected, or moved to a permanent tracker.

Overall verdict: the architecture is sound. No critical production security issue surfaced, and no rewrite is justified. The best improvements are bounded fixes to correctness, test reliability, privacy, and multi-session performance.

## Verification baseline

- [x] `just ci` passed: Go vet, gofmt, and tests; TypeScript and Biome; 160 Vitest tests; 7 real mobile Chromium scenarios.
- [x] Production `npm audit --omit=dev` reported 0 vulnerabilities.
- [x] `go test -race ./internal/pi ./internal/server ./internal/update` was run. It exposed races in test fixtures, described below.
- [x] `go fix -diff ./...` was inspected. Its changes are optional Go 1.27 modernizations, not evidence of defects.
- [x] No audit code changes were made during the review.

## Priority 1: correctness, privacy, and test reliability

### 1. New-agent attachments can exceed the limit and flood uploads

- [x] **Status: completed 2026-11-06**
- Verification: mobile Chromium regressions failed with 11 uploads and then 5 simultaneous uploads across incremental batches; both now pass with 10 total files and at most 3 active uploads across batches. The full 9-scenario mobile suite passes.
- Location: `web/src/components/AgentLauncherFields.tsx:62-77`
- Related implementation: `web/src/components/Composer.tsx:291-338`
- Finding: `onFiles` reads `files.length` from the render before the batch. That value does not change inside the loop, so selecting more than 10 files can add and upload all of them. Every accepted file also starts uploading immediately, which can create a large memory and network burst.
- Fix: share one attachment-selection and upload helper between the launcher and composer. Enforce the remaining capacity before starting work and retain bounded concurrency.
- Acceptance: selecting more than 10 files retains at most 10; no more than 3 uploads run concurrently; existing prompt attachment behavior remains intact.
- Effort: small, about half a day.

### 2. The race detector fails on test fixtures

- [x] **Status: completed 2026-11-06**
- Verification: the original race run failed on fake-status writes and concurrent ResponseRecorder access. Atomic fake status and a real HTTP terminal stream fixed both; the targeted race suite passes and now runs in `scripts/ci.sh`.
- Locations: `internal/server/api_dialog_memo_test.go:108`, `internal/server/api_sessions_driven_test.go:37`, `internal/server/hc_terminal_test.go:274-276`
- Finding: the fake herdr server reads `o.status` while a test writes it. `TestHCTerminalStreamsDeltas` also reads `httptest.ResponseRecorder.Body` while the stream writes to it. These are test-harness races, not demonstrated production races, but they prevent race detection from serving as a reliable gate.
- Fix: protect fake state with a mutex or atomic value. Use a real `httptest.Server` or synchronize access to the terminal response body. Add a targeted race gate to `scripts/ci.sh` after the suite is clean.
- Acceptance: `go test -race ./internal/pi ./internal/server ./internal/update` passes repeatedly and the targeted race gate runs in CI.
- Effort: small to medium.

### 3. Android logs dictated prompt content

- [x] **Status: completed 2026-11-06**
- Verification: the speech result log now records only state and text length; `./gradlew assembleDebug` passes.
- Location: `android/app/src/main/java/com/estebanforge/couchpi/MainActivity.kt:431`
- Finding: `Log.i(TAG, "results: wanting=$wanting text=${text?.take(40)}")` writes the first 40 characters of each dictated response to Logcat. Dictated prompts can contain private code, credentials, or client information.
- Fix: log recognition state and result length, never the dictated text.
- Acceptance: final and partial speech logs contain no recognized prompt content; non-content diagnostics remain available.
- Effort: minutes.

## Priority 2: performance and reliability

### 4. One transcript cache causes repeated full parses across sessions

- [x] **Status: completed 2026-11-06**
- Verification: the alternating-session regression failed on the single slot. Four per-session LRU tails now retain the four latest parsed entries and evict the oldest on the fifth; rewrite, replacement, concurrent-reader, and race tests pass.
- Location: `internal/pi/index.go:63-70,662-770`
- Finding: `Index` holds one `tailCache`. Simultaneous transcript streams for different sessions replace each other's cache entry, so subsequent calls repeatedly perform a full `io.ReadAll` and parse instead of using the incremental path.
- Fix: replace the single slot with a small bounded cache keyed by session, approximately 2 to 4 entries. Keep the existing delta validation and concurrency rules.
- Acceptance: alternating reads from two active sessions retain incremental parsing for both; cache size remains bounded; existing identity and same-size replacement checks still pass.
- Effort: medium.

### 5. The initial client download is larger than necessary

- [x] **Status: completed 2026-11-06**
- Verification: the browser regression required a deferred script on session navigation and failed before route splitting. The initial chunk is now 349.28 kB (107.14 kB gzip), down from 660.96 kB (196.52 kB gzip); Session and Terminal load separately.
- Location: `web/src/App.tsx:5-12`
- Finding: only Terminal is lazy-loaded. Session, Changes, Files, Settings, and Quotas load at startup even when Home is the only visible screen. The audited build produced a 660.63 kB initial chunk, 196.34 kB gzip, plus a separate 294.36 kB Terminal chunk, 70.55 kB gzip.
- Fix: keep Home eager and lazy-load Session and secondary screens. Start with route-level splitting before considering manual chunk configuration.
- Acceptance: Home does not load session-only or secondary-screen modules; all routes and mobile browser tests still pass; before-and-after bundle sizes are recorded.
- Effort: small.

### 6. Retry backoff ignores cancellation and sleeps after the last failure

- [x] **Status: completed 2026-11-06**
- Verification: the cancellation regression failed after a 1.2-second wait and second attempt on the old code; it now returns immediately with one attempt. A separate regression proves there is no backoff after the eighth failure, and the full Go suite passes.
- Location: `internal/server/agents_create.go:50-63`
- Finding: `herdrRetry` uses `time.Sleep` directly. Cancellation during that sleep has no effect, and the loop sleeps once more after its final failed attempt. All current callers already have a context.
- Fix: pass the context into the helper, wait with a timer and `select`, and return immediately after the last failure.
- Acceptance: cancellation ends the wait promptly; no delay occurs after the final attempt; existing transient dial retry behavior remains intact.
- Effort: small.

## Priority 3: maintainability and DRY

### 7. The bridge still permits the subscription mistake that previously broke it

- [ ] **Status: pending**
- Locations: `extensions/couch-pi-bridge.ts:55-61`, `web/tsconfig.json:20`, `scripts/ci.sh`
- Finding: the local `ExtensionAPI` retains a generic `pi.on(event: string, ...)` overload, so TypeScript would accept another incorrect `pi.on("rpiv:...")` subscription even though cross-extension events require `pi.events.on`. The extension also sits outside the normal TypeScript project's `include` set, so regular `tsc` does not check it.
- Fix: remove the generic lifecycle `on` overload. Add a dedicated extension TypeScript configuration and run it in CI. Keep the existing runtime event-bus regression tests.
- Acceptance: the incorrect lifecycle subscription form fails type checking; current valid event-bus listeners pass; the extension typecheck is a CI gate.
- Effort: small.

### 8. Bridge HTTP delivery is duplicated

- [ ] **Status: pending**
- Location: `extensions/couch-pi-bridge.ts:111-139,174-203`
- Finding: `postBridge` and `dial` repeat request construction, headers, timeout, rejection logging, and failure handling. Authorization header construction also has a second copy inside `dial`.
- Fix: make `dial` a small payload builder over `postBridge`. Keep one request and warning path.
- Acceptance: no behavioral change; one shared request implementation remains; existing bridge tests pass.
- Effort: small.

### 9. Stream lifecycle tests cover only backoff arithmetic

- [ ] **Status: pending**
- Locations: `web/src/stream.ts:22-115`, `web/src/stream.test.ts:1-11`
- Finding: production code handles reconnection, silence detection, visibility changes, pageshow, and cleanup, but the unit test checks only `nextBackoffMs`.
- Fix: use a fake `EventSource` to test reconnects, background shutdown, foreground recovery, silence timeout, and cleanup. Keep real mobile browser E2E for interaction behavior.
- Acceptance: each lifecycle path has a deterministic test, including no connection or timer left after close.
- Effort: medium.

### 10. Large files need extraction by responsibility, not arbitrary splitting

- [ ] **Status: pending; perform incrementally when touching the relevant behavior**
- Locations: `web/src/screens/Session.tsx` at 1,498 lines; `web/src/components/Composer.tsx` at 1,103 lines; `internal/server/api_dialog.go` at 1,084 lines; `internal/pi/index.go` at 1,056 lines; `internal/server/agents_create.go` at 820 lines.
- Finding: these files combine several responsibilities and increase review and change cost. File size alone is not a reason for a wholesale rearrangement.
- Fix: move Session's tool and transcript presentation into `web/src/chat/`; move agent readiness and retry helpers out of HTTP creation handlers; share attachment and model controls; split dialog answer operations by verb when next changing them.
- Acceptance: each extraction is independently reviewable, preserves behavior, and passes existing tests. No whole-project file shuffle.
- Effort: incremental.

## Low-priority security hardening

### 11. Image responses trust the session-provided MIME type

- [ ] **Status: pending hardening decision**
- Location: `internal/server/api_item.go:41-44`
- Finding: the endpoint writes the stored MIME type directly. An unexpected `text/html` or active image format would be served under the application origin. The normal `<img>` path limits practical exposure, so this is hardening rather than a demonstrated exploit.
- Fix: allow only supported image MIME types and add `X-Content-Type-Options: nosniff`.
- Acceptance: supported transcript images still render; unexpected active MIME types are not served as executable same-origin content.
- Effort: small.

## Routine maintenance

- [ ] Apply patch and minor dependency updates in a separate maintenance batch. Candidates observed during the audit include TOML, JWT, `x/crypto`, React, Vite, and Vitest.
- [ ] Keep major updates such as xterm 6 and TypeScript 7 out of the routine batch and evaluate them independently.
- [ ] Optionally apply `go fix` Go 1.27 modernizations as a separate mechanical commit after behavioral work.
- [ ] Recheck production dependency vulnerabilities during maintenance. The audit baseline reported no known npm production vulnerabilities, so no urgent dependency response is required.

## Recommended implementation order

1. Fix attachment limits, Android logging, and retry cancellation.
2. Make the race suite clean and add it as a gate.
3. Add bounded multi-session parser caching and route-level client splitting.
4. Tighten the bridge's types and consolidate its HTTP helper.
5. Add stream lifecycle tests and schedule a separate dependency maintenance pass.

## Explicit non-goals

- Do not replace the loopback plus tailnet security model with passkeys or another authorization system as part of maintenance.
- Do not change the two-driver architecture, embedded SPA, read-only JSONL model, or lack of a database.
- Do not rewrite the incremental parser. Only its single-session cache needs adjustment.
- Do not replace React, Konsta, or the current test stack.
- Do not split files merely to reduce line counts.
- Do not add abstraction layers around one-off retry, upload, or dialog behavior. Small shared helpers are enough.
