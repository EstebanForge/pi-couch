#!/usr/bin/env bash
# Build Couch Pi: templ pages, esbuild TS bundle, one Go binary.
set -euo pipefail
cd "$(dirname "$0")/.."

go tool templ generate ./...
go tool esbuild internal/web/static/js/main.ts \
	--bundle --minify --outfile=internal/web/static/js/main.js
mkdir -p bin
go build -trimpath -o bin/couch-pi ./cmd/couch-pi
