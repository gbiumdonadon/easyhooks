import React, {useCallback, useEffect, useRef, useState} from 'react';

export type EventConsoleProps = {
  tenantId: string;
  secretKey: string;
  apiBaseUrl: string;
};

type ConnStatus = 'disconnected' | 'connecting' | 'connected' | 'error';

type LogLine = {id: string; ts: string; text: string};

function normalizeBaseUrl(url: string): string {
  return url.trim().replace(/\/+$/, '');
}

function httpBaseToWsBase(httpBase: string): string {
  const b = normalizeBaseUrl(httpBase);
  if (b.startsWith('https://')) {
    return `wss://${b.slice('https://'.length)}`;
  }
  if (b.startsWith('http://')) {
    return `ws://${b.slice('http://'.length)}`;
  }
  return b;
}

function formatTime(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export default function EventConsole({
  tenantId,
  secretKey,
  apiBaseUrl,
}: EventConsoleProps): React.ReactElement {
  const [status, setStatus] = useState<ConnStatus>('disconnected');
  const [lines, setLines] = useState<LogLine[]>([]);
  const [lastError, setLastError] = useState<string | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const terminalRef = useRef<HTMLDivElement | null>(null);

  const appendLine = useCallback((text: string) => {
    const id =
      typeof crypto !== 'undefined' && crypto.randomUUID
        ? crypto.randomUUID()
        : `${Date.now()}-${Math.random()}`;
    setLines((prev) => [...prev, {id, ts: formatTime(), text}]);
  }, []);

  useEffect(() => {
    return () => {
      wsRef.current?.close();
      wsRef.current = null;
    };
  }, []);

  useEffect(() => {
    const el = terminalRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lines]);

  const disconnect = useCallback(() => {
    wsRef.current?.close();
    wsRef.current = null;
    setStatus('disconnected');
    appendLine('[local] WebSocket fechado.');
  }, [appendLine]);

  const connect = useCallback(async () => {
    setLastError(null);
    appendLine('[local] Conectando…');

    const base = normalizeBaseUrl(apiBaseUrl);
    const tid = tenantId.trim();
    const secret = secretKey.trim();

    if (!base || !tid || !secret) {
      const msg = 'Preencha URL da API, Tenant ID e Secret Key.';
      setLastError(msg);
      appendLine(`[local] Erro: ${msg}`);
      setStatus('error');
      return;
    }

    setStatus('connecting');

    try {
      const tokenUrl = `${base}/v1/tokens/${tid}`;
      const tokenRes = await fetch(tokenUrl, {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${secret}`,
        },
      });

      if (!tokenRes.ok) {
        const t = await tokenRes.text();
        throw new Error(`Token HTTP ${tokenRes.status}: ${t || tokenRes.statusText}`);
      }

      const json = (await tokenRes.json()) as {token?: string};
      const token = json.token;
      if (!token) {
        throw new Error('Resposta sem campo token.');
      }

      const wsBase = httpBaseToWsBase(base);
      const wsUrl = `${wsBase}/ws/events/${tid}?token=${encodeURIComponent(token)}`;

      appendLine(`[local] Abrindo WebSocket…`);

      wsRef.current?.close();
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setStatus('connected');
        appendLine('[local] Conectado ao gateway. Aguardando eventos…');
      };

      ws.onmessage = (ev) => {
        const raw = typeof ev.data === 'string' ? ev.data : String(ev.data);
        try {
          const parsed = JSON.parse(raw);
          appendLine(JSON.stringify(parsed, null, 2));
        } catch {
          appendLine(raw);
        }
      };

      ws.onerror = () => {
        setLastError('Erro no WebSocket (verifique CORS/rede).');
        setStatus('error');
      };

      ws.onclose = (ev) => {
        wsRef.current = null;
        setStatus('disconnected');
        appendLine(`[local] WebSocket encerrado (code=${ev.code}${ev.reason ? `, reason=${ev.reason}` : ''}).`);
      };
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      setLastError(msg);
      setStatus('error');
      appendLine(`[local] Falha: ${msg}`);
    }
  }, [apiBaseUrl, appendLine, secretKey, tenantId]);

  const clear = useCallback(() => {
    setLines([]);
  }, []);

  return (
    <section className="playground-panel playground-console">
      <h3>Console de Eventos</h3>
      <p className="playground-hint">
        Obtém um token com <code>POST /v1/tokens/&#123;tenant_id&#125;</code> (Bearer) e escuta{' '}
        <code>/ws/events/&#123;tenant_id&#125;</code>. Dispare um evento no simulador para ver a mensagem aqui
        (após o worker publicar no Redis).
      </p>

      <div className="playground-console-toolbar">
        <span className={`playground-conn-pill playground-conn-pill--${status}`}>
          {status === 'disconnected' && 'Desconectado'}
          {status === 'connecting' && 'Conectando…'}
          {status === 'connected' && 'Conectado'}
          {status === 'error' && 'Erro'}
        </span>
        <div className="playground-inline-actions">
          <button type="button" className="button button--primary button--sm" onClick={() => void connect()} disabled={status === 'connecting' || status === 'connected'}>
            Conectar
          </button>
          <button type="button" className="button button--secondary button--sm" onClick={disconnect} disabled={status === 'disconnected'}>
            Desconectar
          </button>
          <button type="button" className="button button--outline button--sm" onClick={clear}>
            Limpar
          </button>
        </div>
      </div>

      {lastError ? (
        <div className="playground-alert playground-alert--warn" role="status">
          {lastError}
        </div>
      ) : null}

      <div ref={terminalRef} className="playground-terminal" aria-live="polite">
        {lines.length === 0 ? (
          <div className="playground-terminal-empty playground-muted">Nenhum evento ainda.</div>
        ) : (
          lines.map((l) => (
            <div key={l.id} className="playground-terminal-line">
              <span className="playground-terminal-ts">{l.ts}</span>
              <pre className="playground-terminal-pre">{l.text}</pre>
            </div>
          ))
        )}
      </div>
    </section>
  );
}
