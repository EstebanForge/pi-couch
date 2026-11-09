// Copy for the rewind outcome (ADR-0010), kept pure for tests.
export function rewindErrorCopy(error?: string, code?: string): string {
	if (error === "busy") return "pi is busy - wait for it to finish";
	if (error === "notdriven") return "No driver runs this session.";
	if (code === "timeout")
		return "the agent didn't confirm the rewind - check the terminal before retrying";
	if (code === "refused") return "pi refused the rewind";
	return "Rewind failed.";
}

// A user bubble offers the long-press rewind only when it is the last
// message the user sent and the session is idle and driven.
export function rewindOffer(
	items: { kind: string; seq: number }[],
	it: { kind: string; seq: number },
	state: { undriven?: boolean; resuming?: boolean; waiting?: boolean },
): boolean {
	if (it.kind !== "user" || state.undriven || state.resuming || state.waiting) return false;
	for (let i = items.length - 1; i >= 0; i--) {
		if (items[i].kind === "user") return items[i].seq === it.seq;
	}
	return false;
}
