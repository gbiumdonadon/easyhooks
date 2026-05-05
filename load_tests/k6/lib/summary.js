/**
 * Gera relatórios finais ao término do teste k6.
 *
 * Para cada cenário, produz:
 *   - stdout: resumo textual com as principais métricas
 *   - reports/<scenario>-summary.json: dump completo do `data` do k6
 *   - reports/<scenario>-summary.html: HTML simples e auto-contido com as
 *     métricas mais relevantes (RPS, p95, p99, taxa de erro, checks, etc.)
 *
 * Uso (em cada scenario):
 *   import { buildHandleSummary } from '../lib/summary.js';
 *   export const handleSummary = buildHandleSummary('baseline');
 */

function fmt(n, digits = 2) {
  if (n === undefined || n === null || Number.isNaN(n)) return '-';
  if (typeof n !== 'number') return String(n);
  if (Math.abs(n) >= 1000) return n.toFixed(0);
  return n.toFixed(digits);
}

function getMetric(data, name) {
  return (data && data.metrics && data.metrics[name]) || null;
}

function metricVal(data, name, key) {
  const m = getMetric(data, name);
  if (!m || !m.values) return undefined;
  return m.values[key];
}

function buildTextSummary(scenarioName, data) {
  const reqs = metricVal(data, 'http_reqs', 'count') || 0;
  const rps = metricVal(data, 'http_reqs', 'rate') || 0;
  const failRate = metricVal(data, 'http_req_failed', 'rate') || 0;
  const avg = metricVal(data, 'http_req_duration', 'avg');
  const p95 = metricVal(data, 'http_req_duration', 'p(95)');
  const p99 = metricVal(data, 'http_req_duration', 'p(99)');
  const max = metricVal(data, 'http_req_duration', 'max');
  const vusMax = metricVal(data, 'vus_max', 'max');
  const iters = metricVal(data, 'iterations', 'count') || 0;
  const dataSent = metricVal(data, 'data_sent', 'count') || 0;
  const dataRecv = metricVal(data, 'data_received', 'count') || 0;

  const checks = getMetric(data, 'checks');
  const checksPass = checks && checks.values ? checks.values.passes || 0 : 0;
  const checksFail = checks && checks.values ? checks.values.fails || 0 : 0;
  const checksTotal = checksPass + checksFail;
  const checksRate = checksTotal > 0 ? (checksPass / checksTotal) * 100 : 0;

  const lines = [];
  lines.push('');
  lines.push(`========== Resumo do cenário: ${scenarioName} ==========`);
  lines.push(`  Requisições totais : ${fmt(reqs, 0)}`);
  lines.push(`  RPS médio          : ${fmt(rps, 2)} req/s`);
  lines.push(`  Iterações          : ${fmt(iters, 0)}`);
  lines.push(`  VUs máximos        : ${fmt(vusMax, 0)}`);
  lines.push(`  Taxa de erro HTTP  : ${fmt(failRate * 100, 2)}%`);
  lines.push(`  Checks             : ${checksPass}/${checksTotal} (${fmt(checksRate, 2)}%)`);
  lines.push(`  Latência avg       : ${fmt(avg, 2)} ms`);
  lines.push(`  Latência p95       : ${fmt(p95, 2)} ms`);
  lines.push(`  Latência p99       : ${fmt(p99, 2)} ms`);
  lines.push(`  Latência máx       : ${fmt(max, 2)} ms`);
  lines.push(`  Dados enviados     : ${fmt(dataSent / 1024, 2)} KB`);
  lines.push(`  Dados recebidos    : ${fmt(dataRecv / 1024, 2)} KB`);

  const thresholds = [];
  if (data && data.metrics) {
    for (const [name, m] of Object.entries(data.metrics)) {
      if (!m.thresholds) continue;
      for (const [thr, info] of Object.entries(m.thresholds)) {
        thresholds.push(`    - ${name} :: ${thr} :: ${info.ok ? 'OK' : 'FAIL'}`);
      }
    }
  }
  if (thresholds.length > 0) {
    lines.push('  Thresholds:');
    lines.push(...thresholds);
  }
  lines.push('==========================================================');
  lines.push('');
  return lines.join('\n');
}

