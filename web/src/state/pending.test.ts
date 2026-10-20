import { describe, expect, it } from "vitest";
import {
	addPending,
	type DriverState,
	emptyDriver,
	markPending,
	reduceAppend,
	removePending,
} from "./transcript";

const base: DriverState = { ...emptyDriver };

describe("pending bubbles", () => {
	it("adds, marks and removes by id", () => {
		let s = addPending(base, 1, "hello agent", false);
		expect(s.pending).toEqual([
			{ id: 1, seed: "hello agent", text: "hello agent", state: "sending" },
		]);
		s = markPending(s, 1, "sent");
		expect(s.pending[0].state).toBe("sent");
		s = removePending(s, 1);
		expect(s.pending).toEqual([]);
	});

	it("normalizes whitespace in the seed", () => {
		const s = addPending(base, 1, "  multi\n\nspace   text  ", false);
		expect(s.pending[0].seed).toBe("multi space text");
	});

	it("reconciles when a matching user item appends", () => {
		let s = addPending(base, 1, "please check the diff", false);
		s = reduceAppend(s, {
			items: [
				{
					kind: "user",
					text: "please check the diff closely",
					seq: 10,
				},
			],
		});
		expect(s.pending).toEqual([]);
	});

	it("keeps the bubble when no user item matches", () => {
		let s = addPending(base, 1, "unrelated prompt", false);
		s = reduceAppend(s, { items: [{ kind: "text", text: "some reply", seq: 11 }] });
		expect(s.pending).toHaveLength(1);
	});

	it("never matches an empty seed", () => {
		let s = addPending(base, 1, "   ", false);
		s = reduceAppend(s, { items: [{ kind: "user", text: "anything", seq: 12 }] });
		expect(s.pending).toHaveLength(1);
	});
});
