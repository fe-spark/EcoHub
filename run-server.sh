#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$ROOT_DIR/server"
ENV_FILE="$ROOT_DIR/.env"

if [[ $# -gt 0 ]]; then
  echo "Usage: $0" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "Missing env file: $ENV_FILE" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Missing required command: go" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

echo "Using env file: $ENV_FILE"

cd "$SERVER_DIR"
exec go run ./cmd/server
