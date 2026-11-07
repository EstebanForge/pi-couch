import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

// go:embed needs internal/web/app to exist even on a fresh clone before the
// first build. emptyOutDir wipes the dir, so recreate the marker on close.
const keepEmbedDir = (): Plugin => ({
	name: "keep-embed-dir",
	closeBundle() {
		mkdirSync("../internal/web/app", { recursive: true });
		writeFileSync("../internal/web/app/.gitkeep", "");
	},
});

// The Go embed serves .gz sidecars only when the client asks for them
// (Accept-Encoding), so it carries both forms; emptyOutDir clears stale
// sidecars on the next build. The extension set mirrors gzipExt in
// internal/web/web.go.
const emitGzip = (): Plugin => ({
	name: "emit-gzip",
	apply: "build",
	closeBundle() {
		const walk = (dir: string): string[] => {
			if (!existsSync(dir)) return [];
			return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
				e.isDirectory() ? walk(join(dir, e.name)) : [join(dir, e.name)],
			);
		};
		for (const p of walk("../internal/web/app")) {
			if (!/\.(js|css|html|svg|json|webmanifest)$/.test(p)) continue;
			writeFileSync(`${p}.gz`, gzipSync(readFileSync(p), { level: 9 }));
		}
	},
});

// The SPA is the whole site since cutover: base '/' and the Go server
// mounts the embed on GET /.
const proxyTarget = process.env.COUCHPI_PROXY ?? "127.0.0.1:8090";

export default defineConfig({
	base: "/",
	plugins: [react(), tailwindcss(), keepEmbedDir(), emitGzip()],
	build: {
		outDir: "../internal/web/app",
		emptyOutDir: true,
	},
	server: {
		proxy: {
			// COUCHPI_PROXY points dev at web/dev/fixture.mjs for UI work
			// without a live agent.
			"/api": `http://${proxyTarget}`,
			"/stream": `http://${proxyTarget}`,
		},
	},
});
