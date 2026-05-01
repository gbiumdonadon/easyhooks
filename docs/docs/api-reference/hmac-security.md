---
id: hmac-security
title: HMAC Security
sidebar_position: 2
description: How to sign and validate the X-Webhook-Signature header for authentication and integrity.
---

# HMAC Security (`X-Webhook-Signature`)

The platform authenticates webhooks using **HMAC-SHA256** over the **raw request body**. The result is placed in the `X-Webhook-Signature` header in the format:

```
X-Webhook-Signature: sha256=<hex_lowercase>
```

## Why HMAC and not just Bearer?

- **Proof of possession without exposing the secret.** The secret never travels over the network.
- **Tampering resistance.** Any change to the body invalidates the signature.
- **Compatible with proxies and CDN.** The header is stable and simple.
- **Industry standard.** GitHub, Stripe, Shopify, and Slack use the same `sha256=<hex>` format.

## Algorithm

1. Get the **raw body** of the request as `bytes` (without reformatting / re-serializing).
2. Calculate `digest = HMAC_SHA256(secret_key, body)`.
3. Convert `digest` to **lowercase** hexadecimal.
4. Prefix with `sha256=` and send in the header.

The platform compares using `hmac.compare_digest` (constant time) to mitigate timing attacks.

## Implementation Examples

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

> Important: serialize **once** and send the same `bytes` you signed. Libraries that re-serialize (e.g., passing `json=` instead of `data=`) may change the body and invalidate the signature.

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

> Use `printf '%s'` instead of `echo` to avoid trailing `\n`.

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

## Server-side Verification (reference)

The platform does exactly this, but the code is useful for implementing in other consumers that receive webhooks:

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

## Common Errors

- **`echo` in bash adds `\n`** — use `printf '%s'` or `echo -n`.
- **Re-serializing JSON** changes the body — sign the exact `bytes` you'll send.
- **Using `tenant_id` as secret** — the secret is a separate field, returned in `secret_key` at tenant creation.
- **Uppercase hex** — always lowercase. Comparison is case-insensitive on the platform, but keep lowercase by convention.

## Roadmap

- **Replay protection.** Future support for `X-Webhook-Timestamp` + 5-minute window. Will be signaled in header `X-Webhook-Signature-Version: v2` when available.
- **Secret rotation.** Endpoint `POST /admin/tenants/{id}/rotate-secret` planned.
