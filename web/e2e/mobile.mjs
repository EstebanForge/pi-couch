import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { after, afterEach, before, test } from "node:test";
import { launchBrowser } from "./cdp.mjs";
import { startFixture } from "./fixture.mjs";

const artifacts = resolve(process.env.E2E_ARTIFACTS ?? "test-results/mobile");
const questions = [
	{
		question: "Which release channel?",
		header: "Deploy",
		multi_select: false,
		options: [
			{ label: "GitHub Release", description: "self-updater" },
			{ label: "Manual install", description: "no tag" },
		],
	},
	{
		question: "Which answer types?",
		header: "Scope",
		multi_select: true,
		options: [
			{ label: "Single", description: "one option" },
			{ label: "Multi", description: "several options" },
		],
	},
];

let fixture;
let browser;
let page;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function eventually(check, label, timeoutMs = 5000) {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		if (check()) return;
		await sleep(50);
	}
	throw new Error(`timed out waiting for ${label}`);
}
async function open(path = "/") {
	await page.navigate(`${fixture.origin}${path}`);
}
async function openSession() {
	await open("/sessions/demo");
	await page.waitFor(
		"document.querySelector('textarea[placeholder=\"Steer the agent…\"]')",
		"session composer",
	);
}
async function submitDisabled() {
	return page.evaluate(`(() => {
		const button = [...document.querySelectorAll("button")].find((node) => node.textContent.trim() === "Submit");
		return button ? button.disabled : null;
	})()`);
}
async function assertNoOverflow() {
	const dimensions = await page.evaluate(`({
		document: document.documentElement.scrollWidth,
		body: document.body.scrollWidth,
		viewport: window.innerWidth,
	})`);
	assert.ok(
		dimensions.document <= dimensions.viewport && dimensions.body <= dimensions.viewport,
		`horizontal overflow: ${JSON.stringify(dimensions)}`,
	);
}

before(async () => {
	fixture = await startFixture();
	browser = await launchBrowser(artifacts);
	page = browser.page;
});

after(async () => {
	try {
		await browser?.close();
	} finally {
		await fixture?.close();
	}
});

afterEach(async () => {
	await assertNoOverflow();
	assert.deepEqual(page.exceptions, [], "uncaught browser exceptions");
});

test("home opens a session with real touch and no horizontal overflow", async () => {
	fixture.reset();
	await open();
	await page.waitFor("document.body.innerText.includes('Fixture session')", "home session row");
	const homeScripts = fixture.state.scripts.length;
	await page.screenshot(artifacts, "01-home");
	await page.tapText("Fixture session", "button", false);
	await page.waitFor("location.pathname === '/sessions/demo'", "session navigation");
	await page.waitFor("document.querySelector('textarea')", "session composer");
	assert.ok(
		fixture.state.scripts.slice(homeScripts).some((path) => path.endsWith(".js")),
		"the session route did not load a deferred script",
	);
	await page.screenshot(artifacts, "02-session");
});

test("ask card submits option and multi-select answers through the structured endpoint", async () => {
	fixture.reset();
	fixture.openAsk(questions);
	await openSession();
	await page.waitFor(
		"document.querySelector('input[aria-label=\"Own answer for Deploy\"]')",
		"ask card",
	);
	await page.waitFor(
		"new DOMMatrix(getComputedStyle(document.querySelector('.fixed.inset-x-0.bottom-0.z-50')).transform).m42 === 0",
		"ask sheet transition",
	);
	assert.equal(
		await submitDisabled(),
		true,
		"submit starts gated until every question is answered",
	);
	await page.tapText("GitHub Release", "button", false);
	await page.tapText("Single", "button", false);
	await page.tapText("Multi", "button", false);
	await page.waitFor(
		"(() => { const b = [...document.querySelectorAll('button')].find((n) => n.textContent.trim() === 'Submit'); return b && !b.disabled; })()",
		"enabled ask submit",
	);
	assert.equal(await submitDisabled(), false);
	await page.screenshot(artifacts, "03-ask-options");
	await page.tapText("Submit");
	await eventually(() => fixture.state.askAnswers.length === 1, "ask answer request");
	assert.deepEqual(fixture.state.askAnswers[0], {
		tool_call_id: "ask-e2e",
		answers: [
			{ question_index: 0, option_indexes: [0] },
			{ question_index: 1, option_indexes: [0, 1] },
		],
	});
	await page.waitFor(
		"!document.querySelector('input[aria-label=\"Own answer for Deploy\"]')",
		"ask sheet close",
	);
	await page.screenshot(artifacts, "04-ask-answered");
});

