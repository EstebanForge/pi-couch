// The composer card: draft box, send, attachments, model and thinking
// chips, and the quick keys strip above it. Per docs/rewrite-plan.md:
// pending bubbles reconcile via the stream, drafts survive the send
// window, and an unknown outcome never restores or retries.

import { Sheet, Toast } from "konsta/react";
import { memo, useEffect, useMemo, useRef, useState } from "react";
import {
	getModels,
	type ModelRow,
	sendKeys,
	sendPrompt,
	setModel,
	setThinking,
	uploadImage,
} from "../api";
import { dispositionFor } from "../chat/disposition";
import { ESC_SETTLE_MS, orderKeys, type QuickKey } from "../chat/quick-keys";
import { money } from "../fmt";
import { useSheetBack } from "../nav";
import { effectiveEnterSends, quickKeyOrder, quickKeysEnabled } from "../prefs";
import {
	FaBrain,
	FaCheck,
	FaChevronDown,
	FaPaperclip,
	FaPaperPlane,
	FaSparkles,
	FaTableCellsLarge,
	FaXmark,
} from "./fa";
import { Spinner } from "./Spinner";

const MAX_FILES = 10;
const MAX_PARALLEL = 3;
const MAX_BYTES = 10 << 20; // mirrors the server cap
const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];

type Attachment = { id: number; name: string; path?: string; error?: string };

