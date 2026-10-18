import { useEffect, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { FaCaretDown, FaCaretRight, FaCheck, FaCopy } from "./fa";

/** Assistant text renders as markdown (GFM). Raw HTML never renders:
 * there is no rehype-raw, so tags arrive as escaped plain text. User
 * bubbles and thinking stay plain text by design. */

const SAFE_HREF = /^(https?:|mailto:)/i;

const FOLD_LINES = 12;

function textOf(child: unknown): { text: string; lang?: string } {
	const el = child as { props?: { className?: string; children?: unknown } } | undefined;
	const cls = el?.props?.className ?? "";
	const lang = /language-([\w-]+)/.exec(cls)?.[1];
	const raw = el?.props?.children;
	if (typeof raw === "string") {
		return { text: raw, lang };
	}
	return { text: nodeText(raw), lang };
}

function nodeText(node: unknown): string {
	if (typeof node === "string") {
		return node;
	}
	if (typeof node === "number") {
		return String(node);
	}
	if (Array.isArray(node)) {
		return node.map(nodeText).join("");
	}
	const el = node as { props?: { children?: unknown } } | undefined;
	return el?.props ? nodeText(el.props.children) : "";
}

function CopyGlyph() {
	return <FaCopy size={14} />;
}

function CheckGlyph() {
	return <FaCheck size={14} />;
}

function CopyButton({ text, className }: { text: string; className: string }) {
	const [state, setState] = useState("idle");
	const copy = () => {
		navigator.clipboard.writeText(text).then(
			() => setState("done"),
			() => setState("failed"),
		);
		setTimeout(() => setState("idle"), 1500);
	};
	return (
		<button
			type="button"
			onClick={copy}
			aria-label={state === "done" ? "Copied" : "Copy code"}
			className={`flex h-9 w-9 items-center justify-center rounded-md bg-base-200/90 text-muted shadow active:scale-95 ${className}`}
		>
			{state === "done" ? (
				<CheckGlyph />
			) : state === "failed" ? (
				<span className="text-[10px]">err</span>
			) : (
				<CopyGlyph />
			)}
		</button>
	);
}

// Mobile text selection is miserable, so inline code spans copy on tap
// with a fixed pill for feedback.
function InlineCode(props: React.ComponentPropsWithoutRef<"code"> & { node?: unknown }) {
	const [copied, setCopied] = useState(false);
	// react-markdown hands every component a node prop; leaking it into
	// the DOM pollutes the rendered code element.
	const { node: _node, children, ...rest } = props;
	const text = nodeText(children);
	if (!text) {
		return <code {...rest}>{children}</code>;
	}
	const copy = () => {
		navigator.clipboard.writeText(text).then(() => {
			setCopied(true);
			setTimeout(() => setCopied(false), 1500);
		});
	};
	return (
		<>
			<button
				type="button"
				onClick={copy}
				title="Tap to copy"
				className="m-0 inline-block max-w-full cursor-pointer border-0 bg-transparent p-0 text-left text-inherit"
			>
				<code {...rest}>{children}</code>
			</button>
			{copied && (
				<span className="fixed inset-x-0 bottom-28 z-50 mx-auto w-fit rounded-full bg-base-300 px-3 py-1 text-[12px] text-base-content shadow-lg">
					Copied
				</span>
			)}
		</>
	);
}

function CodeFold({ text, lang }: { text: string; lang?: string }) {
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

	// The collapsed branch re-renders the code element by hand instead
	// of passing react-markdown's node through: a components.code entry
	// would otherwise wrap fenced code in the tap-to-copy span too.
	const body =
		open && html !== null ? (
			<pre>
				{/* biome-ignore lint/security/noDangerouslySetInnerHtml: highlight.js output is escaped code, never raw HTML */}
				<code className="hljs" dangerouslySetInnerHTML={{ __html: html }} />
			</pre>
		) : (
			<pre>
				<code className={lang ? `language-${lang}` : undefined}>{text}</code>
			</pre>
		);

	if (lines <= FOLD_LINES) {
		return (
			<div className="relative">
				<CopyButton text={text} className="absolute right-1.5 top-1.5 z-10" />
				{body}
			</div>
		);
	}
	return (
		<div>
			<div className="flex w-full items-center gap-1 rounded-t-lg bg-base-300/60 text-left font-mono text-[11px] text-muted">
				<button
					type="button"
					onClick={() => setOpen(!open)}
					aria-expanded={open}
					className="flex flex-1 items-center gap-1 px-2 py-1"
				>
					<span>{open ? <FaCaretDown size={12} /> : <FaCaretRight size={12} />}</span>
					<span>
						{lang || "code"} · {lines} lines
					</span>
				</button>
				<CopyButton text={text} className="mr-1 shrink-0" />
			</div>
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
		const { text: code, lang } = textOf(child);
		if (!code) {
			return <pre>{props.children}</pre>;
		}
		return <CodeFold text={code} lang={lang} />;
	},
	code: InlineCode,
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
