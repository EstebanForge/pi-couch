import { Sheet } from "konsta/react";
import { useEffect, useState } from "react";
import { createQuickAgent, getHome } from "../api";
import { tildify } from "../fmt";
import { navigate, useSheetBack } from "../nav";
import { quickPath } from "../prefs";
import { AgentLauncherFields, type LaunchFields } from "./AgentLauncherFields";
import { Spinner } from "./Spinner";

// The FAB speed dial's "Quick agent" step: a first prompt with
// attachments and an optional model/thinking pin, then the agent
// starts in the fixed Quick workspace at the configured path.
export function QuickSheet({ opened, onClose }: { opened: boolean; onClose: () => void }) {
	useSheetBack(opened, onClose);
	const [fields, setFields] = useState<LaunchFields>({ body: "", uploading: false });
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	const [home, setHome] = useState("");

	useEffect(() => {
		if (!opened) return;
		setFields({ body: "", uploading: false });
		setBusy(false);
		setError("");
		void getHome().then(setHome);
	}, [opened]);

	const create = () => {
		if (!fields.body.trim() || fields.uploading || busy) return;
		setBusy(true);
		setError("");
		createQuickAgent("Quick", quickPath(), fields.body, fields.model, fields.thinking)
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
			<div className="flex max-h-[85dvh] flex-col gap-2.5 p-4">
				<div className="mx-auto mb-1 h-1.5 w-12 rounded-full bg-base-content/25" />
				<div className="text-[13px] font-bold tracking-wider text-muted uppercase">Quick agent</div>

				<div className="rounded-xl border border-base-content/6 bg-base-300/40 px-3 py-1.5 truncate text-[12.5px] font-mono text-muted">
					{tildify(quickPath(), home)}
				</div>

				<AgentLauncherFields opened={opened} busy={busy} onChange={setFields} />

				{error && <div className="text-[12px] text-error">{error}</div>}

				<button
					type="button"
					disabled={busy || !fields.body.trim() || fields.uploading}
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
