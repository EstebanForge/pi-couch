import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Markdown } from "./Markdown";

describe("Markdown", () => {
	it("renders gfm markup", () => {
		const html = renderToString(
			<Markdown
				text={"# Head\n\n**bold** and `code`\n\n- a\n- b\n\n| x | y |\n|---|---|\n| 1 | 2 |"}
			/>,
		);
		expect(html).toContain("<h1>");
		expect(html).toContain("<strong>bold</strong>");
		expect(html).toContain("<code>code</code>");
		expect(html).toContain("<ul>");
		expect(html).toContain("<table>");
	});

	it("renders raw html as text, never as markup", () => {
		const html = renderToString(
			<Markdown text={'hello <script>alert(1)</script> <img src="x">'} />,
		);
		expect(html).not.toContain("<script");
		expect(html).not.toContain("<img");
		expect(html).toContain("&lt;script&gt;");
	});

	it("opens links in a new tab with noopener", () => {
		const html = renderToString(<Markdown text="[site](https://example.com)" />);
		expect(html).toContain('target="_blank"');
		expect(html).toContain('rel="noopener noreferrer"');
	});
});

it("neutralizes non-http link schemes", () => {
	const html = renderToString(
		<Markdown
			text={"[x](javascript:alert(1))\n\n[y](data:text/html,hi)\n\n[z](file:///etc/passwd)"}
		/>,
	);
	expect(html).not.toContain('<a href="javascript:');
	expect(html).not.toContain('<a href="data:');
	expect(html).not.toContain('<a href="file:');
	// The link text stays visible as plain content.
	expect(html).toContain('<span class="text-muted">x</span>');
	expect(html).toContain('<span class="text-muted">z</span>');
});

it("keeps mailto links clickable", () => {
	const html = renderToString(<Markdown text="[me](mailto:a@b.c)" />);
	expect(html).toContain('href="mailto:a@b.c"');
	expect(html).toContain('target="_blank"');
});

it("folds long code fences behind a header", () => {
	const long = Array.from({ length: 15 }, (_, i) => `line ${i}`).join("\n");
	const fence = "```";
	const html = renderToString(<Markdown text={`${fence}js\n${long}\n${fence}`} />);
	const out = html.replace(/<!--.*?-->/g, "");
	expect(out).toContain("js · 16 lines"); // fence + trailing newline
	expect(out).not.toContain("line 14"); // body stays collapsed
});

it("renders short fences open with no fold chrome", () => {
	const html = renderToString(<Markdown text={"```js\nconst a = 1;\n```"} />);
	expect(html).toContain("<pre>");
	expect(html).not.toContain("· 2 lines");
});
