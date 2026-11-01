import { useState } from "react";
import { type AskAnswerRequest, type AskQuestion, answerAsk, type BridgeDialog } from "../api";
import { Spinner } from "./Spinner";

/** The rpiv ask_user_question card (ADR-0009): the questionnaire as
 * the tool authored it, answered natively through the bridge's answer
 * channel - no Terminal trip, no keystroke walking. Single-select rows
 * tap like radios, multiSelect rows toggle like checkboxes, and every
 * question keeps the reserved custom-answer text row (options + text
 * combine on multiSelect only, mirroring rpiv's contract). Submit
 * posts the structured answer and returns the real verdict; ok keeps
 * the spinner until the stream's close frame clears the card, a
 * refusal keeps the card with the reason, a timeout means the answer
 * may still land. Same surface conventions as DialogActions: pi's own
 * dialogs already answer from hand-built cards, so Konsta's form
 * components would read as a different app here. */

/** Submit gate: every question answered - options picked or custom
 * text typed, either one counts. */
export function askReady(picked: number[][], texts: string[]): boolean {
	return picked.every((sel, qi) => sel.length > 0 || (texts[qi] ?? "").trim() !== "");
}

/** The structured answer payload. Single-select questions carry text
 * over options when both are somehow present: rpiv refuses options +
 * text combined, and the typed words are the user's latest intent. */
export function askAnswerBody(
	toolCallID: string,
	questions: AskQuestion[],
	picked: number[][],
	texts: string[],
): AskAnswerRequest {
	return {
		tool_call_id: toolCallID,
		answers: questions.map((q, qi) => {
			const text = (texts[qi] ?? "").trim();
			const options = q.multi_select === true ? picked[qi] : text === "" ? picked[qi] : [];
			return {
				question_index: qi,
				...(options.length ? { option_indexes: options } : {}),
				...(text ? { text } : {}),
			};
		}),
	};
}

