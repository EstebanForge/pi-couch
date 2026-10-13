import { describe, expect, it } from "vitest";
import type { SessionMeta } from "../api";
import { grouped } from "./sessions";

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
		const rows = [meta("a", "2026-01-04T00:00:00Z"), meta("b", "2026-01-03T00:00:00Z"), meta("c", "2026-01-02T00:00:00Z"), meta("d", "2026-01-01T00:00:00Z")];
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
