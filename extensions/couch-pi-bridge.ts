// couch-pi bridge: surface pi's blocking dialogs as herdr blocked state,
// and carry each dialog's kind and title to Couch Pi by direct dial so
// the phone can show which dialog pi waits on (herdr drops the label).
// The rpiv ask_user_question questionnaire rides a second relay
// (ADR-0009): its rpiv:ask-user:prompt event carries the questions, and
// the inbound rpiv:ask-user:answer event lets the phone answer without
// a keystroke; this extension polls Couch Pi for pending answers while
// a questionnaire is open and re-emits them on pi's event bus.
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
import { connect } from "node:net";

declare const process: { env: Record<string, string | undefined> };
declare const crypto: { randomUUID?: () => string };

type PromptKind = "select" | "confirm" | "input" | "editor" | "custom";

interface PromptEvent {
	type: "ui_prompt_start" | "ui_prompt_end";
	kind: PromptKind;
	title?: string;
}

interface AskQuestion {
	question: string;
	header?: string;
	multiSelect?: boolean;
	options?: Array<{ label: string; description?: string }>;
}

interface AskPromptEvent {
	toolCallId?: string;
	questions?: AskQuestion[];
}

interface AskAnswerResultEvent {
	toolCallId?: string;
	requestId?: string;
	ok?: boolean;
	code?: string;
	reason?: string;
}

interface ExtensionContext {
	mode?: string;
	sessionManager?: { getSessionFile?: () => string | undefined };
}

interface ExtensionAPI {
	events: {
		emit(event: string, data: unknown): void;
		on(event: string, handler: (data: unknown) => void): () => void;
	};
	on(event: "ui_prompt_start", handler: (event: PromptEvent) => void): () => void;
	on(event: "ui_prompt_end", handler: (event: PromptEvent) => void): () => void;
	on(event: "session_start", handler: (event: unknown, ctx: ExtensionContext) => void): () => void;
}

const HERDR_ENV = process.env.HERDR_ENV;
const PANE_ID = process.env.HERDR_PANE_ID;
// herdr-core (hc) presents itself through a different gate: the
// terminal's id and socket ride the environment instead (ADR-0008).
const HC_TERMINAL_ID = process.env.HC_TERMINAL_ID;
const HC_TERMINAL_SOCKET = process.env.HC_TERMINAL_SOCKET;
const underHerdr = HERDR_ENV === "1" && !!PANE_ID;
const underHC = !!HC_TERMINAL_ID && !!HC_TERMINAL_SOCKET;
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

// Ask relay (ADR-0009): the extension polls Couch Pi for a pending
// phone answer while a questionnaire is open and re-emits it on pi's
// event bus. One poll tick of latency per answer.
const ASK_POLL_MS = 700;

