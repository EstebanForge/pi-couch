# ADR-0006: React SPA client supersedes the templ and Datastack stack

Date: 2026-10-13. Status: accepted. Supersedes ADR-0002.

## Context

ADR-0002 locked the web client to server-rendered templ pages, Datastar
SSE patches, and vanilla TS islands with a no-node-on-the-server rule.
It was written before the core steering path ever worked on a phone.
Live use through the M3-M5 era proved its premises wrong: the browser
JIT'd Tailwind from a CDN on every streamed patch, whole-page form
posts made steering feel broken, and full-tail DOM re-renders froze
phones mid-turn. Fixing those one patch at a time was losing ground
(2026-10-12: "clunky, slow, lags, not fluid as a proper web app").

## Decision

The client is a React 19 + Vite + Tailwind 4 + Konsta single-page app,
built in web/ and served from the Go binary via go:embed. React
Virtualization via react-virtuoso carries the transcript; zustand holds
client state; the server exposes JSON feeds (/api/sessions,
/api/stream/*) and JSON commands instead of HTML fragments.

Consequences:

- node is a build-time dependency, not a server dependency. The binary
  still embeds everything; deploys still copy one file.
- The templ pages, the Datastar Go SDK and browser bundle, the daisyUI
  CDN links, the esbuild TS bundle, and the /stream/* datastar routes
  are deleted. JSON twins replaced the form-post endpoints.
- konsta/react and @xterm/xterm come from npm; xterm is no longer
  vendored.
- The steering POSTs ride fetch with an optimistic pending bubble;
  pi's queue semantics surface as bubble states, not page reloads.
- The APK shell (ADR-0003) is unchanged: it loads the same URL and
  renders whatever the server serves.
