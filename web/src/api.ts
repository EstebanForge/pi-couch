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
	awaiting: Record<string, boolean>;
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
	awaiting?: boolean;
	meta?: { name: string; first_user: string; cwd: string } | null;
};

export type TranscriptAppend = { items: StreamItem[] };
export type TranscriptStatus = { status: string; model: string; thinking: string; awaiting?: boolean };

export async function getSessions(): Promise<SessionsSnapshot> {
	const res = await fetch("/api/sessions");
	if (!res.ok) throw new Error(`sessions ${res.status}`);
	return (await res.json()) as SessionsSnapshot;
}

// --- stage 4: changes, files, comments ---

export type ChangeFile = {
	path: string;
	staged: string;
	work: string;
	added: number;
	deleted: number;
	binary: boolean;
	label: string;
};

export type ChangesList = { repo: boolean; branch: string; files: ChangeFile[] };
export type ChangesDiff = { file: string; diff: string; truncated: boolean };

export async function getChanges(key: string): Promise<ChangesList> {
	const res = await fetch(`/api/sessions/${encodeURIComponent(key)}/changes`);
	if (!res.ok) throw new Error(`changes ${res.status}`);
	return (await res.json()) as ChangesList;
}

export async function getDiff(key: string, file: string): Promise<ChangesDiff> {
	const res = await fetch(
		`/api/sessions/${encodeURIComponent(key)}/changes?file=${encodeURIComponent(file)}`,
	);
	if (!res.ok) throw new Error(`diff ${res.status}`);
	return (await res.json()) as ChangesDiff;
}

export type FileEntry = { name: string; dir: boolean; size: number };
export type FilesList = { repo: boolean; dir: string; entries: FileEntry[] };
export type FileView = { file: string; kind: string; content: string };

export async function getFiles(key: string, dir = ""): Promise<FilesList> {
	const q = dir ? `?dir=${encodeURIComponent(dir)}` : "";
	const res = await fetch(`/api/sessions/${encodeURIComponent(key)}/files${q}`);
	if (!res.ok) throw new Error(`files ${res.status}`);
	return (await res.json()) as FilesList;
}

export async function getFileView(key: string, file: string): Promise<FileView> {
	const res = await fetch(
		`/api/sessions/${encodeURIComponent(key)}/files/view?file=${encodeURIComponent(file)}`,
	);
	if (!res.ok) throw new Error(`view ${res.status}`);
	return (await res.json()) as FileView;
}

export type SentComment = { ts: number; file: string; line: number; body: string };

export async function getSentComments(key: string): Promise<SentComment[]> {
	const res = await fetch(`/api/sessions/${encodeURIComponent(key)}/comments`);
	if (!res.ok) throw new Error(`comments ${res.status}`);
	const data = (await res.json()) as { sent: SentComment[] };
	return data.sent ?? [];
}

export type PendingComment = { file: string; line: number; text: string };

// Send-all posts the JSON twin under /api.
export async function sendAllComments(
	key: string,
	comments: PendingComment[],
): Promise<{ ok: boolean; error?: string }> {
	const res = await fetch(`/api/sessions/${encodeURIComponent(key)}/comments`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({ comments }),
	});
	let body: { ok?: boolean; error?: string } = {};
	try {
		body = (await res.json()) as typeof body;
	} catch {
		// empty body: the status decides
	}
	if (!res.ok) return { ok: false, error: body.error ?? `http ${res.status}` };
	return { ok: true };
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
// Raw text into the TUI (editor buffer, menus); pi does not see it as a
// prompt and it never submits a line by itself (spike-proven).
export const sendTextInput = (key: string, text: string) =>
	postJSONBody(`/api/sessions/${encodeURIComponent(key)}/terminal/input`, { text });

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
