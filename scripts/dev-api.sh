#!/usr/bin/env bash
# Runs the API binary locally against the compose database, using .env.
# When .env points at the compose fake gateway, rewrite it to the local port.
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; source .env; set +a
if [[ "${LITELLM_BASE_URL:-}" == http://fakegateway:* ]]; then
  export LITELLM_BASE_URL="http://127.0.0.1:4000"
fi
export DATABASE_URL="${DATABASE_URL:-postgres://writersguild:${POSTGRES_PASSWORD}@127.0.0.1:5433/writersguild?sslmode=disable}"
exec go run ./cmd/writersguild "$@"
