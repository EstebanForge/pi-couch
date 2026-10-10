# Android install surface is a thin WebView APK, not the PWA install flow

Esteban does not use a traditional Android app launcher, so the PWA install-icon flow is not a real option, and he wants no browser tabs. The Android install surface is a minimal Kotlin APK: one Activity, one WebView, opening the configured URL on launch full-screen. It is an icon and a container, nothing more: no IPC bridge, no bundled assets, no FCM. The PWA manifest stays for desktop browsers.

## Consequences

- URL is configured on first launch and persisted on device, defaulting to the tailscale serve URL.
- The Go server needs nothing new; the shell consumes the same SSE/WS endpoints as the browser.
- Native fixes (FCM push, file chooser, IME tuning) stay out until a milestone demands them.
