import { useCallback, useEffect, useState } from "react";
import { type DirsResponse, getDirs } from "../api";
import { FaTurnUp } from "./fa";

// Shared jailed folder browser over /api/dirs: home-rooted on the
// machine running herdr, hidden folders behind a toggle, client-side
// filter. Reports the currently browsed path upward; the caller owns
// what "use this folder" means.

export function DirBrowser({
	onPathChange,
	disabled,
}: {
	onPathChange: (path: string) => void;
	disabled?: boolean;
}) {
	const [browse, setBrowse] = useState<DirsResponse | null>(null);
	const [filter, setFilter] = useState("");
	const [showHidden, setShowHidden] = useState(false);

	const load = useCallback(
		(path?: string) => {
			getDirs(path)
				.then((b) => {
					setBrowse(b);
					onPathChange(b.path);
				})
				.catch(() => setBrowse({ path: path ?? "", dirs: [], truncated: false }));
		},
		[onPathChange],
	);

	useEffect(() => {
		load();
	}, [load]);

	const dirs = (browse?.dirs ?? []).filter(
		(d) =>
			(showHidden || !d.name.startsWith(".")) &&
			d.name.toLowerCase().includes(filter.toLowerCase()),
	);

	return (
		<>
			<div className="flex items-center gap-1.5 py-0.5">
				{browse?.parent && (
					<button
						type="button"
						disabled={disabled}
						onClick={() => load(browse.parent)}
						className="press-scale flex size-8 items-center justify-center rounded-xl border border-base-content/8 bg-base-300/80 text-[13px] text-accent active:bg-base-300"
					>
						<FaTurnUp size={13} />
					</button>
				)}
				<div
					className="min-w-0 flex-1 truncate font-mono text-[12px] text-muted"
					title={browse?.path}
				>
					{browse?.path.replace(/^\/home\/[^/]+/, "~")}
				</div>
				<button
					type="button"
					onClick={() => setShowHidden(!showHidden)}
					className={`press-scale rounded-xl border px-2.5 py-1 text-[11px] font-semibold transition-all ${
						showHidden
							? "border-accent/40 bg-accent/15 text-accent shadow-surface-sm"
							: "border-base-content/8 bg-base-300/60 text-muted active:text-base-content"
					}`}
				>
					hidden
				</button>
			</div>

			<input
				type="text"
				value={filter}
				onChange={(e) => setFilter(e.target.value)}
				placeholder="Filter folders…"
				className="min-h-[40px] rounded-2xl border border-base-content/10 bg-base-300/60 px-3.5 text-[15px] outline-none transition-all placeholder:text-muted/60 focus:border-primary/60 focus:shadow-[0_0_0_2px_rgba(64,157,212,0.18)]"
			/>

			<div className="flex max-h-48 min-h-24 flex-col gap-1 overflow-y-auto rounded-2xl border border-base-content/10 bg-base-300/50 p-1.5 shadow-surface-sm">
				{dirs.map((d) => (
					<button
						key={d.path}
						type="button"
						disabled={disabled}
						onClick={() => load(d.path)}
						className="press-scale block w-full truncate rounded-xl px-3 py-2 text-left text-[14px] text-base-content transition-colors active:bg-base-200 hover:bg-base-300/80"
					>
						{d.name}
					</button>
				))}
				{browse?.truncated && (
					<div className="px-3 py-1.5 text-[11px] text-muted">Showing the first 500 folders</div>
				)}
				{browse?.error && <div className="px-3 py-1.5 text-[12px] text-error">{browse.error}</div>}
			</div>
		</>
	);
}
