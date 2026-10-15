// Per-device UI prefs, localStorage-backed. The settings screen writes
// them; the composer reads them at mount (screens remount on navigate,
// so no live sync is needed).

export type EnterSendsPref = "auto" | "send" | "newline";

const ENTER_KEY = "couch-pi.enterSends";
const KEYS_KEY = "couch-pi.quickKeys";

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
