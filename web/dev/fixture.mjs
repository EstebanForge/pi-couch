// web/dev/fixture.mjs — replay server for UI work without a live agent.
//
//   node web/dev/fixture.mjs            # API on 127.0.0.1:9918
//   COUCHPI_PROXY=127.0.0.1:9918 npm run dev   # vite proxies /api and /stream here
//
// Serves the same wire shape as the Go server: /api/sessions,
// /api/stream/sessions, /api/stream/transcript/demo (reset first, then
// scripted appends and a working→done status), /api/models. The
// transcript is synthetic: realistic item shapes, nothing private.

import { createServer } from "node:http";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const recorded = JSON.parse(readFileSync(join(here, "fixture-session.json"), "utf8"));

const PORT = Number(process.env.FIXTURE_PORT ?? 9918);
const sessionsSnapshot = {
	sessions: [
		{
			key: "demo",
			path: "/tmp/demo/.jsonl",
			id: "demo-id",
			cwd: "/tmp/demo",
			name: "Fixture session",
			first_user: "First user line of the fixture",
			mtime: new Date().toISOString(),
			version: 3,
			entries: recorded.length,
			total_tokens: 12345,
			total_cost: 0.02,
			model: "fixture/fake-model",
			thinking: "medium",
		},
	],
	badges: { demo: "working" },
};

const sse = (res) => {
	res.writeHead(200, {
		"Content-Type": "text/event-stream",
		"Cache-Control": "no-cache",
		Connection: "keep-alive",
	});
	return (event, data) => {
		res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
	};
};

const transcript = (res) => {
	const send = sse(res);
	let seq = 0;
	const withSeq = (items) => items.map((it) => ({ ...it, seq: ++seq }));
	send("reset", {
		items: withSeq(recorded),
		status: "working",
		model: "fixture/fake-model",
		thinking: "medium",
		meta: { name: "Fixture session", first_user: "First user line", cwd: "/tmp/demo" },
	});
	// Scripted growth: a thinking step, a tool call, then the answer.
	const steps = [
		{ kind: "thinking", text: "Thinking about the fixture request in detail…" },
		{ kind: "tool_call", text: "fixture.Arg", tool_name: "fixture_tool" },
		{ kind: "tool_result", text: "tool output line one\ntool output line two", tool_name: "fixture_tool" },
		{ kind: "text", text: "Fixture answer: the composer should show this text." },
	];
	let i = 0;
	const grow = setInterval(() => {
		if (i < steps.length) {
			send("append", { items: withSeq([steps[i++]]) });
		} else {
			clearInterval(grow);
			send("status", { status: "done", model: "fixture/fake-model", thinking: "medium" });
		}
	}, 2000);
	const beat = setInterval(() => send("ping", {}), 15000);
	res.on("close", () => {
		clearInterval(grow);
		clearInterval(beat);
	});
};

createServer((req, res) => {
	const url = new URL(req.url, "http://fixture");
	if (url.pathname === "/api/sessions") {
		res.writeHead(200, { "Content-Type": "application/json" });
		res.end(JSON.stringify(sessionsSnapshot));
		return;
	}
	if (url.pathname === "/api/stream/sessions") {
		const send = sse(res);
		send("reset", sessionsSnapshot);
		const beat = setInterval(() => send("ping", {}), 15000);
		res.on("close", () => clearInterval(beat));
		return;
	}
	if (url.pathname === "/api/stream/transcript/demo") {
		transcript(res);
		return;
	}
	if (url.pathname === "/api/models") {
		res.writeHead(200, { "Content-Type": "application/json" });
		res.end(JSON.stringify([{ id: "fixture/fake-model" }, { id: "fixture/other-model" }]));
		return;
	}
	res.writeHead(404, { "Content-Type": "application/json" });
	res.end('{"error":"notfound"}');
}).listen(PORT, "127.0.0.1", () => {
	console.log(`fixture API on http://127.0.0.1:${PORT} (session key: demo)`);
});
