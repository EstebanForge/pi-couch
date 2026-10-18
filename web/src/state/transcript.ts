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
	state: "sending" | "sent" | "queued" | "unconfirmed";
};

export type DriverState = {
	items: StreamItem[]; // ordered by seq, tail of the active branch
	status: string; // herdr badge: working, blocked, done, idle, ""
	model: string;
	thinking: string;
	awaiting: boolean; // dialog chrome on the pane: pi waits on the human
	dialog: BridgeDialog | null; // bridge-reported open pi dialog (kind + title)
	todos: Todo[]; // extension todo checklist, latest state
	header: { name: string; first_user: string; cwd: string } | null;
	pending: PendingMsg[];
};

export const emptyDriver: DriverState = {
	items: [],
	status: "",
	model: "",
	thinking: "",
	awaiting: false,
	dialog: null,
	todos: [],
	header: null,
	pending: [],
};

// normalizeSeed folds whitespace so the seed matches the server's
// re-serialization of the same text.
function normalizeSeed(text: string): string {
	return text.replace(/\s+/g, " ").trim().slice(0, 60);
}

export function addPending(s: DriverState, id: number, text: string, queued: boolean): DriverState {
	const msg: PendingMsg = {
		id,
		seed: normalizeSeed(text),
		state: queued ? "queued" : "sending",
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

// reconcilePending drops bubbles whose user item arrived (text starts
// with the seed). A reset re-syncs everything, so it clears all.
function reconcile(s: DriverState): DriverState {
	if (s.pending.length === 0) return s;
	const keep = s.pending.filter(
		(p) =>
			!s.items.some(
				(it) => it.kind === "user" && normalizeSeed(it.text).startsWith(p.seed) && p.seed !== "",
			),
	);
	return keep.length === s.pending.length ? s : { ...s, pending: keep };
}

export function reduceReset(_s: DriverState, p: TranscriptReset): DriverState {
	return {
		items: p.items.slice().sort((a, b) => a.seq - b.seq),
		status: p.status,
		model: p.model,
		thinking: p.thinking,
		awaiting: p.awaiting ?? false,
		dialog: p.dialog ?? null,
		todos: p.todos ?? [],
		header: p.meta ?? null,
		pending: [],
	};
}

export function reduceAppend(s: DriverState, p: TranscriptAppend): DriverState {
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

export function reduceStatus(s: DriverState, p: TranscriptStatus): DriverState {
	// Dialog is three-way on the wire: undefined keeps, null clears
	// (pi's end event), an object sets.
	return {
		...s,
		status: p.status,
		model: p.model,
		thinking: p.thinking,
		awaiting: p.awaiting ?? false,
		dialog: p.dialog === undefined ? s.dialog : p.dialog,
		todos: p.todos ?? s.todos,
	};
}

// Work blocks fold consecutive non-speech items (thinking, tool calls and
// results, bash, custom) into one collapsible row: the biggest scroll
// length win on a 360px phone. Speech (user/text) and markers stay solo.
// Blocks render collapsed; the reader opens them on demand.
export type WorkBlock =
	| { type: "item"; item: StreamItem }
	| { type: "block"; items: StreamItem[] }
	// The Working/blocked marker rides the list as a pseudo-block so
	// follow output scrolls it into view like any other row.
	| { type: "status" };

const FOLDABLE = new Set(["thinking", "tool_call", "tool_result", "bash", "custom"]);

export function groupWorkBlocks(items: StreamItem[]): WorkBlock[] {
	const out: WorkBlock[] = [];
	let run: StreamItem[] = [];
	const flush = () => {
		if (run.length > 0) {
			out.push({ type: "block", items: run });
			run = [];
		}
	};
	for (const it of items) {
		if (FOLDABLE.has(it.kind)) {
			run.push(it);
		} else {
			flush();
			out.push({ type: "item", item: it });
		}
	}
	flush();
	return out;
}
