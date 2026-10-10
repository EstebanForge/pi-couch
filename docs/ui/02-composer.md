# Home Composer Screen

Purpose: Configure parameters and draft the initial prompt to start a new pi session driven by herdr.
Feature ids covered: D1, D2, D10.
Entry points: "New Session" button from Session list screen.

```
+-----------------------------------+
| [Cancel]   New Session    [Start] |
+-----------------------------------+
| Workdir:                          |
| [ ~/Dev/EstebanForge/PiRemote  v] |
+-----------------------------------+
| Driver Target:                    |
| (o) Create new target             |
| ( ) Existing: [target-1 v]       |
+-----------------------------------+
| Model:           Thinking:        |
| [claude-3-7-son] [medium       v] |
+-----------------------------------+
| Prompt:                           |
| +-------------------------------+ |
| | Add live tail test for SSE    | |
| | resumption on mobile wake     | |
| |                               | |
| |                               | |
| |                               | |
| +-------------------------------+ |
+-----------------------------------+
| [Attach Image]         [Mic: 12s] |
| Dictating (Whistle WASM)...       |
+-----------------------------------+
```

## Layout and Components

Top action bar features a prominent "Cancel" button on the left and an active "Start" button on the right.
Workdir picker displays recent folders with a dropdown selector, plus a browse button for manual path input.
Driver target picker allows selecting between creating a new herdr target or targeting an existing inactive herdr target.
Model picker dropdown lists available configured models for the pi process (D2).
Thinking picker dropdown sets reasoning budget flags (none, low, medium, high) passed to pi on startup.
Prompt textarea provides an auto-expanding multi-line text input with 4 lines minimum height.
Bottom action row contains the image attachment button on the left and the voice dictation microphone button on the right.
Voice recording status bar displays live audio waveform or elapsed duration when dictation runs.

## Interactions

Tap "Cancel": Abort session creation and return to Session list screen.
Tap "Start": Validate inputs, request herdr target creation, launch pi process with selected flags, and navigate to Session chat view.
Tap Workdir dropdown: Open selection sheet displaying the five most recent Workdirs and a custom path input field.
Select target radio button: Toggle between creating a new herdr target and picking an existing one from the dropdown list.
Tap Model or Thinking dropdown: Select startup flags passed to the pi execution command.
Tap Prompt textarea: Focus input and open on-screen software keyboard.
Tap "Attach Image": Open device photo picker or file browser to select an image attachment (D6).
Tap Microphone button: Toggle voice dictation on or off using Whistle WASM (D10).
Long-press Microphone button: Open voice options menu (switch language, view fallback status).

## Voice Dictation Flow

Whistle runs client-side inside the TS island via WebAssembly without uploading audio to external servers.
Tap mic icon: Browser requests microphone permission via HTTPS (tailnet TLS).
Recording starts: Red pulsing ring appears around microphone icon, timer counts up from 0s toward the 30s limit.
Audio capture runs in 16 kHz mono format via Web Audio API.
At 30 seconds or on manual stop tap: WASM engine transcribes buffer, appends transcribed text directly into the prompt textarea, and resets timer.
If WASM execution fails or device hardware stalls: The client falls back to uploading the audio buffer to the server for processing via the vendored needle binary.

## States

Loading: Model list and herdr target options fetch asynchronously while inputs show loading placeholders.
Empty: Prompt textarea shows placeholder: "What should the agent build or fix?".
Error: Red banner displays below top bar if target creation or pi process startup fails.
Live: Microphone button pulses bright red and displays active seconds counter while recording.
Backgrounded: If user switches apps while recording, audio recording stops immediately and processes collected audio up to the pause point.

## Data Needs

Scanner fields: Read known Workdir paths from existing session files to populate recent folder choices.
herdr actions: List current targets via socket; execute target creation and `pi --session <id>` startup command.
SSE events: Not active on this configuration screen.
Git service: Verify that selected Workdir exists and check for initialized Repos within it.
State JSON: Read client settings for last used model and thinking level preferences.

## Edge Cases

Backgrounding and SSE death: Form draft persists in local browser memory so switching apps does not discard entered prompt text.
herdr attach contention: Steering is unaffected; only the terminal tab obeys the one-attach-client rule (T3), so new targets are always valid here.
Unknown jsonl entries: Not applicable on this setup screen.
Missing Workdir or Repo: If user inputs a non-existent directory path, the UI presents an inline validation error: "Path does not exist on server".
Stale Status: If herdr daemon terminates while picker is open, target list disables and shows error: "herdr driver unavailable".
Microphone permission denied: If browser blocks audio access, mic button displays warning slash and toast: "Microphone permission denied. Type prompt manually."
Large image attachments: If image exceeds 5 MB, client downscales image in browser canvas before encoding to base64.
Audio window rollover: Dictation exceeding 30 seconds automatically cuts current window, transcribes text, inserts it, and begins next window immediately.
