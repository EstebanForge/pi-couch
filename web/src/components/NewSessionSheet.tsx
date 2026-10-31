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
				<div className="mx-auto mb-1 h-1.5 w-12 rounded-full bg-base-content/25" />
				<div className="text-[13px] font-bold tracking-wider text-muted uppercase">New agent</div>

				{recent.length > 0 && (
					<div className="flex flex-col gap-1 py-1">
						<div className="text-[11px] font-semibold tracking-wide text-muted uppercase">
							Recent folders. Tap to create here.
						</div>
						<div className="flex flex-wrap gap-1.5">
							{recent.map((c) => (
								<button
									key={c}
									type="button"
									disabled={busy}
									onClick={() => create(c)}
									className="press-scale max-w-full truncate rounded-full border border-base-content/10 bg-base-300/80 px-3 py-1 text-[12px] font-medium text-base-content shadow-surface-sm active:bg-base-300"
									title={c}
								>
									{c.replace(/^\/home\/[^/]+/, "~")}
								</button>
							))}
						</div>
					</div>
				)}

				<DirBrowser onPathChange={setPickedPath} disabled={busy} />

				<textarea
					value={prompt}
					onChange={(e) => setPrompt(e.target.value)}
					rows={4}
					placeholder="First prompt for the agent…"
					className="rounded-2xl border border-base-content/10 bg-base-300/70 p-3 text-[15px] outline-none transition-all placeholder:text-muted/60 focus:border-primary/60 focus:shadow-[0_0_0_2px_rgba(64,157,212,0.18)]"
				/>

				{error && <div className="text-[12px] text-error">{error}</div>}

				<button
					type="button"
					disabled={busy || !pickedPath || !prompt.trim()}
					onClick={() => create(pickedPath)}
					className="press-scale flex min-h-[48px] items-center justify-center gap-2 rounded-2xl bg-primary text-[15px] font-semibold text-primary-content shadow-[0_2px_12px_rgba(64,157,212,0.4)] disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					{busy ? "Creating…" : "Create agent here"}
				</button>
			</div>
		</Sheet>
	);
}
