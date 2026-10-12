// One entry per key cap in the strip above the composer. Transport is
// the terminal input endpoint; sends ride one promise chain per session
// so taps stay ordered (herdr holds a lone Esc ~150ms on the pty).

export type QuickKey = {
	id: string;
	label: string;
	// keys go to the TUI; queue is the Alt+Enter flow (draft first, then
	// the key, per the spike); close opens the confirm dialog instead of
	// sending a keystroke (Ctrl+D would strand the workspace).
	action: { kind: "keys"; keys: string[] } | { kind: "queue" } | { kind: "close" };
	enabled: (status: string, hasText: boolean) => boolean;
};

const hasDriver = (status: string) => status !== "";

export const QUICK_KEYS: QuickKey[] = [
	{
		id: "esc",
		label: "Esc",
		action: { kind: "keys", keys: ["esc"] },
		enabled: (status) => status === "working" || status === "blocked",
	},
	{ id: "enter", label: "⏎", action: { kind: "keys", keys: ["enter"] }, enabled: hasDriver },
	{ id: "left", label: "←", action: { kind: "keys", keys: ["left"] }, enabled: hasDriver },
	{ id: "up", label: "↑", action: { kind: "keys", keys: ["up"] }, enabled: hasDriver },
	{ id: "down", label: "↓", action: { kind: "keys", keys: ["down"] }, enabled: hasDriver },
	{ id: "right", label: "→", action: { kind: "keys", keys: ["right"] }, enabled: hasDriver },
	{
		id: "altenter",
		label: "Alt+⏎",
		action: { kind: "queue" },
		enabled: (status, hasText) => status === "working" && hasText,
	},
	{ id: "ctrld", label: "^D", action: { kind: "close" }, enabled: hasDriver },
];

// After an Esc, wait before the next key: herdr's lone-Esc finding.
export const ESC_SETTLE_MS = 150;
