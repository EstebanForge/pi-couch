// state/transcript.ts — the session view store. The server stream sends
// reset/append/status; every reduction is a pure function here so the
// logic is unit-testable without a DOM.

import type {
	BridgeDialog,
	StreamItem,
	Todo,
	TranscriptAppend,
	TranscriptReset,
	TranscriptStatus,
} from "../api";

export type PendingMsg = {
	id: number;
	seed: string; // first 60 normalized chars, matched against arriving user items
	text: string; // full text, so the bubble renders the same box as the real row
	state: "sending" | "sent" | "queued" | "unconfirmed";
	at: number; // creation epoch ms; 0 marks legacy entries, which expire
};

// A queue drains within a turn or two; reconcile only sees the in-memory
// tail (40 items on connect), so a delivery missed while dozed can never
// match its item and the bubble would stick forever. Age retires it.
export const PENDING_TTL_MS = 30 * 60 * 1000;

export type DriverState = {
	items: StreamItem[]; // ordered by seq, tail of the active branch
	status: string; // herdr badge: working, blocked, done, idle, ""
	model: string;
	thinking: string;
	ctx: number; // newest turn's usage total, for the context fill
	memory: string | null; // agent memory server health class
	awaiting: boolean; // dialog chrome on the pane: pi waits on the human
	dialog: BridgeDialog | null; // bridge-reported open pi dialog (kind + title)
	todos: Todo[]; // extension todo checklist, latest state
	header: {
		name: string;
		first_user: string;
		cwd: string;
		ctx_tokens?: number;
		total_cost?: number;
	} | null;
	pending: PendingMsg[];
};

export const emptyDriver: DriverState = {
	items: [],
	status: "",
	model: "",
	thinking: "",
	ctx: 0,
	memory: null,
	awaiting: false,
	dialog: null,
	todos: [],
	header: null,
	pending: [],
};

// normalizeSeed folds whitespace so the seed matches the server's
// re-serialization of the same text.
// normalizeSeed folds whitespace so the seed matches the server's
// rendering; the match side uses the untruncated fold, because pi can
// prepend a whole skill block before the typed text.
function foldWs(text: string): string {
	return text.replace(/\s+/g, " ").trim();
}

function normalizeSeed(text: string): string {
	return foldWs(text).slice(0, 60);
}

export function addPending(
	s: DriverState,
	id: number,
	text: string,
	queued: boolean,
	at = Date.now(),
): DriverState {
	const msg: PendingMsg = {
		id,
		seed: normalizeSeed(text),
		text,
		state: queued ? "queued" : "sending",
		at,
	};
	return { ...s, pending: [...s.pending, msg] };
}

export function markPending(s: DriverState, id: number, state: PendingMsg["state"]): DriverState {
	return {
		...s,
		pending: s.pending.map((p) => (p.id === id ? { ...p, state } : p)),
	};
}

export function removePending(s: DriverState, id: number): DriverState {
	return { ...s, pending: s.pending.filter((p) => p.id !== id) };
}

// reconcilePending drops bubbles whose user item arrived. The landed
// text must CONTAIN the seed, not start with it: pi expands $skill
// invocations by prepending the <skill> block to the typed text, so a
// seeded draft no longer prefixes the landed message (stuck-bubble
// field bug). A reset re-syncs everything, so it clears all.
function reconcile(s: DriverState): DriverState {
	if (s.pending.length === 0) return s;
	const keep = s.pending.filter(
		(p) =>
			Date.now() - p.at < PENDING_TTL_MS &&
			!s.items.some(
				(it) => it.kind === "user" && p.seed !== "" && foldWs(it.text).includes(p.seed),
			),
	);
	return keep.length === s.pending.length ? s : { ...s, pending: keep };
}

export function reduceReset(_s: DriverState, p: TranscriptReset): DriverState {
	// A queued message is not in the transcript yet, and resets arrive on
	// every reconnect and chat reopen — wiping pending here made queued
	// messages vanish from the view while pi still held them (field
	// bug). Reconcile instead: landed bubbles drop, queued ones survive.
	return reconcile({
		items: p.items.slice().sort((a, b) => a.seq - b.seq),
		status: p.status,
		model: p.model,
		thinking: p.thinking,
		awaiting: p.awaiting ?? false,
		dialog: p.dialog ?? null,
		todos: p.todos ?? [],
		ctx: p.meta?.ctx_tokens ?? 0,
		memory: p.memory ?? null,
		header: p.meta ?? null,
		pending: _s.pending,
	});
}

