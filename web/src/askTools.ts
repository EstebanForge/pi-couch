// Peer-ask tool cards (pi-ask-claude / pi-ask-codex / pi-ask-antigravity
// and the pi-antigravity-bridge ask tool, which registers the same
// AskAntigravity name) render dark green so they stand out at a glance.

export const askToolNames = new Set(["AskAntigravity", "AskClaude", "AskCodex"]);

export function isAskTool(name: string): boolean {
	return askToolNames.has(name);
}

export function stepsAskClass(names: string[]): string {
	if (names.length === 0 || !names.every(isAskTool)) {
		return "border-base-content/6 bg-base-200/75";
	}
	return "border-emerald-600/50 bg-emerald-900/40";
}
