import { describe, expect, it } from "vitest";
import { nextBackoffMs } from "./stream";

describe("nextBackoffMs", () => {
	it("doubles from 250ms and caps at 5s", () => {
		expect(nextBackoffMs(0)).toBe(250);
		expect(nextBackoffMs(1)).toBe(500);
		expect(nextBackoffMs(2)).toBe(1000);
		expect(nextBackoffMs(3)).toBe(2000);
		expect(nextBackoffMs(4)).toBe(4000);
		expect(nextBackoffMs(5)).toBe(5000);
		expect(nextBackoffMs(50)).toBe(5000);
	});
});
