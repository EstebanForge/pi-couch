# Android install surface is a thin WebView APK, not the PWA install flow

Esteban does not use a traditional Android app launcher, so the PWA install-icon flow is not a real option, and he wants no browser tabs. The Android install surface is a minimal Kotlin APK: one Activity, one WebView, opening the configured URL on launch full-screen. It is an icon and a container, nothing more: no IPC bridge, no bundled assets, no FCM. The PWA manifest stays for desktop browsers.

## Consequences

- URL is configured on first launch and persisted on device, defaulting to the tailscale serve URL.
- The Go server needs nothing new; the shell consumes the same SSE/WS endpoints as the browser.
- Native fixes (FCM push, file chooser, IME tuning) stay out until a milestone demands them.
- Edge-to-edge at targetSdk 35: the WebView stays full-bleed under a transparent status bar so the page background flows behind it, and the shell injects the real window insets as one-way CSS variables (`--safe-top`, `--safe-bottom`) on page load. Chromium WebView keeps `env(safe-area-inset-*)` at 0, so pages cannot read the inset themselves; the variables are styling data, not an IPC bridge. The page halves the top inset (Esteban's preference) and keeps the full bottom gesture inset.
- Branding: the Pi Agent mark is an adaptive launcher icon (vector foreground on the dark background) and the Android 12+ system splash shows the same glyph, so the launch screen dissolves into the dark app.
