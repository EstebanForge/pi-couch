default:
    @just --list

# Build web SPA and Go binary to bin/couch-pi
build:
    ./scripts/build.sh

# Run local CI gates (go vet, gofmt, go test, tsc, biome, vitest)
ci:
    ./scripts/ci.sh

# Run Go tests only
test-go *args:
    go test ./... {{args}}

# Run Web checks only (tsc + biome)
check-web:
    cd web && npm run check

# Build Android debug APK
apk:
    cd android && ./gradlew assembleDebug
