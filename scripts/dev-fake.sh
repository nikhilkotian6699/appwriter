#!/usr/bin/env bash
# Runs the fake gateway locally on :4000.
set -euo pipefail
cd "$(dirname "$0")/.."
export PORT="${PORT:-4000}"
exec go run ./cmd/fakegateway
