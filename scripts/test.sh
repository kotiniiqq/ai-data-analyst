#!/usr/bin/env sh
# Go tests including the integration tests against the compose database
# (start it first: docker compose up -d db).
set -e
cd "$(dirname "$0")/.."
export TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://app:app-dev-only@host.docker.internal:55432/analyst}"
export TEST_WAREHOUSE_URL="${TEST_WAREHOUSE_URL:-postgres://analyst_ro:warehouse-dev-only@host.docker.internal:55432/analyst}"
exec scripts/go.sh test "${@:-./...}"
