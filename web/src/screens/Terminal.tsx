// Terminal.tsx — /sessions/{key}/terminal: the pi TUI mirrored over
// SSE. The stream is the legacy /stream/terminal/{key} (term frames
// carry a whole-screen pane.read snapshot per revision, gone means no
// driver). Input never rides xterm's textarea (that is where Android
// IME composition breaks); it rides the key row and the input box
// through the terminal input endpoint, behind a take-the-wheel gate.
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { Toast } from "konsta/react";
import { useCallback, useEffect, useRef, useState } from "react";
import { type SessionsSnapshot, sendKeys, sendTextInput } from "../api";
import { ESC_SETTLE_MS } from "../chat/quick-keys";
import { ScreenHeader } from "../components/ScreenHeader";
import { connectStream, type Stream } from "../stream";

// Dracula palette for the grid; same values as index.css tokens.
const TERM_THEME = {
	background: "#282a36",
	foreground: "#f8f8f2",
	cursor: "#f8f8f2",
	cursorAccent: "#282a36",
	selectionBackground: "#44475a",
	black: "#21222c",
	red: "#ff5555",
	green: "#50fa7b",
	yellow: "#f1fa8c",
	blue: "#bd93f9",
	magenta: "#ff79c6",
	cyan: "#8be9fd",
	white: "#f8f8f2",
	brightBlack: "#6272a4",
	brightRed: "#ff6e6e",
	brightGreen: "#69ff94",
	brightYellow: "#ffffa5",
	brightBlue: "#d6acff",
	brightMagenta: "#ff92df",
	brightCyan: "#a4ffff",
	brightWhite: "#ffffff",
};

// The terminal's own key row: the chat strip minus Alt+Enter and Ctrl+D
// (they belong to the composer and the close dialog). Ctrl+C is the
// ADR-0001 hard interrupt, so it carries the same working/blocked gate
// as the overflow Stop; on an idle pi prompt it would exit pi.
type TermKey = { id: string; label: string; keys: string[]; enabled: (status: string) => boolean };

const hasDriver = (status: string) => status !== "";

const TERM_KEYS: TermKey[] = [
	{ id: "esc", label: "Esc", keys: ["esc"], enabled: (s) => s === "working" || s === "blocked" },
	{ id: "enter", label: "⏎", keys: ["enter"], enabled: hasDriver },
	{ id: "up", label: "↑", keys: ["up"], enabled: hasDriver },
	{ id: "down", label: "↓", keys: ["down"], enabled: hasDriver },
	{ id: "left", label: "←", keys: ["left"], enabled: hasDriver },
	{ id: "right", label: "→", keys: ["right"], enabled: hasDriver },
	{
		id: "ctrlc",
		label: "^C",
		keys: ["ctrl+c"],
		enabled: (s) => s === "working" || s === "blocked",
	},
];

