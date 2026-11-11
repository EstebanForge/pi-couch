import { useEffect, useRef, useState } from "react";
import { answerDialog, type BridgeDialog } from "../api";
import { AskCard } from "./AskCard";
import { Spinner } from "./Spinner";

const piOwnAnswerRe = /^(?:type something.*|\d+[.)]\s*)$/i;
const checkboxPrefixRe = /^\[[ xX]\]\s*/;

/** The inline dialog card: whatever pi waits on, read off the pane by
 * the server's parser and answered from the chat - no Terminal trip.
 * Option rows tap into the verify-then-fire walk (the cursor highlight
 * is the pane's), an editor draft shows pi's buffer read-only with an
 * accept-as-is Submit (hc's send cannot rewrite a buffer), and a
 * free-text row turns into a real input - pi's "Type something." and
 * native input widgets both answer from here. An in-band refusal
 * (409 changed, send failed) reads as pi refusing; a network throw
 * reads as unreachable; a timeout reads as unknown - the server may
 * still land the keys, so check the terminal before retrying. All
 * three keep the card. The tapped control keeps its spinner until
 * the keys land, so the seconds before pi's end event never read as a
 * dead tap. */
export function dialogAnswerVerb(_label: string, multi: boolean, next = false): string {
	return multi && !next ? "toggle" : "option";
}

export function dialogServedSignature(dialog: BridgeDialog): string {
	// Body rides along: two same-title commit previews are different
	// dialogs, and the second must re-arm the card's busy state.
	return `${dialog.title}\n${(dialog.options ?? []).map((o) => `${o.label}:${o.checked === true}`).join("\n")}\n${dialog.input_mode === true}\n${dialog.body ?? ""}`;
}

export function DialogActions({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	// The rpiv questionnaire answers from its questions (ADR-0009); the
	// keystroke verbs below never fire for it.
	if (dialog.questions?.length) {
		return <AskCard k={k} dialog={dialog} />;
	}
	return <KeystrokeDialogActions k={k} dialog={dialog} />;
}

function KeystrokeDialogActions({ k, dialog }: { k: string; dialog: BridgeDialog }) {
	const [busy, setBusy] = useState(false);
	const [active, setActive] = useState<string>("");
	const [failed, setFailed] = useState<"refused" | "network" | "timeout" | null>(null);
	const [detail, setDetail] = useState("");
	const [draft, setDraft] = useState("");
	// The editor dialog's buffer starts as the message pi holds; the
	// box is editable (Submit rewrites pi's buffer with what the box
	// shows), except when scrolled: the painted body is only the tail,
	// so that box stays empty and Submit accepts as-is.
	const [message, setMessage] = useState(dialog.scrolled ? "" : (dialog.body ?? ""));
	// A multi-question rpiv dialog keeps one id across its questions, so
	// a successful answer never brings the closing dialog:null frame -
	// the next question's paint arrives instead. New served content
	// means pi moved on: re-arm the card or the second question renders
	// permanently disabled.
	const servedSig = useRef("");
	useEffect(() => {
		const sig = dialogServedSignature(dialog);
		if (servedSig.current !== "" && sig !== servedSig.current) {
			setBusy(false);
			setActive("");
			setFailed(null);
			setMessage(dialog.scrolled ? "" : (dialog.body ?? ""));
		}
		servedSig.current = sig;
	}, [dialog]);
	const id = dialog.id;
	// A refused answer keeps the card with a retry note: pi may still be
	// waiting, and collapsing the panel strands a header sliver until
	// the next stream frame. The dialog:null frame closes it when pi
	// truly moved on.
	if (!id) return null;
	const options = dialog.options ?? [];
	const multi = options.some((o) => o.checkbox);
	const ownAnswer = options.find((o) => piOwnAnswerRe.test(o.label.replace(checkboxPrefixRe, "")));
	// hc's type verb only lands in a real input state: an input-kind
	// dialog or an open input hint. On a single-select the custom row is
	// a tappable option that opens pi's input, and the box appears on
	// the next pane parse. Multi-select keeps the legacy box and hides
	// the custom row: its rows toggle, so a tap would answer wrong.
	const tappable = multi ? options.filter((o) => o !== ownAnswer) : options;
	const isEditor = dialog.kind === "editor";
	const freeText = dialog.kind === "input" || Boolean(dialog.input_mode) || (Boolean(ownAnswer) && multi);

	const answer = (a: string, label: string, text?: string, was?: string) => {
		if (busy) return;
		setBusy(true);
		setActive(label);
		setFailed(null);
		answerDialog(k, id, a, text, was)
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

	const accept = () => {
		if (busy) return;
		// An edited editor rides the type verb: the server rewrites pi's
		// buffer with the box's text (per-line clear, raw paste, content
		// match) before the enter, and the served body rides as was —
		// proof the user saw this buffer and not a same-title successor.
		// Whitespace-only differences stay unedited: pi soft-wraps, and
		// the rewrite path is churn the server would refuse anyway.
		const collapse = (s: string) => s.replace(/\s+/g, " ").trim();
		const body = dialog.body ?? "";
		const edited = !dialog.scrolled && collapse(message) !== collapse(body);
		if (edited) answer("type", "submit", message, body);
		else answer("yes", "submit", message || undefined);
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
			{multi && (
				<p className="mt-2 text-[13px] text-muted">Select one or more options, then Submit.</p>
			)}
			{tappable.length > 0 && (
				<div className="mt-3 flex flex-col gap-1.5">
					{tappable.map((o, i) => (
						<button
							// biome-ignore lint/suspicious/noArrayIndexKey: option labels can repeat; the index keeps keys unique for these render-only rows
							key={`${o.label}-${i}`}
							type="button"
							disabled={busy}
							onClick={() => {
								const verb = dialogAnswerVerb(o.label, multi, o.next);
								answer(
									verb === "toggle" ? "toggle" : o.label,
									o.label,
									verb === "toggle" ? o.label : undefined,
								);
							}}
							className={
								"flex min-h-[48px] items-center gap-2 rounded-xl px-3.5 py-2 text-left active:scale-[0.99] disabled:opacity-50 " +
								(i === dialog.cursor && !multi
									? // Subtle tint, not solid primary: rows carry long
										// descriptions and a solid fill reads as a slab.
										"bg-primary/20 font-medium text-base-content"
									: "bg-base-300")
							}
						>
							{active === o.label && <Spinner size={14} />}
							<span className="min-w-0 flex-1">
								<span className="block text-[14px]">
									{o.checkbox ? `${o.checked ? "[✓]" : "[ ]"} ${o.label}` : o.label}
								</span>
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
							The message is longer than pi's editor shows. Submit accepts the whole buffer as it
							stands in pi; edit it there first if it needs changing.
						</p>
					)}
					<textarea
						value={message}
						readOnly={Boolean(dialog.scrolled)}
						onChange={(e) => setMessage(e.target.value)}
						rows={Math.min(10, Math.max(3, Math.max(1, message.split("\n").length)))}
						placeholder="pi's editor holds the message"
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
						disabled={busy || (!dialog.scrolled && !message.trim())}
						onClick={accept}
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
