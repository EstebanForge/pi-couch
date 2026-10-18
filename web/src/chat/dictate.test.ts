import { afterEach, describe, expect, it, vi } from "vitest";
import { dictationSupported, startDictation } from "./dictate";

type Bridge = {
	available: () => boolean;
	start: (lang: string) => void;
	stop: () => void;
};

type NativeCb = { onFinal: (t: string) => void; onError: () => void; onEnd: () => void };

function fakeWindow(bridge: Bridge | undefined): Record<string, unknown> {
	const w: Record<string, unknown> = {};
	if (bridge) w.CouchPiSpeech = bridge;
	return w;
}

function nativeInstalled(): boolean {
	return (window as unknown as Record<string, unknown>).__couchPiDictation !== undefined;
}

function nativeCb(): NativeCb {
	const w = window as unknown as { __couchPiDictation?: NativeCb };
	const cb = w.__couchPiDictation;
	if (!cb) throw new Error("native callbacks not installed");
	return cb;
}

afterEach(() => vi.unstubAllGlobals());

describe("dictation", () => {
	it("reports support through the native bridge when the web API is absent", () => {
		vi.stubGlobal("window", fakeWindow({ available: () => true, start: () => {}, stop: () => {} }));
		expect(dictationSupported()).toBe(true);
	});

	it("reports no support without web API or bridge", () => {
		vi.stubGlobal("window", fakeWindow(undefined));
		expect(dictationSupported()).toBe(false);
	});

	it("dictates through the native bridge: start, finals, stop", () => {
		let started = "";
		let stopped = false;
		vi.stubGlobal(
			"window",
			fakeWindow({
				available: () => true,
				start: (lang: string) => {
					started = lang;
				},
				stop: () => {
					stopped = true;
				},
			}),
		);
		const finals: string[] = [];
		let ended = 0;
		let errored = 0;
		const d = startDictation({
			onFinal: (t) => finals.push(t),
			onEnd: () => ended++,
			onError: () => errored++,
		});
		expect(d).not.toBeNull();
		expect(started).toBe(globalThis.navigator?.language ?? "en");
		nativeCb().onFinal("hello from the phone");
		expect(finals).toEqual(["hello from the phone"]);
		d!.stop();
		expect(stopped).toBe(true);
		// Stop uninstalls the callbacks: a late native event has nothing to
		// call, so a dead session cannot be resurrected.
		expect(nativeInstalled()).toBe(false);
		expect(ended).toBe(0);
		expect(errored).toBe(0);
	});

	it("surfaces a native error and drops later events", () => {
		vi.stubGlobal("window", fakeWindow({ available: () => true, start: () => {}, stop: () => {} }));
		let errored = 0;
		let ended = 0;
		startDictation({ onFinal: () => {}, onEnd: () => ended++, onError: () => errored++ });
		nativeCb().onError();
		// Error uninstalls the callbacks: nothing can fire afterwards.
		expect(errored).toBe(1);
		expect(nativeInstalled()).toBe(false);
		expect(ended).toBe(0);
	});

	it("prefers the native bridge when both web API and bridge exist", () => {
		// The Android WebView ships webkitSpeechRecognition without a working
		// backend, so the shell bridge must win over a present-but-dead
		// web constructor.
		let webConstructed = 0;
		let nativeStarted = false;
		const w: Record<string, unknown> = {
			webkitSpeechRecognition: class {
				constructor() {
					webConstructed++;
				}
				lang = "";
				continuous = false;
				interimResults = false;
				start() {}
				stop() {}
				onresult = null;
				onerror = null;
				onend = null;
			},
			CouchPiSpeech: {
				available: () => true,
				start: () => {
					nativeStarted = true;
				},
				stop: () => {},
			},
		};
		vi.stubGlobal("window", w);
		const d = startDictation({ onFinal: () => {}, onEnd: () => {}, onError: () => {} });
		expect(d).not.toBeNull();
		expect(nativeStarted).toBe(true);
		expect(webConstructed).toBe(0);
		expect(nativeInstalled()).toBe(true);
	});

	it("returns null when the bridge exists but has no recognizer", () => {
		vi.stubGlobal(
			"window",
			fakeWindow({ available: () => false, start: () => {}, stop: () => {} }),
		);
		expect(dictationSupported()).toBe(false);
		const d = startDictation({ onFinal: () => {}, onEnd: () => {}, onError: () => {} });
		expect(d).toBeNull();
	});
});
