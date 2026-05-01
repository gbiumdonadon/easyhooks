---
id: seguranca-hmac
title: Segurança HMAC
sidebar_position: 2
description: Como assinar e validar o header X-Webhook-Signature para autenticar e garantir integridade.
---

# Segurança HMAC (`X-Webhook-Signature`)

A plataforma autentica webhooks usando **HMAC-SHA256** sobre o **corpo bruto da requisição**. O resultado é colocado no header `X-Webhook-Signature` no formato:

```
X-Webhook-Signature: sha256=<hex_lowercase>
```

## Por que HMAC e não apenas Bearer?

- **Prova de posse do secret sem expô-lo.** O secret nunca trafega pela rede.
- **Resistência a tampering.** Qualquer alteração no body invalida a assinatura.
- **Compatível com proxies e CDN.** O header é estável e simples.
- **Padrão de mercado.** GitHub, Stripe, Shopify e Slack usam o mesmo formato `sha256=<hex>`.

## Algoritmo

1. Pegue o **corpo bruto** da requisição como `bytes` (sem reformatar / re-serializar).
2. Calcule `digest = HMAC_SHA256(secret_key, body)`.
3. Converta `digest` para hexadecimal **minúsculo**.
4. Prefixe com `sha256=` e envie no header.

A plataforma compara usando `hmac.compare_digest` (tempo constante) para mitigar ataques de timing.

## Exemplos de implementação

### Python

```python
import hmac
import hashlib
import json
import requests

secret = "a-very-long-base64url-secret-..."
tenant_id = "f1a2b3c4-..."
payload = {"event": "order.created", "data": {"id": 1}}

body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
signature = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()

requests.post(
    f"http://localhost:8000/v1/webhooks/{tenant_id}",
    data=body,
    headers={
        "X-Webhook-Signature": signature,
        "X-Event-Id": "evt-001",
        "Content-Type": "application/json",
    },
)
```

> Importante: serialize **uma vez** e envie os mesmos `bytes` que assinou. Bibliotecas que re-serializam (ex.: passar `json=` em vez de `data=`) podem alterar o body e invalidar a assinatura.

### Node.js

```javascript
import crypto from 'node:crypto';

const secret = 'a-very-long-base64url-secret-...';
const tenantId = 'f1a2b3c4-...';
const payload = { event: 'order.created', data: { id: 1 } };

const body = JSON.stringify(payload);
const signature =
  'sha256=' + crypto.createHmac('sha256', secret).update(body).digest('hex');

await fetch(`http://localhost:8000/v1/webhooks/${tenantId}`, {
  method: 'POST',
  body,
  headers: {
    'X-Webhook-Signature': signature,
    'X-Event-Id': 'evt-001',
    'Content-Type': 'application/json',
  },
});
```

### Bash + OpenSSL

```bash
SECRET="a-very-long-base64url-secret-..."
BODY='{"event":"order.created","data":{"id":1}}'

SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $2}')"

curl -X POST "http://localhost:8000/v1/webhooks/$TENANT_ID" \
  -H "X-Webhook-Signature: $SIG" \
  -H "X-Event-Id: evt-001" \
  -H "Content-Type: application/json" \
  -d "$BODY"
```

> Use `printf '%s'` em vez de `echo` para evitar `\n` no final.

### Go

```go
package main

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/http"
)

func main() {
    secret := []byte("a-very-long-base64url-secret-...")
    tenantID := "f1a2b3c4-..."
    body := []byte(`{"event":"order.created","data":{"id":1}}`)

    mac := hmac.New(sha256.New, secret)
    mac.Write(body)
    signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

    req, _ := http.NewRequest("POST",
        "http://localhost:8000/v1/webhooks/"+tenantID,
        bytes.NewReader(body),
    )
    req.Header.Set("X-Webhook-Signature", signature)
    req.Header.Set("X-Event-Id", "evt-001")
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        panic(err)
    }
    fmt.Println("status:", resp.Status)
}
```

## Verificação no lado do servidor (referência)

A plataforma faz exatamente isto, mas o código é útil para você implementar em outros consumidores que recebem webhooks:

```python
def verify_hmac_signature(secret_raw: str, body: bytes, signature: str) -> bool:
    if not signature:
        return False

    provided = signature.strip()
    if provided.startswith("sha256="):
        provided = provided[len("sha256="):]

    expected = hmac.new(
        secret_raw.encode("utf-8"), body, hashlib.sha256
    ).hexdigest()

    return hmac.compare_digest(provided.lower(), expected)
```

## Erros comuns

- **`echo` no bash adiciona `\n`** — use `printf '%s'` ou `echo -n`.
- **Re-serializar JSON** muda o body — assine o `bytes` exato que vai enviar.
- **Usar o `tenant_id` como secret** — o secret é um campo separado, retornado em `secret_key` na criação do tenant.
- **Hex maiúsculo** — sempre minúsculo. A comparação é case-insensitive na plataforma, mas mantenha minúsculo por convenção.

## Roadmap

- **Replay protection.** Suporte futuro a `X-Webhook-Timestamp` + janela de 5 minutos. Será sinalizado no header `X-Webhook-Signature-Version: v2` quando disponível.
- **Rotação de secret.** Endpoint `POST /admin/tenants/{id}/rotate-secret` planejado.
