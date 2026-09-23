// Isolated component proofs. Uses synthetic data and never contacts an XACT server.
// Run from ui: node security-audit/reproduce.mjs
// Optionally set AUDIT_CHROMIUM_EXECUTABLE to an installed Chromium executable.
import { build } from 'esbuild';
import { chromium } from '@playwright/test';
import { createServer } from 'node:http';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ui = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const dir = await mkdtemp(join(tmpdir(), 'xact-ui-audit-'));
let browser;
let server;
try {
  await build({
    stdin: {
      contents: `
        import './src/dashboards/widgets/text-widget';
        import './src/dashboards/widgets/html-widget';
        import './src/dashboards/widgets/tags-manager-widget';
        import './src/dashboards/widgets/map-widget';
        import './src/dashboards/widgets/timeseries-chart-widget';
        import './src/components/app-sidebar';
        import { parseSvgTemplate } from './src/dashboards/widgets/svg-diagram-widget';
        import { sanitizeHtml } from './src/utils/html-sanitize';
        import { getMirrorStore } from './src/store/store';
        window.audit = { parseSvgTemplate, sanitizeHtml, getMirrorStore };
      `,
      resolveDir: ui,
      loader: 'ts',
    },
    bundle: true,
    platform: 'browser',
    format: 'iife',
    outfile: join(dir, 'audit.js'),
    define: { 'import.meta.env': '{}' },
    loader: { '.css': 'empty', '.png': 'dataurl' },
    logLevel: 'silent',
  });
  const bundle = await readFile(join(dir, 'audit.js'));
  server = createServer((req, res) => {
    // Match the current Go server's CSP; event handlers and eval remain allowed.
    res.setHeader('Content-Security-Policy', "frame-ancestors 'none'; object-src 'none'; base-uri 'self'");
    if (req.url === '/audit.js') {
      res.setHeader('Content-Type', 'application/javascript');
      res.end(bundle);
    } else if (req.url === '/') {
      res.setHeader('Content-Type', 'text/html');
      res.end('<!doctype html><html><body><script src="/audit.js"></script></body></html>');
    } else {
      res.writeHead(404, { 'Content-Type': 'application/json' });
      res.end('{}');
    }
  });
  await new Promise((ok, fail) => { server.once('error', fail); server.listen(0, '127.0.0.1', ok); });
  const origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({
    headless: true,
    ...(process.env.AUDIT_CHROMIUM_EXECUTABLE ? { executablePath: process.env.AUDIT_CHROMIUM_EXECUTABLE } : {}),
  });
  const context = await browser.newContext();
  await context.route('**/*', route => new URL(route.request().url()).origin === origin
    ? route.continue() : route.abort());
  const page = await context.newPage();
  await page.goto(origin);
  const results = await page.evaluate(async () => {
    const { getMirrorStore, sanitizeHtml, parseSvgTemplate } = window.audit;
    const store = getMirrorStore();
    store.orgName = 'audit';
    const token = 'SYNTHETIC-AUDIT-TOKEN-NOT-A-REAL-CREDENTIAL';
    localStorage.setItem('xact_auth_token', token);
    window.auditProofs = {};
    const proof = key => `window.auditProofs[${JSON.stringify(key)}]=localStorage.getItem('xact_auth_token')`;
    const img = key => `<img src="/missing-audit-image" onerror="${proof(key).replaceAll('"', '&quot;')}">`;
    const pause = () => new Promise(ok => setTimeout(ok, 120));
    const host = html => { const el = document.createElement('div'); el.innerHTML = html; document.body.append(el); return el; };
    const data = {};

    // Initial tag-table rendering, with the real mirror-store metadata loader.
    const tags = document.createElement('tags-manager-widget');
    const path = 'audit.device.message';
    store.applyTagMetadataToNode(path, { value: img('tag-value'), shared: {} });
    tags.valueCache.set(path, { value: store.resolveTagReference(path), timestamp: 1, status: '' });
    let el = host(tags.renderLeafTable([path], 0));
    await pause(); el.remove();
    store.applyTagMetadataToNode(path, { value: 42, shared: { units: img('tag-units') } });
    tags.valueCache.set(path, { value: 42, timestamp: 1, status: '' });
    el = host(tags.renderLeafTable([path], 0));
    await pause(); el.remove();

    // Map template evaluation and the returned HTML supplied to Leaflet divIcon.
    const map = document.createElement('area-map-widget');
    map.renderTemplateContent({ divTemplate: '${' + proof('map-expression') + '}' }, 'audit.device');
    store.applyTagMetadataToNode(path, { value: img('map-tag-value'), shared: {} });
    el = host(map.evaluateTemplate({ divTemplate: "${tag('message')}" }, 'audit.device'));
    await pause(); el.remove();

    // Saved configuration bypasses the SVG file-import sanitizer.
    const svg = document.createElement('svg-diagram-widget');
    svg.setConfig({ templateSvg: { viewBox: '0 0 100 100', content: '</svg>' + img('saved-svg') } });
    document.body.append(svg);
    await pause(); svg.remove();

    // Actual TextWidget setter/render; a valid color-picker UI is not validation of saved JSON.
    const color = 'red;">' + img('text-color') + '<div style="';
    const text = document.createElement('text-widget');
    text.setConfig({ color }); document.body.append(text);
    await pause(); text.remove();

    // Sanitizer strips onerror in ordinary HTML, but preserves active custom elements/config.
    const nested = document.createElement('text-widget');
    nested.setAttribute('config', JSON.stringify({ color: 'red;">' + img('custom-element') + '<div style="' }));
    const payload = nested.outerHTML;
    data.customElementSurvivedSanitizer = sanitizeHtml(payload).includes('text-widget');
    store.applyTagMetadataToNode(path, { value: payload, shared: {} });
    const html = document.createElement('html-widget');
    html.setConfig({ html: '{tag:audit.device.message}' });
    document.body.append(html);
    await pause(); html.remove();
    el = host(sanitizeHtml(img('ordinary-html-control')));
    await pause(); el.remove();

    // Organisation logo uses a text-only escaper inside a quoted HTML attribute.
    const sidebar = document.createElement('app-sidebar');
    sidebar.currentOrg = 'audit';
    sidebar.orgDetails.set('audit', { name: 'audit', logo: '/missing-audit-image" onerror="' + proof('org-logo').replaceAll('"', "'") });
    sidebar.render();
    el = host(sidebar.innerHTML);
    await pause(); el.remove();

    // Application's custom ECharts formatter, independent of the upstream Lines-series CVE.
    const chart = document.createElement('timeseries-chart-widget');
    const option = chart.buildChartOption();
    el = host(option.tooltip.formatter([{ value: [Date.now(), 42], color: '#fff', seriesName: img('chart-tooltip') }]));
    await pause(); el.remove();

    // SVG file sanitizer permits SMIL URL mutation; check only through the actual renderer.
    const parsed = parseSvgTemplate(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><a id="audit-svg-link" href="#"><set attributeName="href" to="javascript:${proof('svg-smil').replaceAll('"', '&quot;')}"/><rect width="100" height="100" fill="red" pointer-events="all"/></a></svg>`);
    const imported = document.createElement('svg-diagram-widget');
    imported.style.cssText = 'display:block;width:200px;height:200px';
    imported.setConfig({ templateSvg: parsed.template });
    document.body.append(imported);
    await pause();
    data.svgAnimatedHref = document.querySelector('#audit-svg-link')?.href?.animVal;
    data.proofs = { ...window.auditProofs };
    data.ordinaryHtmlControlBlocked = !window.auditProofs['ordinary-html-control'];
    return data;
  });
  // Real browser click, allowing child pointer-events override of the template layer.
  try {
    await page.locator('#audit-svg-link rect').click({ timeout: 2500 });
    await page.waitForTimeout(120);
    results.proofs = await page.evaluate(() => ({ ...window.auditProofs }));
  } catch (error) { results.svgClickError = error.message.split('\n')[0]; }
  results.browserVersion = browser.version();
  console.log(JSON.stringify(results, null, 2));
} finally {
  await browser?.close();
  if (server?.listening) await new Promise(ok => server.close(ok));
  await rm(dir, { recursive: true, force: true });
}
