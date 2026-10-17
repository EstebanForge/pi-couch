import { describe, expect, it } from "vitest";
import type { BridgeDialog, StreamItem } from "../api";
import {
	type DriverState,
	emptyDriver,
	groupWorkBlocks,
	mergeOlder,
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
	it("folds consecutive foldables and keeps speech solo", () => {
		const items = [
			item(0, "user", "q"),
			item(1, "thinking", "h"),
			item(2, "tool_call", "run"),
			item(3, "tool_result", "out"),
			item(4, "text", "answer"),
		];
		const blocks = groupWorkBlocks(items);
		expect(blocks).toHaveLength(3);
		expect(blocks[0]).toEqual({ type: "item", item: items[0] });
		expect(blocks[1]).toMatchObject({ type: "block" });
		if (blocks[1].type === "block") {
			expect(blocks[1].items.map((i) => i.seq)).toEqual([1, 2, 3]);
		}
		expect(blocks[2]).toEqual({ type: "item", item: items[4] });
	});

	it("never folds markers between runs", () => {
		const items = [
			item(0, "thinking", "a"),
			item(1, "marker", "Model -> x"),
			item(2, "bash", "ls"),
		];
		const blocks = groupWorkBlocks(items);
		expect(blocks).toHaveLength(3);
	});
});
