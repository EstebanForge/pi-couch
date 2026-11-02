// The thinking clamp, mirrored from @earendil-works/pi-ai's
// clampThinkingLevel walk; the supported levels themselves come from
// the server, which runs pi's code to dump them per model.
// The fallback to the full ladder when a model has no catalog data
// is deliberate: the picker must never narrow to nothing because a
// dump failed.

export const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"];

// Missing catalog data means "show everything": the picker must never
// narrow to nothing because a dump failed.
export const levelsForModel = (levels?: string[]): string[] =>
	levels && levels.length > 0 ? levels : THINKING_LEVELS;

// Nearest supported level at or above the request, then below, then
// the first supported — pi's clampThinkingLevel walk.
export const clampThinking = (levels: string[], level: string): string => {
	const supported = levelsForModel(levels);
	if (supported.includes(level)) return level;
	const requested = THINKING_LEVELS.indexOf(level);
	if (requested === -1) return supported[0] ?? "off";
	for (let i = requested; i < THINKING_LEVELS.length; i++) {
		if (supported.includes(THINKING_LEVELS[i])) return THINKING_LEVELS[i];
	}
	for (let i = requested - 1; i >= 0; i--) {
		if (supported.includes(THINKING_LEVELS[i])) return THINKING_LEVELS[i];
	}
	return supported[0] ?? "off";
};
