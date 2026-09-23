// Verify a production build and locally bundled vendors under the server CSP.
// node security-audit/smoke.mjs /path/to/build
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, readdir } from 'node:fs/promises';
import { resolve, extname, sep } from 'node:path';
import { chromium } from '@playwright/test';

const root = resolve(process.argv[2] || 'dist');
const assets = await readdir(resolve(root, 'assets'));
const moduleUrl = name => '/xact/assets/' + assets.find(file => file.startsWith(name + '-') && file.endsWith('.js'));
const headerSource = await readFile(new URL('../../server/rtdb/api/server_hardening.go', import.meta.url), 'utf8');
const csp = headerSource.match(/h.Set\("Content-Security-Policy", "([^"]+)"\)/)[1];
const mime = { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png' };
const server = createServer(async (req, res) => {
  res.setHeader('Content-Security-Policy', csp);
  res.setHeader('X-Content-Type-Options', 'nosniff');
  if (req.url === '/xact/api/v1/bootstrap/admin') {
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ setupRequired: false, setupEnabled: false, passwordSet: true })); return;
  }
  const pathname = new URL(req.url, 'http://localhost').pathname;
  const relative = pathname === '/xact/' ? 'index.html' : pathname.replace(/^\/xact\//, '');
  const path = resolve(root, relative);
  if (!path.startsWith(root + sep)) { res.writeHead(404); res.end(); return; }
  try {
    const body = await readFile(path);
    res.setHeader('Content-Type', mime[extname(path)] || 'application/octet-stream'); res.end(body);
  } catch { res.writeHead(404, { 'Content-Type': 'application/json' }); res.end('{}'); }
});
let browser;
try {
  await new Promise((ok, fail) => { server.once('error', fail); server.listen(0, '127.0.0.1', ok); });
  const origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({ headless: true, ...(process.env.AUDIT_CHROMIUM_EXECUTABLE ? { executablePath: process.env.AUDIT_CHROMIUM_EXECUTABLE } : {}) });
  const context = await browser.newContext();
  await context.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : route.abort());
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    window.cspViolations = [];
    document.addEventListener('securitypolicyviolation', event => window.cspViolations.push(event.violatedDirective));
  });
  await page.goto(origin + '/xact/');
  await page.locator('login-page input').first().waitFor();
  await page.evaluate(async url => {
    await import(url);
    const editor = document.createElement('html-editor');
    document.body.append(editor); editor.setValue('<p>Smoke test</p>');
    const map = document.createElement('area-map-widget');
    map.style.cssText = 'display:block;width:500px;height:350px';
    map.setConfig({ layers: [] }); document.body.append(map);
  }, moduleUrl('map-widget'));
  await page.waitForFunction(() => window.L && window.CodeMirror && document.querySelector('html-editor .CodeMirror') && document.querySelector('area-map-widget .leaflet-pane'));
  await page.evaluate(async url => {
    await import(url);
    const events = document.createElement('events-viewer-widget');
    events.entries = [{ timestamp: new Date().toISOString(), severity: 'info', message: '=1+1' }];
    await events._onExport();
  }, moduleUrl('events-viewer-widget'));
  const result = await page.evaluate(() => {
    const sheet = window.XLSX.utils.json_to_sheet([{ Value: '=1+1' }]);
    return {
      loginRendered: !!document.querySelector('login-page'),
      codeMirrorModeLoaded: !!window.CodeMirror.modes.htmlmixed,
      leafletLoaded: !!window.L.map,
      spreadsheetCellIsText: sheet.A2.t === 's' && !sheet.A2.f,
      cspViolations: window.cspViolations,
    };
  });
  assert.deepEqual(errors, []);
  assert.deepEqual(result.cspViolations, []);
  assert.equal(result.codeMirrorModeLoaded && result.leafletLoaded && result.spreadsheetCellIsText, true);
  console.log(JSON.stringify({ ...result, browserVersion: browser.version() }, null, 2));
} finally {
  await browser?.close();
  if (server.listening) await new Promise(ok => server.close(ok));
}
