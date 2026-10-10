# Terminal Tab Screen

Purpose: Provide an interactive xterm.js terminal bridge to the session's herdr target, supporting read-only peek mode, explicit takeover, and mobile keys.
Feature ids covered: T1, T2, T3, T4.
Entry points: Tapping the "Terminal" tab in Session view; tapping quick terminal toggle icon in session header.

```
+-----------------------------------+
| [<] fix-scanner-mtime [working][:]|
+-----------------------------------+
|[Chat][Changes][Term*][Files][Cmts]|
+-----------------------------------+
| Peek Mode (Read-only)             |
| [ Take the wheel (Attach)       ] |
+-----------------------------------+
| pi@server:~/Dev/PiRemote$ git st  |
| On branch main                    |
| Changes not staged for commit:    |
|   modified:   scanner/tail.go     |
|                                   |
| pi agent thinking...              |
|                                   |
|                                   |
|                                   |
+-----------------------------------+
| [ESC] [TAB] [CTRL*] [ALT] [^] [v] |
+-----------------------------------+
| (System Keyboard Attached)        |
+-----------------------------------+
```

## Layout and Components

Top header maintains persistent session navigation and status indicators.
Mode banner: Indicates current attachment state. In "Peek Mode (Read-only)" (T4), it presents a prominent "Take the wheel (Attach)" button (T3). When attached interactively, it displays "Attached (Interactive)" with a "Detach" button.
Terminal viewport: Embeds vendored xterm.js canvas rendering the pane's ANSI stream fetched over the herdr socket with `pane.read` format `ansi`; `pane_output_changed` events plus buffer revisions drive incremental redraws (T1). Verified in M0 findings: the socket has no attach method and no PTY bridge exists.
Mobile key bar: Sits fixed directly above the on-screen keyboard in the thumb zone. Includes buttons: `[ESC]`, `[TAB]`, `[CTRL]`, `[ALT]`, `[^]` (up), `[v]` (down), `[<]` (left), `[>]` (right) (T2).
Key latch indicators: Modifier keys (`CTRL` and `ALT`) toggle an active latch highlight when pressed once, applying to the next typed character before resetting. Double-tapping locks modifier.
Terminal toolbar: Bottom edge holds font size reset button and copy selection button.

## Interactions

Tap "Take the wheel": Switches from peek to interactive mode: keystrokes enabled and sent through `pane.send_input`. There is no takeover dialog: the socket has no attach lock and no `--takeover` (M0 findings). If the desk holds a CLI attach, both sides see output and typed input interleaves; a banner notes "Desk terminal attached" when the pane reports one.
Tap "Detach": Drop interactive PTY connection and revert to read-only peek stream.
Tap terminal viewport: In interactive mode, focuses hidden input element to invoke system on-screen software keyboard.
Tap mobile key (`ESC`, `TAB`, arrow keys): Sends corresponding ANSI escape sequence through WebSocket to server PTY.
Tap modifier key (`CTRL`, `ALT`): Latches modifier state. Typing subsequent letter sends control sequence (example: latched `CTRL` + `C` sends `\x03`).
Long-press modifier key: Lock modifier continuously until tapped again.
Rotate phone to horizontal orientation: Terminal container recalculates width and height, sending JSON resize frame to server PTY bridge (`{"type": "resize", "cols": 120, "rows": 32}`).
Pinch to zoom inside terminal: Adjusts xterm.js font size smoothly between 10px and 18px without altering container geometry.
Double tap terminal text: Select word for copying.
Swipe up or down inside terminal viewport: Scroll terminal history buffer when not in alternate screen mode.

## States

Loading: Viewport renders black canvas with centered text: "Connecting to terminal bridge...".
Peek: Read-only stream renders live terminal output without sending keystrokes or claiming exclusive attach lock (T4).
Interactive: Bidirectional WebSocket stream active; keystrokes send directly to PTY; mobile key row enabled.
Contended: The desk holds a CLI attach on the same pane; interactive mode stays available and a coexistence banner is shown (T3).
Error: If PTY spawn fails or herdr target disappears, displays red banner: "Failed to connect to target pane. Session may be stopped."
Backgrounded: WebSocket disconnects automatically when browser or WebView enters background to avoid keeping unused PTY processes alive on the server.

## Data Needs

Scanner fields: Workdir path for context.
herdr actions: `pane.read` (format `ansi`, revision tracked) on connect and on `pane_output_changed` events; `pane.send_input` for keystrokes and `pane.send_keys` for the key row. No PTY process, no cgo (M0 findings).
SSE events: Secondary to WebSocket; transmits Status badge changes in header.
Git service: Not used directly by terminal.
State JSON: Stores preferred terminal font size.

## Edge Cases

Backgrounding and SSE death: Mobile browser backgrounds drop WebSocket connections immediately. Upon return to foreground, PiRemote reconnects in peek mode to avoid stealing focus if another client connected in the meantime.
herdr attach contention: The socket has no attach lock, so phone and desk can share the pane; the banner keeps the user aware. Peek mode remains the default so a phone visit never types into the desk session (T3, T4).
Unknown jsonl entries: Not applicable; terminal bridge operates on raw byte streams from herdr PTY, completely independent of jsonl parsing.
Missing Workdir or Repo: Terminal opens in whichever directory herdr target is located; missing repo does not prevent terminal access.
Stale Status: If agent Driver terminates, PTY stream closes and displays message: "Terminal disconnected: process exited."
Software keyboard viewport resizing: On mobile devices, opening the software keyboard shrinks the window viewport. The TS island handles viewport resize events and sends updated row dimensions to prevent output clipping.
Rapid key repeat: Mobile key row debounces touches to prevent accidental double-execution of escape commands.
PTY buffer desynchronization: If network jitter causes missing frame bursts, sending a terminal clear signal restores accurate canvas state.
