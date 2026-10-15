import { Sheet } from "konsta/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { createAgent, getDirs, type DirsResponse } from "../api";
import { navigate } from "../nav";
import { useSessions } from "../state/sessions";
import { Spinner } from "./Spinner";

// The new-agent flow behind the home FAB: recent folders as chips,
// a jailed directory browser, a required first prompt. Create runs
// the herdr sequence server-side and lands in the new session.

const MAX_CHIPS = 8;

export function NewSessionSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	const sessions = useSessions((s) => s.sessions);
	const [browse, setBrowse] = useState<DirsResponse | null>(null);
	const [filter, setFilter] = useState("");
	const [showHidden, setShowHidden] = useState(false);
	const [prompt, setPrompt] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	const recent = useMemo(() => {
		const seen = new Set<string>();
		const out: string[] = [];
		for (const s of sessions) {
			if (s.cwd && !seen.has(s.cwd)) {
				seen.add(s.cwd);
				out.push(s.cwd);
				if (out.length >= MAX_CHIPS) break;
			}
		}
		return out;
	}, [sessions]);

	const load = useCallback((path?: string) => {
		getDirs(path)
			.then(setBrowse)
			.catch(() => setError("Cannot list folders"));
	}, []);

	useEffect(() => {
		if (opened) load();
	}, [opened, load]);

	const dirs = (browse?.dirs ?? []).filter(
		(d) => (showHidden || !d.name.startsWith(".")) && d.name.toLowerCase().includes(filter.toLowerCase()),
	);

	const create = (cwd: string) => {
		if (!prompt.trim() || busy) return;
		setBusy(true);
		setError("");
		createAgent(cwd, prompt)
			.then(({ key }) => {
				navigate(`/sessions/${encodeURIComponent(key)}`);
				onClose();
			})
			.catch((e: Error) => setError(e.message))
			.finally(() => setBusy(false));
	};

	return (
		<Sheet
			opened={opened}
			onBackdropClick={() => {
				if (!busy) onClose();
			}}
			className="pb-safe-all bg-base-200"
		>
			<div className="flex max-h-[80dvh] flex-col gap-2 p-3">
				<div className="text-[13px] font-semibold tracking-wider text-muted uppercase">New agent</div>

				{recent.length > 0 && (
					<div className="flex flex-wrap gap-1">
						{recent.map((c) => (
							<button
								key={c}
								type="button"
								disabled={busy}
								onClick={() => create(c)}
								className="max-w-full truncate rounded-full border border-muted/40 px-2 py-1 text-[12px] text-muted active:scale-95"
								title={c}
							>
								{c.replace(/^\/home\/[^/]+/, "~")}
							</button>
						))}
					</div>
				)}

				<div className="flex items-center gap-1">
					{browse?.parent && (
						<button
							type="button"
							disabled={busy}
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
							disabled={busy}
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

				<textarea
					value={prompt}
					onChange={(e) => setPrompt(e.target.value)}
					rows={2}
					placeholder="First prompt for the agent…"
					className="rounded-xl border border-muted/40 bg-base-300 px-3 py-2 text-[16px] outline-none focus:border-primary/60"
				/>

				{error && <div className="text-[12px] text-error">{error}</div>}

				<button
					type="button"
					disabled={busy || !browse || !prompt.trim()}
					onClick={() => browse && create(browse.path)}
					className="flex min-h-[44px] items-center justify-center gap-2 rounded-xl bg-primary font-semibold text-primary-content disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					{busy ? "Creating…" : "Create agent here"}
				</button>
			</div>
		</Sheet>
	);
}