test("custom text replaces a single-select option and submits without option indexes", async () => {
	fixture.reset();
	fixture.openAsk([questions[0]], "ask-custom");
	await openSession();
	await page.waitFor(
		"document.querySelector('input[aria-label=\"Own answer for Deploy\"]')",
		"custom ask card",
	);
	await page.waitFor(
		"new DOMMatrix(getComputedStyle(document.querySelector('.fixed.inset-x-0.bottom-0.z-50')).transform).m42 === 0",
		"custom sheet transition",
	);
	await page.tapText("GitHub Release", "button", false);
	await page.fill('input[aria-label="Own answer for Deploy"]', "Use the custom path");
	await page.waitFor(
		"(() => { const b = [...document.querySelectorAll('button')].find((n) => n.textContent.trim() === 'Submit'); return b && !b.disabled; })()",
		"enabled custom submit",
	);
	assert.equal(await submitDisabled(), false);
	await page.screenshot(artifacts, "05-ask-custom");
	await page.tapText("Submit");
	await eventually(() => fixture.state.askAnswers.length === 1, "custom answer request");
	assert.deepEqual(fixture.state.askAnswers[0], {
		tool_call_id: "ask-custom",
		answers: [{ question_index: 0, text: "Use the custom path" }],
	});
	await page.waitFor(
		"!document.querySelector('input[aria-label=\"Own answer for Deploy\"]')",
		"custom sheet close",
	);
});

test("cancel sends the structured cancellation and closes the sheet", async () => {
	fixture.reset();
	fixture.openAsk([questions[0]], "ask-cancel");
	await openSession();
	await page.waitFor(
		"document.querySelector('input[aria-label=\"Own answer for Deploy\"]')",
		"cancelable ask card",
	);
	await page.waitFor(
		"new DOMMatrix(getComputedStyle(document.querySelector('.fixed.inset-x-0.bottom-0.z-50')).transform).m42 === 0",
		"cancel sheet transition",
	);
	await page.tapText("Cancel");
	await eventually(() => fixture.state.askAnswers.length === 1, "cancel request");
	assert.deepEqual(fixture.state.askAnswers[0], { tool_call_id: "ask-cancel", cancel: true });
	await page.waitFor(
		"!document.querySelector('input[aria-label=\"Own answer for Deploy\"]')",
		"cancel sheet close",
	);
	await page.screenshot(artifacts, "06-ask-cancelled");
});

test("long-pressing the last user message rewinds and refills the composer", async () => {
	fixture.reset();
	await openSession();
	await page.waitFor("document.querySelector('textarea')", "session composer");
	fixture.setIdle();

	const lastUserText = "Second steer: continue with the strip.";
	await page.waitFor(
		"document.querySelector('[data-testid=\"rewindable\"]')?.textContent.includes('Second steer')",
		"rewindable last user bubble",
	);
	await page.holdSel('[data-testid="rewindable"]');
	await page.waitFor("document.body.innerText.includes('Rewind message')", "rewind confirm dialog");
	await page.screenshot(artifacts, "07-rewind-dialog");
	await page.tapText("Rewind");
	await eventually(() => fixture.state.rewinds.length === 1, "rewind request");
	assert.deepEqual(fixture.state.rewinds[0], { key: "demo", seq: 9 });
	await page.waitFor(
		`document.querySelector('textarea').value === ${JSON.stringify(lastUserText)}`,
		"composer refilled with the rewound message",
	);
	await page.screenshot(artifacts, "08-rewind-refilled");
});

