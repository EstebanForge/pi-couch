# Session List Screen

Purpose: Display all pi sessions on the server grouped by Workdir, showing live Status badges and providing entry into Session views or composer.
Feature ids covered: V1, V12, D1, D8.
Entry points: App launch, back navigation from any Session view, header brand tap.

```
+-----------------------------------+
| PiRemote                   [Q] [+]|
+-----------------------------------+
| [All Workdirs v]   Sort: Activity |
+-----------------------------------+
| v ~/Dev/EstebanForge/PiRemote     |
| +-------------------------------+ |
| | fix-scanner-mtime             | |
| | [working] [driver] [named]    | |
| | "Fix fsnotify tail race"  2m  | |
| +-------------------------------+ |
| | session-8fa12c                | |
| | [idle] [driver]               | |
| | "Added unit test for SSE" 1h  | |
| +-------------------------------+ |
|                                   |
| > ~/Dev/EstebanForge/Baluard      |
| +-------------------------------+ |
| | update-gradle                 | |
| | [done]                        | |
| | "Updated kotlin to 2.1"   1d  | |
| | [Resume]                      | |
| +-------------------------------+ |
|                                   |
|                                   |
+-----------------------------------+
| [+] New Session                   |
+-----------------------------------+
```

## Layout and Components

Top bar displays app title "PiRemote", search filter button, and top "+" shortcut button.
Filter bar provides a Workdir dropdown selector to isolate a single folder, plus activity sort indicator.
Session group header displays folder path, expand or collapse chevron, and count of contained sessions.
Session card displays the session name (explicit `--name` flag or truncated first prompt text), age timestamp, and last message snippet.
Status badge displays one of five states: working (blue), idle (green), blocked (amber), done (gray), unknown (red) (V12). Verified M0 delta: pi turn completion reports `done`, not `idle`; a gray `done` badge means the turn finished and pi waits for input. `idle` appears only before the first prompt.
Driver badge appears if an active herdr target currently drives the session.
Named badge appears if the session was explicitly assigned a name via the `--name` flag.
Resume button appears on inactive sessions that have no live Driver.
Bottom bar anchors the primary "New Session" button in the thumb zone for quick one-handed access.

## Interactions

Tap "New Session" button: Open home composer screen (D1).
Tap Session card: Open the selected Session chat view (V1).
Tap Resume button: Send request to start pi process in a new herdr target using the session ID, then open Session view (D8).
Tap Workdir group header: Expand or collapse that group's session cards list.
Tap search filter button: Open text input to filter sessions by name, path, or prompt text.
Long-press Session card: Open contextual bottom sheet with options: "Copy Session ID", "Copy Workdir Path", "Close Session" (hides card).
Pull down from top of list: Force filesystem scanner rescan and refresh session list.

## States

Loading: Skeleton list cards display pulsing placeholder bars for title, badges, and preview text.
Empty: Displays centered card: "No sessions found. Tap New Session to start your first session." with primary action button.
Error: Red banner displays scanner failure: "Failed to read session directory. Check path permissions." with retry button.
Live: New sessions and updated timestamps insert dynamically via Datastar SSE fragments without page reload. Status badges pulse during active operations.
Backgrounded: SSE connection suspends. UI freezes timestamps. Immediate refresh triggers on foreground wake.

## Data Needs

Scanner fields: JSONL file paths under `~/.pi/agent/sessions/`, header metadata (`id`, `cwd`/Workdir, `name`), file last modification timestamp (`mtime`), and trailing prompt snippet.
herdr actions: Query the agent snapshot via socket to obtain live target list, map each pane to its session file through the pane's `agent_session` path (populated live on first prompt), and resolve Driver presence and Status.
SSE events: Server streams `session_list_patch` containing updated cards, timestamp updates, and Status transitions.
Git service: Not used on this screen.
State JSON: Read user preferences file for closed (hidden) session IDs to omit them from the default view.

## Edge Cases

Backgrounding and SSE death: Client reconnects immediately upon returning to foreground. Reconnection pulls complete active session list snapshot to catch any sessions created during absence.
herdr attach contention: Not applicable on list screen; starting or resuming a session allocates a new herdr target.
Unknown jsonl entries: Scanner skips unparseable lines during list indexing; damaged files display with warning icon and "Damaged session file" subtitle.
Missing Workdir or Repo: If Workdir was deleted from disk, the card displays a warning tag `[missing workdir]` and disables the Resume button.
Stale Status: If herdr daemon terminates, all active Driver badges disappear and Status badges switch to `[unknown]`.
Rapid file writes: Active sessions write jsonl rapidly. The server throttles SSE list card updates to at most once per second per card to prevent UI churn.
Closed sessions: Users can toggle "Show closed sessions" in filter bar to unhide previously closed entries.
