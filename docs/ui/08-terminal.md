# Terminal Tab Screen

Purpose: Provide an interactive xterm.js terminal bridge to the session's hc terminal, supporting a read-only default view, mobile keys, and paste-style text delivery.
Feature ids covered: T1, T2, T3, T4.
Entry points: Tapping the "Terminal" tab in Session view; tapping quick terminal toggle icon in session header.

```
+-----------------------------------+
| [<] fix-scanner-mtime [working][:]|
+-----------------------------------+
|[Chat][Changes][Term*][Files][Cmts]|
+-----------------------------------+
| Read-only view                    |
| [ Take the wheel (Keys)         ] |
+-----------------------------------+
| pi@server:~/Dev/pi-couch$ git st  |
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
Mode banner: In the read-only view (T4), it presents the "Take the wheel (Keys)" button (T3). With the wheel held, it displays "At the wheel" with a "Drop" button.
Terminal viewport: Embeds vendored xterm.js canvas rendering the terminal's ANSI stream. The server holds one hc framed attach (observe) per connected client and fans the bytes out as SSE delta frames: a `size` frame carries the terminal's real grid before the first paint, then base64 delta frames. A `reset` frame (core resync, hc ErrUnsynced) makes the client reconnect; a `gone` frame (terminal exited, hc error kind finished) ends the stream (T1).
Composer box: One text input above the key bar. Submitting sends the text through the server's writer attach: hc kind 6 (automatic input) injects the raw bytes into the terminal's pty without appending Enter, so the text lands as typed characters, never as a submitted line. A dead session or a paste failure raises the "Paste not sent" banner (T2).
Mobile key bar: Sits fixed directly above the on-screen keyboard in the thumb zone. Includes buttons: `[ESC]`, `[TAB]`, `[CTRL]`, `[ALT]`, `[^]` (up), `[v]` (down), `[<]` (left), `[>]` (right) (T2).
Key latch indicators: Modifier keys (`CTRL` and `ALT`) toggle an active latch highlight when pressed once, applying to the next typed character before resetting. Double-tapping locks modifier.

## Interactions

Tap "Take the wheel": Enables the composer box and confirms the key row target; the stream stays the same observe attach. There is no takeover handshake: hc has no attach lock, and desk-side sessions keep rendering whatever they render.
Tap "Drop": Hides the composer box; key row stays available for direct keys.
Tap terminal viewport: In wheel mode, focuses hidden input element to invoke system on-screen software keyboard.
Submit the composer box: One POST to the terminal input endpoint; the server opens a short-lived writer attach (observe false, automatic_input required, fail closed without it), sends the draft as one kind 6 frame, drains and credits the core's output window, and closes. No Enter is ever appended: a draft never submits a line by itself.
Tap mobile key (`ESC`, `TAB`, arrow keys): Sends the key name through hc `terminal.keys` in one request.
Tap modifier key (`CTRL`, `ALT`): Latches modifier state. Typing subsequent letter sends the composed key name (example: latched `CTRL` + `C` sends key `ctrl+c`).
Long-press modifier key: Lock modifier continuously until tapped again.
Rotate phone to horizontal orientation: The client reconnects the stream with new `cols`/`rows` query params (clamped to 40-500 by 10-200) so the core resizes the pty and the first `size` frame carries the new grid.
Double tap terminal text: Select word for copying.
Swipe up or down inside terminal viewport: Scroll terminal history buffer when not in alternate screen mode.

## States

Loading: Viewport renders black canvas with centered text: "Connecting to terminal bridge...".
Read-only: Stream renders live terminal output; key row works, composer box hidden until the wheel is taken (T4).
Wheel: Composer box visible; keys and paste flow to the terminal.hc has no attach lock, so desk and phone share the terminal by construction.
Contended: Not applicable - there is no attach lock and no takeover. If pi is mid-turn the keys still land; pi's own queue mode decides what they do to the running turn.
Error: A dead terminal answers the stream with `gone` and displays the "Session may be stopped" banner; a failed composer send shows "Paste not sent" with the server's reason.
Backgrounded: SSE disconnects automatically when browser or WebView enters background; reconnect resumes the observe attach without disturbing the session.

## Data Needs

Scanner fields: Workdir path for context.
hc actions: one framed attach (observe) per stream client for output; `terminal.keys` for the key row and quick keys; a writer attach with kind 6 per composer submit for text. No PTY process, no cgo; the wire format is pinned in docs/hc-findings.md.
SSE events: `size` then base64 `delta` frames on `/stream/terminal/{key}`; status badges ride the sessions stream, not the terminal stream.
Git service: Not used directly by terminal.

## Edge Cases

Backgrounding and SSE death: Mobile browser backgrounds drop SSE connections immediately. Upon return to foreground, Couch Pi reconnects the observe attach; the first `size` frame repaints the real grid.
Paste safety: The composer's writer attach never appends Enter, so a pasted multi-line draft sits in pi's editor unsubmitted. This is the same verb the dialog card uses to rewrite pi's editor buffer (ADR-0012).
hc resync: A core resync surfaces as a `reset` SSE frame; the client reconnects and repaints from the next `size` frame. Not an error.
Missing Workdir or Repo: Terminal opens on the session's hc terminal regardless of repo state; a missing repo does not prevent terminal access.
Stale Status: If the terminal exits, the attach path surfaces hc error kind finished as `gone`, and the screen shows "Terminal disconnected".
Software keyboard viewport resizing: On mobile devices, opening the software keyboard shrinks the window viewport. The client re-reads the container and reconnects with updated dimensions to prevent output clipping.
Stream desynchronization: Frame loss is impossible to observe from the client: every reconnect starts from a fresh `size` frame, so the canvas always resyncs to the real grid.
