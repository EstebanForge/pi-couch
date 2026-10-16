import { Sheet } from "konsta/react";
import { useEffect, useMemo, useState } from "react";
import { createAgent } from "../api";
import { navigate, useSheetBack } from "../nav";
import { useSessions } from "../state/sessions";
import { DirBrowser } from "./DirBrowser";
import { Spinner } from "./Spinner";

// The new-agent flow behind the home FAB: recent folders as chips,
// a jailed directory browser, a required first prompt. Create runs
// the herdr sequence server-side and lands in the new session.

const MAX_CHIPS = 8;

export function NewSessionSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	useSheetBack(opened, onClose);
	const sessions = useSessions((s) => s.sessions);
	const [pickedPath, setPickedPath] = useState("");
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

	useEffect(() => {
		if (!opened) setPickedPath("");
	}, [opened]);

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
				<div className="text-[13px] font-semibold tracking-wider text-muted uppercase">
					New agent
				</div>

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

				<DirBrowser onPathChange={setPickedPath} disabled={busy} />

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
					disabled={busy || !pickedPath || !prompt.trim()}
					onClick={() => create(pickedPath)}
					className="flex min-h-[44px] items-center justify-center gap-2 rounded-xl bg-primary font-semibold text-primary-content disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					{busy ? "Creating…" : "Create agent here"}
				</button>
			</div>
		</Sheet>
	);
}
