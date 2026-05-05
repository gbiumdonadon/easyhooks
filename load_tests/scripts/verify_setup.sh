#!/bin/bash
# Verify load test setup (k6 + Docker + tenant pool)

set -e

echo "=========================================="
echo "Load test setup verification (k6)"
echo "=========================================="
echo ""

ERRORS=0
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
LOAD_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"

echo "[1/6] Docker..."
if command -v docker >/dev/null 2>&1; then
  echo "  ✓ $(docker --version)"
  docker info >/dev/null 2>&1 || { echo "  ✗ Docker daemon not running"; ERRORS=$((ERRORS + 1)); }
else
  echo "  ✗ Docker not found"
  ERRORS=$((ERRORS + 1))
fi
echo ""

echo "[2/6] k6 (optional if you only use Docker)..."
if command -v k6 >/dev/null 2>&1; then
  echo "  ✓ $(k6 version 2>/dev/null | tr '\n' ' ')"
else
  echo "  ⚠ k6 not in PATH — use: docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 run ..."
fi
echo ""

echo "[3/6] curl / jq..."
command -v curl >/dev/null 2>&1 || { echo "  ✗ curl required for tenant pool script"; ERRORS=$((ERRORS + 1)); }
command -v jq >/dev/null 2>&1 || { echo "  ✗ jq required for tenant pool script"; ERRORS=$((ERRORS + 1)); }
echo "  ✓ curl/jq checks done"
echo ""

echo "[4/6] API health (http://localhost:8000/health)..."
if curl -sf http://localhost:8000/health >/dev/null 2>&1; then
  echo "  ✓ API responding"
else
  echo "  ✗ API not reachable — run: docker compose up -d"
  ERRORS=$((ERRORS + 1))
fi
echo ""

echo "[5/6] Tenant pool ($LOAD_ROOT/.tenant_pool.json)..."
if [ -f "$LOAD_ROOT/.tenant_pool.json" ]; then
  n=$(jq 'length' "$LOAD_ROOT/.tenant_pool.json" 2>/dev/null || echo 0)
  echo "  ✓ Pool exists ($n tenants)"
  if [ "${n:-0}" -lt 10 ] 2>/dev/null; then
    echo "  ⚠ Few tenants — recommend 50+"
  fi
else
  echo "  ⚠ No pool yet — run: cd load_tests && bash scripts/create_tenant_pool.sh"
fi
echo ""

echo "[6/6] Grafana (optional)..."
if curl -sf -o /dev/null -w "%{http_code}" http://localhost:3000 | grep -qE '200|302'; then
  echo "  ✓ Grafana reachable"
else
  echo "  ⚠ Grafana not reachable (optional)"
fi
echo ""

echo "=========================================="
if [ "$ERRORS" -eq 0 ]; then
  echo "✓ Core checks passed"
  echo ""
  echo "Run a scenario from repo root, e.g.:"
  echo "  cd load_tests && bash run-loadtest.sh baseline"
  echo "  docker compose -f load_tests/docker-compose.loadtest.yml run --rm --no-deps -e TENANT_POOL_FILE=/load_tests/.tenant_pool.json k6 run k6/scenarios/baseline.js"
  exit 0
fi

echo "✗ $ERRORS error(s)"
exit 1
