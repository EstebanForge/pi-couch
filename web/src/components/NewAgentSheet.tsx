import { Sheet } from "konsta/react";
import { useEffect, useState } from "react";
import { createAgent, createAgentInSpace, getSpaces, type Space } from "../api";
import { navigate, useSheetBack } from "../nav";
import { AgentLauncherFields, type LaunchFields } from "./AgentLauncherFields";
import { DirBrowser } from "./DirBrowser";
import { FaCaretDown } from "./fa";
import { Spinner } from "./Spinner";

// The FAB speed dial's "New agent" step: spawn into a space (its
// folder, adopted into the herdr app's space tree) or browse any
// folder for a fresh one. A picked folder wins.
export function NewAgentSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	useSheetBack(opened, onClose);
	const [spaces, setSpaces] = useState<Space[] | null>(null);
	const [folder, setFolder] = useState("");
	const [wsOpen, setWsOpen] = useState(false);
	const [picked, setPicked] = useState("");
	const [fields, setFields] = useState<LaunchFields>({ body: "", uploading: false });
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	useEffect(() => {
		if (!opened) return;
		setSpaces(null);
		setFolder("");
		setWsOpen(false);
		setPicked("");
		setFields({ body: "", uploading: false });
		setBusy(false);
		setError("");
		getSpaces()
			.then(setSpaces)
			.catch(() => setSpaces([]));
	}, [opened]);

	const pickedSpace = (spaces ?? []).find((w) => w.id === picked);

	const create = () => {
		if (!fields.body.trim() || fields.uploading || busy) return;
		if (!picked && !folder) return;
		setBusy(true);
		setError("");
		const req = folder
			? createAgent(folder, fields.body, fields.model, fields.thinking)
			: createAgentInSpace(picked, fields.body, fields.model, fields.thinking);
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

				<button
					type="button"
					disabled={busy}
					onClick={() => setWsOpen(!wsOpen)}
					className="press-scale flex min-h-[46px] items-center gap-2 rounded-2xl border border-base-content/10 bg-base-300/80 px-3.5 text-[14.5px] font-medium shadow-surface-sm active:bg-base-300"
				>
					<span className="truncate">
						{pickedSpace ? `${pickedSpace.name} · ${pickedSpace.folder}` : "Pick a space"}
					</span>
					<FaCaretDown size={12} className="ml-auto shrink-0 text-muted" />
				</button>

				{wsOpen && (
					<div className="flex max-h-44 flex-col gap-1 overflow-y-auto rounded-2xl border border-base-content/10 bg-base-300/80 p-1.5 shadow-surface-sm">
						{spaces === null && (
							<div className="flex items-center justify-center py-4">
								<Spinner size={14} />
							</div>
						)}
						{spaces !== null && spaces.length === 0 && (
							<div className="px-3 py-2 text-[13px] text-muted">No spaces yet</div>
						)}
						{(spaces ?? []).map((w) => (
							<button
								key={w.id}
								type="button"
								disabled={busy}
								onClick={() => {
									setPicked(w.id);
									setWsOpen(false);
								}}
								className={`press-scale flex w-full flex-col rounded-xl px-3 py-2 text-left text-[14px] transition-colors ${
									picked === w.id
										? "bg-primary/20 font-semibold text-primary"
										: "text-base-content active:bg-base-200"
								}`}
							>
								<span className="truncate">{w.name}</span>
								<span className="truncate font-mono text-[11px] text-muted">{w.folder}</span>
							</button>
						))}
					</div>
				)}

				<div className="pt-1 text-[11px] font-semibold tracking-wide text-muted uppercase">
					Or browse any folder. A picked folder overrides the space.
				</div>

				<DirBrowser onPathChange={setFolder} disabled={busy} />

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
