#!/usr/bin/env bash
# Build Couch Pi: the React SPA (when node is present), one Go binary.
# The SPA embeds into internal/web/app; a fresh clone serves 404 on every
# page until it is built here.
set -euo pipefail
cd "$(dirname "$0")/.."

if command -v npm >/dev/null 2>&1 && [ -f web/package.json ]; then
	(cd web && npm ci --silent && npm run build --silent)
fi

mkdir -p bin
go build -trimpath -o bin/couch-pi ./cmd/couch-pi
