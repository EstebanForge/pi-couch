import type { PreviewLine } from "../api";
import { STATUS_DOT } from "../status";

// A rename via /name needs a live pi process; the preview status is
// "unknown" exactly when no herdr driver runs the session.
export const canRename = (status: string): boolean => status !== "unknown";

// While pi is generating, /name must ride the follow-up queue: sent
// plain it would be steered into the model as text, not run as a
// command. undefined sends immediately.
export const renameQueueMode = (status: string): "followUp" | undefined =>
	status === "working" ? "followUp" : undefined;

// Blocked is the one hard refusal: pi is holding a dialog, and typed
// text would land in the dialog, not in the name.
export const renameHint = (status: string): string | null =>
	status === "blocked" ? "pi is waiting on you - answer it first, then rename." : null;

// Status chip with the same vocabulary and colors as the home card:
// herdr status dot, yellow for waiting-for-you.
export function PreviewChip({ status, awaiting }: { status: string; awaiting: boolean }) {
	return (
		<span
			className={`inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${
				awaiting ? "bg-warning/15 text-warning" : "bg-base-content/5 text-muted"
			}`}
		>
			<span
				className={`h-2 w-2 rounded-full ${awaiting ? "bg-warning" : (STATUS_DOT[status] ?? "bg-muted")}`}
			/>
			{awaiting ? "waiting for you" : status}
		</span>
	);
}

// The frozen tail: speaker labels over one-line takes, newest last.
// The server caps each line, so the client renders as-is.
export function PreviewLines({ lines }: { lines: PreviewLine[] }) {
	if (lines.length === 0) {
		return <p className="py-6 text-sm text-muted">Nothing said yet.</p>;
	}
	return (
		<ol className="flex flex-col gap-3">
			{lines.map((l) => (
				<li key={l.seq} className="flex flex-col gap-0.5">
					<span
						className={`text-[11px] font-semibold tracking-wider uppercase ${
							l.who === "user" ? "text-primary" : "text-muted"
						}`}
					>
						{l.who === "user" ? "you" : "pi"}
					</span>
					<p className="line-clamp-3 text-[13.5px] leading-snug whitespace-pre-wrap text-base-content">
						{l.text}
					</p>
				</li>
			))}
		</ol>
	);
}
