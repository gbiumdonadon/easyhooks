import { SharedArray } from 'k6/data';

/** Default pool path relative to this module (k6/lib → repo load_tests/.tenant_pool.json). */
export function tenantPoolPath() {
  return __ENV.TENANT_POOL_FILE || '../../.tenant_pool.json';
}

/**
 * Load tenant pool JSON: [{ tenant_id, secret_key }, ...]
 * Set TENANT_POOL_FILE for an absolute path (e.g. in Docker: /load_tests/.tenant_pool.json).
 */
export function loadTenantSharedArray() {
  const path = tenantPoolPath();
  return new SharedArray('tenants', function () {
    const raw = open(path);
    const data = JSON.parse(raw);
    if (!Array.isArray(data) || data.length === 0) {
      throw new Error(`Tenant pool empty or invalid: ${path}`);
    }
    return data;
  });
}

export function pickTenant(tenants) {
  const i = (__VU - 1) % tenants.length;
  return tenants[i];
}
