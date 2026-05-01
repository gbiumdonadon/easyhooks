---
id: protocolo
title: Protocolo (Heartbeats e Reconexão)
sidebar_position: 2
description: Como funcionam ping/pong nativos do WebSocket e padrão de reconexão com backoff exponencial.
---

# Protocolo, Heartbeats e Reconexão

## Formato das mensagens

Cada mensagem é um **frame de texto** (opcode `0x1`) carregando o JSON exato publicado no ingestor. Não há envelope adicional. Veja [Conexão](./conexao.md#o-que-vem-na-mensagem).

A plataforma **não envia mensagens de protocolo da aplicação** (ex.: `{"type":"ping"}`). Tudo o que chega é evento de negócio do tenant.

## Heartbeats: ping/pong nativo (RFC 6455)

A camada WebSocket do protocolo já tem **frames de controle de ping/pong** independentes do payload da aplicação:

- O servidor (uvicorn) envia **ping** automaticamente em intervalos regulares (padrão: a cada 20s).
- O cliente WebSocket — todos os clientes compatíveis com a RFC 6455, incluindo navegadores e bibliotecas como `websockets` (Python), `ws` (Node), `gorilla/websocket` (Go) — responde com **pong** automaticamente.
- Se o servidor não receber pong em ~20s, ele encerra a conexão (ela aparece como `onclose` no cliente).

> **Você não precisa implementar ping/pong manualmente.** Apenas certifique-se de **não** desabilitar o handler default da sua biblioteca.

### Configurando o cliente Python (`websockets`)

```python
async with websockets.connect(
    url,
    ping_interval=20,   # cliente também envia ping a cada 20s
    ping_timeout=20,    # fecha se não receber pong em 20s
) as ws:
    ...
```

### Configurando o cliente Node (`ws`)

```javascript
const ws = new WebSocket(url);
// 'ws' já trata pong nativo automaticamente.
// Para customizar:
ws.on('ping', () => ws.pong());
```

### Browsers

A API `WebSocket` do navegador **não expõe** os frames de ping/pong para o JavaScript — eles são tratados pelo runtime. Você não precisa fazer nada no client-side; basta confiar que o navegador responde aos pings do servidor.

## Reconexão com Backoff Exponencial

Se a conexão cair (queda de rede, deploy do servidor, expiração do token), você deve **reconectar** com backoff exponencial + jitter para não sobrecarregar o servidor numa subida coordenada.

Política recomendada:

- Delay inicial: **1s**.
- Multiplicador: **2x** por tentativa.
- Teto: **30s**.
- Jitter aleatório: **0–1s** somado ao delay.
- Reset do contador ao reconectar com sucesso (`onopen`).

### JavaScript (browser ou Node)

```javascript
let attempt = 0;

function connect() {
  const ws = new WebSocket(`wss://your-host/ws/events/${TENANT_ID}?token=${token}`);

  ws.onopen = () => {
    console.log('connected');
    attempt = 0;
  };

  ws.onmessage = (ev) => {
    const event = JSON.parse(ev.data);
    handleEvent(event);
  };

  ws.onclose = (ev) => {
    const baseDelay = Math.min(1000 * 2 ** attempt, 30000);
    const jitter = Math.random() * 1000;
    const delay = baseDelay + jitter;

    attempt += 1;
    console.warn(`closed (${ev.code}); reconnecting in ${Math.round(delay)}ms`);

    setTimeout(connect, delay);
  };

  ws.onerror = (err) => console.error('ws error', err);
}

connect();
```

### Python (`websockets`)

```python
import asyncio
import random
import websockets
from websockets.exceptions import ConnectionClosed

async def run_with_backoff(url_factory, on_message):
    attempt = 0
    while True:
        try:
            url = await url_factory()  # gera novo token se preciso
            async with websockets.connect(url, ping_interval=20) as ws:
                attempt = 0
                async for message in ws:
                    await on_message(message)
        except (OSError, ConnectionClosed) as exc:
            delay = min(1.0 * 2 ** attempt, 30.0) + random.random()
            print(f"reconnecting in {delay:.1f}s ({exc})")
            await asyncio.sleep(delay)
            attempt += 1
```

## Renovação de token

Tokens expiram em `WS_TOKEN_TTL_SECONDS` (padrão 5 min). Estratégias:

1. **Reativa** — tente reconectar quando a conexão cair; gere um novo token nesse momento. Simples e suficiente para a maioria dos casos.
2. **Proativa** — antes do TTL vencer (ex.: 30s antes), gere um novo token, abra uma nova conexão e migre o tráfego. Útil para conexões muito longevas onde a queda momentânea durante a reconexão não é aceitável.

## Encerramento gracioso

Para fechar pelo cliente:

```javascript
ws.close(1000, 'client requested shutdown');
```

O servidor cancela o forwarder do Pub/Sub e libera os recursos imediatamente.
