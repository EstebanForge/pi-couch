// Wire types for the JSON API. Mirrors the Go structs served under /api/.

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
};

export type SessionsSnapshot = {
	sessions: SessionMeta[];
	badges: Record<string, string>;
};

export async function getJSON<T>(url: string): Promise<T> {
	const res = await fetch(url, { headers: { accept: "application/json" } });
	if (!res.ok) throw new Error(`${res.status} ${url}`);
	return (await res.json()) as T;
}
