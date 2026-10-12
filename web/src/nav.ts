import { useSyncExternalStore } from "react";
import { flushSync } from "react-dom";

// Tiny path router: real URLs, push for forward, replace for sideways hops,
// popstate for the Back gesture. BASE is '/app/' until cutover.
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

// cur tracks the index of the entry we are on; seed the boot entry so
// popstate always finds an index, even for deep links.
let cur = 0;
history.replaceState({ i: 0 }, "");

let seq = 0;

export function navigate(to: string, opts: { replace?: boolean; dir?: Dir } = {}): void {
	transition(opts.dir ?? "fwd", () => {
		if (opts.replace) history.replaceState({ i: cur }, "", href(to));
		else history.pushState({ i: ++seq }, "", href(to));
		cur = typeof history.state?.i === "number" ? history.state.i : cur;
		notify();
	});
}

window.addEventListener("popstate", (e) => {
	const next = typeof e.state?.i === "number" ? e.state.i : cur;
	const dir: Dir = next < cur ? "back" : "fwd";
	cur = next;
	transition(dir, notify);
});
