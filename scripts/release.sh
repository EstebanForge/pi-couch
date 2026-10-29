#!/usr/bin/env bash
# Local release: gate, cross-build, checksum, publish. This is the
# default release path; release.yml only exists for a manual dispatch
# and never runs on push.
set -euo pipefail
cd "$(dirname "$0")/.."

version="$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/server/version.go)"
tag="${1:-$version}"
if [ "$tag" != "$version" ]; then
	echo "tag $tag does not match version.go ($version)" >&2
	exit 1
fi

./scripts/ci.sh

rm -rf dist
mkdir dist
(cd web && npm run build)
for pair in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	os=${pair%/*}
	arch=${pair#*/}
	echo "== build pi-couch-$os-$arch"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -o "dist/pi-couch-$os-$arch" ./cmd/couch-pi
done
(cd dist && sha256sum pi-couch-* > checksums.txt)

if gh release view "$tag" >/dev/null 2>&1; then
	echo "release $tag exists; uploading assets"
	gh release upload "$tag" dist/pi-couch-* dist/checksums.txt --clobber
else
	gh release create "$tag" dist/pi-couch-* dist/checksums.txt --title "$tag" --generate-notes
fi
echo "release $tag published"
