import { hmac } from 'k6/crypto';

/**
 * X-Webhook-Signature value: sha256=<hex> (same rules as go-api/internal/security).
 */
export function signWebhook(secret, body) {
  const hex = hmac('sha256', secret, body, 'hex');
  return `sha256=${hex}`;
}
