import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { DialogActions } from "./DialogActions";

// JSX interpolations render with comment markers between segments.
const flat = (html: string) => html.replace(/<!--.*?-->/g, "");

describe("DialogActions", () => {
	it("renders Yes and No for a confirm dialog carrying an id", () => {
		const html = flat(
			renderToString(
				<DialogActions k="k" dialog={{ id: "d1", kind: "confirm", title: "Allow edit?" }} />,
			),
		);
		expect(html).toContain("pi asks: Allow edit?");
		expect(html).toContain(">Yes<");
		expect(html).toContain(">No<");
	});

	it("renders nothing for other kinds or id-less dialogs", () => {
		expect(
			renderToString(<DialogActions k="k" dialog={{ id: "d1", kind: "editor", title: "T" }} />),
		).toBe("");
		expect(renderToString(<DialogActions k="k" dialog={{ kind: "confirm", title: "T" }} />)).toBe(
			"",
		);
	});
});
