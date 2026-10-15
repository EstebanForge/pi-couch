// Lazy highlight boundary. Markdown.tsx imports this module
// dynamically, so highlight.js (common bundle) and its theme CSS land
// in their own chunk fetched only when a fence first expands.

import "highlight.js/styles/github-dark.css";

// Fences above this many characters stay plain: highlighting cost and
// output size grow with the code, and the scroll container caps the
// damage either way.
const MAX_CHARS = 20000;

export async function highlight(text: string, lang?: string): Promise<string | null> {
	if (text.length > MAX_CHARS) {
		return null;
	}
	const hljs = (await import("highlight.js/lib/common")).default;
	if (lang && hljs.getLanguage(lang)) {
		return hljs.highlight(text, { language: lang, ignoreIllegals: true }).value;
	}
	return null;
}
