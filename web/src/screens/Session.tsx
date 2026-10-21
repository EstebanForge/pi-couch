import { Dialog, DialogButton } from "konsta/react";
import { memo, useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { Virtuoso, type VirtuosoHandle } from "react-virtuoso";
import {
	agentSession,
	closeSession,
	getItemText,
	getTranscriptPage,
	type StreamItem,
	sendAgentPrompt,
	stopSession,
	type Todo,
	type TranscriptAppend,
	type TranscriptReset,
	type TranscriptStatus,
} from "../api";
import { Composer } from "../components/Composer";
import { DialogActions } from "../components/DialogActions";
import {
	FaCaretDown,
	FaCaretRight,
	FaCheck,
	FaChevronLeft,
	FaCircle,
	FaEllipsis,
} from "../components/fa";
import { Markdown } from "../components/Markdown";
import { Spinner } from "../components/Spinner";
import { navigate, useSheetBack } from "../nav";
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
	reduceAppend,
	reduceReset,
	reduceStatus,
	removePending,
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
	| { t: "add-pending"; id: number; text: string; queued: boolean }
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
			return addPending(s, a.id, a.text, a.queued);
		case "mark-pending":
			return markPending(s, a.id, a.state);
		case "remove-pending":
			return removePending(s, a.id);
	}
}

function itemKey(b: WorkBlock): string {
	if (b.type === "status") return "b-status";
	if (b.type === "pending") return `b-pending-${b.msg.id}`;
	return b.type === "item" ? `s${b.item.seq}` : `b${b.items[0].seq}`;
}

/** Truncated tool output and bash text expand in place: the fetch
 * returns the whole thing once and it stays for the component's life. */
