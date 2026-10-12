import { create } from "zustand";
import { getJSON, type SessionsSnapshot } from "../api";

type Status = "idle" | "loading" | "ok" | "error";

type SessionsState = {
	sessions: SessionsSnapshot["sessions"];
	badges: SessionsSnapshot["badges"];
	status: Status;
	error: string;
	load: () => Promise<void>;
};

export const useSessions = create<SessionsState>((set) => ({
	sessions: [],
	badges: {},
	status: "idle",
	error: "",
	load: async () => {
		set({ status: "loading", error: "" });
		try {
			const snap = await getJSON<SessionsSnapshot>("/api/sessions");
			set({
				sessions: snap.sessions,
				badges: snap.badges,
				status: "ok",
			});
		} catch (e) {
			set({ status: "error", error: e instanceof Error ? e.message : "load failed" });
		}
	},
}));
