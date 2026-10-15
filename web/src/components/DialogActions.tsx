import { useState } from "react";
import { answerDialog, type BridgeDialog } from "../api";

/** The dialog answer bar. Confirm kinds render Yes/No (enter, and
 * down+enter - pi's confirm cursor starts on Yes). Select kinds render
 * the option rows parsed off the pane; tapping one walks pi's cursor
 * there and confirms it, and Cancel sends escape. Any in-band refusal
 * (409 changed, send failed) hides the bar - the stream brings
 * whatever pi shows now - and only a network throw re-enables the
 * buttons for a retry. */
export function DialogActions({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	const [busy, setBusy] = useState(false);
	const [stale, setStale] = useState(false);
	// Narrowed into locals: the closures would otherwise see id as
	// string | undefined and options as possibly-undefined again.
	const id = dialog.id;
	const options = dialog.kind === "select" ? dialog.options : undefined;
	const isConfirm = dialog.kind === "confirm";
	const isSelect = options !== undefined && options.length > 0;
	if ((!isConfirm && !isSelect) || !id || stale) return null;
	const answer = (a: string) => {
		if (busy) return;
		setBusy(true);
		answerDialog(k, id, a)
			.then((r) => {
				if (!r.ok) setStale(true);
				// On success the keys are on their way; the stream clears
				// the bar when pi's end event lands.
			})
			.catch(() => setBusy(false));
	};
	return (
		<div className="border-b border-base-300 px-3 py-1.5 text-[13px]">
			<div className="flex items-center gap-2">
				<span className="min-w-0 flex-1 truncate text-accent" title={dialog.title || dialog.kind}>
					pi asks: {dialog.title || dialog.kind}
				</span>
				{isConfirm && (
					<>
						<button
							type="button"
							disabled={busy}
							onClick={() => answer("yes")}
							className="min-h-[36px] rounded-lg bg-primary px-4 font-medium text-primary-content active:scale-95 disabled:opacity-50"
						>
							Yes
						</button>
						<button
							type="button"
							disabled={busy}
							onClick={() => answer("no")}
							className="min-h-[36px] rounded-lg bg-base-300 px-4 active:scale-95 disabled:opacity-50"
						>
							No
						</button>
					</>
				)}
			</div>
			{options !== undefined && options.length > 0 && (
				<div className="mt-1.5 flex flex-col gap-1">
					{options.map((o, i) => (
						<button
							// biome-ignore lint/suspicious/noArrayIndexKey: option labels can repeat; the index keeps keys unique for these render-only rows
							key={`${o}-${i}`}
							type="button"
							disabled={busy}
							onClick={() => answer(o)}
							className={
								"min-h-[36px] rounded-lg px-3 text-left active:scale-95 disabled:opacity-50 " +
								(i === dialog.cursor
									? "bg-primary font-medium text-primary-content"
									: "bg-base-300")
							}
						>
							{o}
						</button>
					))}
					<button
						type="button"
						disabled={busy}
						onClick={() => answer("escape")}
						className="min-h-[36px] rounded-lg text-muted active:scale-95 disabled:opacity-50"
					>
						Cancel
					</button>
				</div>
			)}
		</div>
	);
}
