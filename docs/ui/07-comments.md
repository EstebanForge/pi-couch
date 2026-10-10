# Line Comments Screen

Purpose: Collect, manage, and batch-submit line-specific feedback comments into the live Session driver.
Feature ids covered: R5, R6, R7, R8.
Entry points: Tapping "Comments" tab in Session view; tapping "Add Comment" on a diff line in Changes tab; tapping pending tray.

```
+-----------------------------------+
| [<] fix-scanner-mtime [working][:]|
+-----------------------------------+
|[Chat][Changes][Term][Files][Cmts*]|
+-----------------------------------+
| Pending Tray: (2 comments)        |
| [Discard All]     [Send All (2) >]|
+-----------------------------------+
| PENDING COMMENTS                  |
| +-------------------------------+ |
| | tail.go:42 (Repo: PiRemote)   | |
| | "t.mu.Lock() before Add()"    | |
| | [Edit] [Delete] [Jump to Line]| |
| +-------------------------------+ |
| | main.go:18 (Repo: PiRemote)   | |
| | "Check init error return"     | |
| | [Edit] [Delete] [Jump to Line]| |
| +-------------------------------+ |
|                                   |
| SENT COMMENTS (3)                 |
| +-------------------------------+ |
| | tail.go:12 [Sent 1h ago]      | |
| | "Buffer size is too small"    | |
| +-------------------------------+ |
+-----------------------------------+
| Draft Comment Modal (On tap line):|
| File: tail.go Line: 42            |
| [ Add feedback comment...       ] |
| [Cancel]                   [Save] |
+-----------------------------------+
```

## Layout and Components

Top header maintains persistent session navigation and status indicators.
Pending tray banner sits at top: displays pending comment count badge, "Discard All" button (neutral red), and "Send All" button (primary accent) (R6).
Pending comments list section: displays cards for comments currently stored in device local memory waiting for submission.
Each pending comment card displays Repo name, target relative file path, line number, preview snippet of commented code, comment body text, and actions ("Edit", "Delete", "Jump to Line") (R5, R8).
Sent comments list section displays previously delivered feedback comments with delivery timestamps, loaded from server state JSON.
Draft Comment bottom sheet pops up when a user selects a diff line: includes code line reference, textarea, "Cancel" button, and "Save" button.

## Interactions

Tap "Add Comment on Line X" from Changes tab: Open Draft Comment bottom sheet with prefilled file and line number.
Tap "Save" in Draft sheet: Save comment to pending tray in device storage and close sheet.
Tap "Cancel" in Draft sheet: Discard text in progress and close sheet.
Tap "Edit" on pending card: Re-open Draft sheet to update existing comment text.
Tap "Delete" on pending card: Remove individual comment from pending tray immediately.
Tap "Jump to Line": Switch to Changes tab and scroll directly to the exact file and line number where the comment belongs (R8).
Tap "Discard All": Open confirmation dialog: "Discard all pending comments? This cannot be undone." If confirmed, clears local pending storage.
Tap "Send All": Open confirmation dialog: "Send 2 comments to driver? This submits formatted review instructions to the agent." (R6, R7).
Confirm "Send All": Format all pending comments into a structured prompt, submit prompt to Session Driver via herdr, move comments from local pending to server sent state, and redirect user to Chat tab.

## Format of Sent Prompt

When "Send All" executes, PiRemote formats comments into a clear review prompt submitted to herdr:
```
Review comments on changes:
- internal/scanner/tail.go:42: t.mu.Lock() before Add()
- cmd/server/main.go:18: Check init error return
Please address these items.
```

## States

Loading: Skeleton comment cards render while fetching sent comments from server state JSON.
Empty: If zero comments exist, displays centered card: "No comments yet. Tap any line in the Changes tab to leave a comment."
Error: If herdr prompt submission fails during Send All, displays red banner: "Failed to submit comments to driver. Comments preserved in tray."
Live: As new sent comments register via SSE, they append to the Sent Comments list dynamically.
Backgrounded: Pending comments remain safe in local browser storage; backgrounding never drops pending feedback drafts.

## Data Needs

Scanner fields: Workdir path to locate Repo files.
herdr actions: Execute `agent prompt` to submit batch comment prompt to the active agent Driver (R7).
SSE events: Server streams `comments_updated` when comments are saved or modified in server state.
Git service: Verify file path and line context for jump targets.
State JSON: Server persists sent comments in session state JSON file (`state.json`), including timestamp, file, line, and comment body.

## Edge Cases

Backgrounding and SSE death: Pending comments persist in device local storage, ensuring zero data loss if the phone sleeps or reboots.
herdr attach contention: Submitting comments uses the herdr socket prompt endpoint; it does not require terminal attach.
Unknown jsonl entries: Not applicable to comments subsystem.
Missing Workdir or Repo: If target Repo is deleted before Send All, prompt reports error and keeps drafts in tray.
Stale Status: If agent Driver is stopped or unknown, Send All prompts user: "Driver not running. Resume session before sending comments?"
File modified after comment draft: If line numbers shift due to agent edits before Send All is tapped, the comment text still submits with original line metadata and code snippet context.
Multiple devices: Pending tray is local to the device; sent comments synchronize across devices via server state JSON.
