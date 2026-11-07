import { describe, expect, it } from "vitest";
import { askToolNames, isAskTool, stepsAskClass } from "./askTools";

describe("ask tool cards", () => {
	it("recognizes every peer-ask tool name", () => {
		expect([...askToolNames].sort()).toEqual(["AskAntigravity", "AskClaude", "AskCodex"]);
		expect(isAskTool("AskAntigravity")).toBe(true);
		expect(isAskTool("AskClaude")).toBe(true);
		expect(isAskTool("AskCodex")).toBe(true);
	});

	it("leaves ordinary tools alone", () => {
		expect(isAskTool("Bash")).toBe(false);
		expect(isAskTool("askantigravity")).toBe(false);
		expect(isAskTool("")).toBe(false);
	});

	it("paints a group green only when every step is an ask tool", () => {
		const green = stepsAskClass(["AskAntigravity"]);
		expect(green).toContain("bg-emerald-900/40");
		expect(green).not.toContain("bg-base-200");

		const multi = stepsAskClass(["AskClaude", "AskCodex"]);
		expect(multi).toContain("bg-emerald-900/40");
	});

	it("keeps mixed and empty groups on the default card", () => {
		const mixed = stepsAskClass(["AskClaude", "Read"]);
		expect(mixed).toContain("bg-base-200/75");
		expect(mixed).not.toContain("emerald");

		expect(stepsAskClass([])).toContain("bg-base-200/75");
		expect(stepsAskClass(["Read"])).toContain("bg-base-200/75");
	});
});