function FullText({ k, it }: { k: string; it: StreamItem }) {
	const [full, setFull] = useState<string | null>(null);
	const [failed, setFailed] = useState(false);
	if (full !== null) {
		return (
			<div className="max-h-72 overflow-y-auto whitespace-pre-wrap break-words text-muted">
				{full}
			</div>
		);
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

function itemRow(it: StreamItem, expanded: boolean, sessionKey: string) {
	switch (it.kind) {
		case "user":
			return (
				<div className="flex justify-end px-3 py-1">
					<div className="max-w-[85%] rounded-2xl bg-primary/20 px-3 py-2 text-[15px] whitespace-pre-wrap [overflow-wrap:anywhere]">
						{it.text}
						{!!it.images && (
							<img
								src={`/api/sessions/${encodeURIComponent(sessionKey)}/items/${it.seq}/image`}
								alt="attachment"
								loading="lazy"
								className="mt-1 max-h-64 rounded-xl"
							/>
						)}
					</div>
				</div>
			);
		case "text":
			return <Markdown text={it.text} />;
		case "marker":
			return (
				<div className="px-3 py-1 text-center text-[11px] tracking-wider text-muted uppercase">
					{it.text}
				</div>
			);
		case "thinking":
			return (
				<div className="px-3 py-0.5 text-[13px] text-muted italic whitespace-pre-wrap break-words">
					{it.text}
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
						<img
							src={`/api/sessions/${encodeURIComponent(sessionKey)}/items/${it.seq}/image`}
							alt="tool attachment"
							loading="lazy"
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

const Block = memo(function Block({ block, sessionKey }: { block: WorkBlock; sessionKey: string }) {
	const [override, setOverride] = useState<boolean | null>(null);
	if (block.type === "pending") {
		const p = block.msg;
		return (
			// Same box as the real user row: when the append frame swaps
			// this for the item the height must not change, or Virtuoso
			// re-measures and the view twitches.
			<div className="flex justify-end px-3 py-1">
				<div
					className={`bubble-in max-w-[85%] rounded-2xl bg-primary/20 px-3 py-2 text-[15px] whitespace-pre-wrap [overflow-wrap:anywhere] ${
						p.state === "unconfirmed" ? "bg-warning/30" : ""
					} ${p.state === "sending" ? "opacity-70" : ""}`}
				>
					{p.state === "sending" && <Spinner size={14} className="mr-1 text-primary" />}
					{p.state === "queued" && <span className="mr-1">⏳</span>}
					{p.text}
					{p.state === "queued" && (
						<div className="text-[10px] text-muted">queued for the end of the turn</div>
					)}
					{p.state === "unconfirmed" && (
						<div className="text-[10px] text-warning">
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
	if (block.type === "item") {
		return itemRow(block.item, true, sessionKey);
	}
	// Steps stay collapsed unless the reader opens them; the live stream
	// already carries the Working marker, so auto-expansion is noise.
	const open = override ?? false;
	const names = [...new Set(block.items.map((i) => i.tool_name || i.kind))].slice(0, 3).join(", ");
	return (
		<div className="px-3 py-0.5">
			<button
				type="button"
				className="flex min-h-9 w-full items-center gap-2 rounded-lg bg-base-200 px-2 py-2 text-left text-[12px] text-muted"
				onClick={() => setOverride(!open)}
			>
				<span>{open ? <FaCaretDown size={12} /> : <FaCaretRight size={12} />}</span>
				<span>
					{block.items.length} {block.items.length === 1 ? "step" : "steps"} · {names}
					{block.items.length > 3 ? "…" : ""}
				</span>
			</button>
			{open && (
				<div className="mt-0.5 border-l border-base-300 pl-1">
					{block.items.map((it) => (
						<div key={it.seq}>{itemRow(it, true, sessionKey)}</div>
					))}
				</div>
			)}
		</div>
	);
});

export default function SessionScreen({ sessionKey }: { sessionKey: string }) {
	// A leading "~" marks a promptless agent chat: no session file
	// exists yet, prompts go to herdr by agent name and the screen
	// swaps to the real session once pi writes the jsonl at turn start.
	const pendingAgent = sessionKey.startsWith("~") ? sessionKey.slice(1) : "";
	const [awaitUpgrade, setAwaitUpgrade] = useState(false);
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
	const [notice, setNotice] = useState("");
	const pinned = useRef(true);
	const virtuoso = useRef<VirtuosoHandle | null>(null);
	const scroller = useRef<HTMLDivElement | null>(null);
	const dragStart = useRef<{ y: number } | null>(null);
	const taBlur = useRef<HTMLTextAreaElement | null>(null);

	useEffect(() => {
		if (pendingAgent) return;
		const stream = connectStream(`/api/stream/transcript/${encodeURIComponent(sessionKey)}`, {
			onStatus: setConn,
			onEvent: (name, data) => {
				if (name === "reset") {
					// A reset replaces items with the fresh tail: history pages
					// and their index bookkeeping restart with it.
					setFirstItemIndex(INDEX_BASE);
					setMoreHistory(true);
					dispatch({ t: "reset", p: JSON.parse(data) as TranscriptReset });
				} else if (name === "append")
					dispatch({ t: "append", p: JSON.parse(data) as TranscriptAppend });
				else if (name === "status")
					dispatch({ t: "status", p: JSON.parse(data) as TranscriptStatus });
			},
		});
		return () => stream.close();
	}, [sessionKey, pendingAgent]);

	// The upgrade poll: pi writes the session jsonl at turn start, so
	// the first prompt makes the real key appear under the agent name.
	useEffect(() => {
		if (!pendingAgent || !awaitUpgrade) return;
		const iv = setInterval(() => {
			void agentSession(pendingAgent).then((key) => {
				if (key) navigate(`/sessions/${encodeURIComponent(key)}`, { replace: true });
			});
		}, 1200);
		return () => clearInterval(iv);
	}, [pendingAgent, awaitUpgrade]);

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
		pendingAgent ||
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
		(_: number, b: WorkBlock) => <Block block={b} sessionKey={sessionKey} />,
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
		if (pendingAgent) return;
		const storageKey = `couch-pi.pending.${sessionKey}`;
		let raw: string | null = null;
		try {
			raw = localStorage.getItem(storageKey);
		} catch {
			raw = null;
		}
		if (!raw) return;
		try {
			const msgs = JSON.parse(raw) as PendingMsg[];
			for (const m of msgs) {
				if (typeof m.text !== "string" || m.text === "") continue;
				dispatch({ t: "add-pending", id: nextPendingId(), text: m.text, queued: true });
			}
		} catch {
			// junk in storage: drop it
		}
	}, [sessionKey, pendingAgent]);
	useEffect(() => {
		if (pendingAgent) return;
		const storageKey = `couch-pi.pending.${sessionKey}`;
		try {
			if (state.pending.length === 0) localStorage.removeItem(storageKey);
			else localStorage.setItem(storageKey, JSON.stringify(state.pending));
		} catch {
			// storage full or blocked: the bubble still shows this session
		}
	}, [state.pending, sessionKey, pendingAgent]);
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
		if (pendingAgent) {
			setNotice("Not started yet.");
			return;
		}
		const r = await stopSession(sessionKey);
		if (!r.ok) setNotice(r.error === "notdriven" ? "No driver runs this session." : "Stop failed.");
	};

	const runClose = async () => {
		setOverflow(false);
		setConfirmClose(false);
		if (pendingAgent) {
			navigate("/", { dir: "back" });
			return;
		}
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
			<header className="flex h-11 shrink-0 items-center gap-1 border-b border-base-300 px-1">
				<button
					type="button"
					onClick={() => history.back()}
					aria-label="Back"
					className="flex size-11 items-center justify-center text-[18px] active:scale-95"
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
							className={`size-2 shrink-0 rounded-full ${dot}`}
							title={state.status || "idle"}
						/>
					)}
					<span className="truncate text-[15px]">{title}</span>
					{(state.dialog || state.awaiting) && (
						<span
							className="shrink-0 rounded-full border border-accent/60 px-2 py-0.5 text-[11px] text-accent"
							title="pi waits on a dialog - open the Terminal"
						>
							{state.dialog ? state.dialog.title || state.dialog.kind : "input"}
						</span>
					)}
				</div>
			</header>
			{/* tool strip */}
			<div
				className="flex gap-1 overflow-x-auto border-b border-base-300 px-2 py-1"
				style={{ scrollbarWidth: "none" }}
			>
				{TOOLS.filter((t) => t.placement === "strip").map((t) => (
					<button
						key={t.id}
						type="button"
						disabled={t.disabled}
						className={`min-h-[36px] shrink-0 rounded-lg px-3 text-[13px] ${
							t.disabled ? "text-muted/40" : "text-base-content active:scale-95"
						} ${t.id === "search" && query !== null ? "bg-base-300" : ""}`}
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
					className="ml-auto min-h-[36px] shrink-0 rounded-lg px-3 text-[13px] text-base-content active:scale-95"
					onClick={() => setOverflow(true)}
				>
					<FaEllipsis size={18} />
				</button>
			</div>
			{query !== null && (
				<div className="border-b border-base-300 px-3 py-1">
					<input
						value={query}
						onChange={(e) => setQuery(e.target.value)}
						placeholder="Filter the transcript…"
						className="w-full rounded-lg bg-base-200 px-3 py-2 text-[16px] outline-none"
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

			{pendingAgent && state.items.length === 0 && (
				<div className="flex flex-1 items-center justify-center px-6 text-center text-[13px] text-muted">
					pi is running here. Send the first prompt to start the transcript.
				</div>
			)}

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
					atBottomStateChange={(atBottom) => {
						pinned.current = atBottom;
					}}
					initialTopMostItemIndex={Math.max(0, blocks.length - 1)}
					className="h-full"
					itemContent={renderItem}
					components={listHeader}
				/>
				{blocks.length === 0 && !pendingAgent && (
					<div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 text-[13px] text-muted">
						{state.status === "" ? <Spinner size={22} /> : <span>Nothing here yet.</span>}
					</div>
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
				sendAgent={
					pendingAgent
						? (body) =>
								sendAgentPrompt(pendingAgent, body).then(
									() => {
										setAwaitUpgrade(true);
										return { ok: true as const };
									},
									(e: Error) => ({ ok: false as const, error: e.message }),
								)
						: undefined
				}
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

			{/* The ⋯ command panel: drops top-to-bottom under the safe area
			    and lists every tool on the bar — the strip items and the
			    overflow actions alike — so the whole bar is reachable from
			    one tap. Same scrim grammar as every panel: dim, no
			    tap-through; the scrim tap closes. */}
			<div
				aria-hidden={!overflow}
				className={`fixed inset-0 z-40 bg-black/50 transition-opacity duration-200 ease-in ${
					overflow ? "opacity-100" : "pointer-events-none opacity-0"
				}`}
				onClick={() => setOverflow(false)}
			/>
			<div
				aria-hidden={!overflow}
				className={`fixed inset-x-0 top-0 z-50 transition-transform duration-200 ease-in ${
					overflow ? "translate-y-0" : "pointer-events-none -translate-y-full"
				}`}
			>
				{/* The inset pads inside the box so the closed -100% slide
				    clears the screen edge; offsetting the wrapper by the
				    inset would leave that much panel visible under the
				    status bar. */}
				<div
					className="rounded-b-2xl border-b border-base-300 bg-base-200 shadow-2xl"
					style={{ paddingTop: "var(--safe-top, 0px)" }}
				>
					{TOOLS.map((t) => (
						<button
							key={t.id}
							type="button"
							disabled={!toolEnabled(t, state.status)}
							className={`flex min-h-[44px] w-full items-center px-5 text-[15px] active:bg-base-300 disabled:text-muted/40 ${
								t.id === "search" && query !== null ? "text-accent" : ""
							}`}
							onClick={() => {
								setOverflow(false);
								if (t.id === "search") setQuery(query === null ? "" : null);
								else if (t.id === "stop") void runStop();
								else if (t.id === "close") setConfirmClose(true);
								else if (t.href) navigate(t.href(sessionKey), { replace: true, dir: "side" });
							}}
						>
							{t.label}
						</button>
					))}
					<div className="mx-auto mb-2 h-1 w-10 rounded-full bg-base-content/20" />
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
