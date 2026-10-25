import { Dialog, DialogButton } from "konsta/react";
import { memo, useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import {
	closeSession,
	getItemText,
	getTranscriptPage,
	type StreamItem,
	stopSession,
	type Todo,
	type TranscriptAppend,
	type TranscriptReset,
	type TranscriptStatus,
} from "../api";
import { Composer } from "../components/Composer";
import { DialogActions } from "../components/DialogActions";
import {
	FaArrowDown,
	FaCaretDown,
	FaCaretRight,
	FaCheck,
	FaChevronLeft,
	FaCircle,
	FaEllipsis,
	FaGaugeHigh,
	FaHourglassHalf,
	FaXmark,
} from "../components/fa";
import { Markdown } from "../components/Markdown";
import { Spinner } from "../components/Spinner";
import { navigate, useSheetBack } from "../nav";
import { prefKey, readPref, writePref } from "../prefs";
import { TOOLS, toolEnabled } from "../session-tools";
import { useSessions } from "../state/sessions";
import {
	addPending,
	type DriverState,
	emptyDriver,
	groupWorkBlocks,
	markPending,
	mergeOlder,
	type PendingMsg,
	pairTools,
	parseSkillExpansion,
	type SkillExpansion,
	reduceAppend,
	reduceReset,
	reduceStatus,
	removePending,
	type ToolPair,
	type WorkBlock,
} from "../state/transcript";
import { isWaiting, STATUS_DOT } from "../status";
import { connectStream, type StreamStatus } from "../stream";

const LIMIT = 2000; // client-side tail cap; the server re-resets past it
// History page: conversation items per scroll-to-top — the server
// widens the raw fetch past tool rows until this many real messages
// ride along, so one load always shows the conversation advancing.
const PAGE = 5;
// Virtuoso's firstItemIndex headroom: pages decrement it as items
// prepend above, so the index never reaches 0 for a human-scale thread.
const INDEX_BASE = 1_000_000;

let pendingCounter = 0;
const nextPendingId = () => ++pendingCounter;

type Action =
	| { t: "reset"; p: TranscriptReset }
	| { t: "append"; p: TranscriptAppend }
	| { t: "status"; p: TranscriptStatus }
	| { t: "older"; p: StreamItem[] }
	| { t: "add-pending"; id: number; text: string; queued: boolean; at?: number }
	| { t: "mark-pending"; id: number; state: "sent" | "queued" | "unconfirmed" }
	| { t: "remove-pending"; id: number };

function driver(s: DriverState, a: Action): DriverState {
	switch (a.t) {
		case "reset":
			return reduceReset(s, a.p);
		case "append":
			return reduceAppend(s, a.p);
		case "status":
			return reduceStatus(s, a.p);
		case "older":
			return { ...s, items: mergeOlder(s.items, a.p) };
		case "add-pending":
			return addPending(s, a.id, a.text, a.queued, a.at);
		case "mark-pending":
			return markPending(s, a.id, a.state);
		case "remove-pending":
			return removePending(s, a.id);
	}
}

function itemKey(b: WorkBlock): string {
	if (b.type === "status") return "b-status";
	if (b.type === "pending") return `b-pending-${b.msg.id}`;
	if (b.type === "thinking") return `th${b.items[0].seq}`;
	return b.type === "item" ? `s${b.item.seq}` : `b${b.items[0].seq}`;
}

/** Truncated tool output and bash text expand in place: the fetch
 * returns the whole thing once and it stays for the component's life.
 * Content renders diff-colored, since most tool output worth reading
 * is a patch or a log with +/- lines. */
function diffLineClass(ln: string): string {
	if (ln.startsWith("+")) return "text-success";
	if (ln.startsWith("-")) return "text-error";
	if (ln.startsWith("@@")) return "text-accent";
	return "text-muted";
}

function DiffText({ text }: { text: string }) {
	return (
		<div className="whitespace-pre-wrap break-words font-mono text-[11.5px]">
			{text.split("\n").map((ln, i) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: diff rows are positional; the line itself is the identity
				<div key={i} className={diffLineClass(ln)}>
					{ln || "\u00a0"}
				</div>
			))}
		</div>
	);
}

function FullText({ k, it }: { k: string; it: StreamItem }) {
	const [full, setFull] = useState<string | null>(null);
	const [failed, setFailed] = useState(false);
	if (full !== null) {
		return <DiffText text={full} />;
	}
	return (
		<>
			<div className="whitespace-pre-wrap break-words text-muted">{it.text}</div>
			<button
				type="button"
				onClick={() => {
					getItemText(k, it.seq)
						.then(setFull)
						.catch(() => setFailed(true));
				}}
				className="text-[11px] text-accent underline-offset-2 active:scale-[0.98]"
			>
				{failed ? "could not load" : "show all"}
			</button>
		</>
	);
}

// Tool rows read like code clients: "Edited src/a.tsx", "Ran npm test".
// The server's tool summary already carries the path or command; this
// maps the tool name to the past-tense verb in front of it.
function toolVerb(name: string): string {
	const n = name.toLowerCase();
	if (n === "edit") return "Edited";
	if (n === "write") return "Wrote";
	if (n === "read") return "Read";
	if (n === "bash") return "Ran";
	if (n === "grep" || n === "find") return "Searched";
	if (n.startsWith("codegraph")) return "Looked up";
	if (n.startsWith("browser")) return "Browsed";
	if (n.startsWith("todo")) return "Planned";
	if (n.startsWith("memory")) return "Remembered";
	if (n === "git_commit" || n === "commit") return "Committed";
	return name;
}

// mm:ss under an hour, h:mm:ss beyond; the running-row clock.
function fmtElapsed(ms: number): string {
	const s = Math.max(0, Math.floor(ms / 1000));
	const h = Math.floor(s / 3600);
	const m = Math.floor((s % 3600) / 60);
	const sec = s % 60;
	const two = (n: number) => String(n).padStart(2, "0");
	return h > 0 ? `${h}:${two(m)}:${two(sec)}` : `${m}:${two(sec)}`;
}

