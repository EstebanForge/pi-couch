// state/transcript.ts — the session view store. The server stream sends
// reset/append/status; every reduction is a pure function here so the
// logic is unit-testable without a DOM.

import type { StreamItem, TranscriptAppend, TranscriptReset, TranscriptStatus } from "../api";

export type DriverState = {
	items: StreamItem[]; // ordered by seq, tail of the active branch
	status: string; // herdr badge: working, blocked, done, idle, ""
	model: string;
	thinking: string;
	header: { name: string; first_user: string; cwd: string } | null;
};

export const emptyDriver: DriverState = { items: [], status: "", model: "", thinking: "", header: null };

export function reduceReset(_s: DriverState, p: TranscriptReset): DriverState {
	return {
		items: p.items.slice().sort((a, b) => a.seq - b.seq),
		status: p.status,
		model: p.model,
		thinking: p.thinking,
		header: p.meta ?? null,
	};
}

export function reduceAppend(s: DriverState, p: TranscriptAppend): DriverState {
	const fresh = p.items.filter((it) => !s.items.some((cur) => cur.seq === it.seq));
	if (fresh.length === 0) return s;
	return { ...s, items: [...s.items, ...fresh].sort((a, b) => a.seq - b.seq) };
}

export function reduceStatus(s: DriverState, p: TranscriptStatus): DriverState {
	return { ...s, status: p.status, model: p.model, thinking: p.thinking };
}

// Work blocks fold consecutive non-speech items (thinking, tool calls and
// results, bash, custom) into one collapsible row: the biggest scroll
// length win on a 360px phone. Speech (user/text) and markers stay solo.
export type WorkBlock =
	| { type: "item"; item: StreamItem }
	| { type: "block"; items: StreamItem[]; open: boolean };

const FOLDABLE = new Set(["thinking", "tool_call", "tool_result", "bash", "custom"]);

export function groupWorkBlocks(items: StreamItem[], agentWorking: boolean): WorkBlock[] {
	const out: WorkBlock[] = [];
	let run: StreamItem[] = [];
	const flush = () => {
		if (run.length > 0) {
			out.push({ type: "block", items: run, open: agentWorking });
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
