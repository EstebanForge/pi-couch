import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	connectStream,
	nextBackoffMs,
	type Stream,
	type StreamHandlers,
	type StreamStatus,
} from "./stream";

class FakeEventTarget extends EventTarget {
	private listeners = new Map<string, Set<EventListenerOrEventListenerObject>>();

	override addEventListener(
		type: string,
		callback: EventListenerOrEventListenerObject | null,
		options?: AddEventListenerOptions | boolean,
	): void {
		super.addEventListener(type, callback, options);
		if (!callback) return;
		const listeners = this.listeners.get(type) ?? new Set();
		listeners.add(callback);
		this.listeners.set(type, listeners);
	}

	override removeEventListener(
		type: string,
		callback: EventListenerOrEventListenerObject | null,
		options?: EventListenerOptions | boolean,
	): void {
		super.removeEventListener(type, callback, options);
		if (callback) this.listeners.get(type)?.delete(callback);
	}

	listenerCount(type: string): number {
		return this.listeners.get(type)?.size ?? 0;
	}
}

class FakeDocument extends FakeEventTarget {
	visibilityState: "visible" | "hidden" = "visible";
}

class FakeEventSource {
	static instances: FakeEventSource[] = [];

	readonly url: string;
	onopen: ((event: Event) => void) | null = null;
	onmessage: ((event: MessageEvent) => void) | null = null;
	onerror: ((event: Event) => void) | null = null;
	closed = false;
	private listeners = new Map<string, Set<(event: MessageEvent) => void>>();

	constructor(url: string) {
		this.url = url;
		FakeEventSource.instances.push(this);
	}

	addEventListener(name: string, handler: (event: MessageEvent) => void): void {
		const handlers = this.listeners.get(name) ?? new Set();
		handlers.add(handler);
		this.listeners.set(name, handlers);
	}

	close(): void {
		this.closed = true;
	}

	open(): void {
		if (this.closed) throw new Error("cannot open a closed fake EventSource");
		this.onopen?.(new Event("open"));
	}

	fail(): void {
		if (this.closed) throw new Error("cannot fail a closed fake EventSource");
		this.onerror?.(new Event("error"));
	}

	emit(name: string, data: string): void {
		if (this.closed) throw new Error("cannot emit from a closed fake EventSource");
		const event = new MessageEvent(name, { data });
		if (name === "message") this.onmessage?.(event);
		for (const handler of this.listeners.get(name) ?? []) handler(event);
	}
}

describe("nextBackoffMs", () => {
	it("doubles from 250ms and caps at 5s", () => {
		expect(nextBackoffMs(0)).toBe(250);
		expect(nextBackoffMs(1)).toBe(500);
		expect(nextBackoffMs(2)).toBe(1000);
		expect(nextBackoffMs(3)).toBe(2000);
		expect(nextBackoffMs(4)).toBe(4000);
		expect(nextBackoffMs(5)).toBe(5000);
		expect(nextBackoffMs(50)).toBe(5000);
	});
});