// One fire-and-forget POST path for dialogs and the ask relay.
// Dialogs surface while the server is up; failures warn once.
function postBridge(path: string, body: unknown): void {
	try {
		fetch(`${COUCHPI_URL}${path}`, {
			method: "POST",
			headers: {
				"content-type": "application/json",
				...authHeaders(),
			},
			body: JSON.stringify(body),
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

// One poll tick: the pending phone answer, or undefined when the
// server has none (204) or cannot be reached.
async function fetchAskAnswer(toolCallID: string): Promise<unknown> {
	const res = await fetch(`${COUCHPI_URL}/api/bridge/ask/answer?tool_call_id=${encodeURIComponent(toolCallID)}`, {
		headers: authHeaders(),
		signal: AbortSignal.timeout(1500),
	});
	if (res.status !== 200) return undefined;
	try {
		return await res.json();
	} catch {
		return undefined;
	}
}

function authHeaders(): Record<string, string> {
	const token = accessTokenForDial();
	return token ? { authorization: `Bearer ${token}` } : {};
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
	postBridge("/api/bridge/dialog", { phase, kind, title, id, session_path: sessionPath });
}

let labelWarned = false;
function warnLabel(): void {
	if (labelWarned) return;
	labelWarned = true;
	console.error("couch-pi bridge: cannot reach the hc socket; the pi.session label stays stale until the next session start");
}

// Under hc the terminal finds its session by the pi.session label (the
// pane-id scheme does not exist there), so every session_start
// relabels, covering /new and /resume too.
function labelTerminal(sessionPath: string | undefined): void {
	if (!sessionPath || !HC_TERMINAL_SOCKET || !HC_TERMINAL_ID) return;
	try {
		const socket = connect(HC_TERMINAL_SOCKET);
		const payload = `${JSON.stringify({
			jsonrpc: "2.0",
			id: 1,
			method: "terminal.label",
			params: { terminal: HC_TERMINAL_ID, labels: { "pi.session": sessionPath } },
		})}\n`;
		let buf = "";
		socket.setTimeout(2000, () => socket.destroy());
		socket.on("connect", () => socket.write(payload));
		socket.on("data", (chunk: Buffer) => {
			buf += chunk.toString();
			if (buf.includes("\n")) socket.destroy();
		});
		socket.on("error", warnLabel);
	} catch {
		warnLabel();
	}
}

export default function couchPiBridge(pi: ExtensionAPI) {
	// Sensor only for driver-run TUI sessions: herdr's extension owns
	// the same gate, and dialogs never fire without a UI anyway.
	if (!underHerdr && !underHC) {
		return;
	}
	let tui = false;
	let depth = 0;
	let sessionPath: string | undefined;
	let dialogID: string | undefined;
	// Ask relay state: one open questionnaire per pane (the latest
	// prompt wins), the pending poll timer, and the request id of the
	// answer this extension last delivered from the phone.
	let askID: string | undefined;
	let askPoll: ReturnType<typeof setTimeout> | undefined;
	let askRequestID: string | undefined;

	function endAsk(): void {
		const id = askID;
		askID = undefined;
		askRequestID = undefined;
		if (askPoll) {
			clearTimeout(askPoll);
			askPoll = undefined;
		}
		if (!id) return;
		postBridge("/api/bridge/ask", { phase: "close", tool_call_id: id, session_path: sessionPath });
	}

	function armAskPoll(): void {
		if (askPoll) return;
		const tick = (): void => {
			askPoll = undefined;
			const id = askID;
			if (!id) return;
			fetchAskAnswer(id)
				.then((payload) => {
					if (!payload || askID !== id) return;
					const reqID = (payload as { request_id?: unknown }).request_id;
					askRequestID = typeof reqID === "string" ? reqID : undefined;
					pi.events.emit("rpiv:ask-user:answer", payload);
				})
				.catch(() => {
					// server down: keep ticking, the dialog outlives the outage
				})
				.finally(() => {
					if (askID === id) askPoll = setTimeout(tick, ASK_POLL_MS);
				});
		};
		askPoll = setTimeout(tick, ASK_POLL_MS);
	}

	pi.on("session_start", (_event, ctx) => {
		tui = ctx?.mode === "tui";
		sessionPath = ctx?.sessionManager?.getSessionFile?.();
		// a new or resumed session owns fresh dialog state: a lost
		// ui_prompt_end must not mute every later dialog in the pane
		depth = 0;
		dialogID = undefined;
		endAsk();
		// A non-TUI child inherits HC_TERMINAL_*: it must not steal
		// the parent's session label.
		if (underHC && tui) {
			labelTerminal(sessionPath);
		}
	});
	pi.on("ui_prompt_start", (event) => {
		if (!tui) {
			return;
		}
		depth += 1;
		if (depth === 1) {
			// herdr's managed integration is the only herdr:blocked
			// consumer; hc learns blocked from pi's own OSC 7501
			if (underHerdr) {
				pi.events.emit("herdr:blocked", {
					active: true,
					label: event.title || event.kind,
				});
			}
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
			if (underHerdr) {
				pi.events.emit("herdr:blocked", { active: false });
			}
			dial("end", "", undefined, sessionPath, dialogID);
			dialogID = undefined;
			// A lost rpiv blocked:false must not strand the poll; endAsk
			// no-ops when no questionnaire is open.
			endAsk();
		}
	});

	// rpiv ask_user_question relay (ADR-0009). The prompt event carries
	// the questions; the inbound answer event answers without keys; the
	// result event carries the verdict back to Couch Pi. These are
	// cross-extension event-bus channels, NOT pi lifecycle events:
	// pi.on only registers lifecycle handlers, so they must ride
	// pi.events.on or they never fire.
	pi.events.on("rpiv:ask-user:prompt", (data) => {
		if (!tui) return;
		const event = data as AskPromptEvent | null;
		const id = event?.toolCallId;
		const questions = event?.questions;
		if (typeof id !== "string" || !Array.isArray(questions)) return;
		askID = id;
		askRequestID = undefined;
		postBridge("/api/bridge/ask", {
			phase: "open",
			tool_call_id: id,
			session_path: sessionPath,
			questions,
		});
		armAskPoll();
	});
	pi.events.on("rpiv:ask-user:blocked", (data) => {
		if (!tui) return;
		const event = data as { active?: boolean; toolCallId?: string } | null;
		if (event?.active) return;
		if (event?.toolCallId && event.toolCallId !== askID) return;
		endAsk();
	});
	pi.events.on("rpiv:ask-user:answer-result", (data) => {
		if (!tui) return;
		const event = data as AskAnswerResultEvent | null;
		if (!event || typeof event.toolCallId !== "string") return;
		if (askID && event.toolCallId !== askID) return;
		if (!event.requestId || (askRequestID && event.requestId !== askRequestID)) return;
		postBridge("/api/bridge/ask/verdict", {
			request_id: event.requestId,
			tool_call_id: event.toolCallId,
			ok: !!event.ok,
			...(event.code ? { code: event.code } : {}),
			...(event.reason ? { reason: event.reason } : {}),
		});
	});
}
