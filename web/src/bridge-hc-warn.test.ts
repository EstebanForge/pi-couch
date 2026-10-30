// The unreachable-socket warn lives in its own file on purpose: the
// warn-once flag is per module instance, and socket errors from other
// tests' instances can land inside a shared worker's console spy.
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";

type Handler = (event: unknown, ctx?: unknown) => void;

function fakePi() {
	const handlers = new Map<string, Handler[]>();
	const pi = {
		events: { emit: () => {} },
		on: (event: string, h: Handler) => {
			const list = handlers.get(event) ?? [];
			list.push(h);
			handlers.set(event, list);
		},
	};
	const fire = (event: string, arg: unknown, ctx?: unknown) => {
		for (const h of handlers.get(event) ?? []) h(arg, ctx);
	};
	return { pi, fire };
}

const sessionCtx = {
	mode: "tui",
	sessionManager: { getSessionFile: () => "/home/me/.pi/sessions/s.jsonl" },
};

describe("couch-pi bridge: unreachable hc socket", () => {
	afterEach(() => {
		vi.unstubAllEnvs();
		vi.unstubAllGlobals();
		vi.restoreAllMocks();
	});

	it("warns once, not per session start", async () => {
		vi.stubEnv("HERDR_ENV", "");
		vi.stubEnv("HERDR_PANE_ID", "");
		vi.stubEnv("HC_TERMINAL_ID", "t9");
		vi.stubEnv("HC_TERMINAL_SOCKET", join(mkdtempSync(join(tmpdir(), "hc-none-")), "absent.sock"));
		vi.stubGlobal("fetch", vi.fn(async () => ({ ok: true, status: 200 })));
		vi.resetModules();
		const mod = await import("../../extensions/couch-pi-bridge");
		const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
		const { pi, fire } = fakePi();
		(mod.default as (pi: object) => void)(pi);
		fire("session_start", {}, sessionCtx);
		fire("session_start", {}, sessionCtx);

		await vi.waitFor(() => expect(errSpy).toHaveBeenCalledTimes(1));
		expect(errSpy.mock.calls[0][0]).toContain("hc socket");
	});
});
