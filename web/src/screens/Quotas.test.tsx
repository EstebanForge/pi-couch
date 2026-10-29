import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ProviderCard } from "./Quotas";

const errored = {
	id: "minimax",
	name: "MiniMax",
	plan: "Standard",
	updated_at: "2026-10-29T00:00:00Z",
	error: 'Get "https://www.minimax.io/v1/token_plan/remains": context deadline exceeded',
};

describe("ProviderCard", () => {
	it("shows an unavailable chip, not the raw error, in the header", () => {
		const html = renderToString(<ProviderCard provider={errored} />);
		expect(html).toContain("unavailable");
		// the raw reason must not ride in the title flex row, where a
		// long URL shoves it out of the card
		const headerEnd = html.indexOf("</header>");
		const reasonAt = html.indexOf("context deadline exceeded");
		expect(reasonAt).toBeGreaterThan(headerEnd);
	});

	it("wraps the reason inside the card instead of overflowing", () => {
		const html = renderToString(<ProviderCard provider={errored} />);
		expect(html).toContain("break-words");
		expect(html).toContain("context deadline exceeded");
	});

	it("keeps rendering quota windows when there is no error", () => {
		const html = renderToString(
			<ProviderCard
				provider={{
					id: "zai",
					name: "Zai GLM",
					plan: "Pro",
					updated_at: "2026-10-29T00:00:00Z",
					windows: [
						{
							label: "Session remaining (5h)",
							percent: 15,
							unit: "%",
							resets_at: "2026-10-29T02:00:00Z",
						},
					],
				}}
			/>,
		);
		expect(html).not.toContain("unavailable");
		expect(html).toContain("Session remaining (5h)");
	});
});
