import { useSyncExternalStore } from "react";

// Tiny path router: real URLs, push for forward, replace for sideways hops,
// popstate for the Back gesture. BASE is '/app/' until cutover.

export const BASE = import.meta.env.BASE_URL;

const listeners = new Set<() => void>();

export function subscribe(cb: () => void): () => void {
	listeners.add(cb);
	window.addEventListener("popstate", cb);
	return () => {
		listeners.delete(cb);
		window.removeEventListener("popstate", cb);
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

function href(to: string): string {
	return BASE.replace(/\/$/, "") + (to.startsWith("/") ? to : `/${to}`);
}

export function navigate(to: string, opts: { replace?: boolean; dir?: Dir } = {}): void {
	const apply = () => {
		if (opts.replace) history.replaceState({}, "", href(to));
		else history.pushState({}, "", href(to));
		for (const l of listeners) l();
	};
	const doc = document as Document & {
		startViewTransition?: (cb: () => void) => void;
	};
	const reduce =
		typeof matchMedia === "function" &&
		!matchMedia("(prefers-reduced-motion: no-preference)").matches;
	if (!doc.startViewTransition || reduce) {
		apply();
		return;
	}
	document.documentElement.dataset.nav = opts.dir ?? "fwd";
	doc.startViewTransition(apply);
}
