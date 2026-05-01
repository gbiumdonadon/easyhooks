---
id: codigos-http
title: Códigos HTTP
sidebar_position: 1
description: O que cada status code retornado pelo ingestor significa.
---

# Códigos HTTP retornados pelo Ingestor

A API segue convenções RESTful padronizadas. Use estes códigos para decidir se deve **retentar**, **logar e ignorar**, ou **alertar** no seu sistema.

## Sucesso

- **`202 Accepted`** — Webhook recebido, autenticado, idempotente, e enfileirado em Kafka. **Não significa** que o handler de negócio já processou; significa que a entrega futura está garantida (com retry + DLQ). Não retentar.

## Erros do cliente — não retentar (4xx)

Erros 4xx indicam que **o pedido está errado**. Retentar com a mesma payload retorna o mesmo erro. Corrija e reenvie.

- **`400 Bad Request`** — Header obrigatório `X-Event-Id` ausente ou vazio.
  - Causa típica: cliente esqueceu de enviar o ID idempotente.
  - Ação: gerar um UUID/ULID e reenviar.
- **`401 Unauthorized`** — Nenhuma credencial enviada.
  - Causa típica: faltou tanto `Authorization: Bearer ...` quanto `X-Webhook-Signature: sha256=...`.
  - Ação: revisar configuração de credenciais.
- **`403 Forbidden`** — Credenciais presentes, mas inválidas para o tenant.
  - Sub-casos:
    - Bearer token não confere com o `tenant_id` da URL.
    - HMAC `sha256=...` não bate com `HMAC-SHA256(secret, body)`.
    - Tentativa de cross-tenant (tenant X usando secret de tenant Y).
  - Ação: verificar `secret_key`, `tenant_id` e o cálculo do HMAC. Veja [Segurança HMAC → Erros comuns](../api-reference/seguranca-hmac.md#erros-comuns).
- **`404 Not Found`** — Endpoint inexistente. Confira a URL.
- **`422 Unprocessable Entity`** — Body inválido.
  - Sub-casos:
    - `tenant_id` da URL não é um UUID válido.
    - Body não é JSON parseável (chave sem valor, vírgula sobrando, encoding errado).
  - Ação: validar antes de enviar.

## Erros do servidor — pode retentar (5xx)

Erros 5xx geralmente são transitórios.

- **`500 Internal Server Error`** — Bug ou erro inesperado na API. Logar e **retentar com backoff exponencial**.
- **`502 Bad Gateway` / `503 Service Unavailable`** — Indisponibilidade momentânea (ex.: Postgres caiu, Kafka indisponível). Retentar com backoff.
- **`504 Gateway Timeout`** — Demora além do timeout do reverse proxy. Retentar.

> Recomendação para o cliente: tratar todos os 5xx com a **mesma política de backoff** descrita em [Retentativas e DLQ](./retentativas-dlq.md), mas no **lado do cliente** (já que esses erros ocorrem antes do `202` que dispara o retry interno do worker).

## Resumo: tabela de decisão para o seu sistema

- `202` → marcar como entregue.
- `400` / `422` → fix code, **não** retentar.
- `401` / `403` → fix credenciais, **não** retentar.
- `404` → fix URL, **não** retentar.
- `5xx` → **retentar** com exponencial backoff (1s, 2s, 4s, ..., teto 30s) + jitter.

## Headers de resposta úteis

Toda resposta inclui:

- `X-Request-Id` — ID único da requisição. Inclua em tickets de suporte.
- `Content-Type: application/json` — exceto em `204` (raramente retornado).

Caso o body de erro venha vazio, o motivo está apenas no status — verifique o `X-Request-Id` e logs server-side.
