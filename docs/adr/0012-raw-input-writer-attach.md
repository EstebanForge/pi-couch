# ADR-0012: Raw input over the attach path

Date: 2026-11-10
Status: Accepted (Esteban, 2026-11-10: validated the kind-6 probes and approved the build order "composer paste first, editor typing second").
Amends: ADR-0011 (the no-editor-rewrite consequence), ADR-0008 findings as restated in docs/hc-findings.md. Related: ADR-0007 (bridge dialogs), docs/ui/08-terminal.md.

## Context

hc's `terminal.send` always appends Enter, and its `terminal.keys` verb only carries key names: probes against hc 0.1.19 showed that punctuation reachable through keys is a small fixed set (21 of 35 printable non-alphanumerics refused, and every shift composition arrives unshifted). Under herdr classic, pi-couch had a rewrite verb for pi's editor dialogs; ADR-0011 recorded that verb as gone, which broke two phone flows: the Terminal tab's composer box (every text send refused with 409) and phone-side editing of pi editor dialogs (commit messages and the like, accept-as-is only).

Live probes (2026-11-10, throwaway terminals, killed after) established the missing capability:

- A writer attach (`terminal.attach` with `observe:false`) replies with `automatic_input:true`, and a frame kind 6 (AUTOMATIC_INPUT) on the attach channel delivers raw bytes to the terminal's pty with no Enter appended - arbitrary printable text, multi-line drafts included.
- Observe attaches silently ignore kind 6, so the phone's read-only streams cannot inject; any raw input needs its own writer attach.
- Writer and observer coexist: an established observe stream keeps receiving output while a writer attaches, sends, and detaches, so a per-send writer attach neither kills nor starves the phone's terminal view.
- pi-tui's editor answers emacs-style keys: `ctrl+a` (line start), `ctrl+k` (delete to line end), `backspace`. One `ctrl+a ctrl+k backspace` `terminal.keys` call removes exactly one buffer line, and pi drops burst key batches, so each iteration must ride its own call with a settle.

## Decision

1. **The driver gains a Write verb.** `hc.Driver.Write` opens a short-lived writer attach per call (one dial, like every hc call in this codebase), refuses to send unless the reply advertises `automatic_input` (fail closed: a core without it drops kind 6 silently and typing would look like success while nothing lands), sends the draft as one kind 6 frame, drains and credits the core's output window until quiet (250 ms), and closes. Output frames that overtake the attach reply are credited inline; cols/rows stay zero so the core keeps its grid.

2. **The Terminal composer pastes.** The terminal input endpoint routes `text` payloads through the writer attach and `keys` payloads through `terminal.keys`. The dead herdr-era raw-text refusal dies with its web helper. hc has no attach lock and no takeover, so writer and observer coexist by construction; the composer's Enter-free delivery means a draft never submits a line by itself.

3. **Editors answer type again.** The dialog answer path's `type` verb on an editor-kind dialog rewrites pi's buffer: one clear call per painted line (`ctrl+a ctrl+k backspace`, never batched), the typed text through the writer attach, a whitespace-collapsed content match on the freshly read screen, and only then one `enter`. Scrolled editors refuse (the painted body is a tail fragment, so the clear count is unknown); buffers over 64 painted lines refuse up front so a half-cleared buffer never happens; a mismatched screen refuses without the enter, leaving pi's buffer unsubmitted for the user to cancel or accept. The card serves the box editable again, posting `type` when the text differs from the served body and `yes` (accept-as-is with content proof) when it does not. The `type` request carries the served body as `was`: proof of which buffer the user answered against, refusing a same-title successor before any clear key fires. The rewrite re-reads the screen mid-loop and before the paste, so a dialog dismissed mid-rewrite abandons the rewrite instead of leaving clears and paste in pi's idle editor.
4. **Pasted bytes are sanitized at both doors.** The composer endpoint and the dialog `type` verb refuse text carrying C0 control bytes other than newline and tab: ESC or ctrl-c riding a paste would drive pi's TUI rather than fill a buffer. A core whose attach reply lacks `automatic_input` is a permanent refusal (`hc.ErrNoAutomaticInput`, HTTP 501) so the client can tell it from a transient send failure.

5. **The dead herdr client package goes.** `internal/herdr` survived the ADR-0011 waves as an unimported leftover; ADR-0011's client-package deletion is now complete in fact.

## Consequences

- ADR-0011's "editor rewrite verb is gone" consequence is reversed: editors answer yes, escape and type under hc, with editing bounded by the content match instead of a rewrite primitive pi does not expose.
- Phone-side editor editing exists again, but only for unscrolled editors, and the card always sends the user's copy - a same-title successor buffer still refuses at the content gate.
- The composer box works under hc; the composer remains a paste-style channel and the key row remains the way to press enter, so a pasted draft sits in pi's editor unsubmitted until the user (or a dialog answer) submits it.
- Writer attaches cost one full attach handshake per send; budgets for composer submits and editor rewrites stay inside the existing dialog budget ceilings.
- pi's mid-turn paste guard was probed at commit-message sizes; the single-frame delivery keeps the 200 ms paste gap semantics hc already applies to `terminal.send`.
