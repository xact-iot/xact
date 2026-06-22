# syntax=docker/dockerfile:1.7

ARG ALPINE_VERSION=3.22
FROM alpine:${ALPINE_VERSION}

ARG XACT_ARTIFACT_DIR=server/deploy/intermediate/docker-image

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S xact \
    && adduser -S -G xact -h /opt/xact xact \
    && mkdir -p /opt/xact/bin /opt/xact/web /opt/xact/plugins /var/lib/xact/nats-store /var/log/xact \
    && chown -R xact:xact /opt/xact /var/lib/xact /var/log/xact

WORKDIR /opt/xact

COPY --chown=xact:xact ${XACT_ARTIFACT_DIR}/xact /opt/xact/bin/xact
COPY --chown=xact:xact ${XACT_ARTIFACT_DIR}/restore /opt/xact/bin/restore
COPY --chown=xact:xact ${XACT_ARTIFACT_DIR}/web /opt/xact/web

ENV STATIC_SERVE_MODE=server \
    STATIC_DIR=/opt/xact/web \
    PLUGIN_DIR=/opt/xact/plugins \
    NATS_STORE_DIR=/var/lib/xact/nats-store \
    NATS_LOG_FILE=/var/log/xact/nats.log \
    API_HOST=0.0.0.0 \
    API_PORT=8080 \
    NATS_HOST=0.0.0.0 \
    NATS_PORT=4222 \
    NATS_WS_HOST=0.0.0.0 \
    NATS_WS_PORT=9222

USER xact

EXPOSE 8080 9222 4222 1883

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/xact/health >/dev/null || exit 1

ENTRYPOINT ["/opt/xact/bin/xact"]

