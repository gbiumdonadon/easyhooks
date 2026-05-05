# Makefile for EasyHooks load testing (k6 + Docker helpers)
#
# Prerequisites:
#   - Docker and Docker Compose installed
#   - LOADTEST_ADMIN_TOKEN environment variable set
#
# Quick Start:
#   make loadtest-validate    # Validate environment
#   make loadtest-local       # Run baseline test
#
# For more control, use the scripts directly:
#   cd load_tests && ./run-loadtest.sh baseline --tenant-count 100

.PHONY: help
help: ## Show this help message
	@echo "EasyHooks Load Testing Commands"
	@echo "================================"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}'
	@echo ""

# ============================================================================
# Environment Setup
# ============================================================================

.PHONY: loadtest-validate
loadtest-validate: ## Validate load testing environment
	@cd load_tests && bash validate-env.sh

.PHONY: loadtest-env
loadtest-env: ## Create .env.loadtest from example
	@if [ ! -f load_tests/.env.loadtest ]; then \
		cp load_tests/.env.loadtest.example load_tests/.env.loadtest; \
		echo "✓ Created load_tests/.env.loadtest"; \
		echo "  Edit this file to configure your environment"; \
	else \
		echo "✓ load_tests/.env.loadtest already exists"; \
	fi

# ============================================================================
# Running Tests
# ============================================================================

.PHONY: loadtest-local
loadtest-local: ## Run baseline test against local API
	@cd load_tests && bash run-loadtest.sh baseline

.PHONY: loadtest-baseline
loadtest-baseline: loadtest-local ## Alias for loadtest-local

.PHONY: loadtest-throughput
loadtest-throughput: ## Run throughput test (high load)
	@cd load_tests && bash run-loadtest.sh throughput

.PHONY: loadtest-ws-scale
loadtest-ws-scale: ## Run WebSocket scale test (10k connections)
	@cd load_tests && bash run-loadtest.sh websocket_scale

.PHONY: loadtest-multi-tenant
loadtest-multi-tenant: ## Run multi-tenant isolation test
	@cd load_tests && bash run-loadtest.sh multi_tenant

.PHONY: loadtest-stress
loadtest-stress: ## Run stress test (breaking point)
	@cd load_tests && bash run-loadtest.sh stress

.PHONY: loadtest-custom
loadtest-custom: ## Run custom scenario
	@cd load_tests && bash run-loadtest.sh custom_scenario

# ============================================================================
# Management
# ============================================================================

.PHONY: loadtest-ui
loadtest-ui: ## Hint: k6 has no web UI — open Grafana load-test dashboard
	@echo "k6 is CLI-only. Open Grafana: http://localhost:3000/d/loadtest-overview"
	@open http://localhost:3000/d/loadtest-overview 2>/dev/null || xdg-open http://localhost:3000/d/loadtest-overview 2>/dev/null || start http://localhost:3000/d/loadtest-overview 2>/dev/null || true

.PHONY: loadtest-logs
loadtest-logs: ## Show last k6 / init compose logs (if any)
	@cd load_tests && docker compose -f docker-compose.loadtest.yml logs --tail=200 2>/dev/null || true

.PHONY: loadtest-status
loadtest-status: ## Show status of load test containers
	@cd load_tests && docker compose -f docker-compose.loadtest.yml ps

.PHONY: loadtest-stop
loadtest-stop: ## Stop load test containers
	@cd load_tests && docker compose -f docker-compose.loadtest.yml down
	@echo "✓ Load test containers stopped"

.PHONY: loadtest-clean
loadtest-clean: loadtest-stop ## Stop containers and clean tenant pool
	@rm -f load_tests/.tenant_pool.json
	@echo "✓ Tenant pool cleaned"

.PHONY: loadtest-restart
loadtest-restart: loadtest-stop loadtest-local ## Restart load test

# ============================================================================
# Tenant Management
# ============================================================================

.PHONY: loadtest-tenants-create
loadtest-tenants-create: ## Create tenant pool (50 tenants via curl)
	@cd load_tests && bash scripts/create_tenant_pool.sh

.PHONY: loadtest-tenants-list
loadtest-tenants-list: ## Show tenant pool JSON (requires jq)
	@test -f load_tests/.tenant_pool.json && jq 'length' load_tests/.tenant_pool.json || echo "No load_tests/.tenant_pool.json"

.PHONY: loadtest-tenants-clean
loadtest-tenants-clean: ## Remove tenant pool file
	@rm -f load_tests/.tenant_pool.json
	@echo "✓ Tenant pool removed"

# ============================================================================
# Monitoring & Dashboards
# ============================================================================

.PHONY: loadtest-grafana
loadtest-grafana: ## Open Grafana dashboard
	@echo "Opening Grafana..."
	@open http://localhost:3000 2>/dev/null || xdg-open http://localhost:3000 2>/dev/null || start http://localhost:3000 2>/dev/null || echo "Please open http://localhost:3000 in your browser"

.PHONY: loadtest-prometheus
loadtest-prometheus: ## Open Prometheus
	@echo "Opening Prometheus..."
	@open http://localhost:9090 2>/dev/null || xdg-open http://localhost:9090 2>/dev/null || start http://localhost:9090 2>/dev/null || echo "Please open http://localhost:9090 in your browser"

.PHONY: loadtest-jaeger
loadtest-jaeger: ## Open Jaeger traces
	@echo "Opening Jaeger..."
	@open http://localhost:16686 2>/dev/null || xdg-open http://localhost:16686 2>/dev/null || start http://localhost:16686 2>/dev/null || echo "Please open http://localhost:16686 in your browser"

# ============================================================================
# Development
# ============================================================================

.PHONY: loadtest-build
loadtest-build: ## Rebuild k6 Docker image (load_tests/Dockerfile)
	@cd load_tests && docker compose -f docker-compose.loadtest.yml build

.PHONY: loadtest-shell
loadtest-shell: ## Open shell in k6 image (debug)
	@cd load_tests && docker compose -f docker-compose.loadtest.yml run --rm --entrypoint sh k6 -c "echo k6 image ready; sh"

# ============================================================================
# Quick Start Workflow
# ============================================================================

.PHONY: loadtest-quick
loadtest-quick: loadtest-validate loadtest-local ## Validate and run baseline test

.PHONY: loadtest-full
loadtest-full: loadtest-validate loadtest-baseline loadtest-throughput ## Run full test suite

# Set default target
.DEFAULT_GOAL := help
