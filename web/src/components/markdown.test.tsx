import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Markdown } from "./Markdown";

describe("Markdown", () => {
	it("renders gfm markup", () => {
		const html = renderToString(
			<Markdown text={"# Head\n\n**bold** and `code`\n\n- a\n- b\n\n| x | y |\n|---|---|\n| 1 | 2 |"} />,
		);
		expect(html).toContain("<h1>");
		expect(html).toContain("<strong>bold</strong>");
		expect(html).toContain("<code>code</code>");
		expect(html).toContain("<ul>");
		expect(html).toContain("<table>");
	});

	it("renders raw html as text, never as markup", () => {
		const html = renderToString(<Markdown text={'hello <script>alert(1)</script> <img src="x">'} />);
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
