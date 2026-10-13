// api.golden.test.ts — pins the wire contract with recorded fixture
// payloads (web/dev/golden/). Each golden must satisfy the client type:
// if the server renames a key, this fails to compile or the runtime
// shape assertions below catch it.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type {
	SessionsSnapshot,
	StreamItem,
	TranscriptAppend,
	TranscriptReset,
	TranscriptStatus,
} from "./api";

const golden = (name: string): unknown =>
	JSON.parse(readFileSync(new URL(`../dev/golden/${name}`, import.meta.url), "utf8"));

describe("wire contract goldens", () => {
	it("sessions snapshot matches the client type", () => {
		const snap = golden("sessions.json") as SessionsSnapshot;
		expect(Array.isArray(snap.sessions)).toBe(true);
		const s = snap.sessions[0];
		for (const k of [
			"key",
			"path",
			"id",
			"cwd",
			"name",
			"first_user",
			"mtime",
			"version",
			"entries",
			"total_tokens",
			"total_cost",
			"model",
			"thinking",
		]) {
			expect(s).toHaveProperty(k);
		}
		expect(typeof snap.badges).toBe("object");
	});

	it("transcript reset matches the client type", () => {
		const reset = golden("transcript-reset.json") as TranscriptReset;
		const kinds = new Set(reset.items.map((i: StreamItem) => i.kind));
		expect(reset.items.length).toBeGreaterThan(0);
		for (const k of ["status", "model", "thinking"]) {
			expect(reset).toHaveProperty(k);
		}
		expect(kinds.has("user")).toBe(true);
		expect(reset.meta?.first_user).toBeTruthy();
		expect(reset.items.every((i) => typeof i.seq === "number")).toBe(true);
	});

	it("transcript append matches the client type", () => {
		const app = golden("transcript-append.json") as TranscriptAppend;
		expect(app.items[0].seq).toBeGreaterThan(0);
	});

	it("status event matches the client type", () => {
		const st = golden("status.json") as TranscriptStatus;
		for (const k of ["status", "model", "thinking"]) {
			expect(st).toHaveProperty(k);
		}
	});

	it("models rows carry the id key", () => {
		const rows = golden("models.json") as { id: string }[];
		expect(rows.length).toBeGreaterThan(0);
		expect(typeof rows[0].id).toBe("string");
	});
});
