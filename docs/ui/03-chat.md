# Chat Transcript Screen

Purpose: Primary interactive view displaying the live session transcript, tool executions, agent thinking, usage metrics, and the Steering composer.
Feature ids covered: V2, V3, V4, V5, V7, V8, V9, V10, D3, D4, D5, D6.
Entry points: Tapping a Session card on the session list screen; default tab when entering any Session.

```
+-----------------------------------+
| [<] fix-scanner-mtime [working][:]|
+-----------------------------------+
|[Chat*][Changes][Term][Files][Cmts]|
+-----------------------------------+
| [Active branch]           [Search]|
+-----------------------------------+
|                                   |
|                +----------------+ |
|                | Fix fsnotify   | |
|                | tail race      | |
|                +----------------+ |
|                                   |
| v Thinking (1.2k tokens)    [Copy]|
|   Analyzing fsnotify events...    |
|                                   |
| [x] tool: exec rg "tail"    [Copy]|
|   > main.go:42: watcher.Add()     |
|                                   |
| ! Provider error: Rate limited    |
|   Retrying backoff in 4s...       |
|                                   |
| The watcher needs a buffer...     |
|                             [Copy]|
+-----------------------------------+
| claude-3-7-son | med | $0.04 | 22%|
+-----------------------------------+
| [Stop] | Steering input...    [>] |
| (Queued follow-up when active)    |
+-----------------------------------+
```

## Layout and Components

Header bar provides back button, Session name, live Status badge, quick terminal toggle, and session menu button (V8).
Tabs navigation strip sits below header: Chat (active), Changes, Terminal, Files, Comments.
Subheader strip displays a read-only "Active branch" label and an in-transcript search toggle button (V3, V10). The label means the pi Session tree branch (the `id`/`parentId` path ending at the current leaf), not a git branch; it is not tappable in v1 because the branch picker is unscheduled, and it is hidden when the tree has a single path. The git branch shows in the Repo picker on the Changes tab.
Transcript container holds message bubbles: user owner messages right-aligned, assistant messages left-aligned.
Thinking blocks render inside collapsible cards with token counts, collapsed by default after message completion.
Tool-call rows render as compact one-line bars showing execution icon, tool name, brief arguments, and success checkmark (V4).
Tool output expands downward upon tap into a monospace container with copy button (V7).
Inline error cards render full-width cards with amber or red borders for provider refusals, rate limits, or network errors (V5).
Usage footer strip sits fixed above the composer: displays model name, thinking level, session cost in dollars, and context window percent (V9).
Steering bar sits fixed at the screen bottom: Stop button (red icon) on left, auto-expanding text input in center, submit button on right, with hint text below input (D3, D4, D5).

## Interactions

Tap Thinking header: Toggle expansion of reasoning text block.
Tap Tool-call row: Toggle expansion of raw stdout or stderr output.
Tap "Copy" icon: Copy message markdown, code snippet, or tool output directly to system clipboard, displaying a temporary toast confirmation (V7).
Tap "Search" button: Open search input field to filter and highlight matching text occurrences throughout the transcript (V10).
Tap "Stop" button: Send Ctrl-C key sequence through herdr `send-keys` API to halt active generation immediately (D5).
Tap submit button: Submit entered text to herdr `agent prompt` API for Steering (D3). If the agent is working, pi queue mode buffers the message as a queued follow-up (D4).
Tap attachment icon: Open system photo picker or camera. Each image shows as a thumbnail chip above the input with a remove button (D6). On submit the server sends the image as base64 in the herdr prompt when the Driver supports it.
Image fallback: if herdr prompt does not accept images, the server saves the file under the Session state directory (outside the Workdir) and appends a line to the prompt text: `Attached image: <absolute path>`. The chip then reads "Sent as file path" so the user knows the model gets a path, not the pixels. If the file save fails, the prompt is not sent and an error card shows the failure (D6).
Scroll up in transcript: Suspend auto-scroll lock so the user can read previous messages without viewport jumping.
Scroll to bottom: Re-engage auto-scroll lock to track new incoming streaming tokens.

## States

Loading: Displays skeleton message cards while reading session jsonl from disk.
Empty: Displays centered prompt card: "Session started. Send a prompt below to begin."
Error: Displays full-width error notification at bottom if herdr socket communication fails.
Live: New message tokens stream into the transcript via SSE; Status badge shows working with active pulse (V2).
Backgrounded: Browser suspends SSE. When app regains focus, client reconnects with last known line ID and replays any missing entries.

## Data Needs

Scanner fields: Read session jsonl entries linked by `id` and `parentId`, extract active branch path, message roles, content blocks (text, thinking, toolCall), and token usage statistics.
herdr actions: Send `agent prompt` for Steering (with base64 images where supported, else a path note, D6), `send-keys` with Ctrl-C byte sequence for Stop, and subscribe to agent Status updates.
SSE events: Server streams `transcript_append` events for new entries and `status_update` events for live badge changes.
Git service: Not used directly on this screen.
State JSON: Read user display preferences (expanded tool rows, search query).

## Edge Cases

Backgrounding and SSE death: Mobile OS kills background SSE. On resume, the client uses `Last-Event-ID` to request missed entries from the scanner.
herdr attach contention: Not applicable to Steering via prompt; prompt injection works independently of terminal attach status.
Unknown jsonl entries: Parser encounters custom or unexpected entry types and renders them as collapsible JSON rows labeled "raw event" without crashing.
Missing Workdir or Repo: If Workdir disappears, transcript remains readable from jsonl cache, but Steering input disables with a warning banner.
Stale Status: If herdr stops responding, Status badge changes to `[unknown]` and the Stop button disables.
Branched jsonl trees: The transcript follows the deepest active branch path matching the latest entry parent links; divergent branches remain hidden.
Large transcripts: Transcripts exceeding 500 blocks use CSS containment and virtual rendering to maintain 60 FPS scrolling on mobile devices.
