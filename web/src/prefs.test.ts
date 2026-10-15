import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
	effectiveEnterSends,
	enterSendsPref,
	quickKeysEnabled,
	setEnterSendsPref,
	setQuickKeysEnabled,
} from "./prefs";

let backing: Map<string, string>;
beforeEach(() => {
	backing = new Map();
	(globalThis as Record<string, unknown>).localStorage = {
		getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
		setItem: (k: string, v: string) => void backing.set(k, v),
		removeItem: (k: string) => void backing.delete(k),
	};
});
afterEach(() => {
	delete (globalThis as Record<string, unknown>).localStorage;
});

describe("prefs", () => {
	it("defaults to auto enter and quick keys on", () => {
		expect(enterSendsPref()).toBe("auto");
		expect(quickKeysEnabled()).toBe(true);
	});

	it("roundtrips the enter pref and treats garbage as auto", () => {
		setEnterSendsPref("send");
		expect(enterSendsPref()).toBe("send");
		setEnterSendsPref("auto");
		expect(enterSendsPref()).toBe("auto");
		expect(backing.has("couch-pi.enterSends")).toBe(false);
		backing.set("couch-pi.enterSends", "bogus");
		expect(enterSendsPref()).toBe("auto");
	});

	it("stores quick keys off as the 0 marker and clears on re-enable", () => {
		setQuickKeysEnabled(false);
		expect(quickKeysEnabled()).toBe(false);
		expect(backing.get("couch-pi.quickKeys")).toBe("0");
		setQuickKeysEnabled(true);
		expect(quickKeysEnabled()).toBe(true);
		expect(backing.has("couch-pi.quickKeys")).toBe(false);
	});

	it("effectiveEnterSends follows explicit prefs over the pointer heuristic", () => {
		setEnterSendsPref("send");
		expect(effectiveEnterSends()).toBe(true);
		setEnterSendsPref("newline");
		expect(effectiveEnterSends()).toBe(false);
		setEnterSendsPref("auto");
		// no matchMedia in the node test env: auto falls back to false
		expect(effectiveEnterSends()).toBe(false);
	});
});
