import { describe, expect, it } from "vitest";
import { activeScheme, DEFAULT_SCHEME, SCHEMES, schemeById } from "./theme";

describe("schemes", () => {
	it("carries cobalt next dark as the default", () => {
		expect(DEFAULT_SCHEME).toBe("cobalt-next-dark");
		expect(activeScheme().id).toBe("cobalt-next-dark");
	});

	it("keeps dracula available", () => {
		expect(schemeById("dracula").id).toBe("dracula");
		expect(schemeById("dracula").label).toBe("Dracula");
	});

	// Future schemes ride the same shape: the Settings picker renders
	// whatever SCHEMES holds, so a lopsided entry would render a broken
	// row or an unstyled app on switch.
	it("gives every scheme the same token and terminal keys", () => {
		expect(SCHEMES.length).toBeGreaterThanOrEqual(2);
		const varKeys = SCHEMES.map((s) => Object.keys(s.vars).sort());
		for (const k of varKeys) expect(k).toEqual(varKeys[0]);
		const termKeys = SCHEMES.map((s) => Object.keys(s.term).sort());
		for (const k of termKeys) expect(k).toEqual(termKeys[0]);
		for (const s of SCHEMES) {
			for (const v of Object.values(s.vars)) expect(v).toMatch(/^#[0-9a-f]{6}$/i);
			for (const v of Object.values(s.term)) expect(v).toMatch(/^#[0-9a-f]{6}$/i);
		}
	});

	it("falls back to the default on a garbage stored id", () => {
		// store() is null-safe; without localStorage this reads default.
		expect(schemeById("nope").id).toBe(DEFAULT_SCHEME);
	});
});
