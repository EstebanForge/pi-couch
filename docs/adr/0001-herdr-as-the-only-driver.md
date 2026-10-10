# PiRemote drives pi through herdr; sessions stay plain pi sessions

PiRemote is a client, not a runtime. It starts, steers, and watches pi sessions exclusively through herdr's socket API, and it reads every session from pi's own jsonl files. A future reader will wonder why a pi client uses neither pi's RPC mode nor pi-durable: because herdr already tracks every live agent with its session file and status, exposes prompt, keys, read, and attach, and is the same surface Esteban drives from the desk, so phone and desktop share one truth and sessions resume anywhere pi runs.

## Considered options

- pi-durable harness: rejected. A second agent runtime that ignores his pi extensions and settings, splits the session format, and rebuilds tracking herdr already provides.
- pi RPC child processes per session: rejected. Typed steering is nice, but it forks the driver story, splits the terminal story, and creates sessions that live outside herdr.

## Consequences

- Steering fidelity is herdr's prompt injection into the pi TUI; steer versus follow-up is whatever pi's queue mode is set to, not a per-message toggle.
- herdr's protocol (v22 as of writing) is a pinned dependency; PiRemote adapts when it changes.
- Abort is a key event (Ctrl-C via send-keys), not an API verb.