// Completed-tool duration: "41s", "4m 12s", "1h 03m".
function fmtDur(ms: number): string {
	const s = Math.round(ms / 1000);
	if (s < 60) return `${s}s`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
	const h = Math.floor(m / 60);
	return `${h}h ${String(m % 60).padStart(2, "0")}m`;
}

// Ticks once a second; mounted only on the live tool row.
function Elapsed({ from }: { from: number }) {
	const [, setTick] = useState(0);
	useEffect(() => {
		const id = setInterval(() => setTick((t) => t + 1), 1000);
		return () => clearInterval(id);
	}, []);
	return <>{fmtElapsed(Date.now() - from)}</>;
}

function ToolStep({
	pair,
	sessionKey,
	live = false,
	zoom,
}: {
	pair: ToolPair;
	sessionKey: string;
	live?: boolean;
	zoom: (src: string) => void;
}) {
	const [open, setOpen] = useState(false);
	const first = pair.outs[0];
	const last = pair.outs[pair.outs.length - 1];
	const summary = pair.call ? pair.call.text : (first?.text.split("\n")[0] ?? "");
	return (
		<div className="py-0.5">
			<button
				type="button"
				className="flex min-h-9 w-full items-center gap-2 rounded-lg px-1 py-1 text-left text-[12px]"
				onClick={() => setOpen(!open)}
			>
				<span className="shrink-0 text-muted">
					{open ? <FaCaretDown size={12} /> : <FaCaretRight size={12} />}
				</span>
				{pair.call && (
					<span className="shrink-0 text-base-content">{toolVerb(pair.call.tool_name || "")}</span>
				)}
				<span className="truncate font-mono text-[11px] text-muted">{summary}</span>
				{live && pair.call?.time ? (
					<span className="ml-auto shrink-0 font-mono text-[11px] text-accent tabular-nums">
						<Elapsed from={pair.call.time} />
					</span>
				) : last?.duration_ms ? (
					<span className="ml-auto shrink-0 font-mono text-[11px] text-muted tabular-nums">
						{fmtDur(last.duration_ms)}
					</span>
				) : null}
			</button>
			{open && (
				<div className="ml-3 border-l border-base-300 pl-2">
					{live && pair.call && pair.outs.length === 0 && (
						// The ask tools write only the call while they run; the
						// prompt head is what the terminal shows too.
						<div className="py-0.5 text-[12px]">
							<FullText k={sessionKey} it={pair.call} />
						</div>
					)}
					{pair.outs.map((o) => (
						<div key={o.seq} className="py-0.5 text-[12px]">
							{o.truncated ? <FullText k={sessionKey} it={o} /> : <DiffText text={o.text} />}
							{!!o.images && (
								<ChatImage
									src={`/api/sessions/${encodeURIComponent(sessionKey)}/items/${o.seq}/image`}
									zoom={zoom}
									alt="tool attachment"
									className="mt-1 max-h-64 rounded-xl border border-base-300"
								/>
							)}
						</div>
					))}
				</div>
			)}
		</div>
	);
}

function itemRow(
	it: StreamItem,
	expanded: boolean,
	sessionKey: string,
	zoom: (src: string) => void,
) {
	switch (it.kind) {
		case "user": {
			const exp = parseSkillExpansion(it.text);
			return (
				<div className="flex justify-end px-3.5 py-1.5">
					<div className="max-w-[85%] rounded-2xl rounded-br-sm border border-primary/25 bg-gradient-to-br from-primary/25 to-primary/15 px-3.5 py-2.5 text-[15px] leading-relaxed shadow-surface-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
						{exp && <SkillCard exp={exp} />}
						{exp ? exp.rest : it.text}
						{!!it.images && (
							<ChatImage
								src={`/api/sessions/${encodeURIComponent(sessionKey)}/items/${it.seq}/image`}
								zoom={zoom}
								alt="attachment"
								className="mt-1.5 max-h-64 rounded-xl border border-primary/20"
							/>
						)}
					</div>
				</div>
			);
		}
		case "text":
			return <Markdown text={it.text} />;
		case "marker":
			return (
				<div className="px-3 py-1 text-center text-[11px] tracking-wider text-muted uppercase">
					{it.text}
				</div>
			);
		case "error":
			return (
				<div className="mx-3 my-1 rounded-lg border border-error/30 bg-error/10 px-3 py-2">
					<div className="text-[10px] font-medium uppercase tracking-wider text-error/80">
						api error
					</div>
					<div className="mt-0.5 break-words font-mono text-[12px] text-error [overflow-wrap:anywhere]">
						{it.text}
					</div>
				</div>
			);
		case "tool_call":
			return (
				<div className="flex gap-2 px-3 py-0.5 text-[13px]">
					<span className="shrink-0 rounded bg-base-300 px-1.5 font-mono text-[11px] text-accent">
						{it.tool_name || it.kind}
					</span>
					<span className="truncate text-muted">{it.text}</span>
				</div>
			);
		case "bash":
			// Bash output is the one truncated row the show-all affordance
			// skipped; it gets the same fetch-once expansion as tool results.
			return (
				<div className="px-3 py-0.5 pl-10 text-[12px]">
					{it.truncated ? (
						<FullText k={sessionKey} it={it} />
					) : (
						<div className="flex gap-2 text-[13px]">
							<span className="shrink-0 rounded bg-base-300 px-1.5 font-mono text-[11px] text-accent">
								{it.tool_name || it.kind}
							</span>
							<span className="truncate text-muted">{it.text}</span>
						</div>
					)}
				</div>
			);
		case "tool_result":
			return (
				<div className="px-3 py-0.5 pl-10 text-[12px]">
					{it.truncated ? (
						<FullText k={sessionKey} it={it} />
					) : expanded ? (
						<div className="whitespace-pre-wrap break-words text-muted">{it.text}</div>
					) : (
						<span className="line-clamp-1 text-muted">{it.text}</span>
					)}
					{!!it.images && (
						<ChatImage
							src={`/api/sessions/${encodeURIComponent(sessionKey)}/items/${it.seq}/image`}
							zoom={zoom}
							alt="tool attachment"
							className="mt-1 max-h-64 rounded-xl border border-base-300"
						/>
					)}
				</div>
			);
		default:
			return it.truncated ? (
				<div className="px-3 py-0.5 text-[12px]">
					<FullText k={sessionKey} it={it} />
				</div>
			) : (
				<div className="px-3 py-0.5 text-[12px] text-muted">{it.text}</div>
			);
	}
}

