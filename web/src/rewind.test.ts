import { describe, expect, it } from "vitest";
import { rewindErrorCopy, rewindOffer } from "./rewind";

describe("rewind error copy", () => {
	it("maps the server's refusal vocabulary", () => {
		expect(rewindErrorCopy("busy")).toBe("pi is busy - wait for it to finish");
		expect(rewindErrorCopy("notdriven")).toBe("No driver runs this session.");
		expect(rewindErrorCopy("unsupported-driver")).toBe(
			"rewind needs the herdr driver - this session runs on hc.",
		);
		expect(rewindErrorCopy(undefined, "timeout")).toBe(
			"the agent didn't confirm the rewind - check the terminal before retrying",
		);
		expect(rewindErrorCopy(undefined, "refused")).toBe("pi refused the rewind");
	});

	it("keeps a fallback for unknown failures", () => {
		expect(rewindErrorCopy("http 502")).toBe("Rewind failed.");
		expect(rewindErrorCopy(undefined, undefined)).toBe("Rewind failed.");
	});

	const items = [
		{ kind: "user", seq: 0 },
		{ kind: "text", seq: 1 },
		{ kind: "user", seq: 2 },
		{ kind: "text", seq: 3 },
	];

	it("offers the rewind only on the last user message of an idle driven session", () => {
		expect(rewindOffer(items, items[2], {})).toBe(true);
		expect(rewindOffer(items, items[0], {})).toBe(false);
		expect(rewindOffer(items, items[1], {})).toBe(false);
		expect(rewindOffer(items, { kind: "user", seq: 99 }, {})).toBe(false);
	});

	it("blocks the offer while busy, resuming, or undriven", () => {
		expect(rewindOffer(items, items[2], { waiting: true })).toBe(false);
		expect(rewindOffer(items, items[2], { resuming: true })).toBe(false);
		expect(rewindOffer(items, items[2], { undriven: true })).toBe(false);
	});

	it("survives an empty transcript", () => {
		expect(rewindOffer([], { kind: "user", seq: 0 }, {})).toBe(false);
	});
});
