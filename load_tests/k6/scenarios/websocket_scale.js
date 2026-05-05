/*
 * Cenário: WEBSOCKET SCALE
 * ------------------------
 * Objetivo: medir a capacidade do servidor de manter conexões WebSocket
 * concorrentes consumindo eventos por tenant (não mede ingestão HTTP, e sim
 * o caminho de fan-out).
 *
 * Perfil de carga (vus + duration constantes):
 *   - vus      = K6_WS_VUS    (default 80 conexões simultâneas)
 *   - duration = K6_DURATION  (default 3m)
 *   - Cada VU obtém um token (POST /v1/tokens/:tenant), abre o WebSocket e
 *     mantém aberto por 60s antes de fechar.
 *   - Duração máxima total: K6_DURATION (default 3m).
 *
 * RPS máximo:
 *   Esse cenário é orientado a CONEXÕES, não a RPS. O número de novas
 *   conexões/seg fica em torno de vus / 60s (~1.3 conn/s com defaults). O
 *   "load" relevante é o número de WebSockets vivos em paralelo (~vus).
 *
 * Resultado esperado:
 *   - http_req_failed < 20% (threshold) considerando a chamada de token.
 *   - Check `ws 101` deve passar em todas as conexões (upgrade aceito).
 *   - Sem vazamento de goroutines/conexões no servidor após o teste.
 *   - Latência de entrega de eventos publicados ao tenant deve permanecer
 *     baixa (verificar via dashboards de observabilidade).
 */
import http from 'k6/http';
import ws from 'k6/experimental/websockets';
import { check } from 'k6';

import { loadTenantSharedArray, pickTenant } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';
import { buildHandleSummary } from '../lib/summary.js';

const tenants = loadTenantSharedArray();

const vus = Number(__ENV.K6_WS_VUS || 80);
const duration = __ENV.K6_DURATION || '3m';

export const options = {
  vus,
  duration,
  thresholds: {
    http_req_failed: ['rate<0.2'],
  },
};

function httpToWs(url) {
  if (url.startsWith('https://')) {
    return `wss://${url.slice('https://'.length)}`;
  }
  return `ws://${url.slice('http://'.length)}`;
}

export default function () {
  const t = pickTenant(tenants);
  const httpBase = baseURL();
  const tokRes = http.post(`${httpBase}/v1/tokens/${t.tenant_id}`, null, {
    headers: { Authorization: `Bearer ${t.secret_key}` },
    tags: { name: 'ws_token' },
  });
  check(tokRes, { 'token 200': (r) => r.status === 200 });
  if (tokRes.status !== 200) {
    return;
  }
  let token;
  try {
    token = JSON.parse(tokRes.body).token;
  } catch (e) {
    return;
  }

  const wsRoot = httpToWs(httpBase);
  const url = `${wsRoot}/ws/events/${t.tenant_id}?token=${encodeURIComponent(token)}`;

  const res = ws.connect(url, {}, function (socket) {
    socket.on('message', () => {});
    socket.setTimeout(() => {
      try {
        socket.close();
      } catch (e) {
        /* ignore */
      }
    }, 60000);
  });

  check(res, { 'ws 101': (r) => r && r.status === 101 });
}

export const handleSummary = buildHandleSummary('websocket_scale');
