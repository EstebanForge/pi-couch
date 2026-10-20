import { useEffect, useRef, useSyncExternalStore } from "react";
import { flushSync } from "react-dom";

// Tiny path router: real URLs, push for forward, replace for sideways hops,
// popstate for the Back gesture. BASE tracks Vite's base ('/' since cutover).
//
// Direction comes from a monotonic index stored in history.state: push
// stores the next index, replace keeps the current one, and popstate
// compares the entry it lands on against the one we left. That makes the
// Back/Forward slide work for the browser buttons too, not just in-app
// navigation.

export const BASE = import.meta.env.BASE_URL;

const listeners = new Set<() => void>();

function notify(): void {
	for (const l of listeners) l();
}

export function subscribe(cb: () => void): () => void {
	listeners.add(cb);
	return () => {
		listeners.delete(cb);
	};
}

export function stripBase(p: string): string {
	if (p === BASE || p === BASE.replace(/\/$/, "")) return "/";
	if (p.startsWith(BASE)) return p.slice(BASE.length - 1);
	return p;
}

export function getPath(): string {
	return stripBase(window.location.pathname);
}

export function usePath(): string {
	return useSyncExternalStore(subscribe, getPath);
}

type Dir = "fwd" | "back" | "side";

type DocVT = Document & {
	startViewTransition?: (cb: () => void) => void;
};

function reducedMotion(): boolean {
	return (
		typeof matchMedia === "function" &&
		!matchMedia("(prefers-reduced-motion: no-preference)").matches
	);
}

function transition(nav: Dir, apply: () => void): void {
	const doc = document as DocVT;
	if (!doc.startViewTransition || reducedMotion()) {
		apply();
		return;
	}
	document.documentElement.dataset.nav = nav;
	doc.startViewTransition(() => flushSync(apply));
}

function href(to: string): string {
	return BASE.replace(/\/$/, "") + (to.startsWith("/") ? to : `/${to}`);
}

// cur tracks the index of the entry we are on.
let cur = 0;

let seq = 0;

// Seed the history stack on a cold deep link: [deep, home, deep] with
// the deep path current, so the first Back lands on the home list
// instead of leaving the app. Only on a fresh load (no state yet) and
// only when the boot path is not the root.
if (history.state === null) {
	const deep = window.location.pathname + window.location.search + window.location.hash;
	if (deep !== BASE && deep !== BASE.replace(/\/$/, "")) {
		history.replaceState({ i: 1 }, "", deep);
		history.pushState({ i: 0 }, "", BASE);
		history.pushState({ i: 1 }, "", deep);
		cur = 1;
		seq = 1;
	} else {
		history.replaceState({ i: 0 }, "");
	}
}

export function navigate(to: string, opts: { replace?: boolean; dir?: Dir } = {}): void {
	transition(opts.dir ?? "fwd", () => {
		// A sheet marker still on top must be stripped HERE: the sheet's
		// own cleanup runs only on the next render, which is after this
		// push when close and navigate share one callback — leaving the
		// marker as a dead history entry every Back gesture then eats
		// with no visible effect (field bug).
		if (onMarker) {
			markerStack.pop();
			onMarker = false;
			history.replaceState({ i: cur }, "", href(getPath()));
		}
		if (opts.replace) history.replaceState({ i: cur }, "", href(to));
		else history.pushState({ i: ++seq }, "", href(to));
		cur = typeof history.state?.i === "number" ? history.state.i : cur;
		onMarker = false;
		notify();
	});
}

// Sheets and overlays push one marker history entry, so the Android
// back gesture closes them instead of leaving the screen. Marker
// pops never reach the router: closing a sheet is component state,
// not navigation. The stack handles nesting; ids tell an effect
// whether its own marker was already popped by the gesture.
let markerSeq = 0;
const markerStack: number[] = [];
const poppedMarkers = new Set<number>();
const sheetClosers = new Set<() => void>();
let onMarker = false;

export function useSheetBack(opened: boolean, close: () => void): void {
	const ref = useRef(close);
	ref.current = close;
	useEffect(() => {
		if (!opened) return;
		const id = ++markerSeq;
		markerStack.push(id);
		history.pushState({ i: cur, sheet: id }, "", href(getPath()));
		onMarker = true;
		const closer = () => ref.current();
		sheetClosers.add(closer);
		return () => {
			sheetClosers.delete(closer);
			// The gesture already popped the marker. A programmatic close
			// (backdrop, confirm) strips the marker in place — replaceState,
			// never history.back(): a back here is async and would land
			// after a sibling sheet's push in the same commit, popping the
			// wrong marker. A navigation above the marker leaves it stale;
			// it dies as a no-op on a later Back.
			if (poppedMarkers.delete(id)) return;
			if (onMarker && history.state?.sheet === id) {
				markerStack.pop();
				history.replaceState({ i: cur }, "", href(getPath()));
				onMarker = false;
			}
		};
	}, [opened]);
}

window.addEventListener("popstate", (e) => {
	if (onMarker) {
		const id = markerStack.pop();
		if (typeof id === "number") poppedMarkers.add(id);
		for (const close of sheetClosers) close();
		onMarker = e.state?.sheet !== undefined;
		return;
	}
	if (e.state?.sheet !== undefined) {
		// Landed on a marker nobody owns: its sheet navigated away and
		// the entry survived only as history residue. Keep going back
		// instead of spending a gesture on it.
		if (sheetClosers.size === 0) {
			history.back();
			return;
		}
		onMarker = true;
		return;
	}
	const next = typeof e.state?.i === "number" ? e.state.i : cur;
	const dir: Dir = next < cur ? "back" : "fwd";
	cur = next;
	transition(dir, notify);
});
