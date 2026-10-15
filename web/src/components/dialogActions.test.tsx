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

	it("renders the option rows for a select dialog", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "d1",
						kind: "select",
						title: "Pick one",
						options: ["alpha", "beta"],
						cursor: 1,
					}}
				/>,
			),
		);
		expect(html).toContain("pi asks: Pick one");
		expect(html).toContain(">alpha<");
		expect(html).toContain(">beta<");
		expect(html).toContain(">Cancel<");
	});

	it("renders nothing for other kinds, id-less dialogs, or option-less selects", () => {
		expect(
			renderToString(<DialogActions k="k" dialog={{ id: "d1", kind: "editor", title: "T" }} />),
		).toBe("");
		expect(renderToString(<DialogActions k="k" dialog={{ kind: "confirm", title: "T" }} />)).toBe(
			"",
		);
		expect(
			renderToString(<DialogActions k="k" dialog={{ id: "d1", kind: "select", title: "T" }} />),
		).toBe("");
	});
});
