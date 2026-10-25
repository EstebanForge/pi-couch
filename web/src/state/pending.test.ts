import { describe, expect, it } from "vitest";
import {
	addPending,
	type DriverState,
	emptyDriver,
	markPending,
	reduceAppend,
	reduceReset,
	removePending,
} from "./transcript";

const base: DriverState = { ...emptyDriver };

describe("pending bubbles", () => {
	it("adds, marks and removes by id", () => {
		let s = addPending(base, 1, "hello agent", false);
		expect(s.pending).toEqual([
			{ id: 1, seed: "hello agent", text: "hello agent", state: "sending", at: expect.any(Number) },
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

	it("reconciles when pi expands a skill around the text", () => {
		// pi expands $skill invocations by prepending the skill block to
		// the typed text, so the landed message no longer STARTS with the
		// seed - only a contains match drops the bubble.
		let s = addPending(base, 1, "do E3 mobile-passkey-provider.", false);
		s = reduceAppend(s, {
			items: [
				{
					kind: "user",
					text: '<skill name="bro" location="/skills/bro/SKILL.md">\nlong skill body\n</skill>\n\ndo E3 mobile-passkey-provider. and more',
					seq: 13,
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

	// The reconcile world is the in-memory tail (40 items on connect), so
	// a delivery that lands while the phone is dozed falls outside it and
	// the bubble would stick forever. Age expires it instead.
	it("expires stale bubbles on any reduce", () => {
		const s = addPending(base, 1, "slow queue", true, Date.now() - 31 * 60 * 1000);
		const next = reduceAppend(s, { items: [{ kind: "text", text: "reply", seq: 13 }] });
		expect(next.pending).toEqual([]);
	});

	it("keeps fresh bubbles through reduces", () => {
		const s = addPending(base, 1, "slow queue", true, Date.now() - 60 * 1000);
		const next = reduceAppend(s, { items: [{ kind: "text", text: "reply", seq: 13 }] });
		expect(next.pending).toHaveLength(1);
	});

	it("expires restored bubbles that carry no timestamp", () => {
		// pre-fix storage entries: no at field means ancient, drop them
		const s = addPending(base, 1, "stuck since yesterday", true, 0);
		const next = reduceReset(s, { items: [], status: "", model: "", thinking: "" });
		expect(next.pending).toEqual([]);
	});
});
