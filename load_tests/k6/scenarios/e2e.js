/*
 * Cenário: COMBINED BASELINE + WEBSOCKET
 * --------------------------------------
 * Objetivo: rodar publishers HTTP (estilo baseline) e consumidores WebSocket
 * (estilo websocket_scale) no MESMO run, garantindo que os WS estejam
 * conectados nos MESMOS tenants para os quais o publisher posta eventos, e
 * medir a latência fim-a-fim ingest → fan-out → entrega via WS.
 *
 * Diferenças em relação a rodar os dois scripts em paralelo:
 *   1. WS sobe primeiro (warm-up de K6_WS_WARMUP) e o publisher só inicia
 *      depois — evita perder os primeiros eventos.
 *   2. Ambos os cenários selecionam o tenant pelo MESMO módulo
 *      (Math.min(K6_WS_VUS, tenants.length)), de modo que os tenants
 *      cobertos pelos consumidores são exatamente os mesmos visados pelo
 *      publisher. Sem isso, com __VU global distinto por cenário em k6, os
 *      conjuntos podem não se intersectar.
 *   3. O publisher embute `data.k6_ts_ms` no payload; o consumidor mede
 *      Date.now() - k6_ts_ms na chegada via WS, alimentando a métrica
 *      custom `ws_event_latency_ms`.
 *
 * Variáveis de ambiente:
 *   K6_WS_VUS         (default 10)   conexões WebSocket simultâneas
 *   K6_PUB_VUS        (default 10)   VUs do publisher HTTP no platô
 *   K6_WS_WARMUP      (default 15s)  delay antes do publisher iniciar
 *   K6_PUB_DURATION   (default 2m)   platô do publisher
 *   K6_TOTAL_DURATION (default 2m45s) duração total do cenário WS
 *   K6_WS_LIFETIME_MS (default 160000) tempo que cada WS fica aberto (ms)
 *
 * Saídas:
 *   - reports/combined_baseline_ws-summary.json  (dump completo)
 *   - reports/combined_baseline_ws-summary.html  (resumo visual)
 *   - stdout: resumo + métricas custom de entrega WS.
 */
import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Counter, Trend, Rate } from 'k6/metrics';

import { signWebhook } from '../lib/hmac.js';
import { loadTenantSharedArray } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';
import { buildHandleSummary } from '../lib/summary.js';

const tenants = loadTenantSharedArray();

const wsVUs = Number(__ENV.K6_WS_VUS || 10);
const pubVUs = Number(__ENV.K6_PUB_VUS || 10);
const wsWarmup = __ENV.K6_WS_WARMUP || '15s';
const pubDuration = __ENV.K6_PUB_DURATION || '2h';
const totalDuration = __ENV.K6_TOTAL_DURATION || '2h';
const wsLifetimeMs = Number(__ENV.K6_WS_LIFETIME_MS || 160000);

// Quantos tenants efetivamente cobertos pelos WS — também usado pelo publisher
// para garantir intersecção dos tenants entre os dois cenários.
const sharedTenantCount = Math.min(wsVUs, tenants.length);

const wsMessages = new Counter('ws_messages_received');
const wsPings = new Counter('ws_pings_received');
const wsLatency = new Trend('ws_event_latency_ms', true);
const wsConnects = new Counter('ws_connect_success');
const wsConnFail = new Counter('ws_connect_failed');
const publishedCnt = new Counter('publish_attempts');
const publishedOK = new Counter('publish_accepted');
const publishFail = new Rate('publish_fail_rate');

export const options = {
  scenarios: {
    ws_consumers: {
      executor: 'constant-vus',
      vus: wsVUs,
      duration: totalDuration,
      exec: 'wsConsumer',
      gracefulStop: '15s',
    },
    webhook_publisher: {
      executor: 'ramping-vus',
      startTime: wsWarmup,
      startVUs: 1,
      stages: [
        { duration: '15s', target: pubVUs },
        { duration: pubDuration, target: pubVUs },
        { duration: '15s', target: 0 },
      ],
      gracefulRampDown: '15s',
      exec: 'publisher',
    },
  },
  thresholds: {
    'http_req_failed{name:webhook_ingest}': ['rate<0.15'],
    'http_req_failed{name:ws_token}': ['rate<0.20'],
    ws_messages_received: ['count>0'],
    ws_event_latency_ms: ['p(95)<2000'],
    publish_fail_rate: ['rate<0.15'],
  },
};

function httpToWs(url) {
  if (url.startsWith('https://')) {
    return `wss://${url.slice('https://'.length)}`;
  }
  return `ws://${url.slice('http://'.length)}`;
}

function pickSharedTenant() {
  if (sharedTenantCount <= 0) {
    throw new Error('Tenant pool vazio — rode scripts/create_tenant_pool.sh');
  }
  const idx = (__VU - 1) % sharedTenantCount;
  return tenants[idx];
}

