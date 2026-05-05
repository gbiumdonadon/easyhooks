# Load tests — quick start (k6)

1. Start the stack: `docker compose up -d` (repo root).
2. Set `LOADTEST_ADMIN_TOKEN` to the same value as `ADMIN_SEED_TOKEN` (e.g. in `.env`).
3. Create tenants and run baseline:

```bash
cd load_tests
export LOADTEST_API_BASE_URL=http://localhost:8000
export LOADTEST_ADMIN_TOKEN="$ADMIN_SEED_TOKEN"
bash scripts/create_tenant_pool.sh
k6 run k6/scenarios/baseline.js
```

Or use Docker only:

```bash
docker compose -f load_tests/docker-compose.loadtest.yml run --rm loadtest-init
docker compose -f load_tests/docker-compose.loadtest.yml run --rm --no-deps \
  -e TENANT_POOL_FILE=/load_tests/.tenant_pool.json \
  k6 run k6/scenarios/baseline.js
```

See [README.md](./README.md) for all scenarios and tuning variables.
