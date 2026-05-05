# Load test stack status

The load test runner uses **Grafana k6** and shell-based tenant provisioning (`scripts/create_tenant_pool.sh`).

Use `bash load_tests/run-loadtest.sh <scenario>` from `load_tests/` to validate the environment and refresh the tenant pool, then run k6 as printed in the script output.

Scenarios: `baseline`, `throughput`, `websocket_scale`, `multi_tenant`, `stress`, `custom_scenario`.
