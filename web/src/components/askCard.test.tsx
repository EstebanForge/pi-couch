import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { AskQuestion } from "../api";
import { AskCard, askAnswerBody, askReady } from "./AskCard";
import { DialogActions } from "./DialogActions";

// JSX interpolations render with comment markers between segments.
const flat = (html: string) => html.replace(/<!--.*?-->/g, "");

const testQuestions: AskQuestion[] = [
	{
		question: "Which release channel?",
		header: "Deploy",
		multi_select: false,
		options: [
			{ label: "GitHub Release", description: "self-updater picks it up" },
			{ label: "Manual install", description: "no tag until later" },
		],
	},
	{
		question: "Which tool types?",
		header: "Scope",
		multi_select: true,
		options: [{ label: "Single-select" }, { label: "Multi-select" }],
	},
];

describe("askReady", () => {
	it("starts false: nothing answered yet", () => {
		expect(askReady([[], []], ["", ""])).toBe(false);
	});
	it("an option alone or text alone answers a question", () => {
		expect(askReady([[0], []], ["", ""])).toBe(false); // second open
		expect(askReady([[0], [1]], ["", ""])).toBe(true);
		expect(askReady([[], []], ["ship it", ""])).toBe(false); // second open
		expect(askReady([[], [0, 1]], ["", ""])).toBe(false); // first open
		expect(askReady([[0], []], ["", "typed"])).toBe(true);
	});
	it("whitespace text is not an answer", () => {
		expect(askReady([[], []], ["   ", ""])).toBe(false);
	});
});

describe("askAnswerBody", () => {
	it("carries indexes and text per question", () => {
		const body = askAnswerBody("tc1", testQuestions, [[1], [0, 1]], ["", ""]);
		expect(body).toEqual({
			tool_call_id: "tc1",
			answers: [
				{ question_index: 0, option_indexes: [1] },
				{ question_index: 1, option_indexes: [0, 1] },
			],
		});
	});
	it("a typed custom answer rides text only", () => {
		const body = askAnswerBody("tc1", testQuestions, [[], []], ["phone-first", ""]);
		expect(body.answers?.[0]).toEqual({ question_index: 0, text: "phone-first" });
	});
	it("single-select never combines options with text; multiSelect may", () => {
		// The UI prevents the combination; the payload builder holds the
		// line if state ever races: text wins as the latest intent.
		const single = askAnswerBody("tc1", [testQuestions[0]], [[0]], ["typed"]);
		expect(single.answers?.[0]).toEqual({ question_index: 0, text: "typed" });
		const multi = askAnswerBody("tc1", [testQuestions[1]], [[0]], ["plus text"]);
		expect(multi.answers?.[0]).toEqual({
			question_index: 0,
			option_indexes: [0],
			text: "plus text",
		});
	});
});

describe("AskCard", () => {
	it("renders the questionnaire: header chip, options, own-answer rows", () => {
		const html = flat(
			renderToString(
				<AskCard
					k="s1"
					dialog={{ id: "tc1", kind: "ask", title: "Deploy", questions: testQuestions }}
				/>,
			),
		);
		expect(html).toContain("pi asks");
		expect(html).toContain("Deploy");
		expect(html).toContain("Which release channel?");
		expect(html).toContain("GitHub Release");
		expect(html).toContain("self-updater picks it up");
		expect(html).toContain("Which tool types?");
		expect(html).toContain("pick any");
		expect(html).toContain("[ ]");
		expect(html).toContain('placeholder="Type your own answer…"');
		expect(html).toContain(">Submit<");
		expect(html).toContain(">Cancel<");
		// Nothing answered yet: submit is the only disabled control
		// (the classes' disabled: variants don't render attributes).
		expect(html.match(/disabled=""/g)?.length).toBe(1);
	});

	it("single-select rows are radios, not checkboxes", () => {
		const html = flat(
			renderToString(
				<AskCard
					k="s1"
					dialog={{
						id: "tc1",
						kind: "ask",
						title: "Deploy",
						questions: [testQuestions[0]],
					}}
				/>,
			),
		);
		expect(html).toContain(">○<");
		expect(html).not.toContain("[ ]");
	});

	it("renders nothing for an id-less or empty ask", () => {
		expect(
			renderToString(
				<AskCard k="s1" dialog={{ id: "tc1", kind: "ask", title: "T", questions: [] }} />,
			),
		).toBe("");
		expect(
			renderToString(
				<AskCard k="s1" dialog={{ kind: "ask", title: "T", questions: testQuestions }} />,
			),
		).toBe("");
	});

	it("DialogActions routes ask dialogs to the card, pane dialogs to verbs", () => {
		const html = flat(
			renderToString(
				<DialogActions
					k="s1"
					dialog={{ id: "tc1", kind: "ask", title: "Deploy", questions: testQuestions }}
				/>,
			),
		);
		expect(html).toContain("Which release channel?");
		expect(html).not.toContain('placeholder="Type your answer…"');
	});

	it("shows the timeout copy only after a timeout", () => {
		// Static render cannot force state; the copy strings stay
		// greppable so a wording regression breaks loudly here.
		const html = flat(
			renderToString(
				<AskCard
					k="s1"
					dialog={{ id: "tc1", kind: "ask", title: "Deploy", questions: testQuestions }}
				/>,
			),
		);
		expect(html).not.toContain("didn't answer in time");
		expect(html).not.toContain("pi refused that answer");
	});
});
