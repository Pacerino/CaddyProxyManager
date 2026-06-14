# syntax=docker/dockerfile:1

# ---- Frontend build ----
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci || npm install
COPY frontend/ ./
# Vite is configured (vite.config.ts) to emit into ../backend/embed/assets,
# so create that target and build into it.
RUN mkdir -p /app/backend/embed/assets && npm run build

# ---- Backend build ----
FROM golang:1.25-alpine AS backend
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Bring in the embedded frontend assets produced in the previous stage so the
# `go:embed all:assets/**` directive has files to embed.
COPY --from=frontend /app/backend/embed/assets ./embed/assets
ARG VERSION=dev
ARG COMMIT=unknown
# Pure-Go build (the sqlite driver is CGO-free), so no C toolchain is needed.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/cpm ./cmd/main.go

# ---- Runtime ----
FROM alpine:3.21
RUN apk add --no-cache ca-certificates caddy
COPY --from=backend /out/cpm /usr/local/bin/cpm
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Sensible container defaults: keep data in /data, talk to Caddy over the API.
ENV CPM_DATAFOLDER=/data \
    CPM_LOGFOLDER=/data/logs \
    CPM_CADDYFILE=/data/Caddyfile \
    CPM_CADDY_MODE=api \
    CPM_CADDY_ADMINURL=http://localhost:2019

VOLUME ["/data"]
EXPOSE 3001 80 443
# The entrypoint launches Caddy (admin API) then CPM. Override the CMD or set
# CADDY_CONFIG to bring your own Caddy configuration.
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
