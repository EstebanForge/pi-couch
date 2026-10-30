import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import net from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type Handler = (event: unknown, ctx?: unknown) => void;

function fakePi() {
	const handlers = new Map<string, Handler[]>();
	const emitted: { name: string; data: unknown }[] = [];
	const pi = {
		events: { emit: (name: string, data: unknown) => emitted.push({ name, data }) },
		on: (event: string, h: Handler) => {
			const list = handlers.get(event) ?? [];
			list.push(h);
			handlers.set(event, list);
		},
	};
	const fire = (event: string, arg: unknown, ctx?: unknown) => {
		for (const h of handlers.get(event) ?? []) h(arg, ctx);
	};
	return { pi, fire, emitted };
}

const sessionCtx = {
	mode: "tui",
	sessionManager: { getSessionFile: () => "/home/me/.pi/sessions/s.jsonl" },
};

async function loadFactory() {
	vi.resetModules();
	const mod = await import("../../extensions/couch-pi-bridge");
	return mod.default as (pi: object) => void;
}

const startEvent = { type: "ui_prompt_start" as const, kind: "confirm" as const, title: "Commit?" };

// A real unix-socket stand-in for the hc core, recording every JSON-RPC
// line it receives.
async function fakeCore() {
	const path = join(mkdtempSync(join(tmpdir(), "hc-core-")), "core.sock");
	const lines: string[] = [];
	const server = net.createServer((socket) => {
		let buf = "";
		socket.on("data", (chunk: Buffer) => {
			buf += chunk.toString();
			let i: number;
			while ((i = buf.indexOf("\n")) >= 0) {
				const line = buf.slice(0, i);
				buf = buf.slice(i + 1);
				lines.push(line);
				// reply like the real core so label sockets close in-test
				try {
					const id = (JSON.parse(line) as { id: unknown }).id;
					socket.write(`${JSON.stringify({ jsonrpc: "2.0", id, result: {} })}\n`);
				} catch {
					// malformed line: recording is enough
				}
			}
		});
	});
	await new Promise<void>((resolve) => server.listen(path, resolve));
	return {
		path,
		lines,
		close: () => server.close(),
	};
}

async function labelRequests(core: Awaited<ReturnType<typeof fakeCore>>) {
	return vi.waitFor(() => {
		expect(core.lines.length).toBeGreaterThan(0);
		return core.lines.map((l) => JSON.parse(l) as { method: string; params: { terminal: string; labels: Record<string, string> } });
	});
}

async function postBodies() {
	const calls = vi.mocked(fetch).mock.calls;
	return calls.map(
		([, init]) => JSON.parse((init as RequestInit).body as string) as Record<string, unknown>,
	);
}

function authHeader(callIndex: number): string | undefined {
	const init = vi.mocked(fetch).mock.calls[callIndex][1] as RequestInit;
	return (init.headers as Record<string, string>).authorization;
}

