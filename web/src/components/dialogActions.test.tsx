import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { DialogActions, dialogServedSignature } from "./DialogActions";

// JSX interpolations render with comment markers between segments.
const flat = (html: string) => html.replace(/<!--.*?-->/g, "");

describe("DialogActions", () => {
	it("renders option rows for a confirm carrying an id", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "d1",
						kind: "confirm",
						title: "Allow edit?",
						options: [{ label: "Yes" }, { label: "No", description: "keep the file as it is" }],
						cursor: 0,
					}}
				/>,
			),
		);
		expect(html).toContain("pi asks");
		expect(html).toContain("Allow edit?");
		expect(html).toContain(">Yes<");
		expect(html).toContain("keep the file as it is");
		expect(html).toContain(">Cancel<");
	});

	it("renders the option rows for a questionnaire with its question body", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "d1",
						kind: "custom",
						title: "Color",
						body: "Which color, Red or Blue?",
						options: [
							{ label: "Red", description: "Pick red." },
							{ label: "Blue", description: "Pick blue." },
							{ label: "Type something." },
						],
						cursor: 0,
					}}
				/>,
			),
		);
		expect(html).toContain("Which color, Red or Blue?");
		expect(html).toContain(">Red<");
		expect(html).toContain("Pick red.");
		// the own-answer row is not a tap; it is a real input with a send
		expect(html).not.toContain(">Type something.<");
		expect(html).toContain('placeholder="Type your answer…"');
		expect(html).toContain(">Send<");
		expect(html).toContain(">Cancel<");
	});

	it("uses a numbered custom row as a text input, not an option", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "d1",
						kind: "custom",
						title: "Color",
						options: [{ label: "Red" }, { label: "Blue" }, { label: "3." }],
						cursor: 0,
					}}
				/>,
			),
		);
		expect(html).not.toContain(">3.<");
		expect(html).toContain('placeholder="Type your answer…"');
	});

	it("re-arms when the same dialog enters input mode", () => {
		const base = {
			id: "d1",
			kind: "custom",
			title: "Color",
			options: [{ label: "Red" }, { label: "3." }],
		};
		expect(dialogServedSignature({ ...base, input_mode: true })).not.toBe(
			dialogServedSignature(base),
		);
	});

	it("renders an editor draft with Submit and Cancel", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "d1",
						kind: "editor",
						title: "Commit staged changes with this message?",
						body: "probe commit",
					}}
				/>,
			),
		);
		expect(html).toContain("Commit staged changes with this message?");
		// the whole buffer is one editable message, like pi's editor
		expect(html).toContain('placeholder="Commit message"');
		expect(html).toContain(">probe commit</textarea>");
		expect(html).toContain(">Submit<");
		expect(html).toContain(">Cancel<");
	});

	it("starts a scrolled editor with an empty box", () => {
		// a scrolled editor paints only the buffer's tail - the head is
		// not on the pane, so the box must not prefill a fragment
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "e2",
						kind: "editor",
						title: "Commit staged changes with this message?",
						body: "visible tail",
						scrolled: true,
					}}
				/>,
			),
		);
		expect(html).toContain("longer than pi&#x27;s editor shows");
		expect(html).not.toContain(">visible tail<");
	});

	it("renders the card for id-less... no: id-less dialogs render nothing", () => {
		expect(renderToString(<DialogActions k="k" dialog={{ kind: "confirm", title: "T" }} />)).toBe(
			"",
		);
	});
});
