import { useEffect, useState } from "react";
import { answerDialog, type BridgeDialog } from "../api";
import { Spinner } from "./Spinner";

const piOwnAnswerRe = /^type something/i;

/** The inline dialog card: whatever pi waits on, read off the pane by
 * the server's parser and answered from the chat - no Terminal trip.
 * Option rows tap into the verify-then-fire walk (the cursor highlight
 * is the pane's), an editor draft shows its text with Submit, and a
 * free-text row turns into a real input - pi's "Type something." and
 * native input widgets both answer from here. Any in-band refusal
 * (409 changed, send failed) hides the card - the stream brings
 * whatever pi shows now - and only a network throw re-enables it for a
 * retry. The tapped control keeps its spinner until the keys land, so
 * the seconds before pi's end event never read as a dead tap. */
export function DialogActions({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	const [busy, setBusy] = useState(false);
	const [active, setActive] = useState<string>("");
	const [failed, setFailed] = useState(false);
	const [draft, setDraft] = useState("");
	// The editor dialog's buffer is one message, exactly as pi paints
	// it: title, blank line, description. One textarea edits it all at
	// once and Submit rewrites pi's editor and accepts in one step.
	// A scrolled editor paints only the buffer's tail - the head is
	// not on the pane - so the box starts empty instead of prefilling
	// a fragment whose subject would be silently lost on a rewrite.
	const [message, setMessage] = useState(dialog.scrolled ? "" : (dialog.body ?? ""));
	useEffect(() => {
		setMessage(dialog.scrolled ? "" : (dialog.body ?? ""));
	}, [dialog.id, dialog.body, dialog.scrolled]);
	const id = dialog.id;
	// A refused answer keeps the card with a retry note: pi may still be
	// waiting, and collapsing the panel strands a header sliver until
	// the next stream frame. The dialog:null frame closes it when pi
	// truly moved on.
	if (!id) return null;
	const options = dialog.options ?? [];
	const ownAnswer = options.find((o) => piOwnAnswerRe.test(o.label));
	const tappable = options.filter((o) => o !== ownAnswer);
	const isEditor = dialog.kind === "editor";
	const freeText = Boolean(ownAnswer) || dialog.kind === "input";

	const answer = (a: string, label: string, text?: string) => {
		if (busy) return;
		setBusy(true);
		setActive(label);
		setFailed(false);
		answerDialog(k, id, a, text)
			.then((r) => {
				if (!r.ok) {
					setFailed(true);
					setBusy(false);
					setActive("");
				}
				// On success the keys are on their way; the stream clears
				// the card when pi's end event lands.
			})
			.catch(() => {
				setFailed(true);
				setBusy(false);
				setActive("");
			});
	};

	const saveEdit = () => {
		if (busy) return;
		const text = message.trim();
		if (!text) return;
		answer("type", "submit", text);
	};

	return (
		<div className="px-4 pb-4 pt-3">
			<div className="text-[11px] uppercase tracking-widest text-accent">pi asks</div>
			<div
				className="mt-0.5 whitespace-normal break-words text-[14px] font-medium"
				title={dialog.title || dialog.kind}
			>
				{dialog.title || dialog.kind}
			</div>
			{dialog.body && !isEditor && (
				<p className="mt-1 max-h-40 overflow-y-auto whitespace-pre-wrap break-words text-[13px] text-base-content/90">
					{dialog.body}
				</p>
			)}
			{tappable.length > 0 && (
				<div className="mt-3 flex flex-col gap-1.5">
					{tappable.map((o, i) => (
						<button
							// biome-ignore lint/suspicious/noArrayIndexKey: option labels can repeat; the index keeps keys unique for these render-only rows
							key={`${o.label}-${i}`}
							type="button"
							disabled={busy}
							onClick={() => answer(o.label, o.label)}
							className={
								"flex min-h-[48px] items-center gap-2 rounded-xl px-3.5 py-2 text-left active:scale-[0.99] disabled:opacity-50 " +
								(i === dialog.cursor
									? "bg-primary font-medium text-primary-content"
									: "bg-base-300")
							}
						>
							{active === o.label && <Spinner size={14} />}
							<span>
								<span className="block text-[14px]">{o.label}</span>
								{o.description && (
									<span className="mt-0.5 block text-[12px] opacity-80">{o.description}</span>
								)}
							</span>
						</button>
					))}
				</div>
			)}
			{freeText && (
				<form
					className="mt-3 flex items-center gap-2"
					onSubmit={(e) => {
						e.preventDefault();
						if (draft.trim()) answer("type", "type", draft);
					}}
				>
					<input
						value={draft}
						onChange={(e) => setDraft(e.target.value)}
						disabled={busy}
						placeholder="Type your answer…"
						className="h-11 min-w-0 flex-1 rounded-xl bg-base-300 px-3 text-[14px] outline-none placeholder:text-muted"
					/>
					<button
						type="submit"
						disabled={busy || !draft.trim()}
						className="flex h-11 min-h-[44px] items-center gap-1.5 rounded-xl bg-primary px-4 text-[14px] font-medium text-primary-content active:scale-[0.99] disabled:opacity-50"
					>
						{active === "type" && <Spinner size={14} />}
						Send
					</button>
				</form>
			)}
			{isEditor && (
				<>
					{dialog.scrolled && (
						<p className="mt-2 text-[12px] text-muted">
							The message is longer than pi's editor shows. Write the full message below - it
							replaces the whole buffer.
						</p>
					)}
					<textarea
						value={message}
						onChange={(e) => setMessage(e.target.value)}
						disabled={busy}
						rows={Math.min(10, Math.max(3, message.split("\n").length))}
						placeholder="Commit message"
						aria-label="Commit message"
						className="mt-2 w-full resize-none rounded-xl bg-base-300 px-3 py-2 font-mono text-[13px] outline-none placeholder:text-muted"
					/>
				</>
			)}
			{failed && (
				<p className="mt-2 text-[12px] text-accent">
					pi refused that answer - it may have moved on. Try again or cancel.
				</p>
			)}
			<div className="mt-3 flex items-center gap-2">
				{isEditor && (
					<button
						type="button"
						disabled={busy}
						onClick={saveEdit}
						className="flex min-h-[44px] flex-1 items-center justify-center gap-1.5 rounded-xl bg-primary px-4 py-2 text-[14px] font-medium text-primary-content active:scale-[0.99] disabled:opacity-50"
					>
						{active === "submit" && <Spinner size={14} />}
						Submit
					</button>
				)}
				<button
					type="button"
					disabled={busy}
					onClick={() => answer("escape", "escape")}
					className="flex min-h-[44px] flex-1 items-center justify-center gap-1.5 rounded-xl bg-base-300 px-4 py-2 text-[14px] text-base-content active:scale-[0.99] disabled:opacity-50"
				>
					{active === "escape" && <Spinner size={14} />}
					Cancel
				</button>
			</div>
		</div>
	);
}
