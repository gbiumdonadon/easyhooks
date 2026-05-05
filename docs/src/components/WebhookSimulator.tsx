import React, {useCallback, useEffect, useMemo, useState} from 'react';

export type WebhookSimulatorProps = {
  tenantId: string;
  secretKey: string;
  apiBaseUrl: string;
};

type HmacStep = {title: string; detail: string};

function normalizeBaseUrl(url: string): string {
  return url.trim().replace(/\/+$/, '');
}

async function computeHmacSha256Hex(secret: string, bodyText: string): Promise<string> {
  const enc = new TextEncoder();
  const keyData = enc.encode(secret);
  const bodyBytes = enc.encode(bodyText);

  const cryptoKey = await crypto.subtle.importKey(
    'raw',
    keyData,
    {name: 'HMAC', hash: 'SHA-256'},
    false,
    ['sign'],
  );

  const sig = await crypto.subtle.sign('HMAC', cryptoKey, bodyBytes);
  const bytes = new Uint8Array(sig);
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

function newEventId(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return `evt-${Date.now()}-${Math.random().toString(36).slice(2, 11)}`;
}

const DEFAULT_PAYLOAD = `{\n  "hello": "world",\n  "source": "playground"\n}`;

export default function WebhookSimulator({
  tenantId,
  secretKey,
  apiBaseUrl,
}: WebhookSimulatorProps): React.ReactElement {
  const [payloadText, setPayloadText] = useState(DEFAULT_PAYLOAD);
  const [eventId, setEventId] = useState(() => newEventId());
  const [steps, setSteps] = useState<HmacStep[]>([]);
  const [signatureHex, setSignatureHex] = useState<string | null>(null);
  const [parseError, setParseError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [lastStatus, setLastStatus] = useState<number | null>(null);
  const [lastBody, setLastBody] = useState<string | null>(null);

  const base = useMemo(() => normalizeBaseUrl(apiBaseUrl || ''), [apiBaseUrl]);

  useEffect(() => {
    let cancelled = false;

    async function run(): Promise<void> {
      if (typeof window === 'undefined' || !secretKey.trim()) {
        setSteps([]);
        setSignatureHex(null);
        return;
      }

      try {
        JSON.parse(payloadText);
      } catch {
        setParseError('JSON inválido no payload.');
        setSteps([]);
        setSignatureHex(null);
        return;
      }

      setParseError(null);

      const bodyForSigning = payloadText;
      const enc = new TextEncoder();
      const bodyBytes = enc.encode(bodyForSigning);

      try {
        const hex = await computeHmacSha256Hex(secretKey.trim(), bodyForSigning);
        if (cancelled) return;

        setSignatureHex(hex);
        setSteps([
          {
            title: '1. Corpo da requisição (bytes UTF-8)',
            detail: `${bodyBytes.length} bytes — o servidor valida o HMAC sobre os bytes exatos enviados no POST.`,
          },
          {
            title: '2. Importar chave HMAC (secret em UTF-8)',
            detail:
              'crypto.subtle.importKey("raw", TextEncoder.encode(secret), { name: "HMAC", hash: "SHA-256" }, …)',
          },
          {
            title: '3. Assinar com HMAC-SHA256',
            detail: 'crypto.subtle.sign("HMAC", key, bodyBytes)',
          },
          {
            title: '4. Digest em hexadecimal (minúsculas)',
            detail: hex,
          },
          {
            title: '5. Header enviado',
            detail: `X-Webhook-Signature: sha256=${hex}`,
          },
        ]);
      } catch (e) {
        if (cancelled) return;
        setSignatureHex(null);
        setSteps([]);
        setParseError(e instanceof Error ? e.message : 'Falha ao calcular HMAC.');
      }
    }

    void run();
    return () => {
      cancelled = true;
    };
  }, [payloadText, secretKey]);

  const ingestUrl = useMemo(() => {
    const id = tenantId.trim();
    if (!base || !id) return '';
    return `${base}/v1/webhooks/${id}`;
  }, [base, tenantId]);

  const handleFire = useCallback(async () => {
    setLastStatus(null);
    setLastBody(null);

    if (!tenantId.trim()) {
      setParseError('Informe o Tenant ID.');
      return;
    }
    if (!secretKey.trim()) {
      setParseError('Informe a Secret Key.');
      return;
    }
    if (!base) {
      setParseError('Informe a URL da API.');
      return;
    }

    try {
      JSON.parse(payloadText);
    } catch {
      setParseError('JSON inválido no payload.');
      return;
    }

    if (!signatureHex) {
      setParseError('Assinatura HMAC indisponível. Corrija os campos acima.');
      return;
    }

    setLoading(true);
    try {
      const res = await fetch(ingestUrl, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${secretKey.trim()}`,
          'X-Webhook-Signature': `sha256=${signatureHex}`,
          'X-Event-Id': eventId.trim() || newEventId(),
        },
        body: payloadText,
      });

      const text = await res.text();
      setLastStatus(res.status);
      setLastBody(text);
    } catch (err) {
      setLastStatus(0);
      setLastBody(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [
    base,
    eventId,
    ingestUrl,
    payloadText,
    secretKey,
    signatureHex,
    tenantId,
  ]);

  return (
    <section className="playground-panel playground-simulator">
      <h3>Simulador de Webhook (The Tester)</h3>
      <p className="playground-hint">
        O payload abaixo é enviado byte-a-byte para o ingestor; o HMAC é calculado sobre{' '}
        <strong>exatamente</strong> esse texto (como em <code>go-api/internal/security/security.go</code>).
      </p>

      <label className="playground-label">
        URL do ingestor
        <input className="playground-input playground-input-mono" readOnly value={ingestUrl || '(preencha Tenant ID e URL da API)'} />
      </label>

      <label className="playground-label">
        X-Event-Id
        <div className="playground-inline-actions">
          <input
            className="playground-input playground-input-mono"
            value={eventId}
            onChange={(e) => setEventId(e.target.value)}
            spellCheck={false}
          />
          <button type="button" className="button button--secondary button--sm" onClick={() => setEventId(newEventId())}>
            Novo ID
          </button>
        </div>
      </label>

      <label className="playground-label">
        Payload JSON
        <textarea
          className="playground-textarea playground-input-mono"
          rows={10}
          value={payloadText}
          onChange={(e) => setPayloadText(e.target.value)}
          spellCheck={false}
        />
      </label>

      {parseError ? (
        <div className="playground-alert playground-alert--warn" role="status">
          {parseError}
        </div>
      ) : null}

      <div className="playground-hmac-steps">
        <h4>Cálculo do HMAC em tempo real</h4>
        {steps.length === 0 ? (
          <p className="playground-muted">Preencha a secret key e um JSON válido para ver os passos.</p>
        ) : (
          <ol className="playground-step-list">
            {steps.map((s) => (
              <li key={s.title}>
                <div className="playground-step-title">{s.title}</div>
                <pre className="playground-step-pre">{s.detail}</pre>
              </li>
            ))}
          </ol>
        )}
      </div>

      <div className="playground-actions">
        <button type="button" className="button button--primary" disabled={loading} onClick={() => void handleFire()}>
          {loading ? 'Enviando…' : 'Disparar'}
        </button>
      </div>

      {lastStatus !== null ? (
        <div className="playground-response">
          <div className="playground-response-header">
            <span className="playground-muted">Resposta</span>
            <span
              className={`playground-status-badge ${
                lastStatus === 202
                  ? 'playground-status-badge--ok'
                  : lastStatus >= 400
                    ? 'playground-status-badge--err'
                    : lastStatus === 0
                      ? 'playground-status-badge--err'
                      : 'playground-status-badge--warn'
              }`}>
              {lastStatus === 0 ? 'Erro de rede' : `HTTP ${lastStatus}`}
            </span>
          </div>
          {lastBody ? <pre className="playground-step-pre">{lastBody}</pre> : null}
        </div>
      ) : null}
    </section>
  );
}