export function reduceAppend(s: DriverState, p: TranscriptAppend): DriverState {
	if (p.items.length === 0) return s;
	// Appends are new tail entries in practice; only a raced page fetch
	// or a reset gap ever needs the dedupe+sort fallback.
	const last = s.items.length > 0 ? s.items[s.items.length - 1].seq : -1;
	let ascending = p.items[0].seq > last;
	for (let i = 1; ascending && i < p.items.length; i++) {
		if (p.items[i].seq <= p.items[i - 1].seq) ascending = false;
	}
	if (ascending) {
		return reconcile({ ...s, items: s.items.concat(p.items) });
	}
	const fresh = p.items.filter((it) => !s.items.some((cur) => cur.seq === it.seq));
	if (fresh.length === 0) return s;
	return reconcile({ ...s, items: [...s.items, ...fresh].sort((a, b) => a.seq - b.seq) });
}

/** Prepend one fetched history page: dedupe by seq (an SSE append can
 * race the page fetch) with the live copy winning, ascending out. */
export function mergeOlder(items: StreamItem[], older: StreamItem[]): StreamItem[] {
	const known = new Map(items.map((it) => [it.seq, it]));
	for (const it of older) {
		if (!known.has(it.seq)) known.set(it.seq, it);
	}
	return [...known.values()].sort((a, b) => a.seq - b.seq);
}

export type ToolPair = { call: StreamItem | null; outs: StreamItem[] };

// pairTools joins each tool_call with the result items that follow it,
// so the transcript renders one titled row per tool with its output
// nested underneath. Results before any call — a group-edge leftover
// from loadOlder windows — ride a call-less pair instead of vanishing.
export function pairTools(items: StreamItem[]): ToolPair[] {
	const pairs: ToolPair[] = [];
	for (const it of items) {
		if (it.kind === "tool_call") pairs.push({ call: it, outs: [] });
		else if (pairs.length === 0) pairs.push({ call: null, outs: [it] });
		else pairs[pairs.length - 1].outs.push(it);
	}
	return pairs;
}

export function reduceStatus(s: DriverState, p: TranscriptStatus): DriverState {
	// Dialog is three-way on the wire: undefined keeps, null clears
	// (pi's end event), an object sets.
	return {
		...s,
		status: p.status,
		model: p.model,
		thinking: p.thinking,
		ctx: p.ctx_tokens ?? s.ctx,
		memory: p.memory ?? s.memory,
		awaiting: p.awaiting ?? false,
		dialog: p.dialog === undefined ? s.dialog : p.dialog,
		todos: p.todos ?? s.todos,
	};
}

// Work blocks fold consecutive non-speech items (tool calls and results,
// bash, custom) into one collapsible row: the biggest scroll length win
// on a 360px phone. Thinking stays solo — each chunk is its own collapsed
// card so the reader can open exactly the reasoning they want. Speech
// (user/text) and markers stay solo. Blocks render collapsed; the reader
// opens them on demand.
export type WorkBlock =
	| { type: "item"; item: StreamItem }
	| { type: "block"; items: StreamItem[] }
	// Consecutive thinking chunks fold into one toggleable group, kept
	// apart from the tool runs so each collapses independently.
	| { type: "thinking"; items: StreamItem[] }
	// The Working/blocked marker rides the list as a pseudo-block so
	// follow output scrolls it into view like any other row. Fields
	// inline so rows memoize without a DriverState prop.
	| {
			type: "status";
			dialog: DriverState["dialog"];
			awaiting: boolean;
			status: string;
	  }
	// Same for the pending sent bubble: as a sibling div it resizes
	// the scroller twice per send (insert, then reconcile removal),
	// which read as a clunky jump on every message.
	| { type: "pending"; msg: PendingMsg };

const FOLDABLE = new Set(["tool_call", "tool_result", "bash", "custom"]);

export function groupWorkBlocks(items: StreamItem[]): WorkBlock[] {
	const out: WorkBlock[] = [];
	let run: StreamItem[] = [];
	let kind: "tool" | "thinking" | null = null;
	const flush = () => {
		if (run.length > 0) {
			out.push(
				kind === "thinking" ? { type: "thinking", items: run } : { type: "block", items: run },
			);
			run = [];
		}
	};
	for (const it of items) {
		const k = FOLDABLE.has(it.kind) ? "tool" : it.kind === "thinking" ? "thinking" : null;
		if (k === null) {
			flush();
			kind = null;
			out.push({ type: "item", item: it });
		} else {
			if (kind !== null && k !== kind) flush();
			kind = k;
			run.push(it);
		}
	}
	flush();
	return out;
}
