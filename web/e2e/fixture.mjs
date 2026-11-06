import { readFile, stat } from "node:fs/promises";
import { createServer } from "node:http";
import { dirname, extname, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const demoPath = "/tmp/demo/session.jsonl";
const demoSession = {
	key: "demo",
	path: demoPath,
	id: "demo-id",
	cwd: "/tmp/demo",
	name: "Fixture session",
	first_user: "First user line of the fixture",
	mtime: "2026-11-04T12:00:00Z",
	version: 3,
	entries: 5,
	total_tokens: 12345,
	total_cost: 0.02,
	model: "fixture/fake-model",
	thinking: "medium",
};

const json = (res, status, body) => {
	res.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
	res.end(JSON.stringify(body));
};

const mime = {
	".css": "text/css",
	".gif": "image/gif",
	".html": "text/html",
	".ico": "image/x-icon",
	".jpg": "image/jpeg",
	".js": "text/javascript",
	".json": "application/json",
	".png": "image/png",
	".svg": "image/svg+xml",
	".webmanifest": "application/manifest+json",
	".webp": "image/webp",
};

async function readBody(req) {
	let body = "";
	for await (const chunk of req) body += chunk;
	return body ? JSON.parse(body) : {};
}

const sleep = (ms) => new Promise((resolveSleep) => setTimeout(resolveSleep, ms));

export async function startFixture() {
	const recorded = JSON.parse(await readFile(resolve(here, "../dev/fixture-session.json"), "utf8"));
	const state = {
		dialog: null,
		todos: [],
		status: "working",
		model: "fixture/fake-model",
		thinking: "medium",
		askAnswers: [],
		prompts: [],
		agents: [],
		failCreates: 0,
		uploads: 0,
		activeUploads: 0,
		maxActiveUploads: 0,
		uploadDelay: 120,
	};
	const sessions = [structuredClone(demoSession)];
	const transcriptStreams = new Set();
	const sessionStreams = new Set();

	const sessionSnapshot = () => ({
		sessions,
		total: sessions.length,
		badges: Object.fromEntries(sessions.map((session) => [session.path, state.status])),
		driven: Object.fromEntries(
			sessions.map((session) => [
				session.path,
				{ status: state.status, title: session.name, label: "E2E" },
			]),
		),
		awaiting: Object.fromEntries(sessions.map((session) => [session.path, state.dialog !== null])),
	});

	const transcriptStatus = () => ({
		status: state.status,
		model: state.model,
		thinking: state.thinking,
		awaiting: state.dialog !== null,
		dialog: state.dialog,
		todos: state.todos,
		queue: [],
	});

	const writeEvent = (res, event, data) => {
		res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
	};
	const openSSE = (res, streams) => {
		res.writeHead(200, {
			"Content-Type": "text/event-stream",
			"Cache-Control": "no-cache",
			Connection: "keep-alive",
		});
		streams.add(res);
		res.on("close", () => streams.delete(res));
	};
	const broadcast = () => {
		for (const res of transcriptStreams) writeEvent(res, "status", transcriptStatus());
		for (const res of sessionStreams) writeEvent(res, "reset", sessionSnapshot());
	};
	const reset = () => {
		state.dialog = null;
		state.todos = [];
		state.status = "working";
		state.model = "fixture/fake-model";
		state.thinking = "medium";
		state.askAnswers.length = 0;
		state.prompts.length = 0;
		state.agents.length = 0;
		state.failCreates = 0;
		state.uploads = 0;
		state.activeUploads = 0;
		state.maxActiveUploads = 0;
		state.uploadDelay = 120;
		sessions.splice(1);
	};
	const closeDialog = () => {
		state.dialog = null;
		state.status = "done";
		broadcast();
	};

	const server = createServer(async (req, res) => {
		try {
			const url = new URL(req.url ?? "/", "http://fixture");
			const path = url.pathname;
			if (path === "/api/sessions" && req.method === "GET") {
				json(res, 200, sessionSnapshot());
				return;
			}
			if (path === "/api/stream/sessions") {
				openSSE(res, sessionStreams);
				writeEvent(res, "reset", sessionSnapshot());
				return;
			}
			if (/^\/api\/stream\/transcript\/[^/]+$/.test(path)) {
				openSSE(res, transcriptStreams);
				writeEvent(res, "reset", {
					items: recorded.map((item, index) => ({ ...item, seq: index + 1 })),
					...transcriptStatus(),
					meta: {
						name: "Fixture session",
						first_user: "First user line",
						cwd: "/tmp/demo",
					},
				});
				return;
			}
			if (path === "/api/version") {
				json(res, 200, { version: "e2e", home: "/tmp/demo", driver: "herdr" });
				return;
			}
			if (path === "/api/workspaces") {
				json(res, 200, [{ workspace_id: "w1", label: "Forge", number: 1 }]);
				return;
			}
			if (path === "/api/dirs") {
				json(res, 200, {
					path: "/tmp/demo",
					parent: "/tmp",
					truncated: false,
					dirs: [{ name: "project", path: "/tmp/demo/project" }],
				});
				return;
			}
			if (path === "/api/models") {
				json(res, 200, [
					{ id: "fixture/fake-model", levels: ["low", "medium", "high"] },
					{ id: "fixture/other-model", levels: ["off"] },
				]);
				return;
			}
			if (path === "/api/models/refresh" && req.method === "POST") {
				json(res, 200, [{ id: "fixture/fake-model", levels: ["low", "medium", "high"] }]);
				return;
			}
			if (path === "/api/upload" && req.method === "POST") {
				for await (const _chunk of req) {
					// Drain multipart data before answering.
				}
				state.uploads += 1;
				const upload = state.uploads;
				state.activeUploads += 1;
				state.maxActiveUploads = Math.max(state.maxActiveUploads, state.activeUploads);
				await sleep(state.uploadDelay);
				state.activeUploads -= 1;
				json(res, 200, { path: `/tmp/demo/upload-${upload}` });
				return;
			}
			if (path === "/api/agents" && req.method === "POST") {
				const body = await readBody(req);
				state.agents.push(body);
				if (state.failCreates > 0) {
					state.failCreates -= 1;
					json(res, 502, { error: "create failed" });
					return;
				}
				if (!sessions.some((session) => session.key === "created-demo")) {
					sessions.unshift({
						...structuredClone(demoSession),
						key: "created-demo",
						path: "/tmp/demo/created.jsonl",
						name: "Created agent",
					});
				}
				json(res, 200, { key: "created-demo" });
				return;
			}
			const preview = path.match(/^\/api\/sessions\/[^/]+\/preview$/);
			if (preview) {
				json(res, 200, {
					title: "Fixture session",
					status: state.status,
					awaiting: state.dialog !== null,
					lines: [{ seq: 1, who: "assistant", text: "Fixture preview" }],
				});
				return;
			}
			const ask = path.match(/^\/api\/sessions\/([^/]+)\/ask\/answer$/);
			if (ask && req.method === "POST") {
				state.askAnswers.push(await readBody(req));
				json(res, 200, { ok: true });
				setTimeout(closeDialog, 50);
				return;
			}
			const prompt = path.match(/^\/api\/sessions\/([^/]+)\/prompt$/);
			if (prompt && req.method === "POST") {
				state.prompts.push(await readBody(req));
				json(res, 200, { ok: true });
				return;
			}
			const model = path.match(/^\/api\/sessions\/([^/]+)\/(model|thinking)$/);
			if (model && req.method === "POST") {
				const body = await readBody(req);
				if (model[2] === "model") state.model = body.id;
				else state.thinking = body.level;
				json(res, 200, { ok: true });
				broadcast();
				return;
			}
			if (path.startsWith("/api/")) {
				json(res, 404, { error: "notfound" });
				return;
			}

			const assets = resolve(here, "../../internal/web/app");
			const requested = resolve(assets, `.${decodeURIComponent(path)}`);
			if (requested !== assets && !requested.startsWith(`${assets}${sep}`)) {
				json(res, 400, { error: "badpath" });
				return;
			}
			let file = requested;
			try {
				if ((await stat(file)).isDirectory()) file = resolve(file, "index.html");
			} catch {
				file = resolve(assets, "index.html");
			}
			let body;
			try {
				body = await readFile(file);
			} catch {
				body = await readFile(resolve(assets, "index.html"));
			}
			res.writeHead(200, {
				"Content-Type": mime[extname(file)] ?? "application/octet-stream",
				"Cache-Control": "no-store",
			});
			res.end(body);
		} catch (error) {
			json(res, 500, { error: error instanceof Error ? error.message : "fixture error" });
		}
	});
	await new Promise((resolveListen) => server.listen(0, "127.0.0.1", resolveListen));
	const address = server.address();
	if (!address || typeof address === "string") throw new Error("fixture has no TCP port");

	return {
		origin: `http://127.0.0.1:${address.port}`,
		state,
		reset,
		openAsk(questions, id = "ask-e2e") {
			state.dialog = { id, kind: "ask", title: questions[0]?.header ?? "Question", questions };
			state.status = "blocked";
		},
		setTodos(todos) {
			state.todos = todos;
			broadcast();
		},
		close: async () => {
			for (const res of [...transcriptStreams, ...sessionStreams]) res.end();
			server.closeAllConnections();
			await new Promise((resolveClose) => server.close(resolveClose));
		},
	};
}