test("an empty TODO status frame removes the visible checklist", async () => {
	fixture.reset();
	fixture.setTodos([
		{ id: 17, subject: "Old unfinished task", status: "in_progress" },
		{ id: 18, subject: "Current pending task", status: "pending" },
	]);
	await openSession();
	await page.waitFor("document.body.innerText.includes('TODOS')", "TODO checklist");
	await page.tapText("Todos", "button", false);
	await page.waitFor(
		"document.body.innerText.includes('Old unfinished task')",
		"expanded TODO checklist",
	);
	await page.screenshot(artifacts, "07-todos-open");
	fixture.setTodos([]);
	await page.waitFor(
		"!document.body.innerText.includes('Old unfinished task')",
		"TODO checklist clear",
	);
	assert.equal(
		await page.evaluate("document.body.innerText.includes('Current pending task')"),
		false,
	);
	await page.screenshot(artifacts, "08-todos-cleared");
});

test("touch Enter adds a newline and Send posts the complete composer draft", async () => {
	fixture.reset();
	await openSession();
	const textarea = 'textarea[placeholder="Steer the agent…"]';
	await page.fill(textarea, "First line");
	await page.pressEnter();
	await page.send("Input.insertText", { text: "Second line" });
	assert.equal(
		await page.evaluate(`document.querySelector(${JSON.stringify(textarea)}).value`),
		"First line\nSecond line",
	);
	assert.equal(fixture.state.prompts.length, 0, "touch Enter must not send");
	await page.waitFor(
		"!document.querySelector('button[aria-label=\"Send\"]').disabled",
		"enabled composer send",
	);
	await page.screenshot(artifacts, "09-composer-multiline");
	await page.tap('button[aria-label="Send"]');
	await eventually(() => fixture.state.prompts.length === 1, "prompt request");
	assert.deepEqual(fixture.state.prompts[0], {
		text: "First line\nSecond line",
		whenBusy: "followUp",
	});
});

test("quick-create failure re-enables the sheet and retry opens the created session", async () => {
	fixture.reset();
	fixture.state.failCreates = 1;
	await open();
	await page.waitFor(
		"document.body.innerText.includes('Fixture session')",
		"home before quick create",
	);
	await page.evaluate("localStorage.setItem('pi-couch.quickPath', '/tmp/demo')");
	await page.tap('[aria-label="New agent"]');
	await page.tapText("Quick agent");
	await page.waitFor(
		"document.querySelector('textarea[placeholder=\"First prompt for the agent…\"]')",
		"quick agent sheet",
	);
	await sleep(500);
	await page.fill('textarea[placeholder="First prompt for the agent…"]', "Retry this create");
	await page.waitFor(
		"[...document.querySelectorAll('button')].some((n) => n.textContent.trim() === 'Start agent' && n.getClientRects().length > 0 && !n.disabled)",
		"enabled quick create",
	);
	await page.tapText("Start agent");
	await page.waitFor("document.body.innerText.includes('create failed')", "create failure copy");
	await page.waitFor(
		"[...document.querySelectorAll('button')].some((n) => n.textContent.trim() === 'Start agent' && n.getClientRects().length > 0 && !n.disabled)",
		"enabled create retry",
	);
	assert.equal(
		await page.evaluate(
			"[...document.querySelectorAll('button')].some((n) => n.textContent.trim() === 'Start agent' && n.getClientRects().length > 0 && !n.disabled)",
		),
		true,
		"failure must re-enable retry",
	);
	await page.screenshot(artifacts, "10-create-failed");
	await page.tapText("Start agent");
	await page.waitFor(
		"location.pathname === '/sessions/created-demo'",
		"created session navigation",
	);
	assert.equal(fixture.state.agents.length, 2);
	assert.deepEqual(fixture.state.agents[0], fixture.state.agents[1]);
	assert.equal(fixture.state.agents[1].prompt, "Retry this create");
	await page.screenshot(artifacts, "11-create-retried");
});

