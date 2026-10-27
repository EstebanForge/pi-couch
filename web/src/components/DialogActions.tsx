import { useEffect, useRef, useState } from "react";
import { answerDialog, type BridgeDialog } from "../api";
import { Spinner } from "./Spinner";

const piOwnAnswerRe = /^type something/i;

/** The inline dialog card: whatever pi waits on, read off the pane by
 * the server's parser and answered from the chat - no Terminal trip.
 * Option rows tap into the verify-then-fire walk (the cursor highlight
 * is the pane's), an editor draft shows its text with Submit, and a
 * free-text row turns into a real input - pi's "Type something." and
 * native input widgets both answer from here. An in-band refusal
 * (409 changed, send failed) reads as pi refusing; a network throw
 * reads as unreachable; a timeout reads as unknown - the server may
 * still land the keys, so check the terminal before retrying. All
 * three keep the card. The tapped control keeps its spinner until
 * the keys land, so the seconds before pi's end event never read as a
 * dead tap. */
export function DialogActions({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	const [busy, setBusy] = useState(false);
	const [active, setActive] = useState<string>("");
	const [failed, setFailed] = useState<"refused" | "network" | "timeout" | null>(null);
	const [detail, setDetail] = useState("");
	const [draft, setDraft] = useState("");
	// The editor dialog's buffer is one message, exactly as pi paints
	// it: title, blank line, description. One textarea edits it all at
	// once and Submit rewrites pi's editor and accepts in one step.
	// A scrolled editor paints only the buffer's tail - the head is
	// not on the pane - so the box starts empty instead of prefilling
	// a fragment whose subject would be silently lost on a rewrite.
	const [message, setMessage] = useState(dialog.scrolled ? "" : (dialog.body ?? ""));
	const servedBody = useRef(message);
	// A multi-question rpiv dialog keeps one id across its questions, so
	// a successful answer never brings the closing dialog:null frame -
	// the next question's paint arrives instead. New served content
	// means pi moved on: re-arm the card or the second question renders
	// permanently disabled.
	const servedSig = useRef("");
	useEffect(() => {
		const sig = `${dialog.title}\n${(dialog.options ?? []).map((o) => o.label).join("\n")}`;
		if (servedSig.current !== "" && sig !== servedSig.current) {
			setBusy(false);
			setActive("");
			setFailed(null);
		}
		servedSig.current = sig;
	}, [dialog.title, dialog.options]);
	// biome-ignore lint/correctness/useExhaustiveDependencies: dialog.id re-runs the resync when a new dialog replaces the old, even if the body text is identical.
	useEffect(() => {
		const next = dialog.scrolled ? "" : (dialog.body ?? "");
		// A stream tick never touches text mid-typing: the box only
		// takes a new parse while it still holds what was served last -
		// the scrolled-to-stitched transition fills it, edits survive.
		setMessage((prev) => (prev === servedBody.current ? next : prev));
		servedBody.current = next;
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
		setFailed(null);
		answerDialog(k, id, a, text)
			.then((r) => {
				if (!r.ok) {
					setFailed("refused");
					setDetail(r.error ?? "");
					setBusy(false);
					setActive("");
				}
				// On success the keys are on their way; the stream clears
				// the card when pi's end event lands.
			})
			.catch((e: unknown) => {
				const name = (e as { name?: string } | null)?.name;
				setFailed(name === "TimeoutError" || name === "AbortError" ? "timeout" : "network");
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
									? // Subtle tint, not solid primary: rows carry long
										// descriptions and a solid fill reads as a slab.
										"bg-primary/20 font-medium text-base-content"
									: "bg-base-300")
							}
						>
							{active === o.label && <Spinner size={14} />}
							<span className="min-w-0 flex-1">
								<span className="block text-[14px]">{o.label}</span>
								{o.description && (
									<span className="mt-0.5 block text-[12px] opacity-80 [overflow-wrap:anywhere]">
										{o.description}
									</span>
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
			{failed === "refused" && (
				<p className="mt-2 text-[12px] text-accent">
					pi refused that answer{detail ? ` (${detail})` : ""} - it may have moved on. Try again or
					cancel.
				</p>
			)}
			{failed === "timeout" && (
				<p className="mt-2 text-[12px] text-accent">
					the server didn't answer in time - the keys may still land. Check the terminal before
					retrying.
				</p>
			)}
			{failed === "network" && (
				<p className="mt-2 text-[12px] text-accent">
					couldn't reach the server - the request never landed. Try again.
				</p>
			)}
			<div className="mt-3 flex items-center gap-2">
				<button
					type="button"
					disabled={busy}
					onClick={() => answer("escape", "escape")}
					className="flex min-h-[44px] flex-1 items-center justify-center gap-1.5 rounded-xl bg-base-300 px-4 py-2 text-[14px] text-base-content active:scale-[0.99] disabled:opacity-50"
				>
					{active === "escape" && <Spinner size={14} />}
					Cancel
				</button>
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
			</div>
		</div>
	);
}
