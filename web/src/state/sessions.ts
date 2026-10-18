import { create } from "zustand";
import { getSessions, type SessionMeta, type SessionsPatch, type SessionsSnapshot } from "../api";

type Status = "idle" | "loading" | "ok" | "error";

// Priority split for the home list: sessions herdr drives (the driven
// map is keyed by session path) float above the recency list, once
// each. A driven session without a loaded meta has no card yet.
export function splitDriven(
	sessions: SessionsSnapshot["sessions"],
	driven: SessionsSnapshot["driven"],
): { priority: SessionsSnapshot["sessions"]; recents: SessionsSnapshot["sessions"] } {
	const priority: SessionsSnapshot["sessions"] = [];
	const recents: SessionsSnapshot["sessions"] = [];
	for (const s of sessions) {
		(driven[s.path] ? priority : recents).push(s);
	}
	return { priority, recents };
}

// Stream upsert frames carry unordered new-or-changed metas; the store
// list stays mtime-desc with one entry per key.
export function mergeUpserts(list: SessionMeta[], upserts: SessionMeta[]): SessionMeta[] {
	const byKey = new Map(list.map((s) => [s.key, s]));
	for (const u of upserts) byKey.set(u.key, u);
	return [...byKey.values()].sort((a, b) => Date.parse(b.mtime) - Date.parse(a.mtime));
}

export function dropSessions(list: SessionMeta[], keys: string[]): SessionMeta[] {
	if (keys.length === 0) return list;
	const gone = new Set(keys);
	return list.filter((s) => !gone.has(s.key));
}

type SessionsState = {
	sessions: SessionsSnapshot["sessions"];
	total: number;
	badges: SessionsSnapshot["badges"];
	driven: SessionsSnapshot["driven"];
	awaiting: SessionsSnapshot["awaiting"];
	status: Status;
	error: string;
	load: (opts?: { limit?: number; q?: string }) => Promise<void>;
	apply: (patch: SessionsPatch) => void;
};

// A slower earlier response must not overwrite a newer one (typing fast
// in search, or load-more racing a search).
let loadSeq = 0;

export const useSessions = create<SessionsState>((set) => ({
	sessions: [],
	total: 0,
	badges: {},
	driven: {},
	awaiting: {},
	status: "idle",
	error: "",
	load: async (opts) => {
		const seq = ++loadSeq;
		set({ status: "loading", error: "" });
		try {
			const snap = await getSessions(opts?.limit, opts?.q);
			if (seq !== loadSeq) return;
			set({
				sessions: snap.sessions,
				total: snap.total,
				badges: snap.badges,
				driven: snap.driven ?? {},
				awaiting: snap.awaiting ?? {},
				status: "ok",
			});
		} catch (e) {
			if (seq !== loadSeq) return;
			set({ status: "error", error: e instanceof Error ? e.message : "load failed" });
		}
	},
	// Stream frames are deltas: an omitted field keeps the one we hold,
	// upserts merge by key, removals drop. GET /api/sessions stays
	// whole, so load() passes a complete snapshot.
	apply: (patch) =>
		set((s) => ({
			sessions: patch.sessions
				? patch.sessions
				: mergeUpserts(dropSessions(s.sessions, patch.removed ?? []), patch.upserts ?? []),
			total: patch.total,
			badges: patch.badges ?? s.badges,
			driven: patch.driven ?? s.driven,
			awaiting: patch.awaiting ?? s.awaiting,
			status: "ok" as Status,
		})),
}));
