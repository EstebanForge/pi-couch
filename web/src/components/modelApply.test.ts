import { describe, expect, it } from "vitest";
import { stalledApply } from "./modelApply";

describe("stalledApply", () => {
	it("confirms when the pending value already moved on", () => {
		expect(stalledApply(null, "m2", "idle")).toBe("confirmed");
		expect(stalledApply("other", "m2", "idle")).toBe("confirmed");
	});

	it("clears silently while the agent works: pi queues the command", () => {
		expect(stalledApply("m2", "m2", "working")).toBe("clear");
	});

	it("clears silently while a dialog waits on the user", () => {
		expect(stalledApply("m2", "m2", "blocked")).toBe("clear");
	});

	it("clears and dismisses picker residue when the agent is idle", () => {
		expect(stalledApply("m2", "m2", "idle")).toBe("clear-and-dismiss");
		expect(stalledApply("m2", "m2", "done")).toBe("clear-and-dismiss");
	});
});
