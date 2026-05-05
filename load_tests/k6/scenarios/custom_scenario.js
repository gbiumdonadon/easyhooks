import http from 'k6/http';
import { check, sleep } from 'k6';

import { signWebhook } from '../lib/hmac.js';
import { loadTenantSharedArray, pickTenant } from '../lib/tenants.js';
import { baseURL } from '../lib/http.js';

const tenants = loadTenantSharedArray();

const target = Number(__ENV.K6_CUSTOM_TARGET || 30);
const dur = __ENV.K6_DURATION || '5m';

export const options = {
  scenarios: {
    custom: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '30s', target },
        { duration: dur, target },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '30s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.2'],
  },
};

export default function () {
  const t = pickTenant(tenants);
  const b = baseURL();
  const body = JSON.stringify({ event: 'custom.load', data: { vu: __VU } });
  const sig = signWebhook(t.secret_key, body);
  const res = http.post(`${b}/v1/webhooks/${t.tenant_id}`, body, {
    headers: {
      'Content-Type': 'application/json',
      'X-Webhook-Signature': sig,
      'X-Event-Id': `cust-${__VU}-${__ITER}-${Date.now()}`,
    },
    tags: { name: 'webhook_ingest' },
  });
  check(res, { '202': (r) => r.status === 202 });
  sleep(Number(__ENV.K6_CUSTOM_SLEEP || 0.2));
}
