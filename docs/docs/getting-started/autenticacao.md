---
id: autenticacao
title: Autenticação
sidebar_position: 1
description: Como obter o ADMIN_SEED_TOKEN e gerar credenciais (API_KEY/SECRET) através da Admin API.
---

# Autenticação

A Easyhooks usa um modelo de **dois níveis de credenciais**:

1. **Admin** — administradores criam e gerenciam tenants. A autenticação é feita via **Bearer token** definido pela variável `ADMIN_SEED_TOKEN`.
2. **Tenant** — cada tenant recebe um `tenant_id` (UUID) e um `secret_key` (string opaca de alta entropia) que é usado para autenticar requisições no ingestor (HMAC ou Bearer) e para emitir tokens de WebSocket.

> O `secret_key` é mostrado **apenas uma vez** no momento da criação. Guarde-o em um cofre (ex.: HashiCorp Vault, AWS Secrets Manager). A plataforma armazena apenas o hash `bcrypt` para verificação Bearer e o secret bruto criptografado em Redis para verificação HMAC.

## 1. Obtendo o `ADMIN_SEED_TOKEN`

O token administrativo é semeado no banco pelo script [`scripts/seed_admin.py`](https://github.com/) automaticamente na primeira subida do container `app` quando você fornece a variável `ADMIN_SEED_TOKEN`.

Configure no arquivo `.env`:

```bash
ADMIN_SEED_TOKEN=<seu-token-seguro-aqui>
```

Em produção, defina via secret manager / variável de ambiente segura. Para gerar um token seguro:

```bash
# Linux/macOS/WSL
openssl rand -hex 32

# Windows PowerShell
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }))
```

## 2. Subindo o ambiente

```bash
docker compose up -d
```

Isso inicializa Postgres, Redis, Kafka, a API (`app`) e o `worker` consumidor.

Aguarde 5-10 segundos para os healthchecks ficarem verdes:

```bash
docker compose ps
```

## 3. Criando seu primeiro tenant

Use o `ADMIN_SEED_TOKEN` para chamar `POST /admin/tenants`:

```bash
export ADMIN_SEED_TOKEN="<seu-admin-token-do-env>"

curl -X POST http://localhost:8000/admin/tenants \
  -H "Authorization: Bearer $ADMIN_SEED_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Acme Inc."}'
```

Resposta esperada (`201 Created`):

```json
{
  "tenant_id": "f1a2b3c4-d5e6-7890-abcd-ef0123456789",
  "secret_key": "a-very-long-base64url-secret-that-only-appears-here"
}
```

Anote os dois valores. Você vai usá-los em todas as próximas requisições.

## 4. Definindo variáveis de ambiente

Para facilitar os exemplos, exporte:

```bash
export TENANT_ID="f1a2b3c4-d5e6-7890-abcd-ef0123456789"
export SECRET="a-very-long-base64url-secret-that-only-appears-here"
```

Pronto, agora siga para [Primeiro Evento](./primeiro-evento.md) para enviar um webhook de verdade.
