// Terminal island: xterm.js peek view over the raw SSE stream, with
// opt-in input (take-the-wheel) and a mobile key row. No framework.

const KEYMAP: Record<string, string[]> = {
	"\r": ["enter"],
	"\n": ["enter"],
	"\u007f": ["backspace"],
	"\t": ["tab"],
	"\u001b": ["esc"],
	"\u0003": ["ctrl+c"],
	"\u0004": ["ctrl+d"],
	"\u0001": ["ctrl+a"],
	"\u0005": ["ctrl+e"],
	"\u0015": ["ctrl+u"],
	"\u0017": ["ctrl+w"],
	"\u001b[A": ["up"],
	"\u001b[B": ["down"],
	"\u001b[C": ["right"],
	"\u001b[D": ["left"],
};

function initTerminal(): void {
	const el = document.getElementById("term");
	if (!el || !(window as any).Terminal) {
		return;
	}
	const key = el.dataset.key!;
	const term = new (window as any).Terminal({
		fontSize: 13,
		cursorBlink: false,
		convertEol: false,
		theme: {
			background: "#0f1115",
			foreground: "#e5e7eb",
		},
	});
	const fit = new (window as any).FitAddon.FitAddon();
	term.loadAddon(fit);
	term.open(el);
	try {
		fit.fit();
	} catch {
		// zero-size container before layout; stream still works
	}

	let wheel = false;
	const btn = document.getElementById("wheel-btn");
	btn?.addEventListener("click", () => {
		wheel = !wheel;
		btn.classList.toggle("on", wheel);
		btn.textContent = wheel ? "Wheel: ON" : "Take the wheel";
	});

	// Keystrokes must reach herdr in typing order: serialize every send on a
	// promise chain so a burst of keydowns cannot race, and flag failures so
	// the wheel does not silently lose input.
	let chain: Promise<void> = Promise.resolve();
	const send = (body: Record<string, unknown>): void => {
		chain = chain.then(async () => {
			const res = await fetch(`/sessions/${encodeURIComponent(key)}/terminal/input`, {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify(body),
			});
			if (!res.ok) {
				term.write("\r\n\x1b[31m[input failed]\x1b[0m\r\n");
			}
		}).catch(() => {
			term.write("\r\n\x1b[31m[input failed]\x1b[0m\r\n");
		});
	};

	term.onData((data: string) => {
		if (!wheel) {
			return;
		}
		if (KEYMAP[data]) {
			send({ keys: KEYMAP[data] });
		} else {
			send({ text: data });
		}
	});

	document.querySelectorAll<HTMLButtonElement>("#keyrow button[data-keys]").forEach((b) => {
		b.addEventListener("click", () => {
			if (!wheel) {
				return;
			}
			send({ keys: b.dataset.keys!.split(",") });
		});
	});

	const es = new EventSource(`/stream/terminal/${encodeURIComponent(key)}`);
	es.addEventListener("term", (e: MessageEvent) => {
		const { text } = JSON.parse(e.data);
		term.reset();
		term.write(text);
	});
	es.addEventListener("gone", () => {
		term.write("\r\n\x1b[2m[no herdr driver on this session]\x1b[0m\r\n");
		es.close();
	});
}

initTerminal();
initComments();
initPush();
initTranscriptSearch();

// In-transcript search (V10): count matches in #chat and cycle them with
// the browser's find, so a multi-MB DOM never gets walked node by node.
function initTranscriptSearch(): void {
	const input = document.getElementById("ts-input") as HTMLInputElement | null;
	if (!input) {
		return; // not the transcript page
	}
	const count = document.getElementById("ts-count")!;
	const matches = (q: string): number => {
		const hay = (document.getElementById("chat")?.innerText || "").toLowerCase();
		return q ? hay.split(q.toLowerCase()).length - 1 : 0;
	};
	// window.find is nonstandard but universal; it moves the selection, so
	// the browser scrolls the match into view for free. A miss resets to
	// the top once so the next press wraps around.
	const findFrom = (q: string, backwards: boolean): boolean => {
		if ((window as any).find?.(q, false, backwards, true, false, false, false)) {
			return true;
		}
		const sel = window.getSelection();
		sel?.removeAllRanges();
		if (backwards) {
			const end = document.getElementById("chat");
			end?.scrollIntoView(false);
		} else {
			window.scrollTo({ top: 0 });
		}
		return (window as any).find?.(q, false, backwards, true, false, false, false) ?? false;
	};
	const seek = (backwards: boolean): void => {
		const q = input.value.trim();
		count.textContent = q ? `${matches(q)} matches` : "";
		if (q) {
			findFrom(q, backwards);
		}
	};
	input.addEventListener("keydown", (e: KeyboardEvent) => {
		if (e.key === "Enter") {
			e.preventDefault();
			seek(e.shiftKey);
		}
	});
	document.getElementById("ts-next")?.addEventListener("click", () => seek(false));
	document.getElementById("ts-prev")?.addEventListener("click", () => seek(true));
	input.addEventListener("input", () => {
		count.textContent = input.value.trim() ? `${matches(input.value.trim())} matches` : "";
	});
}

