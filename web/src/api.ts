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

async function postJSONBody(path: string, body: unknown): Promise<{ ok: boolean; error?: string }> {
	const res = await fetch(path, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(body),
	});
	let parsed: { ok?: boolean; error?: string } = {};
	try {
		parsed = (await res.json()) as typeof parsed;
	} catch {
		// 204 and other empty bodies: the status decides
	}
	if (!res.ok) return { ok: false, error: parsed.error ?? `http ${res.status}` };
	return { ok: true };
}

export const sendPrompt = (key: string, text: string) =>
	postJSONBody(`/api/sessions/${encodeURIComponent(key)}/prompt`, { text });
export const setModel = (key: string, pattern: string) =>
	postJSONBody(`/api/sessions/${encodeURIComponent(key)}/model`, { pattern });
export const setThinking = (key: string, level: string) =>
	postJSONBody(`/api/sessions/${encodeURIComponent(key)}/thinking`, { level });
export const sendKeys = (key: string, keys: string[]) =>
	postJSONBody(`/api/sessions/${encodeURIComponent(key)}/terminal/input`, { keys });

export async function getModels(q: string): Promise<string[]> {
	const res = await fetch(`/api/models?q=${encodeURIComponent(q)}`);
	if (!res.ok) throw new Error(`models ${res.status}`);
	const rows = (await res.json()) as { id: string }[];
	return rows.map((r) => r.id);
}

export async function uploadImage(file: File): Promise<string> {
	const fd = new FormData();
	fd.append("file", file);
	const res = await fetch("/api/upload", { method: "POST", body: fd });
	if (!res.ok) throw new Error(`upload ${res.status}`);
	const data = (await res.json()) as { path: string };
	return data.path;
}
