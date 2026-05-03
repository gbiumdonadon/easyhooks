# Load Testing - EasyHooks

Infraestrutura completa de testes de carga para medir throughput, escala de WebSockets e isolamento multi-tenant da plataforma EasyHooks.

## Visão Geral

Este diretório contém:
- **Cenários de teste**: baseline, throughput, WebSocket scale, multi-tenant, stress
- **Ferramentas**: tenant factory, HMAC helpers, metrics collector
- **Dashboards**: Grafana dashboard customizado para análise
- **Scripts**: preparação de sistema e automação

## Quick Start v2 (Recomendado)

### Novo Fluxo Simplificado

A infraestrutura foi reestruturada para facilitar a execução de testes. Agora você pode rodar testes com **um único comando**!

#### Opção 1: Usando Scripts Wrapper (Mais Flexível)

```bash
# 1. Configure as variáveis de ambiente (primeira vez apenas)
cd load_tests
cp .env.loadtest.example .env.loadtest
# Edite .env.loadtest se necessário

# 2. Valide o ambiente
./validate-env.sh

# 3. Execute o teste desejado
./run-loadtest.sh baseline              # Teste baseline (10 min, carga moderada)
./run-loadtest.sh throughput            # Teste de throughput (30 min, alta carga)
./run-loadtest.sh websocket_scale       # Teste WebSocket (10k conexões)
```

**O script automaticamente:**
- ✅ Valida pré-requisitos (Docker, variáveis de ambiente)
- ✅ Detecta se API é local e garante que o stack está rodando
- ✅ Aguarda API ficar pronta (healthcheck automático)
- ✅ Cria tenants de teste automaticamente
- ✅ Inicia Locust com master + workers
- ✅ Abre UI em http://localhost:8089

#### Opção 2: Usando Makefile (Mais Rápido)

```bash
# Validar ambiente
make loadtest-validate

# Rodar testes
make loadtest-local          # Baseline contra localhost
make loadtest-throughput     # Teste de throughput
make loadtest-ws-scale       # Teste WebSocket scale

# Gerenciar testes
make loadtest-status         # Ver status dos containers
make loadtest-logs           # Ver logs em tempo real
make loadtest-stop           # Parar containers
make loadtest-clean          # Parar e limpar tenant pool

# Abrir dashboards
make loadtest-ui             # Abrir Locust UI
make loadtest-grafana        # Abrir Grafana
make loadtest-prometheus     # Abrir Prometheus

# Ver todos os comandos
make help
```

### Testando Contra Ambientes Remotos

```bash
# Staging
export LOADTEST_API_BASE_URL=https://staging.easyhooks.io
export LOADTEST_ADMIN_TOKEN=<staging-token>
./run-loadtest.sh baseline

# Ou usando Makefile
LOADTEST_API_BASE_URL=https://staging.easyhooks.io make loadtest-throughput
```

### Variáveis de Ambiente Importantes

| Variável | Descrição | Padrão |
|----------|-----------|--------|
| `LOADTEST_API_BASE_URL` | URL da API a testar | `http://localhost:8000` |
| `LOADTEST_ADMIN_TOKEN` | Token admin para criar tenants | (obrigatório) |
| `LOADTEST_TENANT_COUNT` | Número de tenants | `50` |
| `LOADTEST_WORKERS` | Número de workers Locust | `2` |

### Troubleshooting Quick Start

**Erro: "API não está respondendo"**
```bash
# Verifique se o stack principal está rodando
docker compose ps

# Se não estiver, suba o stack
docker compose up -d

# Aguarde ficar healthy
docker compose ps
```

**Erro: "LOADTEST_ADMIN_TOKEN não configurada"**
```bash
# Copie o token do .env principal
export LOADTEST_ADMIN_TOKEN=$(grep ADMIN_SEED_TOKEN ../.env | cut -d= -f2)

# Ou defina manualmente
export LOADTEST_ADMIN_TOKEN=seu-token-aqui
```

**Erro: "Docker daemon não está acessível"**
```bash
# Inicie o Docker Desktop (Windows/Mac)
# Ou inicie o daemon (Linux)
sudo systemctl start docker
```