test("quick-create attachments keep the ten-file cap and three-upload limit", async () => {
	fixture.reset();
	await open();
	await page.evaluate("localStorage.setItem('pi-couch.quickPath', '/tmp/demo')");
	await page.tap('[aria-label="New agent"]');
	await page.tapText("Quick agent");
	await page.waitFor(
		"document.querySelector('textarea[placeholder=\"First prompt for the agent…\"]')",
		"quick attachment sheet",
	);
	await sleep(500);
	const files = await Promise.all(
		Array.from({ length: 11 }, async (_, index) => {
			const path = resolve(artifacts, `attachment-${index}.png`);
			await writeFile(path, `attachment ${index}`);
			return path;
		}),
	);
	await page.setFiles(
		`(() => {
			const area = [...document.querySelectorAll('textarea[placeholder="First prompt for the agent…"]')].find((node) => {
				const rect = node.getBoundingClientRect();
				return node.checkVisibility() && rect.top >= 0 && rect.bottom <= innerHeight;
			});
			return area?.parentElement.querySelector('input[type="file"]') ?? null;
		})()`,
		files,
	);
	await eventually(() => fixture.state.uploads >= 10, "capped upload batch");
	await sleep(300);
	assert.equal(fixture.state.uploads, 10, "the eleventh file must be dropped");
	assert.equal(fixture.state.activeUploads, 0, "all uploads finished");
	assert.ok(
		fixture.state.maxActiveUploads <= 3,
		`observed ${fixture.state.maxActiveUploads} simultaneous uploads`,
	);
	await page.screenshot(artifacts, "12-attachments-capped");
});

test("incremental attachment batches share the cap and upload slots", async () => {
	fixture.reset();
	await open();
	await page.evaluate("localStorage.setItem('pi-couch.quickPath', '/tmp/demo')");
	await page.tap('[aria-label="New agent"]');
	await page.tapText("Quick agent");
	await page.waitFor(
		"document.querySelector('textarea[placeholder=\"First prompt for the agent…\"]')",
		"incremental attachment sheet",
	);
	await sleep(500);
	const input = `(() => {
		const area = [...document.querySelectorAll('textarea[placeholder="First prompt for the agent…"]')].find((node) => {
			const rect = node.getBoundingClientRect();
			return node.checkVisibility() && rect.top >= 0 && rect.bottom <= innerHeight;
		});
		return area?.parentElement.querySelector('input[type="file"]') ?? null;
	})()`;
	const files = async (prefix) =>
		Promise.all(
			Array.from({ length: 6 }, async (_, index) => {
				const path = resolve(artifacts, `${prefix}-${index}.png`);
				await writeFile(path, `${prefix} ${index}`);
				return path;
			}),
		);
	await page.setFiles(input, await files("first-batch"));
	await page.setFiles(input, await files("second-batch"));
	await eventually(() => fixture.state.uploads >= 10, "incremental upload batches");
	await sleep(300);
	assert.equal(fixture.state.uploads, 10, "incremental selections must share the cap");
	assert.ok(
		fixture.state.maxActiveUploads <= 3,
		`observed ${fixture.state.maxActiveUploads} simultaneous uploads across batches`,
	);
	await page.screenshot(artifacts, "13-attachments-incremental");
});

test("quotas refresh CPU history and RAM every five seconds only while open", async () => {
	fixture.reset();
	await open("/quotas");
	await page.waitFor("document.body.innerText.includes('22.4%')", "initial CPU sample");
	assert.equal(
		await page.evaluate(
			"document.querySelector('[role=progressbar][aria-label=\"Memory usage\"]')?.getAttribute('aria-valuenow')",
		),
		"37.5",
	);
	assert.equal(
		await page.evaluate("document.querySelector('svg[aria-label^=\"CPU history\"]') !== null"),
		true,
		"CPU history must be graphical",
	);
	await page.waitFor("document.body.innerText.includes('68.9%')", "five-second CPU refresh");
	assert.ok(fixture.state.metricsRequests >= 2, "the second metrics request did not arrive");
	assert.equal(
		await page.evaluate(
			"document.querySelector('[role=progressbar][aria-label=\"Memory usage\"]')?.getAttribute('aria-valuenow')",
		),
		"50",
	);
	await page.screenshot(artifacts, "14-system-metrics");
	await open("/");
	const stoppedAt = fixture.state.metricsRequests;
	await sleep(5500);
	assert.equal(fixture.state.metricsRequests, stoppedAt, "closed quotas must stop sampling");
});

