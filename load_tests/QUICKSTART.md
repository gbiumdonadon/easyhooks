# Load tests — quick start (k6)

1. Start the stack: `docker compose up -d` (from the repo root). Only Redis,
   API and worker are required — the monitoring stack is optional.
2. Set `LOADTEST_ADMIN_TOKEN` to the same value as `ADMIN_SEED_TOKEN`
   (defined in `.env`).
3. Run the baseline scenario via Docker (provisions the tenant pool
   automatically):

```bash
docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 run k6/scenarios/baseline.js
```

Or, if you have k6 installed locally:

```bash
cd load_tests
export LOADTEST_API_BASE_URL=http://localhost:8000
export LOADTEST_ADMIN_TOKEN="$ADMIN_SEED_TOKEN"
bash scripts/create_tenant_pool.sh
k6 run k6/scenarios/baseline.js
```

While the test runs, you can watch the queue depth from the host:

```bash
docker compose exec redis redis-cli XLEN events:in
docker compose exec redis redis-cli XPENDING events:in webhook-workers
docker compose exec redis redis-cli XLEN events:failed
```

## Rastreando falhas (`failure_tracking.js`)

Para investigar respostas não-202, timeouts e erros de conexão, rode o
cenário dedicado — ele injeta ~10% de requisições inválidas e grava cada
falha num stream Redis (`loadtest:dlq`):

```bash
docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 \
  run k6/scenarios/failure_tracking.js
```

Após o run, inspecione as entradas:

```bash
docker compose exec redis redis-cli XLEN loadtest:dlq
docker compose exec redis redis-cli XREVRANGE loadtest:dlq + - COUNT 20
# DLQ do worker (eventos que esgotaram retries — raramente cresce só por 4xx)
docker compose exec redis redis-cli XLEN events:failed
```

Tunable via env: `LOADTEST_FAILURE_RATIO` (default `0.10`),
`LOADTEST_DLQ_STREAM` (default `loadtest:dlq`),
`LOADTEST_DLQ_MAX_LEN` (default `50000`). Detalhes em
[README.md](./README.md#investigando-falhas-loadtestdlq).

See [README.md](./README.md) for all scenarios and tuning variables.
