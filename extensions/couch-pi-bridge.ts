// couch-pi bridge: surface pi's blocking dialogs as herdr blocked state,
// and carry each dialog's kind and title to Couch Pi by direct dial so
// the phone can show which dialog pi waits on (herdr drops the label).
//
// API-only by design (ADR-0007): this extension listens to pi's typed
// ui_prompt_start / ui_prompt_end extension events, re-emits them on
// pi's event bus as the herdr:blocked event that herdr's own managed
// integration (herdr-agent-state.ts) already consumes and forwards via
// pane.report_agent, and POSTs {phase, kind, title, id, session_path} to
// Couch Pi's /api/bridge/dialog. The id is generated here per dialog so
// the phone's answer endpoint can refuse keys into a replaced dialog.
// It never mutates pi objects, never answers dialogs by itself, and
// degrades silently when events are absent.
//
// Install: symlink this file into ~/.pi/agent/extensions/.

import { readFileSync } from "node:fs";

declare const process: { env: Record<string, string | undefined> };
declare const crypto: { randomUUID?: () => string };

type PromptKind = "select" | "confirm" | "input" | "editor" | "custom";

interface PromptEvent {
	type: "ui_prompt_start" | "ui_prompt_end";
	kind: PromptKind;
	title?: string;
}

interface ExtensionContext {
	mode?: string;
	sessionManager?: { getSessionFile?: () => string | undefined };
}

interface ExtensionAPI {
	events: { emit(event: string, data: unknown): void };
	on(event: "ui_prompt_start", handler: (event: PromptEvent) => void): () => void;
	on(event: "ui_prompt_end", handler: (event: PromptEvent) => void): () => void;
	on(event: "session_start", handler: (event: unknown, ctx: ExtensionContext) => void): () => void;
}

const HERDR_ENV = process.env.HERDR_ENV;
const PANE_ID = process.env.HERDR_PANE_ID;
// Couch Pi's loopback listener. Fire-and-forget: dialogs surface when
// the server is up, and vanish by TTL when it is not.
const COUCHPI_URL = process.env.COUCHPI_URL || "http://127.0.0.1:8090";

let accessToken: string | undefined;
let tokenLookedUp = false;
let warned = false;

// The server enforces its bearer token by default; without it every
// dialog POST 401s and the bridge stays dead while looking alive. Env
// wins, then the systemd unit's 0600 env file.
function accessTokenForDial(): string {
	if (tokenLookedUp) {
		return accessToken ?? "";
	}
	tokenLookedUp = true;
	accessToken = process.env.PICOUCH_ACCESS_TOKEN;
	if (!accessToken) {
		try {
			const raw = readFileSync(`${process.env.HOME || ""}/.config/pi-couch/env`, "utf8");
			const line = raw.split("\n").find((l) => l.startsWith("PICOUCH_ACCESS_TOKEN="));
			if (line) accessToken = line.slice("PICOUCH_ACCESS_TOKEN=".length).trim();
		} catch {
			// no env file either: the dial still goes out and the warn explains
		}
	}
	return accessToken ?? "";
}

function newDialogID(): string {
	try {
		if (typeof crypto !== "undefined" && crypto.randomUUID) return crypto.randomUUID();
	} catch {
		// fall through to the counter below
	}
	return `d-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function dial(phase: "start" | "end", kind: string, title: string | undefined, sessionPath: string | undefined, id: string | undefined): void {
	const body = JSON.stringify({ phase, kind, title, id, session_path: sessionPath });
	try {
		const token = accessTokenForDial();
		fetch(`${COUCHPI_URL}/api/bridge/dialog`, {
			method: "POST",
			headers: {
				"content-type": "application/json",
				...(token ? { authorization: `Bearer ${token}` } : {}),
			},
			body,
			signal: AbortSignal.timeout(1500),
		})
			.then((res) => {
				if (!res.ok && !warned) {
					warned = true;
					console.error(`couch-pi bridge: POST rejected (HTTP ${res.status}); check PICOUCH_ACCESS_TOKEN`);
				}
			})
			.catch(() => {
				if (!warned) {
					warned = true;
					console.error("couch-pi bridge: cannot reach the server; dialogs surface only while it is up");
				}
			});
	} catch {
		// no fetch, no server, no problem: herdr blocked state still carries
	}
}

export default function couchPiBridge(pi: ExtensionAPI) {
	// Sensor only for herdr-driven TUI sessions: herdr's extension owns
	// the same gate, and dialogs never fire without a UI anyway.
	if (HERDR_ENV !== "1" || !PANE_ID) {
		return;
	}
	let tui = false;
	let depth = 0;
	let sessionPath: string | undefined;
	let dialogID: string | undefined;

	pi.on("session_start", (_event, ctx) => {
		tui = ctx?.mode === "tui";
		sessionPath = ctx?.sessionManager?.getSessionFile?.();
		// a new or resumed session owns fresh dialog state: a lost
		// ui_prompt_end must not mute every later dialog in the pane
		depth = 0;
		dialogID = undefined;
	});
	pi.on("ui_prompt_start", (event) => {
		if (!tui) {
			return;
		}
		depth += 1;
		if (depth === 1) {
			pi.events.emit("herdr:blocked", {
				active: true,
				label: event.title || event.kind,
			});
			// pi enforces one dialog at a time, so the id belongs to the
			// outermost prompt - the one the server's entry describes.
			dialogID = newDialogID();
			dial("start", event.kind, event.title, sessionPath, dialogID);
		}
	});
	pi.on("ui_prompt_end", () => {
		if (!tui || depth === 0) {
			return;
		}
		depth -= 1;
		if (depth === 0) {
			pi.events.emit("herdr:blocked", { active: false });
			dial("end", "", undefined, sessionPath, dialogID);
			dialogID = undefined;
		}
	});
}
