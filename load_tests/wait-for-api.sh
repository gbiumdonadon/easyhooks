#!/bin/sh
#
# wait-for-api.sh - Wait for API to be ready before proceeding
#
# Description:
#   Polls the API health endpoint until it responds successfully or timeout is reached.
#   Used by loadtest-init container to ensure API is ready before creating tenants.
#
# Environment Variables:
#   LOADTEST_API_BASE_URL - Base URL of the API to check (required)
#   MAX_RETRIES           - Maximum number of retry attempts (default: 60)
#   RETRY_INTERVAL        - Seconds between retries (default: 2)
#
# Exit Codes:
#   0 - API is ready and responding
#   1 - Timeout reached or API not responding
#
# Example:
#   export LOADTEST_API_BASE_URL=http://localhost:8000
#   ./wait-for-api.sh

set -e

# Configuration
API_URL="${LOADTEST_API_BASE_URL}/health"
MAX_RETRIES="${MAX_RETRIES:-60}"
RETRY_INTERVAL="${RETRY_INTERVAL:-2}"

printf "Aguardando API estar pronta em %s...\n\n" "$API_URL"

# Retry loop
i=1
while [ "$i" -le "$MAX_RETRIES" ]; do
  if curl -sf "$API_URL" > /dev/null 2>&1; then
    printf "\n[OK] API esta pronta e respondendo!\n"
    exit 0
  fi

  remaining=$((MAX_RETRIES - i))
  printf "[%d/%d] API ainda nao responde... (%d tentativas restantes)\n" \
    "$i" "$MAX_RETRIES" "$remaining"

  sleep "$RETRY_INTERVAL"
  i=$((i + 1))
done

# Timeout reached
printf "\n[TIMEOUT] API nao ficou pronta em %ds\n" "$((MAX_RETRIES * RETRY_INTERVAL))"
printf "Verifique se a API esta rodando em: %s\n" "$LOADTEST_API_BASE_URL"
exit 1
