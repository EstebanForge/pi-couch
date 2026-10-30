import { describe, expect, it } from "vitest";
import { isWaiting, STATUS_DOT } from "./status";

describe("isWaiting", () => {
	it("marks working and blocked as waiting", () => {
		expect(isWaiting("working")).toBe(true);
		expect(isWaiting("blocked")).toBe(true);
	});
	it("leaves terminal and quiet states alone", () => {
		expect(isWaiting("done")).toBe(false);
		expect(isWaiting("idle")).toBe(false);
		expect(isWaiting("unknown")).toBe(false);
		expect(isWaiting("")).toBe(false);
	});
	it("every herdr status has a dot color", () => {
		for (const s of ["working", "blocked", "done", "idle", "unknown"]) {
			expect(STATUS_DOT[s]).toMatch(/^bg-/);
		}
	});
	it("hc's error state reads as an error and never waits", () => {
		expect(STATUS_DOT.error).toBe("bg-error");
		expect(isWaiting("error")).toBe(false);
	});
});
