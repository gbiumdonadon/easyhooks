import http from 'k6/http';
import { check, sleep } from 'k6';

import { signWebhook } from '../lib/hmac.js';
import { loadTenantSharedArray } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';

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