---

## Arquitetura dos Testes

```
┌─────────────────────────────────────────────────────────────┐
│                     Locust Load Generator                    │
│  ┌──────────────────┐        ┌──────────────────┐          │
│  │  WebhookUser     │        │  WebSocketUser   │          │
│  │  (HTTP POST)     │        │  (WS Listener)   │          │
│  └──────────────────┘        └──────────────────┘          │
└─────────────────────────────────────────────────────────────┘
                      │                    │
                      ↓                    ↓
         ┌────────────────────────────────────────┐
         │           FastAPI + Kafka              │
         │        Redis + PostgreSQL              │
         └────────────────────────────────────────┘
                      │
                      ↓
         ┌────────────────────────────────────────┐
         │    Prometheus + Grafana + Jaeger       │
         │         (Observabilidade)              │
         └────────────────────────────────────────┘
```

---

## Manual Setup (Modo Legado)

Se preferir executar manualmente ou precisa de mais controle, use este método:

### 1. Preparar o Sistema

**Windows (PowerShell)**:
```powershell
.\load_tests\scripts\prepare_system.ps1
```

**Linux/macOS**:
```bash
chmod +x load_tests/scripts/prepare_system.sh
sudo ./load_tests/scripts/prepare_system.sh
```

### 2. Subir o Stack Otimizado

```bash
# Subir com configurações de load testing
docker compose -f docker-compose.yml -f docker-compose.loadtest.yml up -d

# Verificar que todos os serviços estão healthy
docker compose ps
```

### 3. Criar Pool de Tenants de Teste

```bash
cd load_tests
python utils/tenant_factory.py --create --count 50
```

Isso cria 50 tenants de teste e salva em `.tenant_pool.json`.

### 4. Instalar Dependências de Teste

```bash
pip install -r load_tests/requirements.txt
```

### 5. Executar um Teste

```bash
# Teste baseline (10 minutos, carga moderada)
locust -f scenarios/baseline.py --host=http://localhost:8000
```

Abra http://localhost:8089 para controlar o teste via interface web.

## Cenários de Teste

### 1. Baseline Test
**Objetivo**: Estabelecer baseline de performance

```bash
locust -f scenarios/baseline.py \
  --host=http://localhost:8000 \
  --users 100 \
  --spawn-rate 10 \
  --run-time 10m \
  --html reports/baseline_$(date +%Y%m%d_%H%M%S).html
```

**Configuração**:
- 100 usuários virtuais
- ~100 requisições/segundo
- 10 minutos de duração
- Mix 80% webhook / 20% WebSocket

**Métricas Esperadas**:
- Latência p95: < 200ms
- Taxa de erro: < 0.1%
- Consumer lag: < 50 mensagens

---

### 2. Throughput Test
**Objetivo**: Medir throughput máximo sustentável

```bash
locust -f scenarios/throughput.py \
  --host=http://localhost:8000 \
  --users 5000 \
  --spawn-rate 100 \
  --run-time 30m \
  --html reports/throughput_$(date +%Y%m%d_%H%M%S).html
```

**Configuração**:
- Escalonamento gradual: 100 → 5000 usuários
- 30 minutos de duração
- Mix 90% webhook / 10% WebSocket

**Critérios de Sucesso**:
- Throughput sustentado > 1000 req/s
- Latência p95: < 500ms
- Taxa de erro: < 1%

---

### 3. WebSocket Scale Test
**Objetivo**: Testar 10.000 conexões WebSocket simultâneas

```bash
locust -f scenarios/websocket_scale.py \
  --host=http://localhost:8000 \
  --users 10000 \
  --spawn-rate 50 \
  --run-time 60m \
  --html reports/ws_scale_$(date +%Y%m%d_%H%M%S).html
```

**Configuração**:
- 10.000 conexões WebSocket persistentes
- 100 webhooks/segundo (para gerar eventos)
- 60 minutos de duração
- Mix 1% webhook / 99% WebSocket

