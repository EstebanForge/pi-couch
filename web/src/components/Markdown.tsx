import { type ReactNode, useEffect, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

/** Assistant text renders as markdown (GFM). Raw HTML never renders:
 * there is no rehype-raw, so tags arrive as escaped plain text. User
 * bubbles and thinking stay plain text by design. */

const SAFE_HREF = /^(https?:|mailto:)/i;

const FOLD_LINES = 12;

function textOf(child: unknown): { text: string; lang?: string; node: ReactNode } {
	const el = child as { props?: { className?: string; children?: unknown } } | undefined;
	const cls = el?.props?.className ?? "";
	const lang = /language-([\w-]+)/.exec(cls)?.[1];
	const raw = el?.props?.children;
	if (typeof raw === "string") {
		return { text: raw, lang, node: child as ReactNode };
	}
	return { text: "", lang, node: child as ReactNode };
}

function CodeFold({ text, lang, node }: { text: string; lang?: string; node: ReactNode }) {
	const lines = text ? text.split("\n").length : 0;
	const [open, setOpen] = useState(false);
	const [html, setHtml] = useState<string | null>(null);
	const [tried, setTried] = useState(false);

	useEffect(() => {
		if (!open || tried || !text) {
			return;
		}
		setTried(true);
		import("../chat/highlight")
			.then((m) => m.highlight(text, lang))
			.then((h) => setHtml(h))
			.catch(() => setHtml(null));
	}, [open, tried, text, lang]);

	const body =
		open && html !== null ? (
			<pre>
				{/* biome-ignore lint/security/noDangerouslySetInnerHtml: highlight.js output is escaped code, never raw HTML */}
				<code className="hljs" dangerouslySetInnerHTML={{ __html: html }} />
			</pre>
		) : (
			<pre>{node}</pre>
		);

	if (lines <= FOLD_LINES) {
		return body;
	}
	return (
		<div>
			<button
				type="button"
				onClick={() => setOpen(!open)}
				className="flex w-full items-center gap-1 rounded-t-lg bg-base-300/60 px-2 py-1 text-left font-mono text-[11px] text-muted"
			>
				<span>{open ? "▾" : "▸"}</span>
				<span>
					{lang || "code"} · {lines} lines
				</span>
			</button>
			{open ? body : null}
		</div>
	);
}

// Module-level so the identity is stable: a fresh components object
// per render would remount the whole markdown subtree (resetting fold
// state) and defeat react-markdown's memoization on every stream tick.
const COMPONENTS = {
	a: (props: React.ComponentPropsWithoutRef<"a">) => {
		const href = props.href ?? "";
		if (!SAFE_HREF.test(href)) {
			return <span className="text-muted">{props.children}</span>;
		}
		return <a {...props} href={href} target="_blank" rel="noopener noreferrer" />;
	},
	pre: (props: React.ComponentPropsWithoutRef<"pre">) => {
		const child = Array.isArray(props.children) ? props.children[0] : props.children;
		const { text: code, lang, node } = textOf(child);
		if (!code) {
			return <pre>{props.children}</pre>;
		}
		return <CodeFold text={code} lang={lang} node={node} />;
	},
};

export function Markdown({ text }: { text: string }) {
	return (
		<div className="md px-3 py-1 text-[15px] text-base-content">
			<ReactMarkdown remarkPlugins={[remarkGfm]} components={COMPONENTS}>
				{text}
			</ReactMarkdown>
		</div>
	);
}
