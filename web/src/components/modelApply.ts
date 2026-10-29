import { isWaiting } from "../status";

export type ApplyOutcome = "confirmed" | "clear" | "clear-and-dismiss";

/** Decides what an unconfirmed /model or /thinking apply does when its
 *  8 s stall timer fires. pi queues these commands while the agent
 *  works, so Esc there would abort a running turn; only an idle agent
 *  can have picker residue on screen to dismiss. */
export function stalledApply<T>(current: T | null, applied: T, status: string): ApplyOutcome {
	if (current !== applied) return "confirmed";
	return isWaiting(status) ? "clear" : "clear-and-dismiss";
}
