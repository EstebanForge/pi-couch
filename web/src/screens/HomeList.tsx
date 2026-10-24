import { Fab } from "konsta/react";
import { useEffect, useRef, useState } from "react";
import { getHome } from "../api";
import type { SessionMeta, SessionsPatch } from "../api";
import {
	FaBell,
	FaBellSlash,
	FaGauge,
	FaGear,
	FaMagnifyingGlass,
	FaPlus,
	FaXmark,
} from "../components/fa";
import { NewAgentSheet } from "../components/NewAgentSheet";
import { NewSessionSheet } from "../components/NewSessionSheet";
import { QuickSheet } from "../components/QuickSheet";
import { Spinner } from "../components/Spinner";
import { useTouchRipple } from "../components/useRipple";
import { short, thousand, tildify } from "../fmt";
import { navigate, useSheetBack } from "../nav";
import { quickPath } from "../prefs";
import { type PushState, pushState, togglePush } from "../push";
import { splitDriven, useSessions } from "../state/sessions";
import { isWaiting, STATUS_DOT } from "../status";
import { connectStream } from "../stream";

const LIST_WINDOW = 10;
const WINDOW_STEP = 10;
const SEARCH_WINDOW = 50;
const SEARCH_DEBOUNCE_MS = 300;

export default function HomeList() {
	const { sessions, total, badges, driven, awaiting, status, error, load, apply } = useSessions();
	const [query, setQuery] = useState("");
	const [home, setHome] = useState("");
	const [limit, setLimit] = useState(LIST_WINDOW);
	const [loadingMore, setLoadingMore] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	// FAB speed dial: closed shows the +; menu fans out the three
	// actions and turns the FAB into a close button (the Calendar
	// pattern). sheet picks which bottom sheet owns the next step.
	const [fabOpen, setFabOpen] = useState(false);
	useSheetBack(fabOpen, () => setFabOpen(false));
	const [agentSheetOpen, setAgentSheetOpen] = useState(false);
	const [quickSheet, setQuickSheet] = useState(false);
	// Load more refetches a bigger window from the server (the store
	// only holds what was asked for). The spinner must paint first and
	// the step lands after a deliberate 500ms beat, so the load is
	// visible instead of a blur.
	// The recents ul: after a browse-mode Load more, the first newly
	// loaded card is scrolled into view — new rows insert above the
	// button, and scroll anchoring otherwise makes the load invisible.
	const recentsUl = useRef<HTMLUListElement>(null);

	// Plain setTimeout only: chaining the beat inside
	// requestAnimationFrame wedges the whole flow when the WebView stops
	// producing frames (doze/background) — loadingMore then stays true
	// and every later tap early-returns with no fetch (field bug).
	const loadMore = () => {
		if (loadingMore) return;
		setLoadingMore(true);
		// Snapshot the count of rendered recents, not the raw fetch limit:
		// driven/priority sessions sit in their own section above recentsUl,
		// so recentsUl.children starts at index 0 for recents, and scrolling
		// to visibleRecentsCount lands directly on the first newly added card.
		const prevCount = needle ? searchList.length : visible.length;
		const next = limit + WINDOW_STEP;
		setTimeout(() => {
			setLimit(next);
			void load({ limit: next, q: needle || undefined })
				.then(() => {
					if (needle) return;
					requestAnimationFrame(() => {
						recentsUl.current?.children[prevCount]?.scrollIntoView({
							behavior: "smooth",
							block: "start",
						});
					});
				})
				.finally(() => setLoadingMore(false));
		}, 300);
	};
	const [push, setPush] = useState<PushState>("off");

	useEffect(() => {
		void pushState().then(setPush);
	}, []);

	useEffect(() => {
		void getHome().then(setHome);
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

	// Quick agent: the create panel collects the first prompt,
	// attachments and an optional model pin; without a configured path
	// the pill sends you to Settings instead.
	const runQuickAgent = () => {
		if (!quickPath()) {
			setFabOpen(false);
			navigate("/settings");
			return;
		}
		setFabOpen(false);
		setQuickSheet(true);
	};

	const card = (s: SessionMeta, index = 0) => {
		const st = badges[s.path] ?? "unknown";
		return (
			<SessionCard
				s={s}
				st={st}
				isAwaiting={!!awaiting[s.path]}
				drivenMeta={driven[s.path]}
				index={index}
				home={home}
			/>
		);
	};

	const showPriority = !needle && priority.length > 0;

	return (
		<div
			className="mx-auto flex min-h-dvh max-w-3xl flex-col"
			style={{ paddingBottom: "var(--safe-bottom, 0px)" }}
		>
			<header
				className="header-glass fixed inset-x-0 top-0 z-30 border-b border-base-content/8 shadow-surface-sm"
				style={{ paddingTop: "var(--safe-top, 0px)" }}
			>
				<div className="mx-auto flex max-w-3xl items-center gap-3 px-4 py-3">
					{/* The Pi Agent mark, the same three paths as the Android
					    launcher icon (800x800 box). */}
					<svg
						width="31"
						height="31"
						viewBox="0 0 800 800"
						aria-hidden="true"
						className="drop-shadow-sm"
					>
						<path d="M165.29,165.29H517.36V400H400V282.65H165.29Z" fill="#F09082" />
						<path d="M165.29,282.65H282.65V400H400V517.36H282.65V634.72H165.29Z" fill="#4D9ABF" />
						<path d="M517.36,400H634.72V634.72H517.36Z" fill="#F1BE58" />
					</svg>
					<h1 className="text-[19px] font-semibold tracking-tight text-base-content">Couch</h1>
					<button
						type="button"
						aria-label="Provider Quotas"
						onClick={() => navigate("/quotas", { dir: "fwd" })}
						className="press-scale ml-auto grid h-11 w-11 place-items-center rounded-xl text-muted active:text-base-content"
					>
						<FaGauge size={20} />
					</button>
					<button
						type="button"
						aria-label="Settings"
						onClick={() => navigate("/settings", { dir: "fwd" })}
						className="press-scale grid h-11 w-11 place-items-center rounded-xl text-muted active:text-base-content"
					>
						<FaGear size={20} />
					</button>
					{push !== "unsupported" && (
						<button
							type="button"
							aria-label={push === "on" ? "Notifications on" : "Notifications off"}
							title={push === "on" ? "Notifications on" : "Notifications off"}
							onClick={flipPush}
							className={`press-scale grid h-11 w-11 place-items-center rounded-xl transition-colors ${push === "on" ? "text-warning" : "text-muted active:text-base-content"}`}
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
						className="w-full rounded-2xl border border-base-content/10 bg-base-200/60 py-3.5 pl-11 pr-10 text-[16px] text-base-content placeholder:text-muted/70 shadow-inner backdrop-blur-sm transition-all focus:border-primary/70 focus:bg-base-200/90 focus:shadow-[0_0_0_3px_rgba(64,157,212,0.18)] focus:outline-none"
					/>
					<FaMagnifyingGlass
						size={17}
						className="pointer-events-none absolute top-1/2 left-3.5 -translate-y-1/2 text-muted"
					/>
					{query !== "" && (
						<button
							type="button"
							aria-label="Clear search"
							onClick={() => setQuery("")}
							className="press-scale absolute top-1/2 right-1.5 grid h-8 w-8 -translate-y-1/2 place-items-center rounded-lg text-muted active:text-base-content"
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
					<section aria-label="priority" className="mt-3">
						<div className="flex items-center gap-2 px-1 pb-2">
							<span className="text-[11px] font-semibold tracking-wider text-primary uppercase">
								Priority
							</span>
							<span className="h-px flex-1 bg-base-content/10" />
						</div>
						<ul className="flex flex-col gap-2.5">
							{priority.map((s, i) => (
								<li key={s.key}>{card(s, i)}</li>
							))}
						</ul>
						<div aria-hidden="true" className="my-4 flex items-center gap-3">
							<span className="h-px flex-1 bg-base-content/10" />
							<span className="text-[11px] font-semibold tracking-wider text-muted uppercase">
								earlier
							</span>
							<span className="h-px flex-1 bg-base-content/10" />
						</div>
					</section>
				)}
				<ul
					ref={recentsUl}
					className={showPriority ? "flex flex-col gap-2.5" : "mt-3 flex flex-col gap-2.5"}
				>
					{(needle ? searchList : visible).map((s, i) => (
						<li key={s.key}>{card(s, showPriority ? priority.length + i : i)}</li>
					))}
				</ul>
				{status === "ok" &&
					(needle ? searchList.length > 0 : visible.length > 0) &&
					limit < total && (
						<button
							type="button"
							onClick={loadMore}
							disabled={loadingMore}
							className="press-scale mx-auto mt-4 flex items-center gap-2 rounded-2xl border border-base-content/10 bg-base-200/80 px-6 py-3 text-[13.5px] font-medium text-muted shadow-surface-sm backdrop-blur-sm active:text-base-content"
						>
							{loadingMore && <Spinner size={14} className="text-primary" />}
							Load more{needle ? ` (${thousand(total - limit)} earlier)` : ""}
						</button>
					)}
			</main>

			{/* Side menu: opens only from the home hamburger, no edge swipe,
			    so it never fights the Android Back gesture. Empty but for
			    settings until stage 8. */}
			<NewSessionSheet opened={createOpen} onClose={() => setCreateOpen(false)} />
			<NewAgentSheet opened={agentSheetOpen} onClose={() => setAgentSheetOpen(false)} />
			<QuickSheet opened={quickSheet} onClose={() => setQuickSheet(false)} />
			{/* Speed dial: scrim + pill stack above the FAB, the Calendar
			    pattern. Konsta ships a single Fab and a bottom-sheet
			    Actions but no speed dial, so this is hand-built. */}
			{fabOpen && (
				<button
					type="button"
					aria-label="Close menu"
					onClick={() => setFabOpen(false)}
					className="fixed inset-0 z-40 bg-black/60 backdrop-blur-xs transition-opacity duration-200"
				/>
			)}
			{/* position:fixed overlays escape root/body padding, so the FAB
			    carries its own safe-area offset via pb-safe-all. The pill
			    stack anchors inside the same wrapper (bottom-full), so it
			    sits just above the FAB at any inset and any FAB size. */}
			<div
				className={`pb-safe-all fixed right-4 bottom-4 z-50 transition-opacity ${
					createOpen || quickSheet || agentSheetOpen
						? "opacity-0 pointer-events-none"
						: "opacity-100"
				}`}
			>
				<div
					className="absolute bottom-full right-0 mb-3 flex w-max flex-col items-end gap-2.5 transition-all duration-200 ease-[cubic-bezier(0.05,0.7,0.1,1)]"
					style={{
						opacity: fabOpen ? 1 : 0,
						transform: fabOpen ? "none" : "translateY(12px) scale(0.95)",
						pointerEvents: fabOpen ? "auto" : "none",
					}}
				>
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
					<FabAction label="Quick agent" onClick={runQuickAgent} />
				</div>
				<Fab
					aria-label={fabOpen ? "Close menu" : "New agent"}
					icon={fabOpen ? <FaXmark size={24} /> : <FaPlus size={24} />}
					onClick={() => setFabOpen(!fabOpen)}
					className="shadow-[0_4px_20px_rgba(64,157,212,0.45)] transition-transform active:scale-95"
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
	const ref = useRef<HTMLButtonElement>(null);
	useTouchRipple(ref, !busy);

	return (
		<button
			ref={ref}
			type="button"
			disabled={busy}
			onClick={onClick}
			className="press-scale relative overflow-hidden flex min-h-[52px] items-center gap-2 rounded-full border border-base-content/10 bg-base-200 px-6 text-[15px] font-medium text-base-content shadow-surface-lg disabled:opacity-60"
		>
			{busy && <Spinner size={14} />}
			{label}
		</button>
	);
}

function SessionCard({
	s,
	st,
	isAwaiting,
	drivenMeta,
	index = 0,
	home,
}: {
	s: SessionMeta;
	st: string;
	isAwaiting: boolean;
	drivenMeta?: { label?: string; title?: string };
	index?: number;
	home: string;
}) {
	const ref = useRef<HTMLButtonElement>(null);
	useTouchRipple(ref);

	// Cap staggering to top rows so fast scrolls or long windows don't lag
	const delay = Math.min(index * 25, 250);

	return (
		<button
			ref={ref}
			type="button"
			style={{ animationDelay: `${delay}ms` }}
			onClick={() => navigate(`/sessions/${encodeURIComponent(s.key)}`, { dir: "fwd" })}
			className="card-in press-scale relative overflow-hidden flex w-full items-center gap-3.5 rounded-2xl border border-base-content/8 bg-base-200/90 px-4 py-3.5 text-left shadow-surface-sm backdrop-blur-sm"
		>
			{isWaiting(st) && st === "working" ? (
				<div className="grid h-6 w-6 shrink-0 place-items-center">
					<Spinner size={14} className={isAwaiting ? "text-accent" : "text-primary"} />
				</div>
			) : (
				<div className="grid h-6 w-6 shrink-0 place-items-center">
					<span
						title={isAwaiting ? `${st} · waiting for input` : st}
						className={`h-2.5 w-2.5 rounded-full transition-all duration-300 ${isAwaiting ? "bg-accent shadow-[0_0_8px_rgba(55,181,180,0.6)]" : (STATUS_DOT[st] ?? "bg-muted")}`}
					/>
				</div>
			)}
			<span className="min-w-0 flex-1">
				<span className="block truncate text-[15.5px] font-medium tracking-tight text-base-content">
					{short(s.name || s.cwd.split("/").filter(Boolean).pop() || s.key, 60)}
				</span>
				<span className="block truncate text-[12.5px] text-muted">{short(tildify(s.cwd, home), 48)}</span>
				{(drivenMeta?.label || drivenMeta?.title) && (
					<span className="mt-0.5 block truncate text-[11px] font-mono text-muted/75">
						{[drivenMeta.label, drivenMeta.title].filter(Boolean).join(" · ")}
					</span>
				)}
			</span>
		</button>
	);
}
