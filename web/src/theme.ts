// Color schemes. Each carries the Tailwind token set (the same
// --color-* custom properties index.css's @theme pins for first paint)
// plus an xterm palette for the terminal screen. Adding a scheme later
// means appending one entry here; the Settings picker renders SCHEMES.

export type SchemeId = "cobalt-next-dark" | "dracula";

export type TermPalette = {
	background: string;
	foreground: string;
	cursor: string;
	cursorAccent: string;
	selectionBackground: string;
	black: string;
	red: string;
	green: string;
	yellow: string;
	blue: string;
	magenta: string;
	cyan: string;
	white: string;
	brightBlack: string;
	brightRed: string;
	brightGreen: string;
	brightYellow: string;
	brightBlue: string;
	brightMagenta: string;
	brightCyan: string;
	brightWhite: string;
};

export type Scheme = {
	id: SchemeId;
	label: string;
	vars: Record<string, string>;
	term: TermPalette;
};

const COBALT: Scheme = {
	id: "cobalt-next-dark",
	label: "Cobalt Next Dark",
	vars: {
		"--color-base-100": "#0b1c24",
		"--color-base-200": "#11242e",
		"--color-base-300": "#071219",
		"--color-base-content": "#d7deea",
		"--color-muted": "#62747f",
		"--color-primary": "#409dd4",
		"--color-primary-content": "#071219",
		"--color-secondary": "#cba3c7",
		"--color-accent": "#37b5b4",
		"--color-success": "#8cc98f",
		"--color-warning": "#ffc64c",
		"--color-error": "#f94967",
	},
	term: {
		background: "#0b1c24",
		foreground: "#d7deea",
		cursor: "#ffc64c",
		cursorAccent: "#0b1c24",
		selectionBackground: "#262f37",
		black: "#262f37",
		red: "#f94967",
		green: "#8cc98f",
		yellow: "#ffc64c",
		blue: "#409dd4",
		magenta: "#cba3c7",
		cyan: "#37b5b4",
		white: "#d7deea",
		brightBlack: "#62747f",
		brightRed: "#e47e8b",
		brightGreen: "#baddbb",
		brightYellow: "#ffdc91",
		brightBlue: "#7ac0eb",
		brightMagenta: "#f3ccef",
		brightCyan: "#84e4e3",
		brightWhite: "#ffffff",
	},
};

const DRACULA: Scheme = {
	id: "dracula",
	label: "Dracula",
	vars: {
		"--color-base-100": "#282a36",
		"--color-base-200": "#21222c",
		"--color-base-300": "#191a21",
		"--color-base-content": "#f8f8f2",
		"--color-muted": "#6272a4",
		"--color-primary": "#bd93f9",
		"--color-primary-content": "#191a21",
		"--color-secondary": "#ff79c6",
		"--color-accent": "#8be9fd",
		"--color-success": "#50fa7b",
		"--color-warning": "#f1fa8c",
		"--color-error": "#ff5555",
	},
	term: {
		background: "#282a36",
		foreground: "#f8f8f2",
		cursor: "#f8f8f2",
		cursorAccent: "#282a36",
		selectionBackground: "#44475a",
		black: "#21222c",
		red: "#ff5555",
		green: "#50fa7b",
		yellow: "#f1fa8c",
		blue: "#bd93f9",
		magenta: "#ff79c6",
		cyan: "#8be9fd",
		white: "#f8f8f2",
		brightBlack: "#6272a4",
		brightRed: "#ff6e6e",
		brightGreen: "#69ff94",
		brightYellow: "#ffffa5",
		brightBlue: "#d6acff",
		brightMagenta: "#ff92df",
		brightCyan: "#a4ffff",
		brightWhite: "#ffffff",
	},
};

export const SCHEMES: Scheme[] = [COBALT, DRACULA];

export const DEFAULT_SCHEME: SchemeId = "cobalt-next-dark";

import { prefKey, readPref, writePref } from "./prefs";

export function schemeById(id: string): Scheme {
	return SCHEMES.find((s) => s.id === id) ?? SCHEMES[0];
}

/** The active scheme: the stored choice when valid, else the default.
 * The default also lives in index.css's @theme, so first paint is
 * already themed before this runs. */
export function activeScheme(): Scheme {
	return schemeById(readPref(prefKey("theme")) ?? DEFAULT_SCHEME);
}

export function setScheme(id: SchemeId): void {
	writePref(prefKey("theme"), id);
	applyScheme(schemeById(id));
	for (const fn of listeners) fn();
}

/** Paints one scheme onto the document element. Inline custom
 * properties beat the @theme stylesheet values. */
export function applyScheme(s: Scheme): void {
	if (typeof document === "undefined") return;
	for (const [k, v] of Object.entries(s.vars)) {
		document.documentElement.style.setProperty(k, v);
	}
	// the browser paints the status bar / address bar from this meta
	document
		.querySelector('meta[name="theme-color"]')
		?.setAttribute("content", s.vars["--color-base-300"]);
}

const listeners = new Set<() => void>();

/** Live re-theme for surfaces that hold colors outside CSS (xterm). */
export function onSchemeChange(fn: () => void): () => void {
	listeners.add(fn);
	return () => listeners.delete(fn);
}
