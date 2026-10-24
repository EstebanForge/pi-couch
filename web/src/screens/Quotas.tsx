import { useCallback, useEffect, useState } from "react";
import { getQuotas, type ProviderQuota, refreshQuotas, type WindowQuota } from "../api";
import { FaChevronLeft, FaGauge } from "../components/fa";
import { Spinner } from "../components/Spinner";
import { relTime } from "../fmt";

export default function QuotasScreen() {
	const [quotas, setQuotas] = useState<ProviderQuota[]>([]);
	const [loading, setLoading] = useState(true);
	const [refreshing, setRefreshing] = useState(false);
	const [updatedAt, setUpdatedAt] = useState<string>("");
	const [error, setError] = useState("");

	const load = useCallback(() => {
		setLoading(true);
		setError("");
		getQuotas()
			.then((snap) => {
				setQuotas(snap.providers);
				setUpdatedAt(snap.updated_at);
			})
			.catch((e: Error) => setError(e.message))
			.finally(() => setLoading(false));
	}, []);

	const refresh = () => {
		if (refreshing) return;
		setRefreshing(true);
		refreshQuotas()
			.then((snap) => {
				setQuotas(snap.providers);
				setUpdatedAt(snap.updated_at);
			})
			.catch((e: Error) => setError(e.message))
			.finally(() => setRefreshing(false));
	};

	useEffect(() => {
		load();
	}, [load]);

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
						<FaGauge size={19} className="text-primary" />
						<h1 className="text-[18px] font-semibold tracking-tight text-base-content">
							Provider Quotas
						</h1>
					</div>
					<button
						type="button"
						onClick={refresh}
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
						<span>Synced from local environment & credentials</span>
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
	const pct = Math.min(Math.max(window.percent, 0), 100);
	const isHigh = pct >= 80;
	const isCritical = pct >= 95;

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
						{pct.toFixed(0)}%
					</span>
				</div>
			</div>

			{/* Progress track */}
			<div className="h-2 w-full overflow-hidden rounded-full bg-base-300">
				<div
					className={`h-full rounded-full transition-all duration-500 ${barColor}`}
					style={{ width: `${pct}%` }}
				/>
			</div>

			{resetFormatted && (
				<div className="text-right text-[11px] text-muted">Resets {resetFormatted}</div>
			)}
		</div>
	);
}
