// couch-pi bridge: surface pi's blocking dialogs as herdr blocked state.
//
// API-only by design (ADR-0007): this extension listens to pi's typed
// ui_prompt_start / ui_prompt_end extension events and re-emits them on
// pi's event bus as the herdr:blocked event that herdr's own managed
// integration (herdr-agent-state.ts) already consumes and forwards via
// pane.report_agent. It never mutates pi objects, never answers
// dialogs, and degrades silently when events are absent.
//
// Install: symlink this file into ~/.pi/agent/extensions/.

declare const process: { env: Record<string, string | undefined> };

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

function dial(phase: "start" | "end", kind: string, title: string | undefined, sessionPath: string | undefined): void {
	const body = JSON.stringify({ phase, kind, title, session_path: sessionPath });
	try {
		fetch(`${COUCHPI_URL}/api/bridge/dialog`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body,
			signal: AbortSignal.timeout(1500),
		}).catch(() => {});
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

	pi.on("session_start", (_event, ctx) => {
		tui = ctx?.mode === "tui";
		sessionPath = ctx?.sessionManager?.getSessionFile?.();
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
			dial("start", event.kind, event.title, sessionPath);
		}
	});
	pi.on("ui_prompt_end", () => {
		if (!tui || depth === 0) {
			return;
		}
		depth -= 1;
		if (depth === 0) {
			pi.events.emit("herdr:blocked", { active: false });
			dial("end", "", undefined, sessionPath);
		}
	});
}
