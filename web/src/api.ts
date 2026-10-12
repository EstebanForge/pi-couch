// api.ts — typed REST + JSON SSE client for Couch Pi.

export type SessionMeta = {
	key: string;
	path: string;
	id: string;
	cwd: string;
	name: string;
	first_user: string;
	mtime: string;
	version: number;
	entries: number;
	total_tokens: number;
	total_cost: number;
	model: string;
	thinking: string;
};

export type SessionsSnapshot = {
	sessions: SessionMeta[];
	badges: Record<string, string>;
};

export type StreamItem = {
	kind: "user" | "text" | "thinking" | "tool_call" | "tool_result" | "marker" | "bash" | "custom";
	text: string;
	tool_name?: string;
	is_error?: boolean;
	redacted?: boolean;
	duration_ms?: number;
	images?: number;
	seq: number;
};

export type TranscriptReset = {
	items: StreamItem[];
	status: string;
	model: string;
	thinking: string;
	meta?: { name: string; first_user: string; cwd: string } | null;
};

export type TranscriptAppend = { items: StreamItem[] };
export type TranscriptStatus = { status: string; model: string; thinking: string };

export async function getSessions(): Promise<SessionsSnapshot> {
	const res = await fetch("/api/sessions");
	if (!res.ok) throw new Error(`sessions ${res.status}`);
	return (await res.json()) as SessionsSnapshot;
}

async function postJSON(path: string): Promise<{ ok: boolean; error?: string }> {
	const res = await fetch(path, { method: "POST" });
	let body: { ok?: boolean; error?: string } = {};
	try {
		body = (await res.json()) as typeof body;
	} catch {
		// empty or non-JSON body: fall through on status alone
	}
	if (!res.ok) return { ok: false, error: body.error ?? `http ${res.status}` };
	return { ok: true };
}

export const stopSession = (key: string) =>
	postJSON(`/api/sessions/${encodeURIComponent(key)}/stop`);
export const closeSession = (key: string) =>
	postJSON(`/api/sessions/${encodeURIComponent(key)}/close`);