**Métricas Chave**:
- Taxa de sucesso de conexão: > 99%
- Latência de entrega (HTTP → WS): p95 < 1s
- Mensagens perdidas: 0

---

### 4. Multi-tenant Isolation Test
**Objetivo**: Validar isolamento entre tenants

```bash
locust -f scenarios/multi_tenant.py \
  --host=http://localhost:8000 \
  --run-time 30m \
  --html reports/multi_tenant_$(date +%Y%m%d_%H%M%S).html
```

**Configuração**:
- 3 grupos de tenants:
  - **VIP**: 10 req/s (normal)
  - **Normais** (10): 50 req/s cada
  - **Abusivo**: 5000 req/s (flood)
- 30 minutos de duração

**Validação**:
- Latência VIP não degrada > 10%
- Latência normais não degrada > 20%
- Tenant abusivo não impacta outros

---

### 5. Stress Test (Breaking Point)
**Objetivo**: Identificar ponto de ruptura

```bash
locust -f scenarios/stress.py \
  --host=http://localhost:8000 \
  --spawn-rate 500 \
  --run-time 60m \
  --html reports/stress_$(date +%Y%m%d_%H%M%S).html
```

**Configuração**:
- Escalonamento agressivo: +500 usuários a cada 2min
- Sem limite superior
- Parar quando: taxa de erro > 10% OU latência p95 > 5s

**Observar**:
- Em que ponto o sistema quebra?
- Qual componente falha primeiro?
- Como degrada?

## Modo Distribuído

Para testes muito pesados (10k+ usuários), use Locust distribuído:

### Master + Workers Localmente

```bash
# Terminal 1: Master
locust -f locustfile.py --master --expect-workers 4 --host=http://localhost:8000

# Terminais 2-5: Workers
locust -f locustfile.py --worker --master-host=localhost &
locust -f locustfile.py --worker --master-host=localhost &
locust -f locustfile.py --worker --master-host=localhost &
locust -f locustfile.py --worker --master-host=localhost
```

### Via Docker Compose

O arquivo `docker-compose.loadtest.yml` já inclui master + 2 workers:

```bash
docker compose -f docker-compose.yml -f docker-compose.loadtest.yml up -d

# Acessar interface web
open http://localhost:8089
```

## Métricas e Dashboards

### Grafana

Acesse http://localhost:3000 (admin/admin)

Dashboard customizado: **"Load Test Overview"**

Painéis principais:
1. **Request Rate & Latency**: RPS e latências p50/p95/p99
2. **Kafka Health**: Consumer lag, throughput
3. **WebSocket Metrics**: Conexões ativas, latência de entrega
4. **Resource Usage**: CPU/Memory por serviço
5. **Multi-tenant Isolation**: Latência por tenant

### Prometheus

Acesse http://localhost:9090

Queries úteis:

```promql
# Throughput atual
rate(http_requests_total[1m])

# Latência p95
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[1m]))

# Consumer lag (CRÍTICO)
kafka_consumergroup_lag{consumergroup="webhook-workers"}

# Taxa de erro
rate(http_requests_total{status_code=~"5.."}[1m]) / rate(http_requests_total[1m]) * 100

# WebSocket connections
sum(websocket_connections_active)

# End-to-end latency
histogram_quantile(0.95, rate(websocket_e2e_latency_seconds_bucket[1m]))
```

### Jaeger

Acesse http://localhost:16686

Use para analisar:
- Traces lentos (p99)
- Traces com erro
- Breakdown de latência por componente

## Interpretação de Resultados

### Critérios de Aceitação

| Métrica | Baseline | Alta Carga | Crítico |
|---------|----------|------------|---------|
| **Throughput** | 100 RPS | 1000 RPS | 5000+ RPS |
| **Latência p95** | < 200ms | < 500ms | < 1s |
| **Taxa de erro** | < 0.1% | < 1% | < 5% |
| **Consumer lag** | < 50 | < 500 | < 5000 |
| **WebSocket connections** | 50 | 1000 | 10000 |
| **WS message latency p95** | < 500ms | < 1s | < 2s |

### Sinais de Alerta

