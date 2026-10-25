import { describe, expect, it } from "vitest";
import type { BridgeDialog, StreamItem } from "../api";
import {
	addPending,
	type DriverState,
	emptyDriver,
	groupWorkBlocks,
	mergeOlder,
	pairTools,
	parseSkillExpansion,
	reduceAppend,
	reduceReset,
	reduceStatus,
} from "./transcript";

const item = (seq: number, kind: StreamItem["kind"], text = ""): StreamItem => ({
	seq,
	kind,
	text,
});

describe("mergeOlder", () => {
	it("prepends older pages deduped and ascending", () => {
		const cur = [item(40, "user", "new"), item(41, "text", "newer")];
		const merged = mergeOlder(cur, [item(41, "text", "dup"), item(39, "user", "old")]);
		expect(merged.map((i) => i.seq)).toEqual([39, 40, 41]);
		// the live copy wins over the fetched page
		expect(merged.find((i) => i.seq === 41)?.text).toBe("newer");
	});

	it("keeps a fully duplicate page a no-op", () => {
		const cur = [item(40, "user", "new")];
		expect(mergeOlder(cur, [item(40, "user", "dup")])).toEqual(cur);
	});
});

describe("reduceReset", () => {
	it("replaces items and the status triple", () => {
		const s = reduceReset(emptyDriver, {
			items: [item(1, "user", "hi"), item(0, "text", "hello")],
			status: "working",
			model: "p/m",
			thinking: "high",
		});
		expect(s.items.map((i) => i.seq)).toEqual([0, 1]);
		expect(s.status).toBe("working");
		expect(s.model).toBe("p/m");
		expect(s.thinking).toBe("high");
	});
});

describe("pending survives resets", () => {
	it("keeps a queued bubble whose message pi still holds", () => {
		let s = reduceReset(emptyDriver, {
			items: [item(0, "user", "earlier"), item(1, "text", "answer")],
			status: "working",
			model: "",
			thinking: "",
		});
		s = addPending(s, 1, "queued while busy", true);
		const after = reduceReset(s, {
			items: [item(0, "user", "earlier"), item(1, "text", "answer")],
			status: "working",
			model: "",
			thinking: "",
		});
		expect(after.pending.map((p) => p.id)).toEqual([1]);
	});

	it("drops a bubble whose user item arrived", () => {
		let s = reduceReset(emptyDriver, {
			items: [item(0, "user", "earlier")],
			status: "working",
			model: "",
			thinking: "",
		});
		s = addPending(s, 1, "queued while busy", true);
		const after = reduceReset(s, {
			items: [item(0, "user", "earlier"), item(1, "user", "queued while busy")],
			status: "working",
			model: "",
			thinking: "",
		});
		expect(after.pending).toEqual([]);
	});
});

describe("pairTools", () => {
	it("joins a call with its following results", () => {
		const pairs = pairTools([
			item(1, "tool_call", "src/a.tsx"),
			item(2, "tool_result", "ok"),
			item(3, "tool_call", "src/b.tsx"),
			item(4, "bash", "ran"),
		]);
		expect(pairs).toHaveLength(2);
		expect(pairs[0].call?.text).toBe("src/a.tsx");
		expect(pairs[0].outs.map((o) => o.seq)).toEqual([2]);
		expect(pairs[1].call?.text).toBe("src/b.tsx");
		expect(pairs[1].outs.map((o) => o.seq)).toEqual([4]);
	});

	it("keeps group-edge results as call-less pairs", () => {
		const pairs = pairTools([item(1, "tool_result", "leftover"), item(2, "bash", "ran")]);
		expect(pairs).toHaveLength(1);
		expect(pairs[0].call).toBeNull();
		expect(pairs[0].outs.map((o) => o.seq)).toEqual([1, 2]);
	});
});

describe("reduceAppend", () => {
	it("appends and keeps seq order", () => {
		let s = reduceReset(emptyDriver, {
			items: [item(0, "user", "hi")],
			status: "working",
			model: "",
			thinking: "",
		});
		s = reduceAppend(s, { items: [item(2, "text", "done"), item(1, "thinking", "hmm")] });
		expect(s.items.map((i) => i.seq)).toEqual([0, 1, 2]);
	});

	it("ignores duplicate seqs (reconnect replay)", () => {
		const s = reduceReset(emptyDriver, {
			items: [item(0, "user", "hi")],
			status: "",
			model: "",
			thinking: "",
		});
		const once = reduceAppend(s, { items: [item(1, "text", "x")] });
		const twice = reduceAppend(once, { items: [item(1, "text", "x")] });
		expect(twice).toBe(once);
	});
});

