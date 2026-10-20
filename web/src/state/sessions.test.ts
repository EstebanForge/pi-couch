import { describe, expect, it } from "vitest";
import type { SessionMeta } from "../api";
import { dropSessions, mergeUpserts, splitDriven } from "./sessions";

const meta = (key: string, mtime: string): SessionMeta => ({
	key,
	path: `/p/${key}`,
	id: key,
	cwd: "/tmp",
	name: key,
	first_user: "",
	mtime,
	version: 3,
	entries: 1,
	total_tokens: 0,
	total_cost: 0,
	model: "",
	thinking: "",
});

const keys = (rows: SessionMeta[]) => rows.map((s) => s.key);

// Stream upsert frames carry unordered new-or-changed metas; the store
// list stays mtime-desc with one entry per key.
describe("splitDriven", () => {
	it("lifts driven sessions into priority, mtime desc, without repeats", () => {
		const list = [
			meta("a", "2026-10-17T12:00:00Z"),
			meta("b", "2026-10-17T11:00:00Z"),
			meta("c", "2026-10-17T10:00:00Z"),
		];
		const { priority, recents } = splitDriven(list, { "/p/b": { status: "working" } });
		expect(keys(priority)).toEqual(["b"]);
		expect(keys(recents)).toEqual(["a", "c"]);
	});

	it("keeps everything in recents when nothing is driven", () => {
		const list = [meta("a", "2026-10-17T12:00:00Z")];
		const { priority, recents } = splitDriven(list, {});
		expect(keys(priority)).toEqual([]);
		expect(keys(recents)).toEqual(["a"]);
	});

	it("keeps driven rows outside the loaded window out of recents only by presence", () => {
		// a driven session whose meta is not loaded yet simply has no card
		const list = [meta("a", "2026-10-17T12:00:00Z")];
		const { priority, recents } = splitDriven(list, {
			"/p/ghost": { status: "working" },
			"/p/a": { status: "idle" },
		});
		expect(keys(priority)).toEqual(["a"]);
		expect(keys(recents)).toEqual([]);
	});
});

describe("session merge helpers", () => {
	it("merges upserts in place and prepends only new keys", () => {
		const list = [meta("a", "2026-10-17T10:00:00Z"), meta("b", "2026-10-17T09:00:00Z")];
		const out = mergeUpserts(list, [
			meta("d", "2026-10-17T08:00:00Z"),
			meta("c", "2026-10-17T12:00:00Z"),
			meta("b", "2026-10-17T13:00:00Z"),
		]);
		// b stays in its slot despite the newer mtime; new keys prepend, newest first
		expect(out.map((s) => s.key)).toEqual(["c", "d", "a", "b"]);
		expect(out[3].mtime).toBe("2026-10-17T13:00:00Z");
	});

	it("drops removed keys and keeps the rest", () => {
		const list = [meta("a", "2026-10-17T10:00:00Z"), meta("b", "2026-10-17T09:00:00Z")];
		expect(dropSessions(list, ["b"]).map((s) => s.key)).toEqual(["a"]);
		expect(dropSessions(list, []).map((s) => s.key)).toEqual(["a", "b"]);
	});
});
