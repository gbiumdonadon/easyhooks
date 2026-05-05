/*
 * Cenário: THROUGHPUT (constant arrival rate)
 * -------------------------------------------
 * Objetivo: medir a vazão máxima sustentada do ingestor com uma taxa de
 * chegada constante (independente da latência), simulando picos/SLO de RPS.
 *
 * Perfil de carga (constant-arrival-rate):
 *   - rate     = K6_TARGET_RPS (default 150 req/s)
 *   - duration = K6_DURATION   (default 3m)
 *   - VUs pré-alocados: K6_PREALLOC_VUS (80) ; máximos: K6_MAX_VUS (400)
 *   - Duração máxima total: K6_DURATION (default 3m).
 *
 * RPS máximo:
 *   Configurado pela variável K6_TARGET_RPS (default 150 RPS, mas pode ser
 *   elevado ao limite suportado pelos VUs disponíveis - até ~400 RPS com os
 *   defaults antes de virar bottleneck do gerador).
 *
 * Resultado esperado:
 *   - http_req_failed < 20% (threshold).
 *   - O k6 deve manter o RPS configurado; se a API saturar, ele alocará VUs
 *     adicionais (até maxVUs) e `dropped_iterations` indicará perda.
 *   - Use para validar capacidade nominal e calibrar autoscaling.
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
    spike: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.K6_TARGET_RPS || 150),
      timeUnit: '1s',
      duration: __ENV.K6_DURATION || '3m',
      preAllocatedVUs: Number(__ENV.K6_PREALLOC_VUS || 80),
      maxVUs: Number(__ENV.K6_MAX_VUS || 400),
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.2'],
  },
};

export default function () {
  const t = pickTenant(tenants);
  const b = baseURL();
  const body = JSON.stringify({
    event: 'order.created',
    data: { ts: Date.now() / 1000 },
  });
  const sig = signWebhook(t.secret_key, body);
  const res = http.post(`${b}/v1/webhooks/${t.tenant_id}`, body, {
    headers: {
      'Content-Type': 'application/json',
      'X-Webhook-Signature': sig,
      'X-Event-Id': `evt-${__VU}-${__ITER}-${Date.now()}`,
    },
    tags: { name: 'webhook_ingest' },
  });
  check(res, { '202': (r) => r.status === 202 });
  sleep(0.02);
}

export const handleSummary = buildHandleSummary('throughput');
