import { Sheet } from "konsta/react";
import { useEffect, useState } from "react";
import { createAgentInWorkspace, getWorkspaces, type Workspace } from "../api";
import { navigate, useSheetBack } from "../nav";
import { AgentLauncherFields, type LaunchFields } from "./AgentLauncherFields";
import { FaCaretDown } from "./fa";
import { Spinner } from "./Spinner";

// The FAB speed dial's "New agent" step: pick an existing workspace,
// write the first prompt with attachments and an optional
// model/thinking pin, and the server splits a pane in it and starts
// the agent there. Fields are shared with the quick sheet.
export function NewAgentSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	useSheetBack(opened, onClose);
	const [workspaces, setWorkspaces] = useState<Workspace[] | null>(null);
	const [wsOpen, setWsOpen] = useState(false);
	const [picked, setPicked] = useState("");
	const [fields, setFields] = useState<LaunchFields>({ body: "", uploading: false });
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	useEffect(() => {
		if (!opened) return;
		setWorkspaces(null);
		setWsOpen(false);
		setPicked("");
		setFields({ body: "", uploading: false });
		setBusy(false);
		setError("");
		getWorkspaces()
			.then(setWorkspaces)
			.catch((e: Error) => setError(e.message));
	}, [opened]);

	const pickedWs = (workspaces ?? []).find((w) => w.workspace_id === picked);

	const create = () => {
		if (!picked || !fields.body.trim() || fields.uploading || busy) return;
		setBusy(true);
		setError("");
		createAgentInWorkspace(picked, fields.body, fields.model, fields.thinking)
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

				<button
					type="button"
					disabled={busy}
					onClick={() => setWsOpen(!wsOpen)}
					className="flex min-h-9 items-center gap-1.5 rounded-lg bg-base-300 px-2.5 text-[13px]"
				>
					<span className="truncate">
						{pickedWs ? `${pickedWs.label} #${pickedWs.number}` : "Pick a workspace"}
					</span>
					<FaCaretDown size={11} className="ml-auto shrink-0 text-muted" />
				</button>

				{wsOpen && (
					<div className="max-h-40 overflow-y-auto rounded-xl border border-muted/30">
						{workspaces === null && (
							<div className="flex items-center justify-center py-4">
								<Spinner size={14} />
							</div>
						)}
						{workspaces !== null && workspaces.length === 0 && (
							<div className="px-2 py-2 text-[13px] text-muted">No workspaces yet</div>
						)}
						{(workspaces ?? []).map((w) => (
							<button
								key={w.workspace_id}
								type="button"
								disabled={busy}
								onClick={() => {
									setPicked(w.workspace_id);
									setWsOpen(false);
								}}
								className={`block w-full truncate px-2 py-2 text-left text-[14px] ${picked === w.workspace_id ? "bg-primary/20 text-base-content" : ""}`}
							>
								{w.label}
								<span className="ml-2 text-[11px] text-muted">#{w.number}</span>
							</button>
						))}
					</div>
				)}

				<AgentLauncherFields opened={opened} busy={busy} onChange={setFields} />

				{error && <div className="text-[12px] text-error">{error}</div>}

				<button
					type="button"
					disabled={busy || !picked || !fields.body.trim() || fields.uploading}
					onClick={create}
					className="press-scale flex min-h-[48px] items-center justify-center gap-2 rounded-2xl bg-primary text-[15px] font-semibold text-primary-content shadow-[0_2px_12px_rgba(64,157,212,0.4)] disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					{busy ? "Creating…" : "Start agent"}
				</button>
			</div>
		</Sheet>
	);
}