export function publisher() {
  const t = pickSharedTenant();
  const b = baseURL();
  const nowMs = Date.now();
  const body = JSON.stringify({
    event: 'order.created',
    data: {
      order_id: __ITER,
      ts: nowMs / 1000,
      k6_ts_ms: nowMs,
      k6_origin: 'combined_publisher',
    },
  });
  const sig = signWebhook(t.secret_key, body);
  const eventId = `evt-pub-${__VU}-${__ITER}-${nowMs}`;
  const res = http.post(`${b}/v1/webhooks/${t.tenant_id}`, body, {
    headers: {
      'Content-Type': 'application/json',
      'X-Webhook-Signature': sig,
      'X-Event-Id': eventId,
    },
    tags: { name: 'webhook_ingest' },
  });
  publishedCnt.add(1);
  const ok = res.status === 202;
  publishFail.add(!ok);
  if (ok) {
    publishedOK.add(1);
  }
  check(res, { '202 accepted': (r) => r.status === 202 });
  sleep(0.1 + Math.random() * 0.4);
}

export function wsConsumer() {
  const t = pickSharedTenant();
  const httpBase = baseURL();
  const tokRes = http.post(`${httpBase}/v1/tokens/${t.tenant_id}`, null, {
    headers: { Authorization: `Bearer ${t.secret_key}` },
    tags: { name: 'ws_token' },
  });
  if (!check(tokRes, { 'token 200': (r) => r.status === 200 })) {
    wsConnFail.add(1);
    return;
  }
  let token;
  try {
    token = JSON.parse(tokRes.body).token;
  } catch (_) {
    wsConnFail.add(1);
    return;
  }

  const url = `${httpToWs(httpBase)}/ws/events/${t.tenant_id}?token=${encodeURIComponent(token)}`;

  const res = ws.connect(url, {}, function (socket) {
    socket.on('open', () => wsConnects.add(1));

    socket.on('message', (raw) => {
      let parsed;
      try {
        parsed = JSON.parse(raw);
      } catch (_) {
        return;
      }
      // Heartbeat do servidor: { type: 'ping', timestamp }
      if (parsed && parsed.type === 'ping') {
        wsPings.add(1);
        return;
      }
      wsMessages.add(1);
      // Eventos publicados por este cenário carregam k6_ts_ms.
      // Eventos de history (reload) ou ruído externo são contados em
      // ws_messages_received mas não entram na latência.
      const ts = parsed && parsed.data && parsed.data.k6_ts_ms;
      if (typeof ts === 'number') {
        const lat = Date.now() - ts;
        if (lat >= 0 && lat < 60000) {
          wsLatency.add(lat);
        }
      }
    });

    socket.setTimeout(() => {
      try {
        socket.close();
      } catch (_) {
        /* ignore */
      }
    }, wsLifetimeMs);
  });

  check(res, { 'ws 101': (r) => r && r.status === 101 });
  if (!(res && res.status === 101)) {
    wsConnFail.add(1);
  }
}

const baseSummary = buildHandleSummary('combined_baseline_ws');

function metricVal(data, name, key) {
  const m = data && data.metrics && data.metrics[name];
  if (!m || !m.values) return undefined;
  return m.values[key];
}

function fmt(n, digits = 2) {
  if (n === undefined || n === null || Number.isNaN(n)) return '-';
  if (typeof n !== 'number') return String(n);
  return n.toFixed(digits);
}

export function handleSummary(data) {
  const out = baseSummary(data);

  const recv = metricVal(data, 'ws_messages_received', 'count') || 0;
  const pings = metricVal(data, 'ws_pings_received', 'count') || 0;
  const sent = metricVal(data, 'publish_accepted', 'count') || 0;
  const tries = metricVal(data, 'publish_attempts', 'count') || 0;
  const conns = metricVal(data, 'ws_connect_success', 'count') || 0;
  const connFail = metricVal(data, 'ws_connect_failed', 'count') || 0;
  const latAvg = metricVal(data, 'ws_event_latency_ms', 'avg');
  const latP95 = metricVal(data, 'ws_event_latency_ms', 'p(95)');
  const latP99 = metricVal(data, 'ws_event_latency_ms', 'p(99)');
  const latMax = metricVal(data, 'ws_event_latency_ms', 'max');
  const fanout = sent > 0 ? recv / sent : 0;

  const extra = [
    '',
    '---------- Métricas custom (combined_baseline_ws) ----------',
    `  WS conexões aceitas       : ${fmt(conns, 0)} (falhas: ${fmt(connFail, 0)})`,
    `  Tenants compartilhados    : ${sharedTenantCount} (de ${tenants.length} no pool)`,
    `  Publisher: aceitos/total  : ${fmt(sent, 0)} / ${fmt(tries, 0)}`,
    `  Mensagens recebidas no WS : ${fmt(recv, 0)} (pings: ${fmt(pings, 0)})`,
    `  Razão recv/sent           : ${fmt(fanout, 2)}  (esperado >= 1.0 se 1 WS por tenant)`,
    `  Latência WS avg / p95     : ${fmt(latAvg, 2)} ms / ${fmt(latP95, 2)} ms`,
    `  Latência WS p99 / max     : ${fmt(latP99, 2)} ms / ${fmt(latMax, 2)} ms`,
    '------------------------------------------------------------',
    '',
  ].join('\n');

  out.stdout = (out.stdout || '') + extra;
  return out;
}
