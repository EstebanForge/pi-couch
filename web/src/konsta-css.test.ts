import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// Tailwind only generates utilities for class strings it scans, so a
// konsta/react import without an @source entry in index.css renders
// unstyled. This pins the scan list to the import list. DialogButton
// wraps Button and NavbarBackLink uses Icon, so those ride along even
// though no screen imports them directly.
const EXTRA = ["Button", "Icon"];

function konstaImports(dir: string): Set<string> {
	const names = new Set<string>();
	for (const entry of readdirSync(dir, { withFileTypes: true })) {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) {
			for (const n of konstaImports(path)) names.add(n);
			continue;
		}
		if (!/\.(ts|tsx)$/.test(entry.name) || /\.test\.(ts|tsx)$/.test(entry.name)) continue;
		const src = readFileSync(path, "utf8");
		for (const m of src.matchAll(/import\s*\{([^}]*)\}\s*from\s*"konsta\/react"/g)) {
			for (const part of m[1].split(",")) {
				const name = part.split(/\s+as\s+/)[0].trim();
				if (name) names.add(name);
			}
		}
	}
	return names;
}

describe("konsta css pruning", () => {
	it("scans every imported konsta component in index.css", () => {
		const here = dirname(fileURLToPath(import.meta.url));
		const css = readFileSync(join(here, "index.css"), "utf8");
		const names = new Set([...konstaImports(here), ...EXTRA]);
		expect(names.size).toBeGreaterThan(0);
		const classesDir = join(here, "..", "node_modules", "konsta", "shared", "classes");
		for (const name of names) {
			expect(css, `${name}: missing components/${name}.js @source`).toContain(
				`components/${name}.js`,
			);
			if (readdirSync(classesDir).includes(`${name}Classes.js`)) {
				expect(css, `${name}: missing classes/${name}Classes.js @source`).toContain(
					`classes/${name}Classes.js`,
				);
			}
		}
	});
});