// Entry placement: opening a session with a tall transcript must land
// on the latest message, not somewhere back in the history. Half the
// passes deliver the tail window late, the way a phone radio does on a
// cold connect.
test("entering a tall session lands on the latest message", async () => {
	const placements = [];
	for (let pass = 0; pass < 12; pass++) {
		fixture.state.resetDelay = pass >= 6 ? 60 + Math.floor(Math.random() * 90) : 0;
		await open("/sessions/long");
		await page.waitFor("document.querySelector('textarea')", `long session composer, pass ${pass}`);
		await page.waitFor(
			"document.querySelector('[data-virtuoso-scroller]') !== null",
			`transcript scroller, pass ${pass}`,
		);
		await sleep(350);
		const m = await page.evaluate(`(() => {
			const pass = ${pass};
			const sc = document.querySelector('[data-virtuoso-scroller]');
			if (!sc) return null;
			const bottomGap = sc.scrollHeight - (sc.scrollTop + sc.clientHeight);
			const walker = document.createTreeWalker(sc, NodeFilter.SHOW_TEXT);
			let node = null;
			while (walker.nextNode()) {
				if (walker.currentNode.textContent.includes("Final fixture marker")) {
					node = walker.currentNode.parentElement;
					break;
				}
			}
			let markerVisible = null;
			if (node) {
				const rect = node.getBoundingClientRect();
				const view = sc.getBoundingClientRect();
				markerVisible = rect.bottom <= view.bottom + 8 && rect.top >= view.top - 8;
			}
			return { pass, bottomGap, markerVisible, hasMarker: node !== null };
		})()`);
		placements.push(m);
	}
	fixture.state.resetDelay = 0;
	const bad = placements.filter(
		(p) => !p?.hasMarker || p.bottomGap > 12 || p.markerVisible !== true,
	);
	assert.deepEqual(
		bad,
		[],
		`${bad.length}/12 passes opened away from the latest message: ${JSON.stringify(bad)}`,
	);
});

