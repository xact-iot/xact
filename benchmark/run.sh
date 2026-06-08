#!/usr/bin/env bash
set -euo pipefail

go build -o main .

: "${NATS_INTERNAL_PASSWORD:=xact-internal-secret}"
export NATS_INTERNAL_PASSWORD

./main "$@"