// Tappable chat image: opens the fullscreen viewer. Konsta ships no
// photo viewer and Popup is a bare container, so the lightbox is a
// fixed overlay like the app's other full-screen layers.
function ChatImage({
	src,
	zoom,
	alt,
	className,
}: {
	src: string;
	zoom: (src: string) => void;
	alt: string;
	className: string;
}) {
	return (
		<button type="button" className="press-scale block cursor-zoom-in" onClick={() => zoom(src)}>
			<img
				src={src}
				alt={alt}
				loading="lazy"
				draggable={false}
				className={`${className} aspect-auto min-h-[60px] bg-base-300/60 object-contain`}
			/>
		</button>
	);
}

const ZOOM_MAX = 8;

// Fullscreen image viewer: pinch or wheel to zoom, double-tap toggles
// 2.5x at the tapped point, drag pans, tap the dark area or X closes.
function Lightbox({ src, onClose }: { src: string; onClose: () => void }) {
	const [t, setT] = useState({ s: 1, x: 0, y: 0 });
	const ptrs = useRef(new Map<number, { x: number; y: number }>());
	const pinch = useRef<{
		d0: number;
		s0: number;
		m0x: number;
		m0y: number;
		x: number;
		y: number;
	} | null>(null);
	const drag = useRef<{ px: number; py: number; x: number; y: number } | null>(null);
	const moved = useRef(false);
	const box = useRef<HTMLDivElement | null>(null);

	const clamp = (v: number) => Math.min(ZOOM_MAX, Math.max(1, v));
	const apply = (s: number, x: number, y: number) =>
		setT(s <= 1 ? { s: 1, x: 0, y: 0 } : { s, x, y });

	const onDown = (e: React.PointerEvent) => {
		(e.target as Element).setPointerCapture?.(e.pointerId);
		ptrs.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
		moved.current = false;
		if (ptrs.current.size === 2) {
			const [a, b] = [...ptrs.current.values()];
			pinch.current = {
				d0: Math.hypot(a.x - b.x, a.y - b.y),
				s0: t.s,
				m0x: (a.x + b.x) / 2,
				m0y: (a.y + b.y) / 2,
				x: t.x,
				y: t.y,
			};
			drag.current = null;
		} else {
			drag.current = { px: e.clientX, py: e.clientY, x: t.x, y: t.y };
		}
	};
	const onMove = (e: React.PointerEvent) => {
		if (!ptrs.current.has(e.pointerId)) return;
		ptrs.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
		moved.current = true;
		if (pinch.current && ptrs.current.size >= 2) {
			const [a, b] = [...ptrs.current.values()];
			const d = Math.hypot(a.x - b.x, a.y - b.y);
			const mx = (a.x + b.x) / 2;
			const my = (a.y + b.y) / 2;
			const p = pinch.current;
			apply(clamp(p.s0 * (d / p.d0)), p.x + (mx - p.m0x), p.y + (my - p.m0y));
		} else if (drag.current && t.s > 1) {
			const dg = drag.current;
			apply(t.s, dg.x + (e.clientX - dg.px), dg.y + (e.clientY - dg.py));
		}
	};
	const onUp = (e: React.PointerEvent) => {
		ptrs.current.delete(e.pointerId);
		if (ptrs.current.size < 2) pinch.current = null;
		if (ptrs.current.size === 0) drag.current = null;
		if (t.s <= 1) apply(1, 0, 0);
	};
	const onDouble = (e: React.MouseEvent) => {
		const r = box.current?.getBoundingClientRect();
		if (!r) return;
		if (t.s > 1) {
			apply(1, 0, 0);
			return;
		}
		// Keep the tapped point fixed under the new scale.
		const S = 2.5;
		const cx = e.clientX - r.left - r.width / 2;
		const cy = e.clientY - r.top - r.height / 2;
		apply(S, cx * (t.s - S), cy * (t.s - S));
	};

	return (
		// biome-ignore lint/a11y/useKeyWithClickEvents: backdrop tap-close is a pointer affordance; keyboard users close via the labeled Close button
		<div
			ref={box}
			className="fixed inset-0 z-[60] flex touch-none items-center justify-center bg-black/95"
			role="dialog"
			aria-label="Image viewer"
			onPointerDown={onDown}
			onPointerMove={onMove}
			onPointerUp={onUp}
			onPointerCancel={onUp}
			onDoubleClick={onDouble}
			onWheel={(e) => apply(clamp(t.s * (e.deltaY < 0 ? 1.15 : 0.87)), t.x, t.y)}
			onClick={() => {
				if (t.s <= 1 && !moved.current) onClose();
			}}
		>
			<img
				src={src}
				alt="attachment enlarged"
				draggable={false}
				className="max-h-full max-w-full select-none object-contain"
				style={{ transform: `translate(${t.x}px, ${t.y}px) scale(${t.s})` }}
			/>
			<button
				type="button"
				aria-label="Close image"
				className="absolute right-3 top-3 grid h-11 w-11 place-items-center rounded-full bg-white/10 text-base-content"
				onPointerDown={(e) => e.stopPropagation()}
				onClick={(e) => {
					e.stopPropagation();
					onClose();
				}}
			>
				<FaXmark size={18} />
			</button>
		</div>
	);
}

