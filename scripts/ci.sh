#!/usr/bin/env bash
# Local CI: the gates release.yml's verify job runs, plus gofmt.
# Pushing green code is the contract; run this before every push.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== go vet"
go vet ./...

echo "== gofmt"
dirty="$(gofmt -l internal cmd)"
if [ -n "$dirty" ]; then
	echo "$dirty"
	exit 1
fi

echo "== go test"
go test ./...

echo "== go test -race"
go test -race ./internal/pi ./internal/server ./internal/update

echo "== web: npm ci"
(cd web && npm ci --no-audit --no-fund)

echo "== web: tsc + biome"
(cd web && npm run check)

echo "== web: vitest"
(cd web && ./node_modules/.bin/vitest run)

echo "== web: mobile browser E2E"
(cd web && npm run test:e2e)

echo "ci: all gates green"
