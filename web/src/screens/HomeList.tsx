import { Panel } from "konsta/react";
import { Fragment, useEffect, useState } from "react";
import type { SessionsSnapshot } from "../api";
import { relTime, short } from "../fmt";
import { navigate } from "../nav";
import { type PushState, pushState, togglePush } from "../push";
import { grouped, useSessions } from "../state/sessions";
import { connectStream } from "../stream";
import { Spinner } from "../components/Spinner";
import { STATUS_DOT, isWaiting } from "../status";

const LIST_WINDOW = 7;
const WINDOW_STEP = 5;

export default function HomeList() {
	const { sessions, badges, status, error, load, apply } = useSessions();
	const [menuOpen, setMenuOpen] = useState(false);
	const [size, setSize] = useState(LIST_WINDOW);
	const [loadingMore, setLoadingMore] = useState(false);

	// Grow the window one step per tap. The spinner must paint first
	// and the step lands after a deliberate 500ms beat, so the load is
	// visible instead of a blur.
	const loadMore = () => {
		if (loadingMore) return;
		setLoadingMore(true);
		requestAnimationFrame(() => {
			setTimeout(() => {
				setSize((s) => s + WINDOW_STEP);
				setLoadingMore(false);
			}, 500);
		});
	};
	const [push, setPush] = useState<PushState>("off");

	useEffect(() => {
		if (status === "idle") void load();
	}, [status, load]);

	useEffect(() => {
		void pushState().then(setPush);
	}, []);

	// Live updates: the sessions stream resets on every connect and on
	// change; the snapshot lands straight in the store.
	useEffect(() => {
		const stream = connectStream("/api/stream/sessions", {
			onEvent: (name, data) => {
				if (name === "reset") apply(JSON.parse(data) as SessionsSnapshot);
			},
		});
		return () => stream.close();
	}, [apply]);

	const ordered = grouped(sessions, badges);
	const visible = ordered.slice(0, size);
	// First non-active row: the Earlier divider renders before it when
	// actives exist and the boundary sits inside the visible window.
	const boundary = ordered.findIndex((s) => {
		const st = badges[s.path] ?? "unknown";
		return st !== "working" && st !== "blocked";
	});

	const flipPush = () => {
		void togglePush(push).then(setPush);
	};

	return (
		<div className="mx-auto flex min-h-dvh max-w-3xl flex-col" style={{ paddingBottom: "var(--safe-bottom, 0px)" }}>
			<header className="sticky top-0 z-10 flex items-center gap-3 bg-base-300/95 px-4 py-3 backdrop-blur">
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
				<span className="ml-auto text-sm text-muted">
					{status === "ok" ? `${sessions.length}` : ""}
				</span>
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
			</header>

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
											<Spinner size={13} className="text-primary" />
										) : (
											<span
												title={st}
												className={`h-2.5 w-2.5 shrink-0 rounded-full ${STATUS_DOT[st] ?? "bg-muted"}`}
											/>
										)}
										<span className="min-w-0 flex-1">
											<span className="block truncate text-[15px] font-medium">
												{short(s.name || s.first_user || s.key, 60)}
											</span>
											<span className="block truncate text-xs text-muted">
												{short(s.cwd, 48)} · {relTime(s.mtime)}
											</span>
										</span>
									</button>
								</li>
							</Fragment>
						);
					})}
				</ul>
				{status === "ok" && size < ordered.length && (
					<button
						type="button"
						onClick={loadMore}
						disabled={loadingMore}
						className="mx-auto mt-3 flex items-center gap-2 rounded-xl bg-base-200 px-5 py-3 text-sm text-muted active:scale-[0.99]"
					>
						{loadingMore && <Spinner size={14} />}
						Load more ({ordered.length - size} earlier)
					</button>
				)}
			</main>

			{/* Side menu: opens only from the home hamburger, no edge swipe,
			    so it never fights the Android Back gesture. Empty but for
			    settings until stage 8. */}
			<Panel
				side="left"
				opened={menuOpen}
				onBackdropClick={() => setMenuOpen(false)}
				className="bg-base-300 text-base-content"
			>
				{/* fixed-position overlay escapes the body safe-area inset,
				    so the content carries its own top offset */}
				<div
					className="p-4"
					style={{ paddingTop: "calc(var(--safe-top, 0px) + 1rem)" }}
				>
					<p className="px-2 pb-2 text-xs uppercase tracking-widest text-muted">Menu</p>
					<button
						type="button"
						onClick={() => setMenuOpen(false)}
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
				</div>
			</Panel>
		</div>
	);
}
