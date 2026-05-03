#!/bin/bash
#
# run-loadtest.sh - Intelligent wrapper for running load tests
#
# Description:
#   Master script that orchestrates the entire load testing workflow:
#   1. Validates environment prerequisites
#   2. Detects if API is local or remote
#   3. For local APIs, ensures the main stack is running
#   4. Starts the load testing stack (Locust)
#   5. Shows real-time logs and status
#
# Usage:
#   ./run-loadtest.sh [scenario] [options]
#
# Arguments:
#   scenario              - Test scenario to run (default: baseline)
#                          Options: baseline, throughput, websocket_scale, 
#                                   multi_tenant, stress
#
# Options:
#   --tenant-count NUM   - Number of tenants to create (default: 50)
#   --api-url URL        - Override API URL (default: from env or localhost:8000)
#   --workers NUM        - Number of Locust workers (default: 2)
#   --headless           - Run in headless mode (no UI)
#   --help               - Show this help message
#
# Environment Variables:
#   LOADTEST_API_BASE_URL  - API endpoint (default: http://localhost:8000)
#   LOADTEST_ADMIN_TOKEN   - Admin token for tenant creation
#   LOADTEST_TENANT_COUNT  - Number of tenants (default: 50)
#   LOADTEST_WORKERS       - Number of workers (default: 2)
#
# Examples:
#   # Run baseline test against local API
#   ./run-loadtest.sh baseline
#
#   # Run throughput test with 100 tenants
#   ./run-loadtest.sh throughput --tenant-count 100
#
#   # Test against staging
#   export LOADTEST_API_BASE_URL=https://staging.easyhooks.io
#   ./run-loadtest.sh baseline
#
# Exit Codes:
#   0 - Test completed successfully
#   1 - Validation failed or error occurred

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Default values
SCENARIO="${1:-baseline}"
TENANT_COUNT="${LOADTEST_TENANT_COUNT:-50}"
API_URL="${LOADTEST_API_BASE_URL:-http://localhost:8000}"
WORKERS="${LOADTEST_WORKERS:-2}"
HEADLESS=false

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Parse command line arguments
shift || true  # Remove first argument (scenario)

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
    --workers)
      WORKERS="$2"
      shift 2
      ;;
    --headless)
      HEADLESS=true
      shift
      ;;
    --help)
      grep '^#' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo -e "${RED}❌ Argumento desconhecido: $1${NC}"
      echo "   Use --help para ver opções disponíveis"
      exit 1
      ;;
  esac
done

# Banner
echo ""
echo "${BLUE}════════════════════════════════════════════════════════${NC}"
echo "${BLUE}  🚀 EasyHooks Load Test Runner${NC}"
echo "${BLUE}════════════════════════════════════════════════════════${NC}"
echo ""
echo "${CYAN}Configuração:${NC}"
echo "  Scenario:      ${YELLOW}${SCENARIO}${NC}"
echo "  API URL:       ${YELLOW}${API_URL}${NC}"
echo "  Tenants:       ${YELLOW}${TENANT_COUNT}${NC}"
echo "  Workers:       ${YELLOW}${WORKERS}${NC}"
echo "  Headless:      ${YELLOW}${HEADLESS}${NC}"
echo ""

# Export environment variables
export LOADTEST_API_BASE_URL="$API_URL"
export LOADTEST_TENANT_COUNT="$TENANT_COUNT"
export LOADTEST_WORKERS="$WORKERS"
export LOADTEST_ADMIN_TOKEN="${LOADTEST_ADMIN_TOKEN}"

# Step 1: Validate environment
echo "${BLUE}[Passo 1/4]${NC} Validando ambiente..."
if ! bash "$SCRIPT_DIR/validate-env.sh"; then
  echo ""
  echo "${RED}❌ Validação falhou. Corrija os problemas acima.${NC}"
  exit 1
fi

# Step 2: Check if API is local and ensure stack is running
echo "${BLUE}[Passo 2/4]${NC} Verificando API..."

