import { useCallback, useEffect, useState } from "react";
import { type DirsResponse, getDirs } from "../api";

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
			<div className="flex items-center gap-1">
				{browse?.parent && (
					<button
						type="button"
						disabled={disabled}
						onClick={() => load(browse.parent)}
						className="min-h-[36px] rounded-lg px-2 text-[13px] text-accent active:scale-95"
					>
						↑
					</button>
				)}
				<div className="min-w-0 flex-1 truncate text-[12px] text-muted" title={browse?.path}>
					{browse?.path.replace(/^\/home\/[^/]+/, "~")}
				</div>
				<button
					type="button"
					onClick={() => setShowHidden(!showHidden)}
					className={`min-h-[36px] rounded-lg px-2 text-[12px] ${showHidden ? "text-accent" : "text-muted"}`}
				>
					hidden
				</button>
			</div>

			<input
				type="text"
				value={filter}
				onChange={(e) => setFilter(e.target.value)}
				placeholder="Filter folders…"
				className="min-h-[36px] rounded-xl border border-muted/40 bg-transparent px-3 text-[16px] outline-none focus:border-primary/60"
			/>

			<div className="max-h-48 min-h-24 overflow-y-auto">
				{dirs.map((d) => (
					<button
						key={d.path}
						type="button"
						disabled={disabled}
						onClick={() => load(d.path)}
						className="block w-full truncate rounded-lg px-2 py-2 text-left text-[14px] active:scale-[0.98] hover:bg-base-300"
					>
						{d.name}
					</button>
				))}
				{browse?.truncated && (
					<div className="px-2 py-1 text-[11px] text-muted">Showing the first 500 folders</div>
				)}
				{browse?.error && <div className="px-2 py-1 text-[12px] text-error">{browse.error}</div>}
			</div>
		</>
	);
}
