---
id: authentication
title: Authentication
sidebar_position: 1
description: How to obtain ADMIN_SEED_TOKEN and generate credentials (API_KEY/SECRET) through the Admin API.
---

# Authentication

The Easyhooks uses a **two-level credentials** model:

1. **Admin** — administrators create and manage tenants. Authentication is done via **Bearer token** defined by the `ADMIN_SEED_TOKEN` variable.
2. **Tenant** — each tenant receives a `tenant_id` (UUID) and a `secret_key` (high-entropy opaque string) that is used to authenticate requests to the ingestor (HMAC or Bearer) and to issue WebSocket tokens.

> The `secret_key` is shown **only once** at creation time. Store it in a vault (e.g., HashiCorp Vault, AWS Secrets Manager). The platform stores only the `bcrypt` hash under `tenant_auth:{id}` (used for Bearer verification) and the raw secret under `tenant_hmac_key:{id}` (used for HMAC verification). Both keys live in Redis with no TTL — the AOF + RDB persistence ensures they survive restarts.

## 1. Getting the `ADMIN_SEED_TOKEN`

On the first startup of the `app` container, the Go API runs
`redisstore.SeedSuperAdmin` (see `go-api/cmd/api/main.go`). It writes the
bcrypt hash of `ADMIN_SEED_TOKEN` into the single Redis key
`admin:token_hash` if it is not already present. Subsequent boots are no-ops,
so changing the env var at runtime does not silently rotate the password —
delete the key and restart the API to force a re-seed.

Configure in the `.env` file:

```bash
ADMIN_SEED_TOKEN=<your-secure-token-here>
```

In production, set via secret manager / secure environment variable. To generate a secure token:

```bash
# Linux/macOS/WSL
openssl rand -hex 32

# Windows PowerShell
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }))
```

## 2. Starting the environment

```bash
docker compose up -d
```

This brings up Redis (with AOF + RDB persistence), the API (`app`) and the
`worker` consumer. The optional Prometheus/Grafana/Jaeger stack lives in
`docker-compose.monitoring.yml` and is not required for development.

Wait a couple of seconds for healthchecks to become green:

```bash
docker compose ps
```

## 3. Creating your first tenant

Use the `ADMIN_SEED_TOKEN` to call `POST /admin/tenants`:

```bash
export ADMIN_SEED_TOKEN="<your-admin-token-from-env>"

curl -X POST http://localhost:8000/admin/tenants \
  -H "Authorization: Bearer $ADMIN_SEED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Acme Inc."}'
```

Expected response (`201 Created`):

```json
{
  "tenant_id": "f1a2b3c4-d5e6-7890-abcd-ef0123456789",
  "secret_key": "a-very-long-base64url-secret-that-only-appears-here"
}
```

Write down both values. You'll use them in all subsequent requests.

## 4. Setting environment variables

To simplify examples, export:

```bash
export TENANT_ID="f1a2b3c4-d5e6-7890-abcd-ef0123456789"
export SECRET="a-very-long-base64url-secret-that-only-appears-here"
```

Ready! Now proceed to [First Event](./first-event.md) to send a real webhook.
