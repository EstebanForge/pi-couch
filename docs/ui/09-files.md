# Files Tab Screen

Purpose: Browse directory tree of Repos within the session Workdir, inspect raw text file contents, and download files directly to mobile device.
Feature ids covered: V11.
Entry points: Tapping the "Files" tab in Session view.

```
+-----------------------------------+
| [<] fix-scanner-mtime [working][:]|
+-----------------------------------+
|[Chat][Changes][Term][Files*][Cmts]|
+-----------------------------------+
| Repo: [ Couch Pi (main)        v] |
| Path: /internal/scanner           |
+-----------------------------------+
| [..] (Parent directory)           |
| > internal/                       |
|   v scanner/                      |
|     scanner.go          14.2 KB   |
|     tail.go              8.1 KB   |
|     tail_test.go         4.3 KB   |
|   > driver/                       |
| > cmd/                            |
| go.mod                   1.1 KB   |
| README.md                3.4 KB   |
+-----------------------------------+
| File Modal (On file tap):         |
| tail.go (8.1 KB) [Raw] [Download] |
| 1 package scanner                 |
| 2 import ( "os" ... )             |
| [Close]                           |
+-----------------------------------+
```

## Layout and Components

Top header maintains persistent session navigation and status indicators.
Repo selector dropdown sits at top: allows selecting target Repo if Workdir contains multiple repositories.
Path breadcrumb bar: Displays current directory path with clickable segment links for fast folder ascending.
File tree list: Displays folder items with expand and collapse chevrons (`>` or `v`), and file items with filename and formatted file size in kilobytes or megabytes.
File viewer bottom sheet: Appears upon tapping any text file. Features top bar with filename, file size, "Raw" toggle button, and "Download" button. Body shows monospace text content with line numbers.
Large-tree handling: Directories load lazily on expansion; deep scans apply ignore rules to exclude heavy dependency directories.

## Interactions

Tap Repo selector: Switch browsing context to another Repo within the Workdir.
Tap folder row: Expand folder node to fetch and display immediate child contents. Tapping an open folder collapses its sub-tree.
Tap parent directory (`[..]`): Navigate upward one directory level in tree hierarchy.
Tap breadcrumb segment: Jump directly to selected ancestor directory in path.
Tap file row: Fetch file content from server and slide up File viewer sheet.
Tap "Raw" button in viewer: Toggle syntax-highlighted formatted view with plain raw text view.
Tap "Download" button in viewer: Trigger browser download of file blob to phone local storage.
Tap "Close" button in viewer: Dismiss file viewer sheet and return to file tree.
Swipe down on file viewer header: Dismiss viewer sheet smoothly.

## States

Loading: Displays shimmer placeholders on tree rows while directory listing is fetched from server.
Empty: If folder has zero files, displays centered message: "Directory is empty."
Error: If file permissions prevent reading or file was deleted, displays red notification card: "Cannot read file: Permission denied."
Live: Tree updates dynamically if files are created or deleted during session.
Backgrounded: File viewer keeps currently opened file content intact in browser DOM; resumes without reloading.

## Data Needs

Scanner fields: Workdir path from session header.
herdr actions: Not used directly for file browsing.
SSE events: Server sends optional `files_changed` event when filesystem watcher notes additions or deletions in repository root.
Git service / Filesystem service: Server executes filesystem read operations pruned by denylist rules (node_modules, vendor, build, dist, target, .venv, caches, hidden directories); streams file text content and downloads.
State JSON: Stores last opened directory path per Repo.

## Edge Cases

Backgrounding and SSE death: Cached file tree remains visible when app is backgrounded; refreshes upon user interaction after wake.
herdr attach contention: Not applicable to read-only file browsing.
Unknown jsonl entries: Not applicable on Files tab.
Missing Workdir or Repo: If target directory disappears, tree displays warning: "Workdir not accessible on disk" and provides a retry button.
Stale Status: Status updates in header independently; file browsing continues unaffected by agent Driver state.
Large directory trees: Repositories with tens of thousands of files only load child nodes on demand when a directory is tapped, never loading entire trees at once.
Large or binary files: If file size exceeds 2 MB or contains binary data, preview is disabled and shows note: "File is binary or exceeds 2 MB. Direct download only."
Symlink loops: Filesystem reader checks canonical file paths and ignores recursive symlink loops safely.
