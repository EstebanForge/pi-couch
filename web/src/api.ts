// api.ts — typed REST + JSON SSE client for Pi Couch.

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
	ctx_tokens?: number;
	model: string;
	thinking: string;
};

export type DriveInfo = {
	status: string;
	title?: string;
	label?: string;
};

export type SessionsSnapshot = {
	sessions: SessionMeta[];
	total: number;
	badges: Record<string, string>;
	driven: Record<string, DriveInfo>;
	awaiting: Record<string, boolean>;
};

// One /api/stream/sessions frame: entry deltas (upserts/removed) plus
// the matched total, with a full sessions list on connect or reset.
// Omitted fields keep the values the client already holds.
export type SessionsPatch = {
	total: number;
	sessions?: SessionMeta[];
	upserts?: SessionMeta[];
	removed?: string[];
	badges?: Record<string, string>;
	driven?: Record<string, DriveInfo>;
	awaiting?: Record<string, boolean>;
};

export type StreamItem = {
	kind:
		| "user"
		| "text"
		| "thinking"
		| "tool_call"
		| "tool_result"
		| "marker"
		| "bash"
		| "custom"
		| "error";
	text: string;
	tool_name?: string;
	is_error?: boolean;
	redacted?: boolean;
	duration_ms?: number;
	images?: number;
	/** The render path shortened this item's text; fetch the whole thing
	 * via getItemText. */
	truncated?: boolean;
	/** Entry timestamp in epoch ms; the chat ticks live elapsed time for
	 * running tools from the call's time. */
	time?: number;
	seq: number;
};

export type Todo = { id: number; subject: string; status: string; active_form?: string };

/** An open blocking pi dialog, reported by the couch-pi bridge extension. */
export type DialogOption = {
	label: string;
	description?: string;
};

export type BridgeDialog = {
	id?: string;
	kind: string;
	title: string;
	/** The dialog's own text under the title (a commit draft, the
	 * question), read off the pane. */
	body?: string;
	/** The option rows parsed off the pane, cursor included. */
	options?: DialogOption[];
	cursor?: number;
	/** True when pi's editor viewport clipped the buffer: body is the
	 * painted tail only, so the card must not prefill from it. */
	scrolled?: boolean;
};

export type TranscriptReset = {
	items: StreamItem[];
	status: string;
	model: string;
	thinking: string;
	awaiting?: boolean;
	dialog?: BridgeDialog | null;
	todos?: Todo[];
	memory?: string;
	meta?: {
		name: string;
		first_user: string;
		cwd: string;
		ctx_tokens?: number;
		total_cost?: number;
	} | null;
};

export type TranscriptAppend = { items: StreamItem[] };
export type TranscriptStatus = {
	status: string;
	model: string;
	thinking: string;
	ctx_tokens?: number;
	awaiting?: boolean;
	dialog?: BridgeDialog | null;
	todos?: Todo[];
	memory?: string;
};

export async function getSessions(limit?: number, q?: string): Promise<SessionsSnapshot> {
	const p = new URLSearchParams();
	if (limit) p.set("limit", String(limit));
	if (q) p.set("q", q);
	const qs = p.toString();
	const res = await fetch(`/api/sessions${qs ? `?${qs}` : ""}`);
	if (!res.ok) throw new Error(`sessions ${res.status}`);
	return (await res.json()) as SessionsSnapshot;
}

export async function getVersion(): Promise<string> {
	const res = await fetch("/api/version");
	if (!res.ok) throw new Error(`version ${res.status}`);
	const out = (await res.json()) as { version: string };
	return out.version;
}

// --- new agent: folder picker and create ---

export type DirsResponse = {
	path: string;
	parent?: string;
	dirs: { name: string; path: string }[];
	truncated?: boolean;
	error?: string;
};

export async function getDirs(path?: string): Promise<DirsResponse> {
	const q = path ? `?path=${encodeURIComponent(path)}` : "";
	const res = await fetch(`/api/dirs${q}`);
	if (!res.ok) throw new Error(`dirs ${res.status}`);
	return (await res.json()) as DirsResponse;
}