// Reading anchor: a tall assistant reply parks at its start instead of
// riding its end; a short one keeps the usual bottom glue. The Latest
// pill stays as the explicit way down, with no unread counter ticking.
test("a tall reply anchors at its start, a short one stays glued", async () => {
	fixture.reset();
	await open("/sessions/long");
	await page.waitFor("document.querySelector('textarea')", "long session composer");
	await page.waitFor("document.querySelector('[data-virtuoso-scroller]')", "scroller");
	await sleep(450);
	const glued = await page.evaluate(`(() => {
		const sc = document.querySelector('[data-virtuoso-scroller]');
		return sc.scrollHeight - (sc.scrollTop + sc.clientHeight);
	})()`);
	assert.ok(glued <= 12, `entry must sit at the bottom before the reply, gap ${glued}`);

	const tall = Array.from(
		{ length: 40 },
		(_, i) =>
			`Anchor probe paragraph ${i}. Filler sentence long enough to wrap across several lines on a 360px phone screen and stack up real height inside the message body block.`,
	).join("\n\n");
	fixture.pushAssistant(tall);
	await page.waitFor(
		"document.body.innerText.includes('Anchor probe paragraph 39')",
		"tall reply rendered",
	);
	await sleep(450);
	const anchored = await page.evaluate(`(() => {
		const sc = document.querySelector('[data-virtuoso-scroller]');
		const view = sc.getBoundingClientRect();
		const walker = document.createTreeWalker(sc, NodeFilter.SHOW_TEXT);
		let row = null;
		while (walker.nextNode()) {
			if (walker.currentNode.textContent.includes("Anchor probe paragraph 0.")) {
				row = walker.currentNode.parentElement.closest("[data-item-index]");
				break;
			}
		}
		if (!row) return { hasRow: false };
		const rect = row.getBoundingClientRect();
		const pill = [...document.querySelectorAll("button")].find((n) => n.textContent.includes("Latest"));
		return {
			hasRow: true,
			topDelta: Math.round(rect.top - view.top),
			bottomGap: Math.round(sc.scrollHeight - (sc.scrollTop + sc.clientHeight)),
			pillShown: !!pill,
			pillText: pill?.textContent.replace(/\\s+/g, " ").trim(),
		};
	})()`);
	assert.ok(anchored.hasRow, "tall reply row missing");
	assert.ok(
		Math.abs(anchored.topDelta) <= 16,
		`anchor must park at the reply start, topDelta ${anchored.topDelta}`,
	);
	assert.ok(
		anchored.bottomGap > 100,
		`anchored view must leave the bottom, gap ${anchored.bottomGap}`,
	);
	assert.ok(anchored.pillShown, "Latest pill must stay available while anchored");
	assert.ok(
		!/\\d/.test(anchored.pillText ?? ""),
		`unread must not tick while anchored: ${anchored.pillText}`,
	);

	const heightBefore = await page.evaluate(
		"document.querySelector('[data-virtuoso-scroller]').scrollHeight",
	);
	fixture.pushAssistant("Short follow-up landing after the anchor.");
	await page.waitFor(
		`(() => { const sc = document.querySelector('[data-virtuoso-scroller]'); return sc.scrollHeight > ${heightBefore} + 40; })()`,
		"short reply lands below the fold",
	);
	await sleep(350);
	const still = await page.evaluate(`(() => {
		const sc = document.querySelector('[data-virtuoso-scroller]');
		const view = sc.getBoundingClientRect();
		const walker = document.createTreeWalker(sc, NodeFilter.SHOW_TEXT);
		let row = null;
		while (walker.nextNode()) {
			if (walker.currentNode.textContent.includes("Anchor probe paragraph 0.")) {
				row = walker.currentNode.parentElement.closest("[data-item-index]");
				break;
			}
		}
		return {
			topDelta: row ? Math.round(row.getBoundingClientRect().top - view.top) : null,
			bottomGap: Math.round(sc.scrollHeight - (sc.scrollTop + sc.clientHeight)),
		};
	})()`);
	assert.ok(
		still.topDelta !== null && Math.abs(still.topDelta) <= 16,
		`anchor must hold while short replies land, topDelta ${still.topDelta}`,
	);
	assert.ok(still.bottomGap > 100, "short follow-up must not re-glue the view");

	await page.tapText("Latest", "button", false);
	await page.waitFor(
		"(() => { const sc = document.querySelector('[data-virtuoso-scroller]'); return sc.scrollHeight - (sc.scrollTop + sc.clientHeight) <= 12; })()",
		"pill tap re-pins the bottom",
	);
	const repinned = await page.evaluate(`(() => {
		const sc = document.querySelector('[data-virtuoso-scroller]');
		const pill = [...document.querySelectorAll("button")].find((n) => n.textContent.includes("Latest"));
		return {
			bottomGap: Math.round(sc.scrollHeight - (sc.scrollTop + sc.clientHeight)),
			pillShown: !!pill,
			shortVisible: document.body.innerText.includes("Short follow-up landing"),
		};
	})()`);
	assert.ok(repinned.bottomGap <= 12, `pill tap must re-pin the bottom, gap ${repinned.bottomGap}`);
	assert.ok(!repinned.pillShown, "pill must hide once the bottom re-pins");
	assert.ok(repinned.shortVisible, "the short reply must render once the view returns down");

	fixture.pushAssistant("Short reply while glued again.");
	await page.waitFor("document.body.innerText.includes('while glued again')", "post re-pin reply");
	await sleep(350);
	const reglued = await page.evaluate(
		"(() => { const sc = document.querySelector('[data-virtuoso-scroller]'); return sc.scrollHeight - (sc.scrollTop + sc.clientHeight); })()",
	);
	assert.ok(reglued <= 12, `bottom follow must resume after re-pin, gap ${reglued}`);
});
