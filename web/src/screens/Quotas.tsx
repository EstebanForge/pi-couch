import { useCallback, useEffect, useRef, useState } from "react";
import { getQuotas, type ProviderQuota, type QuotasSnapshot, type WindowQuota } from "../api";
import { FaChevronLeft, FaGaugeHigh } from "../components/fa";
import { Spinner } from "../components/Spinner";
import { relTime } from "../fmt";
import { prefKey, readPref, writePref } from "../prefs";

const QUOTAS_STORE = prefKey("quotas");

type Stored = { providers: ProviderQuota[]; updated_at: string };

function loadStored(): Stored | null {
	try {
		const raw = readPref(QUOTAS_STORE);
		return raw ? (JSON.parse(raw) as Stored) : null;
	} catch {
		return null;
	}
}

export default function QuotasScreen() {
	const stored = useRef(loadStored());
	const [quotas, setQuotas] = useState<ProviderQuota[]>(stored.current?.providers ?? []);
	const [loading, setLoading] = useState(!stored.current);
	const [refreshing, setRefreshing] = useState(false);
	const [updatedAt, setUpdatedAt] = useState<string>(stored.current?.updated_at ?? "");
	const [error, setError] = useState("");
	const syncRef = useRef<() => void>(() => {});

	const apply = useCallback((snap: QuotasSnapshot) => {
		setQuotas(snap.providers);
		setUpdatedAt(snap.updated_at);
		setLoading(false);
		writePref(
			QUOTAS_STORE,
			JSON.stringify({ providers: snap.providers, updated_at: snap.updated_at }),
		);
	}, []);

	// One fetch round per visit: the server answers the cache instantly
	// when fresh and serves a stale snapshot with refreshing set while
	// it revalidates, so the poll only continues while an update is
	// truly in flight. Providers are capped at one round per 5 minutes
	// server-side; this screen just shows that happening.
	useEffect(() => {
		let dead = false;
		let timer: ReturnType<typeof setInterval> | null = null;
		const stop = () => {
			if (timer) clearInterval(timer);
			timer = null;
			setRefreshing(false);
		};
		const round = (first: boolean) => {
			if (first) setRefreshing(true);
			getQuotas()
				.then((snap) => {
					if (dead) return;
					apply(snap);
					if (snap.refreshing && !timer) {
						const started = Date.now();
						timer = setInterval(() => {
							if (Date.now() - started > 45_000) {
								stop();
								return;
							}
							void getQuotas()
								.then((next) => {
									if (dead) return;
									apply(next);
									if (!next.refreshing) stop();
								})
								.catch(() => {});
						}, 3000);
					}
				})
				.catch((e: Error) => {
					if (!dead) setError(e.message);
					stop();
				});
		};
		syncRef.current = () => {
			if (!timer) round(true);
		};
		round(true);
		return () => {
			dead = true;
			stop();
		};
	}, [apply]);

	return (
		<div
			className="mx-auto flex min-h-dvh max-w-3xl flex-col bg-base-100 text-base-content"
			style={{ paddingBottom: "var(--safe-bottom, 0px)" }}
		>
			<header
				className="header-glass fixed inset-x-0 top-0 z-30 border-b border-base-content/8 shadow-surface-sm"
				style={{ paddingTop: "var(--safe-top, 0px)" }}
			>
				<div className="mx-auto flex max-w-3xl items-center gap-3 px-3 py-2.5">
					<button
						type="button"
						onClick={() => history.back()}
						aria-label="Back"
						className="press-scale flex size-11 items-center justify-center rounded-xl text-muted active:text-base-content"
					>
						<FaChevronLeft size={20} />
					</button>
					<div className="flex items-center gap-2">
						<FaGaugeHigh size={19} className="text-primary" />
						<h1 className="text-[18px] font-semibold tracking-tight text-base-content">
							Provider Quotas
						</h1>
					</div>
					<button
						type="button"
						onClick={() => syncRef.current()}
						disabled={refreshing || loading}
						className="press-scale ml-auto flex min-h-[38px] items-center gap-1.5 rounded-xl border border-base-content/10 bg-base-300/60 px-3.5 text-[13px] font-medium text-base-content shadow-surface-sm active:bg-base-300 disabled:opacity-40"
					>
						{refreshing ? <Spinner size={13} className="text-primary" /> : "Refresh"}
					</button>
				</div>
			</header>

			<main className="flex-1 px-4 pt-[74px] pb-12">
				{updatedAt && (
					<div className="mb-4 flex items-center justify-between px-1 text-[12px] text-muted">
						{refreshing && quotas.length > 0 ? (
							<span className="flex items-center gap-1.5">
								<Spinner size={11} className="text-primary" />
								Updating from providers…
							</span>
						) : (
							<span>Synced from local environment & credentials</span>
						)}
						<span>{relTime(updatedAt)}</span>
					</div>
				)}

				{error && (
					<div className="mb-4 rounded-2xl border border-error/30 bg-error/10 p-3.5 text-[13px] text-error">
						{error}
					</div>
				)}

				{loading && quotas.length === 0 ? (
					<div className="flex h-[50dvh] flex-col items-center justify-center gap-3 text-muted">
						<Spinner size={24} className="text-primary" />
						<span className="text-[14px]">Querying provider balances…</span>
					</div>
				) : (
					<div className="flex flex-col gap-3.5">
						{quotas.map((p) => (
							<ProviderCard key={p.id} provider={p} />
						))}
					</div>
				)}
			</main>
		</div>
	);
}