🔴 **CRÍTICO** - Intervenção imediata necessária:
- Taxa de erro > 5%
- Consumer lag > 5000
- Latência p95 > 5s
- Perda de mensagens
- OOM kills
- Serviços crashando

🟡 **ATENÇÃO** - Investigar:
- Taxa de erro entre 1-5%
- Consumer lag entre 500-5000
- Latência p95 entre 1-5s
- CPU > 80% sustentado
- Memory > 90%
- Reconnect rate > 5%

🟢 **SAUDÁVEL**:
- Taxa de erro < 1%
- Consumer lag < 500
- Latência p95 < 1s
- Recursos em níveis normais

### Análise de Gargalos

**Sintomas e Diagnósticos**:

1. **Alta latência + Consumer lag alto**
   - Gargalo: Workers insuficientes
   - Solução: Escalar workers horizontalmente

2. **Alta latência + Consumer lag baixo**
   - Gargalo: Redis ou PostgreSQL
   - Solução: Otimizar queries, aumentar recursos

3. **Taxa de erro alta + CPU alto**
   - Gargalo: Recursos computacionais
   - Solução: Escalar verticalmente ou otimizar código

4. **WebSocket disconnects frequentes**
   - Gargalo: Limites de file descriptors
   - Solução: Aumentar `ulimit -n`

5. **Redis evictions**
   - Gargalo: Memória Redis insuficiente
   - Solução: Aumentar `maxmemory` ou implementar particionamento

## Troubleshooting

### Erro: "Tenant pool is empty"

```bash
cd load_tests
python utils/tenant_factory.py --create --count 50
```

### Erro: WebSocket connection refused

Verificar limites:
```bash
# Linux/macOS
ulimit -n
ulimit -n 65536

# Windows: ajustar no Docker Desktop
# Settings → Resources → Advanced
```

### Kafka consumer lag explodindo

Escalar workers:
```bash
docker compose -f docker-compose.yml -f docker-compose.loadtest.yml up -d --scale worker=8
```

### Redis connection pool exhausted

Aumentar pool em `src/redis_client.py`:
```python
redis_pool = redis.asyncio.ConnectionPool.from_url(
    settings.REDIS_URL,
    max_connections=200  # aumentar de 50
)
```

### Locust workers não conectam ao master

Verificar network:
```bash
# Se usando Docker
docker compose logs locust-master
docker compose logs locust-worker

# Se local, verificar firewall na porta 5557
```

## Estrutura de Arquivos

```
load_tests/
├── locustfile.py                 # Classes principais: WebhookUser, WebSocketUser
├── config.py                     # Configurações (URLs, tokens, etc)
├── requirements.txt              # Dependências Python
├── Dockerfile                    # Imagem Docker para Locust distribuído
│
├── scenarios/                    # Cenários de teste
│   ├── baseline.py               # Carga baseline
│   ├── throughput.py             # Teste de throughput
│   ├── websocket_scale.py        # Escala de WebSockets
│   ├── multi_tenant.py           # Isolamento multi-tenant
│   └── stress.py                 # Breaking point
│
├── utils/                        # Utilitários
│   ├── tenant_factory.py         # Criar tenants de teste
│   ├── hmac_helpers.py           # Assinatura HMAC
│   └── metrics_collector.py     # Métricas customizadas
│
├── scripts/                      # Scripts de automação
│   ├── prepare_system.sh         # Preparar sistema (Linux/macOS)
│   └── prepare_system.ps1        # Preparar sistema (Windows)
│
└── reports/                      # Relatórios gerados
    ├── TEMPLATE.md               # Template de relatório
    ├── *.html                    # Relatórios Locust
    └── custom_metrics.json       # Métricas customizadas
```

## Boas Práticas

### Antes do Teste

1. ✅ Executar `prepare_system.sh` / `prepare_system.ps1`
2. ✅ Verificar recursos do Docker Desktop (8GB+ RAM)
3. ✅ Criar pool de tenants (`--count 50` no mínimo)
4. ✅ Verificar que todos os serviços estão healthy
5. ✅ Limpar dados antigos se necessário

