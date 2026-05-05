import http from 'k6/http';
import { check, sleep } from 'k6';

import { signWebhook } from '../lib/hmac.js';
import { loadTenantSharedArray, pickTenant, tenantPoolPath } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';

const tenants = loadTenantSharedArray();

export function setup() {
  const list = JSON.parse(open(tenantPoolPath()));
  const b = baseURL();
  for (let i = 0; i < list.length; i++) {
    const t = list[i];
    const body = JSON.stringify({ event: 'warmup.ping', data: { seq: i } });
    const sig = signWebhook(t.secret_key, body);
    http.post(`${b}/v1/webhooks/${t.tenant_id}`, body, {
      headers: {
        'Content-Type': 'application/json',
        'X-Webhook-Signature': sig,
        'X-Event-Id': `warmup-${i}`,
      },
    });
    if (i % 10 === 9) {
      sleep(0.05);
    }
  }
  return { count: list.length };
}

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
