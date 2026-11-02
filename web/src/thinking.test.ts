import { describe, expect, it } from "vitest";
import { clampThinking, levelsForModel, THINKING_LEVELS } from "./thinking";

describe("levelsForModel", () => {
	it("falls back to the full ladder without catalog data", () => {
		expect(levelsForModel(undefined)).toEqual(THINKING_LEVELS);
		expect(levelsForModel([])).toEqual(THINKING_LEVELS);
	});

	it("uses the model's own levels", () => {
		expect(levelsForModel(["low", "high", "max"])).toEqual(["low", "high", "max"]);
	});
});

describe("clampThinking", () => {
	it("keeps a supported level", () => {
		expect(clampThinking(["low", "high", "max"], "high")).toBe("high");
	});

	// glm-5.2's ladder: high, max.
	it("walks up to the nearest supported level", () => {
		expect(clampThinking(["high", "max"], "off")).toBe("high");
		expect(clampThinking(["high", "max"], "medium")).toBe("high");
	});

	it("walks down when nothing at or above supports", () => {
		expect(clampThinking(["low", "medium"], "high")).toBe("medium");
	});

	it("falls back to the first supported for unknown levels", () => {
		expect(clampThinking(["high", "max"], "ultra")).toBe("high");
	});

	it("handles the off-only ladder of non-reasoning models", () => {
		expect(clampThinking(["off"], "high")).toBe("off");
	});

	it("treats missing data as the full ladder", () => {
		expect(clampThinking([], "low")).toBe("low");
	});
});
