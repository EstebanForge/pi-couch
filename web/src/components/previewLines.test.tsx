import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { canRename, PreviewChip, PreviewLines, renameHint, renameQueueMode } from "./previewLines";

describe("PreviewLines", () => {
	it("renders the tail with speaker labels, newest last", () => {
		const html = renderToString(
			<PreviewLines
				lines={[
					{ seq: 41, who: "user", text: "Why does the tray flicker on resize?" },
					{ seq: 44, who: "assistant", text: "The panel re-anchors every frame; pinning it." },
				]}
			/>,
		);
		expect(html).toContain(">you<");
		expect(html).toContain(">pi<");
		expect(html).toContain("tray flicker");
		expect(html).toContain("re-anchors every frame");
		// the assistant line follows the user line: newest last
		expect(html.indexOf("tray flicker")).toBeLessThan(html.indexOf("re-anchors"));
	});

	it("answers an empty thread with a line, not a blank card", () => {
		expect(renderToString(<PreviewLines lines={[]} />)).toContain("Nothing said yet.");
	});
});

describe("PreviewChip", () => {
	it("marks the waiting-for-you state", () => {
		expect(renderToString(<PreviewChip status="working" awaiting={true} />)).toContain(
			"waiting for you",
		);
	});
	it("shows the plain status otherwise", () => {
		expect(renderToString(<PreviewChip status="done" awaiting={false} />)).toContain("done");
	});
});

describe("canRename", () => {
	it("allows rename for live driver statuses", () => {
		for (const s of ["working", "idle", "blocked", "done"]) expect(canRename(s)).toBe(true);
	});
	it("refuses rename when no driver runs the session", () => {
		expect(canRename("unknown")).toBe(false);
	});
});

describe("renameHint", () => {
	it("refuses only the dialog-waiting state", () => {
		expect(renameHint("blocked")).toContain("waiting on you");
	});
	it("is silent when the agent can take /name now", () => {
		for (const s of ["working", "idle", "done", "unknown"]) expect(renameHint(s)).toBeNull();
	});
});

describe("renameQueueMode", () => {
	it("queues the rename behind a running turn", () => {
		expect(renameQueueMode("working")).toBe("followUp");
	});
	it("sends immediately otherwise", () => {
		for (const s of ["idle", "done", "blocked", "unknown"])
			expect(renameQueueMode(s)).toBeUndefined();
	});
});
