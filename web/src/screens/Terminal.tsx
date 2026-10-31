// Terminal.tsx — /sessions/{key}/terminal: the pi TUI mirrored over
// SSE. The stream is /stream/terminal/{key}: delta frames carry raw
// base64 pty bytes from a `herdr terminal session observe` viewport
// (frame 0 is a full repaint), term frames are the legacy whole-screen
// pane.read snapshot fallback, gone means no driver. Input never rides
// xterm's textarea (that is where Android IME composition breaks); it
// rides the key row and the input box through the terminal input
// endpoint, behind a take-the-wheel gate.
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { Segmented, SegmentedButton, Toast } from "konsta/react";
import { useCallback, useEffect, useRef, useState } from "react";
import { type SessionsPatch, sendKeys, sendTextInput } from "../api";
import { ESC_SETTLE_MS } from "../chat/quick-keys";
import { ScreenHeader } from "../components/ScreenHeader";
import { useSessions } from "../state/sessions";
import { connectStream, type Stream } from "../stream";

// The terminal palette rides the active color scheme (theme.ts); the
// grid re-themes live when the picker switches schemes.
import { activeScheme, onSchemeChange } from "../theme";

// The terminal's own key row: the chat strip minus Alt+Enter and Ctrl+D
// (they belong to the composer and the close dialog). Ctrl+C is the
// ADR-0001 hard interrupt, so it carries the same working/blocked gate
// as the overflow Stop; on an idle pi prompt it would exit pi.
type TermKey = { id: string; label: string; keys: string[]; enabled: (status: string) => boolean };

const hasDriver = (status: string) => status !== "";

