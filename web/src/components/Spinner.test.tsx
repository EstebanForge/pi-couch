import { renderToString } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { prefKey, spinnerStylePref } from "../prefs";
import { Spinner } from "./Spinner";

function stubStore(value: string | null) {
	vi.stubGlobal("localStorage", {
		getItem: (k: string) => (k === prefKey("spinner") ? value : null),
		setItem: () => {},
		removeItem: () => {},
		clear: () => {},
	});
}

describe("Spinner", () => {
	afterEach(() => vi.unstubAllGlobals());

	it("renders the blocks svg when nothing is stored", () => {
		const html = renderToString(<Spinner />);
		expect(html).toContain("<svg");
		expect(html).toContain('aria-label="waiting"');
	});

	it("renders the two-square rotator when squares is picked", () => {
		const html = renderToString(<Spinner variant="squares" />);
		expect(html).not.toContain("<svg");
		expect(html).toContain("spinner-squares");
		expect(html).toContain('aria-label="waiting"');
	});

	it("picks squares from the stored pref", () => {
		stubStore("squares");
		expect(spinnerStylePref()).toBe("squares");
		expect(renderToString(<Spinner />)).toContain("spinner-squares");
	});

	it("falls back to blocks on a junk or missing pref", () => {
		stubStore("junk");
		expect(spinnerStylePref()).toBe("blocks");
		expect(renderToString(<Spinner />)).toContain("<svg");

		stubStore(null);
		expect(spinnerStylePref()).toBe("blocks");
	});
});
