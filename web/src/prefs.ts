// Per-device UI prefs, localStorage-backed. The settings screen writes
// them; the composer reads them at mount (screens remount on navigate,
// so no live sync is needed).

export type EnterSendsPref = "auto" | "send" | "newline";

const LEGACY_PREFIX = "couch-pi.";
const PREFIX = "pi-couch.";

/** Storage key under the pi-couch prefix; dynamic per-session keys
 * ("pending.<key>", "draft.<key>") ride the same prefix. */
export function prefKey(name: string): string {
	return `${PREFIX}${name}`;
}

function store(): Storage | null {
	return typeof localStorage === "undefined" ? null : localStorage;
}

/** Read a pref; the first read of a renamed key promotes the legacy
 * couch-pi value (2026-10-21 rename) so device prefs survive. */
export function readPref(key: string): string | null {
	const s = store();
	if (!s) return null;
	try {
		const v = s.getItem(key);
		if (v !== null) return v;
		if (!key.startsWith(PREFIX)) return null;
		const legacyKey = `${LEGACY_PREFIX}${key.slice(PREFIX.length)}`;
		const legacy = s.getItem(legacyKey);
		if (legacy === null) return null;
		s.setItem(key, legacy);
		s.removeItem(legacyKey);
		return legacy;
	} catch {
		return null;
	}
}

export function writePref(key: string, value: string | null): void {
	const s = store();
	if (!s) return;
	try {
		if (value === null) {
			s.removeItem(key);
		} else {
			s.setItem(key, value);
		}
		// A write retires the legacy sibling either way, so a pref
		// cleared after the rename cannot resurrect from it.
		if (key.startsWith(PREFIX)) {
			s.removeItem(`${LEGACY_PREFIX}${key.slice(PREFIX.length)}`);
		}
	} catch {
		// storage unavailable (private mode): prefs degrade to defaults
	}
}

export function enterSendsPref(): EnterSendsPref {
	const v = readPref(prefKey("enterSends"));
	return v === "send" || v === "newline" ? v : "auto";
}

export function setEnterSendsPref(v: EnterSendsPref): void {
	writePref(prefKey("enterSends"), v === "auto" ? null : v);
}

export function quickKeysEnabled(): boolean {
	return readPref(prefKey("quickKeys")) !== "0";
}

export function setQuickKeysEnabled(on: boolean): void {
	writePref(prefKey("quickKeys"), on ? null : "0");
}

/** The stored quick-key id order, or null for the default. Garbage
 * reads as null; non-strings inside the array are dropped. */
export function quickKeyOrder(): string[] | null {
	// v2 key: the 2026-10 default reordered the strip and dropped the
	// close key; a stored v1 order would otherwise mask the new default
	// forever.
	const raw = readPref(prefKey("keyOrder.v2"));
	if (!raw) return null;
	try {
		const v: unknown = JSON.parse(raw);
		if (!Array.isArray(v)) return null;
		return v.filter((x): x is string => typeof x === "string");
	} catch {
		return null;
	}
}

export function setQuickKeyOrder(ids: string[]): void {
	writePref(prefKey("keyOrder.v2"), JSON.stringify(ids));
}

/** Quick agent: the fixed folder the home FAB's speed dial starts an
 * agent in (pi runs there with no prompt). Empty until configured in
 * Settings. */
export function quickPath(): string {
	return readPref(prefKey("quickPath")) ?? "";
}

export function setQuickPath(path: string): void {
	writePref(prefKey("quickPath"), path.trim() || null);
}

/** Quick commands: pi slash commands plus raw bash one-liners one tap
 * away in the quick actions panel. Per-device, junk-tolerant; the
 * defaults cover the safe built-ins devs use most (nothing that
 * strands the chat in a picker the phone cannot answer, like /model
 * or /resume) and one git catch-all. Slash fills the draft, bash
 * runs. */
export const DEFAULT_QUICK_COMMANDS = [
	"/compact",
	"/name",
	"/reload",
	"/session",
	'git add -A && (git commit -m "wip $(date +%FT%H:%M)" || true) && git push -u origin HEAD',
];

export function quickCommands(): string[] {
	const raw = readPref(prefKey("quickCommands"));
	if (!raw) return [...DEFAULT_QUICK_COMMANDS];
	try {
		const parsed: unknown = JSON.parse(raw);
		if (Array.isArray(parsed)) {
			const list = parsed.filter((c): c is string => typeof c === "string" && c.trim() !== "");
			if (list.length > 0) return list;
		}
	} catch {
		// fall through to the defaults
	}
	return [...DEFAULT_QUICK_COMMANDS];
}

export function setQuickCommands(list: string[]): void {
	const clean = list.map((c) => c.trim()).filter((c) => c !== "");
	writePref(prefKey("quickCommands"), clean.length === 0 ? null : JSON.stringify(clean));
}

/** Effective Enter behavior: the pref wins; "auto" falls back to the
 * fine-pointer heuristic (desktop keyboards send, touch adds a
 * newline). */
export function effectiveEnterSends(): boolean {
	const pref = enterSendsPref();
	if (pref === "send") {
		return true;
	}
	if (pref === "newline") {
		return false;
	}
	return typeof window !== "undefined" && !!window.matchMedia?.("(pointer: fine)")?.matches;
}
