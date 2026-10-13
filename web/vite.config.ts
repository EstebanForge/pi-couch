import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { mkdirSync, writeFileSync } from 'node:fs';
import { defineConfig, type Plugin } from 'vite';

// go:embed needs internal/web/app to exist even on a fresh clone before the
// first build. emptyOutDir wipes the dir, so recreate the marker on close.
const keepEmbedDir = (): Plugin => ({
	name: 'keep-embed-dir',
	closeBundle() {
		mkdirSync('../internal/web/app', { recursive: true });
		writeFileSync('../internal/web/app/.gitkeep', '');
	},
});

// The SPA is the whole site since cutover: base '/' and the Go server
// mounts the embed on GET /.
const proxyTarget = process.env.COUCHPI_PROXY ?? "127.0.0.1:8090";

export default defineConfig({
	base: '/',
	plugins: [react(), tailwindcss(), keepEmbedDir()],
	build: {
		outDir: '../internal/web/app',
		emptyOutDir: true,
	},
	server: {
		proxy: {
			// COUCHPI_PROXY points dev at web/dev/fixture.mjs for UI work
			// without a live agent.
			'/api': `http://${proxyTarget}`,
			'/stream': `http://${proxyTarget}`,
		},
	},
});