// Push island: bell toggle on the sessions header. Registers /sw.js, mirrors
// the subscription to POST /api/push/subscribe, unsubscribes on opt-out.
// Permission is only asked on this explicit tap, never on page load.
function initPush(): void {
	const btn = document.getElementById("push-btn") as HTMLButtonElement | null;
	if (!btn || !("serviceWorker" in navigator) || !("PushManager" in window)) {
		return; // hidden by default; no support, no control
	}
	btn.hidden = false;
	const setLabel = (on: boolean): void => {
		btn.textContent = on ? "Notifications on" : "Notifications off";
		btn.classList.toggle("btn-primary", on);
	};
	void (async () => {
		try {
			const reg = await navigator.serviceWorker.register("/sw.js");
			let on = false;
			try {
				on = !!(await reg.pushManager.getSubscription());
			} catch {
				on = false; // headless/browser without a push service: control still renders
			}
			setLabel(on);
			btn.addEventListener("click", () => {
				void togglePush(reg, btn, setLabel);
			});
		} catch {
			btn.hidden = true; // SW refused (rare); control is dead weight
		}
	})();
}

async function togglePush(reg: ServiceWorkerRegistration, btn: HTMLButtonElement, setLabel: (on: boolean) => void): Promise<void> {
	btn.disabled = true;
	try {
		let cur: PushSubscription | null = null;
		try {
			cur = await reg.pushManager.getSubscription();
		} catch {
			cur = null;
		}
		if (cur) {
			await fetch("/api/push/unsubscribe", {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify({ endpoint: cur.endpoint }),
			});
			await cur.unsubscribe();
			setLabel(false);
			return;
		}
		const perm = await Notification.requestPermission();
		if (perm !== "granted") {
			alert("Notification permission was not granted, so push stays off.");
			return;
		}
		const keyRes = await fetch("/api/push/key");
		if (!keyRes.ok) {
			throw new Error("key");
		}
		const { publicKey } = (await keyRes.json()) as { publicKey: string };
		const sub = await reg.pushManager.subscribe({
			userVisibleOnly: true,
			applicationServerKey: urlBase64ToBytes(publicKey),
		});
		const res = await fetch("/api/push/subscribe", {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify(sub.toJSON()),
		});
		setLabel(res.ok);
	} catch {
		alert("Could not set up notifications. Try again.");
	} finally {
		btn.disabled = false;
	}
}

// applicationServerKey wants the raw 65 bytes; the server sends base64url.
function urlBase64ToBytes(b64: string): Uint8Array {
	const pad = "=".repeat((4 - (b64.length % 4)) % 4);
	const raw = atob((b64 + pad).replace(/-/g, "+").replace(/_/g, "/"));
	const out = new Uint8Array(raw.length);
	for (let i = 0; i < raw.length; i++) {
		out[i] = raw.charCodeAt(i);
	}
	return out;
}

// Comments island: per-device pending tray in localStorage, draft sheet on
// the diff view, and Send All through the server's herdr prompt. Pending
// data never leaves the device until Send All succeeds.
interface PendingCmt {
	file: string;
	line: number;
	text: string;
}

function trayKey(key: string): string {
	return `couch-pi-cmts:${key}`;
}

function loadTray(key: string): PendingCmt[] {
	try {
		const raw = localStorage.getItem(trayKey(key));
		return raw ? (JSON.parse(raw) as PendingCmt[]) : [];
	} catch {
		return [];
	}
}

function saveTray(key: string, cs: PendingCmt[]): void {
	localStorage.setItem(trayKey(key), JSON.stringify(cs));
}

function initComments(): void {
	const sheet = document.getElementById("cmt-sheet");
	if (sheet) {
		initDraftSheet(sheet);
	}
	const app = document.getElementById("cmt-app");
	if (app) {
		initTray(app as HTMLElement);
	}
}

