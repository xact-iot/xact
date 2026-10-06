import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const require = createRequire(path.join(root, 'ui/package.json'));
const { TraceMap, originalPositionFor } = require('@jridgewell/trace-mapping');
const directory = process.argv[2];
if (!directory) throw new Error('Usage: node profiling/summarize-chrome.mjs CAPTURE_DIRECTORY');
try {
  const status = JSON.parse(await fs.readFile(path.join(directory, 'capture-status.json'), 'utf8'));
  if (!status.completed) throw new Error('Capture was aborted; inspect errors.json before analyzing it');
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
const maps = new Map();
async function frameLabel(frame) {
  let name = frame.functionName || '(anonymous)';
  if (!frame.url) return name;
  const basename = path.basename(new URL(frame.url).pathname);
  let map = maps.get(basename);
  if (map === undefined) {
    try { map = new TraceMap(JSON.parse(await fs.readFile(path.join(process.env.XACT_PROFILE_UI_DIR || path.join(root, 'profiling/results/ui'), 'assets', basename + '.map'), 'utf8'))); }
    catch { map = null; }
    maps.set(basename, map);
  }
  if (map && frame.lineNumber >= 0) {
    const original = originalPositionFor(map, { line: frame.lineNumber + 1, column: frame.columnNumber });
    if (original.source) return `${original.name || name} (${original.source}:${original.line})`;
  }
  return `${name} (${basename}:${frame.lineNumber + 1})`;
}
const cpu = JSON.parse(await fs.readFile(path.join(directory, 'cpu.cpuprofile'), 'utf8'));
const nodes = new Map(cpu.nodes.map(n => [n.id, n]));
const labels = new Map();
for (const node of cpu.nodes) labels.set(node.id, await frameLabel(node.callFrame));
const weights = new Map();
for (let i = 0; i < cpu.samples.length; i++) {
  const label = labels.get(cpu.samples[i]);
  weights.set(label, (weights.get(label) || 0) + cpu.timeDeltas[i] / 1e6);
}
const active = [...weights].filter(([label]) => !['(idle)', '(root)'].includes(label));
const total = active.reduce((sum, [, seconds]) => sum + seconds, 0);
const allocations = JSON.parse(await fs.readFile(path.join(directory, 'allocations.heapprofile'), 'utf8'));
const memory = new Map();
async function visit(node) {
  if (node.selfSize) {
    const label = await frameLabel(node.callFrame);
    memory.set(label, (memory.get(label) || 0) + node.selfSize);
  }
  for (const child of node.children) await visit(child);
}
await visit(allocations.head);
const samples = (await fs.readFile(path.join(directory, 'metrics.jsonl'), 'utf8')).trim().split('\n').map(JSON.parse);
const last = samples.at(-1);
const first = samples[0];
const postGC = Object.fromEntries(JSON.parse(await fs.readFile(path.join(directory, 'after-gc.json'), 'utf8')).metrics.map(m => [m.name, m.value]));
let report = `# Chrome profile: ${path.basename(directory)}\n\n`;
report += `Window: ${last.elapsed.toFixed(1)} seconds. Active sampled CPU: ${total.toFixed(2)} seconds.\n\n`;
if (last.combined) report += `Buses and Tags Manager open in internal XACT tabs; ${last.switchCount} tab switches recorded.\n\n`;
report += `JS heap: ${(first.JSHeapUsedSize / 2 ** 20).toFixed(1)} → ${(last.JSHeapUsedSize / 2 ** 20).toFixed(1)} MiB; after forced GC: ${(postGC.JSHeapUsedSize / 2 ** 20).toFixed(1)} MiB.\n\n`;
report += `DOM nodes: ${first.Nodes} → ${last.Nodes}; after GC: ${postGC.Nodes}. Listeners: ${first.JSEventListeners} → ${last.JSEventListeners}.\n\n`;
report += last.captureNetwork === false ? 'WebSocket payload recording disabled to reduce profiling overhead.\n\n' : `WebSocket frames: ${last.wsFrames}; payload bytes: ${(last.wsBytes / 2 ** 20).toFixed(1)} MiB.\n\n`;
if (last.chromeRssMiB) report += `Chrome process RSS sum: peak ${Math.max(...samples.map(s => s.chromeRssMiB)).toFixed(1)} MiB. Peak interval CPU: ${Math.max(...samples.map(s => s.chromeCpuPercent)).toFixed(1)}% (100% = one core). RSS sums count shared pages more than once.\n\n`;
report += '| CPU self time | Active % | Function |\n|---:|---:|---|\n';
for (const [label, seconds] of active.sort((a, b) => b[1] - a[1]).slice(0, 25)) report += `| ${seconds.toFixed(3)}s | ${(seconds / total * 100).toFixed(1)}% | ${label} |\n`;
report += '\nAllocation sampling estimates allocations still live when sampling ended; it is not total allocated bytes or proof of a leak.\n\n| Sampled live allocation | Function |\n|---:|---|\n';
for (const [label, bytes] of [...memory].sort((a, b) => b[1] - a[1]).slice(0, 20)) report += `| ${(bytes / 2 ** 20).toFixed(2)} MiB | ${label} |\n`;
await fs.writeFile(path.join(directory, 'summary.md'), report);
console.log(report);
