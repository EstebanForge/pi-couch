import { Sheet } from "konsta/react";
import { useEffect, useState } from "react";
import { createQuickAgent } from "../api";
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

	useEffect(() => {
		if (!opened) return;
		setFields({ body: "", uploading: false });
		setBusy(false);
		setError("");
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
			<div className="flex max-h-[80dvh] flex-col gap-2 p-3">
				<div className="text-[13px] font-semibold tracking-wider text-muted uppercase">
					Quick agent
				</div>

				<div className="truncate text-[12px] text-muted">{tildify(quickPath())}</div>

				<AgentLauncherFields opened={opened} busy={busy} onChange={setFields} />

				{error && <div className="text-[12px] text-error">{error}</div>}

				<button
					type="button"
					disabled={busy || !fields.body.trim() || fields.uploading}
					onClick={create}
					className="flex min-h-[44px] items-center justify-center gap-2 rounded-xl bg-primary font-semibold text-primary-content disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					{busy ? "Creating…" : "Start agent"}
				</button>
			</div>
		</Sheet>
	);
}
