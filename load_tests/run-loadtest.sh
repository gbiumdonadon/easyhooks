#!/bin/bash
#
# run-loadtest.sh — prepare tenant pool (Docker) and print k6 commands
#
# Usage:
#   ./run-loadtest.sh [scenario] [--tenant-count N] [--api-url URL] [--help]
#
# Scenarios: baseline, throughput, websocket_scale, multi_tenant, stress, custom_scenario
#
# Examples:
#   ./run-loadtest.sh baseline
#   ./run-loadtest.sh throughput --tenant-count 100 --api-url http://localhost:8000

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

SCENARIO="${1:-baseline}"
TENANT_COUNT="${LOADTEST_TENANT_COUNT:-50}"
API_URL="${LOADTEST_API_BASE_URL:-http://localhost:8000}"

SCRIPT_DIR="$( cd "$( dirname "${0}" )" && pwd )"
shift || true

while [[ $# -gt 0 ]]; do
  case $1 in
    --tenant-count)
      TENANT_COUNT="$2"
      shift 2
      ;;
    --api-url)
      API_URL="$2"
      shift 2
      ;;
    --help)
      grep '^#' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo -e "${RED}Unknown option: $1${NC}"
      exit 1
      ;;
  esac
done

case "$SCENARIO" in
  baseline|throughput|websocket_scale|multi_tenant|stress|custom_scenario)
    K6_SCRIPT="k6/scenarios/${SCENARIO}.js"
    ;;
  *)
    echo -e "${RED}Unknown scenario: $SCENARIO${NC}"
    exit 1
    ;;
esac

export LOADTEST_API_BASE_URL="$API_URL"
export LOADTEST_TENANT_COUNT="$TENANT_COUNT"

echo ""
echo "${BLUE}════════════════════════════════════════════════════════${NC}"
echo "${BLUE}  EasyHooks load test (k6)${NC}"
echo "${BLUE}════════════════════════════════════════════════════════${NC}"
echo ""
echo "${CYAN}Scenario:${NC}  $SCENARIO  →  $K6_SCRIPT"
echo "${CYAN}API:${NC}       $API_URL"
echo "${CYAN}Tenants:${NC}   $TENANT_COUNT"
echo ""

echo "${BLUE}[1/3]${NC} Validating environment..."
if ! bash "$SCRIPT_DIR/validate-env.sh"; then
  echo -e "${RED}Validation failed.${NC}"
  exit 1
fi

IS_LOCAL=false
if [[ "$API_URL" == *"localhost"* ]] || [[ "$API_URL" == *"127.0.0.1"* ]]; then
  IS_LOCAL=true
fi

if $IS_LOCAL; then
  echo "${BLUE}[2/3]${NC} Local API — ensure stack is up (docker compose up -d)"
  cd "$SCRIPT_DIR/.."
  if ! docker compose ps 2>/dev/null | grep -q "app.*Up"; then
    echo -e "${YELLOW}Starting docker compose...${NC}"
    docker compose up -d
    sleep 5
  fi
  cd "$SCRIPT_DIR"
fi

echo "${BLUE}[3/3]${NC} Creating tenant pool via Docker (Alpine + curl)..."
export LOADTEST_TENANT_COUNT
export LOADTEST_API_BASE_URL
DOCKER_API_URL="$API_URL"
if [[ "$API_URL" == *"localhost"* ]] || [[ "$API_URL" == *"127.0.0.1"* ]]; then
  DOCKER_API_URL=$(echo "$API_URL" | sed 's/localhost/host.docker.internal/g' | sed 's/127.0.0.1/host.docker.internal/g')
fi
export LOADTEST_API_BASE_URL="$DOCKER_API_URL"

cd "$SCRIPT_DIR/.."
docker compose -f load_tests/docker-compose.loadtest.yml run --rm \
  -e LOADTEST_API_BASE_URL="$DOCKER_API_URL" \
  -e LOADTEST_TENANT_COUNT="$TENANT_COUNT" \
  loadtest-init

echo ""
echo "${GREEN}Tenant pool ready.${NC}"
echo ""
echo "${CYAN}Run k6 (pick one):${NC}"
echo ""
echo "  ${YELLOW}# With Docker (recommended on Windows)${NC}"
echo "  docker compose -f load_tests/docker-compose.loadtest.yml run --rm --no-deps \\"
echo "    -e LOADTEST_API_BASE_URL=\"$DOCKER_API_URL\" \\"
echo "    -e TENANT_POOL_FILE=/load_tests/.tenant_pool.json \\"
echo "    k6 run $K6_SCRIPT"
echo ""
echo "  ${YELLOW}# With local k6 (from load_tests/)${NC}"
echo "  cd load_tests && export LOADTEST_API_BASE_URL=\"$API_URL\" && export TENANT_POOL_FILE=.tenant_pool.json \\"
echo "    && k6 run $K6_SCRIPT"
echo ""
echo "${CYAN}Metrics:${NC} Grafana dashboard \"EasyHooks Load Test\" shows API Prometheus metrics during the run."
echo ""
