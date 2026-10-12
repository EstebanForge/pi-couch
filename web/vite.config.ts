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

// base stays '/app/' until cutover: the SPA mounts there in production while
// the templ pages keep serving '/'. Dev serves '/app/' too, so one code path.
export default defineConfig({
	base: '/app/',
	plugins: [react(), tailwindcss(), keepEmbedDir()],
	build: {
		outDir: '../internal/web/app',
		emptyOutDir: true,
	},
	server: {
		proxy: {
			'/api': 'http://127.0.0.1:8090',
			'/stream': 'http://127.0.0.1:8090',
		},
	},
});
