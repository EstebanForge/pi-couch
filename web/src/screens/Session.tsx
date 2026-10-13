import { Dialog, DialogButton, Navbar, NavbarBackLink } from "konsta/react";
import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import { Virtuoso } from "react-virtuoso";
import type { StreamItem } from "../api";
import {
	closeSession,
	stopSession,
	type TranscriptAppend,
	type TranscriptReset,
	type TranscriptStatus,
} from "../api";
import { Composer } from "../components/Composer";
import { Spinner } from "../components/Spinner";
import { navigate } from "../nav";
import { TOOLS, toolEnabled } from "../session-tools";
import { useSessions } from "../state/sessions";
import { STATUS_DOT, isWaiting } from "../status";
import {
	addPending,
	type DriverState,
	emptyDriver,
	groupWorkBlocks,
	markPending,
	reduceAppend,
	reduceReset,
	reduceStatus,
	removePending,
	type WorkBlock,
} from "../state/transcript";
import { connectStream, type StreamStatus } from "../stream";

const LIMIT = 400; // client-side tail cap; the server re-resets past it

let pendingCounter = 0;
const nextPendingId = () => ++pendingCounter;

type Action =
	| { t: "reset"; p: TranscriptReset }
	| { t: "append"; p: TranscriptAppend }
	| { t: "status"; p: TranscriptStatus }
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
		case "add-pending":
			return addPending(s, a.id, a.text, a.queued);
		case "mark-pending":
			return markPending(s, a.id, a.state);
		case "remove-pending":
			return removePending(s, a.id);
	}
}

function itemKey(b: WorkBlock): string {
	return b.type === "item" ? `s${b.item.seq}` : `b${b.items[0].seq}`;
}

function itemRow(it: StreamItem, expanded: boolean) {
	switch (it.kind) {
		case "user":
			return (
				<div className="flex justify-end px-3 py-1">
					<div className="max-w-[85%] rounded-2xl bg-primary/20 px-3 py-2 text-[15px] whitespace-pre-wrap">
						{it.text}
					</div>
				</div>
			);
		case "text":
			return (
				<div className="px-3 py-1 text-[15px] whitespace-pre-wrap text-base-content">{it.text}</div>
			);
		case "marker":
			return (
				<div className="px-3 py-1 text-center text-[11px] tracking-wider text-muted uppercase">
					{it.text}
				</div>
			);
		case "thinking":
			return (
				<div className="px-3 py-0.5 text-[13px] text-muted italic whitespace-pre-wrap">
					{it.text}
				</div>
			);
		case "tool_call":
		case "bash":
			return (
				<div className="flex gap-2 px-3 py-0.5 text-[13px]">
					<span className="shrink-0 rounded bg-base-300 px-1.5 font-mono text-[11px] text-accent">
						{it.tool_name || it.kind}
					</span>
					<span className="truncate text-muted">{it.text}</span>
				</div>
			);
		case "tool_result":
			return (
				<div className="px-3 py-0.5 pl-10 text-[12px] whitespace-pre-wrap text-muted">
					{expanded ? it.text : <span className="line-clamp-1">{it.text}</span>}
				</div>
			);
		default:
			return <div className="px-3 py-0.5 text-[12px] text-muted">{it.text}</div>;
	}
}

function Block({ block }: { block: WorkBlock; working?: boolean }) {
	const [override, setOverride] = useState<boolean | null>(null);
	if (block.type === "item") {
		return itemRow(block.item, true);
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
				<span className="font-mono">{open ? "▾" : "▸"}</span>
				<span>
					{block.items.length} {block.items.length === 1 ? "step" : "steps"} · {names}
					{block.items.length > 3 ? "…" : ""}
				</span>
			</button>
			{open && (
				<div className="mt-0.5 border-l border-base-300 pl-1">
					{block.items.map((it) => (
						<div key={it.seq}>{itemRow(it, true)}</div>
					))}
				</div>
			)}
		</div>
	);
}

