# Changes Tab Screen

Purpose: Review working-tree git diffs for Repos discovered within the session Workdir, jump across modifications, and open or download raw files.
Feature ids covered: R1, R2, R3, R4, R9, R5.
Entry points: Tapping the "Changes" tab within any Session view.

```
+-----------------------------------+
| [<] fix-scanner-mtime [working][:]|
+-----------------------------------+
|[Chat][Changes*][Term][Files][Cmts]|
+-----------------------------------+
| Repo: [ Couch Pi (main)        v] |
| Files modified: 2   (+14 -3)      |
+-----------------------------------+
| [Prev Chg] [Next Chg] [Wrap: ON ] |
+-----------------------------------+
| v internal/scanner/tail.go        |
|   [Raw] [Download]                |
| --------------------------------- |
| 41    func (t *Tail) Start() {    |
| 42 -    watcher.Add(path)         |
| 42 +    t.mu.Lock()               |
| 43 +    watcher.Add(t.path)       |
| 44 +    t.mu.Unlock()             |
|                                   |
| v cmd/server/main.go              |
|   [Raw] [Download]                |
| 18 +  scanner.Init()              |
+-----------------------------------+
| [Add Comment on Line 43]          |
+-----------------------------------+
```

## Layout and Components

Top header maintains persistent session identity, Status badge, and tab navigation.
Repo picker dropdown sits at top: if Workdir contains multiple Repos, user switches target Repo; selection persists per Session (R3).
Changes summary bar displays count of modified files along with total lines added and lines removed.
Diff navigation toolbar provides "Prev Chg" and "Next Chg" buttons to jump between hunks, plus a "Wrap" toggle switch (R4).
File diff cards display filename path, file status (modified, added, deleted), and header buttons for "Raw" view and "Download" (R9).
Diff content view displays line numbers, green background for additions, red background for deletions, and monospace text.
Tapping any diff line highlights the row and displays the floating "Add Comment" button in the bottom thumb zone.

## Interactions

Tap Repo picker: Open dropdown list of all Repos found in the Workdir. Changing selection immediately reloads diff.
Tap "Prev Chg" / "Next Chg": Smoothly scroll viewport to previous or next modified diff hunk.
Tap "Wrap" toggle: Switch code lines between horizontal scrolling (`nowrap`) and line wrapping (`pre-wrap`).
Tap "Raw" button: Open modal displaying complete raw file contents directly from disk.
Tap "Download" button: Trigger browser file download of the raw modified file.
Tap any diff line: Select line and reveal "Add Comment on Line X" button in the bottom thumb zone.
Tap "Add Comment": Open inline comment draft bottom sheet bound to Repo, file path, and line number (R5).
Tap file header chevron: Collapse or expand diff body for that specific file.
Pull down on changes list: Trigger fresh server-side git diff execution and update view.

## States

Loading: Displays "Scanning Workdir for Repos..." progress spinner with elapsed time counter during deep directory scans.
Empty: Displays centered card: "No Changes found. Working tree is clean." with a re-scan button.
Error: If git command fails or folder is not a git repository, displays error card: "Git diff error: fatal not a git repository."
Live: While the Changes tab is open and in the foreground, edits show through Datastar SSE patches under the delivery policy in Data Needs. No stream runs when the tab is hidden or the app is backgrounded.
Stale: If a refresh is due while a line is selected or a Comment draft is open, the view does not patch. A banner reads "Changes updated. Tap to refresh."
Backgrounded: The Changes stream closes. View refreshes once on resume.
Slow-scan: If Repo discovery exceeds 2 seconds in large Workdirs, a progress bar shows: "Scanning directory tree... (ignoring caches and vendor)".

## Data Needs

Scanner fields: Workdir path from session header line.
herdr actions: Not used directly for review.
SSE events: Stream `/events/session/<id>/changes` opens when the Changes tab shows and closes when it hides or the app backgrounds. PLAN R2 and R4 promise discovery and rendering only, not live push, so the stream is an addition kept cheap. The server never watches the working tree. Events: `changes_updated` (fragment with the re-diffed file cards) and `changes_stale` (no fragment, drives the banner).
Re-diff triggers: stream connect (tab focus or resume), pull-to-refresh, Repo picker change, Status moving from working to idle, blocked, or done, and a Session jsonl tool-result entry (any tool, since bash can write files) landing on the scanner tail.
Throttle: tool-result triggers debounce with a 3 s quiet period. At most one re-diff per 10 s per Repo. The server compares `git status --porcelain` plus diff hash with the last sent state and sends nothing if unchanged. Only changed file cards are patched; collapse state is kept. Pull-to-refresh and Repo change skip the debounce and the 10 s cap.
Git service: Server runs pruned discovery scan for `.git` using denylist (node_modules, vendor, build, dist, target, .venv, hidden dirs); runs `git status` and `git diff` per Repo; fetches raw file blobs.
State JSON: Read and write active Repo choice per Session to remember preference across visits.

## Edge Cases

Backgrounding and SSE death: Client refreshes diff state from server upon returning to foreground to capture any edits made while away.
herdr attach contention: Not applicable to read-only diff inspection.
Unknown jsonl entries: Not applicable on Changes tab.
Missing Workdir or Repo: If Workdir is missing or has zero git Repos, displays clear fallback card: "No git repositories discovered in Workdir."
Stale Status: Status badge updates in header independently; diff viewer functions normally even if agent driver halts.
Large diffs: Diffs exceeding 2,000 lines truncate by default with a button: "Diff exceeds 2,000 lines. Tap to load remaining 1,420 lines."
Binary files: Binary modifications display header with notice: "Binary file modified (preview unavailable)" and preserve the Download button.
Line number shift: If agent modifies file while user views diff, Datastar live patch updates line numbers cleanly.
