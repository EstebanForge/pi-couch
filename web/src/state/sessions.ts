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
	awaiting: SessionsSnapshot["awaiting"];
	status: Status;
	error: string;
	load: () => Promise<void>;
	apply: (snap: SessionsSnapshot) => void;
};

export const useSessions = create<SessionsState>((set) => ({
	sessions: [],
	badges: {},
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
				awaiting: snap.awaiting ?? {},
				status: "ok",
			});
		} catch (e) {
			set({ status: "error", error: e instanceof Error ? e.message : "load failed" });
		}
	},
	apply: (snap) =>
		set({
			sessions: snap.sessions,
			badges: snap.badges,
			awaiting: snap.awaiting ?? {},
			status: "ok",
		}),
}));
