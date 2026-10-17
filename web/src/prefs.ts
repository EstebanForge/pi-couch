// Per-device UI prefs, localStorage-backed. The settings screen writes
// them; the composer reads them at mount (screens remount on navigate,
// so no live sync is needed).

export type EnterSendsPref = "auto" | "send" | "newline";

const ENTER_KEY = "couch-pi.enterSends";
const KEYS_KEY = "couch-pi.quickKeys";
// v2: the 2026-10 default dropped enter and reordered the strip; a
// stored v1 order would otherwise mask the new default forever.
const ORDER_KEY = "couch-pi.keyOrder.v2";
const QUICK_PATH_KEY = "couch-pi.quickPath";

function store(): Storage | null {
	return typeof localStorage === "undefined" ? null : localStorage;
}

export function enterSendsPref(): EnterSendsPref {
	const v = store()?.getItem(ENTER_KEY);
	return v === "send" || v === "newline" ? v : "auto";
}

export function setEnterSendsPref(v: EnterSendsPref): void {
	if (v === "auto") {
		store()?.removeItem(ENTER_KEY);
		return;
	}
	store()?.setItem(ENTER_KEY, v);
}

export function quickKeysEnabled(): boolean {
	return store()?.getItem(KEYS_KEY) !== "0";
}

export function setQuickKeysEnabled(on: boolean): void {
	if (on) {
		store()?.removeItem(KEYS_KEY);
		return;
	}
	store()?.setItem(KEYS_KEY, "0");
}

/** The stored quick-key id order, or null for the default. Garbage
 * reads as null; non-strings inside the array are dropped. */
export function quickKeyOrder(): string[] | null {
	const raw = store()?.getItem(ORDER_KEY);
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
	store()?.setItem(ORDER_KEY, JSON.stringify(ids));
}

/** Quick agent: the fixed folder the home FAB's speed dial starts an
 * agent in (pi runs there with no prompt). Empty until configured in
 * Settings. */
export function quickPath(): string {
	return store()?.getItem(QUICK_PATH_KEY) ?? "";
}

export function setQuickPath(path: string): void {
	if (path.trim()) {
		store()?.setItem(QUICK_PATH_KEY, path.trim());
		return;
	}
	store()?.removeItem(QUICK_PATH_KEY);
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
