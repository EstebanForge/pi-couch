import { Sheet } from "konsta/react";
import { useEffect, useState } from "react";
import {
	createAgent,
	createAgentInWorkspace,
	getVersion,
	getWorkspaces,
	type Workspace,
} from "../api";
import { navigate, useSheetBack } from "../nav";
import { AgentLauncherFields, type LaunchFields } from "./AgentLauncherFields";
import { DirBrowser } from "./DirBrowser";
import { FaCaretDown } from "./fa";
import { Spinner } from "./Spinner";

// The FAB speed dial's "New agent" step: split an existing workspace
// or browse any folder for a fresh one. A picked folder wins. Fields
// are shared with the quick sheet. Under hc only the folder remains.
export function NewAgentSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	useSheetBack(opened, onClose);
	const [workspaces, setWorkspaces] = useState<Workspace[] | null>(null);
	const [driver, setDriver] = useState("herdr");
	const [folder, setFolder] = useState("");
	const [wsOpen, setWsOpen] = useState(false);
	const [picked, setPicked] = useState("");
	const [fields, setFields] = useState<LaunchFields>({ body: "", uploading: false });
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	useEffect(() => {
		if (!opened) return;
		setWorkspaces(null);
		setDriver("herdr");
		setFolder("");
		setWsOpen(false);
		setPicked("");
		setFields({ body: "", uploading: false });
		setBusy(false);
		setError("");
		getVersion()
			.then((v) => setDriver(v.driver ?? "herdr"))
			.catch(() => {});
		getWorkspaces()
			.then(setWorkspaces)
			.catch((e: Error) => setError(e.message));
	}, [opened]);

	const pickedWs = (workspaces ?? []).find((w) => w.workspace_id === picked);
	const hc = driver === "hc";

	const create = () => {
		if (!fields.body.trim() || fields.uploading || busy) return;
		if (!picked && !folder) return;
		setBusy(true);
		setError("");
		const req = folder
			? createAgent(folder, fields.body, fields.model, fields.thinking)
			: createAgentInWorkspace(picked, fields.body, fields.model, fields.thinking);
		req
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
			<div className="flex max-h-[85dvh] flex-col gap-2.5 overflow-y-auto p-4">
				<div className="mx-auto mb-1 h-1.5 w-12 rounded-full bg-base-content/25" />
				<div className="text-[13px] font-bold tracking-wider text-muted uppercase">New agent</div>

				{hc && <DirBrowser onPathChange={setFolder} disabled={busy} />}

				{!hc && (
					<button
						type="button"
						disabled={busy}
						onClick={() => setWsOpen(!wsOpen)}
						className="press-scale flex min-h-[46px] items-center gap-2 rounded-2xl border border-base-content/10 bg-base-300/80 px-3.5 text-[14.5px] font-medium shadow-surface-sm active:bg-base-300"
					>
						<span className="truncate">
							{pickedWs ? `${pickedWs.label} #${pickedWs.number}` : "Pick a workspace"}
						</span>
						<FaCaretDown size={12} className="ml-auto shrink-0 text-muted" />
					</button>
				)}

				{!hc && wsOpen && (
					<div className="flex max-h-44 flex-col gap-1 overflow-y-auto rounded-2xl border border-base-content/10 bg-base-300/80 p-1.5 shadow-surface-sm">
						{workspaces === null && (
							<div className="flex items-center justify-center py-4">
								<Spinner size={14} />
							</div>
						)}
						{workspaces !== null && workspaces.length === 0 && (
							<div className="px-3 py-2 text-[13px] text-muted">No workspaces yet</div>
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
								className={`press-scale flex w-full items-center justify-between rounded-xl px-3 py-2 text-left text-[14px] transition-colors ${
									picked === w.workspace_id
										? "bg-primary/20 font-semibold text-primary"
										: "text-base-content active:bg-base-200"
								}`}
							>
								<span className="truncate">{w.label}</span>
								<span className="ml-2 font-mono text-[11px] text-muted">#{w.number}</span>
							</button>
						))}
					</div>
				)}

				{!hc && (
					<div className="pt-1 text-[11px] font-semibold tracking-wide text-muted uppercase">
						Or browse any folder. A picked folder overrides the workspace.
					</div>
				)}

				{!hc && <DirBrowser onPathChange={setFolder} disabled={busy} />}

				<AgentLauncherFields opened={opened} busy={busy} onChange={setFields} />

				{error && <div className="text-[12px] text-error">{error}</div>}

				<button
					type="button"
					disabled={busy || (!picked && !folder) || !fields.body.trim() || fields.uploading}
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
