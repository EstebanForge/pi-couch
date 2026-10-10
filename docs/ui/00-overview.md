# System Overview

Purpose: Define navigation, design tokens, interaction rules, and system states across PiRemote.
Feature ids covered: P1, P2, P3, P4, P6, P8.
Entry points: App launch via Android WebView APK shell (P8) or mobile browser PWA.

```
+-----------------------------------+
| PiRemote              [Status] [:]|
+-----------------------------------+
| Tabs: [Chat*] [Chg] [Term] [File] |
+-----------------------------------+
|                                   |
| Viewport Content                  |
| (Scrollable, 360px portrait width)|
|                                   |
| Reach Zone: Secondary / Read      |
| Headers, breadcrumbs, logs        |
|                                   |
|-----------------------------------|
| Thumb Zone: High interaction      |
| Action buttons, cards, list items |
|                                   |
+-----------------------------------+
| Sticky Composer / Key Row (48px)  |
+-----------------------------------+
```

## Layout and Components

PiRemote uses a flat two-tier hierarchy: the Session list at root, and an active Session view.
Selecting a Session opens its dedicated container.
Inside a Session, five tabs organize tools: Chat (default), Changes, Terminal, Files, Comments.
The desktop layout uses the identical DOM stretched horizontally; it never splits into a multi-column desktop layout.
The session header anchors to the top edge and displays Session name, Workdir path, Status badge, terminal toggle, and menu button.
The bottom bar anchors inputs in the thumb zone for reliable one-hand reach.
All interactive controls measure at least 48x48 px in touch area, with 8 px minimum margin between adjacent targets.

## Design Tokens

Dark theme is default (P3). Light theme uses equivalent high-contrast role inversions.
Background base: `#0f1115` (dark), `#ffffff` (light). Surface card: `#181b21` (dark), `#f4f6f8` (light). Surface raised: `#222630` (dark), `#e5e8ec` (light). Border: `#2e3440` (dark), `#d1d5db` (light).
Text primary: `#f3f4f6` (dark), `#111827` (light). Text muted: `#9ca3af` (dark), `#6b7280` (light). Accent primary: `#3b82f6` (dark and light).
Status working: `#3b82f6` (blue). Status idle: `#10b981` (green). Status blocked: `#f59e0b` (amber). Status done: `#6b7280` (gray). Status unknown: `#ef4444` (red).
Spacing scale: 4px, 8px, 12px, 16px, 24px, 32px.
Type scale: text-xs (11px, metadata), text-sm (13px, secondary labels), text-base (15px, body and chat), text-lg (17px, headers), text-xl (20px, titles). Monospace uses system font stack (`ui-monospace, monospace`) at 13px.

## Interactions

Tap top-left back arrow: Pop navigation stack. If inside a secondary tab (Changes, Terminal, Files, Comments), returns to Chat. If in Chat, returns to Session list.
Tap top navigation tab: Switch view immediately via Datastar patch without reloading page assets.
Tap action button in thumb zone: Trigger command or open bottom sheet with minimal thumb movement.
Tap background mask on open modal: Dismiss modal without executing pending action.
Long-press interactive card: Reveal secondary context actions (example: copy ID, close Session).
Swipe right from left screen boundary: Trigger system back navigation in Android WebView or browser.

## States

Loading: Displays skeleton placeholders matching the exact dimensions of final components to prevent layout shift.
Empty: Renders explicit explanatory text and one direct call to action (example: "No Changes found").
Error: Displays full-width banner or card with exact failure description, zero stack traces, and a retry action.
Live: Subtle pulse on the Status badge indicating active SSE stream.
Backgrounded: Client suspends animations and marks connection dormant until viewport refocus.

## Data Needs

Scanner fields: Session paths, timestamps, metadata from jsonl files under `~/.pi/agent/sessions/`.
herdr actions: Agent snapshot listing, prompt submission, key sending, target creation and closing.
SSE events: Datastar fragment patches for transcript lines, Status changes, and comment updates.
Git service: Repository paths, diff lines, raw file blobs executed server-side.
State JSON: Per-session state file on server disk holding sent comments and interface preferences.

## Edge Cases

Backgrounding and SSE death: Android halts background JavaScript and drops SSE connections. On foreground wake, client connects using `Last-Event-ID` header to pull missed events.
herdr attach contention: Only one attach client connects to a herdr target at a time. The UI starts in read-only peek mode and requires explicit confirmation for `--takeover`.
Unknown jsonl entries: Parser renders unknown entry types as collapsible raw-JSON blocks without failing.
Missing Workdir or Repo: If Workdir path vanishes from disk, interface displays warning card and disables git operations.
Stale Status: If herdr socket disconnects or process halts, Status displays "unknown" until socket recovers.

## Copy Rules

All UI text uses domain terms defined in CONTEXT.md: Session, Workdir, Repo, Changes, Comment, Steering, Driver, Status, Stop, Resume, Close.
Copy never displays herdr internal terms (workspace, tab, pane, agent); it refers to them as "herdr target" if required.
Language follows ASD-STE100 technical style: short, direct sentences, active voice, zero conversational filler.
No marketing text, no buzzwords, no em-dashes, and no middle dot symbols.
Markdown prose rule: one paragraph per source line without manual column-wrapping.
