// Shared driver-status vocabulary: one dot map and one wait predicate so
// every screen agrees on what "waiting" means.

export const STATUS_DOT: Record<string, string> = {
	working: "bg-warning",
	blocked: "bg-error",
	done: "bg-success",
	idle: "bg-muted",
	unknown: "bg-muted",
};

// Waiting = the agent is mid-turn (working) or paused for our input
// (blocked). Both get the spinner, not a static dot.
export const isWaiting = (status: string): boolean => status === "working" || status === "blocked";
