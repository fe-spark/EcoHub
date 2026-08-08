#!/usr/bin/env bash
# 诊断「连续采集更新列表反复出现相同影片」
# 用法:
#   ./scripts/diagnose-notify.sh
#   ./scripts/diagnose-notify.sh -sources '魔都,HD(FF)' -limit 10
#   ./scripts/diagnose-notify.sh -mids 98880,98879 -sources '魔都,HD(FF)'
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi
exec go run ./cmd/diagnose-notify "$@"
