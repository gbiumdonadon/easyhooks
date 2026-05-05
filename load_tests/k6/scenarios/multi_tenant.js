/*
 * Cenário: MULTI-TENANT
 * ---------------------
 * Objetivo: validar o isolamento e o spread de carga entre múltiplos tenants
 * (cada VU "fixa" um tenant para maximizar a diversidade de tenants ativos
 * simultaneamente).
 *
 * Perfil de carga (ramping-vus):
 *   - 1m   subindo de 5  -> 40 VUs
 *   - 3m   estáveis em 40 VUs (platô)
 *   - 30s  descendo  -> 0 VUs
 *   - Duração máxima total: ~4m30s + 30s de gracefulRampDown.
 *
 * Carga estimada (RPS máximo aproximado):
 *   Cada VU faz ~1 req a cada 0.15-0.50s (~2-3 req/s/VU). No platô de 40 VUs
 *   isso resulta em ~80-120 req/s, distribuídos entre todos os tenants do
 *   pool (.tenant_pool.json).
 *
 * Resultado esperado:
 *   - http_req_failed < 15% (threshold).
 *   - Latência p95 estável e equivalente entre tenants (sem "tenant ruidoso"
 *     impactando os demais).
 *   - Métricas por tenant (Prometheus/Grafana) devem mostrar distribuição
 *     equilibrada de eventos.
 */
import http from 'k6/http';
import { check, sleep } from 'k6';

import { signWebhook } from '../lib/hmac.js';
import { loadTenantSharedArray } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';
import { buildHandleSummary } from '../lib/summary.js';

const tenants = loadTenantSharedArray();

export const options = {
  scenarios: {
    spread: {
      executor: 'ramping-vus',
      startVUs: 5,
      stages: [
        { duration: '1m', target: 40 },
        { duration: '3m', target: 40 },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.15'],
  },
};

/** Each VU sticks to one tenant index to maximise cross-tenant traffic across VUs. */
export default function () {
  const idx = (__VU - 1) % tenants.length;
  const t = tenants[idx];
  const b = baseURL();
  const body = JSON.stringify({
    event: 'multi_tenant.probe',
    data: { vu: __VU, iter: __ITER },
  });
  const sig = signWebhook(t.secret_key, body);
  const res = http.post(`${b}/v1/webhooks/${t.tenant_id}`, body, {
    headers: {
      'Content-Type': 'application/json',
      'X-Webhook-Signature': sig,
      'X-Event-Id': `mt-${t.tenant_id}-${__ITER}-${Date.now()}`,
    },
    tags: { name: 'webhook_ingest' },
  });
  check(res, { '202': (r) => r.status === 202 });
  sleep(0.15 + Math.random() * 0.35);
}

export const handleSummary = buildHandleSummary('multi_tenant');