describe("connectStream lifecycle", () => {
	let fakeDocument: FakeDocument;
	let fakeWindow: FakeEventTarget;
	let streams: Stream[];

	const connect = (handlers: StreamHandlers): Stream => {
		const stream = connectStream("/api/stream/test", handlers);
		streams.push(stream);
		return stream;
	};

	const source = (index: number): FakeEventSource => {
		const instance = FakeEventSource.instances[index];
		if (!instance) throw new Error(`missing fake EventSource ${index}`);
		return instance;
	};

	beforeEach(() => {
		vi.useFakeTimers();
		FakeEventSource.instances = [];
		streams = [];
		fakeDocument = new FakeDocument();
		fakeWindow = new FakeEventTarget();
		vi.stubGlobal("document", fakeDocument);
		vi.stubGlobal("window", fakeWindow);
		vi.stubGlobal("EventSource", FakeEventSource);
	});

	afterEach(() => {
		for (const stream of streams) stream.close();
		vi.clearAllTimers();
		vi.useRealTimers();
		vi.unstubAllGlobals();
	});

	it("reconnects after errors with backoff and delivers frames from the new source", () => {
		const events: Array<[string, string]> = [];
		const statuses: StreamStatus[] = [];
		connect({
			onEvent: (name, data) => events.push([name, data]),
			onStatus: (status) => statuses.push(status),
		});
		const first = source(0);
		expect(first.url).toBe("/api/stream/test");

		first.fail();
		expect(first.closed).toBe(true);
		expect(statuses.at(-1)).toBe("offline");
		vi.advanceTimersByTime(249);
		expect(FakeEventSource.instances).toHaveLength(1);
		vi.advanceTimersByTime(1);
		expect(FakeEventSource.instances).toHaveLength(2);

		source(1).fail();
		vi.advanceTimersByTime(499);
		expect(FakeEventSource.instances).toHaveLength(2);
		vi.advanceTimersByTime(1);
		expect(FakeEventSource.instances).toHaveLength(3);

		source(2).open();
		source(2).emit("reset", '{"sessions":[]}');
		expect(events).toEqual([["reset", '{"sessions":[]}']]);
		expect(statuses.at(-1)).toBe("open");
	});

	it("forces a reconnect after 45 seconds of silence and resets the clock on frames", () => {
		connect({ onEvent: vi.fn() });
		const first = source(0);
		first.open();

		vi.advanceTimersByTime(45_000);
		expect(first.closed).toBe(false);
		vi.advanceTimersByTime(5_000);
		expect(first.closed).toBe(true);
		vi.advanceTimersByTime(250);
		expect(FakeEventSource.instances).toHaveLength(2);

		const second = source(1);
		second.open();
		vi.advanceTimersByTime(40_000);
		second.emit("ping", "");
		vi.advanceTimersByTime(45_000);
		expect(second.closed).toBe(false);
		vi.advanceTimersByTime(5_000);
		expect(second.closed).toBe(true);
	});

	it("closes the active source in the background and recovers when visible", () => {
		const statuses: StreamStatus[] = [];
		connect({ onEvent: vi.fn(), onStatus: (status) => statuses.push(status) });
		const first = source(0);
		first.open();

		fakeDocument.visibilityState = "hidden";
		fakeDocument.dispatchEvent(new Event("visibilitychange"));
		expect(first.closed).toBe(true);
		expect(statuses.at(-1)).toBe("offline");
		vi.advanceTimersByTime(60_000);
		expect(FakeEventSource.instances).toHaveLength(1);

		fakeDocument.visibilityState = "visible";
		fakeDocument.dispatchEvent(new Event("visibilitychange"));
		expect(FakeEventSource.instances).toHaveLength(2);
		expect(statuses.at(-1)).toBe("connecting");
	});

	it("cancels a pending reconnect when backgrounded", () => {
		connect({ onEvent: vi.fn() });
		source(0).fail();
		expect(vi.getTimerCount()).toBe(2);

		fakeDocument.visibilityState = "hidden";
		fakeDocument.dispatchEvent(new Event("visibilitychange"));
		expect(vi.getTimerCount()).toBe(1);
		vi.advanceTimersByTime(60_000);
		expect(FakeEventSource.instances).toHaveLength(1);

		fakeDocument.visibilityState = "visible";
		fakeDocument.dispatchEvent(new Event("visibilitychange"));
		expect(FakeEventSource.instances).toHaveLength(2);
	});

	it.each(["online", "pageshow"])("%s wakes a backgrounded stream", (eventName) => {
		connect({ onEvent: vi.fn() });
		const first = source(0);
		fakeDocument.visibilityState = "hidden";
		fakeDocument.dispatchEvent(new Event("visibilitychange"));
		expect(first.closed).toBe(true);

		fakeDocument.visibilityState = "visible";
		fakeWindow.dispatchEvent(new Event(eventName));
		expect(FakeEventSource.instances).toHaveLength(2);
	});

	it("close releases active and pending sources, timers, and wake listeners", () => {
		const activeStream = connect({ onEvent: vi.fn() });
		const activeSource = source(0);
		const pendingStream = connect({ onEvent: vi.fn() });
		const pendingSource = source(1);
		pendingSource.fail();
		expect(vi.getTimerCount()).toBe(3);

		activeStream.close();
		pendingStream.close();
		expect(activeSource.closed).toBe(true);
		expect(pendingSource.closed).toBe(true);
		expect(vi.getTimerCount()).toBe(0);
		expect(fakeDocument.listenerCount("visibilitychange")).toBe(0);
		expect(fakeWindow.listenerCount("online")).toBe(0);
		expect(fakeWindow.listenerCount("pageshow")).toBe(0);

		fakeDocument.visibilityState = "visible";
		fakeDocument.dispatchEvent(new Event("visibilitychange"));
		fakeWindow.dispatchEvent(new Event("online"));
		fakeWindow.dispatchEvent(new Event("pageshow"));
		vi.advanceTimersByTime(60_000);
		expect(FakeEventSource.instances).toHaveLength(2);
	});
});
