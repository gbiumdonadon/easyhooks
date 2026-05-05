/*
 * Cenário: BASELINE
 * -----------------
 * Objetivo: estabelecer a linha-base de comportamento da API de ingestão de
 * webhooks sob carga moderada e estável.
 *
 * Perfil de carga (ramping-vus):
 *   - 30s subindo de 1  -> 20 VUs
 *   - 2m  estáveis em 20 VUs (platô principal)
 *   - 30s descendo  -> 0 VUs
 *   - Duração máxima total: ~3m + 30s de gracefulRampDown.
 *
 * Carga estimada (RPS máximo aproximado):
 *   Cada VU faz ~1 req a cada 0.1-0.5s (sleep aleatório), ou seja ~3 req/s/VU.
 *   Com 20 VUs no platô o RPS gira em torno de 50-70 req/s (pico ~80 req/s).
 *
 * Resultado esperado:
 *   - HTTP 202 em praticamente todas as requisições.
 *   - http_req_failed < 15% (threshold). Em ambiente saudável esperar < 1%.
 *   - Latência p95 estável; sem crescimento de filas no Redis Streams.
 *   - Use este cenário como referência antes de comparar throughput/stress.
 */
import http from 'k6/http';
import { check, sleep } from 'k6';

import { signWebhook } from '../lib/hmac.js';
import { loadTenantSharedArray, pickTenant } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';
import { buildHandleSummary } from '../lib/summary.js';

const tenants = loadTenantSharedArray();

export const options = {
  scenarios: {
    webhooks: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '30s', target: 20 },
        { duration: '2m', target: 20 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.15'],
  },
};

export default function () {
  const t = pickTenant(tenants);
  const b = baseURL();
  const body = JSON.stringify({
    event: 'order.created',
    data: { order_id: __ITER, ts: Date.now() / 1000 },
  });
  const sig = signWebhook(t.secret_key, body);
  const eventId = `evt-${__VU}-${__ITER}-${Date.now()}`;
  const res = http.post(`${b}/v1/webhooks/${t.tenant_id}`, body, {
    headers: {
      'Content-Type': 'application/json',
      'X-Webhook-Signature': sig,
      'X-Event-Id': eventId,
    },
    tags: { name: 'webhook_ingest' },
  });
  check(res, { '202 accepted': (r) => r.status === 202 });
  sleep(0.1 + Math.random() * 0.4);
}

export const handleSummary = buildHandleSummary('baseline');
