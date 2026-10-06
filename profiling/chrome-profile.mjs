// Dedicated Chrome integration test. CDP records CPU, allocation sampling,
// WebSocket traffic, DOM counts and process memory; it does not attach to the
// user's existing browser profile. Run with an isolated run-stack.py state.json.
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(path.join(root, 'ui/package.json'));
const { chromium } = require('@playwright/test');
const [stateFile, dashboardName = 'Buses', duration = '150'] = process.argv.slice(2);
if (!stateFile) throw new Error('Usage: node profiling/chrome-profile.mjs STATE_JSON [DASHBOARD] [SECONDS]');
const state = JSON.parse(await fs.readFile(stateFile, 'utf8'));
const combined = process.env.XACT_PROFILE_COMBINED === '1';
const label = process.env.XACT_PROFILE_LABEL || dashboardName.toLowerCase().replaceAll(' ', '-');
const output = path.join(state.output, label);
await fs.mkdir(output, { recursive: true });
const chrome = await chromium.launch({ executablePath: '/usr/bin/google-chrome', headless: false,
  args: ['--disable-dev-shm-usage', '--force-renderer-accessibility', '--js-flags=--max-old-space-size=1536'] });
const context = await chrome.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage();
const cdp = await context.newCDPSession(page);
const browserCdp = await chrome.newBrowserCDPSession();
await cdp.send('Performance.enable');
await cdp.send('Profiler.enable');
await cdp.send('Profiler.setSamplingInterval', { interval: 1000 });
await cdp.send('HeapProfiler.enable');
const captureNetwork = process.env.XACT_PROFILE_NETWORK === '1';
// Recording every WebSocket payload can itself be expensive. Enable this for
// traffic-volume diagnosis, then compare a capture with Network disabled.
if (captureNetwork) await cdp.send('Network.enable');
await cdp.send('HeapProfiler.startSampling', { samplingInterval: 32768 });
await cdp.send('Profiler.start');
let wsFrames = 0, wsBytes = 0, httpBytes = 0, busCoordinateFrames = 0;
cdp.on('Network.webSocketFrameReceived', ({ response }) => {
  wsFrames++;
  // CDP represents binary frames as base64. Count decoded payload bytes.
  wsBytes += Buffer.byteLength(response.payloadData, response.opcode === 2 ? 'base64' : 'utf8');
  const payload = response.opcode === 2 ? Buffer.from(response.payloadData, 'base64').toString('utf8') : response.payloadData;
  if (payload.includes('xact.internal.bcast.tagvalue.') && payload.includes('.PUBLIC_BUS.BUSES.') && /\.meta\.(lat|lon)\b/.test(payload)) busCoordinateFrames++;
});
cdp.on('Network.loadingFinished', ({ encodedDataLength }) => { httpBytes += encodedDataLength; });
const errors = [];
page.on('pageerror', error => errors.push(String(error)));
page.on('console', msg => { if (msg.type() === 'error' && errors.length < 100) errors.push(msg.text().slice(0, 1000)); });
let sampleLog;
let completed = false;
let stopReason = 'duration';
const gcEverySwitches = Number(process.env.XACT_PROFILE_GC_EVERY_SWITCHES || 0);
let lastGcSwitch = 0;
const gcSamples = [];
try {
  console.log('Opening isolated XACT');
  await page.goto(state.url);
  await page.getByRole('textbox', { name: 'Login Name / Email', exact: true }).fill('admin');
  await page.getByRole('textbox', { name: 'Password', exact: true }).fill(state.password);
  await page.getByRole('button', { name: 'Sign In', exact: true }).click();
  await page.getByRole('textbox', { name: 'Password', exact: true }).waitFor({ state: 'hidden' });
  console.log('Signed in');
  if (combined) {
    await page.getByRole('button', { name: 'Buses', exact: true }).click();
    await page.getByRole('status').filter({ hasText: 'Connected' }).waitFor({ state: 'visible', timeout: 30000 });
    await page.locator('#tab-add-btn').click();
    console.log('Added internal tab');
  }
  const dashboard = state.dashboards.find(d => d.name === dashboardName);
  if (!dashboard) throw new Error(`Missing dashboard ${dashboardName}`);
  await page.getByRole('button', { name: dashboardName, exact: true }).click();
  await page.getByRole('status').filter({ hasText: 'Connected' }).waitFor({ state: 'visible', timeout: 30000 });
  console.log(`Opened ${dashboardName}`);
  if (dashboardName === 'Tags Manager') {
    await page.locator('[data-node-path="default.PUBLIC_BUS"]').click({ timeout: 60000 });
    await page.locator('[data-node-path="default.PUBLIC_BUS.BUSES"]').click({ timeout: 60000 });
    // Select one bus group so value rows are visible, matching the reported use.
    const rows = page.locator('.tv-node-row[data-node-path^="default.PUBLIC_BUS.BUSES."]');
    await rows.first().waitFor({ state: 'visible', timeout: 60000 });
    if (await rows.count() === 0) throw new Error('No bus groups found');
    await rows.first().dispatchEvent('click');
    const meta = page.locator('.tv-node-row[data-node-path^="default.PUBLIC_BUS.BUSES."][data-node-path$=".meta"]');
    if (await meta.count() > 0) await meta.first().dispatchEvent('click');
  }
  console.log(`Profiling Chrome: ${dashboardName}, ${duration}s; output=${output}`);
  sampleLog = await fs.open(path.join(output, 'metrics.jsonl'), 'w');
  const start = Date.now();
  let lastCpuTime = 0, lastSample = start;
  let switchCount = 0;
  while (Date.now() - start < Number(duration) * 1000) {
    if (combined && Date.now() - start >= (switchCount + 1) * 20000) {
      const title = switchCount % 2 === 0 ? 'Buses' : 'Tags Manager';
      await page.locator('.xact-tab').filter({ has: page.locator('.xact-tab-title', { hasText: new RegExp(`^${title}$`) }) }).click();
      switchCount++;
    }
    if (gcEverySwitches > 0 && switchCount > 0 && switchCount % gcEverySwitches === 0 && switchCount !== lastGcSwitch) {
      await cdp.send('HeapProfiler.collectGarbage');
      const { metrics } = await cdp.send('Performance.getMetrics');
      gcSamples.push({ elapsed: (Date.now() - start) / 1000, switchCount, ...Object.fromEntries(metrics.map(m => [m.name, m.value])) });
      lastGcSwitch = switchCount;
      await fs.writeFile(path.join(output, 'gc-cycles.json'), JSON.stringify(gcSamples, null, 2));
    }
    const { metrics } = await cdp.send('Performance.getMetrics');
    const entry = { elapsed: (Date.now() - start) / 1000, ...Object.fromEntries(metrics.map(m => [m.name, m.value])), captureNetwork, combined, switchCount, wsFrames, wsBytes, httpBytes };
    const { processInfo } = await browserCdp.send('SystemInfo.getProcessInfo');
    let rssMiB = 0, cpuTime = 0;
    for (const proc of processInfo) {
      cpuTime += proc.cpuTime;
      try {
        const stat = await fs.readFile(`/proc/${proc.id}/stat`, 'utf8');
        const fields = stat.slice(stat.lastIndexOf(') ') + 2).split(' ');
        rssMiB += Number(fields[21]) * 4096 / 2 ** 20;
      } catch { /* process exited between samples */ }
    }
    const now = Date.now();
    entry.chromeRssMiB = rssMiB;
    entry.chromeCpuPercent = lastCpuTime ? (cpuTime - lastCpuTime) / ((now - lastSample) / 1000) * 100 : 0;
    lastCpuTime = cpuTime; lastSample = now;
    await sampleLog.write(JSON.stringify(entry) + '\n');
    if (entry.JSHeapUsedSize > 1200 * 2 ** 20 || rssMiB > 2300) {
      stopReason = 'memory-budget';
      console.log('Chrome memory budget reached; ending capture before OOM');
      break;
    }
    await new Promise(resolve => setTimeout(resolve, 2000));
  }
  await fs.writeFile(path.join(output, 'network.json'), JSON.stringify({ wsFrames, wsBytes, httpBytes, busCoordinateFrames }));
  if (process.env.XACT_PROFILE_REQUIRE_ROUTES === '1') {
    await page.locator('path.leaflet-interactive').first().waitFor({ state: 'attached', timeout: 30000 });
    const drawnRoutes = await page.locator('path.leaflet-interactive').evaluateAll(paths => paths.filter(path => {
      const data = path.getAttribute('d');
      return data && data !== 'M0 0';
    }).length);
    await fs.writeFile(path.join(output, 'functional.json'), JSON.stringify({ drawnRoutes }));
    if (drawnRoutes < 5) throw new Error(`Expected visible Vancouver route geometry, found ${drawnRoutes} drawn paths`);
  }
  await page.screenshot({ path: path.join(output, 'dashboard.png'), timeout: 15000 }).catch(error => errors.push(String(error)));
  completed = true;
} catch (error) {
  errors.push(String(error));
  await page.screenshot({ path: path.join(output, 'failure.png'), timeout: 10000 }).catch(() => {});
  await fs.writeFile(path.join(output, 'failure-dom.html'), await page.content().catch(() => ''));
  throw error;
} finally {
  // Capture profiles before closing Chrome so the files survive a test failure.
  for (const [name, method] of [['cpu.cpuprofile', 'Profiler.stop'], ['allocations.heapprofile', 'HeapProfiler.stopSampling']]) {
    try { const result = await cdp.send(method); await fs.writeFile(path.join(output, name), JSON.stringify(result.profile)); }
    catch (error) { errors.push(String(error)); }
  }
  try {
    await cdp.send('HeapProfiler.collectGarbage');
    await fs.writeFile(path.join(output, 'after-gc.json'), JSON.stringify(await cdp.send('Performance.getMetrics')));
  } catch (error) { errors.push(String(error)); }
  await fs.writeFile(path.join(output, 'errors.json'), JSON.stringify(errors, null, 2));
  await fs.writeFile(path.join(output, 'capture-status.json'), JSON.stringify({ completed, stopReason, dashboardName, combined, gcEverySwitches, duration: Number(duration), captureNetwork, chromeVersion: chrome.version() }));
  await sampleLog?.close();
  await chrome.close();
  console.log(`Capture ${completed ? 'complete' : 'aborted'}: ${output}`);
}
