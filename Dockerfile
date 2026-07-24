# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:22-alpine AS web-build

WORKDIR /src/web

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

COPY web/ ./
RUN npm run build


FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS api-build

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

WORKDIR /src

COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    goarm=""; \
    if [ "$TARGETARCH" = "arm" ]; then \
      case "$TARGETVARIANT" in \
        v7|"") goarm=7 ;; \
        v6) goarm=6 ;; \
        *) goarm="${TARGETVARIANT#v}" ;; \
      esac; \
    fi; \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOARM="$goarm" \
      go build -trimpath -ldflags="-s -w" -o /out/xhs-api ./cmd/api


FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -h /app -u 10001 -G app app \
    && mkdir -p /app/web/dist /app/Volume/Data \
    && chown -R app:app /app

WORKDIR /app

LABEL name="XHS-Downloader" \
      authors="SpiralTower" \
      repository="https://github.com/SpiralTower/XHS-Downloader"

COPY --chown=app:app --from=api-build /out/xhs-api /app/xhs-api
COPY --chown=app:app --from=web-build /src/web/dist /app/web/dist
COPY --chown=app:app LICENSE /app/LICENSE

ENV HOST=0.0.0.0 \
    PORT=5556 \
    WEB_DIST_DIR=/app/web/dist \
    XHS_VOLUME_DIR=/app/Volume \
    XHS_DATABASE_PATH=/app/Volume/Data/xhs.sqlite3 \
    XHS_ADMIN_USERNAME=admin \
    XHS_REQUEST_TIMEOUT=15s \
    XHS_DOWNLOAD_TIMEOUT=30m \
    XHS_DOWNLOAD_IDLE_TIMEOUT=60s \
    XHS_ALLOW_PRIVATE_PROXY=false \
    XHS_MAX_MEDIA_BYTES=2147483648 \
    XHS_SESSION_COOKIE_SECURE=false \
    HOME=/app

# Leave XHS_SECRET_KEY_PATH unset to let the service manage secrets.key beside
# the SQLite database. An explicit path may point at a pre-provisioned,
# read-only 32-byte secret that is readable by UID/GID 10001.

VOLUME ["/app/Volume"]
EXPOSE 5556

USER app:app

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -T 3 -O /dev/null "http://127.0.0.1:${PORT}/healthz" || exit 1

STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/xhs-api"]
