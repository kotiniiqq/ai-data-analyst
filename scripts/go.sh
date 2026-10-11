#!/usr/bin/env sh
# Runs the Go toolchain in Docker, so the host needs only Docker.
# Usage: scripts/go.sh test ./...
# TEST_DATABASE_URL, if set, is passed through (use host.docker.internal for a database on the host).
set -e
root=$(cd "$(dirname "$0")/.." && pwd)
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) root=$(cygpath -w "$root"); export MSYS_NO_PATHCONV=1 ;;
esac
exec docker run --rm -i \
  -v "$root:/src" -w /src \
  -v ai-data-analyst-gomod:/go/pkg/mod \
  -v ai-data-analyst-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 \
  -e TEST_DATABASE_URL \
  --add-host=host.docker.internal:host-gateway \
  golang:1.25-alpine go "$@"
