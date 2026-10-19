import { Fab } from "konsta/react";
import { useEffect, useState } from "react";
import { createQuickAgent, type SessionMeta, type SessionsPatch } from "../api";
import { FaBell, FaBellSlash, FaGear, FaMagnifyingGlass, FaPlus, FaXmark } from "../components/fa";
import { NewAgentSheet } from "../components/NewAgentSheet";
import { NewSessionSheet } from "../components/NewSessionSheet";
import { Spinner } from "../components/Spinner";
import { short, tildify } from "../fmt";
import { navigate, useSheetBack } from "../nav";
import { quickPath } from "../prefs";
import { type PushState, pushState, togglePush } from "../push";
import { splitDriven, useSessions } from "../state/sessions";
import { isWaiting, STATUS_DOT } from "../status";
import { connectStream } from "../stream";

const LIST_WINDOW = 10;
const WINDOW_STEP = 5;
const SEARCH_WINDOW = 50;
const SEARCH_DEBOUNCE_MS = 300;

export default function HomeList() {
	const { sessions, total, badges, driven, awaiting, status, error, load, apply } = useSessions();
	const [query, setQuery] = useState("");
	const [limit, setLimit] = useState(LIST_WINDOW);
	const [loadingMore, setLoadingMore] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	// FAB speed dial: closed shows the +; menu fans out the three
	// actions and turns the FAB into a close button (the Calendar
	// pattern). sheet picks which bottom sheet owns the next step.
	const [fabOpen, setFabOpen] = useState(false);
	useSheetBack(fabOpen, () => setFabOpen(false));
	const [agentSheetOpen, setAgentSheetOpen] = useState(false);
	const [quickBusy, setQuickBusy] = useState(false);
	const [quickError, setQuickError] = useState("");
	// Load more refetches a bigger window from the server (the store
	// only holds what was asked for). The spinner must paint first and
	// the step lands after a deliberate 500ms beat, so the load is
	// visible instead of a blur.
	const loadMore = () => {
		if (loadingMore) return;
		setLoadingMore(true);
		const next = limit + WINDOW_STEP;
		requestAnimationFrame(() => {
			setTimeout(() => {
				setLimit(next);
				void load({ limit: next, q: needle || undefined }).finally(() => setLoadingMore(false));
			}, 500);
		});
	};
	const [push, setPush] = useState<PushState>("off");

	useEffect(() => {
		void pushState().then(setPush);
	}, []);

	// Windowed home: 15 rows at first, search asks the server (50
	// matches) so the window never blinds the search. The needle resets
	// the window; a slower earlier response cannot overwrite a newer
	// one (the store's load sequence guard).
	const needle = query.trim().toLowerCase();
	useEffect(() => {
		const next = needle ? SEARCH_WINDOW : LIST_WINDOW;
		setLimit(next);
		const t = setTimeout(
			() => void load({ limit: next, q: needle || undefined }),
			needle ? SEARCH_DEBOUNCE_MS : 0,
		);
		return () => clearTimeout(t);
	}, [needle, load]);

	// Live updates: the sessions stream resets on every connect and on
	// change; delta frames merge into the store.
	useEffect(() => {
		const stream = connectStream("/api/stream/sessions", {
			onEvent: (name, data) => {
				if (name === "reset") apply(JSON.parse(data) as SessionsPatch);
			},
		});
		return () => stream.close();
	}, [apply]);

	// Priority split: driven sessions (the herdr sidebar agents) float
	// above the recency window, once each. Search is a flat list over
	// everything, driven rows included.
	const { priority, recents } = splitDriven(sessions, driven);
	const matches = (s: SessionMeta) =>
		!needle ||
		s.name.toLowerCase().includes(needle) ||
		s.first_user.toLowerCase().includes(needle) ||
		s.cwd.toLowerCase().includes(needle);
	const visible = recents.filter(matches).slice(0, limit);
	const searchList = needle ? sessions.filter(matches) : [];

	const flipPush = () => {
		void togglePush(push).then(setPush);
	};

	// Quick agent: zero questions — a new pane+agent in the fixed
	// "Quick" workspace at the configured path; without a path the FAB
	// sends you to Settings instead. Promptless: the chat screen talks
	// to the agent by name until pi writes its session file.
	const runQuickAgent = () => {
		const path = quickPath();
		if (!path) {
			setFabOpen(false);
			navigate("/settings");
			return;
		}
		setQuickBusy(true);
		setQuickError("");
		createQuickAgent("Quick", path)
			.then(({ key, agent }) => {
				setFabOpen(false);
				navigate(
					key
						? `/sessions/${encodeURIComponent(key)}`
						: `/sessions/~${encodeURIComponent(agent ?? "")}`,
				);
			})
			.catch((e: Error) => setQuickError(e.message))
			.finally(() => setQuickBusy(false));
	};

	const card = (s: SessionMeta) => {
		const st = badges[s.path] ?? "unknown";
		return (
			<button
				type="button"
				onClick={() => navigate(`/sessions/${encodeURIComponent(s.key)}`, { dir: "fwd" })}
				className="flex w-full items-center gap-3 rounded-2xl bg-base-200 px-4 py-3 text-left active:scale-[0.99]"
			>
				{isWaiting(st) && st === "working" ? (
					<Spinner size={13} className={awaiting[s.path] ? "text-accent" : "text-primary"} />
				) : (
					<span
						title={awaiting[s.path] ? `${st} · waiting for input` : st}
						className={`h-2.5 w-2.5 shrink-0 rounded-full ${awaiting[s.path] ? "bg-accent" : (STATUS_DOT[st] ?? "bg-muted")}`}
					/>
				)}
				<span className="min-w-0 flex-1">
					<span className="block truncate text-[15px] font-medium">
						{short(s.name || s.cwd.split("/").filter(Boolean).pop() || s.key, 60)}
					</span>
					<span className="block truncate text-xs text-muted">{short(tildify(s.cwd), 48)}</span>
					{(driven[s.path]?.label || driven[s.path]?.title) && (
						<span className="block truncate text-[11px] text-muted/70">
							{[driven[s.path]?.label, driven[s.path]?.title].filter(Boolean).join(" · ")}
						</span>
					)}
				</span>
			</button>
		);
	};

	const showPriority = !needle && priority.length > 0;

	return (
		<div
			className="mx-auto flex min-h-dvh max-w-3xl flex-col"
			style={{ paddingBottom: "var(--safe-bottom, 0px)" }}
		>
			<header
				className="fixed inset-x-0 top-0 z-30 bg-base-300/95 backdrop-blur"
				style={{ paddingTop: "var(--safe-top, 0px)" }}
			>
				<div className="mx-auto flex max-w-3xl items-center gap-3 px-4 py-3">
					{/* The Pi Agent mark, the same three paths as the Android
					    launcher icon (800x800 box). */}
					<svg width="22" height="22" viewBox="0 0 800 800" aria-hidden="true">
						<path d="M165.29,165.29H517.36V400H400V282.65H165.29Z" fill="#F09082" />
						<path d="M165.29,282.65H282.65V400H400V517.36H282.65V634.72H165.29Z" fill="#4D9ABF" />
						<path d="M517.36,400H634.72V634.72H517.36Z" fill="#F1BE58" />
					</svg>
					<h1 className="text-lg font-semibold">Couch Pi</h1>
					<button
						type="button"
						aria-label="Settings"
						onClick={() => navigate("/settings", { dir: "fwd" })}
						className="ml-auto grid h-11 w-11 place-items-center rounded-xl text-muted"
					>
						<FaGear size={20} />
					</button>
					{push !== "unsupported" && (
						<button
							type="button"
							aria-label={push === "on" ? "Notifications on" : "Notifications off"}
							title={push === "on" ? "Notifications on" : "Notifications off"}
							onClick={flipPush}
							className={`grid h-11 w-11 place-items-center rounded-xl ${push === "on" ? "text-warning" : "text-muted"}`}
						>
							{push === "on" ? <FaBell size={20} /> : <FaBellSlash size={20} />}
						</button>
					)}
				</div>
			</header>

			{/* 68px fixed bar + the old pt-3 gap; body still pads the status bar */}
			<div className="px-3 pt-[80px]">
				<div className="relative">
					<input
						type="text"
						value={query}
						onChange={(e) => setQuery(e.target.value)}
						placeholder="Search sessions…"
						autoComplete="off"
						className="w-full rounded-xl border border-muted/40 bg-transparent py-3 pl-10 pr-10 text-[16px] text-base-content placeholder:text-muted focus:border-primary/60 focus:outline-none"
					/>
					<FaMagnifyingGlass
						size={18}
						className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
					/>
					{query !== "" && (
						<button
							type="button"
							aria-label="Clear search"
							onClick={() => setQuery("")}
							className="absolute top-1/2 right-1 grid h-8 w-8 -translate-y-1/2 place-items-center rounded-lg text-muted"
						>
							<FaXmark size={16} />
						</button>
					)}
				</div>
			</div>

			<main className="flex-1 px-3 pb-24">
				{status === "error" && (
					<p className="mt-6 rounded-xl bg-base-200 p-4 text-sm text-error">{error}</p>
				)}
				{status === "loading" && sessions.length === 0 && (
					<div className="flex h-[60dvh] flex-col items-center justify-center gap-3 text-sm text-muted">
						<Spinner size={24} className="text-primary" />
						Loading sessions…
					</div>
				)}
				{status === "ok" && sessions.length === 0 && (
					<p className="mt-6 px-1 text-sm text-muted">No sessions.</p>
				)}
				{status === "ok" &&
					sessions.length > 0 &&
					!needle &&
					priority.length === 0 &&
					recents.length === 0 && (
						<p className="mt-6 px-1 text-sm text-muted">No sessions match.</p>
					)}
				{status === "ok" && needle && searchList.length === 0 && sessions.length > 0 && (
					<p className="mt-6 px-1 text-sm text-muted">No sessions match.</p>
				)}
				{showPriority && (
					<section aria-label="priority" className="mt-2">
						<div className="px-1 pb-2 text-[11px] uppercase tracking-widest text-muted">
							priority
						</div>
						<ul className="flex flex-col gap-2">
							{priority.map((s) => (
								<li key={s.key}>{card(s)}</li>
							))}
						</ul>
						<div aria-hidden="true" className="my-4 flex items-center gap-3">
							<span className="h-px flex-1 bg-base-300" />
							<span className="text-[11px] uppercase tracking-widest text-muted">earlier</span>
							<span className="h-px flex-1 bg-base-300" />
						</div>
					</section>
				)}
				<ul className={showPriority ? "flex flex-col gap-2" : "mt-2 flex flex-col gap-2"}>
					{(needle ? searchList : visible).map((s) => (
						<li key={s.key}>{card(s)}</li>
					))}
				</ul>
				{status === "ok" &&
					(needle ? searchList.length > 0 : visible.length > 0) &&
					limit < total && (
						<button
							type="button"
							onClick={loadMore}
							disabled={loadingMore}
							className="mx-auto mt-3 flex items-center gap-2 rounded-xl bg-base-200 px-5 py-3 text-sm text-muted active:scale-[0.99]"
						>
							{loadingMore && <Spinner size={14} />}
							Load more{needle ? ` (${total - limit} earlier)` : ""}
						</button>
					)}
			</main>

			{/* Side menu: opens only from the home hamburger, no edge swipe,
			    so it never fights the Android Back gesture. Empty but for
			    settings until stage 8. */}
			<NewSessionSheet opened={createOpen} onClose={() => setCreateOpen(false)} />
			<NewAgentSheet opened={agentSheetOpen} onClose={() => setAgentSheetOpen(false)} />
			{/* Speed dial: scrim + pill stack above the FAB, the Calendar
			    pattern. Konsta ships a single Fab and a bottom-sheet
			    Actions but no speed dial, so this is hand-built. */}
			{fabOpen && (
				<button
					type="button"
					aria-label="Close menu"
					onClick={() => setFabOpen(false)}
					className="fixed inset-0 z-40 bg-black/50"
				/>
			)}
			{/* position:fixed overlays escape root/body padding, so the FAB
			    carries its own safe-area offset via pb-safe-all. The pill
			    stack anchors inside the same wrapper (bottom-full), so it
			    sits just above the FAB at any inset and any FAB size. */}
			<div className="pb-safe-all fixed right-4 bottom-4 z-50">
				<div
					className="absolute bottom-full right-0 mb-2 flex w-max flex-col items-end gap-2 transition-all"
					style={{
						opacity: fabOpen ? 1 : 0,
						transform: fabOpen ? "none" : "translateY(8px)",
						pointerEvents: fabOpen ? "auto" : "none",
					}}
				>
					{quickError && (
						<div className="max-w-64 rounded-lg bg-error-content/10 px-3 py-1.5 text-[12px] text-error">
							{quickError}
						</div>
					)}
					<FabAction
						label="New workspace"
						onClick={() => {
							setFabOpen(false);
							setCreateOpen(true);
						}}
					/>
					<FabAction
						label="New agent"
						onClick={() => {
							setFabOpen(false);
							setAgentSheetOpen(true);
						}}
					/>
					<FabAction label="Quick agent" busy={quickBusy} onClick={runQuickAgent} />
				</div>
				<Fab
					aria-label={fabOpen ? "Close menu" : "New agent"}
					icon={fabOpen ? <FaXmark size={24} /> : <FaPlus size={24} />}
					onClick={() => setFabOpen(!fabOpen)}
					colors={{
						bgMaterial: "bg-primary",
						textMaterial: "text-primary-content",
						touchRipple: "touch-ripple-primary",
					}}
				/>
			</div>
		</div>
	);
}

// One pill of the FAB speed dial; the roomy padding is the touch
// target — thumbs on the phone, not the design grid.
function FabAction({
	label,
	onClick,
	busy,
}: {
	label: string;
	onClick: () => void;
	busy?: boolean;
}) {
	return (
		<button
			type="button"
			disabled={busy}
			onClick={onClick}
			className="flex min-h-[52px] items-center gap-2 rounded-full bg-base-200 px-6 text-[15px] font-medium text-base-content shadow-lg active:scale-95 disabled:opacity-60"
		>
			{busy && <Spinner size={14} />}
			{label}
		</button>
	);
}