export function AskCard({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	const questions = dialog.questions ?? [];
	const [picked, setPicked] = useState<number[][]>(() => questions.map(() => []));
	const [texts, setTexts] = useState<string[]>(() => questions.map(() => ""));
	const [busy, setBusy] = useState(false);
	const [failed, setFailed] = useState<"refused" | "timeout" | "network" | null>(null);
	const [detail, setDetail] = useState("");
	if (!dialog.id || questions.length === 0) return null;
	const id = dialog.id;

	const toggle = (qi: number, oi: number) => {
		if (busy) return;
		const multi = questions[qi].multi_select === true;
		setPicked((prev) =>
			prev.map((sel, i) => {
				if (i !== qi) return sel;
				if (multi)
					return sel.includes(oi)
						? sel.filter((x) => x !== oi)
						: [...sel, oi].sort((a, b) => a - b);
				return sel.includes(oi) ? [] : [oi];
			}),
		);
		// Single-select takes options or text, never both: tapping after
		// typing must not build an answer rpiv would refuse.
		if (!multi) {
			setTexts((prev) => prev.map((t, i) => (i === qi ? "" : t)));
		}
	};

	const type = (qi: number, text: string) => {
		if (busy) return;
		setTexts((prev) => prev.map((t, i) => (i === qi ? text : t)));
		// Same exclusivity, from the text side.
		if (text.trim() !== "" && questions[qi].multi_select !== true) {
			setPicked((prev) => prev.map((sel, i) => (i === qi ? [] : sel)));
		}
	};

	const send = (cancel: boolean) => {
		if (busy) return;
		if (!cancel && !askReady(picked, texts)) return;
		setBusy(true);
		setFailed(null);
		const body = cancel
			? { tool_call_id: id, cancel: true }
			: askAnswerBody(id, questions, picked, texts);
		answerAsk(k, body)
			.then((r) => {
				if (!r.ok) {
					setFailed(r.code === "timeout" ? "timeout" : "refused");
					setDetail(r.reason ?? r.error ?? "");
					setBusy(false);
				}
				// On ok the verdict is in; the stream's close frame clears the card.
			})
			.catch((e: unknown) => {
				const name = (e as { name?: string } | null)?.name;
				setFailed(name === "TimeoutError" || name === "AbortError" ? "timeout" : "network");
				setBusy(false);
			});
	};

	return (
		<div className="max-h-[70vh] overflow-y-auto px-4 pb-4 pt-3">
			<div className="text-[11px] uppercase tracking-widest text-accent">pi asks</div>
			<div className="mt-0.5 text-[14px] font-medium">{dialog.title || "Question"}</div>
			{questions.map((q, qi) => (
				<div key={q.header || q.question} className="mt-3">
					<div className="flex items-center gap-2">
						{q.header && (
							<span className="rounded-full bg-base-300 px-2 py-0.5 text-[10px] uppercase tracking-wider text-muted">
								{q.header}
							</span>
						)}
						{q.multi_select === true && (
							<span className="text-[10px] uppercase tracking-wider text-muted">pick any</span>
						)}
					</div>
					<p className="mt-1 whitespace-pre-wrap break-words text-[14px] [overflow-wrap:anywhere]">
						{q.question}
					</p>
					<div className="mt-2 flex flex-col gap-1.5">
						{q.options.map((o, oi) => {
							const on = picked[qi].includes(oi);
							return (
								<button
									// biome-ignore lint/suspicious/noArrayIndexKey: option labels can repeat; the index keeps keys unique for these render-only rows
									key={`${o.label}-${oi}`}
									type="button"
									disabled={busy}
									onClick={() => toggle(qi, oi)}
									className={
										"flex min-h-[48px] items-center gap-2 rounded-xl px-3.5 py-2 text-left active:scale-[0.99] disabled:opacity-50 " +
										(on ? "bg-primary/20 font-medium text-base-content" : "bg-base-300")
									}
								>
									<span className={on ? "text-primary" : "text-muted"}>
										{q.multi_select === true ? (on ? "[✓]" : "[ ]") : on ? "●" : "○"}
									</span>
									<span className="min-w-0 flex-1">
										<span className="block text-[14px]">{o.label}</span>
										{o.description && (
											<span className="mt-0.5 block text-[12px] opacity-80 [overflow-wrap:anywhere]">
												{o.description}
											</span>
										)}
									</span>
								</button>
							);
						})}
					</div>
					<input
						value={texts[qi]}
						onChange={(e) => type(qi, e.target.value)}
						disabled={busy}
						placeholder="Type your own answer…"
						aria-label={`Own answer for ${q.header || q.question}`}
						className="mt-2 h-11 w-full rounded-xl bg-base-300 px-3 text-[14px] outline-none placeholder:text-muted"
					/>
				</div>
			))}
			{failed === "refused" && (
				<p className="mt-2 text-[12px] text-accent">
					pi refused that answer{detail ? ` (${detail})` : ""} - it may have moved on. Try again or
					cancel.
				</p>
			)}
			{failed === "timeout" && (
				<p className="mt-2 text-[12px] text-accent">
					the server didn't answer in time - the answer may still land. Check the terminal before
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
					onClick={() => send(true)}
					className="flex min-h-[44px] flex-1 items-center justify-center gap-1.5 rounded-xl bg-base-300 px-4 py-2 text-[14px] text-base-content active:scale-[0.99] disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					Cancel
				</button>
				<button
					type="button"
					disabled={busy || !askReady(picked, texts)}
					onClick={() => send(false)}
					className="flex min-h-[44px] flex-1 items-center justify-center gap-1.5 rounded-xl bg-primary px-4 py-2 text-[14px] font-medium text-primary-content active:scale-[0.99] disabled:opacity-50"
				>
					{busy && <Spinner size={14} />}
					Submit
				</button>
			</div>
		</div>
	);
}