function buildHtmlSummary(scenarioName, data) {
  const reqs = metricVal(data, 'http_reqs', 'count') || 0;
  const rps = metricVal(data, 'http_reqs', 'rate') || 0;
  const failRate = metricVal(data, 'http_req_failed', 'rate') || 0;
  const avg = metricVal(data, 'http_req_duration', 'avg');
  const p95 = metricVal(data, 'http_req_duration', 'p(95)');
  const p99 = metricVal(data, 'http_req_duration', 'p(99)');
  const max = metricVal(data, 'http_req_duration', 'max');
  const vusMax = metricVal(data, 'vus_max', 'max');

  const checks = getMetric(data, 'checks');
  const checksPass = checks && checks.values ? checks.values.passes || 0 : 0;
  const checksFail = checks && checks.values ? checks.values.fails || 0 : 0;
  const checksTotal = checksPass + checksFail;
  const checksRate = checksTotal > 0 ? (checksPass / checksTotal) * 100 : 0;

  const thresholdsRows = [];
  if (data && data.metrics) {
    for (const [name, m] of Object.entries(data.metrics)) {
      if (!m.thresholds) continue;
      for (const [thr, info] of Object.entries(m.thresholds)) {
        const status = info.ok ? 'OK' : 'FAIL';
        const cls = info.ok ? 'ok' : 'fail';
        thresholdsRows.push(
          `<tr><td>${name}</td><td><code>${thr}</code></td><td class="${cls}">${status}</td></tr>`,
        );
      }
    }
  }

  const generated = new Date().toISOString();

  return `<!doctype html>
<html lang="pt-br">
<head>
<meta charset="utf-8" />
<title>k6 - ${scenarioName}</title>
<style>
  body { font-family: -apple-system, Segoe UI, Roboto, sans-serif; margin: 24px; background: #0f1115; color: #e6e6e6; }
  h1 { margin-top: 0; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 12px; }
  .card { background: #1a1d24; border: 1px solid #2a2e38; border-radius: 8px; padding: 16px; }
  .label { color: #8b95a7; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
  .value { font-size: 22px; font-weight: 600; margin-top: 6px; }
  table { width: 100%; border-collapse: collapse; margin-top: 12px; }
  th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid #2a2e38; }
  th { color: #8b95a7; font-weight: 500; font-size: 12px; text-transform: uppercase; }
  .ok { color: #4ade80; font-weight: 600; }
  .fail { color: #f87171; font-weight: 600; }
  code { background: #2a2e38; padding: 2px 6px; border-radius: 4px; font-size: 12px; }
  footer { margin-top: 24px; color: #6b7280; font-size: 12px; }
</style>
</head>
<body>
  <h1>Cenário: ${scenarioName}</h1>
  <div class="grid">
    <div class="card"><div class="label">Requisições</div><div class="value">${fmt(reqs, 0)}</div></div>
    <div class="card"><div class="label">RPS médio</div><div class="value">${fmt(rps, 2)}</div></div>
    <div class="card"><div class="label">VUs máximos</div><div class="value">${fmt(vusMax, 0)}</div></div>
    <div class="card"><div class="label">Taxa de erro</div><div class="value">${fmt(failRate * 100, 2)}%</div></div>
    <div class="card"><div class="label">Checks OK</div><div class="value">${checksPass}/${checksTotal} (${fmt(checksRate, 2)}%)</div></div>
    <div class="card"><div class="label">Latência avg</div><div class="value">${fmt(avg, 2)} ms</div></div>
    <div class="card"><div class="label">Latência p95</div><div class="value">${fmt(p95, 2)} ms</div></div>
    <div class="card"><div class="label">Latência p99</div><div class="value">${fmt(p99, 2)} ms</div></div>
    <div class="card"><div class="label">Latência máx</div><div class="value">${fmt(max, 2)} ms</div></div>
  </div>

  <h2>Thresholds</h2>
  <table>
    <thead><tr><th>Métrica</th><th>Threshold</th><th>Status</th></tr></thead>
    <tbody>
      ${thresholdsRows.join('\n      ') || '<tr><td colspan="3">Nenhum threshold configurado.</td></tr>'}
    </tbody>
  </table>

  <footer>Gerado em ${generated} pelo k6.</footer>
</body>
</html>`;
}

/**
 * @param {string} scenarioName Nome curto usado nos arquivos de saída.
 * @returns {(data: any) => Record<string, string>} handleSummary do k6.
 */
export function buildHandleSummary(scenarioName) {
  return function handleSummary(data) {
    const text = buildTextSummary(scenarioName, data);
    const html = buildHtmlSummary(scenarioName, data);
    const out = {};
    out.stdout = text;
    out[`reports/${scenarioName}-summary.json`] = JSON.stringify(data, null, 2);
    out[`reports/${scenarioName}-summary.html`] = html;
    return out;
  };
}
