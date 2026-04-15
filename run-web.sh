#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$ROOT_DIR/web"
ENV_FILE="$ROOT_DIR/.env"

if [[ $# -gt 0 ]]; then
  echo "Usage: $0" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "Missing env file: $ENV_FILE" >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "Missing required command: npm" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a

WEB_PORT="${WEB_PORT:-3000}"

echo "Using env file: $ENV_FILE"
echo "Starting web dev server on port: $WEB_PORT"

cd "$WEB_DIR"
exec npm run dev -- --port "$WEB_PORT"
