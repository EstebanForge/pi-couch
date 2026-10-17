import { describe, expect, it } from "vitest";
import { orderKeys } from "./quick-keys";

describe("orderKeys", () => {
	it("keeps the default order when nothing is stored", () => {
		expect(orderKeys(null).map((k) => k.id)).toEqual([
			"esc",
			"tab",
			"altenter",
			"space",
			"enter",
			"left",
			"up",
			"down",
			"right",
		]);
	});

	it("applies a stored order and appends omitted keys", () => {
		const ids = orderKeys(["down", "esc"]).map((k) => k.id);
		expect(ids[0]).toBe("down");
		expect(ids[1]).toBe("esc");
		// every key still present exactly once
		expect(new Set(ids).size).toBe(ids.length);
		expect(ids).toContain("altenter");
	});

	it("drops unknown stored ids and keeps every known key", () => {
		const ids = orderKeys(["bogus", "space", "lost"]).map((k) => k.id);
		expect(ids).not.toContain("bogus");
		expect(ids).not.toContain("lost");
		expect(ids[0]).toBe("space");
		expect(new Set(ids).size).toBe(ids.length);
	});
});
