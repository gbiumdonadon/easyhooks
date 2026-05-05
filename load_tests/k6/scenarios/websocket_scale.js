import http from 'k6/http';
import ws from 'k6/experimental/websockets';
import { check } from 'k6';

import { loadTenantSharedArray, pickTenant } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';

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
