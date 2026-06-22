#!/bin/sh
set -eu

PLUGIN_ROOT="${PLUGIN_DIR:-/opt/xact/plugins}"
NATS_ROOT="${NATS_STORE_DIR:-/var/lib/xact/nats-store}"
NATS_LOG_DIR="$(dirname "${NATS_LOG_FILE:-/var/log/xact/nats.log}")"

mkdir -p \
    "$PLUGIN_ROOT/authentication" \
    "$PLUGIN_ROOT/widgets" \
    "$PLUGIN_ROOT/map-layer" \
    "$PLUGIN_ROOT/themes" \
    "$NATS_ROOT" \
    "$NATS_LOG_DIR"

chown -R xact:xact "$PLUGIN_ROOT" "$NATS_ROOT" "$NATS_LOG_DIR" 2>/dev/null || true

exec su-exec xact:xact /opt/xact/bin/xact "$@"