export default function SessionScreen({ sessionKey }: { sessionKey: string }) {
	const [state, dispatch] = useReducer(driver, emptyDriver);
	const [conn, setConn] = useState<StreamStatus>("connecting");
	const [query, setQuery] = useState<string | null>(null);
	const [overflow, setOverflow] = useState(false);
	const [confirmClose, setConfirmClose] = useState(false);
	const [notice, setNotice] = useState("");
	const pinned = useRef(true);
	const scroller = useRef<HTMLDivElement | null>(null);
	const dragStart = useRef<{ y: number } | null>(null);
	const taBlur = useRef<HTMLTextAreaElement | null>(null);

	useEffect(() => {
		const stream = connectStream(`/api/stream/transcript/${encodeURIComponent(sessionKey)}`, {
			onStatus: setConn,
			onEvent: (name, data) => {
				if (name === "reset") dispatch({ t: "reset", p: JSON.parse(data) as TranscriptReset });
				else if (name === "append")
					dispatch({ t: "append", p: JSON.parse(data) as TranscriptAppend });
				else if (name === "status")
					dispatch({ t: "status", p: JSON.parse(data) as TranscriptStatus });
			},
		});
		return () => stream.close();
	}, [sessionKey]);

	const blocks = useMemo(() => {
		const q = query?.trim().toLowerCase();
		const items = q ? state.items.filter((it) => it.text.toLowerCase().includes(q)) : state.items;
		if (items.length > LIMIT)
			return groupWorkBlocks(items.slice(items.length - LIMIT));
		return groupWorkBlocks(items);
	}, [state.items, query]);

	const meta = useSessions((s) => s.sessions.find((x) => x.key === sessionKey));
	const firstUser = state.items.find((it) => it.kind === "user")?.text.slice(0, 60);
	const title =
		state.header?.name ||
		state.header?.first_user ||
		meta?.name ||
		meta?.first_user ||
		firstUser ||
		"Session";
	const dot = STATUS_DOT[state.status] ?? "bg-muted";

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

	return (
		<div className="flex h-dvh flex-col bg-base-100" style={{ paddingBottom: "var(--safe-bottom, 0px)" }}>
			<Navbar
				className="top-safe-area"
				left={<NavbarBackLink onClick={() => history.back()} />}
				title={
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
					</div>
				}
			/>
			<div className="flex items-center gap-2 border-b border-base-300 px-3 pb-1 text-[12px] text-muted">
				<span className="truncate">{state.model || "no model"}</span>
				{state.thinking && <span className="shrink-0">· thinking {state.thinking}</span>}
			</div>
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
					⋯
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
			{conn === "offline" && (
				<div className="bg-error/20 px-3 py-1 text-center text-[12px] text-error">
					Reconnecting…
				</div>
			)}
			{notice && <div className="bg-warning/20 px-3 py-1 text-center text-[12px]">{notice}</div>}

			<div
				ref={scroller}
				className="min-h-0 flex-1"
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
					data={blocks}
					computeItemKey={(_, b) => itemKey(b)}
					followOutput={(isAtBottom) => (pinned.current && isAtBottom ? "auto" : false)}
					atBottomThreshold={90}
					atBottomStateChange={(atBottom) => {
						pinned.current = atBottom;
					}}
					initialTopMostItemIndex={Math.max(0, blocks.length - 1)}
					className="h-full"
					itemContent={(_, b) => <Block block={b} />}
					components={{
						Footer: () =>
							isWaiting(state.status) ? (
								<div className="flex items-center gap-2 px-3 py-2 text-[13px] text-muted">
									<Spinner
										size={14}
										className={state.status === "blocked" ? "text-error" : "text-primary"}
									/>
									{state.status === "blocked" ? "Waiting for you" : "Working"}
								</div>
							) : null,
					}}
				/>
				{blocks.length === 0 && (
					<div className="p-6 text-center text-[13px] text-muted">Nothing here yet.</div>
				)}
			</div>

			{/* pending bubbles: reconcile with the stream; the composer
			    marks or removes them from the fetch outcome */}
			{state.pending.length > 0 && (
				<div className="space-y-1 px-3 pb-1">
					{state.pending.map((p) => (
						<div key={p.id} className="flex justify-end">
							<div
								className={`max-w-[85%] rounded-2xl px-3 py-2 text-[13px] opacity-70 ${
									p.state === "unconfirmed" ? "bg-warning/30" : "bg-primary/20"
								}`}
							>
								{p.state === "sending" && <Spinner size={14} className="mr-1 text-primary" />}
								{p.state === "queued" && <span className="mr-1">⏳</span>}
								{p.seed}
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
					))}
				</div>
			)}

			<Composer
				sessionKey={sessionKey}
				status={state.status}
				model={state.model}
				thinking={state.thinking}
				addPending={(text, queued) => {
					const id = nextPendingId();
					dispatch({ t: "add-pending", id, text, queued });
					return id;
				}}
				markPending={(id, mark) => dispatch({ t: "mark-pending", id, state: mark })}
				removePending={(id) => dispatch({ t: "remove-pending", id })}
				requestClose={() => setConfirmClose(true)}
			/>

			<Dialog
				opened={overflow}
				onBackdropClick={() => setOverflow(false)}
				buttons={<DialogButton onClick={() => setOverflow(false)}>Cancel</DialogButton>}
			>
				{TOOLS.filter((t) => t.placement === "overflow").map((t) => (
					<DialogButton
						key={t.id}
						disabled={!toolEnabled(t, state.status)}
						onClick={() => {
							if (t.id === "stop") void runStop();
							else if (t.id === "close") {
								setOverflow(false);
								setConfirmClose(true);
							}
						}}
					>
						{t.label}
					</DialogButton>
				))}
			</Dialog>
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
