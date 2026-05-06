/*
 * Cenário: FAILURE_TRACKING
 * -------------------------
 * Objetivo: rastrear falhas observadas pelo gerador (não-202, timeout, erro
 * de transporte) e persistir cada uma como entrada num stream Redis dedicado
 * (`loadtest:dlq`) para investigação posterior.
 *
 * Diferente da DLQ do worker (`events:failed`), que só recebe eventos que
 * passaram a validação HTTP e esgotaram retries, este stream captura
 * **qualquer falha** vista do lado do k6 — incluindo 4xx de validação,
 * 429 de load-shedding, 5xx, timeouts e erros de conexão.
 *
 * Perfil de carga (ramping-vus):
 *   - 30s subindo de 1 -> 20 VUs
 *   - 2m  estáveis em 20 VUs
 *   - 30s descendo -> 0 VUs
 *
 * Mix de tráfego (controlado por LOADTEST_FAILURE_RATIO, default 0.10):
 *   - (1 - ratio): requisições válidas (HMAC + headers corretos).
 *   - ratio: requisições inválidas, sorteadas entre:
 *       - bad_signature    : HMAC assinado com chave aleatória → 401
 *       - missing_event_id : sem header X-Event-Id            → 400
 *       - unknown_tenant   : tenant_id UUID inexistente       → 401/403
 *
 * Resultado esperado:
 *   - Quase 100% dos `kind:valid` retornam 202 (threshold < 5% de falha).
 *   - Pelo menos 1 escrita no `loadtest:dlq` (threshold count>=1).
 *   - Após o run, `redis-cli XREVRANGE loadtest:dlq + - COUNT 20` deve mostrar
 *     entradas detalhadas com tenant_id, event_id, status, error, latency_ms.
 *   - `loadtest_worker_dlq_after - loadtest_worker_dlq_before` ≈ 0 em ambiente
 *     saudável (a fila do worker não cresce só por causa de 4xx do gerador).
 */
import http from 'k6/http';
import { sleep } from 'k6';

import { signWebhook } from '../lib/hmac.js';
import { loadTenantSharedArray, pickTenant } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';
import { buildHandleSummary } from '../lib/summary.js';
import {
  recordFailure,
  classifyFailure,
  workerDLQLen,
  workerDLQBefore,
  workerDLQAfter,
  dlqConfig,
} from '../lib/dlq.js';

const tenants = loadTenantSharedArray();

const FAILURE_RATIO = Math.max(
  0,
  Math.min(1, Number(__ENV.LOADTEST_FAILURE_RATIO || 0.1)),
);
const FAILURE_KINDS = ['bad_signature', 'missing_event_id', 'unknown_tenant'];
const SCENARIO_NAME = 'failure_tracking';

