import { create } from "zustand";
import { getSessions, type SessionsSnapshot } from "../api";

type Status = "idle" | "loading" | "ok" | "error";

// Active-first grouping, the templ list's rule: working and blocked
// float above the rest, recency order holds inside both groups (the
// snapshots arrive mtime-desc, so a stable partition is enough). The
// badges map is keyed by session path (the herdr agent's session file),
// not by key.
export function grouped(
	sessions: SessionsSnapshot["sessions"],
	badges: SessionsSnapshot["badges"],
): SessionsSnapshot["sessions"] {
	const actives: SessionsSnapshot["sessions"] = [];
	const rest: SessionsSnapshot["sessions"] = [];
	for (const s of sessions) {
		const st = badges[s.path] ?? "unknown";
		(st === "working" || st === "blocked" ? actives : rest).push(s);
	}
	return [...actives, ...rest];
}

type SessionsState = {
	sessions: SessionsSnapshot["sessions"];
	badges: SessionsSnapshot["badges"];
	driven: SessionsSnapshot["driven"];
	awaiting: SessionsSnapshot["awaiting"];
	status: Status;
	error: string;
	load: () => Promise<void>;
	apply: (snap: Partial<SessionsSnapshot>) => void;
};

export const useSessions = create<SessionsState>((set) => ({
	sessions: [],
	badges: {},
	driven: {},
	awaiting: {},
	status: "idle",
	error: "",
	load: async () => {
		set({ status: "loading", error: "" });
		try {
			const snap = await getSessions();
			set({
				sessions: snap.sessions,
				badges: snap.badges,
				driven: snap.driven ?? {},
				awaiting: snap.awaiting ?? {},
				status: "ok",
			});
		} catch (e) {
			set({ status: "error", error: e instanceof Error ? e.message : "load failed" });
		}
	},
	// Stream frames are field deltas: an omitted field keeps the one we
	// hold, so a file tick resends sessions alone without touching the
	// herdr maps. GET /api/sessions stays whole, so load() passes a
	// complete snapshot.
	apply: (snap) =>
		set((s) => ({
			sessions: snap.sessions ?? s.sessions,
			badges: snap.badges ?? s.badges,
			driven: snap.driven ?? s.driven,
			awaiting: snap.awaiting ?? s.awaiting,
			status: "ok" as Status,
		})),
}));
