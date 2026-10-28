import { useEffect, useState } from "react";
import { getSessionPreview, type SessionPreview } from "../api";
import { useSheetBack } from "../nav";
import { FaXmark } from "./fa";
import { PreviewChip, PreviewLines } from "./previewLines";
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
				aria-label={`Preview of ${title}`}
				className="fixed inset-x-4 top-1/2 z-50 mx-auto max-w-md -translate-y-1/2 overflow-hidden rounded-2xl border border-base-content/10 bg-base-200 shadow-surface-lg"
			>
				<header className="flex items-center gap-2 border-b border-base-content/8 px-4 py-3">
					<span className="min-w-0 flex-1 truncate text-[15px] font-semibold tracking-tight text-base-content">
						{title}
					</span>
					{state.s === "ok" && (
						<PreviewChip status={state.preview.status} awaiting={state.preview.awaiting} />
					)}
					<button
						type="button"
						aria-label="Close preview"
						onClick={onClose}
						className="press-scale grid h-8 w-8 shrink-0 place-items-center rounded-lg text-muted active:text-base-content"
					>
						<FaXmark size={16} />
					</button>
				</header>
				<div className="max-h-[50dvh] overflow-y-auto px-4 py-3">
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
