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

	const send = async (body: Record<string, unknown>): Promise<void> => {
		await fetch(`/sessions/${key}/terminal/input`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify(body),
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

// Comments island: per-device pending tray in localStorage, draft sheet on
// the diff view, and Send All through the server's herdr prompt. Pending
// data never leaves the device until Send All succeeds.
interface PendingCmt {
	file: string;
	line: number;
	text: string;
}

function trayKey(key: string): string {
	return `piremote-cmts:${key}`;
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
		head.className = "tray-head";
		const count = document.createElement("strong");
		count.textContent = cs.length === 1 ? "Pending (1 comment)" : `Pending (${cs.length} comments)`;
		head.appendChild(count);
		if (cs.length > 0) {
			const discard = document.createElement("button");
			discard.type = "button";
			discard.textContent = "Discard all";
			discard.addEventListener("click", () => {
				if (confirm("Discard all pending comments? This cannot be undone.")) {
					saveTray(key, []);
					render();
				}
			});
			const send = document.createElement("button");
			send.type = "button";
			send.className = "primary";
			send.textContent = `Send all (${cs.length})`;
			send.addEventListener("click", () => void sendAll(cs, send));
			head.appendChild(discard);
			head.appendChild(send);
		}
		pend.appendChild(head);
		cs.forEach((c, i) => {
			const card = document.createElement("div");
			card.className = "cmt-card";
			const meta = document.createElement("div");
			meta.className = "cmt-meta";
			meta.textContent = `${c.file}:${c.line}`;
			const ta = document.createElement("textarea");
			ta.rows = 2;
			ta.value = c.text;
			ta.addEventListener("change", () => {
				cs[i].text = ta.value.trim();
				saveTray(key, cs);
			});
			const del = document.createElement("button");
			del.type = "button";
			del.textContent = "Delete";
			del.addEventListener("click", () => {
				cs.splice(i, 1);
				saveTray(key, cs);
				render();
			});
			card.appendChild(meta);
			card.appendChild(ta);
			card.appendChild(del);
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