export default function TerminalScreen({ sessionKey }: { sessionKey: string }) {
	const wrapRef = useRef<HTMLDivElement | null>(null);
	const termRef = useRef<Terminal | null>(null);
	const fitRef = useRef<FitAddon | null>(null);
	const [gone, setGone] = useState(false);
	const [wheel, setWheel] = useState(false);
	const [status, setStatus] = useState("");
	const [draft, setDraft] = useState("");
	const [toast, setToast] = useState("");
	const toastTimer = useRef(0);

	const say = useCallback((msg: string) => {
		setToast(msg);
		window.clearTimeout(toastTimer.current);
		toastTimer.current = window.setTimeout(() => setToast(""), 2500);
	}, []);

	// One promise chain per session: taps stay ordered; herdr holds a
	// lone Esc ~150ms on the pty, so settle after one.
	const chain = useRef<Promise<void>>(Promise.resolve());
	const pushKeys = useCallback(
		(keys: string[]) => {
			chain.current = chain.current.then(async () => {
				const r = await sendKeys(sessionKey, keys);
				if (!r.ok) {
					say("Key not sent");
					return;
				}
				if (keys[0] === "esc") {
					await new Promise((resolve) => setTimeout(resolve, ESC_SETTLE_MS));
				}
			});
			return chain.current;
		},
		[sessionKey, say],
	);

	const sendText = () => {
		const text = draft;
		if (!text.trim() || !wheel) return;
		void sendTextInput(sessionKey, text).then((r) => {
			if (r.ok) setDraft("");
			else if (r.error === "notdriven") say("No driver for this session");
			else say("Input not sent");
		});
	};

	// The grid: created once, refit on resize. No onData — the screen
	// never takes the soft keyboard; all input is the key row and box.
	useEffect(() => {
		const host = wrapRef.current;
		if (!host) return;
		const term = new Terminal({
			convertEol: false,
			fontSize: 12,
			fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
			scrollback: 0,
			theme: TERM_THEME,
		});
		const fit = new FitAddon();
		term.loadAddon(fit);
		term.open(host);
		fit.fit();
		termRef.current = term;
		fitRef.current = fit;
		const ro = new ResizeObserver(() => {
			try {
				fit.fit();
			} catch {
				// fit can throw while the host is display:none mid-nav
			}
		});
		ro.observe(host);
		return () => {
			ro.disconnect();
			term.dispose();
			termRef.current = null;
			fitRef.current = null;
		};
	}, []);

	// The mirror stream: a term frame is a whole-screen snapshot (the
	// server hash-gates it — herdr's per-connection revision reads 0 on
	// our one-shot connections). gone ends it for good.
	useEffect(() => {
		let stream: Stream | null = null;
		const run = () => {
			stream = connectStream(`/stream/terminal/${encodeURIComponent(sessionKey)}`, {
				events: ["term", "gone", "ping"],
				onEvent: (name, data) => {
					if (name === "gone") {
						setGone(true);
						stream?.close();
						return;
					}
					if (name !== "term") return;
					let frame: { text?: string; revision?: number };
					try {
						frame = JSON.parse(data) as typeof frame;
					} catch {
						return;
					}
					if (typeof frame.text !== "string") return;
					const term = termRef.current;
					if (!term) return;
					setGone(false);
					term.reset();
					term.write(frame.text);
				},
			});
		};
		run();
		return () => stream?.close();
	}, [sessionKey]);

	// Driver status rides the sessions stream; an agentless session has
	// no badge, which disables every key.
	useEffect(() => {
		const stream = connectStream(`/api/stream/sessions`, {
			events: ["reset"],
			onEvent: (_name, data) => {
				let snap: SessionsSnapshot;
				try {
					snap = JSON.parse(data) as SessionsSnapshot;
				} catch {
					return;
				}
				const s = snap.sessions.find((x) => x.key === sessionKey);
				setStatus(s ? (snap.badges[s.path] ?? "") : "");
			},
		});
		return () => stream.close();
	}, [sessionKey]);

	const back = () => history.back();

	return (
		<div
			className="mx-auto flex min-h-dvh max-w-3xl flex-col"
			style={{ paddingBottom: "var(--safe-bottom, 0px)", paddingTop: "64px" }}
		>
			<ScreenHeader
				title="Terminal"
				subtitle={gone ? "no driver" : wheel ? "wheel" : "peek"}
				onBack={back}
			/>

			<main className="flex min-h-0 flex-1 flex-col px-3 pb-2">
				<div className="flex items-center justify-between pb-2">
					<button
						type="button"
						onClick={() => setWheel((w) => !w)}
						className={`rounded-full px-4 py-2 text-[13px] font-medium active:scale-[0.98] ${wheel ? "bg-primary text-primary-content" : "bg-base-200 text-base-content"}`}
					>
						{wheel ? "Wheel: on" : "Take the wheel"}
					</button>
					<span className="text-xs text-muted">{status ? `agent ${status}` : "read-only"}</span>
				</div>

				<div
					ref={wrapRef}
					className={`min-h-[46dvh] flex-1 overflow-hidden rounded-2xl border border-base-300 p-1 ${wheel ? "" : "opacity-80"}`}
				/>

				{gone && (
					<p className="mt-3 rounded-xl bg-base-200 p-4 text-sm text-muted">
						No driver runs this session. Start one from the desk; this screen follows once the agent
						exists.
					</p>
				)}

				<div
					className={`mt-2 flex gap-2 overflow-x-auto pb-1 ${wheel ? "" : "pointer-events-none opacity-40"}`}
					style={{ scrollbarWidth: "none", touchAction: "manipulation" }}
				>
					{TERM_KEYS.map((k) => {
						const on = wheel && k.enabled(status);
						return (
							<button
								key={k.id}
								type="button"
								disabled={!on}
								onPointerDown={(e) => {
									// A key tap must not blur the input box or drop
									// the soft keyboard.
									e.preventDefault();
								}}
								onMouseDown={(e) => e.preventDefault()}
								onClick={() => void pushKeys(k.keys)}
								className={`shrink-0 rounded-lg border border-b-4 border-base-300 bg-base-200 px-4 py-2.5 font-mono text-[14px] ${on ? "text-base-content active:translate-y-px active:border-b-2" : "text-muted"}`}
							>
								{k.label}
							</button>
						);
					})}
				</div>

				<form
					className={`mt-2 flex gap-2 ${wheel ? "" : "pointer-events-none opacity-40"}`}
					onSubmit={(e) => {
						e.preventDefault();
						sendText();
					}}
				>
					<input
						type="text"
						value={draft}
						onChange={(e) => setDraft(e.target.value)}
						placeholder={wheel ? "Type into the TUI…" : "Take the wheel to type"}
						spellCheck={false}
						autoCorrect="off"
						autoCapitalize="off"
						className="min-w-0 flex-1 rounded-xl bg-base-200 px-3 py-2.5 font-mono text-[14px]"
					/>
					<button
						type="submit"
						disabled={!wheel || !draft.trim()}
						className="shrink-0 rounded-xl bg-primary px-4 py-2.5 text-[14px] font-medium text-primary-content disabled:opacity-40"
					>
						Type
					</button>
				</form>
			</main>

			{toast && (
				<Toast opened className="fixed bottom-24 left-1/2 z-50 -translate-x-1/2">
					{toast}
				</Toast>
			)}
		</div>
	);
}
