# Subagent Drill-in Screen

Purpose: Inspect the dedicated execution transcript of a subagent session spawned by a parent Session.
Feature ids covered: V6, V2, V3, V7.
Entry points: Tapping a subagent tool-call card within the parent Session transcript.

```
+-----------------------------------+
| [<] Parent: fix-scanner   [done]  |
|     Subagent: run-tests-m0        |
+-----------------------------------+
| Breadcrumb: fix-scanner > tests   |
+-----------------------------------+
|                                   |
|                +----------------+ |
|                | Run scanner    | |
|                | unit tests     | |
|                +----------------+ |
|                                   |
| [x] tool: go test ./...     [Copy]|
|   > PASS: TestScannerTail (0.04s) |
|   > ok github.com/pi/remote       |
|                                   |
| All scanner tests passed cleanly. |
|                             [Copy]|
|                                   |
+-----------------------------------+
| Read-only subagent transcript     |
+-----------------------------------+
| [Return to Parent Session]        |
+-----------------------------------+
```

## Layout and Components

Top bar displays back button (`[<]`), parent session reference, subagent name or identifier, and subagent Status badge.
Breadcrumb trail sits beneath the header: shows hierarchical parent-child relationship (e.g. `parent > subagent`).
Transcript area renders the subagent's dedicated jsonl message entries: prompt, assistant replies, tool executions, and errors.
Usage bar displays token count, run duration, and compute cost specific to the subagent session.
Bottom banner displays informational text: "Read-only subagent transcript" since subagents run autonomously under parent guidance.
Bottom action button provides a wide "Return to Parent Session" target placed in the thumb zone for fast return.

## Interactions

Tap back button or "Return to Parent Session": Pop subagent view and return to parent Session chat with original scroll position preserved.
Tap breadcrumb parent link: Jump directly to parent Session view.
Tap Tool-call row: Expand or collapse subagent tool output.
Tap "Copy" button: Copy subagent output or code blocks directly to clipboard.
Long-press tool card: Copy command and stdout together to system clipboard.
Scroll transcript: Review full execution history of the subagent.
Swipe right from left edge: Perform Android system back gesture to navigate back to parent Session.
Pull down from top: Force immediate re-read of subagent jsonl file from server disk.

## States

Loading: Displays skeleton message placeholders while subagent jsonl file loads from disk.
Empty: Displays centered card: "Subagent initialized. Awaiting first execution output."
Error: If subagent session file cannot be located on disk, displays red error card: "Subagent session file missing."
Live: While parent subagent task runs, new entries stream in real time over SSE. Status badge shows `[working]`.
Backgrounded: SSE stream halts. Reconnection verifies if subagent concluded while backgrounded and fetches final entries.

## Data Needs

Scanner fields: Locate subagent session file referenced by path or ID in parent session entry; parse subagent jsonl tree (`id`, `parentId`, `cwd`, roles, content blocks).
herdr actions: Not directly driven via herdr; subagent runs within parent process or as autonomous child process.
SSE events: Client subscribes to SSE stream targeting the subagent's specific session ID (`/events/session/<subagent-id>`).
Git service: Not used directly by this screen.
State JSON: Inherits parent session UI settings.

## Edge Cases

Backgrounding and SSE death: Client reconnects with last received event ID from subagent session stream upon app resume.
herdr attach contention: Subagents do not have separate interactive herdr panes; peek and takeover rules do not apply.
Unknown jsonl entries: Custom entry types within subagent file render as collapsible raw JSON rows without failing.
Missing Workdir or Repo: Subagent inherits parent Workdir; if path is removed, transcript still displays cached lines.
Stale Status: Subagent Status reflects entry sequence; if parent aborts subagent, status updates to `[done]` or `[unknown]`.
Deeply nested subagents: If a subagent spawns another subagent, breadcrumbs truncate with middle ellipsis (`parent > ... > subagent-3`) to maintain 360 px width.
Scroll restoration: Returning to parent Session restores exact scroll offset where the user clicked the subagent link.
Parent completes before child: If parent terminates prematurely, subagent transcript marks last entry with a cancellation notice.
Detached subagent: If parent session completes and closes, subagent session remains preserved on disk and inspectable.
