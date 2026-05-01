# Webhooks Platform

[🇺🇸 English version](README.md)

Plataforma multi-tenant de **ingestão, processamento idempotente e distribuição em tempo real** de webhooks. FastAPI + Kafka + Redis + Postgres, com WebSocket pub/sub para entrega push aos clientes finais.

> **Documentação completa do produto:** consulte o site Docusaurus em `http://localhost:3001` (sobe via `docker compose up -d`). Conteúdo organizado em Início Rápido, Referência da API, WebSockets e Erros/DLQ.

---

## Sumário

- [Arquitetura](#arquitetura)
- [Stack](#stack)
- [Pré-requisitos](#pré-requisitos)
- [Quick Start (5 minutos)](#quick-start-5-minutos)
- [URLs e portas](#urls-e-portas)
- [Estrutura de pastas](#estrutura-de-pastas)
- [Comandos úteis](#comandos-úteis)
- [Variáveis de ambiente](#variáveis-de-ambiente)
- [Desenvolvimento sem Docker (opcional)](#desenvolvimento-sem-docker-opcional)
- [Testes](#testes)
- [Editando a documentação](#editando-a-documentação)
- [Migrations (Alembic)](#migrations-alembic)
- [Troubleshooting](#troubleshooting)
- [Contribuição](#contribuição)
- [Licença](#licença)

---

## Arquitetura

```mermaid
flowchart LR
    Admin[Admin] -->|"POST /admin/tenants"| API[FastAPI app]
    Cliente[Cliente] -->|"POST /v1/webhooks/:id<br/>+ HMAC"| API
    API -->|"webhooks.inbound"| Kafka[(Kafka)]
    Kafka --> Worker[Worker]
    Worker -->|"PUBLISH<br/>tenant_events:id"| Redis[(Redis Pub/Sub)]
    Worker -->|"falha 3x"| DLQ[(webhooks.dlq)]
    Cliente -->|"POST /v1/tokens/:id"| API
    Cliente -->|"WS /ws/events/:id"| API
    Redis --> API
    API -->|"send_text"| Cliente
```

- **`app`** — FastAPI: Admin API, ingestor de webhooks, emissor de tokens WS, endpoint WebSocket.
- **`worker`** — Consumer Kafka dedicado: idempotência (Redis), retry exponencial, DLQ e pub/sub.
- **`docs`** — Site Docusaurus (Nginx servindo estáticos).
- **`db`** — Postgres 16 (tenants, admins).
- **`redis`** — Redis 7 (cache de credenciais, locks de idempotência, pub/sub).
- **`kafka`** — Kafka 3.7 (KRaft, single-broker para dev).

---

## Stack

| Camada | Tecnologia |
| --- | --- |
| Linguagem | Python 3.12 |
| Web framework | FastAPI + Uvicorn |
| ORM / Migrations | SQLAlchemy (async) + Alembic |
| Mensageria | Apache Kafka (`aiokafka`) |
| Cache / Pub-Sub | Redis 7 |
| Banco | PostgreSQL 16 |
| Testes | `pytest`, `pytest-asyncio`, `httpx`, `httpx-ws`, `testcontainers[kafka]` |
| Docs | Docusaurus 3 (Node 20 build → Nginx Alpine runtime) |
| Infra | Docker Compose |

---

## Pré-requisitos

- **Docker** ≥ 24 + **Docker Compose** v2.
- (Opcional para rodar fora do Docker) **Python 3.12+**, **Node 20+**.
- (Opcional, recomendado) **`make`**, **`curl`**, **`jq`**, **`openssl`**.

Para Windows, use **WSL2** ou **Docker Desktop**. Os comandos abaixo são portáveis (PowerShell, bash e zsh).

---

## Quick Start (5 minutos)

### 1. Clonar e configurar ambiente

```bash
git clone https://github.com/seuusuario/easyhook.git
cd easyhook

# Copiar template de ambiente e configurar
cp .env.example .env
```

**Importante:** Edite o `.env` e defina valores seguros para:
- `POSTGRES_PASSWORD`
- `ADMIN_SEED_TOKEN`
- `APP_SECRET_KEY`

Gerar valores aleatórios seguros:

```bash
# No Linux/macOS/WSL
openssl rand -hex 32

# No Windows PowerShell
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }))
```

### 2. Subir o stack

```bash
docker compose up -d
docker compose ps        # confirmar todos health=healthy
```

A primeira subida demora ~1-2 min (build da imagem Python + download das images base). Subidas seguintes são quase instantâneas.

### 3. Verificar que tudo subiu

- API: <http://localhost:8000/docs> (Swagger UI).
- Documentação: <http://localhost:3001>.
- Redis: `docker compose exec redis redis-cli ping` → `PONG`.
- Kafka: `docker compose exec kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --list`.

### 4. Criar seu primeiro tenant

```bash
curl -X POST http://localhost:8000/admin/tenants \
  -H "Authorization: Bearer <SEU_ADMIN_SEED_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Acme Inc."}'
```

Resposta:

```json
{
  "tenant_id": "f1a2b3c4-d5e6-7890-abcd-ef0123456789",
  "secret_key": "a-very-long-base64url-secret-..."
}
```

> O `secret_key` é mostrado **uma única vez**. Guarde-o.

### 5. Enviar seu primeiro evento

```bash
export TENANT_ID="<o tenant_id retornado>"
export SECRET="<o secret_key retornado>"

BODY='{"event":"order.created","data":{"id":1}}'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $2}')"

curl -i -X POST "http://localhost:8000/v1/webhooks/$TENANT_ID" \
  -H "X-Webhook-Signature: $SIG" \
  -H "X-Event-Id: evt-001" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

Resposta esperada: `HTTP/1.1 202 Accepted`.

### 6. Observar o worker processando

```bash
docker compose logs -f worker
```

Você verá algo como:

```
INFO  Acquired idempotency lock for event_id=evt-001 ...
INFO  Published event to tenant channel tenant_events:f1a2b3c4-...
```

Para detalhes (HMAC, WebSocket, DLQ, exemplos em outras linguagens) veja a doc em <http://localhost:3001>.

---

## URLs e portas

| Serviço | URL local | Porta interna | Descrição |
| --- | --- | --- | --- |
| API (Swagger UI) | <http://localhost:8000/docs> | 8000 | OpenAPI interativo |
| API (ReDoc) | <http://localhost:8000/redoc> | 8000 | Documentação alternativa |
| API root | <http://localhost:8000/> | 8000 | FastAPI |
| Documentação | <http://localhost:3001> | 80 | Site Docusaurus (Nginx) |
| Postgres | localhost:5432 | 5432 | usuário do .env |
| Redis | localhost:6379 | 6379 | sem auth (dev) |
| Kafka | localhost:9092 | 9092 | listener PLAINTEXT |

---

## Estrutura de pastas

```
.
├── src/                       # Código da aplicação
│   ├── main.py                # Entry FastAPI
│   ├── worker.py              # Entry Kafka consumer
│   ├── config.py              # Settings (Pydantic)
│   ├── database.py            # Engine + session factory
│   ├── redis_client.py        # Redis async client + DI
│   ├── security.py            # Hash/verify secret + HMAC
│   ├── dependencies.py        # Auth dependencies (admin, tenant)
│   ├── models/                # SQLAlchemy models
│   ├── schemas/               # Pydantic schemas
│   ├── routers/               # FastAPI routers
│   │   ├── admin.py           # POST /admin/tenants
│   │   ├── webhooks.py        # POST /v1/webhooks/{id}
│   │   ├── tokens.py          # POST /v1/tokens/{id}
│   │   └── ws.py              # WS /ws/events/{id}
│   └── services/              # Lógica de negócio
│       ├── tenant_service.py
│       ├── webhook_service.py
│       ├── webhook_processor.py
│       ├── kafka_producer.py
│       ├── pubsub.py
│       └── ws_token.py
├── tests/                     # pytest (29 testes em 6 grupos)
├── alembic/                   # Migrations
├── scripts/
│   └── seed_admin.py          # Idempotente: cria superadmin
├── docs/                      # Site Docusaurus
│   ├── docs/                  # Conteúdo .md (intro + 4 categorias)
│   ├── docusaurus.config.js
│   ├── sidebars.js
│   ├── package.json
│   └── Dockerfile             # Multi-stage build → Nginx
├── work/                      # Specs originais dos grupos 1-6
├── docker-compose.yml
├── Dockerfile                 # Imagem Python (app + worker)
├── pyproject.toml
└── README.md
```

---

## Comandos úteis

### Stack inteiro

```bash
docker compose up -d                  # subir tudo em background
docker compose ps                     # status
docker compose logs -f app worker     # logs em tempo real (api+worker)
docker compose restart app            # reiniciar só a API
docker compose down                   # parar (mantém volumes)
docker compose down -v                # parar e apagar dados (Postgres, Kafka)
```

### Subir apenas alguns serviços

```bash
docker compose up -d db redis kafka   # infra (para rodar app/worker fora)
docker compose up -d docs             # apenas documentação
docker compose up -d app worker       # API + worker (requer infra de pé)
```

### Rebuild

```bash
docker compose build --no-cache app worker docs
docker compose up -d --force-recreate app
```

### Inspecionar Kafka

```bash
docker compose exec kafka /opt/kafka/bin/kafka-topics.sh \
  --bootstrap-server localhost:9092 --list

docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 --topic webhooks.inbound \
  --from-beginning --property print.headers=true

docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 --topic webhooks.dlq \
  --from-beginning --property print.headers=true
```

### Inspecionar Redis

```bash
docker compose exec redis redis-cli

# dentro do CLI:
KEYS tenant_*
GET tenant_hmac_key:<tenant_id>
KEYS event_lock:*
PUBSUB CHANNELS tenant_events:*
```

### Inspecionar Postgres

```bash
docker compose exec db psql -U webhooks -d webhooks
\dt           # listar tabelas
SELECT * FROM tenants;
SELECT * FROM admin_users;
```

---

## Variáveis de ambiente

Todas as variáveis podem ser configuradas via arquivo `.env` na raiz do projeto. Copie `.env.example` para `.env` e customize.

| Variável | Descrição | Exemplo |
| --- | --- | --- |
| `DATABASE_URL` | String de conexão PostgreSQL | `postgresql+asyncpg://webhooks:senha@db:5432/webhooks` |
| `POSTGRES_USER` | Usuário PostgreSQL | `webhooks` |
| `POSTGRES_PASSWORD` | Senha PostgreSQL | `changeme123` |
| `POSTGRES_DB` | Nome do banco PostgreSQL | `webhooks` |
| `REDIS_URL` | String de conexão Redis | `redis://redis:6379/0` |
| `KAFKA_BOOTSTRAP_SERVERS` | Endereços dos brokers Kafka | `kafka:9092` |
| `KAFKA_WEBHOOK_TOPIC` | Tópico de webhooks de entrada | `webhooks.inbound` |
| `KAFKA_DLQ_TOPIC` | Tópico de dead letter queue | `webhooks.dlq` |
| `KAFKA_CONSUMER_GROUP` | ID do grupo de consumidores | `webhook-workers` |
| `WORKER_MAX_RETRIES` | Máximo de tentativas antes da DLQ | `3` |
| `WORKER_BACKOFF_BASE_MS` | Base do backoff exponencial (ms) | `100` |
| `IDEMPOTENCY_TTL_SECONDS` | TTL do lock de idempotência | `86400` |
| `ADMIN_SEED_TOKEN` | Token bootstrap do admin (DEVE MUDAR) | `change-this-to-a-secure-random-token` |
| `APP_SECRET_KEY` | Chave para assinar tokens WS (DEVE MUDAR) | `change-this-to-a-secure-random-key` |
| `WS_TOKEN_TTL_SECONDS` | TTL do token de WebSocket | `300` |
| `TENANT_EVENTS_CHANNEL_PREFIX` | Prefixo dos canais Pub/Sub | `tenant_events:` |
| `TENANT_EVENTS_STREAM_PREFIX` | Prefixo dos streams Redis | `stream:tenant:` |
| `STREAM_MAX_LEN` | Tamanho máximo do stream | `1000` |
| `STREAM_HISTORY_COUNT` | Contagem de histórico na conexão | `50` |
| `CORS_ORIGINS` | Origens CORS permitidas | `http://localhost:3001,http://localhost:3000` |
| `SECRET_KEY_BYTES` | Entropia do secret gerado para tenant | `32` |

> **Produção:** sempre defina `ADMIN_SEED_TOKEN` e `APP_SECRET_KEY` via secret manager. Rotacione antes de promover.

---

## Desenvolvimento sem Docker (opcional)

Recomendado para iteração rápida em código Python (hot reload via uvicorn).

### 1. Subir só a infra com Docker

```bash
docker compose up -d db redis kafka
```

### 2. Criar venv e instalar deps

```bash
python -m venv .venv
source .venv/bin/activate          # bash/zsh
.venv\Scripts\Activate.ps1         # PowerShell
pip install -e ".[dev]"
```

### 3. Configurar variáveis (apontar para localhost)

```bash
export DATABASE_URL="postgresql+asyncpg://webhooks:changeme123@localhost:5432/webhooks"
export REDIS_URL="redis://localhost:6379/0"
export KAFKA_BOOTSTRAP_SERVERS="localhost:9092"
export ADMIN_SEED_TOKEN="<seu-token-seguro>"
export APP_SECRET_KEY="<sua-chave-segura>"
```

### 4. Migrations + seed + run

```bash
alembic upgrade head
python scripts/seed_admin.py

# Terminal 1: API
uvicorn src.main:app --reload --port 8000

# Terminal 2: Worker
python -m src.worker
```

---

## Testes

A suite tem **29 testes** distribuídos em 6 grupos:

- **Group 1 — Governance** (5): admin auth, criação de tenant, sync com Redis.
- **Group 2 — Security** (5): isolamento multi-tenant, Bearer + HMAC.
- **Group 3 — Ingestion** (3): produção em Kafka, headers, validação `X-Event-Id`.
- **Group 4 — Idempotency** (1): lock no Redis evita reprocessamento.
- **Group 5 — Resilience** (2): retry exponencial e DLQ após esgotar tentativas.
- **Group 6 — Distribution** (13): tokens HMAC, WebSocket, Pub/Sub end-to-end.

```bash
# Rodar tudo
pytest

# Rodar um grupo
pytest tests/test_group_2_security.py -v

# Rodar com cobertura
pytest --cov=src --cov-report=term-missing

# Apenas um teste
pytest tests/test_group_4_idempotency.py::test_should_skip_already_processed_event -v
```

> Os grupos 4-6 usam **`testcontainers[kafka]`**, que sobe um broker Kafka real efêmero. Requer Docker rodando. Tempo total da suite: ~50s.

---

## Editando a documentação

A documentação fica em [`docs/docs/`](docs/docs/) (markdown puro com frontmatter Docusaurus).

### Hot reload em modo dev

```bash
cd docs
npm install        # primeira vez
npm start          # http://localhost:3000 com hot reload
```

### Validar build de produção

```bash
cd docs
npm run build      # gera docs/build/
npm run serve      # serve docs/build/ em http://localhost:3000
```

### Subir como container

```bash
docker compose up -d --build docs   # disponível em http://localhost:3001
```

### Adicionar uma página

1. Crie `docs/docs/<categoria>/<slug>.md` com frontmatter:

   ```markdown
   ---
   id: meu-slug
   title: Meu Título
   sidebar_position: 99
   ---

   conteúdo...
   ```

2. Atualize [`docs/sidebars.js`](docs/sidebars.js) adicionando `'<categoria>/<slug>'` na lista da categoria.
3. Rebuild: `cd docs && npm run build`.

---

## Migrations (Alembic)

```bash
# Aplicar todas as migrations pendentes
alembic upgrade head

# Criar nova migration baseada nos models
alembic revision --autogenerate -m "add column foo to tenants"

# Reverter última
alembic downgrade -1

# Ver histórico
alembic history
```

Dentro do Docker, o `app` roda `alembic upgrade head` automaticamente no `command:` do compose.

---

## Troubleshooting

### `app` não sobe — `connection refused` ao Postgres/Kafka

Compose espera healthchecks. Verifique:

```bash
docker compose ps
docker compose logs db
docker compose logs kafka
```

Kafka demora ~15-30s para ficar healthy na primeira subida. Aguarde.

### `403 Forbidden` ao postar webhook

Causas mais comuns:

1. HMAC calculado sobre body diferente do enviado (ex.: `echo` adicionou `\n`). Use `printf '%s'`.
2. Bearer token não confere com o `tenant_id` da URL (cross-tenant).
3. Secret tem caracteres especiais não escapados no shell.

Veja a [doc de Segurança HMAC](http://localhost:3001/api-reference/seguranca-hmac) para detalhes.

### `400 Bad Request — Missing required header X-Event-Id`

O header `X-Event-Id` é obrigatório (idempotência). Sempre envie um UUID/ULID único.

### Worker em loop de erro / muitas mensagens na DLQ

Inspecione:

```bash
docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 --topic webhooks.dlq \
  --from-beginning --property print.headers=true
```

O header `x-original-error` indica o motivo da última falha.

### Reset completo do estado

```bash
docker compose down -v   # apaga volumes (Postgres + Kafka)
docker compose up -d
```

### Build do docs falha por falta de memória

Aumente recursos do Docker Desktop (Settings → Resources → Memory ≥ 2 GB).

### Porta 3001/8000 já em uso

Pare o processo conflitante ou ajuste o port mapping no [`docker-compose.yml`](docker-compose.yml):

```yaml
ports:
  - "8001:8000"   # mapeia 8001 do host → 8000 do container
```

---

## Contribuição

### Workflow TDD

Este projeto foi implementado seguindo TDD estrito (Red → Green → Refactor). Mantenha o padrão:

1. Antes de adicionar feature, escreva o teste em `tests/test_group_<N>_<tema>.py`.
2. Rode `pytest tests/test_group_X.py -v` e veja falhar.
3. Implemente o mínimo para passar.
4. Refatore mantendo verde.

### Padrões de código

- **Type hints obrigatórios** em assinaturas públicas.
- **Async-first**: tudo IO-bound usa `async/await`.
- **Sem comentários óbvios** — comente apenas decisões não triviais.
- **Pydantic** para schemas (request/response).
- **SQLAlchemy 2.0 style** (`select(...)`, sessão async).

### Antes de abrir PR

```bash
pytest                         # 29/29 verde
cd docs && npm run build       # build limpo
```

### Estrutura de commit

```
<tipo>: <resumo curto>

<corpo opcional explicando "por quê">
```

Tipos comuns: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`.

---

## Licença

Este projeto está licenciado sob a Apache License 2.0 - veja o arquivo [LICENSE](LICENSE) para detalhes.

```
Copyright © 2026 Gustavo Bium Donadon
```

---

## Disclaimer

Este projeto é disponibilizado "como está" para fins de estudo e uso livre. Embora implemente padrões prontos para produção (idempotência, lógica de retry, DLQ, multi-tenancy), é destinado principalmente para fins educacionais e como ponto de partida para implementações de infraestrutura de webhooks.

**Use em produção por sua conta e risco.** Sempre realize auditorias completas de segurança, testes de carga e customize o sistema para seus requisitos específicos antes de implantar em ambientes de produção.

---

## Recursos

- **Doc do produto:** <http://localhost:3001> (após `docker compose up -d docs`)
- **Swagger UI:** <http://localhost:8000/docs>
- **Specs originais:** [`work/`](work/) — uma `.md` por grupo (1-6) descrevendo o requisito.

---

**Construído com ❤️ usando FastAPI, Kafka, Redis e PostgreSQL**
