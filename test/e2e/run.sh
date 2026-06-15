#!/usr/bin/env bash
# End-to-end test for a deployed CPM PR environment running in API mode.
#
# Verifies the full stack: API health, admin login, host creation, that Caddy
# actually reverse-proxies a request to the whoami upstream, the per-host basic
# auth plugin, and module discovery. Hits the containers locally on the runner
# (independent of any tunnel).
#
# Env:
#   CPM_URL        base URL of the CPM API (default http://localhost:3001)
#   CADDY_URL      base URL of Caddy's HTTP port (default http://localhost:8080)
#   ADMIN_EMAIL    seeded admin email (default admin@example.com)
#   ADMIN_PASSWORD seeded admin password (default changeme)
#   TEST_DOMAIN    host header used for the proxied request (default app.e2e.local)
set -euo pipefail

CPM_URL="${CPM_URL:-http://localhost:3001}"
CADDY_URL="${CADDY_URL:-http://localhost:8080}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@example.com}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-changeme}"
TEST_DOMAIN="${TEST_DOMAIN:-app.e2e.local}"

pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; exit 1; }

# jqr <json> <filter> — extract a field with jq.
jqr() { printf '%s' "$1" | jq -r "$2"; }

echo "== Waiting for CPM API to become healthy =="
for i in $(seq 1 30); do
  if curl -fsS "${CPM_URL}/api/auth/config" >/dev/null 2>&1; then
    break
  fi
  if [ "$i" = "30" ]; then fail "CPM API did not become healthy in time"; fi
  sleep 2
done
pass "CPM API is up"

echo "== Auth config reports api/local mode =="
cfg=$(curl -fsS "${CPM_URL}/api/auth/config")
mode=$(jqr "$cfg" '.result.mode')
[ "$mode" = "local" ] || fail "expected local auth mode, got '$mode'"
pass "auth mode = local"

echo "== Admin login =="
login=$(curl -fsS -X POST "${CPM_URL}/api/users/login" \
  -H 'Content-Type: application/json' \
  -d "{\"Email\":\"${ADMIN_EMAIL}\",\"Secret\":\"${ADMIN_PASSWORD}\"}")
TOKEN=$(jqr "$login" '.result.token')
[ -n "$TOKEN" ] && [ "$TOKEN" != "null" ] || fail "login did not return a token: $login"
AUTH="Authorization: Bearer ${TOKEN}"
pass "logged in, got JWT"

echo "== Module discovery (list-modules in-container) =="
mods=$(curl -fsS -H "$AUTH" "${CPM_URL}/api/caddy/modules")
mcount=$(jqr "$mods" '.result.modules | length')
[ "$mcount" -gt 0 ] || fail "expected modules from caddy build, got $mcount"
pass "discovered $mcount caddy modules"

echo "== Create a host that proxies ${TEST_DOMAIN} -> whoami =="
created=$(curl -fsS -X POST "${CPM_URL}/api/hosts" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"domains\":\"${TEST_DOMAIN}\",\"matcher\":\"\",\"Upstreams\":[{\"backend\":\"whoami:80\"}]}")
HOST_ID=$(jqr "$created" '.result.ID')
[ -n "$HOST_ID" ] && [ "$HOST_ID" != "null" ] || fail "host create failed: $created"
pass "created host id=$HOST_ID"

echo "== Verify Caddy reverse-proxies the request to whoami =="
# CPM applies the route asynchronously via the job queue; allow a short window.
ok=""
for i in $(seq 1 15); do
  body=$(curl -fsS -H "Host: ${TEST_DOMAIN}" "${CADDY_URL}/" 2>/dev/null || true)
  # whoami echoes a "Hostname:" line in its response.
  if printf '%s' "$body" | grep -qi "Hostname:"; then
    ok="yes"; break
  fi
  sleep 2
done
[ -n "$ok" ] || fail "proxied request did not reach whoami"
pass "request proxied to whoami"

echo "== Add basic auth plugin to the host =="
curl -fsS -X PUT "${CPM_URL}/api/hosts/${HOST_ID}/plugins" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"moduleId":"http.handlers.authentication","values":{"username":"e2e","password":"e2epass"}}' >/dev/null
pass "basic auth plugin configured"

echo "== Request without credentials should be 401 =="
ok=""
for i in $(seq 1 15); do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "Host: ${TEST_DOMAIN}" "${CADDY_URL}/" || true)
  if [ "$code" = "401" ]; then ok="yes"; break; fi
  sleep 2
done
[ -n "$ok" ] || fail "expected 401 without credentials"
pass "unauthenticated request rejected (401)"

echo "== Request with credentials should be 200 =="
code=$(curl -s -o /dev/null -w '%{http_code}' -u "e2e:e2epass" -H "Host: ${TEST_DOMAIN}" "${CADDY_URL}/" || true)
[ "$code" = "200" ] || fail "expected 200 with credentials, got $code"
pass "authenticated request succeeded (200)"

echo "== Delete the host =="
del=$(curl -fsS -X DELETE -H "$AUTH" "${CPM_URL}/api/hosts/${HOST_ID}")
pass "host deleted"

echo
echo "ALL E2E CHECKS PASSED"
