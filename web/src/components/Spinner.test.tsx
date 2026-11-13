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

	it("renders the counter-rotating gears when gears is picked", () => {
		const html = renderToString(<Spinner variant="gears" />);
		expect(html).not.toContain("<svg");
		expect(html).toContain("spinner-gears");
		expect(html).toContain('aria-label="waiting"');
	});

	it("picks gears from the stored pref", () => {
		stubStore("gears");
		expect(spinnerStylePref()).toBe("gears");
		expect(renderToString(<Spinner />)).toContain("spinner-gears");
	});

	it("renders the hopping corner dot when hops is picked", () => {
		const html = renderToString(<Spinner variant="hops" />);
		expect(html).not.toContain("<svg");
		expect(html).toContain("spinner-hops");
		expect(html).toContain('aria-label="waiting"');
	});

	it("picks hops from the stored pref", () => {
		stubStore("hops");
		expect(spinnerStylePref()).toBe("hops");
		expect(renderToString(<Spinner />)).toContain("spinner-hops");
	});

	it("falls back to blocks on a junk or missing pref", () => {
		stubStore("junk");
		expect(spinnerStylePref()).toBe("blocks");
		expect(renderToString(<Spinner />)).toContain("<svg");

		stubStore(null);
		expect(spinnerStylePref()).toBe("blocks");
	});
});