IS_LOCAL=false
if [[ "$API_URL" == *"localhost"* ]] || [[ "$API_URL" == *"127.0.0.1"* ]] || [[ "$API_URL" == *"host.docker.internal"* ]]; then
  IS_LOCAL=true
  echo "  ${YELLOW}→${NC} API detectada como local"
  
  # Check if main docker compose stack is running
  cd "$SCRIPT_DIR/.."
  
  if docker compose ps | grep -q "app.*Up"; then
    echo "  ${GREEN}✓${NC} Stack principal já está rodando"
  else
    echo "  ${YELLOW}⚠${NC}  Stack principal não está rodando"
    echo "  ${BLUE}→${NC} Subindo stack da aplicação..."
    
    docker compose up -d
    
    echo "  ${YELLOW}→${NC} Aguardando serviços ficarem healthy..."
    sleep 5
    
    # Wait for services to be healthy
    MAX_WAIT=60
    WAITED=0
    while [[ $WAITED -lt $MAX_WAIT ]]; do
      if docker compose ps | grep -q "app.*healthy"; then
        echo "  ${GREEN}✓${NC} Serviços estão healthy"
        break
      fi
      sleep 2
      WAITED=$((WAITED + 2))
      echo "  ${YELLOW}→${NC} Aguardando... (${WAITED}s/${MAX_WAIT}s)"
    done
    
    if [[ $WAITED -ge $MAX_WAIT ]]; then
      echo "  ${YELLOW}⚠${NC}  Timeout aguardando services ficarem healthy"
      echo "  ${YELLOW}→${NC} Continuando mesmo assim..."
    fi
  fi
  
  # For local testing, use host.docker.internal to access host from container
  if [[ "$API_URL" == *"localhost"* ]] || [[ "$API_URL" == *"127.0.0.1"* ]]; then
    export LOADTEST_API_BASE_URL=$(echo "$API_URL" | sed 's/localhost/host.docker.internal/g' | sed 's/127.0.0.1/host.docker.internal/g')
    echo "  ${YELLOW}→${NC} API URL ajustada para Docker: ${LOADTEST_API_BASE_URL}"
  fi
  
  cd "$SCRIPT_DIR"
else
  echo "  ${GREEN}✓${NC} API remota: $API_URL"
fi

echo ""

# Step 3: Start load testing stack
echo "${BLUE}[Passo 3/4]${NC} Iniciando stack de testes..."

cd "$SCRIPT_DIR"

# Clean up any existing containers
echo "  ${YELLOW}→${NC} Limpando containers antigos..."
docker compose -f docker-compose.loadtest.yml down 2>/dev/null || true

echo "  ${YELLOW}→${NC} Subindo stack de load testing..."
docker compose -f docker-compose.loadtest.yml up -d

echo "  ${GREEN}✓${NC} Stack iniciado com sucesso"
echo ""

# Step 4: Show information and logs
echo "${BLUE}[Passo 4/4]${NC} Monitoramento..."
echo ""
echo "${GREEN}════════════════════════════════════════════════════════${NC}"
echo "${GREEN}  ✅ Load Test Stack Iniciado!${NC}"
echo "${GREEN}════════════════════════════════════════════════════════${NC}"
echo ""
echo "${CYAN}Acesse a interface do Locust:${NC}"
echo "  ${YELLOW}http://localhost:8089${NC}"
echo ""
echo "${CYAN}Dashboards de Monitoramento:${NC}"

if $IS_LOCAL; then
  echo "  Grafana:    ${YELLOW}http://localhost:3000${NC} (admin/admin)"
  echo "  Prometheus: ${YELLOW}http://localhost:9090${NC}"
  echo "  Jaeger:     ${YELLOW}http://localhost:16686${NC}"
  echo ""
fi

echo "${CYAN}Comandos úteis:${NC}"
echo "  Ver logs:       ${YELLOW}docker compose -f load_tests/docker-compose.loadtest.yml logs -f${NC}"
echo "  Parar teste:    ${YELLOW}docker compose -f load_tests/docker-compose.loadtest.yml down${NC}"
echo "  Ver status:     ${YELLOW}docker compose -f load_tests/docker-compose.loadtest.yml ps${NC}"
echo ""

# Follow logs
echo "${BLUE}Seguindo logs (Ctrl+C para sair)...${NC}"
echo ""
docker compose -f docker-compose.loadtest.yml logs -f
