default:
    @just --list

# Build web SPA and Go binary to bin/couch-pi
build:
    ./scripts/build.sh

# Run local CI gates (Go, web checks, unit and mobile browser tests)
ci:
    ./scripts/ci.sh

# Build the SPA and run real Chromium phone-browser tests
test-e2e:
    cd web && npm run test:e2e

# Run Go tests only
test-go *args:
    go test ./... {{args}}

# Run Web checks only (tsc + biome)
check-web:
    cd web && npm run check

# Build Android debug APK
apk:
    cd android && ./gradlew assembleDebug
