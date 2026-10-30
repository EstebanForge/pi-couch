# herdr-core (hc) probe findings, verified 2026-10-30

All claims below were verified against a live sandboxed core (herdr-core 0.1.14, pi 1.4.0), not docs. The sandbox pattern: a wrapper script exports XDG_STATE_HOME, XDG_CONFIG_HOME and XDG_RUNTIME_DIR into /tmp before exec-ing the binary, so probe terminals, config and socket never touch real state. Any hc command autostarts the core; it survives between invocations; the socket lands at $XDG_RUNTIME_DIR/herdr-core/default.sock. Probe scripts live outside the repo (they were throwaway); the facts below are the durable record.

## JSON-RPC and CLI

- Plain mode is one JSON-RPC message per line over the unix socket. hello answers {core id, host name, features [framed, attach, events, status, shell], generation 1}. server.status reports holder pid and terminal count. server.stop returns {stopped:true}.
- Errors carry {code, message, data:{kind, hint}}; clients branch on data.kind, never message text.
- hc run --bg --name X returns the terminal record directly ({address, agent, cwd, name, state, turn}); hc ls lists {hidden_finished, terminals}.
- Framed mode upgrades via hello {framed:true}: 9-byte little-endian headers ([channel u32][kind u8][len u32]), kinds DATA=0, CREDIT=1, CLOSE=2, RESIZE=3, FOCUS=4, FACTS=5. Channel 0 carries JSON-RPC; attach streams get their own channels. Initial credit window is 256 KiB: the client must send CREDIT for every consumed byte or the stream stalls. A viewer over 4 MiB behind gets a full repaint as plain DATA (indistinguishable from a delta), which is why pi-couch ends a stalled attach as reset instead.

## Running pi under hc

- terminal.run spawns headless at 200x120. terminal.read returns {text, complete, how: screen|history|stitched}: clean plain-text TUI rendering, soft wraps joined.
- Inside an hc-spawned terminal, HC_TERMINAL_ID, HC_TERMINAL_SOCKET and HC_SCOPE are set (TERM_PROGRAM=ghostty on this machine). The bridge re-gates on HC_TERMINAL_ID and labels pi.session by dialing HC_TERMINAL_SOCKET.
- Startup race, live-verified: terminal.send during pi's first paint pastes the prompt but LOSES Enter (SendResult started:null, wait ends idle at turn 0). The client must wait for pi's first status report (reported flag) or retry when started is false.
- Full turn lifecycle verified: terminal.keys enter submits; terminal.send with wait:true rides a turn (started:true, working, then done at turn+1). The done status msg equals the auto session name; blocked carries the dialog title.
- Events: one plain events.subscribe connection gets a snapshot {seq, terminals} then {method:'event', params:{type, terminal, data, seq}} notifications: title {title}, status {status, records, reported}, turn {turn, state}, created/restarted (full record), labels (complete new map), finished/lost/removed. Heartbeats arrive as {method:'heartbeat', params:{seq}} every 15s.
- terminal.label round-trips from outside: setting pi.session on a terminal makes it findable via terminal.list by label. Finished terminals keep labels 24h (up to 500).

## Status semantics

- pi 1.4.0 emits OSC 7501 natively (since 1.1.0): working during runs and compaction, done/error/idle at settle, blocked while a dialog waits, with msg base64-capped at 2048 bytes. hc answers pi's support query, so reports flow with no config.
- CRITICAL: pi's blocked report does NOT fire for extension custom widgets that bypass ctx.ui. With the rpiv ask-user-question selector fully on screen, terminal.read shows the options and hint bar as clean parseable text while status stays {state:working} for 40+s. Bash tool confirms also never blocked (auto-confirms complete the turn to done). The couch-pi bridge POST remains the only dialog kind/title/session-path source under hc, unchanged from herdr.
- Dialog answering over hc works: terminal.keys enter answered the rpiv dialog and pi reached done in ~4s.
- terminal.wait returns immediately when not working and counts never-reported programs as working until exit. terminal.send/keys pace ~3s of human quiet plus a 200ms paste gap plus up to 5s settle, so herdr-era 3s client budgets are too small. terminal.interrupt sends esc for status-reporting programs (the correct pi abort). Never pass caller: hc wraps sent text in tags.
- terminal.kill is SIGKILL to the process group, not graceful; terminal.rm refuses a still-live terminal (error kind live). pi-couch closes graceful-first (ctrl+d) and escalates.

## Migration facts

- hc has no import/migrate path and a fresh state namespace; legacy herdr panes do not carry over. pi sessions need no migration: they are pi's own jsonl files and pi-couch keys everything by session file path.
- hc argv[0] template lookup hijacks a spawn whose first arg matches a user template; line mode (shell-quoted single string) avoids templates entirely. Terminal names: 1-32 chars of a-z 0-9 - _ ., starting alphanumeric, address-shaped names (t7) rejected.
- terminal.attach requires framed mode; observe:true viewers only read and never become the size leader. A finished terminal refuses attach with error kind finished (hint: hc read).