const ThinkingGroup = memo(function ThinkingGroup({ items }: { items: StreamItem[] }) {
	const [open, setOpen] = useState(false);
	return (
		<div className="px-3 py-1">
			<button
				type="button"
				className="press-scale flex min-h-[38px] w-full items-center gap-2 rounded-xl border border-secondary/35 bg-secondary/10 px-3 py-2 text-left text-[12px] text-secondary shadow-surface-sm"
				onClick={() => setOpen(!open)}
			>
				<span>{open ? <FaCaretDown size={12} /> : <FaCaretRight size={12} />}</span>
				<span className="shrink-0 font-medium italic">Thinking</span>
				<span className="rounded-full bg-secondary/20 px-1.5 py-0.5 text-[10px] font-mono opacity-80">
					{items.length}
				</span>
			</button>
			{open && (
				<div className="mt-0.5 space-y-2 px-1">
					{items.map((it) => (
						<div
							key={it.seq}
							className="text-[13px] whitespace-pre-wrap break-words text-muted italic"
						>
							{it.text}
						</div>
					))}
				</div>
			)}
		</div>
	);
});

// pi paints an expanded $skill as a folded purple box-card in its own
// terminal; the chat mirrors that instead of showing the whole skill
// block as bubble text.
const SkillCard = memo(function SkillCard({ exp }: { exp: SkillExpansion }) {
	const [open, setOpen] = useState(false);
	return (
		<>
			<button
				type="button"
				className="mb-1.5 flex w-full items-center gap-1.5 rounded-xl border border-secondary/35 bg-secondary/10 px-2.5 py-1.5 text-left text-[12px] text-secondary"
				onClick={() => setOpen(!open)}
			>
				<span>{open ? <FaCaretDown size={12} /> : <FaCaretRight size={12} />}</span>
				<span className="font-medium">skill {exp.name}</span>
			</button>
			{open && (
				<div className="mb-1.5 max-h-72 overflow-y-auto rounded-xl border border-secondary/25 bg-secondary/5 px-2.5 py-2 text-[12px] leading-relaxed whitespace-pre-wrap break-words text-muted">
					{exp.body}
				</div>
			)}
		</>
	);
});

const Block = memo(function Block({
	block,
	sessionKey,
	zoom,
}: {
	block: WorkBlock;
	sessionKey: string;
	zoom: (src: string) => void;
}) {
	const [override, setOverride] = useState<boolean | null>(null);
	if (block.type === "pending") {
		const p = block.msg;
		return (
			// Same box as the real user row: when the append frame swaps
			// this for the item the height must not change, or Virtuoso
			// re-measures and the view twitches.
			<div className="flex justify-end px-3.5 py-1.5">
				<div
					className={`bubble-in max-w-[85%] rounded-2xl rounded-br-sm border border-primary/25 bg-gradient-to-br from-primary/25 to-primary/15 px-3.5 py-2.5 text-[15px] leading-relaxed shadow-surface-sm whitespace-pre-wrap [overflow-wrap:anywhere] ${
						p.state === "unconfirmed" ? "border-warning/40 bg-warning/20 shadow-none" : ""
					} ${p.state === "sending" ? "opacity-75" : ""}`}
				>
					{p.state === "sending" && <Spinner size={14} className="mr-1 text-primary" />}
					{p.state === "queued" && <FaHourglassHalf size={13} className="mr-1 inline-block" />}
					{p.text}
					{p.state === "queued" && (
						<div className="mt-1 text-[10px] text-muted">queued for the end of the turn</div>
					)}
					{p.state === "unconfirmed" && (
						<div className="mt-1 text-[10px] text-warning">
							Not confirmed. Check the transcript before sending again.
						</div>
					)}
				</div>
			</div>
		);
	}
	if (block.type === "status") {
		return block.dialog || block.awaiting ? (
			<div className="flex items-center gap-2 px-3 py-2 text-[13px] text-accent">
				<Spinner size={14} />
				{block.dialog
					? `pi asks: ${block.dialog.title || block.dialog.kind} — open Terminal`
					: "Waiting for input — open Terminal"}
			</div>
		) : (
			<div className="flex items-center gap-2 px-3 py-2 text-[13px] text-muted">
				<Spinner size={14} className={block.status === "blocked" ? "text-error" : "text-primary"} />
				{block.status === "blocked" ? "Waiting for you" : "Working"}
			</div>
		);
	}
	if (block.type === "thinking") {
		return <ThinkingGroup items={block.items} />;
	}
	if (block.type === "item") {
		return itemRow(block.item, true, sessionKey, zoom);
	}
	// Steps stay collapsed unless the reader opens them; the live stream
	// already carries the Working marker, so auto-expansion is noise.
	// Expanded, each tool is one titled row (verb + target) with its
	// output nested underneath — the code-client look.
	const pairs = pairTools(block.items);
	const trailing = pairs[pairs.length - 1];
	const live = !!trailing?.call && trailing.outs.length === 0;
	// Steps stay collapsed unless the reader opens them - except a
	// running tool, which stays visible with its live clock until the
	// result lands, then the group folds back.
	const open = override ?? live;
	const names = [...new Set(block.items.map((i) => i.tool_name || i.kind))].slice(0, 3).join(", ");
	return (
		<div className="px-3 py-1">
			<button
				type="button"
				className="press-scale flex min-h-[38px] w-full items-center gap-2 rounded-xl border border-base-content/6 bg-base-200/75 px-3 py-2 text-left text-[12px] text-muted shadow-surface-sm"
				onClick={() => setOverride(!open)}
			>
				<span className="text-muted/80">
					{open ? <FaCaretDown size={12} /> : <FaCaretRight size={12} />}
				</span>
				<span className="font-medium text-base-content/85">
					{block.items.length} {block.items.length === 1 ? "step" : "steps"} · {names}
					{block.items.length > 3 ? "…" : ""}
				</span>
			</button>
			{open && (
				<div className="mt-0.5">
					{pairs.map((p) => (
						<ToolStep
							key={p.call?.seq ?? p.outs[0]?.seq ?? "pair"}
							pair={p}
							sessionKey={sessionKey}
							live={live && p === trailing}
							zoom={zoom}
						/>
					))}
				</div>
			)}
		</div>
	);
});

