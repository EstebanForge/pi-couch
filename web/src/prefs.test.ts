import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
	effectiveEnterSends,
	enterSendsPref,
	quickCommands,
	quickKeyOrder,
	quickKeysEnabled,
	setEnterSendsPref,
	setQuickCommands,
	setQuickKeyOrder,
	setQuickKeysEnabled,
} from "./prefs";
import { activeScheme } from "./theme";

let backing: Map<string, string>;
beforeEach(() => {
	backing = new Map();
	(globalThis as Record<string, unknown>).localStorage = {
		getItem: (k: string) => backing.get(k) ?? null,
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
		expect(backing.get("pi-couch.quickKeys")).toBe("0");
		setQuickKeysEnabled(true);
		expect(quickKeysEnabled()).toBe(true);
		expect(backing.has("pi-couch.quickKeys")).toBe(false);
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

	it("roundtrips the key order and rejects garbage", () => {
		expect(quickKeyOrder()).toBeNull();
		setQuickKeyOrder(["down", "esc"]);
		expect(quickKeyOrder()).toEqual(["down", "esc"]);
		backing.set("pi-couch.keyOrder.v2", "{broken");
		expect(quickKeyOrder()).toBeNull();
		backing.set("pi-couch.keyOrder.v2", JSON.stringify(["esc", 42, "up"]));
		expect(quickKeyOrder()).toEqual(["esc", "up"]);
	});

	// The 2026-10-21 rename moved the storage prefix to pi-couch.*; prefs
	// written before it must survive the update.
	it("migrates legacy couch-pi keys on read", () => {
		backing.set("couch-pi.enterSends", "send");
		backing.set("couch-pi.quickKeys", "0");
		backing.set("couch-pi.keyOrder.v2", JSON.stringify(["esc", "up"]));
		backing.set("couch-pi.theme", "dracula");
		expect(enterSendsPref()).toBe("send");
		expect(quickKeysEnabled()).toBe(false);
		expect(quickKeyOrder()).toEqual(["esc", "up"]);
		expect(activeScheme().id).toBe("dracula");
		expect(backing.get("pi-couch.enterSends")).toBe("send");
		expect(backing.get("pi-couch.theme")).toBe("dracula");
		expect(backing.has("couch-pi.enterSends")).toBe(false);
		expect(backing.has("couch-pi.theme")).toBe(false);
	});

	it("lets a pi-couch value win over a leftover legacy value", () => {
		backing.set("pi-couch.enterSends", "newline");
		backing.set("couch-pi.enterSends", "send");
		expect(enterSendsPref()).toBe("newline");
		expect(backing.has("couch-pi.enterSends")).toBe(true);
	});

	it("still writes under the pi-couch prefix", () => {
		setEnterSendsPref("send");
		expect(backing.get("pi-couch.enterSends")).toBe("send");
		expect(backing.has("couch-pi.enterSends")).toBe(false);
	});

	it("never resurrects a cleared pref from its legacy key", () => {
		// a clear without a prior read must still retire the legacy value
		backing.set("couch-pi.enterSends", "send");
		setEnterSendsPref("auto");
		expect(backing.has("couch-pi.enterSends")).toBe(false);
		expect(enterSendsPref()).toBe("auto");
		setEnterSendsPref("send");
		expect(backing.has("couch-pi.enterSends")).toBe(false);
	});

	describe("quickCommands", () => {
		it("defaults when unset", () => {
			expect(quickCommands()).toContain("/compact");
			expect(quickCommands()).toContain(
				'git add -A && (git commit -m "wip $(date +%FT%H:%M)" || true) && git push -u origin HEAD',
			);
		});

		it("round-trips a custom list", () => {
			setQuickCommands(["/compact", "/mycmd"]);
			expect(quickCommands()).toEqual(["/compact", "/mycmd"]);
		});

		it("falls back to defaults on junk and empty lists", () => {
			backing.set("pi-couch.quickCommands", "{not json");
			expect(quickCommands()).toContain("/compact");
			setQuickCommands([]);
			expect(backing.has("pi-couch.quickCommands")).toBe(false);
			expect(quickCommands()).toContain("/compact");
		});
	});
});
