// session-tools.ts — one registry for every per-session tool. The header
// strip renders the strip placement; the overflow dialog renders the rest.
// Order by use, not by who was added first (plan rule).

export type ToolId = "changes" | "files" | "terminal" | "search" | "stop" | "close";

export type Tool = {
	id: ToolId;
	label: string;
	placement: "strip" | "overflow";
	// stage-gated tools are listed but disabled until their stage ships
	disabled?: boolean;
	// driver-gated tools depend on the herdr state
	enabled?: (status: string) => boolean;
	// tool screens hop sideways from the session screen
	href?: (key: string) => string;
};

export const TOOLS: Tool[] = [
	{ id: "changes", label: "Changes", placement: "strip", href: (k) => `/sessions/${k}/changes` },
	{ id: "files", label: "Files", placement: "strip", href: (k) => `/sessions/${k}/files` },
	{ id: "terminal", label: "Terminal", placement: "strip", disabled: true },
	{ id: "search", label: "Search", placement: "strip" },
	// Ctrl-C on an idle pi prompt can exit pi, so stop is working/blocked only.
	{
		id: "stop",
		label: "Stop",
		placement: "overflow",
		enabled: (s) => s === "working" || s === "blocked",
	},
	{ id: "close", label: "Close session", placement: "overflow" },
];

export const toolEnabled = (t: Tool, status: string): boolean =>
	!t.disabled && (!t.enabled || t.enabled(status));