export default function SessionScreen({ sessionKey }: { sessionKey: string }) {
	const [state, dispatch] = useReducer(driver, emptyDriver);
	const [conn, setConn] = useState<StreamStatus>("connecting");
	const [query, setQuery] = useState<string | null>(null);
	const [overflow, setOverflow] = useState(false);
	const [confirmClose, setConfirmClose] = useState(false);
	// Reverse pagination: firstItemIndex drops as older pages prepend so
	// Virtuoso anchors the scroll instead of jumping.
	const [firstItemIndex, setFirstItemIndex] = useState(INDEX_BASE);
	const [moreHistory, setMoreHistory] = useState(true);
	const [loadingOlder, setLoadingOlder] = useState(false);
	const paging = useRef(false);
	useSheetBack(overflow, () => setOverflow(false));
	useSheetBack(confirmClose, () => setConfirmClose(false));
	const [lightbox, setLightbox] = useState<string | null>(null);
	useSheetBack(!!lightbox, () => setLightbox(null));
	const [notice, setNotice] = useState("");
	const [unreadCount, setUnreadCount] = useState(0);
	const [atBottom, setAtBottom] = useState(true);
	const pinned = useRef(true);
	const virtuoso = useRef<VirtuosoHandle | null>(null);
	const scroller = useRef<HTMLDivElement | null>(null);
	const dragStart = useRef<{ y: number } | null>(null);
	const taBlur = useRef<HTMLTextAreaElement | null>(null);

	const [streamRetry, setStreamRetry] = useState(0);
	const retried = useRef(false);
	// biome-ignore lint/correctness/useExhaustiveDependencies: streamRetry is intentionally tracked to trigger reconnections.
	useEffect(() => {
		const stream = connectStream(`/api/stream/transcript/${encodeURIComponent(sessionKey)}`, {
			onStatus: setConn,
			onEvent: (name, data) => {
				if (name === "reset") {
					// A reset replaces items with the fresh tail: history pages
					// and their index bookkeeping restart with it.
					setFirstItemIndex(INDEX_BASE);
					setMoreHistory(true);
					dispatch({ t: "reset", p: JSON.parse(data) as TranscriptReset });
				} else if (name === "append") {
					dispatch({ t: "append", p: JSON.parse(data) as TranscriptAppend });
					if (!pinned.current) {
						setUnreadCount((c) => c + 1);
					}
				} else if (name === "status")
					dispatch({ t: "status", p: JSON.parse(data) as TranscriptStatus });
			},
		});
		return () => stream.close();
	}, [sessionKey, streamRetry]);

	// A radio blip during connect can leave the SSE silently dead — no
	// error event, no frames, an eternal spinner. If nothing has ever
	// carried a status, force one reconnect.
	// biome-ignore lint/correctness/useExhaustiveDependencies: sessionKey resets retried flag on screen change.
	useEffect(() => {
		retried.current = false;
	}, [sessionKey]);
	// biome-ignore lint/correctness/useExhaustiveDependencies: sessionKey resets the timeout per session.
	useEffect(() => {
		if (state.status !== "" || retried.current) return;
		const t = setTimeout(() => {
			retried.current = true;
			setStreamRetry((n) => n + 1);
		}, 12000);
		return () => clearTimeout(t);
	}, [state.status, sessionKey]);

	const blocks = useMemo(() => {
		const q = query?.trim().toLowerCase();
		const items = q ? state.items.filter((it) => it.text.toLowerCase().includes(q)) : state.items;
		const base =
			items.length > LIMIT
				? groupWorkBlocks(items.slice(items.length - LIMIT))
				: groupWorkBlocks(items);
		const out: WorkBlock[] = [...base];
		for (const msg of state.pending) out.push({ type: "pending", msg });
		if (state.dialog || state.awaiting || isWaiting(state.status))
			out.push({
				type: "status",
				dialog: state.dialog,
				awaiting: state.awaiting,
				status: state.status,
			});
		return out;
	}, [state.items, state.pending, state.dialog, state.awaiting, state.status, query]);

	// One history page per top touch. Skipped mid-search: the filtered
	// block list would desync firstItemIndex bookkeeping.
	const loadOlder = () => {
		const oldest = state.items[0];
		if (paging.current || !moreHistory || !oldest || query || state.items.length >= LIMIT) return;
		paging.current = true;
		setLoadingOlder(true);
		getTranscriptPage(sessionKey, oldest.seq, PAGE)
			.then((page) => {
				const merged = mergeOlder(state.items, page.items);
				if (merged.length !== state.items.length) {
					const delta = groupWorkBlocks(merged).length - groupWorkBlocks(state.items).length;
					dispatch({ t: "older", p: page.items });
					if (delta > 0) setFirstItemIndex((fi) => fi - delta);
				}
				setMoreHistory(page.more && merged.length < LIMIT);
			})
			.catch(() => {})
			.finally(() => {
				paging.current = false;
				setLoadingOlder(false);
			});
	};

	// Virtuoso latches startReached against the pre-stream empty list, so
	// the first tail arrival re-checks: a window still parked at the top
	// (short thread, no scroll) fetches its first history page here.
	const loadOlderRef = useRef(loadOlder);
	loadOlderRef.current = loadOlder;
	const hadItems = state.items.length > 0;
	useEffect(() => {
		if (!hadItems) return;
		const t = setTimeout(() => {
			const sc = scroller.current?.querySelector("[data-virtuoso-scroller]");
			if (sc && sc.scrollTop <= 0) loadOlderRef.current();
		}, 60);
		return () => clearTimeout(t);
	}, [hadItems]);

	const meta = useSessions((s) => s.sessions.find((x) => x.key === sessionKey));
	const title =
		state.header?.name ||
		state.header?.first_user ||
		meta?.name ||
		meta?.first_user ||
		state.items.find((it) => it.kind === "user")?.text.slice(0, 60) ||
		"Session";
	const dot = STATUS_DOT[state.status] ?? "bg-muted";
	const hasItems = state.items.length > 0;
	// Stable identities: fresh closures per tick would re-render every
	// mounted row (and re-parse its markdown) on each stream frame.
	// Fixed-height header region: the loading spinner and the
	// exhaustion note swap inside a constant 36px box, so history loads
	// never shift the list (the old appear/disappear header jumped the
	// content twice per load — the scroll-back clunk).
	const listHeader = useMemo(
		() => ({
			Header: () =>
				!hasItems ? null : (
					<div className="flex h-9 items-center justify-center gap-2 text-[12px] text-muted">
						{loadingOlder ? (
							<>
								<Spinner size={14} />
								Loading older messages…
							</>
						) : !moreHistory ? (
							<span className="text-[11px] text-muted/60">Start of the thread</span>
						) : null}
					</div>
				),
		}),
		[loadingOlder, moreHistory, hasItems],
	);
	const renderItem = useCallback(
		(_: number, b: WorkBlock) => <Block block={b} sessionKey={sessionKey} zoom={setLightbox} />,
		[sessionKey],
	);
	const addPendingCb = useCallback((text: string, queued: boolean) => {
		const id = nextPendingId();
		dispatch({ t: "add-pending", id, text, queued });
		pinned.current = true;
		virtuoso.current?.autoscrollToBottom();
		return id;
	}, []);
	const markPendingCb = useCallback(
		(id: number, mark: Exclude<PendingMsg["state"], "sending">) =>
			dispatch({ t: "mark-pending", id, state: mark }),
		[],
	);
	const removePendingCb = useCallback((id: number) => dispatch({ t: "remove-pending", id }), []);
	// Queued bubbles live only in this reducer, and a queued message is
	// not in the transcript yet — leaving the thread or reloading would
	// otherwise hide it while pi still holds it in its steering queue.
	// Mirror the list to localStorage and restore it on mount; the
	// reset's reconcile drops any bubble whose item landed meanwhile.
	useEffect(() => {
		const storageKey = prefKey(`pending.${sessionKey}`);
		const raw = readPref(storageKey);
		if (!raw) return;
		try {
			const msgs = JSON.parse(raw) as PendingMsg[];
			for (const m of msgs) {
				if (typeof m.text !== "string" || m.text === "") continue;
				dispatch({
					t: "add-pending",
					id: nextPendingId(),
					text: m.text,
					queued: true,
					at: typeof m.at === "number" ? m.at : 0,
				});
			}
		} catch {
			// junk in storage: drop it
		}
	}, [sessionKey]);
	useEffect(() => {
		const storageKey = prefKey(`pending.${sessionKey}`);
		writePref(storageKey, state.pending.length === 0 ? null : JSON.stringify(state.pending));
	}, [state.pending, sessionKey]);
	// The sheet opens only when the dialog carries real content. The
	// bridge frame can land before the pane parse (up to a detector
	// tick); an empty panel sliding up reads as broken, and the
	// model-picker dialog never parses at all - which is correct, it
	// must not be answered from here.
	const sheetReady = !!(
		state.dialog &&
		((state.dialog.options?.length ?? 0) > 0 ||
			state.dialog.body ||
			state.dialog.kind === "input" ||
			// An emptied editor buffer is legitimate: the card's empty
			// box plus Submit is the recovery path after a refused sweep.
			state.dialog.kind === "editor")
	);

	// Hold the last card while the sheet slides out: unmounting at the
	// first null frame collapses the panel height mid-transition, which
	// read as a stuck sliver. Cleared just after the slide ends.
	const [lastDialog, setLastDialog] = useState(state.dialog ?? null);
	useEffect(() => {
		if (state.dialog) {
			setLastDialog(state.dialog);
			return;
		}
		const t = setTimeout(() => setLastDialog(null), 350);
		return () => clearTimeout(t);
	}, [state.dialog]);
	const cardDialog = state.dialog ?? lastDialog;

	const runStop = async () => {
		setOverflow(false);
		const r = await stopSession(sessionKey);
		if (!r.ok) setNotice(r.error === "notdriven" ? "No driver runs this session." : "Stop failed.");
	};

	const runClose = async () => {
		setOverflow(false);
		setConfirmClose(false);
		const r = await closeSession(sessionKey);
		if (!r.ok) {
			setNotice(r.error === "notdriven" ? "No driver runs this session." : "Close failed.");
			return;
		}
		navigate("/", { dir: "back" });
	};

	// Fixed root: out of document flow, so focus scroll and flicks can
	// never drag the header under the status bar.
	return (
		<div
			className="fixed inset-x-0 bottom-0 flex flex-col bg-base-100"
			style={{
				top: "var(--safe-top, 0px)",
				paddingBottom: "var(--safe-bottom, 0px)",
			}}
		>
			<header className="header-glass flex h-12 shrink-0 items-center gap-1.5 border-b border-base-content/8 px-1.5 shadow-surface-sm">
				<button
					type="button"
					onClick={() => history.back()}
					aria-label="Back"
					className="press-scale flex size-11 items-center justify-center rounded-xl text-muted active:text-base-content"
				>
					<FaChevronLeft size={20} />
				</button>
				<div className="flex min-w-0 items-center gap-2">
					{isWaiting(state.status) ? (
						<Spinner
							size={14}
							className={state.status === "blocked" ? "text-error" : "text-primary"}
						/>
					) : (
						<span
							className={`size-2.5 shrink-0 rounded-full transition-all duration-300 ${dot}`}
							title={state.status || "idle"}
						/>
					)}
					<span className="truncate text-[15.5px] font-semibold tracking-tight text-base-content">
						{title}
					</span>
					{(state.dialog || state.awaiting) && (
						<span
							className="shrink-0 rounded-full border border-accent/60 bg-accent/10 px-2 py-0.5 text-[11px] font-medium text-accent shadow-[0_0_6px_rgba(55,181,180,0.3)]"
							title="pi waits on a dialog - open the Terminal"
						>
							{state.dialog ? state.dialog.title || state.dialog.kind : "input"}
						</span>
					)}
				</div>
				<button
					type="button"
					aria-label="Provider Quotas"
					onClick={() => navigate("/quotas", { dir: "fwd" })}
					className="press-scale ml-auto grid h-11 w-11 place-items-center rounded-xl text-muted active:text-base-content"
				>
					<FaGaugeHigh size={20} />
				</button>
			</header>
			{/* tool strip */}
			<div
				className="flex gap-1.5 overflow-x-auto border-b border-base-content/6 bg-base-200/50 px-2.5 py-1.5 backdrop-blur-sm"
				style={{ scrollbarWidth: "none" }}
			>
				{TOOLS.filter((t) => t.placement === "strip").map((t) => (
					<button
						key={t.id}
						type="button"
						disabled={t.disabled}
						className={`press-scale min-h-[36px] shrink-0 rounded-xl px-3.5 text-[13px] font-medium transition-all ${
							t.disabled ? "text-muted/40" : "text-base-content active:bg-base-300"
						} ${t.id === "search" && query !== null ? "bg-base-300 text-accent font-semibold" : "bg-base-300/40"}`}
						onClick={() => {
							if (t.id === "search") setQuery(query === null ? "" : null);
							else if (t.href) navigate(t.href(sessionKey), { replace: true, dir: "side" });
						}}
					>
						{t.label}
					</button>
				))}
				<button
					type="button"
					className="press-scale ml-auto min-h-[36px] shrink-0 rounded-xl bg-base-300/40 px-3 text-[13px] text-base-content active:bg-base-300"
					onClick={() => setOverflow(true)}
				>
					<FaEllipsis size={18} />
				</button>
			</div>
			{query !== null && (
				<div className="border-b border-base-content/8 bg-base-200/40 px-3 py-1.5 backdrop-blur-sm">
					<input
						value={query}
						onChange={(e) => setQuery(e.target.value)}
						placeholder="Filter the transcript…"
						className="w-full rounded-xl border border-base-content/10 bg-base-200 px-3.5 py-2 text-[15px] outline-none transition-all focus:border-primary/60 focus:shadow-[0_0_0_2px_rgba(64,157,212,0.18)]"
					/>
				</div>
			)}
			{state.todos.length > 0 && <TodosCard todos={state.todos} />}
			{conn === "offline" && (
				<div className="bg-error/20 px-3 py-1 text-center text-[12px] text-error">
					Reconnecting…
				</div>
			)}
			{notice && <div className="bg-warning/20 px-3 py-1 text-center text-[12px]">{notice}</div>}

			<div
				ref={scroller}
				className="relative min-h-0 flex-1"
				style={{ overscrollBehavior: "contain" }}
				onPointerDown={(e) => {
					dragStart.current = e.pointerType === "touch" ? { y: e.clientY } : null;
				}}
				onPointerUp={(e) => {
					const start = dragStart.current;
					dragStart.current = null;
					if (!start) return;
					// A tap, or a 32px+ downward drag, dismisses the keyboard.
					if (e.clientY - start.y >= 32 || Math.abs(e.clientY - start.y) < 8) {
						taBlur.current?.blur();
					}
				}}
			>
				<Virtuoso
					ref={virtuoso}
					data={blocks}
					computeItemKey={(_, b) => itemKey(b)}
					firstItemIndex={firstItemIndex}
					startReached={loadOlder}
					followOutput={(isAtBottom) => (pinned.current && isAtBottom ? "auto" : false)}
					atBottomThreshold={90}
					atBottomStateChange={(bottom) => {
						pinned.current = bottom;
						setAtBottom(bottom);
						if (bottom) setUnreadCount(0);
					}}
					initialTopMostItemIndex={Math.max(0, blocks.length - 1)}
					className="h-full"
					itemContent={renderItem}
					components={listHeader}
				/>
				{blocks.length === 0 && (
					<div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 text-[13px] text-muted">
						{state.status === "" ? <Spinner size={22} /> : <span>Nothing here yet.</span>}
					</div>
				)}
				{!atBottom && (
					<button
						type="button"
						onClick={() => {
							pinned.current = true;
							virtuoso.current?.scrollToIndex({
								index: blocks.length - 1,
								align: "end",
								behavior: "smooth",
							});
							setUnreadCount(0);
						}}
						className="press-scale absolute bottom-3 right-4 z-20 flex items-center gap-1.5 rounded-full border border-base-content/10 bg-base-200/95 px-3.5 py-2 text-[12px] font-medium text-base-content shadow-surface-lg backdrop-blur-md active:bg-base-300"
					>
						<FaArrowDown size={12} className="text-primary" />
						<span>Latest</span>
						{unreadCount > 0 && (
							<span className="ml-1 rounded-full bg-primary px-1.5 py-0.2 text-[10px] font-bold text-primary-content">
								{unreadCount}
							</span>
						)}
					</button>
				)}
			</div>

			<Composer
				sessionKey={sessionKey}
				status={state.status}
				model={state.model}
				thinking={state.thinking}
				ctxTokens={state.ctx}
				memory={state.memory}
				cost={state.header?.total_cost ?? 0}
				cwd={state.header?.cwd ?? ""}
				addPending={addPendingCb}
				markPending={markPendingCb}
				removePending={removePendingCb}
			/>

			{/* The answering sheet: slides over the composer bottom-to-top
			    while pi waits. It stays down until the pane parse has the
			    actual content, so it appears once, complete, instead of an
			    empty panel that fills in later. The scrim matches every
			    other panel: the screen dims to say this needs action. It
			    blocks tap-through — answering (or Cancel) is the way out. */}
			<div
				aria-hidden={!sheetReady}
				className={`fixed inset-0 z-40 bg-black/50 transition-opacity duration-200 ease-in ${
					sheetReady ? "opacity-100" : "pointer-events-none opacity-0"
				}`}
			/>
			<div
				aria-hidden={!sheetReady}
				className={`fixed inset-x-0 bottom-0 z-50 transition-transform duration-200 ease-in ${
					sheetReady ? "translate-y-0" : "pointer-events-none translate-y-full"
				}`}
			>
				<div className="rounded-t-2xl border-t border-base-300 bg-base-200 pb-safe-all shadow-2xl">
					<div className="mx-auto mt-2 h-1 w-10 rounded-full bg-base-content/20" />
					{cardDialog && <DialogActions key={cardDialog.id} k={sessionKey} dialog={cardDialog} />}
				</div>
			</div>
			{lightbox && <Lightbox src={lightbox} onClose={() => setLightbox(null)} />}

			{/* The ⋯ command panel: drops top-to-bottom under the safe area
			    and lists every tool on the bar — the strip items and the
			    overflow actions alike — so the whole bar is reachable from
			    one tap. Modern frosted top drawer with M3 emphasized deceleration. */}
			<div
				aria-hidden={!overflow}
				className={`fixed inset-0 z-40 bg-black/60 backdrop-blur-xs transition-opacity duration-250 ease-out ${
					overflow ? "opacity-100" : "pointer-events-none opacity-0"
				}`}
				onClick={() => setOverflow(false)}
			/>
			<div
				aria-hidden={!overflow}
				className={`fixed inset-x-0 top-0 z-50 transition-all duration-300 ease-[cubic-bezier(0.05,0.7,0.1,1)] ${
					overflow ? "translate-y-0 opacity-100" : "pointer-events-none -translate-y-4 opacity-0"
				}`}
			>
				{/* The inset pads inside the box so the closed slide
				    clears the screen edge. */}
				<div
					className="rounded-b-[2rem] border-b border-base-content/10 bg-base-200/95 p-4 shadow-[0_12px_40px_rgba(0,0,0,0.65)] backdrop-blur-2xl"
					style={{ paddingTop: "calc(var(--safe-top, 0px) + 0.75rem)" }}
				>
					<div className="mb-3 flex items-center justify-between px-1">
						<span className="text-[12px] font-bold uppercase tracking-wider text-muted">
							Session Actions
						</span>
						<button
							type="button"
							onClick={() => setOverflow(false)}
							aria-label="Close"
							className="press-scale flex size-8 items-center justify-center rounded-full bg-base-300/80 text-muted active:text-base-content"
						>
							<FaXmark size={14} />
						</button>
					</div>

					<div className="grid grid-cols-2 gap-2 pb-2">
						{TOOLS.map((t) => {
							const enabled = toolEnabled(t, state.status);
							const isDangerous = t.id === "close" || t.id === "stop";
							const isSearchActive = t.id === "search" && query !== null;

							return (
								<button
									key={t.id}
									type="button"
									disabled={!enabled}
									onClick={() => {
										setOverflow(false);
										if (t.id === "search") setQuery(query === null ? "" : null);
										else if (t.id === "stop") void runStop();
										else if (t.id === "close") setConfirmClose(true);
										else if (t.href) navigate(t.href(sessionKey), { replace: true, dir: "side" });
									}}
									className={`press-scale flex min-h-[50px] items-center justify-between rounded-2xl border px-3.5 py-2.5 text-left transition-all ${
										!enabled
											? "border-base-content/5 bg-base-300/20 text-muted/30"
											: isDangerous
												? "border-error/20 bg-error/10 text-error active:bg-error/20"
												: isSearchActive
													? "border-accent/40 bg-accent/15 text-accent shadow-surface-sm"
													: "border-base-content/8 bg-base-300/60 text-base-content shadow-surface-sm active:bg-base-300"
									}`}
								>
									<span className="text-[14.5px] font-medium leading-snug">{t.label}</span>
									{isSearchActive && (
										<span className="size-2 rounded-full bg-accent shadow-[0_0_8px_rgba(55,181,180,0.8)]" />
									)}
								</button>
							);
						})}
					</div>

					<div className="mx-auto mt-2 h-1.5 w-12 rounded-full bg-base-content/20" />
				</div>
			</div>

			<Dialog
				opened={confirmClose}
				onBackdropClick={() => setConfirmClose(false)}
				title="Close session"
				buttons={
					<>
						<DialogButton onClick={() => setConfirmClose(false)}>Cancel</DialogButton>
						<DialogButton className="text-error" onClick={() => void runClose()}>
							Close session
						</DialogButton>
					</>
				}
			>
				Close this session? The pi agent exits; the transcript stays readable.
			</Dialog>
		</div>
	);
}

