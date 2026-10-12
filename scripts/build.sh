#!/usr/bin/env bash
# Build Couch Pi: the React SPA (when node is present), templ pages,
# esbuild TS bundle, one Go binary.
set -euo pipefail
cd "$(dirname "$0")/.."

if command -v npm >/dev/null 2>&1 && [ -f web/package.json ]; then
	(cd web && npm ci --silent && npm run build --silent)
fi

go tool templ generate ./...
go tool esbuild internal/web/static/js/main.ts \
	--bundle --minify --outfile=internal/web/static/js/main.js
mkdir -p bin
go build -trimpath -o bin/couch-pi ./cmd/couch-pi
