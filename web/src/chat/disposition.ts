// Submit disposition, the composer-side contract: an intent travels
// with every send so the wire cannot guess. Mirrors pi-durable's inbox
// modes reduced to what herdr carries (docs/durable-evaluation.md):
// an idle or blocked session takes the text now; a working one takes
// it as a follow-up. Steering is pi's queue mode alone (ADR-0001).
export type SendDisposition = "now" | "followUp";

export const dispositionFor = (status: string): SendDisposition =>
	status === "working" ? "followUp" : "now";
