import { describe, expect, it } from "vitest";
import type { SessionMeta } from "../api";
import { dropSessions, grouped, mergeUpserts } from "./sessions";

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

describe("grouped", () => {
	it("floats working and blocked above the rest, keeping recency inside groups", () => {
		const rows = [
			meta("a", "2026-01-04T00:00:00Z"),
			meta("b", "2026-01-03T00:00:00Z"),
			meta("c", "2026-01-02T00:00:00Z"),
			meta("d", "2026-01-01T00:00:00Z"),
		];
		const badges = { "/p/b": "working", "/p/d": "blocked" };
		expect(keys(grouped(rows, badges))).toEqual(["b", "d", "a", "c"]);
	});

	it("treats an empty badge map as recency order untouched", () => {
		const rows = [meta("a", "2026-01-02T00:00:00Z"), meta("b", "2026-01-01T00:00:00Z")];
		expect(keys(grouped(rows, {}))).toEqual(["a", "b"]);
	});

	it("keeps relative order when every session is active", () => {
		const rows = [meta("a", "2026-01-02T00:00:00Z"), meta("b", "2026-01-01T00:00:00Z")];
		const badges = { "/p/a": "working", "/p/b": "blocked" };
		expect(keys(grouped(rows, badges))).toEqual(["a", "b"]);
	});
});

// Stream upsert frames carry unordered new-or-changed metas; the store
// list stays mtime-desc with one entry per key.
describe("session merge helpers", () => {
	it("merges upserts, replacing by key and sorting mtime desc", () => {
		const list = [meta("a", "2026-10-17T10:00:00Z"), meta("b", "2026-10-17T09:00:00Z")];
		const out = mergeUpserts(list, [
			meta("c", "2026-10-17T12:00:00Z"),
			meta("a", "2026-10-17T13:00:00Z"),
		]);
		expect(out.map((s) => s.key)).toEqual(["a", "c", "b"]);
	});

	it("drops removed keys and keeps the rest", () => {
		const list = [meta("a", "2026-10-17T10:00:00Z"), meta("b", "2026-10-17T09:00:00Z")];
		expect(dropSessions(list, ["b"]).map((s) => s.key)).toEqual(["a"]);
		expect(dropSessions(list, []).map((s) => s.key)).toEqual(["a", "b"]);
	});
});
