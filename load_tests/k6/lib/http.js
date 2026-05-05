/** Normalize base URL without trailing slash. */
export function baseURL() {
  const u = __ENV.LOADTEST_API_BASE_URL || 'http://localhost:8000';
  return u.replace(/\/$/, '');
}
