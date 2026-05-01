---
id: tokens-ws
title: Tokens de WebSocket
sidebar_position: 3
description: POST /v1/tokens/{tenant_id} — emissão de token efêmero para conectar no WebSocket.
---

# Tokens de WebSocket `POST /v1/tokens/{tenant_id}`

Para abrir uma conexão WebSocket, o cliente **não** envia o `secret_key` diretamente — em vez disso, troca o secret por um **token efêmero assinado** que carrega `tenant_id` + `expiração`.

Isso evita expor o secret em URLs/cabeçalhos do navegador e permite revogação implícita pelo TTL.

## URL

```
POST /v1/tokens/{tenant_id}
```

## Headers

Igual ao [ingestor](./ingestor.md): use `Authorization: Bearer <secret_key>` **ou** `X-Webhook-Signature: sha256=<hex>`. Para este endpoint, Bearer geralmente é suficiente porque o body é vazio.

```
Authorization: Bearer <secret_key>
Content-Type: application/json
```

## Body

Sem body. (Pode enviar `{}` ou nada.)

## Resposta

`200 OK`:

```json
{
  "token": "eyJzdWIiOiJmMWEyYjNjNC0...QkLi.eq8x...",
  "expires_in": 300
}
```

- `token` — string opaca a ser usada como query string `?token=...` na URL do WebSocket.
- `expires_in` — segundos até a expiração (padrão `300` = 5 min, configurável via `WS_TOKEN_TTL_SECONDS`).

## Erros

- `401 Unauthorized` — credenciais ausentes.
- `403 Forbidden` — secret inválido para este tenant.

## Como o token é gerado

O token é um HMAC assinado com a chave de aplicação `APP_SECRET_KEY`. Estrutura:

```
<base64url(payload)>.<base64url(hmac_sha256(APP_SECRET_KEY, payload))>
```

Onde `payload` é o JSON:

```json
{ "sub": "<tenant_id>", "exp": <unix_timestamp> }
```

> Esse formato é compatível conceitualmente com JWT (header.payload.sig), mas simplificado: dispensa o header e usa apenas HMAC-SHA256 fixo. A intenção é minimizar dependências e superfície de ataque.

## Verificação

A verificação acontece no endpoint WebSocket — não há endpoint público para introspectar o token. Detalhes do consumo em [WebSockets → Conexão](../websockets/conexao.md).

## Exemplo

```bash
curl -X POST "http://localhost:8000/v1/tokens/$TENANT_ID" \
  -H "Authorization: Bearer $SECRET" \
  -H "Content-Type: application/json"
```

```json
{
  "token": "eyJzdWIiOiJmMWEyYjNjNC0uLi4iLCJleHAiOjE3MTQ1MjM0NTZ9.A1B2...",
  "expires_in": 300
}
```

Use o `token` retornado em:

```
ws://localhost:8000/ws/events/{tenant_id}?token=<token>
```
