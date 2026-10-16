import { Sheet } from "konsta/react";
import { useEffect, useState } from "react";
import { createAgentInWorkspace, getWorkspaces, type Workspace } from "../api";
import { navigate, useSheetBack } from "../nav";
import { Spinner } from "./Spinner";

// The FAB speed dial's "New agent" step: pick an existing workspace,
// write the first prompt, and the server splits a pane in it and
// starts the agent there.

export function NewAgentSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	useSheetBack(opened, onClose);
	const [workspaces, setWorkspaces] = useState<Workspace[] | null>(null);
	const [error, setError] = useState("");
	const [picked, setPicked] = useState("");
	const [prompt, setPrompt] = useState("");
	const [busy, setBusy] = useState(false);

	useEffect(() => {
		if (!opened) return;
		setWorkspaces(null);
		setError("");
		setPicked("");
		setPrompt("");
		getWorkspaces()
			.then(setWorkspaces)
			.catch((e: Error) => setError(e.message));
	}, [opened]);

	const create = () => {
		if (!picked || busy) return;
		setBusy(true);
		setError("");
		createAgentInWorkspace(picked, prompt)
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
					New agent in workspace
				</div>

				{workspaces === null && !error && (
					<div className="flex items-center justify-center py-6">
						<Spinner size={16} />
					</div>
				)}

				{workspaces !== null && workspaces.length === 0 && (
					<div className="px-2 py-3 text-[13px] text-muted">No workspaces yet</div>
				)}

				<div className="max-h-56 min-h-24 overflow-y-auto">
					{(workspaces ?? []).map((w) => (
						<button
							key={w.workspace_id}
							type="button"
							disabled={busy}
							onClick={() => setPicked(w.workspace_id)}
							className={`block w-full truncate rounded-lg px-2 py-2 text-left text-[14px] active:scale-[0.98] ${
								picked === w.workspace_id ? "bg-primary/20 text-base-content" : "hover:bg-base-300"
							}`}
						>
							{w.label}
							<span className="ml-2 text-[11px] text-muted">#{w.number}</span>
						</button>
					))}
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
					disabled={busy || !picked || !prompt.trim()}
					onClick={create}
					className="flex min-h-[44px] items-center justify-center gap-2 rounded-xl bg-primary font-semibold text-primary-content disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					{busy ? "Creating…" : "Create agent there"}
				</button>
			</div>
		</Sheet>
	);
}
