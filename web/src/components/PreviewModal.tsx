import { useEffect, useRef, useState } from "react";
import { getSessionPreview, type SessionPreview, sendPrompt } from "../api";
import { useSheetBack } from "../nav";
import { isWaiting } from "../status";
import { FaCheck, FaPencilAlt, FaXmark } from "./fa";
import { canRename, PreviewChip, PreviewLines } from "./previewLines";
import { Spinner } from "./Spinner";

type PreviewState =
	| { s: "loading" }
	| { s: "ok"; preview: SessionPreview }
	| { s: "error"; message: string };

// Hold-to-preview: a centered glance card, not a sheet. Konsta's Popup
// is a bottom sheet and this is a look-only card, so the shell is
// hand-built like the home FAB scrim. One fetch on open, no
// auto-refresh - the frozen snapshot is the point.
export function PreviewModal({
	sessionKey,
	title,
	onClose,
}: {
	sessionKey: string;
	title: string;
	onClose: () => void;
}) {
	useSheetBack(true, onClose);
	const [state, setState] = useState<PreviewState>({ s: "loading" });
	const [shownTitle, setShownTitle] = useState(title);
	const [renaming, setRenaming] = useState(false);
	const [renameVal, setRenameVal] = useState("");
	const [renameError, setRenameError] = useState("");
	const bodyRef = useRef<HTMLDivElement>(null);

	useEffect(() => setShownTitle(title), [title]);

	useEffect(() => {
		let alive = true;
		setState({ s: "loading" });
		getSessionPreview(sessionKey)
			.then((p) => {
				if (alive) setState({ s: "ok", preview: p });
			})
			.catch((e: unknown) => {
				if (alive) setState({ s: "error", message: e instanceof Error ? e.message : String(e) });
			});
		return () => {
			alive = false;
		};
	}, [sessionKey]);

	// The tail is newest-last: open on the bottom edge, chat-style.
	useEffect(() => {
		if (state.s === "ok") {
			const el = bodyRef.current;
			if (el) el.scrollTop = el.scrollHeight;
		}
	}, [state]);

	const status = state.s === "ok" ? state.preview.status : "unknown";
	const runRename = async () => {
		const name = renameVal.trim();
		if (!name || name === shownTitle) {
			setRenaming(false);
			return;
		}
		const r = await sendPrompt(sessionKey, `/name ${name}`);
		if (r.ok) {
			setShownTitle(name);
			setRenaming(false);
			setRenameError("");
		} else {
			setRenameError(r.error === "notdriven" ? "No driver runs this session." : "Rename failed.");
		}
	};

	return (
		<>
			<button
				type="button"
				aria-label="Close preview"
				onClick={onClose}
				className="fixed inset-0 z-40 cursor-default bg-black/60 backdrop-blur-xs"
			/>
			<div
				role="dialog"
				aria-modal="true"
				aria-label={`Preview of ${shownTitle}`}
				className="fixed inset-x-4 top-1/2 z-50 mx-auto max-w-md -translate-y-1/2 overflow-hidden rounded-2xl border border-base-content/10 bg-base-200 shadow-surface-lg"
			>
				<header className="flex items-center gap-2 border-b border-base-content/8 px-4 py-3">
					{renaming ? (
						<form
							className="flex min-w-0 flex-1 items-center gap-1"
							onSubmit={(e) => {
								e.preventDefault();
								runRename();
							}}
						>
							<input
								value={renameVal}
								onChange={(e) => setRenameVal(e.target.value)}
								maxLength={120}
								aria-label="Session name"
								className="min-w-0 flex-1 rounded-lg border border-base-content/15 bg-base-100 px-2.5 py-1.5 text-[14px] text-base-content outline-none focus:border-primary"
							/>
							<button
								type="submit"
								aria-label="Save name"
								disabled={!renameVal.trim() || isWaiting(status)}
								className="press-scale grid h-8 w-8 shrink-0 place-items-center rounded-lg text-primary disabled:opacity-40"
							>
								<FaCheck size={16} />
							</button>
							<button
								type="button"
								aria-label="Cancel rename"
								onClick={() => setRenaming(false)}
								className="press-scale grid h-8 w-8 shrink-0 place-items-center rounded-lg text-muted active:text-base-content"
							>
								<FaXmark size={16} />
							</button>
						</form>
					) : (
						<>
							<span className="min-w-0 flex-1 truncate text-[15px] font-semibold tracking-tight text-base-content">
								{shownTitle}
							</span>
							{state.s === "ok" && (
								<PreviewChip status={status} awaiting={state.preview.awaiting} />
							)}
							{canRename(status) && (
								<button
									type="button"
									aria-label="Rename session"
									onClick={() => {
										setRenameVal(shownTitle);
										setRenameError("");
										setRenaming(true);
									}}
									className="press-scale grid h-8 w-8 shrink-0 place-items-center rounded-lg text-muted active:text-base-content"
								>
									<FaPencilAlt size={15} />
								</button>
							)}
						</>
					)}
					{!renaming && (
						<button
							type="button"
							aria-label="Close preview"
							onClick={onClose}
							className="press-scale grid h-8 w-8 shrink-0 place-items-center rounded-lg text-muted active:text-base-content"
						>
							<FaXmark size={16} />
						</button>
					)}
				</header>
				{renameError && (
					<p className="border-b border-base-content/8 px-4 py-1.5 text-[12px] text-error">
						{renameError}
					</p>
				)}
				<div ref={bodyRef} className="max-h-[50dvh] overflow-y-auto px-4 py-3">
					{state.s === "loading" && (
						<div className="flex items-center gap-2 py-6 text-sm text-muted">
							<Spinner size={16} className="text-primary" />
							Reading…
						</div>
					)}
					{state.s === "error" && <p className="py-6 text-sm text-error">{state.message}</p>}
					{state.s === "ok" && <PreviewLines lines={state.preview.lines} />}
				</div>
			</div>
		</>
	);
}
