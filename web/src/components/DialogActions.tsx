import { useState } from "react";
import { answerDialog, type BridgeDialog } from "../api";

/** The inline dialog card: whatever pi waits on, read off the pane by
 * the server's parser and answered from the chat - no Terminal trip.
 * Option rows tap into the verify-then-fire walk (the cursor highlight
 * is the pane's), an editor draft shows its text with Submit, and
 * every card carries Cancel (escape). Text-input widgets ("Type your
 * own answer") are the one thing a tap cannot answer - the card says
 * so instead of pretending. Any in-band refusal (409 changed, send
 * failed) hides the card - the stream brings whatever pi shows now -
 * and only a network throw re-enables it for a retry. */
export function DialogActions({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	const [busy, setBusy] = useState(false);
	const [stale, setStale] = useState(false);
	const id = dialog.id;
	if (!id || stale) return null;
	const options = dialog.options ?? [];
	const ownAnswer = options.find((o) => /^type something/i.test(o.label));
	const tappable = options.filter((o) => o !== ownAnswer);
	const isEditor = dialog.kind === "editor";

	const answer = (a: string) => {
		if (busy) return;
		setBusy(true);
		answerDialog(k, id, a)
			.then((r) => {
				if (!r.ok) setStale(true);
				// On success the keys are on their way; the stream clears
				// the card when pi's end event lands.
			})
			.catch(() => setBusy(false));
	};

	return (
		<div className="border-b border-base-300 bg-base-200/60 px-3 py-2">
			<div className="flex items-baseline gap-2">
				<span className="text-[11px] uppercase tracking-widest text-accent">pi asks</span>
				<span
					className="min-w-0 flex-1 truncate text-[14px] font-medium"
					title={dialog.title || dialog.kind}
				>
					{dialog.title || dialog.kind}
				</span>
			</div>
			{dialog.body && (
				<p className="mt-1 max-h-40 overflow-y-auto whitespace-pre-wrap break-words text-[13px] text-base-content/90">
					{dialog.body}
				</p>
			)}
			{tappable.length > 0 && (
				<div className="mt-2 flex flex-col gap-1">
					{tappable.map((o, i) => (
						<button
							// biome-ignore lint/suspicious/noArrayIndexKey: option labels can repeat; the index keeps keys unique for these render-only rows
							key={`${o.label}-${i}`}
							type="button"
							disabled={busy}
							onClick={() => answer(o.label)}
							className={
								"min-h-[44px] rounded-xl px-3 text-left active:scale-[0.99] disabled:opacity-50 " +
								(i === dialog.cursor
									? "bg-primary font-medium text-primary-content"
									: "bg-base-300")
							}
						>
							<span className="block text-[14px]">{o.label}</span>
							{o.description && (
								<span className="mt-0.5 block text-[12px] opacity-80">{o.description}</span>
							)}
						</button>
					))}
				</div>
			)}
			{ownAnswer && (
				<p className="mt-2 text-[12px] text-muted">
					{ownAnswer.label}: own answers need the Terminal's keyboard.
				</p>
			)}
			<div className="mt-2 flex items-center gap-2">
				{isEditor && (
					<button
						type="button"
						disabled={busy}
						onClick={() => answer("submit")}
						className="min-h-[40px] flex-1 rounded-xl bg-primary px-4 text-[14px] font-medium text-primary-content active:scale-[0.99] disabled:opacity-50"
					>
						Submit
					</button>
				)}
				<button
					type="button"
					disabled={busy}
					onClick={() => answer("escape")}
					className="min-h-[40px] flex-1 rounded-xl bg-base-300 px-4 text-[14px] text-base-content active:scale-[0.99] disabled:opacity-50"
				>
					Cancel
				</button>
			</div>
		</div>
	);
}
