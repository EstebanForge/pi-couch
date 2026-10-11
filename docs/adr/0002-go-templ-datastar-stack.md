# Go server, templ and Datastar UI, no web framework, no JS build

Couch Pi's server is a streams daemon (jsonl tails, herdr socket, git exec, push signing) with almost no business logic, and its client must stay small on a phone. We chose Go compiled to one binary, templ for type-safe HTML, the official Datastar Go SDK over SSE for transcript, status, and comment updates, vendored xterm.js over one WebSocket for the terminal, and vanilla TS islands for the composer. No web framework: stdlib net/http routing covers a dozen routes. A web push library and fsnotify cover the rest.

Rejected: Bun or Node on the server (Esteban refuses server JS), React or SPA stacks (unneeded client weight and a build pipeline for a hypermedia product), Rust (slowest iteration for tolerant schema-less jsonl parsing, overkill for a single-user daemon), the gogogo scaffold (CRUD-shaped: auth, jobs, workflows, CRDT; we would trim most of it), and an irgo APK (the server cannot run on the phone; it must reach session files, herdr's socket, git, and PTYs, so an APK is just another WebView on the same PWA URL).

## Consequences

- Zero node_modules on the server and no client framework; templ codegen and the esbuild TS bundle are the only build steps.
- daisyUI 5 ships as CDN links in each page head (`daisyui.css`, one theme file, `@tailwindcss/browser@4`), loaded before `app.css`. A compiled Tailwind v4 + daisyUI build is deliberately not adopted; Esteban rejected compilation outright (2026-10-11, "compile never"). Revisit only if he raises it.
- If Datastar fights live transcript streaming, the escape hatch is plain SSE with the same Go handlers; no rework.
- The Android install surface is the thin WebView APK (ADR-0003), which supersedes the parked PWA-install path; the PWA manifest stays for desktop browsers.
