import { Fab, Panel } from "konsta/react";
import { Fragment, useEffect, useState } from "react";
import { createQuickAgent, getVersion, type SessionsPatch } from "../api";
import { NewAgentSheet } from "../components/NewAgentSheet";
import { NewSessionSheet } from "../components/NewSessionSheet";
import { Spinner } from "../components/Spinner";
import { short, tildify } from "../fmt";
import { navigate, useSheetBack } from "../nav";
import { quickPath } from "../prefs";
import { type PushState, pushState, togglePush } from "../push";
import { grouped, useSessions } from "../state/sessions";
import { isWaiting, STATUS_DOT } from "../status";
import { connectStream } from "../stream";

const LIST_WINDOW = 15;
const WINDOW_STEP = 5;
const SEARCH_WINDOW = 50;
const SEARCH_DEBOUNCE_MS = 300;

export default function HomeList() {
	const { sessions, total, badges, driven, awaiting, status, error, load, apply } = useSessions();
	const [menuOpen, setMenuOpen] = useState(false);
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
	const [version, setVersion] = useState<string | null>(null);

	useEffect(() => {
		getVersion()
			.then(setVersion)
			.catch(() => {});
	}, []);

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

	// The client re-filters so a stray upsert for a non-matching session
	// cannot surface mid-search.
	const ordered = grouped(sessions, badges).filter((s) => {
		if (!needle) return true;
		return (
			s.name.toLowerCase().includes(needle) ||
			s.first_user.toLowerCase().includes(needle) ||
			s.cwd.toLowerCase().includes(needle)
		);
	});
	const visible = ordered.slice(0, limit);
	// First non-active row: the Earlier divider renders before it when
	// actives exist and the boundary sits inside the visible window.
	const boundary = ordered.findIndex((s) => {
		const st = badges[s.path] ?? "unknown";
		return st !== "working" && st !== "blocked";
	});

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
					<button
						type="button"
						aria-label="Menu"
						onClick={() => setMenuOpen(true)}
						className="grid h-11 w-11 place-items-center rounded-xl text-muted"
					>
						<svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true">
							<path
								d="M3 5h14M3 10h14M3 15h14"
								stroke="currentColor"
								strokeWidth="2"
								strokeLinecap="round"
							/>
						</svg>
					</button>
					<h1 className="text-lg font-semibold">Couch Pi</h1>
					<span className="ml-auto text-sm text-muted">{status === "ok" ? `${total}` : ""}</span>
					{push !== "unsupported" && (
						<button
							type="button"
							aria-label={push === "on" ? "Notifications on" : "Notifications off"}
							title={push === "on" ? "Notifications on" : "Notifications off"}
							onClick={flipPush}
							className={`grid h-11 w-11 place-items-center rounded-xl ${push === "on" ? "text-warning" : "text-muted"}`}
						>
							<svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
								<path
									d="M12 3a6 6 0 0 0-6 6v3.2l-1.6 3.2a.7.7 0 0 0 .6 1h14a.7.7 0 0 0 .6-1L18 12.2V9a6 6 0 0 0-6-6Zm-2 15a2 2 0 0 0 4 0"
									fill="currentColor"
								/>
							</svg>
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
					<svg
						className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-muted"
						width="18"
						height="18"
						viewBox="0 0 24 24"
						aria-hidden="true"
					>
						<path
							d="M10.5 3a7.5 7.5 0 1 0 4.7 13.4l4.2 4.2 1.4-1.4-4.2-4.2A7.5 7.5 0 0 0 10.5 3Zm0 2a5.5 5.5 0 1 1 0 11 5.5 5.5 0 0 1 0-11Z"
							fill="currentColor"
						/>
					</svg>
					{query !== "" && (
						<button
							type="button"
							aria-label="Clear search"
							onClick={() => setQuery("")}
							className="absolute top-1/2 right-1 grid h-8 w-8 -translate-y-1/2 place-items-center rounded-lg text-muted"
						>
							<svg width="16" height="16" viewBox="0 0 20 20" aria-hidden="true">
								<path
									d="m5 5 10 10M15 5 5 15"
									stroke="currentColor"
									strokeWidth="2"
									strokeLinecap="round"
								/>
							</svg>
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
				{status === "ok" && sessions.length > 0 && ordered.length === 0 && (
					<p className="mt-6 px-1 text-sm text-muted">No sessions match.</p>
				)}
				<ul className="mt-2 flex flex-col gap-2">
					{visible.map((s, i) => {
						const st = badges[s.path] ?? "unknown";
						return (
							<Fragment key={s.key}>
								{i === boundary && boundary > 0 && (
									<li aria-hidden="true" className="my-1 flex items-center gap-3">
										<span className="h-px flex-1 bg-base-300" />
										<span className="text-[11px] uppercase tracking-widest text-muted">
											Earlier
										</span>
										<span className="h-px flex-1 bg-base-300" />
									</li>
								)}
								<li>
									<button
										type="button"
										onClick={() =>
											navigate(`/sessions/${encodeURIComponent(s.key)}`, {
												dir: "fwd",
											})
										}
										className="flex w-full items-center gap-3 rounded-2xl bg-base-200 px-4 py-3 text-left active:scale-[0.99]"
									>
										{isWaiting(st) && st === "working" ? (
											<Spinner
												size={13}
												className={awaiting[s.path] ? "text-accent" : "text-primary"}
											/>
										) : (
											<span
												title={awaiting[s.path] ? `${st} · waiting for input` : st}
												className={`h-2.5 w-2.5 shrink-0 rounded-full ${awaiting[s.path] ? "bg-accent" : (STATUS_DOT[st] ?? "bg-muted")}`}
											/>
										)}
										<span className="min-w-0 flex-1">
											<span className="block truncate text-[15px] font-medium">
												{short(s.name || s.first_user || s.key, 60)}
											</span>
											<span className="block truncate text-xs text-muted">
												{short(tildify(s.cwd), 48)}
											</span>
											{(driven[s.path]?.label || driven[s.path]?.title) && (
												<span className="block truncate text-[11px] text-muted/70">
													{[driven[s.path]?.label, driven[s.path]?.title]
														.filter(Boolean)
														.join(" · ")}
												</span>
											)}
										</span>
									</button>
								</li>
							</Fragment>
						);
					})}
				</ul>
				{status === "ok" && limit < total && (
					<button
						type="button"
						onClick={loadMore}
						disabled={loadingMore}
						className="mx-auto mt-3 flex items-center gap-2 rounded-xl bg-base-200 px-5 py-3 text-sm text-muted active:scale-[0.99]"
					>
						{loadingMore && <Spinner size={14} />}
						Load more ({total - limit} earlier)
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
			<div
				className="fixed right-4 z-50 flex flex-col items-end gap-2 transition-all"
				style={{
					position: "fixed",
					bottom: "calc(var(--safe-bottom, 0px) + 80px)",
					left: "auto",
					top: "auto",
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
			{/* position:fixed overlays escape root/body padding, so the FAB
			    carries its own safe-area offset (the pb-safe-all rule) */}
			<div
				className="pb-safe-all fixed right-4 bottom-4 z-50"
				style={{
					position: "fixed",
					bottom: "calc(var(--safe-bottom, 0px) + 16px)",
					left: "auto",
					top: "auto",
				}}
			>
				<Fab
					aria-label={fabOpen ? "Close menu" : "New agent"}
					icon={
						<svg width="24" height="24" viewBox="0 0 24 24" aria-hidden="true">
							<path
								d={fabOpen ? "M6 6l12 12M18 6L6 18" : "M12 5v14M5 12h14"}
								stroke="currentColor"
								strokeWidth="2"
								strokeLinecap="round"
							/>
						</svg>
					}
					onClick={() => setFabOpen(!fabOpen)}
					colors={{
						bgMaterial: "bg-primary",
						textMaterial: "text-primary-content",
						touchRipple: "touch-ripple-primary",
					}}
				/>
			</div>
			<Panel
				side="left"
				opened={menuOpen}
				onBackdropClick={() => setMenuOpen(false)}
				className="bg-base-300 text-base-content"
			>
				{/* fixed-position overlay escapes the body safe-area inset,
				    so the content carries its own top offset */}
				<div
					className="flex h-full flex-col p-4"
					style={{ paddingTop: "calc(var(--safe-top, 0px) + 1rem)" }}
				>
					<p className="px-2 pb-2 text-xs uppercase tracking-widest text-muted">Menu</p>
					<button
						type="button"
						onClick={() => {
							setMenuOpen(false);
							navigate("/settings", { dir: "fwd" });
						}}
						className="flex w-full items-center gap-3 rounded-xl px-2 py-3 text-left text-[15px] active:scale-[0.99]"
					>
						<svg width="18" height="18" viewBox="0 0 20 20" aria-hidden="true">
							<path
								d="M10 13a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm7-3a7 7 0 0 1-.1 1.2l2 1.6-2 3.4-2.4-1a7 7 0 0 1-2 1.2L12 19H8l-.5-2.6a7 7 0 0 1-2-1.2l-2.4 1-2-3.4 2-1.6A7 7 0 0 1 3 10c0-.4 0-.8.1-1.2l-2-1.6 2-3.4 2.4 1a7 7 0 0 1 2-1.2L8 1h4l.5 2.6a7 7 0 0 1 2 1.2l2.4-1 2 3.4-2 1.6c.1.4.1.8.1 1.2Z"
								fill="currentColor"
							/>
						</svg>
						Settings
					</button>
					<p className="mt-auto px-2 pt-3 text-xs text-muted">Couch Pi {version ?? "…"}</p>
				</div>
			</Panel>
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