describe("couch-pi bridge extension", () => {
	beforeEach(() => {
		vi.stubEnv("HERDR_ENV", "1");
		vi.stubEnv("HERDR_PANE_ID", "pane-1");
		vi.stubGlobal(
			"fetch",
			vi.fn(async () => ({ ok: true, status: 200 })),
		);
	});

	afterEach(() => {
		vi.unstubAllEnvs();
		vi.unstubAllGlobals();
	});

	it("sends the bearer token from the environment", async () => {
		vi.stubEnv("PICOUCH_ACCESS_TOKEN", "tok123");
		const factory = await loadFactory();
		const { pi, fire } = fakePi();
		factory(pi);
		fire("session_start", {}, sessionCtx);
		fire("ui_prompt_start", startEvent);

		expect(fetch).toHaveBeenCalledTimes(1);
		expect(authHeader(0)).toBe("Bearer tok123");
		const bodies = await postBodies();
		expect(bodies[0].phase).toBe("start");
		expect(bodies[0].kind).toBe("confirm");
		expect(typeof bodies[0].id).toBe("string");
	});

	it("falls back to the env file when the variable is unset", async () => {
		const home = mkdtempSync(join(tmpdir(), "bridge-test-"));
		mkdirSync(join(home, ".config/pi-couch"), { recursive: true });
		writeFileSync(join(home, ".config/pi-couch/env"), "OTHER=x\nPICOUCH_ACCESS_TOKEN=filetok\n");
		vi.stubEnv("HOME", home);
		vi.stubEnv("PICOUCH_ACCESS_TOKEN", "");

		const factory = await loadFactory();
		const { pi, fire } = fakePi();
		factory(pi);
		fire("session_start", {}, sessionCtx);
		fire("ui_prompt_start", startEvent);

		expect(fetch).toHaveBeenCalledTimes(1);
		expect(authHeader(0)).toBe("Bearer filetok");
	});

	it("recovers dialog state when a new session starts after a lost end", async () => {
		const factory = await loadFactory();
		const { pi, fire } = fakePi();
		factory(pi);
		fire("session_start", {}, sessionCtx);
		fire("ui_prompt_start", startEvent);
		// ui_prompt_end never arrives; a new session must not inherit the
		// stuck depth or the later dialog stays muted forever
		fire("session_start", {}, sessionCtx);
		fire("ui_prompt_start", startEvent);

		const bodies = await postBodies();
		expect(bodies).toHaveLength(2);
		expect(bodies[1].phase).toBe("start");
		expect(bodies[1].id).not.toBe(bodies[0].id);
	});

	it("carries the dialog id on the end dial and warns once on refusal", async () => {
		vi.stubEnv("PICOUCH_ACCESS_TOKEN", "tok123");
		const factory = await loadFactory();
		const { pi, fire } = fakePi();
		factory(pi);
		fire("session_start", {}, sessionCtx);
		fire("ui_prompt_start", startEvent);
		fire("ui_prompt_end", { type: "ui_prompt_end", kind: "confirm" });

		const bodies = await postBodies();
		expect(bodies[1].phase).toBe("end");
		expect(bodies[1].id).toBe(bodies[0].id);
		expect(authHeader(1)).toBe("Bearer tok123");
	});

	it("warns once, not per POST, when the server refuses the token", async () => {
		vi.stubEnv("PICOUCH_ACCESS_TOKEN", "wrong");
		vi.stubGlobal(
			"fetch",
			vi.fn(async () => ({ ok: false, status: 401 })),
		);
		const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
		const factory = await loadFactory();
		const { pi, fire } = fakePi();
		factory(pi);
		fire("session_start", {}, sessionCtx);
		fire("ui_prompt_start", startEvent);
		fire("ui_prompt_end", { type: "ui_prompt_end", kind: "confirm" });

		expect(fetch).toHaveBeenCalledTimes(2);
		await vi.waitFor(() => expect(errSpy).toHaveBeenCalledTimes(1));
		expect(errSpy.mock.calls[0][0]).toContain("401");
		errSpy.mockRestore();
	});

	describe("under herdr-core", () => {
		beforeEach(() => {
			vi.stubEnv("HERDR_ENV", "");
			vi.stubEnv("HERDR_PANE_ID", "");
		});

		it("labels its terminal with the session path", async () => {
			const core = await fakeCore();
			vi.stubEnv("HC_TERMINAL_ID", "t9");
			vi.stubEnv("HC_TERMINAL_SOCKET", core.path);
			const factory = await loadFactory();
			const { pi, fire } = fakePi();
			factory(pi);
			fire("session_start", {}, sessionCtx);

			const reqs = await labelRequests(core);
			expect(reqs[0].method).toBe("terminal.label");
			expect(reqs[0].params.terminal).toBe("t9");
			expect(reqs[0].params.labels["pi.session"]).toBe("/home/me/.pi/sessions/s.jsonl");
			core.close();
		});

		it("does not dial the hc socket outside hc", async () => {
			const core = await fakeCore();
			vi.stubEnv("HC_TERMINAL_ID", "");
			vi.stubEnv("HC_TERMINAL_SOCKET", "");
			const factory = await loadFactory();
			const { pi, fire } = fakePi();
			factory(pi);
			fire("session_start", {}, sessionCtx);
			await new Promise((r) => setTimeout(r, 50));
			expect(core.lines).toHaveLength(0);
			core.close();
		});

		it("relabels when a new session owns the terminal", async () => {
			const core = await fakeCore();
			vi.stubEnv("HC_TERMINAL_ID", "t9");
			vi.stubEnv("HC_TERMINAL_SOCKET", core.path);
			const factory = await loadFactory();
			const { pi, fire } = fakePi();
			factory(pi);
			fire("session_start", {}, sessionCtx);
			fire("session_start", {}, {
				mode: "tui",
				sessionManager: { getSessionFile: () => "/home/me/.pi/sessions/other.jsonl" },
			});

			const reqs = await labelRequests(core);
			expect(reqs).toHaveLength(2);
			expect(reqs[1].params.labels["pi.session"]).toBe("/home/me/.pi/sessions/other.jsonl");
			core.close();
		});

		it("still POSTs dialogs and skips the herdr-only emit", async () => {
			const core = await fakeCore();
			vi.stubEnv("HC_TERMINAL_ID", "t9");
			vi.stubEnv("HC_TERMINAL_SOCKET", core.path);
			const factory = await loadFactory();
			const { pi, fire, emitted } = fakePi();
			factory(pi);
			fire("session_start", {}, sessionCtx);
			fire("ui_prompt_start", startEvent);

			await postBodies();
			expect(fetch).toHaveBeenCalledTimes(1);
			expect(emitted.filter((e) => e.name === "herdr:blocked")).toHaveLength(0);
			core.close();
		});
	});
});