### Durante o Teste

1. 📊 Monitorar dashboards Grafana em tempo real
2. 📊 Observar consumer lag (métrica mais crítica)
3. 📊 Verificar taxa de erro
4. 📊 Monitorar uso de recursos (CPU/Memory)
5. 📝 Anotar observações e anomalias

### Depois do Teste

1. 📄 Gerar relatório usando template
2. 📄 Exportar snapshots do Grafana
3. 📄 Salvar HTML do Locust
4. 📄 Coletar traces do Jaeger (exemplos)
5. 📄 Identificar gargalos e propor ações
6. 🔄 Iterar e otimizar

## Variáveis de Ambiente

Configurar em `.env` ou via `load_tests/config.py`:

```bash
# API
LOADTEST_API_BASE_URL=http://localhost:8000
LOADTEST_ADMIN_TOKEN=your-admin-token

# Tenant Pool
LOADTEST_TENANT_POOL_SIZE=50
LOADTEST_TENANT_POOL_FILE=load_tests/.tenant_pool.json

# WebSocket
LOADTEST_WS_TIMEOUT=10
LOADTEST_WS_RECEIVE_TIMEOUT=5

# Metrics
LOADTEST_ENABLE_CUSTOM_METRICS=true
LOADTEST_METRICS_EXPORT_INTERVAL=10
```

## Exemplo Completo de Sessão

```bash
# 1. Preparar sistema
./load_tests/scripts/prepare_system.sh

# 2. Subir stack otimizado
docker compose -f docker-compose.yml -f docker-compose.loadtest.yml up -d
docker compose ps  # Verificar health

# 3. Criar tenants
cd load_tests
python utils/tenant_factory.py --create --count 50
python utils/tenant_factory.py --list  # Verificar

# 4. Instalar deps
pip install -r requirements.txt

# 5. Executar baseline
locust -f scenarios/baseline.py \
  --host=http://localhost:8000 \
  --users 100 \
  --spawn-rate 10 \
  --run-time 10m \
  --html reports/baseline_20260501.html

# 6. Monitorar
open http://localhost:8089      # Locust UI
open http://localhost:3000      # Grafana
open http://localhost:16686     # Jaeger

# 7. Após teste, gerar relatório
cp reports/TEMPLATE.md reports/baseline_20260501_report.md
# Preencher com dados reais

# 8. Cleanup (opcional)
docker compose down
```

## FAQ

**P: Quantos tenants devo criar?**  
R: Mínimo 50 para distribuir carga. Para testes de isolamento, pelo menos 15.

**P: Posso executar em produção?**  
R: NÃO! Execute apenas em ambientes de teste dedicados.

**P: Quanto tempo leva cada teste?**  
R: Baseline (10min), Throughput (30min), WebSocket Scale (60min), Multi-tenant (30min), Stress (até quebrar ou 60min).

**P: Como simular tráfego real?**  
R: Ajuste `wait_time` e `weight` das tasks para refletir padrão de uso real.

**P: Consumer lag alto é sempre ruim?**  
R: Depende. Picos temporários < 1000 são aceitáveis. Lag crescente contínuo é crítico.

**P: Posso rodar múltiplos testes simultaneamente?**  
R: Não recomendado. Resultados serão imprecisos.

## Próximos Passos

1. **Automatizar CI/CD**: Integrar testes de performance no pipeline
2. **Testes de Longevidade**: Executar 24h+ para detectar memory leaks
3. **Testes Geográficos**: Simular latência de rede de diferentes regiões
4. **Chaos Engineering**: Injetar falhas durante carga (kill kafka, redis down)
5. **Auto-scaling**: Testar escalamento automático baseado em métricas

## Recursos

- [Documentação Locust](https://docs.locust.io/)
- [Prometheus Query Examples](https://prometheus.io/docs/prometheus/latest/querying/examples/)
- [Grafana Dashboard Best Practices](https://grafana.com/docs/grafana/latest/dashboards/)

---

**Construído com ❤️ para medir a qualidade do EasyHooks**