// TodoRow renders one rpiv-todo checklist row; the state glyph carries
// the status so the list reads at a glance on a phone.
// Collapsed by default: the header row is the toggle, the checklist
// appears only while open (Esteban's call - the card is noise until
// asked for).
function TodosCard({ todos }: { todos: Todo[] }) {
	const [open, setOpen] = useState(false);
	const done = todos.filter((t) => t.status === "completed").length;
	return (
		<div className="border-b border-base-300 px-3 py-2">
			<button
				type="button"
				aria-expanded={open}
				onClick={() => setOpen(!open)}
				className="flex min-h-[36px] w-full items-center gap-2 text-left"
			>
				<span className="text-[11px] tracking-widest text-muted uppercase">Todos</span>
				<span className="text-[11px] text-muted">
					· {done}/{todos.length}
				</span>
				<span className="ml-auto text-muted">
					{open ? <FaCaretDown size={12} /> : <FaCaretRight size={12} />}
				</span>
			</button>
			{open && (
				<ul className="max-h-40 space-y-1 overflow-y-auto pt-1">
					{todos.map((t) => (
						<TodoRow key={t.id} todo={t} />
					))}
				</ul>
			)}
		</div>
	);
}

function TodoRow({ todo }: { todo: Todo }) {
	const done = todo.status === "completed";
	const active = todo.status === "in_progress";
	return (
		<li
			className={`flex items-center gap-2 text-[13px] ${done ? "text-muted line-through" : active ? "text-base-content" : "text-muted"}`}
		>
			{active ? (
				<Spinner size={11} className="shrink-0 text-primary" />
			) : (
				<span className={`shrink-0 ${done ? "text-success" : "text-muted"}`}>
					{done ? <FaCheck size={11} /> : <FaCircle size={9} className="opacity-50" />}
				</span>
			)}
			<span className="min-w-0 truncate">{todo.subject}</span>
		</li>
	);
}
