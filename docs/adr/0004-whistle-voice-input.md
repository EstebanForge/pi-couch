# Voice input runs Whistle as client WASM; server exec is the fallback

Composer dictation uses Whistle (Cactus, Apache-2.0, 16.9 MB, seven languages, CPU only): the engine compiled to WebAssembly runs in the client, so audio never leaves the phone and neither the Go server nor the APK shell changes. If WebView WASM speed or the per-device model download disappoints, the fallback moves transcription server-side: the phone POSTs a 16 kHz mono clip and Go execs a vendored `needle` binary, no cgo at first.

## Considered options

- Cactus Android SDK inside the APK: rejected. Breaks the ADR-0003 shell: no IPC bridge, no bundled assets, no native code.
- Cloud speech APIs: rejected. The tailnet is the boundary; audio leaves it for no one.

## Consequences

- Whistle is days old: pin model and engine versions, adapt on change.
- 30 s per-pass cap: record in windows and concatenate transcripts.
- Mic capture needs TLS; `tailscale serve` already provides it (P1).
- The fallback adds one vendored binary to the service; keep it out until WASM fails.
