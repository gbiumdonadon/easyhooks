/*
 * Cenário: STRESS
 * ---------------
 * Objetivo: empurrar a aplicação muito além do regime nominal para encontrar
 * o ponto de saturação (CPU, conexões, fila Redis Streams, workers).
 *
 * Perfil de carga (ramping-vus, agressivo):
 *   - 1m subindo de 10  -> 100 VUs
 *   - 3m subindo de 100 -> 400 VUs (pico)
 *   - 1m descendo  -> 0 VUs
 *   - Duração máxima total: 5m + 1m de gracefulRampDown.
 *
 * Carga estimada (RPS máximo aproximado):
 *   Cada VU envia ~1 req a cada 0.05-0.20s (~5-20 req/s/VU). No pico de 400
 *   VUs isso pode gerar entre ~2.000 e ~8.000 req/s teóricos - o teto real é
 *   limitado pela latência da API (quando a API trava, o RPS cai).
 *
 * Resultado esperado:
 *   - http_req_failed < 35% (threshold mais permissivo - é stress).
 *   - Espera-se ver a latência p95/p99 crescer e eventuais 5xx no pico.
 *   - O sistema deve se recuperar durante o ramp-down sem deixar streams
 *     pendurados nem aumentar permanentemente a DLQ.
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
    stress: {
      executor: 'ramping-vus',
      startVUs: 10,
      stages: [
        { duration: '1m', target: 100 },
        { duration: '3m', target: 400 },
        { duration: '1m', target: 0 },
      ],
      gracefulRampDown: '1m',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.35'],
  },
};

export default function () {
  const t = pickTenant(tenants);
  const b = baseURL();
  const body = JSON.stringify({ event: 'stress.ping', data: { t: Date.now() } });
  const sig = signWebhook(t.secret_key, body);
  const res = http.post(`${b}/v1/webhooks/${t.tenant_id}`, body, {
    headers: {
      'Content-Type': 'application/json',
      'X-Webhook-Signature': sig,
      'X-Event-Id': `st-${__VU}-${__ITER}-${Date.now()}`,
    },
    tags: { name: 'webhook_ingest' },
  });
  check(res, { '202': (r) => r.status === 202 });
  sleep(0.05 + Math.random() * 0.15);
}

export const handleSummary = buildHandleSummary('stress');
