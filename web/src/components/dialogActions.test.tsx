import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { DialogActions, dialogAnswerVerb, dialogServedSignature } from "./DialogActions";

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
		// hc cannot type into a select: the custom row is a tap that
		// opens pi's real input, and the box appears on the next parse.
		expect(html).toContain(">Type something.<");
		expect(html).not.toContain('placeholder="Type your answer…"');
		expect(html).toContain(">Cancel<");
	});

	it("renders the text box only for a real input state", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "d1",
						kind: "select",
						title: "Name",
						options: [{ label: "Keep" }],
						cursor: 0,
						input_mode: true,
					}}
				/>,
			),
		);
		expect(html).toContain('placeholder="Type your answer…"');
		expect(html).toContain(">Send<");
	});

	it("uses a numbered custom row as a tap that opens pi's input", () => {
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
		expect(html).toContain(">3.<");
		expect(html).not.toContain('placeholder="Type your answer…"');
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

	it("re-arms when a same-title editor gets a new buffer", () => {
		const base = { id: "d1", kind: "editor", title: "Commit staged changes?", body: "feat: a" };
		expect(dialogServedSignature({ ...base, body: "feat: b" })).not.toBe(
			dialogServedSignature(base),
		);
	});

	it("handles checkbox custom rows and toggle verbs", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="k"
					dialog={{
						id: "d1",
						kind: "custom",
						title: "Colors",
						options: [
							{ label: "Red", checkbox: true, checked: false },
							{ label: "Type something.", checkbox: true },
							{ label: "Submit", next: true },
						],
						cursor: 0,
					}}
				/>,
			),
		);
		expect(html).not.toContain("Type something.");
		expect(html).toContain("Select one or more options, then Submit.");
		expect(html).not.toContain("bg-primary/20");
		expect(html).toContain('placeholder="Type your answer…"');
		expect(dialogAnswerVerb("Red", true)).toBe("toggle");
		expect(dialogAnswerVerb("Submit", true, true)).toBe("option");
	});

	it("renders an editor draft editable with Submit and Cancel", () => {
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
		// the box starts as pi's buffer and accepts edits; Submit sends
		// the rewrite (type) or accepts as-is (yes) when untouched
		expect(html).not.toContain("readOnly");
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
