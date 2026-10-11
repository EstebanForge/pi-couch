# Web Push and App Shell Screen

Purpose: Configure the Android WebView APK connection URL on first launch and manage Web Push notifications triggered by agent Status transitions.
Feature ids covered: P5, P8, P1.
Entry points: First launch of the sideloaded Android WebView APK shell; Settings panel in web client; system notification shade.

```
+-----------------------------------+
| Couch Pi Setup                    |
+-----------------------------------+
| Welcome to Couch Pi.              |
| Connect to your server tailnet:   |
|                                   |
| Server URL:                       |
| +-------------------------------+ |
| | https://box.tailscale.net:8443| |
| +-------------------------------+ |
| Default: Tailscale serve address  |
|                                   |
| [ Connect & Save ]                |
+-----------------------------------+
| NOTIFICATION SETTINGS (In-App)    |
| [x] Enable Web Push Notifications |
| Push on:                          |
| - working -> idle                 |
| - working -> blocked              |
| - working -> done                 |
+-----------------------------------+
| ANDROID NOTIFICATION PREVIEW      |
| +-------------------------------+ |
| | [Couch Pi] fix-scanner-mtime  | |
| | Agent blocked: Need input     | |
| +-------------------------------+ |
+-----------------------------------+
```

## Layout and Components

First-launch APK setup card: Displays brief welcome text, server URL text input field (pre-filled with configured default tailscale serve address), helper caption, and "Connect & Save" primary button.
Notification configuration card (inside settings): Contains master toggle checkbox "Enable Web Push Notifications" and descriptive list of trigger conditions based on Status transitions.
Android system notification structure: Shows app icon, notification title formatted as `Couch Pi: <Session Name>`, body text describing event (example: `Agent blocked: Need input` or `Task complete: Done`), and direct deep-link action.

## Web Push Timing and Content

Permission ask timing: The browser or WebView does not prompt for notification permission on initial page load. Instead, Couch Pi asks only when the user submits their first Steering prompt or explicitly toggles the notification setting.
Notification delivery triggers: Web push sends using VAPID keys upon specific herdr Status transitions:
- Transition `working` to `done`: "Agent completed task execution." This is the normal completion transition for pi; a finished turn reports `done`, not `idle` (M0 findings).
- Transition `working` to `blocked`: "Agent is blocked and requires clarification or input."
- Transition `working` to `idle`: rare for pi; kept for agents that report it.
Notifications do not fire for `idle` to `working` or minor intermediate token updates to prevent phone notification clutter.
Tap-through target: Tapping the system notification opens the Android WebView APK shell or browser directly into that specific Session chat view.

## Interactions

Type in Server URL field: Edit target host and port address for Tailscale server connection.
Tap "Connect & Save": Validate URL format, persist value in Android app local SharedPreferences, load web application full screen, and dismiss setup card forever (P8).
Toggle "Enable Web Push Notifications": Invoke browser Notification API `Notification.requestPermission()`. If approved, send VAPID subscription JSON payload to server endpoint `/api/push/subscribe`.
Tap system notification: Wake phone display, bring Couch Pi to foreground, navigate directly to Session Chat view of the notifying session.
Dismiss system notification: Remove alert without affecting background agent state.

## States

Unconfigured: APK launches with URL input card displayed if no saved URL is found in local storage.
Loading: "Connecting to server..." spinner appears while validating server handshake over tailnet.
Subscribed: Push notifications toggle is checked; active registration badge displayed.
Error: If URL connection fails, displays red inline error: "Cannot reach server. Verify Tailscale VPN connection."
Permission denied: If user blocks notification permission in browser, toggle unchecks and displays note: "Notifications blocked in browser settings."
Backgrounded: Server sends pushes via VAPID Web Push protocol; Android system wakes service worker or browser process in background to show notification.

## Data Needs

Scanner fields: Session name and ID to construct notification title and deep-link URL.
herdr actions: Subscribe to agent Status transition events over socket protocol (`subscription_event`).
SSE events: Server generates push messages independently from SSE when client is asleep.
Git service: Not used by notification subsystem.
State JSON: Server database or JSON store records client push subscription endpoints; Android app SharedPreferences stores target server URL.

## Edge Cases

Backgrounding and SSE death: Push notifications exist specifically to handle complete backgrounding and SSE disconnection. Even when browser tab is dead, server pushes via VAPID directly to Android OS.
herdr attach contention: Not applicable to notification push delivery.
Unknown jsonl entries: Not applicable to notifications.
Missing Workdir or Repo: Not applicable; notifications link to Session ID.
Stale Status: Notifications fire solely on discrete transition events from herdr; stationary or unknown statuses never trigger false alarms.
Tailscale offline when notification tapped: If tailnet VPN is disabled when user taps notification, WebView displays offline screen with retry button: "Tailnet unreachable. Ensure Tailscale is connected."
Duplicate transitions: Server debounces Status transition events within a 2-second window to prevent multiple notifications for rapid state bounces.
Revoked subscriptions: If browser push endpoint returns HTTP 410 Gone, server automatically deletes invalid subscription endpoint from database.
