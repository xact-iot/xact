#!/usr/bin/env bash

# XACT pre-deployment unit test runner.
# Runs the UI Vitest suite and the server Go test suite from a clean top-level
# entrypoint. This is intended to migrate naturally into CI/CD later.

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UI_DIR="$ROOT_DIR/ui"
SERVER_DIR="$ROOT_DIR/server"

GREEN=$'\033[0;32m'
RED=$'\033[0;31m'
YELLOW=$'\033[1;33m'
NC=$'\033[0m'

FAILED=0

section() {
  printf '\n%s==> %s%s\n' "$YELLOW" "$1" "$NC"
}

pass() {
  printf '%sPASS%s %s\n' "$GREEN" "$NC" "$1"
}

fail() {
  printf '%sFAIL%s %s\n' "$RED" "$NC" "$1"
  FAILED=1
}

run_step() {
  local name="$1"
  shift

  section "$name"
  if "$@"; then
    pass "$name"
  else
    fail "$name"
  fi
}

require_dir() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    printf '%sMissing directory:%s %s\n' "$RED" "$NC" "$dir" >&2
    exit 1
  fi
}

require_dir "$UI_DIR"
require_dir "$SERVER_DIR"

run_step "UI unit tests" \
  bash -c 'cd "$1" && npm run test:run' _ "$UI_DIR"

run_step "Server unit tests" \
  bash -c 'cd "$1" && GOCACHE="${GOCACHE:-/tmp/xact-go-build}" go test ./...' _ "$SERVER_DIR"

printf '\n'
if [[ "$FAILED" -eq 0 ]]; then
  printf '%sAll unit tests passed.%s\n' "$GREEN" "$NC"
  exit 0
fi

printf '%sOne or more unit test suites failed.%s\n' "$RED" "$NC"
exit 1
