---
id: dimensionamento
title: Planejamento de capacidade
sidebar_position: 3
description: Escolha um perfil do EasyHooks (small / medium / large) e entenda como cada tier se comporta sob carga — números medidos, footprint de memória e degradação graciosa.
---

# Planejamento de capacidade

O EasyHooks vem com três **perfis** pré-tunados que escalam, em conjunto, limite de memória, pool do Redis, tamanho dos streams por tenant, buffers do fanout e o backpressure de ingestão. Escolha um perfil baseado no orçamento de memória do container que você pode dedicar.

> **Garantia de comportamento.** O EasyHooks prioriza a integridade do
> servidor. Sob carga extrema ele prefere **rejeitar requisições novas
> com HTTP 429** a derrubar o serviço por falta de memória (OOM). Os
> números abaixo refletem esse contrato.

## Perfis em uma tabela

| Perfil | Container recomendado | `GOMEMLIMIT` | `STREAM_MAX_LEN` | `REDIS_POOL_SIZE` | `WS_FANOUT_BUFFER_SIZE` | `INGEST_MAX_QUEUE_DEPTH` |
| --- | --- | --- | --- | --- | --- | --- |
| `small`  | 256 MB | 200 MiB | 1 000  | 50  | 100 | 5 000  |
| `medium` | 512 MB | 450 MiB | 5 000  | 100 | 256 | 25 000 |
| `large`  | 1 GB   | 900 MiB | 10 000 | 200 | 512 | 50 000 |
| `custom` | (seu)  | (env)   | (env)  | (env) | (env) | (env)|

Ative um perfil via `EASYHOOKS_PROFILE` no `.env`. Qualquer env var individual ainda vence — definir `STREAM_MAX_LEN=20000` em cima de `EASYHOOKS_PROFILE=large` é honrado como override.

## Comportamento medido sob saturação

Os números abaixo vêm do `load_tests/scripts/run_capacity_benchmark.ps1` rodando em uma máquina dev (Windows + Docker Desktop, 100 VUs do k6, 30 s sustentados, tenant único, payload ≈ 100 B). A carga do k6 excede de propósito a taxa de aceite de cada perfil, para conseguirmos comparar o backpressure.

| Perfil | Cap do container | req/s ofertados | 202 aceitos (30 s) | 429 rejeitados (30 s) | p95 ingestão | RSS do app |
| --- | --- | --- | --- | --- | --- | --- |
| `small`  | 256 MB | ~4 700 | 5 446  | 135 812 | 1,58 ms | ~26 MiB |
| `medium` | 512 MB | ~4 700 | 28 827 | 112 370 | 1,58 ms | ~26 MiB |
| `large`  | 1 GB   | ~4 700 | 52 169 | 88 903  | 1,66 ms | ~27 MiB |

Como ler:

- **Os três perfis ficaram de pé** — sem OOM, sem crash, sem panic. O servidor continuou respondendo, apenas com mais 429s nos tiers menores.
- **p95 da ingestão fica abaixo de 2 ms** mesmo a 4 700 req/s ofertados, em todos os perfis. O caminho do 429 é barato (leitura atômica + retorno antecipado).
- **O volume aceito escala com a folga da fila**: quanto maior o `INGEST_MAX_QUEUE_DEPTH`, maior o pico que a API absorve antes de o backpressure engatar.
- **A memória residente fica bem abaixo do `GOMEMLIMIT`** nessa carga. A folga é deliberada — o GC do Go trabalha duro para nos manter abaixo do limite antes de o container atingir o teto do SO.

## Por que tanta folga?

O `GOMEMLIMIT` **não é** um teto rígido. O runtime do Go ultrapassa o limite brevemente quando recusar crescer a pilha de uma goroutine significaria panic. A proporção recomendada é então `GOMEMLIMIT ≈ 0,8 × memória_do_container` para o runtime ter espaço de manobra antes do OOM killer disparar. É por isso que `small` usa `GOMEMLIMIT=200MiB` em container de 256 MB, `medium` 450 MiB / 512 MB e `large` 900 MiB / 1 GB.

## Knobs de tuning

Quando os defaults precisam de ajuste, sobrescreva env vars individuais (o perfil mantém os outros campos). Os mais comuns:

- `INGEST_MAX_QUEUE_DEPTH` — aumente se o seu worker drena rápido e você pode absorver mais picos; diminua se quer 429s mais cedo sob carga sustentada.
- `QUEUE_DEPTH_LOW_WATER_PCT` — histerese. Default 80 % (só voltamos a aceitar quando `XLEN events:in` cai a 80 % do high watermark). `100` libera assim que cair abaixo do high (mais flapping); `50` segura o freio por mais tempo.
- `WS_FANOUT_BUFFER_SIZE` — aumente para tenants com muitos clientes WS por stream. Quando esse buffer enche, o subscriber lento é desconectado e a métrica `websocket_subscriber_dropped_total{reason="buffer_full"}` incrementa — um sinal claro de que você precisa de um perfil maior ou de clientes mais rápidos.
- `GOMEMLIMIT` — override avançado. A env nativa do Go tem precedência sobre o valor derivado pelo perfil. Use se precisa de uma proporção não-padrão (por exemplo, container compartilhado com sidecar).

## Reproduzindo esses números

```bash
# Da raiz do repo, com Docker Desktop rodando.
powershell -ExecutionPolicy Bypass `
  -File load_tests/scripts/run_capacity_benchmark.ps1 `
  -DurationSec 30 -Vus 100
```

O script:

1. Builda as imagens de `app` e `worker`.
2. Para cada perfil, recria `app` e `worker` com o `mem_limit` correspondente.
3. Drena `events:in` para cada tier começar do zero.
4. Roda `k6 run --vus 100 --duration 30s k6/scenarios/throughput.js`.
5. Escreve resumos em JSON por perfil em `load_tests/reports/capacity/` e imprime uma tabela em markdown.

Rode na sua infra para ter números calibrados para sua CPU, rede e disco do Redis. O valor absoluto vai variar — a **forma** da curva (small rejeita primeiro, large absorve mais burst, p95 baixo em todos) deve se manter.

## Observabilidade

Três métricas Prometheus permitem acompanhar o load shedding ao vivo:

- `webhook_load_shed_total{tenant_id}` — counter de requisições rejeitadas com 429.
- `ingest_queue_depth{stream}` — `XLEN(events:in)` atual amostrado a cada `QUEUE_DEPTH_POLL_MS`.
- `ingest_load_shedding_active` — gauge 0/1 refletindo o estado da histerese. Alerte em `== 1 for 1m` para ser paginado quando a fila está realmente saturada, não em blips transitórios.

Para a camada WebSocket:

- `websocket_subscriber_dropped_total{tenant_id,reason}` — subscribers lentos sendo desconectados. Taxa não-zero significa que você deveria considerar um perfil maior ou investigar o consumidor.

Um gauge constante `easyhooks_profile_info{profile}` expõe o tier ativo, útil como variável no Grafana.

## O que não foi feito (intencionalmente, por enquanto)

- **`sync.Pool`** no caminho de ingestão. O padrão atual de alocação está confortavelmente dentro do `GOMEMLIMIT` na carga medida. Vamos revisitar se profiling mostrar que `io.ReadAll(body)` virou hot path.
- **Backpressure dinâmico baseado em pressão de memória runtime.** O EasyHooks é opinativo: usa um high watermark estático (profundidade de fila) ao invés de tentar adivinhar se o host está "estressado". Mantém o comportamento previsível entre hosts e fácil de raciocinar durante incidentes.
