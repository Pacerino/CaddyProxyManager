#!/bin/sh
set -e

# Start Caddy in the background with just its admin API enabled. CPM (in the
# default "api" mode) then manages the HTTP config through that admin API.
# Mount a custom config at /data/caddy.json to override this behaviour.
CADDY_CONFIG="${CADDY_CONFIG:-/data/caddy.json}"

mkdir -p "$(dirname "$CADDY_CONFIG")"
if [ ! -f "$CADDY_CONFIG" ]; then
  cat > "$CADDY_CONFIG" <<'JSON'
{
  "admin": { "listen": "localhost:2019" },
  "apps": { "http": { "servers": {} } }
}
JSON
fi

# Allow pointing at a custom Caddy build (e.g. one compiled with extra plugins)
# mounted into the container. Keep this in sync with CPM_CADDY_BINARY so CPM's
# module detection inspects the same binary.
CADDY_BINARY="${CPM_CADDY_BINARY:-caddy}"

echo "Starting ${CADDY_BINARY} with admin API…"
"$CADDY_BINARY" run --config "$CADDY_CONFIG" &
CADDY_PID=$!

# Stop Caddy when CPM exits.
trap 'kill "$CADDY_PID" 2>/dev/null || true' EXIT INT TERM

# Give the admin API a moment to come up.
sleep 1

echo "Starting Caddy Proxy Manager…"
exec /usr/local/bin/cpm
