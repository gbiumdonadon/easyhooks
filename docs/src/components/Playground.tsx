import React, {useState} from 'react';

import EventConsole from './EventConsole';
import WebhookSimulator from './WebhookSimulator';

export default function Playground(): React.ReactElement {
  const [tenantId, setTenantId] = useState('');
  const [secretKey, setSecretKey] = useState('');
  const [apiBaseUrl, setApiBaseUrl] = useState('http://localhost:8000');

  return (
    <div className="webhook-playground">
      <section className="playground-panel playground-credentials">
        <h3>Credenciais</h3>
        <p className="playground-hint">
          Use os valores retornados ao criar o tenant (<code>POST /admin/tenants</code>). A secret é enviada como{' '}
          <code>Authorization: Bearer …</code> e também serve de base para o HMAC exibido no simulador.
        </p>

        <label className="playground-label">
          URL da API (origem do FastAPI)
          <input
            className="playground-input playground-input-mono"
            value={apiBaseUrl}
            onChange={(e) => setApiBaseUrl(e.target.value)}
            placeholder="http://localhost:8000"
            spellCheck={false}
          />
        </label>

        <label className="playground-label">
          Tenant ID (UUID)
          <input
            className="playground-input playground-input-mono"
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
            placeholder="00000000-0000-0000-0000-000000000000"
            spellCheck={false}
          />
        </label>

        <label className="playground-label">
          Secret Key
          <input
            className="playground-input playground-input-mono"
            type="password"
            autoComplete="off"
            value={secretKey}
            onChange={(e) => setSecretKey(e.target.value)}
            placeholder="••••••••"
            spellCheck={false}
          />
        </label>
      </section>

      <div className="playground-grid">
        <WebhookSimulator tenantId={tenantId} secretKey={secretKey} apiBaseUrl={apiBaseUrl} />
        <EventConsole tenantId={tenantId} secretKey={secretKey} apiBaseUrl={apiBaseUrl} />
      </div>
    </div>
  );
}
