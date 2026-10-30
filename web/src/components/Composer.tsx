// The composer card: draft box, send, attachments, model and thinking
// chips, and the quick keys strip above it. Per docs/rewrite-plan.md:
// pending bubbles reconcile via the stream, drafts survive the send
// window, and an unknown outcome never restores or retries.

import { Sheet, Toast } from "konsta/react";
import { memo, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { flushSync } from "react-dom";
import {
	getModels,
	getPrompts,
	getSkills,
	type ModelRow,
	type PromptInfo,
	refreshModels,
	runCommand,
	type SkillInfo,
	sendKeys,
	sendPrompt,
	setModel,
	setThinking,
	uploadImage,
} from "../api";
import { dispositionFor } from "../chat/disposition";
import { ESC_SETTLE_MS, orderKeys, type QuickKey } from "../chat/quick-keys";
import { hapticConfirm, hapticTick } from "../haptics";
import { useSheetBack } from "../nav";
import {
	effectiveEnterSends,
	prefKey,
	quickCommands,
	quickKeyOrder,
	quickKeysEnabled,
	readPref,
	writePref,
} from "../prefs";
import {
	FaArrowsRotate,
	FaBrain,
	FaCaretRight,
	FaCheck,
	FaChevronDown,
	FaChevronLeft,
	FaPaperclip,
	FaPaperPlane,
	FaSparkles,
	FaTableCellsLarge,
	FaXmark,
} from "./fa";
import { stalledApply } from "./modelApply";
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
	cwd: string;
	addPending: (text: string, queued: boolean) => number;
	markPending: (id: number, state: "sent" | "queued" | "unconfirmed") => void;
	removePending: (id: number) => void;
	/** Set while no driver runs the session: sends resume it instead of steering. */
	resume?: (body: string) => Promise<{ ok: boolean; error?: string }>;
	/** Greys and kills every control: a resume is in flight. */
	disabled?: boolean;
}) {
	const { sessionKey, status } = props;
	const draftStoreKey = prefKey(`draft.${sessionKey}`);
	const [text, setText] = useState(() => readPref(draftStoreKey) ?? "");
	const [files, setFiles] = useState<Attachment[]>([]);
	const [notice, setNotice] = useState("");
	const [toast, setToast] = useState("");
	const [sheet, setSheet] = useState<"none" | "model" | "thinking" | "commands" | "attach">("none");
	const [qaStep, setQaStep] = useState<"menu" | "skills" | "commands" | "prompts">("menu");
	const [qaFilter, setQaFilter] = useState("");
	const [skills, setSkills] = useState<SkillInfo[] | null>(null);
	const [prompts, setPrompts] = useState<PromptInfo[] | null>(null);
	const [skillsError, setSkillsError] = useState(false);
	const [promptsError, setPromptsError] = useState(false);
	const [runningCmd, setRunningCmd] = useState<string | null>(null);
	const [execResult, setExecResult] = useState<{
		cmd: string;
		output: string;
		code: number;
	} | null>(null);
	const quickCmds = useMemo(() => quickCommands(), []);
	const qaQ = qaFilter.trim().toLowerCase();
	const qaSkills =
		qaQ === "" || !skills
			? skills
			: skills.filter(
					(s) => s.name.toLowerCase().includes(qaQ) || s.description.toLowerCase().includes(qaQ),
				);
	const qaCmdList = qaQ === "" ? quickCmds : quickCmds.filter((c) => c.toLowerCase().includes(qaQ));
	const qaPrompts =
		qaQ === "" || !prompts
			? prompts
			: prompts.filter(
					(p) =>
						p.name.toLowerCase().includes(qaQ) ||
						p.description.toLowerCase().includes(qaQ) ||
						p.hint?.toLowerCase().includes(qaQ),
				);
	// Step switches morph instead of snapping: the content wrapper
	// carries view-transition-name qa-content, and the swap runs inside
	// startViewTransition so the browser cross-fades old and new.
	// Reduced motion and engines without the API fall back to the
	// instant swap.
	const goStep = (s: "menu" | "skills" | "commands" | "prompts") => {
		const apply = () => {
			flushSync(() => {
				setQaStep(s);
				if (s === "menu") setQaFilter("");
			});
		};
		const doc = document as Document & { startViewTransition?: (cb: () => void) => unknown };
		if (window.matchMedia("(prefers-reduced-motion: reduce)").matches || !doc.startViewTransition) {
			apply();
			return;
		}
		doc.startViewTransition(apply);
	};

	// The step and filter reset on every close, not on open: the sheet
	// stays mounted while hidden, so a reset deferred to the next open
	// would slide up with the previous step's content and collapse to
	// the menu mid-animation - the second-open flash.
	const closeQuick = () => {
		setSheet("none");
		setQaStep("menu");
		setQaFilter("");
	};

	useSheetBack(sheet !== "none", closeQuick);
	const [modelQuery, setModelQuery] = useState("");
	const [modelRows, setModelRows] = useState<ModelRow[]>([]);
	const [modelsLoading, setModelsLoading] = useState(false);
	const [modelsRefreshing, setModelsRefreshing] = useState(false);
	const [modelMark, setModelMark] = useState(false);
	const [thinkingMark, setThinkingMark] = useState(false);
	const [chosenModel, setChosenModel] = useState<string | null>(null);
	const [chosenThinking, setChosenThinking] = useState<string | null>(null);
	const ta = useRef<HTMLTextAreaElement | null>(null);
	const fileInput = useRef<HTMLInputElement | null>(null);
	const keyChain = useRef<Promise<unknown>>(Promise.resolve());

	// The quick actions panel opens on its kind menu; skills and
	// prompts load from pi's own discovery the first time each list is
	// entered, and every tap fills the draft so companion text can
	// follow - the panel never sends.
	useEffect(() => {
		if (sheet !== "commands" || qaStep !== "skills" || skills) return;
		getSkills(props.cwd || undefined)
			.then((rows) => {
				setSkills(rows);
			})
			.catch(() => {
				setSkills([]);
				setSkillsError(true);
			});
	}, [sheet, qaStep, skills, props.cwd]);

	useEffect(() => {
		if (sheet !== "commands" || qaStep !== "prompts" || prompts) return;
		getPrompts(props.cwd || undefined)
			.then((rows) => {
				setPrompts(rows);
			})
			.catch(() => {
				setPrompts([]);
				setPromptsError(true);
			});
	}, [sheet, qaStep, prompts, props.cwd]);

	const fillQuick = (value: string) => {
		setText(value);
		closeQuick();
		window.setTimeout(() => ta.current?.focus(), 80);
	};

	// A non-slash quick command is bash for a one-off task: it runs on
	// the box in the session's folder and the panel shows the output;
	// slash entries still fill the draft so pi can take arguments.
	const runBash = async (cmd: string) => {
		if (runningCmd) return;
		setRunningCmd(cmd);
		setExecResult(null);
		try {
			const r = await runCommand(cmd, props.cwd);
			setExecResult({ cmd, output: r.output, code: r.code });
		} catch {
			setExecResult({ cmd, output: "the command failed to run", code: -1 });
		} finally {
			setRunningCmd(null);
		}
	};
	const nextId = useRef(1);
	// One request per intent: a double-tap on send (or Alt+Enter while a
	// send is in flight) must not POST twice - pi would queue both.
	const sending = useRef(false);

	useEffect(() => {
		writePref(draftStoreKey, text === "" ? null : text);
	}, [draftStoreKey, text]);

	// Autosize: grow with content, cap at half the visual viewport.
	// Clearing (send) skips the read entirely: height auto followed by
	// a scrollHeight read forces synchronous layout mid-send.
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
		if (props.disabled) return;
		if (sending.current) return;
		sending.current = true;
		hapticConfirm();
		const id = props.addPending(body, queued);
		// With attachments the body carries upload paths the textarea
		// never held; only the text part can leave the box.
		const acked = draft ?? body;
		// Optimistic clearing of sent text in the same frame
		setText((cur) => (cur.startsWith(acked) ? cur.slice(acked.length) : cur));
		if (ta.current) {
			ta.current.style.height = "";
		}
		let outcome: "ok" | "fail" | "unknown" = "unknown";
		try {
			const r = props.resume
				? await props.resume(body)
				: await sendPrompt(sessionKey, body, queued ? "followUp" : undefined);
			outcome = r.ok ? "ok" : "fail";
			if (!r.ok)
				setNotice(
					r.error === "notdriven"
						? "No driver runs this session."
						: r.error === "resume failed"
							? "Resume failed. Try again."
							: "Send failed.",
				);
		} catch {
			outcome = "unknown";
		}
		if (outcome === "ok") {
			props.markPending(id, queued ? "queued" : "sent");
		} else if (outcome === "fail") {
			props.removePending(id);
			// Restore draft on failure if not replaced
			setText((cur) => (cur ? `${acked}\n${cur}` : acked));
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
		const batch = picked.map((f) => ({
			id: nextId.current++,
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
		hapticTick();
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

	const runModelsRefresh = () => {
		if (modelsRefreshing) return;
		hapticTick();
		setModelsRefreshing(true);
		refreshModels()
			.then((rows) => {
				setModelRows(rows);
				setToast("Model catalog refreshed");
			})
			.catch(() => setToast("Catalog refresh failed"))
			.finally(() => setModelsRefreshing(false));
	};

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
		// picker overlay, blocking the TUI. When the stall timer fires,
		// dismiss that residue only for an idle agent: pi queues /model
		// while working, and Esc would abort the running turn.
		window.setTimeout(() => {
			const outcome = stalledApply(chosenModelRef.current, p, statusRef.current);
			if (outcome === "confirmed") return;
			setChosenModel(null);
			setModelMark(false);
			if (outcome === "clear-and-dismiss") {
				void sendKeys(sessionKey, ["esc"]);
				setToast("Model not applied");
			}
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
		// writing the jsonl entry; the same idle-only rule applies to
		// dismissing TUI residue.
		window.setTimeout(() => {
			const outcome = stalledApply(chosenThinkingRef.current, level, statusRef.current);
			if (outcome === "confirmed") return;
			setChosenThinking(null);
			setThinkingMark(false);
			if (outcome === "clear-and-dismiss") {
				void sendKeys(sessionKey, ["esc"]);
				setToast("Thinking not applied");
			}
		}, 8000);
	};

	// Pending marks clear when the stream confirms the change. The refs
	// mirror the latest values for the stall timers, whose closures
	// would otherwise read the render-time status and pending value.
	const statusRef = useRef(status);
	const chosenModelRef = useRef(chosenModel);
	const chosenThinkingRef = useRef(chosenThinking);
	useEffect(() => {
		statusRef.current = status;
	}, [status]);
	useEffect(() => {
		chosenModelRef.current = chosenModel;
	}, [chosenModel]);
	useEffect(() => {
		chosenThinkingRef.current = chosenThinking;
	}, [chosenThinking]);
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
		<div
			aria-disabled={props.disabled || undefined}
			className={props.disabled ? "pointer-events-none select-none opacity-50" : undefined}
		>
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
									className={`press-scale h-11 min-w-[44px] shrink-0 rounded-xl border px-2.5 font-mono text-[13px] font-medium shadow-surface-sm transition-all ${
										k.id === "enter"
											? "border-primary/50 border-b-2 bg-primary/10 text-primary active:bg-primary/20"
											: `border-base-content/8 border-b-2 bg-base-200/90 ${enabled ? "text-base-content active:bg-base-300" : "text-muted/30 border-transparent shadow-none"}`
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
				<div className="rounded-3xl border border-base-content/10 bg-base-200/90 p-2 shadow-top-surface backdrop-blur-md">
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
						disabled={props.disabled}
						autoCorrect="off"
						autoCapitalize="off"
						spellCheck={false}
						enterKeyHint="send"
						className="max-h-[50vh] min-h-[40px] w-full resize-none bg-transparent px-2.5 py-1.5 text-[16px] leading-relaxed outline-none"
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
					<div className="flex items-center gap-1">
						<button
							type="button"
							aria-label="Commands"
							className="press-scale grid h-9 w-9 shrink-0 place-items-center rounded-full text-muted active:text-base-content"
							onClick={() => setSheet("commands")}
						>
							<FaTableCellsLarge size={18} />
						</button>
						<div className="relative shrink-0">
							<button
								type="button"
								aria-label="Attach image"
								className="press-scale grid h-9 w-9 place-items-center rounded-full text-muted active:text-base-content"
								onClick={() => setSheet("attach")}
							>
								<FaPaperclip size={18} />
							</button>
							{files.length > 0 && (
								<span className="absolute -top-0.5 -right-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-primary px-1 text-[10px] font-bold text-primary-content shadow-sm">
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
							className="press-scale flex min-w-0 max-w-[46%] shrink items-center gap-1 rounded-full border border-base-content/8 bg-base-300/70 px-3 py-1.5 text-[12px] font-medium text-base-content shadow-surface-sm backdrop-blur-sm"
						>
							<FaSparkles size={13} className="shrink-0 text-primary" />
							{modelMark && <Spinner size={12} className="shrink-0 text-primary" />}
							<span className="truncate">{showModel || "model"}</span>
							<FaChevronDown size={11} className="shrink-0 text-muted" />
						</button>
						<button
							type="button"
							onClick={() => setSheet("thinking")}
							className="press-scale shrink-0 rounded-full border border-base-content/8 bg-base-300/70 px-3 py-1.5 text-[12px] font-medium text-base-content shadow-surface-sm backdrop-blur-sm"
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
							className={`press-scale grid h-10 w-10 shrink-0 place-items-center rounded-full text-[16px] transition-all ${
								canSend
									? "bg-primary text-primary-content shadow-[0_2px_10px_rgba(64,157,212,0.45)]"
									: "bg-base-300/80 text-muted/60"
							}`}
						>
							<FaPaperPlane size={17} />
						</button>
					</div>
				</div>
				{(ctxPct !== null || props.memory) && (
					<div className="flex items-center justify-center gap-3 px-3 py-1 font-mono text-[11px] text-muted tabular-nums">
						{ctxPct !== null && (
							<span className="flex items-center gap-1.5 font-sans">
								<span className="font-mono">ctx</span>
								<span className="h-1 w-10 overflow-hidden rounded-full bg-base-300">
									<span
										className="block h-full rounded-full bg-primary/70"
										style={{ width: `${ctxPct}%` }}
									/>
								</span>
								<span className="tabular-nums font-mono">
									{ctxPct}%{ctxWin && `/${ctxWin}`}
								</span>
							</span>
						)}
						{props.memory && (
							<span role="img" aria-label={`memory ${props.memory}`}>
								<FaBrain size={13} />
							</span>
						)}
					</div>
				)}
			</div>

			{/* quick actions: step 1 picks a kind, step 2 lists it */}
			<Sheet
				className="pb-safe-all rounded-t-3xl border-t border-base-content/10 bg-base-200 shadow-2xl"
				opened={sheet === "commands"}
				onBackdropClick={closeQuick}
			>
				<div className="qa-content flex max-h-[72dvh] flex-col gap-1.5 overflow-y-auto p-4">
					<div className="mx-auto mb-1.5 h-1.5 w-12 shrink-0 rounded-full bg-base-content/25" />
					{qaStep === "menu" ? (
						<>
							<div className="px-1 pb-1 text-[13px] font-semibold tracking-wider text-muted uppercase">
								Quick actions
							</div>
							<QaKind
								icon={<FaSparkles size={17} />}
								title="Skills"
								sub="pi skills, invoked as /skill:name"
								onClick={() => goStep("skills")}
							/>
							<QaKind
								icon={<FaTableCellsLarge size={17} />}
								title="Commands"
								sub="pi slash commands, editable in Settings"
								onClick={() => goStep("commands")}
							/>
							<QaKind
								icon={<FaPaperPlane size={16} />}
								title="Prompts"
								sub="pi prompt templates as /commands"
								onClick={() => goStep("prompts")}
							/>
						</>
					) : (
						<>
							<button
								type="button"
								onClick={() => goStep("menu")}
								className="press-scale -mx-1 flex min-h-[40px] items-center gap-1.5 rounded-xl px-1 text-[13.5px] font-medium text-muted active:text-base-content"
							>
								<FaChevronLeft size={13} /> Quick actions
							</button>
							<input
								type="text"
								value={qaFilter}
								onChange={(e) => setQaFilter(e.target.value)}
								placeholder={`Filter ${qaStep}…`}
								aria-label={`Filter ${qaStep}`}
								autoComplete="off"
								spellCheck={false}
								className="h-10 shrink-0 rounded-2xl border border-base-content/10 bg-base-200/80 px-3.5 font-mono text-[13.5px] text-base-content placeholder:text-muted"
							/>
							{qaStep === "skills" &&
								(qaSkills === null ? (
									<QaNote>Loading skills…</QaNote>
								) : skillsError ? (
									<QaNote>pi skills unavailable.</QaNote>
								) : qaSkills.length === 0 ? (
									<QaNote>{qaQ ? "No matches." : "No pi skills installed."}</QaNote>
								) : (
									qaSkills.map((s) => (
										<QaItem
											key={s.name}
											title={`/skill:${s.name}`}
											sub={s.description}
											onClick={() => fillQuick(`/skill:${s.name} `)}
										/>
									))
								))}
							{qaStep === "commands" &&
								(qaCmdList.length === 0 ? (
									<QaNote>{qaQ ? "No matches." : "No commands configured."}</QaNote>
								) : (
									qaCmdList.map((c) => {
										const isSlash = c.startsWith("/");
										return (
											<div key={c}>
												<QaItem
													title={c}
													sub={
														isSlash
															? "pi command - add arguments after it"
															: "bash - runs in this session's folder"
													}
													onClick={() => (isSlash ? fillQuick(`${c} `) : void runBash(c))}
												/>
												{runningCmd === c && (
													<div className="flex items-center gap-2 px-4 py-2 text-[12px] text-muted">
														<Spinner size={13} /> running…
													</div>
												)}
												{execResult?.cmd === c && runningCmd !== c && (
													<div className="mx-1 rounded-2xl border border-base-content/8 bg-base-100/80 p-3">
														<div
															className={`pb-1 text-[11px] font-semibold ${execResult.code === 0 ? "text-success" : "text-error"}`}
														>
															exit {execResult.code}
														</div>
														<pre className="max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-[11.5px] text-base-content/85">
															{execResult.output || "(no output)"}
														</pre>
													</div>
												)}
											</div>
										);
									})
								))}
							{qaStep === "prompts" &&
								(qaPrompts === null ? (
									<QaNote>Loading prompts…</QaNote>
								) : promptsError ? (
									<QaNote>pi prompts unavailable.</QaNote>
								) : qaPrompts.length === 0 ? (
									<QaNote>{qaQ ? "No matches." : "No prompt templates installed."}</QaNote>
								) : (
									qaPrompts.map((p) => (
										<QaItem
											key={p.name}
											title={`/${p.name}`}
											sub={p.hint ? `${p.description} (${p.hint})` : p.description}
											onClick={() => fillQuick(`/${p.name} `)}
										/>
									))
								))}
						</>
					)}
				</div>
			</Sheet>

			{/* attach sheet */}
			<Sheet
				className="pb-safe-all rounded-t-3xl border-t border-base-content/10 bg-base-200 shadow-2xl"
				opened={sheet === "attach"}
				onBackdropClick={() => setSheet("none")}
			>
				<div className="flex flex-col gap-3 p-4">
					<div className="mx-auto mb-1.5 h-1.5 w-12 rounded-full bg-base-content/25" />
					<div className="flex items-center justify-between px-1">
						<span className="text-[13px] font-semibold tracking-wider text-muted uppercase">
							Attachments
						</span>
						<span className="font-mono text-[11.5px] text-muted">
							{files.length}/{MAX_FILES}
						</span>
					</div>

					<button
						type="button"
						className="press-scale flex w-full items-center justify-center gap-2 rounded-2xl border border-dashed border-base-content/20 bg-base-300/40 px-4 py-3 text-[14px] font-medium text-primary shadow-surface-sm backdrop-blur-sm active:bg-base-300 disabled:opacity-50"
						disabled={files.length >= MAX_FILES}
						onClick={() => fileInput.current?.click()}
					>
						<FaPaperclip size={14} />
						<span>Add attachment</span>
					</button>

					<div className="flex max-h-[35vh] flex-col gap-1.5 overflow-y-auto">
						{files.map((f) => (
							<div
								key={f.id}
								className="flex items-center gap-3 rounded-2xl border border-base-content/8 bg-base-300/60 px-3.5 py-2.5 shadow-surface-sm backdrop-blur-sm"
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
								<span className="min-w-0 flex-1 truncate font-mono text-[13px] text-base-content">
									{f.name}
								</span>
								{f.error && (
									<span className="shrink-0 rounded bg-error/15 px-1.5 py-0.5 text-[10px] font-mono text-error">
										{f.error}
									</span>
								)}
								<button
									type="button"
									aria-label={`Remove ${f.name}`}
									className="press-scale grid h-8 w-8 shrink-0 place-items-center rounded-lg text-muted active:text-error"
									onClick={() => setFiles((cur) => cur.filter((x) => x.id !== f.id))}
								>
									<FaXmark size={14} />
								</button>
							</div>
						))}
						{files.length === 0 && (
							<div className="px-2 py-4 text-center text-[12.5px] text-muted">
								Attach images or files and the agent reads them by path.
							</div>
						)}
					</div>

					<div className="mt-1 flex gap-2.5">
						<button
							type="button"
							className="press-scale flex flex-1 items-center justify-center gap-2 rounded-2xl border border-base-content/10 bg-base-300/80 px-4 py-3 text-[14px] font-medium text-muted shadow-surface-sm active:text-base-content disabled:opacity-40"
							disabled={files.length === 0}
							onClick={() => setFiles([])}
						>
							<FaXmark size={13} />
							<span>Remove all</span>
						</button>
						<button
							type="button"
							className="press-scale flex flex-1 items-center justify-center rounded-2xl bg-primary px-4 py-3 text-[14.5px] font-semibold text-primary-content shadow-[0_2px_12px_rgba(64,157,212,0.4)]"
							onClick={() => setSheet("none")}
						>
							Done
						</button>
					</div>
				</div>
			</Sheet>

			{/* model sheet */}
			<Sheet
				className="pb-safe-all rounded-t-3xl border-t border-base-content/10 bg-base-200 shadow-2xl"
				opened={sheet === "model"}
				onBackdropClick={() => setSheet("none")}
			>
				<div className="flex flex-col gap-3 p-4">
					<div className="mx-auto mb-1.5 h-1.5 w-12 rounded-full bg-base-content/25" />
					<div className="flex flex-col gap-2 px-1">
						<span className="text-[13px] font-semibold tracking-wider text-muted uppercase">
							Select model
						</span>
						<div className="h-px bg-base-content/10" />
						<div className="flex items-center gap-2">
							{props.model && (
								<span className="h-7 min-w-0 flex-1 truncate rounded-md bg-primary/15 px-2 font-mono text-[11.5px] font-semibold leading-7 text-primary">
									current: {props.model}
								</span>
							)}
							<button
								type="button"
								onClick={runModelsRefresh}
								disabled={modelsRefreshing}
								aria-label="Refresh model catalog"
								className="press-scale ml-auto flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-primary/30 bg-primary/15 text-primary transition-colors disabled:opacity-50"
							>
								<FaArrowsRotate size={13} className={modelsRefreshing ? "animate-spin" : ""} />
							</button>
						</div>
					</div>

					<div className="relative">
						<input
							value={modelQuery}
							onChange={(e) => setModelQuery(e.target.value)}
							placeholder="Search or enter model pattern…"
							className="w-full rounded-2xl border border-base-content/10 bg-base-300/70 py-3 pr-4 pl-10 text-[15px] text-base-content placeholder:text-muted/70 shadow-inner outline-none transition-all focus:border-primary/60 focus:bg-base-300 focus:shadow-[0_0_0_2px_rgba(64,157,212,0.2)]"
						/>
						<FaSparkles
							size={14}
							className="pointer-events-none absolute top-1/2 left-3.5 -translate-y-1/2 text-muted"
						/>
					</div>

					{modelQuery.trim() !== "" && (
						<button
							type="button"
							className="press-scale flex w-full items-center justify-between rounded-2xl border border-primary/30 bg-primary/15 px-4 py-3 text-left text-[14px] font-medium text-primary shadow-surface-sm backdrop-blur-sm"
							onClick={() => applyModel(modelQuery)}
						>
							<span className="truncate">Use pattern: {modelQuery.trim()}</span>
							<FaCheck size={13} className="shrink-0" />
						</button>
					)}

					<div className="flex max-h-[48vh] flex-col gap-1.5 overflow-y-auto pr-1">
						{modelRows.map((m) => {
							const selected = m.id === props.model;
							return (
								<button
									key={m.id}
									type="button"
									className={`press-scale flex w-full items-center justify-between rounded-2xl border px-4 py-3 text-left text-[14px] font-mono shadow-surface-sm transition-all ${
										selected
											? "border-primary/50 bg-primary/20 text-base-content"
											: "border-base-content/6 bg-base-300/50 text-base-content/90 active:bg-base-300"
									}`}
									onClick={() => applyModel(m.id)}
								>
									<span className="truncate">{m.id}</span>
									{selected && <FaCheck size={14} className="shrink-0 text-primary" />}
								</button>
							);
						})}
						{modelsLoading && modelRows.length === 0 ? (
							<div className="flex items-center justify-center gap-2 py-8 text-[13px] text-muted">
								<Spinner size={16} className="text-primary" />
								<span>Loading models…</span>
							</div>
						) : modelRows.length === 0 ? (
							<div className="py-8 text-center text-[13px] text-muted">
								No catalog matches. Free text pattern still works.
							</div>
						) : null}
					</div>
				</div>
			</Sheet>

			{/* thinking sheet */}
			<Sheet
				className="pb-safe-all rounded-t-3xl border-t border-base-content/10 bg-base-200 shadow-2xl"
				opened={sheet === "thinking"}
				onBackdropClick={() => setSheet("none")}
			>
				<div className="flex flex-col gap-3 p-4">
					<div className="mx-auto mb-1.5 h-1.5 w-12 rounded-full bg-base-content/25" />
					<div className="px-1 text-[13px] font-semibold tracking-wider text-muted uppercase">
						Thinking budget
					</div>
					<div className="flex flex-col gap-1.5 pt-1">
						{THINKING_LEVELS.map((lv) => {
							const selected = lv === props.thinking;
							return (
								<button
									key={lv}
									type="button"
									className={`press-scale flex w-full items-center justify-between rounded-2xl border px-4 py-3 text-left text-[14.5px] font-medium shadow-surface-sm transition-all ${
										selected
											? "border-primary/50 bg-primary/20 text-base-content"
											: "border-base-content/6 bg-base-300/50 text-base-content/90 active:bg-base-300"
									}`}
									onClick={() => applyThinking(lv)}
								>
									<span className="capitalize">{lv}</span>
									{selected && <FaCheck size={14} className="shrink-0 text-primary" />}
								</button>
							);
						})}
					</div>
				</div>
			</Sheet>

			<Toast opened={toast !== ""}>{toast}</Toast>
		</div>
	);
});

function runQueueEnabled(status: string, hasText: boolean): boolean {
	return status === "working" && hasText;
}

// Enter sends on fine pointers; touch keeps Enter as a newline.
function enterSends(): boolean {
	return effectiveEnterSends();
}

// Quick actions rows: kind cards on the menu step, list entries on the
// second step. Module-level so the memoized Composer keeps stable
// element types across renders.
function QaKind(props: { icon: ReactNode; title: string; sub: string; onClick: () => void }) {
	return (
		<button
			type="button"
			onClick={props.onClick}
			className="press-scale flex w-full items-center gap-3 rounded-2xl border border-base-content/8 bg-base-300/60 px-4 py-3 text-left shadow-surface-sm active:bg-base-300"
		>
			<span className="grid size-9 shrink-0 place-items-center rounded-xl bg-base-200 text-primary">
				{props.icon}
			</span>
			<span className="min-w-0 flex-1">
				<span className="block text-[15px] font-semibold text-base-content">{props.title}</span>
				<span className="block truncate text-[12px] text-muted">{props.sub}</span>
			</span>
			<FaCaretRight size={14} className="shrink-0 text-muted" />
		</button>
	);
}

function QaItem(props: { title: string; sub: string; onClick: () => void }) {
	return (
		<button
			type="button"
			onClick={props.onClick}
			className="press-scale flex w-full flex-col gap-0.5 rounded-2xl border border-base-content/8 bg-base-300/60 px-4 py-2.5 text-left shadow-surface-sm active:bg-base-300"
		>
			<span className="font-mono text-[14px] font-medium text-base-content">{props.title}</span>
			<span className="line-clamp-2 text-[12px] text-muted">{props.sub}</span>
		</button>
	);
}

function QaNote(props: { children: ReactNode }) {
	return <div className="px-1 py-3 text-center text-[12.5px] text-muted">{props.children}</div>;
}
