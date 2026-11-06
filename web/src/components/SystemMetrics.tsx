import { useEffect, useState } from "react";
import { getSystemMetrics, type SystemMetricsSample } from "../api";

const HISTORY_LIMIT = 30;

export function formatBytes(bytes: number): string {
	const units = ["B", "KB", "MB", "GB", "TB"];
	let value = bytes;
	let unit = 0;
	while (value >= 1024 && unit < units.length - 1) {
		value /= 1024;
		unit += 1;
	}
	return `${value.toFixed(unit === 0 || value >= 100 ? 0 : 1)} ${units[unit]}`;
}

export function cpuGraph(history: number[], width = 300, height = 64) {
	if (history.length === 0) return { line: "", area: "", last: { x: 0, y: height } };
	const points = history.map((value, index) => ({
		x: history.length === 1 ? width / 2 : (index * width) / (history.length - 1),
		y: height - (Math.min(100, Math.max(0, value)) / 100) * height,
	}));
	const line = points
		.map((point, index) => `${index === 0 ? "M" : "L"}${point.x.toFixed(2)},${point.y.toFixed(2)}`)
		.join(" ");
	const first = points[0];
	const last = points[points.length - 1];
	const area = `${line} L${last.x.toFixed(2)},${height} L${first.x.toFixed(2)},${height} Z`;
	return { line, area, last };
}

function meterColor(percent: number): string {
	if (percent >= 90) return "bg-error";
	if (percent >= 75) return "bg-warning";
	return "bg-primary";
}

function meterTextColor(percent: number): string {
	if (percent >= 90) return "text-error";
	if (percent >= 75) return "text-warning";
	return "text-primary";
}

export function SystemMetricsView({
	history,
	error,
}: {
	history: SystemMetricsSample[];
	error: string;
}) {
	const current = history.at(-1);
	if (!current) {
		return (
			<section
				aria-label="System metrics"
				className="rounded-3xl border border-base-content/10 bg-base-200/90 p-4 shadow-surface-sm"
			>
				<div className="mb-4 flex items-center justify-between">
					<h2 className="text-[16px] font-bold tracking-tight">System</h2>
					<span className="text-[12px] text-muted">Every 5 s</span>
				</div>
				{error ? (
					<p className="text-[13px] text-error">
						System metrics unavailable. Retrying every 5 seconds.
					</p>
				) : (
					<div
						className="motion-safe:animate-pulse"
						role="status"
						aria-label="Loading system metrics"
					>
						<div className="h-4 w-24 rounded bg-base-300" />
						<div className="mt-3 h-16 rounded-xl bg-base-300/70" />
						<div className="mt-5 h-4 w-32 rounded bg-base-300" />
						<div className="mt-3 h-2.5 rounded-full bg-base-300" />
					</div>
				)}
			</section>
		);
	}

	const cpu = current.cpu_percent;
	const memoryPercent =
		current.memory_total_bytes > 0
			? Math.min(100, (current.memory_used_bytes / current.memory_total_bytes) * 100)
			: 0;
	const graph = cpuGraph(history.map((sample) => sample.cpu_percent));
	const cpuColor = meterTextColor(cpu);
	const memoryColor = meterColor(memoryPercent);

	return (
		<section
			aria-label="System metrics"
			className="rounded-3xl border border-base-content/10 bg-base-200/90 p-4 shadow-surface-sm"
		>
			<div className="mb-4 flex items-center justify-between">
				<h2 className="text-[16px] font-bold tracking-tight">System</h2>
				<span className="flex items-center gap-1.5 text-[12px] text-muted">
					<span className="size-1.5 rounded-full bg-success" aria-hidden="true" />
					Every 5 s
				</span>
			</div>

			<div className="flex items-center justify-between">
				<h3 className="text-[13px] font-medium">CPU</h3>
				<span className="text-[20px] font-semibold tabular-nums">{cpu.toFixed(1)}%</span>
			</div>
			<svg
				viewBox="0 0 300 64"
				className={`mt-2 h-16 w-full ${cpuColor}`}
				role="img"
				aria-label={`CPU history, current ${cpu.toFixed(1)}%`}
				preserveAspectRatio="none"
			>
				<path d={graph.area} fill="currentColor" opacity="0.12" />
				<path
					d={graph.line}
					fill="none"
					stroke="currentColor"
					strokeWidth="2"
					strokeLinejoin="round"
					strokeLinecap="round"
					vectorEffect="non-scaling-stroke"
				/>
				<circle cx={graph.last.x} cy={graph.last.y} r="2.5" fill="currentColor" />
			</svg>

			<div className="mt-4 flex items-end justify-between gap-3">
				<div>
					<h3 className="text-[13px] font-medium">Memory</h3>
					<p className="mt-0.5 text-[12px] text-muted">
						{formatBytes(current.memory_used_bytes)} / {formatBytes(current.memory_total_bytes)}
					</p>
				</div>
				<span className="text-[16px] font-semibold tabular-nums">{memoryPercent.toFixed(1)}%</span>
			</div>
			<div
				className="mt-2 h-2.5 w-full overflow-hidden rounded-full bg-base-300"
				role="progressbar"
				aria-label="Memory usage"
				aria-valuemin={0}
				aria-valuemax={100}
				aria-valuenow={Number(memoryPercent.toFixed(1))}
			>
				<div
					className={`h-full rounded-full transition-[width] duration-300 ${memoryColor}`}
					style={{ width: `${memoryPercent}%` }}
				/>
			</div>
			{error && <p className="mt-3 text-[12px] text-warning">{error}</p>}
		</section>
	);
}

export default function SystemMetrics() {
	const [history, setHistory] = useState<SystemMetricsSample[]>([]);
	const [error, setError] = useState("");

	useEffect(() => {
		let dead = false;
		let busy = false;
		const controller = new AbortController();
		const round = async () => {
			if (busy) return;
			busy = true;
			try {
				const sample = await getSystemMetrics(
					AbortSignal.any([controller.signal, AbortSignal.timeout(4000)]),
				);
				if (dead) return;
				setHistory((previous) => [...previous, sample].slice(-HISTORY_LIMIT));
				setError("");
			} catch {
				if (!dead && !controller.signal.aborted) setError("Refresh failed. Retrying in 5 seconds.");
			} finally {
				busy = false;
			}
		};
		void round();
		const timer = setInterval(() => void round(), 5000);
		return () => {
			dead = true;
			clearInterval(timer);
			controller.abort();
		};
	}, []);

	return <SystemMetricsView history={history} error={error} />;
}
