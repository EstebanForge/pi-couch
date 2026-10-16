import { describe, expect, it } from "vitest";
import { dispositionFor } from "./disposition";

describe("dispositionFor", () => {
	it("queues while the agent works", () => {
		expect(dispositionFor("working")).toBe("followUp");
	});

	it("sends now in every other state", () => {
		for (const status of ["idle", "blocked", "done", "gone", ""]) {
			expect(dispositionFor(status)).toBe("now");
		}
	});
});
