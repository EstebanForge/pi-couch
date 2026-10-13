// stream.ts — JSON SSE connection with the reconnect rules from
// rewrite-plan.md: native EventSource never retries after an HTTP error
// (a 502 behind tailscale serve), so this helper closes and reconnects
// itself with capped backoff, wakes early on connectivity changes, and
// declares a silent stream dead.

export type StreamStatus = "connecting" | "open" | "offline";

export type StreamHandlers = {
	onEvent: (name: string, data: string) => void;
	onStatus?: (status: StreamStatus) => void;
	// SSE event names to listen for; defaults to the JSON stream set.
	events?: string[];
};

export type Stream = { close: () => void };

const MAX_BACKOFF_MS = 5000;
const BASE_BACKOFF_MS = 250;
const SILENCE_LIMIT_MS = 45_000;
const SILENCE_CHECK_MS = 5_000;

const EVENT_NAMES = ["reset", "append", "status", "ping"];

export function nextBackoffMs(attempt: number): number {
	return Math.min(MAX_BACKOFF_MS, BASE_BACKOFF_MS * 2 ** attempt);
}

export function connectStream(url: string, h: StreamHandlers): Stream {
	let es: EventSource | null = null;
	let attempt = 0;
	let timer: ReturnType<typeof setTimeout> | null = null;
	let closed = false;
	let lastSeen = Date.now();

	const status = (s: StreamStatus) => {
		if (!closed) h.onStatus?.(s);
	};

	const arm = () => {
		if (closed) return;
		status("connecting");
		const source = new EventSource(url);
		es = source;
		source.onopen = () => {
			attempt = 0;
			lastSeen = Date.now();
			status("open");
		};
		source.onmessage = (e) => {
			lastSeen = Date.now();
			status("open");
			h.onEvent("message", e.data ?? "");
		};
		for (const name of h.events ?? EVENT_NAMES) {
			source.addEventListener(name, (e) => {
				lastSeen = Date.now();
				status("open");
				h.onEvent(name, (e as MessageEvent).data ?? "");
			});
		}
		source.onerror = () => {
			source.close();
			if (es === source) es = null;
			schedule();
		};
	};

	const schedule = () => {
		if (closed) return;
		status("offline");
		const delay = nextBackoffMs(attempt);
		attempt += 1;
		timer = setTimeout(arm, delay);
	};

	const silence = setInterval(() => {
		if (es && Date.now() - lastSeen > SILENCE_LIMIT_MS) {
			// No frames for 45s: a zombie connection (NAT drop without a
			// TCP reset). Force a reconnect.
			es.close();
			es = null;
			schedule();
		}
	}, SILENCE_CHECK_MS);

	const wake = () => {
		if (closed) return;
		if (document.visibilityState === "hidden") {
			// Backgrounded WebView: drop the socket, reopen on visible.
			es?.close();
			es = null;
			if (timer !== null) {
				clearTimeout(timer);
				timer = null;
			}
			status("offline");
			return;
		}
		if (!es && timer === null) {
			attempt = 0;
			arm();
		}
	};
	document.addEventListener("visibilitychange", wake);
	window.addEventListener("online", wake);
	window.addEventListener("pageshow", wake);

	arm();

	return {
		close: () => {
			closed = true;
			if (timer !== null) clearTimeout(timer);
			clearInterval(silence);
			es?.close();
			document.removeEventListener("visibilitychange", wake);
			window.removeEventListener("online", wake);
			window.removeEventListener("pageshow", wake);
		},
	};
}
