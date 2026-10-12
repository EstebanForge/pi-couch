// The composer card: draft box, send, attachments, model and thinking
// chips, and the quick keys strip above it. Per docs/rewrite-plan.md:
// pending bubbles reconcile via the stream, drafts survive the send
// window, and an unknown outcome never restores or retries.

import { Sheet, Toast } from "konsta/react";
import { useEffect, useRef, useState } from "react";
import { getModels, sendKeys, sendPrompt, setModel, setThinking, uploadImage } from "../api";
import { ESC_SETTLE_MS, QUICK_KEYS, type QuickKey } from "../chat/quick-keys";

const MAX_FILES = 10;
const MAX_PARALLEL = 3;
const MAX_BYTES = 10 << 20; // mirrors the server cap
const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];

type Attachment = { id: number; name: string; path?: string; error?: string };

export function Composer(props: {
	sessionKey: string;
	status: string;
	model: string;
	thinking: string;
	addPending: (text: string, queued: boolean) => number;
	markPending: (id: number, state: "sent" | "queued" | "unconfirmed") => void;
	removePending: (id: number) => void;
	requestClose: () => void;
}) {
	const { sessionKey, status } = props;
	const draftStoreKey = `couch-pi.draft.${sessionKey}`;
	const [text, setText] = useState(() => localStorage.getItem(draftStoreKey) ?? "");
	const [files, setFiles] = useState<Attachment[]>([]);
	const [notice, setNotice] = useState("");
	const [toast, setToast] = useState("");
	const [sheet, setSheet] = useState<"none" | "model" | "thinking">("none");
	const [modelQuery, setModelQuery] = useState("");
	const [modelRows, setModelRows] = useState<string[]>([]);
	const [modelMark, setModelMark] = useState(false);
	const [thinkingMark, setThinkingMark] = useState(false);
	const [chosenModel, setChosenModel] = useState<string | null>(null);
	const [chosenThinking, setChosenThinking] = useState<string | null>(null);
	const ta = useRef<HTMLTextAreaElement | null>(null);
	const fileInput = useRef<HTMLInputElement | null>(null);
	const keyChain = useRef<Promise<unknown>>(Promise.resolve());
	const nextId = useRef(1);

	useEffect(() => {
		localStorage.setItem(draftStoreKey, text);
	}, [draftStoreKey, text]);

	// Autosize: grow with content, cap at half the visual viewport.
	// biome-ignore lint/correctness/useExhaustiveDependencies: the effect must re-run on every text change even though text is not read
	useEffect(() => {
		const el = ta.current;
		if (!el) return;
		el.style.height = "auto";
		const cap = Math.floor((window.visualViewport?.height ?? window.innerHeight) / 2);
		el.style.height = `${Math.min(el.scrollHeight, cap)}px`;
	}, [text]);

	const uploading = files.some((f) => f.path === undefined && !f.error);
	const hasText = text.trim().length > 0;
	const canSend = hasText && !uploading;

	const sendText = async (body: string, queued: boolean) => {
		const id = props.addPending(body, queued);
		const sent = body;
		let outcome: "ok" | "fail" | "unknown" = "unknown";
		try {
			const r = await sendPrompt(sessionKey, body);
			outcome = r.ok ? "ok" : "fail";
			if (!r.ok)
				setNotice(r.error === "notdriven" ? "No driver runs this session." : "Send failed.");
		} catch {
			outcome = "unknown";
		}
		if (outcome === "ok") {
			// The draft keeps text typed during the flight; only the
			// acknowledged part leaves the box.
			setText((cur) => (cur.startsWith(sent) ? cur.slice(sent.length) : cur));
			props.markPending(id, queued ? "queued" : "sent");
		} else if (outcome === "fail") {
			props.removePending(id);
		} else {
			// Unknown: a duplicate prompt is worse than a lost one.
			props.markPending(id, "unconfirmed");
		}
	};

	const onSubmit = () => {
		if (!canSend) return;
		const paths = files.filter((f) => f.path).map((f) => f.path as string);
		const body = paths.length > 0 ? [...paths, text].join("\n") : text;
		setFiles([]);
		void sendText(body, false);
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
		if (k.action.kind === "close") {
			props.requestClose();
			return;
		}
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

	// Model search: 300ms debounce over /api/models.
	useEffect(() => {
		if (sheet !== "model") return;
		const t = setTimeout(() => {
			getModels(modelQuery)
				.then(setModelRows)
				.catch(() => setModelRows([]));
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

	return (
		<>
			{/* quick keys strip */}
			<div
				className="relative flex gap-2 overflow-x-auto px-2 pb-1"
				style={{ scrollbarWidth: "none", touchAction: "manipulation", userSelect: "none" }}
			>
				{QUICK_KEYS.map((k) => {
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
							className={`h-11 min-w-[44px] shrink-0 rounded-lg border border-base-300 border-b-2 border-b-base-300/70 bg-base-200 px-2 font-mono text-[13px] active:scale-95 ${
								enabled ? "text-base-content" : "text-muted/40"
							}`}
						>
							{k.label}
						</button>
					);
				})}
				<div className="pointer-events-none absolute inset-y-0 right-0 w-8 bg-gradient-to-l from-base-100 to-transparent" />
			</div>

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
			<div className="border-t border-base-300 bg-base-200/60 px-2 pt-1 pb-safe-area">
				<div className="flex items-center gap-1 pb-1">
					<button
						type="button"
						onClick={() => {
							setModelQuery("");
							setSheet("model");
						}}
						className="max-w-[55%] truncate rounded-full bg-base-300 px-2.5 py-1 text-[11px] text-base-content active:scale-95"
					>
						{modelMark ? "…" : ""} {showModel || "model"}
					</button>
					<button
						type="button"
						onClick={() => setSheet("thinking")}
						className="rounded-full bg-base-300 px-2.5 py-1 text-[11px] text-base-content active:scale-95"
					>
						{thinkingMark ? "…" : ""} {showThinking || "thinking"}
					</button>
					<div className="ml-auto flex items-center gap-1">
						{files.map((f) => (
							<span
								key={f.id}
								className={`flex max-w-[110px] items-center gap-1 rounded-full px-2 py-1 text-[10px] ${
									f.error ? "bg-error/30 text-error" : "bg-base-300 text-muted"
								}`}
							>
								<span className="truncate">
									{f.path ? "✓" : f.error ? "✕" : "↑"} {f.name}
								</span>
								<button
									type="button"
									aria-label="Remove"
									className="shrink-0"
									onClick={() => setFiles((cur) => cur.filter((x) => x.id !== f.id))}
								>
									✕
								</button>
							</span>
						))}
						<button
							type="button"
							aria-label="Attach image"
							className="grid h-9 w-9 place-items-center rounded-full text-muted active:scale-95"
							onClick={() => fileInput.current?.click()}
						>
							📎
						</button>
					</div>
				</div>
				<input
					ref={fileInput}
					type="file"
					accept="image/png,image/jpeg,image/webp,image/gif"
					multiple
					hidden
					onChange={(e) => {
						void pickFiles(e.target.files);
						e.target.value = "";
					}}
				/>
				<div className="flex items-end gap-2">
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
						className="max-h-[50vh] min-h-[44px] w-full min-w-0 flex-1 resize-none rounded-xl bg-base-100 px-3 py-2.5 text-[16px] outline-none"
					/>
					<button
						type="button"
						disabled={!canSend}
						onClick={onSubmit}
						className={`mb-0.5 grid h-11 w-11 shrink-0 place-items-center rounded-full text-[17px] active:scale-95 ${
							canSend ? "bg-primary text-base-100" : "bg-base-300 text-muted/50"
						}`}
					>
						➤
					</button>
				</div>
			</div>

			{/* model sheet */}
			<Sheet
				className="pb-safe-area"
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
								key={m}
								type="button"
								className={`block w-full rounded-xl px-3 py-2.5 text-left text-[14px] active:bg-base-200 ${
									m === props.model ? "text-primary" : ""
								}`}
								onClick={() => applyModel(m)}
							>
								{m}
							</button>
						))}
						{modelRows.length === 0 && (
							<div className="px-3 py-3 text-[13px] text-muted">
								No matches. Free text still works.
							</div>
						)}
					</div>
				</div>
			</Sheet>

			{/* thinking sheet */}
			<Sheet
				className="pb-safe-area"
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

			<Toast opened={toast !== ""} title={toast} />
		</>
	);
}

function runQueueEnabled(status: string, hasText: boolean): boolean {
	return status === "working" && hasText;
}

// Enter sends on fine pointers; touch keeps Enter as a newline.
function enterSends(): boolean {
	return window.matchMedia("(pointer: fine)").matches;
}
