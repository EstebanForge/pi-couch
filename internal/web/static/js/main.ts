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
