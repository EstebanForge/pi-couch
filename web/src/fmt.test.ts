import { describe, expect, it } from "vitest";
import { money, thousand } from "./fmt";

describe("thousand", () => {
	it("formats integers with Chilean dot separators", () => {
		expect(thousand(1234567)).toBe("1.234.567");
		expect(thousand(662)).toBe("662");
	});
});

describe("money", () => {
	it("formats cost with dot thousands and comma decimals", () => {
		expect(money(1234.56)).toBe("1.234,56");
		expect(money(11.75)).toBe("11,75");
	});
});