describe("reduceStatus", () => {
	// ctx rides the newest turn's usage: absent keeps, present sets,
	// and a reset seeds it from the header.
	it("tracks ctx from resets and statuses", () => {
		const s: DriverState = reduceReset(emptyDriver, {
			items: [],
			status: "working",
			model: "a/b",
			thinking: "low",
			meta: { name: "n", first_user: "f", cwd: "/p", ctx_tokens: 4096 },
		});
		expect(s.ctx).toBe(4096);
		expect(reduceStatus(s, { status: "working", model: "a/b", thinking: "low" }).ctx).toBe(4096);
		expect(
			reduceStatus(s, { status: "working", model: "a/b", thinking: "low", ctx_tokens: 8192 }).ctx,
		).toBe(8192);
	});

	it("tracks memory health from resets and statuses", () => {
		const s: DriverState = reduceReset(emptyDriver, {
			items: [],
			status: "working",
			model: "a/b",
			thinking: "low",
			memory: "healthy",
		});
		expect(s.memory).toBe("healthy");
		// absent on a status keeps the current value, a new class wins
		expect(reduceStatus(s, { status: "working", model: "a/b", thinking: "low" }).memory).toBe(
			"healthy",
		);
		expect(
			reduceStatus(s, { status: "working", model: "a/b", thinking: "low", memory: "down" }).memory,
		).toBe("down");
	});

	it("updates only the triple", () => {
		const s: DriverState = reduceReset(emptyDriver, {
			items: [item(0, "text", "x")],
			status: "working",
			model: "a/b",
			thinking: "low",
		});
		const next = reduceStatus(s, { status: "done", model: "a/b", thinking: "low" });
		expect(next.status).toBe("done");
		expect(next.items).toBe(s.items);
	});

	// The server always sends the dialog key on statuses, so undefined
	// only happens on hand-built frames; null is pi's end event.
	it("keeps the dialog when absent and clears it on null", () => {
		const live: BridgeDialog = { id: "d1", kind: "custom", title: "Color", cursor: 0 };
		const s: DriverState = reduceReset(emptyDriver, {
			items: [],
			status: "blocked",
			model: "a/b",
			thinking: "low",
			dialog: live,
		});
		expect(reduceStatus(s, { status: "blocked", model: "a/b", thinking: "low" }).dialog).toBe(live);
		expect(
			reduceStatus(s, { status: "blocked", model: "a/b", thinking: "low", dialog: live }).dialog,
		).toBe(live);
		expect(
			reduceStatus(s, { status: "working", model: "a/b", thinking: "low", dialog: null }).dialog,
		).toBeNull();
	});
});

describe("groupWorkBlocks", () => {
	it("groups consecutive thinking into one collapsed block", () => {
		const items = [
			item(0, "user", "q"),
			item(1, "thinking", "h1"),
			item(2, "thinking", "h2"),
			item(3, "tool_call", "run"),
			item(4, "tool_result", "out"),
			item(5, "text", "answer"),
		];
		const blocks = groupWorkBlocks(items);
		expect(blocks).toHaveLength(4);
		expect(blocks[0]).toEqual({ type: "item", item: items[0] });
		expect(blocks[1]).toMatchObject({ type: "thinking" });
		if (blocks[1].type === "thinking") {
			expect(blocks[1].items.map((i) => i.seq)).toEqual([1, 2]);
		}
		expect(blocks[2]).toMatchObject({ type: "block" });
		if (blocks[2].type === "block") {
			expect(blocks[2].items.map((i) => i.seq)).toEqual([3, 4]);
		}
		expect(blocks[3]).toEqual({ type: "item", item: items[5] });
	});

	it("never folds markers between runs", () => {
		const items = [
			item(0, "thinking", "a"),
			item(1, "marker", "Model -> x"),
			item(2, "bash", "ls"),
		];
		const blocks = groupWorkBlocks(items);
		expect(blocks).toHaveLength(3);
		expect(blocks[0]).toMatchObject({ type: "thinking" });
		expect(blocks[1]).toEqual({ type: "item", item: items[1] });
	});
});

describe("parseSkillExpansion", () => {
	it("splits the real pi expansion into skill and typed text", () => {
		const text =
			'<skill name="bro" location="/home/you/.agents/skills/bro/SKILL.md">\n# bro\n\nRestate your message.\n</skill>\n\ndo E3 mobile-passkey-provider.';
		const p = parseSkillExpansion(text);
		expect(p).not.toBeNull();
		expect(p?.name).toBe("bro");
		expect(p?.location).toBe("/home/you/.agents/skills/bro/SKILL.md");
		expect(p?.body).toBe("# bro\n\nRestate your message.");
		expect(p?.rest).toBe("do E3 mobile-passkey-provider.");
	});

	it("keeps interior blank lines of the skill body", () => {
		const p = parseSkillExpansion(
			'<skill name="x">\nline one\n\nline two\n</skill>\ndone',
		);
		expect(p?.body).toBe("line one\n\nline two");
		expect(p?.rest).toBe("done");
	});

	it("returns null for plain text or an unclosed block", () => {
		expect(parseSkillExpansion("just a normal message")).toBeNull();
		expect(parseSkillExpansion('<skill name="x">\nnever closed')).toBeNull();
		expect(parseSkillExpansion('leading text\n<skill name="x">\nbody\n</skill>')).toBeNull();
	});

	it("tolerates a missing location attribute", () => {
		const p = parseSkillExpansion('<skill name="x">\nbody\n</skill>');
		expect(p?.name).toBe("x");
		expect(p?.location).toBe("");
		expect(p?.rest).toBe("");
	});
});
