#!/bin/bash
#
# validate-env.sh - Validate environment before running load tests
#
# Description:
#   Checks all prerequisites needed for load testing including:
#   - Required environment variables
#   - API connectivity and health
#   - Docker and Docker Compose availability
#   - Tenant pool status
#
# Environment Variables:
#   LOADTEST_API_BASE_URL - Base URL of the API (required)
#   LOADTEST_ADMIN_TOKEN  - Admin token for authentication (required)
#
# Exit Codes:
#   0 - All validations passed
#   1 - One or more validations failed
#
# Example:
#   export LOADTEST_API_BASE_URL=http://localhost:8000
#   export LOADTEST_ADMIN_TOKEN=my-token
#   ./validate-env.sh

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

ERRORS=0

echo ""
echo "${BLUE}════════════════════════════════════════════════════════${NC}"
echo "${BLUE}  Load Test Environment Validation${NC}"
echo "${BLUE}════════════════════════════════════════════════════════${NC}"
echo ""

# Check required environment variables
echo "${YELLOW}[1/5]${NC} Verificando variáveis de ambiente..."

if [[ -z "$LOADTEST_API_BASE_URL" ]]; then
  echo "  ${RED}❌ LOADTEST_API_BASE_URL não está configurada${NC}"
  echo "     Defina: export LOADTEST_API_BASE_URL=http://localhost:8000"
  ERRORS=$((ERRORS + 1))
else
  echo "  ${GREEN}✓${NC} LOADTEST_API_BASE_URL: $LOADTEST_API_BASE_URL"
fi

if [[ -z "$LOADTEST_ADMIN_TOKEN" ]]; then
  echo "  ${RED}❌ LOADTEST_ADMIN_TOKEN não está configurada${NC}"
  echo "     Defina: export LOADTEST_ADMIN_TOKEN=your-token"
  ERRORS=$((ERRORS + 1))
else
  echo "  ${GREEN}✓${NC} LOADTEST_ADMIN_TOKEN: configurado (${#LOADTEST_ADMIN_TOKEN} chars)"
fi

echo ""

# Check Docker availability
echo "${YELLOW}[2/5]${NC} Verificando Docker..."

if ! command -v docker &> /dev/null; then
  echo "  ${RED}❌ Docker não está instalado${NC}"
  ERRORS=$((ERRORS + 1))
else
  DOCKER_VERSION=$(docker --version)
  echo "  ${GREEN}✓${NC} Docker disponível: $DOCKER_VERSION"
fi

if ! command -v docker compose &> /dev/null; then
  echo "  ${RED}❌ Docker Compose não está disponível${NC}"
  echo "     Instale o Docker Compose ou atualize para Docker Desktop recente"
  ERRORS=$((ERRORS + 1))
else
  COMPOSE_VERSION=$(docker compose version)
  echo "  ${GREEN}✓${NC} Docker Compose disponível: $COMPOSE_VERSION"
fi

echo ""

# Check API health
echo "${YELLOW}[3/5]${NC} Verificando saúde da API..."

if [[ -n "$LOADTEST_API_BASE_URL" ]]; then
  HEALTH_URL="${LOADTEST_API_BASE_URL}/health"
  
  if command -v curl &> /dev/null; then
    if HEALTH_RESPONSE=$(curl -sf "$HEALTH_URL" 2>&1); then
      echo "  ${GREEN}✓${NC} API está respondendo: $HEALTH_URL"
      echo "     Response: $HEALTH_RESPONSE"
    else
      echo "  ${RED}❌ API não está respondendo em $HEALTH_URL${NC}"
      echo "     Se a API for local, certifique-se de que está rodando:"
      echo "     docker compose up -d"
      ERRORS=$((ERRORS + 1))
    fi
  else
    echo "  ${YELLOW}⚠${NC}  curl não está disponível, pulando verificação de health"
  fi
fi

echo ""

# Check tenant pool
echo "${YELLOW}[4/5]${NC} Verificando tenant pool..."

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
TENANT_POOL_FILE="$SCRIPT_DIR/.tenant_pool.json"

if [[ -f "$TENANT_POOL_FILE" ]]; then
  TENANT_COUNT=$(cat "$TENANT_POOL_FILE" | grep -o '"tenant_id"' | wc -l)
  echo "  ${GREEN}✓${NC} Tenant pool existe com $TENANT_COUNT tenants"
  
  if [[ $TENANT_COUNT -lt 10 ]]; then
    echo "  ${YELLOW}⚠${NC}  Poucos tenants ($TENANT_COUNT). Recomendado: pelo menos 50"
  fi
else
  echo "  ${YELLOW}⚠${NC}  Tenant pool não existe em $TENANT_POOL_FILE"
  echo "     Crie com: cd load_tests && bash scripts/create_tenant_pool.sh"
fi

echo ""

# Check Docker daemon
echo "${YELLOW}[5/5]${NC} Verificando Docker daemon..."

if docker info &> /dev/null; then
  echo "  ${GREEN}✓${NC} Docker daemon está rodando"
else
  echo "  ${RED}❌ Docker daemon não está acessível${NC}"
  echo "     Inicie o Docker Desktop ou docker daemon"
  ERRORS=$((ERRORS + 1))
fi

echo ""
echo "${BLUE}════════════════════════════════════════════════════════${NC}"

# Final result
if [[ $ERRORS -gt 0 ]]; then
  echo ""
  echo "${RED}❌ Validação falhou com $ERRORS erro(s)${NC}"
  echo "   Corrija os problemas acima antes de executar os testes."
  echo ""
  exit 1
else
  echo ""
  echo "${GREEN}✅ Ambiente validado com sucesso!${NC}"
  echo "   Tudo pronto para executar testes de carga."
  echo ""
  exit 0
fi