function ProviderCard({ provider }: { provider: ProviderQuota }) {
	const hasWindows = provider.windows && provider.windows.length > 0;
	const isError = Boolean(provider.error);

	return (
		<div className="rounded-3xl border border-base-content/10 bg-base-200/90 p-4 shadow-surface-sm backdrop-blur-sm">
			<div className="flex items-center justify-between pb-3">
				<div className="flex items-center gap-2">
					<span className="text-[16px] font-bold text-base-content tracking-tight">
						{provider.name}
					</span>
					{provider.plan && (
						<span className="rounded-full border border-primary/30 bg-primary/10 px-2 py-0.5 text-[11px] font-semibold text-primary">
							{provider.plan}
						</span>
					)}
				</div>
				{isError && <span className="text-[12px] font-medium text-muted/80">{provider.error}</span>}
			</div>

			{hasWindows && (
				<div className="flex flex-col gap-3 border-t border-base-content/6 pt-3">
					{provider.windows?.map((w) => (
						<WindowRow key={w.label} window={w} />
					))}
				</div>
			)}
		</div>
	);
}

function WindowRow({ window }: { window: WindowQuota }) {
	const used = Math.min(Math.max(window.percent, 0), 100);
	// The whole row speaks "remaining": bar and number both start at
	// 100 and drain as the window is consumed; color thresholds still
	// key on usage.
	const left = 100 - used;
	const isHigh = used >= 80;
	const isCritical = used >= 95;

	const barColor = isCritical ? "bg-error" : isHigh ? "bg-warning" : "bg-primary";
	const resetFormatted = window.resets_at ? relTime(window.resets_at) : "";

	return (
		<div className="flex flex-col gap-1.5">
			<div className="flex items-center justify-between text-[13px]">
				<span className="font-medium text-base-content">{window.label}</span>
				<div className="flex items-center gap-2">
					{window.remaining && (
						<span className="font-mono text-[11.5px] text-muted">{window.remaining}</span>
					)}
					<span
						className={`font-semibold tabular-nums ${isCritical ? "text-error" : isHigh ? "text-warning" : "text-base-content"}`}
					>
						{left.toFixed(0)}%
					</span>
				</div>
			</div>

			{/* Progress track: the bar shows what is LEFT, starting full
				and depleting as usage climbs; color still tracks usage. */}
			<div className="h-2 w-full overflow-hidden rounded-full bg-base-300">
				<div
					className={`h-full rounded-full transition-all duration-500 ${barColor}`}
					style={{ width: `${left}%` }}
				/>
			</div>

			{resetFormatted && (
				<div className="text-right text-[11px] text-muted">Resets {resetFormatted}</div>
			)}
		</div>
	);
}