// Draft sheet on the changes page: tap a diff line, save to the tray.
function initDraftSheet(sheet: HTMLElement): void {
	const key = sheet.dataset.key!;
	const file = sheet.dataset.file!;
	const ref = document.getElementById("cmt-ref")!;
	const text = document.getElementById("cmt-text") as HTMLTextAreaElement;
	let line = 0;
	document.querySelectorAll<HTMLButtonElement>("[data-cmt-line]").forEach((b) => {
		b.addEventListener("click", () => {
			line = parseInt(b.dataset.cmtLine || "0", 10);
			ref.textContent = `${file}:${line}`;
			text.value = "";
			sheet.hidden = false;
			text.focus();
		});
	});
	document.getElementById("cmt-cancel")?.addEventListener("click", () => {
		sheet.hidden = true;
	});
	document.getElementById("cmt-save")?.addEventListener("click", () => {
		const body = text.value.trim();
		if (!body) {
			return;
		}
		const cs = loadTray(key);
		cs.push({ file, line, text: body });
		saveTray(key, cs);
		sheet.hidden = true;
	});
}

// Tray on the Cmts page: pending cards, discard all, send all.
function initTray(app: HTMLElement): void {
	const key = app.dataset.key!;
	const pend = document.getElementById("cmt-pending")!;
	const render = (): void => {
		const cs = loadTray(key);
		pend.textContent = "";
		const head = document.createElement("div");
		head.className = "flex items-center justify-between gap-2 mb-2";
		const count = document.createElement("strong");
		count.textContent = cs.length === 1 ? "Pending (1 comment)" : `Pending (${cs.length} comments)`;
		head.appendChild(count);
		if (cs.length > 0) {
			const discard = document.createElement("button");
			discard.type = "button";
			discard.className = "btn btn-ghost btn-sm";
			discard.textContent = "Discard all";
			discard.addEventListener("click", () => {
				if (confirm("Discard all pending comments? This cannot be undone.")) {
					saveTray(key, []);
					render();
				}
			});
			const send = document.createElement("button");
			send.type = "button";
			send.className = "btn btn-primary btn-sm rounded-full";
			send.textContent = `Send all (${cs.length})`;
			send.addEventListener("click", () => void sendAll(cs, send));
			head.appendChild(discard);
			head.appendChild(send);
		}
		pend.appendChild(head);
		cs.forEach((c, i) => {
			const card = document.createElement("div");
			card.className = "card bg-base-200 rounded-xl shadow-none mb-2";
			const body = document.createElement("div");
			body.className = "card-body p-3 gap-1";
			const meta = document.createElement("div");
			meta.className = "text-xs opacity-60 flex justify-between font-mono";
			meta.textContent = `${c.file}:${c.line}`;
			const ta = document.createElement("textarea");
			ta.rows = 2;
			ta.className = "textarea textarea-sm w-full bg-base-100 rounded-lg";
			ta.value = c.text;
			ta.addEventListener("change", () => {
				cs[i].text = ta.value.trim();
				saveTray(key, cs);
			});
			const del = document.createElement("button");
			del.type = "button";
			del.className = "btn btn-ghost btn-sm text-error self-end";
			del.textContent = "Delete";
			del.addEventListener("click", () => {
				cs.splice(i, 1);
				saveTray(key, cs);
				render();
			});
			card.appendChild(body);
			body.appendChild(meta);
			body.appendChild(ta);
			body.appendChild(del);
			pend.appendChild(card);
		});
	};
	const sendAll = async (cs: PendingCmt[], btn: HTMLButtonElement): Promise<void> => {
		if (!confirm(`Send ${cs.length} comments to the driver? This submits a review prompt.`)) {
			return;
		}
		btn.disabled = true;
		try {
			const res = await fetch(`/sessions/${encodeURIComponent(key)}/comments`, {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify({ comments: cs }),
			});
			if (res.ok) {
				saveTray(key, []);
				location.href = `/sessions/${encodeURIComponent(key)}?sent=1`;
				return;
			}
			alert(
				res.status === 409
					? "Driver not running. Resume the session, then send again. Comments preserved."
					: "Failed to submit comments. Comments preserved in tray.",
			);
		} catch {
			alert("Failed to submit comments. Comments preserved in tray.");
		} finally {
			btn.disabled = false;
		}
	};
	render();
}