export const options = {
  scenarios: {
    failures: {
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
    // Requisições válidas precisam continuar passando: o teste só faz sentido
    // se o gerador injeta apenas a fração esperada de falhas.
    'http_req_failed{kind:valid}': ['rate<0.05'],
    // Pelo menos uma falha deve ter sido capturada (caso contrário, ou o
    // FAILURE_RATIO está zero, ou o Redis está inacessível).
    loadtest_dlq_writes: ['count>=1'],
    // Erros ao tentar escrever no Redis de DLQ devem ser zero em ambiente OK.
    loadtest_dlq_write_errors: ['count==0'],
    // Submetric thresholds (sempre passam) só servem para forçar o k6 a expor
    // a contagem por kind no `data.metrics` consumido pelo handleSummary.
    'loadtest_failures_by_kind{kind:bad_signature}': ['count>=0'],
    'loadtest_failures_by_kind{kind:missing_event_id}': ['count>=0'],
    'loadtest_failures_by_kind{kind:unknown_tenant}': ['count>=0'],
    'loadtest_failures_by_kind{kind:valid}': ['count>=0'],
    'loadtest_dlq_writes{kind:bad_signature}': ['count>=0'],
    'loadtest_dlq_writes{kind:missing_event_id}': ['count>=0'],
    'loadtest_dlq_writes{kind:unknown_tenant}': ['count>=0'],
    'loadtest_dlq_writes{kind:valid}': ['count>=0'],
  },
};

function buildPayload() {
  return JSON.stringify({
    event: 'order.created',
    data: { order_id: __ITER, ts: Date.now() / 1000 },
  });
}

function fakeTenantId() {
  // UUID válido sintaticamente, mas que não existe no pool.
  const hex = (n) => Math.floor(Math.random() * 0xffffffff).toString(16).padStart(n, '0');
  return `${hex(8)}-${hex(4).slice(0, 4)}-4${hex(4).slice(0, 3)}-8${hex(4).slice(0, 3)}-${hex(8)}${hex(4)}`;
}

/** Constrói a requisição conforme o tipo desejado. */
function buildRequest(kind, t) {
  const b = baseURL();
  const eventId = `evt-${__VU}-${__ITER}-${Date.now()}`;
  const body = buildPayload();

  switch (kind) {
    case 'bad_signature': {
      const sig = signWebhook(`fake-secret-${__VU}-${__ITER}`, body);
      return {
        url: `${b}/v1/webhooks/${t.tenant_id}`,
        body,
        headers: {
          'Content-Type': 'application/json',
          'X-Webhook-Signature': sig,
          'X-Event-Id': eventId,
        },
        tenantId: t.tenant_id,
        eventId,
      };
    }
    case 'missing_event_id': {
      const sig = signWebhook(t.secret_key, body);
      return {
        url: `${b}/v1/webhooks/${t.tenant_id}`,
        body,
        headers: {
          'Content-Type': 'application/json',
          'X-Webhook-Signature': sig,
        },
        tenantId: t.tenant_id,
        eventId: '',
      };
    }
    case 'unknown_tenant': {
      const fakeTid = fakeTenantId();
      const sig = signWebhook(t.secret_key, body);
      return {
        url: `${b}/v1/webhooks/${fakeTid}`,
        body,
        headers: {
          'Content-Type': 'application/json',
          'X-Webhook-Signature': sig,
          'X-Event-Id': eventId,
        },
        tenantId: fakeTid,
        eventId,
      };
    }
    case 'valid':
    default: {
      const sig = signWebhook(t.secret_key, body);
      return {
        url: `${b}/v1/webhooks/${t.tenant_id}`,
        body,
        headers: {
          'Content-Type': 'application/json',
          'X-Webhook-Signature': sig,
          'X-Event-Id': eventId,
        },
        tenantId: t.tenant_id,
        eventId,
      };
    }
  }
}

export async function setup() {
  const before = await workerDLQLen();
  workerDLQBefore.add(before);
  return {
    workerDLQBefore: before,
    config: dlqConfig,
    failureRatio: FAILURE_RATIO,
  };
}

export default async function () {
  const t = pickTenant(tenants);
  const isFailure = Math.random() < FAILURE_RATIO;
  const kind = isFailure
    ? FAILURE_KINDS[Math.floor(Math.random() * FAILURE_KINDS.length)]
    : 'valid';

  const req = buildRequest(kind, t);
  const res = http.post(req.url, req.body, {
    headers: req.headers,
    tags: { name: 'webhook_ingest', kind },
  });

  const transportError = res.error_code && res.error_code !== 0;
  const failed = transportError || res.status !== 202;

  if (failed) {
    await recordFailure({
      scenario: SCENARIO_NAME,
      kind,
      injected: isFailure ? 'true' : 'false',
      tenant_id: req.tenantId,
      event_id: req.eventId,
      status: String(res.status || 0),
      error: res.error || '',
      error_code: String(res.error_code || 0),
      classification: classifyFailure(res),
      latency_ms: String(Math.round(res.timings ? res.timings.duration : 0)),
      body_excerpt: (res.body || '').slice(0, 256),
      vu: String(__VU),
      iter: String(__ITER),
      ts: String(Date.now()),
    });
  }

  sleep(0.1 + Math.random() * 0.4);
}

export async function teardown(_data) {
  const after = await workerDLQLen();
  workerDLQAfter.add(after);
}

export const handleSummary = buildHandleSummary(SCENARIO_NAME);
