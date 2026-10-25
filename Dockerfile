# syntax=docker/dockerfile:1

# All build stages run on the native build platform ($BUILDPLATFORM) and
# cross-compile for $TARGETPLATFORM, so multi-arch builds don't need to emulate
# the compilers.

# ---- Frontend build ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Vite is configured (vite.config.ts) to emit into ../backend/embed/assets,
# so create that target and build into it.
RUN mkdir -p /app/backend/embed/assets && npm run build

# ---- Backend build ----
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Bring in the embedded frontend assets produced in the previous stage so the
# `go:embed all:assets/**` directive has files to embed.
COPY --from=frontend /app/backend/embed/assets ./embed/assets
ARG VERSION=dev
ARG COMMIT=unknown
ARG TARGETOS
ARG TARGETARCH
# Pure-Go build (the sqlite driver is CGO-free), so no C toolchain is needed.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/cpm ./cmd/main.go

# ---- Caddy ----
# Always built via xcaddy rather than the stock release: the build includes
# caddy-json-schema, which the runtime stage uses to generate the schema CPM
# reads for DNS provider forms. CADDY_PLUGINS (a space-separated list of Go
# module paths, optionally `@version`) adds further plugins on top.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS caddy
ARG CADDY_VERSION=2.11.7
ARG XCADDY_VERSION=0.4.5
ARG CADDY_JSON_SCHEMA=github.com/abiosoft/caddy-json-schema@v0.0.0-20220621031927-c4d6e132f3af
ARG CADDY_PLUGINS=""
ARG TARGETOS
ARG TARGETARCH
RUN set -eu; \
    mkdir -p /out; \
    apk add --no-cache git; \
    go install "github.com/caddyserver/xcaddy/cmd/xcaddy@v${XCADDY_VERSION}"; \
    with="--with ${CADDY_JSON_SCHEMA}"; \
    for p in ${CADDY_PLUGINS}; do with="${with} --with ${p}"; done; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
      xcaddy build "v${CADDY_VERSION}" ${with} --output /out/caddy; \
    chmod +x /out/caddy

# ---- Runtime ----
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=caddy /out/caddy /usr/local/bin/caddy
RUN caddy version
# The schema describes every module compiled into this Caddy; generating it
# needs internet access (it fetches the Caddy docs), fine during the build.
RUN mkdir -p /usr/share/cpm && caddy json-schema --output /usr/share/cpm/caddy_schema.json
COPY --from=backend /out/cpm /usr/local/bin/cpm
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Sensible container defaults: keep data in /data, talk to Caddy over the API.
ENV CPM_DATAFOLDER=/data \
    CPM_LOGFOLDER=/data/logs \
    CPM_CADDYFILE=/data/Caddyfile \
    CPM_CADDY_MODE=api \
    CPM_CADDY_ADMINURL=http://localhost:2019 \
    CPM_CADDY_SCHEMAFILE=/usr/share/cpm/caddy_schema.json \
    CPM_CADDY_SUPERVISE=true \
    CPM_CADDY_CONFIGFILE=/data/caddy.json \
    XDG_CONFIG_HOME=/data/.config \
    XDG_DATA_HOME=/data/.local/share

VOLUME ["/data"]
EXPOSE 3001 80 443
# CPM supervises Caddy itself (CPM_CADDY_SUPERVISE=true, API mode); otherwise the
# entrypoint launches Caddy (admin API) then CPM. Caddy's autosave and
# certificates live under /data via XDG_*. Set CADDY_CONFIG to bring your own
# Caddy configuration when not supervising.
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
