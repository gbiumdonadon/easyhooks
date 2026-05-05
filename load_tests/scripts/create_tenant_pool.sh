#!/bin/sh
# Creates load-test tenants via POST /admin/tenants and writes .tenant_pool.json (JSON array).
# Requires: curl, jq
# Env: LOADTEST_API_BASE_URL, LOADTEST_ADMIN_TOKEN, LOADTEST_TENANT_COUNT (default 50),
#      LOADTEST_TENANT_PREFIX (default loadtest), TENANT_POOL_FILE (default .tenant_pool.json)

set -e

BASE="${LOADTEST_API_BASE_URL:-http://localhost:8000}"
BASE="${BASE%/}"
TOKEN="${LOADTEST_ADMIN_TOKEN:?LOADTEST_ADMIN_TOKEN is required}"
COUNT="${LOADTEST_TENANT_COUNT:-50}"
PREFIX="${LOADTEST_TENANT_PREFIX:-loadtest}"
OUT="${TENANT_POOL_FILE:-.tenant_pool.json}"

TMP="${OUT}.ndjson"
: >"$TMP"

i=1
while [ "$i" -le "$COUNT" ]; do
  name="${PREFIX}-tenant-$(printf '%04d' "$i")"
  resp=$(curl -sS -X POST "${BASE}/admin/tenants" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"name\":\"${name}\"}")
  if ! echo "$resp" | jq -e '.tenant_id and .secret_key' >/dev/null 2>&1; then
    echo "Failed to create tenant ${name}: $resp" >&2
    exit 1
  fi
  echo "$resp" | jq -c '{tenant_id, secret_key}' >>"$TMP"
  i=$((i + 1))
done

jq -s '.' "$TMP" >"$OUT"
rm -f "$TMP"
echo "Wrote ${COUNT} tenants to ${OUT}"