export async function createAgent(cwd: string, prompt: string): Promise<{ key: string }> {
	return postAgent({ cwd, prompt });
}

export async function createAgentInWorkspace(
	workspaceId: string,
	prompt: string,
): Promise<{ key: string }> {
	return postAgent({ workspace_id: workspaceId, prompt });
}

// The Quick flow: reuse the labeled workspace or create it at cwd.
// The first prompt is required, so the create answers {key} once pi
// writes its session file at turn start. model/thinking ride
// agent.start args as one pi --model flag.
export async function createQuickAgent(
	label: string,
	cwd: string,
	prompt: string,
	model?: string,
	thinking?: string,
): Promise<{ key: string }> {
	return postAgent({ label, cwd, prompt, model, thinking });
}

async function postAgent(
	body: Record<string, string | undefined>,
): Promise<{ key: string; agent?: string }> {
	const res = await fetch("/api/agents", {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify(body),
	});
	const data = (await res.json().catch(() => ({}))) as {
		key?: string;
		agent?: string;
		error?: string;
	};
	if (!res.ok) throw new Error(data.error || `create ${res.status}`);
	return { key: data.key ?? "", agent: data.agent };
}

export type Workspace = {
	workspace_id: string;
	label: string;
	number: number;
};

export async function getWorkspaces(): Promise<Workspace[]> {
	const res = await fetch("/api/workspaces");
	if (!res.ok) throw new Error(`workspaces ${res.status}`);
	return (await res.json()) as Workspace[];
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

/** One transcript item's untruncated text (tool output, bash). */
export async function getItemText(key: string, seq: number): Promise<string> {
	const res = await fetch(`/api/sessions/${encodeURIComponent(key)}/items/${seq}`);
	if (!res.ok) throw new Error(`item ${res.status}`);
	return ((await res.json()) as { text: string }).text;
}

/** One reverse-pagination page: items strictly older than `before`,
 * ascending; more says whether an older page exists. */
export async function getTranscriptPage(
	key: string,
	before: number,
	// Conversation target, not raw items: the server widens the page
	// past tool rows until it holds this many user/assistant texts.
	limit = 5,
): Promise<{ items: StreamItem[]; more: boolean }> {
	const res = await fetch(
		`/api/sessions/${encodeURIComponent(key)}/transcript?before=${before}&limit=${limit}`,
	);
	if (!res.ok) throw new Error(`transcript ${res.status}`);
	return (await res.json()) as { items: StreamItem[]; more: boolean };
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

export const sendPrompt = (key: string, text: string, whenBusy?: "followUp") =>
	postJSONBody(
		`/api/sessions/${encodeURIComponent(key)}/prompt`,
		whenBusy ? { text, whenBusy } : { text },
	);

/** Answer an open pi confirm dialog: yes presses enter, no presses
 * down+enter. ok:false carries the in-band refusal (changed, notfound,
 * send failed) and the caller hides its buttons. */
export const answerDialog = (key: string, id: string, answer: string, text?: string) =>
	postJSONBody(`/api/sessions/${encodeURIComponent(key)}/dialog/answer`, {
		id,
		answer,
		...(text !== undefined ? { text } : {}),
	});
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

export type ModelRow = { id: string; context?: number };

export async function getModels(q: string): Promise<ModelRow[]> {
	const res = await fetch(`/api/models?q=${encodeURIComponent(q)}`);
	if (!res.ok) throw new Error(`models ${res.status}`);
	return (await res.json()) as ModelRow[];
}

export async function uploadImage(file: File): Promise<string> {
	const fd = new FormData();
	fd.append("file", file);
	const res = await fetch("/api/upload", { method: "POST", body: fd });
	if (!res.ok) throw new Error(`upload ${res.status}`);
	const data = (await res.json()) as { path: string };
	return data.path;
}
