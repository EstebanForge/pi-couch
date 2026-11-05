# Mobile browser E2E

Run `just test-e2e`, or `npm run test:e2e` under `web/`. The command builds the production SPA, starts an isolated synthetic API, drives system Chrome or Chromium through the DevTools protocol, and uses Node's built-in test runner. No npm test dependency or live agent is required.

The browser uses a 393×873 Pixel-class mobile viewport, device scale factor 2, touch emulation, and an Android user agent. Tests dispatch real touch and keyboard input and assert navigation, request payloads, visibility, composer behavior, and horizontal overflow. The seven scenarios cover home/session navigation, structured option answers, custom answers, cancellation, TODO clearing, multiline touch composition, and quick-create failure/retry.

Screenshots and the Chrome log land in `web/test-results/mobile/`. Set `E2E_ARTIFACTS` to change that directory and `CHROME_BIN` when the browser is not discoverable as `google-chrome`, `google-chrome-stable`, `chromium`, or `chromium-browser`.

The API fixture mirrors the wire contract while the existing Go tests own server behavior. This suite deliberately tests the production browser bundle rather than duplicating those endpoint tests.

Headless Chromium cannot prove Gboard behavior, Android WebView differences, Bluetooth audio routing, or real background/foreground lifecycle. Those still need a hardware pass. Run a focused test with `node --test --test-concurrency=1 --test-name-pattern='quick-create' e2e/mobile.mjs` after the SPA build.
