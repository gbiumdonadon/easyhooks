/**
 * DLQ helper para cenários k6.
 *
 * Persiste cada falha observada do lado do gerador (não-202, timeout, erro
 * de transporte/conexão) num stream Redis dedicado ao load test
 * (`loadtest:dlq` por padrão), separado da DLQ do worker (`events:failed`).
 *
 * Variáveis de ambiente:
 *   - LOADTEST_REDIS_URL          Conexão (default: redis://redis:6379/0)
 *   - LOADTEST_DLQ_STREAM         Stream destino  (default: loadtest:dlq)
 *   - LOADTEST_DLQ_MAX_LEN        Cap aproximado  (default: 50000)
 *   - LOADTEST_WORKER_DLQ_STREAM  DLQ do worker observada (default: events:failed)
 *
 * Métricas expostas (consumidas por `lib/summary.js`):
 *   - loadtest_dlq_writes        : Counter total de XADD bem-sucedidos
 *   - loadtest_dlq_write_errors  : Counter de XADD que falhou (Redis indisponível, etc.)
 *   - loadtest_failures_by_kind  : Counter com tag {kind:<...>} (use submetric threshold)
 *   - loadtest_worker_dlq_before : Counter incrementado uma vez no setup
 *   - loadtest_worker_dlq_after  : Counter incrementado uma vez no teardown
 *
 * Uso básico no cenário:
 *
 *   import { recordFailure, classifyFailure, workerDLQLen,
 *            workerDLQBefore, workerDLQAfter } from '../lib/dlq.js';
 *
 *   export async function setup() {
 *     workerDLQBefore.add(await workerDLQLen());
 *   }
 *   export default async function () {
 *     const res = http.post(...);
 *     if (res.status !== 202) {
 *       await recordFailure({ kind: 'bad_signature', tenant_id, ... });
 *     }
 *   }
 *   export async function teardown() {
 *     workerDLQAfter.add(await workerDLQLen());
 *   }
 */

import { Client } from 'k6/experimental/redis';
import { Counter } from 'k6/metrics';

const REDIS_URL = __ENV.LOADTEST_REDIS_URL || 'redis://redis:6379/0';
const DLQ_STREAM = __ENV.LOADTEST_DLQ_STREAM || 'loadtest:dlq';
const DLQ_MAX_LEN = Number(__ENV.LOADTEST_DLQ_MAX_LEN || 50000);
const WORKER_DLQ_STREAM = __ENV.LOADTEST_WORKER_DLQ_STREAM || 'events:failed';

let _client = null;
function getClient() {
  if (_client === null) _client = new Client(REDIS_URL);
  return _client;
}

export const dlqWrites = new Counter('loadtest_dlq_writes');
export const dlqWriteErrors = new Counter('loadtest_dlq_write_errors');
export const failuresByKind = new Counter('loadtest_failures_by_kind');
export const workerDLQBefore = new Counter('loadtest_worker_dlq_before');
export const workerDLQAfter = new Counter('loadtest_worker_dlq_after');

function flatten(fields) {
  const out = [];
  for (const k of Object.keys(fields)) {
    const v = fields[k];
    if (v === undefined || v === null) continue;
    out.push(String(k));
    out.push(typeof v === 'string' ? v : JSON.stringify(v));
  }
  return out;
}

/**
 * Classifica a resposta http do k6 num rótulo curto e investigável.
 * Cobre tanto erros de transporte (error_code != 0) quanto status HTTP.
 * @param {object} res Objeto retornado por http.post / http.get.
 * @returns {string}
 */
export function classifyFailure(res) {
  if (!res) return 'unknown';
  if (res.error_code && res.error_code !== 0) {
    if (res.error_code === 1050) return 'timeout';
    if (res.error_code >= 1300 && res.error_code <= 1310) return 'connection_error';
    return 'transport_error';
  }
  switch (res.status) {
    case 0:
      return 'network_error';
    case 400:
      return 'bad_request';
    case 401:
      return 'unauthorized';
    case 403:
      return 'forbidden';
    case 404:
      return 'not_found';
    case 422:
      return 'unprocessable';
    case 429:
      return 'load_shed';
    case 500:
      return 'server_error';
    case 502:
      return 'bad_gateway';
    case 503:
      return 'unavailable';
    case 504:
      return 'gateway_timeout';
    default:
      if (res.status >= 500) return 'server_error';
      if (res.status >= 400) return 'client_error';
      return 'other';
  }
}

/**
 * Persiste um registro de falha no stream Redis loadtest:dlq via XADD.
 * Errors são absorvidos: a falha do k6 → Redis nunca quebra o cenário,
 * apenas incrementa loadtest_dlq_write_errors para visibilidade.
 *
 * @param {object} fields Campos arbitrários (todos serializados como string).
 *   Convenção mínima: { scenario, kind, tenant_id, event_id, status, error,
 *   error_code, classification, latency_ms, body_excerpt, vu, iter, ts }.
 * @returns {Promise<void>}
 */
export async function recordFailure(fields) {
  const kind = (fields && fields.kind) || 'unknown';
  failuresByKind.add(1, { kind });
  const flat = flatten(fields);
  try {
    await getClient().sendCommand(
      'XADD',
      DLQ_STREAM,
      'MAXLEN',
      '~',
      String(DLQ_MAX_LEN),
      '*',
      ...flat,
    );
    dlqWrites.add(1, { kind });
  } catch (_e) {
    dlqWriteErrors.add(1, { kind });
  }
}

/**
 * XLEN genérico, usado para inspecionar a DLQ do worker em setup/teardown.
 * @param {string} stream
 * @returns {Promise<number>}
 */
export async function xlen(stream) {
  try {
    const v = await getClient().sendCommand('XLEN', stream);
    return Number(v) || 0;
  } catch (_e) {
    return 0;
  }
}

/** Atalho para `xlen(WORKER_DLQ_STREAM)`. */
export async function workerDLQLen() {
  return xlen(WORKER_DLQ_STREAM);
}

export const dlqConfig = Object.freeze({
  REDIS_URL,
  DLQ_STREAM,
  DLQ_MAX_LEN,
  WORKER_DLQ_STREAM,
});