export const Composer = memo(function Composer(props: {
	sessionKey: string;
	status: string;
	model: string;
	thinking: string;
	ctxTokens: number;
	memory: string | null;
	cost: number;
	addPending: (text: string, queued: boolean) => number;
	markPending: (id: number, state: "sent" | "queued" | "unconfirmed") => void;
	removePending: (id: number) => void;
	sendAgent?: (body: string, queued: boolean) => Promise<{ ok: boolean; error?: string }>;
}) {
	const { sessionKey, status } = props;
	const draftStoreKey = `couch-pi.draft.${sessionKey}`;
	const [text, setText] = useState(() => localStorage.getItem(draftStoreKey) ?? "");
	const [files, setFiles] = useState<Attachment[]>([]);
	const [notice, setNotice] = useState("");
	const [toast, setToast] = useState("");
	const [sheet, setSheet] = useState<"none" | "model" | "thinking" | "commands" | "attach">("none");
	useSheetBack(sheet !== "none", () => setSheet("none"));
	const [modelQuery, setModelQuery] = useState("");
	const [modelRows, setModelRows] = useState<ModelRow[]>([]);
	const [modelsLoading, setModelsLoading] = useState(false);
	const [modelMark, setModelMark] = useState(false);
	const [thinkingMark, setThinkingMark] = useState(false);
	const [chosenModel, setChosenModel] = useState<string | null>(null);
	const [chosenThinking, setChosenThinking] = useState<string | null>(null);
	const ta = useRef<HTMLTextAreaElement | null>(null);
	const fileInput = useRef<HTMLInputElement | null>(null);
	const keyChain = useRef<Promise<unknown>>(Promise.resolve());
	const nextId = useRef(1);
	// One request per intent: a double-tap on send (or Alt+Enter while a
	// send is in flight) must not POST twice - pi would queue both.
	const sending = useRef(false);

	useEffect(() => {
		localStorage.setItem(draftStoreKey, text);
	}, [draftStoreKey, text]);

	// Autosize: grow with content, cap at half the visual viewport.
	// Clearing (send) skips the read entirely: height auto followed by
	// a scrollHeight read forces synchronous layout mid-send.
	// biome-ignore lint/correctness/useExhaustiveDependencies: the effect must re-run on every text change even though text is not read
	useEffect(() => {
		const el = ta.current;
		if (!el) return;
		if (!text) {
			el.style.height = "";
			return;
		}
		el.style.height = "auto";
		const cap = Math.floor((window.visualViewport?.height ?? window.innerHeight) / 2);
		el.style.height = `${Math.min(el.scrollHeight, cap)}px`;
	}, [text]);

	const uploading = files.some((f) => f.path === undefined && !f.error);
	const hasText = text.trim().length > 0;
	const canSend = hasText && !uploading;

	const sendText = async (body: string, queued: boolean, draft?: string) => {
		if (sending.current) return;
		sending.current = true;
		const id = props.addPending(body, queued);
		// With attachments the body carries upload paths the textarea
		// never held; only the text part can leave the box.
		const acked = draft ?? body;
		let outcome: "ok" | "fail" | "unknown" = "unknown";
		try {
			const r = props.sendAgent
				? await props.sendAgent(body, queued)
				: await sendPrompt(sessionKey, body, queued ? "followUp" : undefined);
			outcome = r.ok ? "ok" : "fail";
			if (!r.ok)
				setNotice(r.error === "notdriven" ? "No driver runs this session." : "Send failed.");
		} catch {
			outcome = "unknown";
		}
		if (outcome === "ok") {
			// The draft keeps text typed during the flight; only the
			// acknowledged part leaves the box.
			setText((cur) => (cur.startsWith(acked) ? cur.slice(acked.length) : cur));
			props.markPending(id, queued ? "queued" : "sent");
		} else if (outcome === "fail") {
			props.removePending(id);
		} else {
			// Unknown: a duplicate prompt is worse than a lost one.
			props.markPending(id, "unconfirmed");
		}
		sending.current = false;
	};

	const onSubmit = () => {
		if (!canSend) return;
		const paths = files.filter((f) => f.path).map((f) => f.path as string);
		const body = paths.length > 0 ? [...paths, text].join("\n") : text;
		setFiles([]);
		// The disposition rides on the send: a working session takes the
		// text as a follow-up, so the bubble never claims a placement the
		// wire did not ask for.
		const queued = dispositionFor(status) === "followUp";
		void sendText(body, queued, text);
	};

	const pickFiles = async (list: FileList | null) => {
		if (!list || list.length === 0) return;
		const picked = [...list].slice(0, MAX_FILES - files.length);
		if (list.length > MAX_FILES - files.length) {
			setNotice(`Only ${MAX_FILES} files per prompt; extras dropped.`);
		}
		const batch = picked.map((f, i) => ({
			id: nextId.current++ + i,
			name: f.name,
		}));
		setFiles((cur) => [...cur, ...batch]);
		// Three uploads in flight at a time, in pick order.
		let active = 0;
		await Promise.all(
			batch.map(
				(slot, idx) =>
					new Promise<void>((resolve) => {
						const start = () => {
							active++;
							const file = picked[idx];
							const done = (path?: string, error?: string) => {
								setFiles((cur) => cur.map((f) => (f.id === slot.id ? { ...f, path, error } : f)));
								active--;
								resolve();
							};
							if (file.size > MAX_BYTES) {
								done(undefined, "too large");
								return;
							}
							uploadImage(file).then(
								(path) => done(path),
								() => done(undefined, "failed"),
							);
						};
						const wait = setInterval(() => {
							if (active < MAX_PARALLEL) {
								clearInterval(wait);
								start();
							}
						}, 25);
					}),
			),
		);
	};

	const runKey = (k: QuickKey) => {
		if (k.action.kind === "queue") {
			runQueue();
			return;
		}
		if (k.action.kind !== "keys") return;
		const act = k.action;
		keyChain.current = keyChain.current.then(async () => {
			try {
				await sendKeys(sessionKey, act.keys);
				if (act.keys[0] === "esc") {
					await new Promise((r) => setTimeout(r, ESC_SETTLE_MS));
				}
			} catch {
				setToast("Key not sent");
			}
		});
	};

	// The Alt+Enter cap is its own flow: send the draft through the
	// prompt endpoint (queued by pi until the turn ends), no keystroke
	// typing of the body into the TUI (slow and visible). The pending
	// bubble bridges the gap.
	const runQueue = () => {
		if (!(status === "working" && hasText)) return;
		const body = text;
		setText("");
		void sendText(body, true);
	};

	// Model search: 300ms debounce over /api/models. Old rows stay
	// visible while a new query loads; only a cold, empty list spins.
	// Rows also feed the context fill below the card, so the catalog
	// is fetched once at mount (the server cache serves it warm).
	useEffect(() => {
		getModels("")
			.then(setModelRows)
			.catch(() => {});
	}, []);

	useEffect(() => {
		if (sheet !== "model") return;
		setModelsLoading(true);
		const t = setTimeout(() => {
			getModels(modelQuery)
				.then(setModelRows)
				.catch(() => setModelRows([]))
				.finally(() => setModelsLoading(false));
		}, 300);
		return () => clearTimeout(t);
	}, [modelQuery, sheet]);

	useEffect(() => {
		if (!toast) return;
		const t = setTimeout(() => setToast(""), 2200);
		return () => clearTimeout(t);
	}, [toast]);

	const applyModel = (pattern: string) => {
		const p = pattern.trim();
		if (!p) return;
		setSheet("none");
		setModelMark(true);
		setChosenModel(p);
		void setModel(sessionKey, p).then((r) => {
			if (!r.ok) {
				setModelMark(false);
				setChosenModel(null);
				setToast(r.error === "notdriven" ? "No driver" : "Model not set");
			}
		});
		// A pattern with no exact match leaves pi sitting in its model
		// picker overlay, blocking the TUI. If the stream does not
		// confirm the switch, dismiss the picker and say so.
		window.setTimeout(() => {
			setChosenModel((cur) => {
				if (cur !== p) return cur;
				setModelMark(false);
				void sendKeys(sessionKey, ["esc"]);
				setToast("Model not applied");
				return null;
			});
		}, 8000);
	};

	const applyThinking = (level: string) => {
		setSheet("none");
		setThinkingMark(true);
		setChosenThinking(level);
		void setThinking(sessionKey, level).then((r) => {
			if (!r.ok) {
				setThinkingMark(false);
				setChosenThinking(null);
				setToast(r.error === "notdriven" ? "No driver" : "Thinking not set");
			}
		});
		// pi rejects a level the current model does not support without
		// writing the jsonl entry; dismiss any TUI residue and unstick
		// the chip when the stream never confirms.
		window.setTimeout(() => {
			setChosenThinking((cur) => {
				if (cur !== level) return cur;
				setThinkingMark(false);
				void sendKeys(sessionKey, ["esc"]);
				setToast("Thinking not applied");
				return null;
			});
		}, 8000);
	};

	// Pending marks clear when the stream confirms the change.
	useEffect(() => {
		if (chosenModel !== null && props.model === chosenModel) {
			setModelMark(false);
			setChosenModel(null);
		}
	}, [props.model, chosenModel]);
	useEffect(() => {
		if (chosenThinking !== null && props.thinking === chosenThinking) {
			setThinkingMark(false);
			setChosenThinking(null);
		}
	}, [props.thinking, chosenThinking]);

	const showModel = chosenModel ?? props.model;
	const showThinking = chosenThinking ?? props.thinking;

	// Context fill in pi's own bar format: 23.5%/1.0M. Without either
	// number there is no line.
	const ctxWindow = modelRows.find((r) => r.id === showModel)?.context ?? 0;
	const ctxPct =
		props.ctxTokens > 0 && ctxWindow > 0
			? Math.min(100, (props.ctxTokens / ctxWindow) * 100).toFixed(1)
			: null;
	const ctxWin =
		ctxWindow >= 1_000_000
			? `${(ctxWindow / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`
			: ctxWindow >= 1_000
				? `${Math.round(ctxWindow / 1_000)}K`
				: "";

	// localStorage reads are main-thread IPC on Android WebView; read
	// once per mount. Quick-key settings changes land on next visit.
	const quickKeys = useMemo(
		() => ({ enabled: quickKeysEnabled(), keys: orderKeys(quickKeyOrder()) }),
		[],
	);

	return (
		<>
			{/* quick keys strip */}
			{quickKeys.enabled && (
				<div className="relative">
					<div
						className="flex gap-2 overflow-x-auto px-2 pt-2 pb-1"
						style={{
							scrollbarWidth: "none",
							touchAction: "manipulation",
							userSelect: "none",
							transform: "translateZ(0)",
						}}
					>
						{quickKeys.keys.map((k) => {
							const enabled =
								k.action.kind === "queue"
									? runQueueEnabled(status, hasText)
									: k.enabled(status, hasText);
							return (
								<button
									key={k.id}
									type="button"
									disabled={!enabled}
									onPointerDown={(e) => e.preventDefault()}
									onMouseDown={(e) => e.preventDefault()}
									onClick={() => (k.action.kind === "queue" ? runQueue() : runKey(k))}
									className={`h-11 min-w-[44px] shrink-0 rounded-lg border px-2 font-mono text-[13px] active:scale-95 ${
										k.id === "enter"
											? "border-primary/70 border-b-2 border-b-primary/50 bg-base-200 text-primary"
											: `border-base-300 border-b-2 border-b-base-300/70 bg-base-200 ${enabled ? "text-base-content" : "text-muted/40"}`
									}`}
								>
									{k.id === "enter" ? (
										// U+23CE rides a fallback font; pin it to the line box so
										// it centers on the arrows' optical row
										<span className="inline-block leading-none">⏎</span>
									) : (
										k.label
									)}
								</button>
							);
						})}
					</div>
					{/* pinned outside the scroller: inside, it slides with the
					keys and its opaque edge cuts through them */}
					<div className="pointer-events-none absolute inset-y-0 right-0 w-8 bg-gradient-to-l from-base-100 to-transparent" />
				</div>
			)}

			{notice && (
				<button
					type="button"
					className="w-full px-3 pb-1 text-left text-[12px] text-warning"
					onClick={() => setNotice("")}
				>
					{notice}
				</button>
			)}

			{/* composer card */}
			<div className="px-2 pt-1 pb-safe">
				<div className="rounded-2xl border border-base-300 bg-base-200 p-1.5">
					<textarea
						ref={ta}
						value={text}
						onChange={(e) => setText(e.target.value)}
						onKeyDown={(e) => {
							if (
								e.key === "Enter" &&
								!e.shiftKey &&
								enterSends() &&
								!e.nativeEvent.isComposing &&
								e.keyCode !== 229
							) {
								e.preventDefault();
								onSubmit();
							}
						}}
						rows={1}
						placeholder="Steer the agent…"
						autoCorrect="off"
						autoCapitalize="off"
						spellCheck={false}
						className="max-h-[50vh] min-h-[40px] w-full resize-none bg-transparent px-2 py-2 text-[16px] outline-none"
					/>
					<input
						ref={fileInput}
						type="file"
						// No accept filter: any file rides the prompt as a path
						// for the agent's own file tools.
						multiple
						hidden
						onChange={(e) => {
							void pickFiles(e.target.files);
							e.target.value = "";
						}}
					/>
					<div className="flex items-center gap-0.5">
						<button
							type="button"
							aria-label="Commands"
							className="grid h-9 w-9 shrink-0 place-items-center rounded-full text-muted active:scale-95"
							onClick={() => setSheet("commands")}
						>
							<FaTableCellsLarge size={18} />
						</button>
						<div className="relative shrink-0">
							<button
								type="button"
								aria-label="Attach image"
								className="grid h-9 w-9 place-items-center rounded-full text-muted active:scale-95"
								onClick={() => setSheet("attach")}
							>
								<FaPaperclip size={18} />
							</button>
							{files.length > 0 && (
								<span className="absolute -top-0.5 -right-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-primary px-1 text-[10px] text-base-100">
									{files.length}
								</span>
							)}
						</div>
						<button
							type="button"
							onClick={() => {
								setModelQuery("");
								setSheet("model");
							}}
							className="flex min-w-0 max-w-[46%] shrink items-center gap-1 rounded-full bg-base-300 px-3 py-1.5 text-[12px] text-base-content active:scale-95"
						>
							<FaSparkles size={13} className="shrink-0 text-primary" />
							{modelMark && <Spinner size={12} className="shrink-0 text-primary" />}
							<span className="truncate">{showModel || "model"}</span>
							<FaChevronDown size={12} className="shrink-0 text-muted" />
						</button>
						<button
							type="button"
							onClick={() => setSheet("thinking")}
							className="shrink-0 rounded-full bg-base-300 px-3 py-1.5 text-[12px] text-base-content active:scale-95"
						>
							{thinkingMark && <Spinner size={12} className="mr-1 text-primary" />}
							{showThinking || "thinking"}
						</button>
						<div className="ml-auto" />
						<button
							type="button"
							aria-label="Send"
							onClick={() => onSubmit()}
							disabled={!canSend}
							className={`grid h-10 w-10 shrink-0 place-items-center rounded-full text-[16px] transition-colors active:scale-95 ${
								canSend ? "bg-primary text-base-100 text-[17px]" : "bg-base-300 text-muted"
							}`}
						>
							<FaPaperPlane size={17} />
						</button>
					</div>
				</div>
				{(ctxPct !== null || props.cost > 0 || props.memory) && (
					<div className="flex items-center justify-center gap-3 px-3 py-1 text-[11px] text-muted">
						{ctxPct !== null && (
							<span className="flex items-center gap-1.5">
								ctx
								<span className="h-1 w-10 overflow-hidden rounded-full bg-base-300">
									<span
										className="block h-full rounded-full bg-primary/70"
										style={{ width: `${ctxPct}%` }}
									/>
								</span>
								{ctxPct}%{ctxWin && `/${ctxWin}`}
							</span>
						)}
						{props.cost > 0 && <span>${money(props.cost)}</span>}
						{props.memory && (
							<span aria-label={"memory " + props.memory}>
								<FaBrain size={13} />
							</span>
						)}
					</div>
				)}
			</div>

			{/* commands sheet: placeholder panel, real commands land later */}
			<Sheet
				className="pb-safe-all"
				opened={sheet === "commands"}
				onBackdropClick={() => setSheet("none")}
			>
				<div className="p-3">
					<div className="mb-2 px-3 text-[13px] text-muted">Commands</div>
					{["Command one", "Command two", "Command three"].map((c) => (
						<button
							key={c}
							type="button"
							className="block w-full rounded-xl px-3 py-2.5 text-left text-[14px] active:bg-base-200"
						>
							{c}
						</button>
					))}
					<div className="px-3 py-3 text-[12px] text-muted">More commands land here soon.</div>
				</div>
			</Sheet>

			{/* attach sheet: the paperclip badge carries the count; this
				sheet is the management surface */}
			<Sheet
				className="pb-safe-all"
				opened={sheet === "attach"}
				onBackdropClick={() => setSheet("none")}
			>
				<div className="p-3">
					<div className="mb-1 px-3 text-[13px] text-muted">
						{files.length > 0
							? `${files.length} attachment${files.length === 1 ? "" : "s"}`
							: "No attachments"}
					</div>
					{/* Add lives as a list row, not a bottom button: the
					    primary-right slot is where every panel closes, and a
					    picker button there read as the confirm (field report). */}
					<button
						type="button"
						className="mb-1 flex w-full items-center gap-2 rounded-xl border border-dashed border-muted/40 px-3 py-2.5 text-[14px] text-muted active:bg-base-200 disabled:opacity-50"
						disabled={files.length >= MAX_FILES}
						onClick={() => fileInput.current?.click()}
					>
						<FaPaperclip size={13} />
						Add attachment
					</button>
					{files.map((f) => (
						<div
							key={f.id}
							className="flex items-center gap-2 rounded-xl px-3 py-2.5 active:bg-base-200"
						>
							<span className="shrink-0">
								{f.path ? (
									<FaCheck size={13} className="text-primary" />
								) : f.error ? (
									<FaXmark size={13} className="text-error" />
								) : (
									<Spinner size={13} className="text-muted" />
								)}
							</span>
							<span className="min-w-0 flex-1 truncate text-[14px]">{f.name}</span>
							{f.error && <span className="shrink-0 text-[11px] text-error">{f.error}</span>}
							<button
								type="button"
								aria-label={`Remove ${f.name}`}
								className="grid h-9 w-9 shrink-0 place-items-center rounded-full text-muted active:scale-95"
								onClick={() => setFiles((cur) => cur.filter((x) => x.id !== f.id))}
							>
								<FaXmark size={14} />
							</button>
						</div>
					))}
					{files.length === 0 && (
						<div className="px-3 py-3 text-[12px] text-muted">
							Attach files and the agent reads them by path.
						</div>
					)}
					<div className="mt-2 flex gap-2">
						<button
							type="button"
							className="flex flex-1 items-center justify-center gap-2 rounded-xl bg-base-300 px-3 py-2.5 text-[14px] active:scale-95 disabled:opacity-50"
							disabled={files.length === 0}
							onClick={() => setFiles([])}
						>
							<FaXmark size={13} />
							Remove all
						</button>
						<button
							type="button"
							className="flex flex-1 items-center justify-center rounded-xl bg-primary px-3 py-2.5 text-[14px] text-base-100 active:scale-95"
							onClick={() => setSheet("none")}
						>
							Done
						</button>
					</div>
				</div>
			</Sheet>

			{/* model sheet */}
			<Sheet
				className="pb-safe-all"
				opened={sheet === "model"}
				onBackdropClick={() => setSheet("none")}
			>
				<div className="p-3">
					<input
						value={modelQuery}
						onChange={(e) => setModelQuery(e.target.value)}
						placeholder="Search or type a pattern (provider/model[:thinking])"
						className="mb-2 w-full rounded-xl bg-base-200 px-3 py-2.5 text-[16px] outline-none"
					/>
					{modelQuery.trim() !== "" && (
						<button
							type="button"
							className="mb-1 w-full rounded-xl bg-primary/20 px-3 py-2.5 text-left text-[14px] active:scale-[0.99]"
							onClick={() => applyModel(modelQuery)}
						>
							Use pattern: {modelQuery.trim()}
						</button>
					)}
					<div className="max-h-[45vh] overflow-y-auto">
						{modelRows.map((m) => (
							<button
								key={m.id}
								type="button"
								className={`block w-full rounded-xl px-3 py-2.5 text-left text-[14px] active:bg-base-200 ${
									m.id === props.model ? "text-primary" : ""
								}`}
								onClick={() => applyModel(m.id)}
							>
								{m.id}
							</button>
						))}
						{modelsLoading && modelRows.length === 0 ? (
							<div className="flex items-center gap-2 px-3 py-3 text-[13px] text-muted">
								<Spinner size={14} />
								Loading models…
							</div>
						) : modelRows.length === 0 ? (
							<div className="px-3 py-3 text-[13px] text-muted">
								No matches. Free text still works.
							</div>
						) : null}
					</div>
				</div>
			</Sheet>

			{/* thinking sheet */}
			<Sheet
				className="pb-safe-all"
				opened={sheet === "thinking"}
				onBackdropClick={() => setSheet("none")}
			>
				<div className="p-3">
					{THINKING_LEVELS.map((lv) => (
						<button
							key={lv}
							type="button"
							className={`block w-full rounded-xl px-3 py-2.5 text-left text-[14px] active:bg-base-200 ${
								lv === props.thinking ? "text-primary" : ""
							}`}
							onClick={() => applyThinking(lv)}
						>
							{lv}
						</button>
					))}
				</div>
			</Sheet>

			<Toast opened={toast !== ""}>{toast}</Toast>
		</>
	);
});

function runQueueEnabled(status: string, hasText: boolean): boolean {
	return status === "working" && hasText;
}

// Enter sends on fine pointers; touch keeps Enter as a newline.
function enterSends(): boolean {
	return effectiveEnterSends();
}
