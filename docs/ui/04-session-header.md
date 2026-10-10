# Session Header and Menu

Purpose: Display persistent session identification and state indicators, providing fast tab switching and lifecycle management controls.
Feature ids covered: V8, D7, D8.
Entry points: Present at the top of every tab within an active Session view.

```
+-----------------------------------+
| [<] fix-scanner-mtime [working][:]|
|     ~/Dev/EstebanForge/PiRemote   |
+-----------------------------------+
|[Chat*][Changes][Term][Files][Cmts]|
+-----------------------------------+
|                                   |
|       (Session tab content)       |
|                                   |
+-----------------------------------+
| Context Menu Sheet:               |
| +-------------------------------+ |
| | Session: fix-scanner-mtime    | |
| | ID: sess_981a4b2c             | |
| |-------------------------------| |
| | [>_] Open Terminal Tab        | |
| | [>]  Resume Session           | |
| | [!]  Stop Session (Confirmed) | |
| | [x]  Close Session (Hide)     | |
| +-------------------------------+ |
+-----------------------------------+
```

## Layout and Components

Back button (`[<]`) sits at the top left corner, sized 48x48 px for easy reach.
Session title displays the session `--name` value, or the truncated opening prompt if unnamed.
Workdir path displays in small text directly below the title, truncated with an ellipsis if long.
Status badge renders adjacent to the title: working (blue), idle (green), blocked (amber), done (gray), unknown (red).
Quick terminal toggle icon (`[>_]`) provides one-tap transition between Chat and Terminal tabs.
Overflow menu icon (`[:]`) sits at the top right corner.
Context menu sheet slides up from the bottom edge when the menu icon is pressed.
Menu sheet items include: Session details (name and ID), Open Terminal Tab, Resume Session (if stopped), Stop Session (danger red), and Close Session (neutral gray).

## Interactions

Tap back button: Pop current view. Returns to Session list from Chat tab, or returns to Chat tab from secondary tabs.
Tap Session title or Workdir path: Open informational dialog showing full absolute Workdir path, session file path, and exact creation timestamp with copy buttons.
Long-press Session title: Copy session ID directly to clipboard and show toast notification.
Tap quick terminal toggle: Switch active tab directly to Terminal tab without opening the menu.
Tap overflow menu button: Slide open the context menu bottom sheet.
Tap "Open Terminal Tab": Navigate to Terminal tab and dismiss menu.
Tap "Resume Session": Send command to herdr to spawn a new target running `pi --session <id>` (D8).
Tap "Stop Session": Open modal confirmation dialog: "Stop Session? This closes the active herdr target and halts execution." Upon confirmation, sends close target request to herdr (D7).
Tap "Close Session": Mark session as hidden in client preferences and return to Session list screen. Session file remains intact on disk.
Tap outside menu sheet: Dismiss context menu sheet without taking action.
Swipe down on menu sheet header: Dismiss context menu sheet smoothly.

## States

Loading: Title displays skeleton bar; Status badge shows neutral gray.
Empty: Not applicable; header only renders inside an initialized Session.
Error: If herdr socket disconnects, header displays warning triangle next to Status badge.
Live: Status badge updates in real time via SSE events; pulsing glow indicates active working state.
Backgrounded: Status badge maintains last known state until app wakes from sleep and SSE confirms current state.

## Data Needs

Scanner fields: Read session header line containing `id`, `cwd` (Workdir), and optional `name`.
herdr actions: Read live agent snapshot to determine Driver attachment; execute target close for Stop Session (D7); execute agent start `pi --session <id>` for Resume Session (D8).
SSE events: Receive `status_update` events carrying state transitions (working, idle, blocked, done, unknown).
Git service: Not used directly by the header.
State JSON: Update hidden session list in user state JSON upon "Close Session" action.

## Edge Cases

Backgrounding and SSE death: When device returns from background, header immediately polls or requests fresh snapshot to refresh stale Status badges.
herdr attach contention: Terminal quick toggle respects peek mode rules defined in Terminal documentation.
Unknown jsonl entries: Header operates from file header line and herdr socket data; ignores inner entry parse errors.
Missing Workdir or Repo: If Workdir is removed from disk while Session is open, header path turns red and displays "Workdir missing" tag.
Stale Status: If herdr service terminates unexpectedly, Status badge switches to `[unknown]` and Stop action is disabled.
Double Stop tap: Stop action disables its button immediately upon first tap to prevent sending duplicate close calls to herdr.
Accidental Close: Close action only hides the Session from the UI list; it can be restored anytime from the Session list filter bar.
Session name change: If pi process updates session name flag during execution, scanner detects change and header updates via SSE.
