// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Início Rápido',
      collapsed: false,
      items: [
        'getting-started/autenticacao',
        'getting-started/primeiro-evento',
      ],
    },
    {
      type: 'category',
      label: 'Referência da API',
      collapsed: false,
      items: [
        'api-reference/ingestor',
        'api-reference/seguranca-hmac',
        'api-reference/tokens-ws',
      ],
    },
    {
      type: 'category',
      label: 'WebSockets & Tempo Real',
      collapsed: false,
      items: [
        'websockets/conexao',
        'websockets/protocolo',
      ],
    },
    {
      type: 'category',
      label: 'Erros e DLQ',
      collapsed: false,
      items: [
        'errors/codigos-http',
        'errors/retentativas-dlq',
      ],
    },
    'playground',
  ],
};

export default sidebars;
