import { useState } from "react";
import { answerDialog, type BridgeDialog } from "../api";

/** The confirm-dialog answer bar: Yes presses enter, No presses
 * down+enter, both through the server's guarded answer endpoint. Any
 * in-band refusal (409 changed, send failed) hides the bar - the
 * stream brings whatever pi shows now - and only a network throw
 * re-enables the buttons for a retry. */
export function DialogActions({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	const [busy, setBusy] = useState(false);
	const [stale, setStale] = useState(false);
	// Narrowed into a local: the closure would otherwise see id as
	// string | undefined again.
	const id = dialog.id;
	if (dialog.kind !== "confirm" || !id || stale) return null;
	const answer = (a: "yes" | "no") => {
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
		<div className="flex items-center gap-2 border-b border-base-300 px-3 py-1.5 text-[13px]">
			<span className="min-w-0 flex-1 truncate text-accent" title={dialog.title || dialog.kind}>
				pi asks: {dialog.title || dialog.kind}
			</span>
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
		</div>
	);
}
