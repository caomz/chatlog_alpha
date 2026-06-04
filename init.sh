#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'USAGE'
Usage: ./init.sh [--full] [--runtime] [--help]

Default quick gate:
  - node scripts/check-root-harness.mjs
  - node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
  - go run . report daily --help
  - go run . http list

Options:
  --full     Also run go test ./... and make build.
  --runtime  Check an already-running local HTTP service at 127.0.0.1:5030.

Privacy/quota boundary:
  This script does not run real daily reports, --vision, or --summary.
USAGE
}

run() {
  echo "=== $* ==="
  "$@"
}

FULL=0
RUNTIME=0

for arg in "$@"; do
  case "$arg" in
    --full)
      FULL=1
      ;;
    --runtime)
      RUNTIME=1
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

echo "=== Harness Initialization ==="
echo "Root: $ROOT_DIR"
MODE="quick"
if [ "$FULL" -eq 1 ]; then
  MODE="$MODE +full"
fi
if [ "$RUNTIME" -eq 1 ]; then
  MODE="$MODE +runtime"
fi
echo "Mode: $MODE"
echo ""

run node scripts/check-root-harness.mjs
run node skills/chatlog-http-cli/scripts/check-harness-skill.mjs
run go run . report daily --help
run go run . http list

if [ "$FULL" -eq 1 ]; then
  run go test ./...
  run make build
fi

if [ "$RUNTIME" -eq 1 ]; then
  run curl -fsS http://127.0.0.1:5030/health
  echo ""
  run curl -fsS http://127.0.0.1:5030/api/v1/ping
  echo ""
fi

echo "=== Verification Complete ==="
echo "Next steps:"
echo "1. Record command evidence in progress.md."
echo "2. Update feature_list.json status/evidence for the active feature."
echo "3. Update session-handoff.md so the next session is restartable."