// Wide mode: herdr's observe viewport cannot rewrap pi's paint (it
// clips the cursor-addressed screen), so seeing the full width means
// requesting a wider grid and panning across it at a readable size.
const WIDE_COLS = 160;
const WIDE_FONT = 9;

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
	const [toast, setToast] = useState("");

	// Driver status rides the shared sessions store: stream frames are
	// patches, and a herdr-only flip (dialog opens) touches no session
	// file, so the status must come from applied patch state.
	const badges = useSessions((s) => s.badges);
	const sessions = useSessions((s) => s.sessions);
	const applySessions = useSessions((s) => s.apply);
	const loadSessions = useSessions((s) => s.load);
	const sessionMeta = sessions.find((x) => x.key === sessionKey);
	const status = sessionMeta ? (badges[sessionMeta.path] ?? "") : "";
	const [draft, setDraft] = useState("");
	const [dims, setDims] = useState({ cols: 100, rows: 30 });
	const [mode, setMode] = useState<"fit" | "wide">("fit");
	const building = useRef(false);
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

	// The grid: rebuilt per view mode, refit on resize. Wide mode holds
	// the full WIDE_COLS data grid at a fixed readable font and pans
	// horizontally (herdr's observe viewport clips pi's cursor-addressed
	// paint — nothing can rewrap it, so seeing it all means more columns,
	// not wrapping). No onData — the screen never takes the soft
	// keyboard; all input is the key row and box.
	useEffect(() => {
		const host = wrapRef.current;
		if (!host) return;
		building.current = true;
		const term = new Terminal({
			convertEol: false,
			fontSize: mode === "wide" ? WIDE_FONT : 12,
			fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
			scrollback: 0,
			theme: activeScheme().term,
		});
		const fit = new FitAddon();
		term.loadAddon(fit);
		term.open(host);
		if (mode === "wide") {
			// fixed readable font; only the rows follow the viewport —
			// fit() here would collapse the wide grid back to ~73 cols
			term.options.fontSize = WIDE_FONT;
			const prop = fit.proposeDimensions();
			term.resize(WIDE_COLS, Math.max(10, Math.min(200, prop?.rows ?? 30)));
		} else {
			fit.fit();
		}
		setDims({ cols: term.cols, rows: term.rows });
		// the mode rebuild rewrites the grid once; the ResizeObserver
		// must not feed intermediate sizes back into the stream
		requestAnimationFrame(() => {
			building.current = false;
		});
		termRef.current = term;
		fitRef.current = fit;
		const ro = new ResizeObserver(() => {
			if (building.current) return;
			try {
				if (mode === "wide") {
					// cols stay pinned to the wide data grid; only rows track
					const prop = fit.proposeDimensions();
					if (prop?.rows && prop.rows !== term.rows) {
						term.resize(term.cols, prop.rows);
						setDims({ cols: term.cols, rows: prop.rows });
					}
					return;
				}
				fit.fit();
				setDims((d) =>
					d.cols === term.cols && d.rows === term.rows ? d : { cols: term.cols, rows: term.rows },
				);
			} catch {
				// fit can throw while the host is display:none mid-nav
			}
		});
		ro.observe(host);
		const offScheme = onSchemeChange(() => {
			term.options.theme = activeScheme().term;
		});
		return () => {
			offScheme();
			ro.disconnect();
			term.dispose();
			termRef.current = null;
			fitRef.current = null;
		};
	}, [mode]);

	// The mirror stream: delta frames are incremental pty bytes (the
	// first one repaints the whole viewport, so reset once per connect);
	// term frames are the fallback's whole-screen snapshots. gone ends
	// it for good. Reconnecting on dims changes respawns the observe
	// viewport at the new grid.
	// A fresh EventSource (including every reconnect) gets frame 0 as
	// a full repaint, so the grid must reset on each open or the new
	// paint lands on top of the old screen's leftovers.
	useEffect(() => {
		let stream: Stream | null = null;
		let first = true;
		const run = () => {
			stream = connectStream(
				`/stream/terminal/${encodeURIComponent(sessionKey)}?cols=${dims.cols}&rows=${dims.rows}`,
				{
					events: ["delta", "term", "gone", "reset", "size", "ping"],
					onStatus: (status) => {
						if (status === "connecting") first = true;
					},
					onEvent: (name, data) => {
						if (name === "gone") {
							setGone(true);
							stream?.close();
							return;
						}
						const term = termRef.current;
						if (!term) return;
						if (name === "size") {
							// hc observe paints at the core's grid, not ours.
							let grid: { cols?: number; rows?: number };
							try {
								grid = JSON.parse(data) as typeof grid;
							} catch {
								return;
							}
							if (grid.cols && grid.rows) term.resize(grid.cols, grid.rows);
							return;
						}
						if (name === "reset") {
							// Evicted for stalling: clear and let the
							// reconnect's frame 0 repaint cleanly.
							term.reset();
							return;
						}
						if (name !== "delta" && name !== "term") return;
						setGone(false);
						if (name === "term") {
							let frame: { text?: string };
							try {
								frame = JSON.parse(data) as typeof frame;
							} catch {
								return;
							}
							if (typeof frame.text !== "string") return;
							term.reset();
							term.write(frame.text);
							return;
						}
						let frame: { bytes?: string };
						try {
							frame = JSON.parse(data) as typeof frame;
						} catch {
							return;
						}
						if (typeof frame.bytes !== "string") return;
						const bin = atob(frame.bytes);
						const arr = new Uint8Array(bin.length);
						for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
						if (first) {
							first = false;
							term.reset();
						}
						term.write(arr);
					},
				},
			);
		};
		run();
		return () => stream?.close();
	}, [sessionKey, dims]);

	// Driver status rides the sessions stream through the shared store;
	// frames are patches, so apply them instead of parsing snapshots.
	useEffect(() => {
		void loadSessions().catch(() => {});
		const stream = connectStream(`/api/stream/sessions`, {
			events: ["reset"],
			onEvent: (_name, data) => {
				let patch: SessionsPatch;
				try {
					patch = JSON.parse(data) as SessionsPatch;
				} catch {
					return;
				}
				applySessions(patch);
			},
		});
		return () => stream.close();
	}, [applySessions, loadSessions]);

	const back = () => history.back();

	return (
		<div
			className="flex min-h-dvh flex-col"
			style={{ paddingBottom: "var(--safe-bottom, 0px)", paddingTop: "64px" }}
		>
			<ScreenHeader
				title="Terminal"
				subtitle={gone ? "no driver" : wheel ? "wheel" : "peek"}
				onBack={back}
			/>

			<main className="flex min-h-0 flex-1 flex-col px-1 pb-2">
				<div className="flex items-center justify-between gap-2 px-1 pb-2">
					<Segmented strong className="min-w-0">
						<SegmentedButton
							active={mode === "fit"}
							onClick={() => setMode("fit")}
							className="min-w-0 px-2 text-[12px]"
						>
							Fit
						</SegmentedButton>
						<SegmentedButton
							active={mode === "wide"}
							onClick={() => setMode("wide")}
							className="min-w-0 px-2 text-[12px]"
						>
							Wide
						</SegmentedButton>
					</Segmented>
					<button
						type="button"
						onClick={() => setWheel((w) => !w)}
						className={`shrink-0 rounded-full px-4 py-2 text-[13px] font-medium active:scale-[0.98] ${wheel ? "bg-primary text-primary-content" : "bg-base-200 text-base-content"}`}
					>
						{wheel ? "Wheel: on" : "Take the wheel"}
					</button>
				</div>

				<div className="relative min-h-0 flex-1 overflow-hidden rounded-2xl border border-base-300 p-1">
					{/* absolutely positioned: xterm's grid must never feed its
						rendered height back into this box's size, or fit()
						and the ResizeObserver chase each other forever */}
					<div
						ref={wrapRef}
						className={`absolute inset-0 ${mode === "wide" ? "overflow-x-auto overflow-y-hidden" : "overflow-hidden"} ${wheel ? "" : "opacity-80"}`}
					/>
					{mode === "wide" && (
						<div className="pointer-events-none absolute right-2 bottom-1 text-[10px] text-muted/70">
							swipe to pan →
						</div>
					)}
				</div>

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
