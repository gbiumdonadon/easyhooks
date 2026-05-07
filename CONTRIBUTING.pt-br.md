# Contribuindo com o EasyHooks

[🇺🇸 English version](CONTRIBUTING.md)

Primeiramente, obrigado por se interessar em contribuir com o **EasyHooks**!
Este projeto visa ser uma plataforma multi-tenant de alto desempenho para
**ingestão, processamento idempotente e distribuição em tempo real** de
webhooks — e a sua ajuda é fundamental para mantê-lo afiado.

Como performance e confiabilidade são objetivos centrais, seguimos algumas
diretrizes para manter o código limpo, eficiente e previsível.

---

## Sumário

- [Como você pode contribuir?](#-como-você-pode-contribuir)
- [Workflow de Desenvolvimento](#-workflow-de-desenvolvimento)
- [Padrões de Pull Request](#-padrões-de-pull-request)
- [Padrões de Código](#-padrões-de-código)
- [Código de Conduta](#-código-de-conduta)

---

## 🛠 Como você pode contribuir?

Além de revisões e sugestões de testes, aqui estão as principais formas de
ajudar:

### 1. Revisão e otimização de código

Buscamos sempre a **menor latência** e o **menor consumo de memória**. Se
você encontrar um gargalo — loops quentes, alocações desnecessárias,
contenção de locks ou uso subótimo do pipeline do Redis — abra uma Issue ou
um PR.

São especialmente bem-vindas contribuições em:

- Reduzir alocações por requisição no caminho de ingestão
  (`/v1/webhooks/:id`).
- Otimizar o uso do cliente Redis (pipelines, batching de
  `XADD`/`XREADGROUP`).
- Melhorar o lock de idempotência e a lógica de retry/backoff do worker.
- Eficiência do fan-out via WebSocket (streams por tenant e buffers dos
  subscribers).

### 2. Cenários de teste e benchmarks

O EasyHooks precisa ser resiliente. Contribuições são bem-vindas em:

- **Testes de carga** — novos cenários em `load_tests/k6/` (Grafana k6).
- **Chaos Engineering** — cenários que testem a resiliência quando o Redis
  fica lento, derruba conexões ou se recupera de um restart.
- **Casos de borda** — testes unitários (`go-api/**/*_test.go`) para
  payloads malformados, HMAC inválido, corridas de idempotência,
  instabilidades de rede e caminhos de DLQ.
- **Benchmarks de capacidade** — extender o
  `load_tests/scripts/run_capacity_benchmark.ps1` ou adicionar scripts
  shell equivalentes.

### 3. Documentação e exemplos

- Melhorar `README.md`, `README.pt-br.md` e o site Docusaurus em `docs/`.
- Criar novos guias de "Getting Started" ou tutoriais.
- Exemplos conceituais de SDK em diferentes linguagens (Node.js, Python,
  Go, etc.) mostrando como assinar e enviar webhooks (HMAC) ou consumir
  o fan-out via WebSocket.

### 4. Triagem de Issues

Ajudar a reproduzir bugs reportados por outros usuários, rotular issues e
confirmar se os problemas ainda existem na `main` mantém o backlog
saudável.

---

## 🚀 Workflow de Desenvolvimento

### Setup local

1. Faça o **fork** do repositório.
2. **Clone** o seu fork:
   ```bash
   git clone https://github.com/<seu-usuario>/easyhooks.git
   cd easyhooks
   ```
3. **Configure o ambiente**:
   ```bash
   cp .env.example .env
   # Edite o .env e defina ADMIN_SEED_TOKEN e APP_SECRET_KEY (veja README.pt-br.md)
   ```
4. **Suba as dependências** (Redis + API + worker + docs):
   ```bash
   docker compose up -d
   docker compose ps
   ```
5. **Verifique a stack**:
   - API health: <http://localhost:8000/health>
   - Redis: `docker compose exec redis redis-cli ping` → `PONG`
6. **Garanta que todos os testes atuais passam** antes de mudar qualquer
   coisa:
   ```bash
   cd go-api
   go test ./...
   go test -race ./...
   ```

### Opcional: observabilidade e testes de carga

```bash
# Stack de observabilidade (Prometheus, Grafana, Jaeger, redis-exporter)
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d

# Testes de carga (Grafana k6)
docker compose -f load_tests/docker-compose.loadtest.yml run --rm k6 \
  run k6/scenarios/baseline.js
```

Veja `load_tests/README.md` para o guia completo de testes de carga.

---

## 📦 Padrões de Pull Request

Ao abrir um PR, por favor:

- **Mantenha-o atômico.** Um PR deve resolver apenas um problema ou
  adicionar uma única funcionalidade. Quebre refatorações grandes em
  pedaços revisáveis.
- **Escreva uma descrição clara.** Explique o **porquê** da mudança, não
  apenas o **o quê**. Vincule a issue relacionada quando aplicável.
- **Apresente evidências de performance** quando a mudança for uma
  otimização. Inclua números antes/depois de `go test -bench`, sumários do
  k6 (ex.: `load_tests/reports/baseline-summary.json`) ou screenshots do
  Grafana no dashboard `EasyHooks Load Test`.
- **Adicione ou atualize testes.** Comportamentos novos devem ser cobertos
  por arquivos `_test.go` em `go-api/`. A suíte unitária usa `miniredis`,
  então não é necessário um Redis real para rodá-la.
- **Rode a suíte completa de testes localmente**:
  ```bash
  cd go-api && go test ./... && go test -race ./...
  ```
- **Reconstrua a documentação** se você mexeu em algo dentro de `docs/`:
  ```bash
  cd docs && npm run build
  ```
- **Atualize o README** (tanto `README.md` quanto `README.pt-br.md`)
  sempre que mudar comportamento exposto ao usuário, variáveis de
  ambiente ou perfis de capacidade.

---

## 📏 Padrões de Código

### Idempotência

Toda alteração no fluxo de mensagens deve preservar a natureza
**idempotente** do servidor. O worker depende do
`SET NX event_lock:<event-id>` para deduplicar eventos; não burle nem
enfraqueça essa garantia. Sempre respeite o header `X-Event-Id` no caminho
de ingestão.

### Logs e observabilidade

- Use **logs estruturados** (seguindo o logger existente). Inclua
  `tenant_id`, `event_id` e `stream_id` sempre que estiverem disponíveis.
- Não logue segredos (chaves HMAC, bearer tokens, payloads inteiros de
  tamanho desconhecido).
- Adicione ou atualize **métricas Prometheus** ao introduzir um novo
  caminho de código que valha a pena medir (histogramas de latência,
  counters de retries/DLQ, etc.).
- Adicione **spans OpenTelemetry** em novas operações que atravessem
  fronteiras de serviço (HTTP → Redis → Worker → WebSocket).

### Tratamento de erros

Não ignore erros. Escolha a estratégia adequada para o contexto:

- **Retry com backoff exponencial** para falhas transientes (configurado
  via `WORKER_BACKOFF_BASE_MS` / `WORKER_MAX_RETRIES`).
- **Dead Letter Queue** (`events:failed`) para falhas permanentes. Sempre
  inclua o campo `x_original_error` para que operadores possam
  investigar depois.
- **HTTP 4xx/5xx** com códigos de erro explícitos para falhas voltadas ao
  cliente. Nunca vaze stack traces internos para o cliente.

### Backpressure e capacidade

O servidor prioriza integridade em detrimento de throughput. Sob
saturação, ele deve **rejeitar novas requisições com HTTP 429** em vez de
quebrar. Qualquer mudança que afete o caminho de ingestão precisa
respeitar o load shedder (`INGEST_MAX_QUEUE_DEPTH`,
`QUEUE_DEPTH_LOW_WATER_PCT`) e os perfis de capacidade existentes
(`small` / `medium` / `large` / `custom`).

### Estilo Go

- Siga `gofmt` / `goimports`. O CI pode rejeitar código não formatado.
- Prefira a biblioteca padrão e as dependências já em uso (`chi`,
  `go-redis/v9`, `testify`, `miniredis`). Evite trazer novas dependências
  pesadas sem discussão prévia.
- Evite abstrações prematuras; espelhe os padrões já presentes em
  `go-api/`.

### Comentários

Comentários devem explicar **intenção, trade-offs ou restrições não
óbvias**. Evite comentários narrativos como `// incrementa contador` —
deixe o código falar por si.

---

## ⚖️ Código de Conduta

Seja respeitoso e colaborativo. Estamos todos aqui para aprender e
construir algo útil para a comunidade. Assuma boa intenção, dê feedback
construtivo e prefira sugestões concretas a críticas vagas.

### Conventional Commits

Adotamos **Conventional Commits** tanto para mensagens de commit quanto
para títulos de PR:

| Prefixo | Quando usar |
| --- | --- |
| `feat:` | nova funcionalidade voltada ao usuário |
| `fix:` | correção de bug |
| `perf:` | melhoria de performance (inclua números no PR) |
| `refactor:` | mudança de código que não corrige bug nem adiciona feature |
| `test:` | adição ou atualização de testes |
| `docs:` | apenas documentação |
| `chore:` | tooling, build, dependências, CI |

Exemplo:

```
perf: reduzir alocações por requisição em /v1/webhooks/:id
```

---

Obrigado novamente por contribuir! Se você estiver em dúvida sobre o
escopo de uma mudança, abra um PR em draft ou uma issue antes — teremos
prazer em discutir a abordagem antes que você invista tempo na
implementação.
