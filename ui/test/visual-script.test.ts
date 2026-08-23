import { describe, expect, it, vi } from 'vitest';
import { DraftStore } from '../src/visual-scripts/draft-store';
import { emptyGraph } from '../src/visual-scripts/types';
import type { NodeDefinition } from '../src/visual-scripts/types';
import '../src/visual-scripts/canvas';
import '../src/visual-scripts/editor';
import '../src/components/app-content';
import '../src/dashboards/dashboard-container';
import '../src/dashboards/widgets/visual-script-widget';

describe('visual script draft and canvas', () => {
  it('keeps graph dirty state and undo independent from dashboard state', () => {
    const store = new DraftStore(emptyGraph());
    const changed = store.value;
    changed.nodes.push({ id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 10, y: 10 }, config: {} });
    store.update(changed);
    expect(store.dirty).toBe(true);
    expect(store.canUndo).toBe(true);
    expect(store.undo().nodes).toHaveLength(0);
    expect(store.redo().nodes).toHaveLength(1);
    store.markSaved(store.value);
    expect(store.dirty).toBe(false);
  });

  it('creates named output-to-input connections without library-owned serialization', () => {
    if (!(globalThis as any).CSS) (globalThis as any).CSS = {};
    if (!(globalThis as any).CSS.escape) (globalThis as any).CSS.escape = (value: string) => value;
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 20, y: 20 }, config: {} },
      { id: 'debug', type: 'core.debug', typeVersion: 1, position: { x: 300, y: 20 }, config: {} },
    ];
    const catalog: NodeDefinition[] = [
      { type: 'core.manual', typeVersion: 1, name: 'Manual', description: '', category: 'Triggers', icon: '▶', inputs: [], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true },
      { type: 'core.debug', typeVersion: 1, name: 'Debug', description: '', category: 'Actions', icon: '◎', inputs: [{ name: 'in', label: 'Input', dataType: 'message' }], outputs: [], parameters: [], available: true },
    ];
    const canvas = document.createElement('visual-script-canvas') as any;
    const changed = vi.fn(); canvas.addEventListener('visual-graph-change', changed); document.body.appendChild(canvas); canvas.setData(graph, catalog, false);
    canvas.querySelector('[data-kind="out"]').click(); canvas.querySelector('[data-kind="in"]').click();
    expect(changed).toHaveBeenCalledTimes(1);
    expect(graph.edges[0]).toMatchObject({ from: { nodeId: 'manual', port: 'out' }, to: { nodeId: 'debug', port: 'in' } });
    const canvasStyles = canvas.querySelector('style').textContent;
    expect(canvasStyles).toContain('width:150px');
    expect(canvasStyles).toContain('background:#ede6d9;color:#3d3428');
    expect(canvasStyles).toContain('border:1px solid #b8aa93');
    expect(canvasStyles).toContain('outline:2px solid var(--accent-color)');
    expect(canvasStyles).not.toContain('background:var(--widget-header-bg)');
    expect(canvasStyles).not.toContain('background:var(--widget-header-surface');
    expect(canvasStyles).not.toContain('color:var(--widget-header-text');
    expect(canvasStyles).toContain('background:var(--vs-category-bg)');
    expect(canvas.querySelector('[aria-label="Manual node"]')?.getAttribute('style')).toContain('--vs-category-bg:#245c48');
    expect(canvas.querySelector('[aria-label="Debug node"]')?.getAttribute('style')).toContain('--vs-category-bg:#673848');
    expect(canvas.querySelector('.vsc-type')).toBeNull();
    expect(canvas.querySelector('[aria-label="Manual node"]')?.textContent).not.toContain('manual');
    canvas.remove();
  });

  it('previews and creates a connection by dragging an output onto an input', () => {
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 20, y: 20 }, config: {} },
      { id: 'debug', type: 'core.debug', typeVersion: 1, position: { x: 300, y: 20 }, config: {} },
    ];
    const catalog: NodeDefinition[] = [
      { type: 'core.manual', typeVersion: 1, name: 'Manual', description: '', category: 'Triggers', icon: '▶', inputs: [], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true },
      { type: 'core.debug', typeVersion: 1, name: 'Debug', description: '', category: 'Actions', icon: '◎', inputs: [{ name: 'in', label: 'Input', dataType: 'message' }], outputs: [], parameters: [], available: true },
    ];
    const canvas = document.createElement('visual-script-canvas') as any;
    const changed = vi.fn(); canvas.addEventListener('visual-graph-change', changed); document.body.appendChild(canvas); canvas.setData(graph, catalog, false);

    let output = canvas.querySelector<HTMLButtonElement>('[data-kind="out"]')!;
    output.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, clientX: 100, clientY: 100 }));
    expect(canvas.classList.contains('connecting')).toBe(true);
    expect(canvas.querySelector('.vsc-draft-edge')).toBeTruthy();
    document.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0, clientX: 150, clientY: 150 }));
    expect(graph.edges).toHaveLength(0);
    expect(canvas.querySelector('.vsc-draft-edge')).toBeNull();

    output = canvas.querySelector<HTMLButtonElement>('[data-kind="out"]')!;
    const input = canvas.querySelector<HTMLButtonElement>('[data-kind="in"]')!;
    output.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, clientX: 100, clientY: 100 }));
    input.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, button: 0, clientX: 300, clientY: 100 }));
    expect(input.classList.contains('drop-target')).toBe(true);
    input.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0, clientX: 300, clientY: 100 }));

    expect(changed).toHaveBeenCalledTimes(1);
    expect(graph.edges[0]).toMatchObject({ from: { nodeId: 'manual', port: 'out' }, to: { nodeId: 'debug', port: 'in' } });
    expect(canvas.classList.contains('connecting')).toBe(false);
    canvas.remove();
  });

  it('selects a connection and deletes it with the keyboard', () => {
    if (!(globalThis as any).CSS) (globalThis as any).CSS = {};
    if (!(globalThis as any).CSS.escape) (globalThis as any).CSS.escape = (value: string) => value;
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 20, y: 20 }, config: {} },
      { id: 'debug', type: 'core.debug', typeVersion: 1, position: { x: 300, y: 20 }, config: {} },
    ];
    graph.edges = [{ id: 'edge-1', from: { nodeId: 'manual', port: 'out' }, to: { nodeId: 'debug', port: 'in' } }];
    const catalog: NodeDefinition[] = [
      { type: 'core.manual', typeVersion: 1, name: 'Manual', description: '', category: 'Triggers', icon: '▶', inputs: [], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true },
      { type: 'core.debug', typeVersion: 1, name: 'Debug', description: '', category: 'Actions', icon: '◎', inputs: [{ name: 'in', label: 'Input', dataType: 'message' }], outputs: [], parameters: [], available: true },
    ];
    const canvas = document.createElement('visual-script-canvas') as any;
    const changed = vi.fn(); canvas.addEventListener('visual-graph-change', changed); document.body.appendChild(canvas); canvas.setData(graph, catalog, false);

    canvas.querySelector<SVGPathElement>('.vsc-edge-hit')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(canvas.querySelector('.vsc-edge.selected')).toBeTruthy();
    expect(canvas.querySelectorAll('.vsc-edge-handle')).toHaveLength(2);
    expect(document.activeElement).toBe(canvas.querySelector('.vsc-edge-hit'));
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: '.', code: 'NumpadDecimal' }));

    expect(graph.edges).toHaveLength(0);
    expect(changed).toHaveBeenCalledTimes(1);
    canvas.remove();
  });

  it('reconnects either end of a selected connection and preserves it after an invalid drop', () => {
    if (!(globalThis as any).CSS) (globalThis as any).CSS = {};
    if (!(globalThis as any).CSS.escape) (globalThis as any).CSS.escape = (value: string) => value;
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'manual-1', type: 'core.manual', typeVersion: 1, position: { x: 20, y: 20 }, config: {} },
      { id: 'manual-2', type: 'core.manual', typeVersion: 1, position: { x: 20, y: 160 }, config: {} },
      { id: 'debug-1', type: 'core.debug', typeVersion: 1, position: { x: 300, y: 20 }, config: {} },
      { id: 'debug-2', type: 'core.debug', typeVersion: 1, position: { x: 300, y: 160 }, config: {} },
    ];
    graph.edges = [{ id: 'edge-1', from: { nodeId: 'manual-1', port: 'out' }, to: { nodeId: 'debug-1', port: 'in' } }];
    const catalog: NodeDefinition[] = [
      { type: 'core.manual', typeVersion: 1, name: 'Manual', description: '', category: 'Triggers', icon: '▶', inputs: [], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true },
      { type: 'core.debug', typeVersion: 1, name: 'Debug', description: '', category: 'Actions', icon: '◎', inputs: [{ name: 'in', label: 'Input', dataType: 'message' }], outputs: [], parameters: [], available: true },
    ];
    const canvas = document.createElement('visual-script-canvas') as any;
    const changed = vi.fn(); canvas.addEventListener('visual-graph-change', changed); document.body.appendChild(canvas); canvas.setData(graph, catalog, false);
    canvas.querySelector<SVGPathElement>('.vsc-edge-hit')!.dispatchEvent(new MouseEvent('click', { bubbles: true }));

    let targetHandle = canvas.querySelector<SVGCircleElement>('[data-end="to"]')!;
    targetHandle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, clientX: 300, clientY: 70 }));
    document.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0, clientX: 220, clientY: 100 }));
    expect(graph.edges[0].to.nodeId).toBe('debug-1');
    expect(changed).not.toHaveBeenCalled();

    targetHandle = canvas.querySelector<SVGCircleElement>('[data-end="to"]')!;
    const secondInput = canvas.querySelector<HTMLButtonElement>('[data-kind="in"][data-node="debug-2"]')!;
    targetHandle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, clientX: 300, clientY: 70 }));
    secondInput.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, button: 0, clientX: 300, clientY: 210 }));
    expect(secondInput.classList.contains('drop-target')).toBe(true);
    secondInput.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0, clientX: 300, clientY: 210 }));
    expect(graph.edges[0].to.nodeId).toBe('debug-2');

    const sourceHandle = canvas.querySelector<SVGCircleElement>('[data-end="from"]')!;
    const secondOutput = canvas.querySelector<HTMLButtonElement>('[data-kind="out"][data-node="manual-2"]')!;
    sourceHandle.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, button: 0, clientX: 170, clientY: 70 }));
    secondOutput.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, button: 0, clientX: 170, clientY: 210 }));
    secondOutput.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, button: 0, clientX: 170, clientY: 210 }));

    expect(graph.edges[0]).toMatchObject({ from: { nodeId: 'manual-2', port: 'out' }, to: { nodeId: 'debug-2', port: 'in' } });
    expect(changed).toHaveBeenCalledTimes(2);
    canvas.remove();
  });

  it('drags parameterless palette nodes onto the canvas at the drop position', () => {
    const editor = document.createElement('visual-script-editor') as any;
    const manual = {
      type: 'core.manual', typeVersion: 1, name: 'Manual', description: '', category: 'Triggers', icon: '▶',
      inputs: null, outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: null, available: true,
    } as unknown as NodeDefinition;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'stopped', latestRevision: 0,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, emptyGraph(), [manual]);
    document.body.appendChild(editor);

    const paletteItem = editor.querySelector<HTMLElement>('[data-add="core.manual"]')!;
    const canvas = editor.querySelector<HTMLElement>('#vse-canvas')!;
    paletteItem.click();
    expect(editor.draft.value.nodes).toHaveLength(0);
    expect(paletteItem.draggable).toBe(true);
    expect(paletteItem.getAttribute('aria-label')).toBe('Drag Manual node to canvas');

    paletteItem.getBoundingClientRect = () => ({ left: 10, top: 10, right: 170, bottom: 40, width: 160, height: 30, x: 10, y: 10, toJSON: () => ({}) });
    canvas.getBoundingClientRect = () => ({ left: 100, top: 50, right: 900, bottom: 650, width: 800, height: 600, x: 100, y: 50, toJSON: () => ({}) });
    canvas.scrollLeft = 40;
    canvas.scrollTop = 20;
    const values = new Map<string, string>();
    const dataTransfer = {
      effectAllowed: 'none', dropEffect: 'none',
      setData: (type: string, value: string) => values.set(type, value),
      getData: (type: string) => values.get(type) || '',
    } as unknown as DataTransfer;
    const dragEvent = (type: string, clientX: number, clientY: number) => {
      const event = new Event(type, { bubbles: true, cancelable: true });
      Object.defineProperties(event, { dataTransfer: { value: dataTransfer }, clientX: { value: clientX }, clientY: { value: clientY } });
      return event;
    };
    paletteItem.dispatchEvent(dragEvent('dragstart', 20, 25));
    canvas.dispatchEvent(dragEvent('dragover', 350, 250));
    expect(canvas.classList.contains('palette-drop-target')).toBe(true);
    canvas.dispatchEvent(dragEvent('drop', 350, 250));

    expect(editor.draft.value.nodes).toHaveLength(1);
    expect(editor.draft.value.nodes[0]).toMatchObject({ type: 'core.manual', position: { x: 280, y: 210 }, config: {} });
    expect(editor.querySelector('style').textContent).toContain('z-index:3100');
    expect(editor.querySelector('style').textContent).toContain('grid-template-columns:180px minmax(360px,1fr) 230px');
    expect(editor.querySelector('[data-category="Triggers"]')?.getAttribute('style')).toContain('--vs-category-bg:#245c48');
    expect(editor.querySelector('#vse-category-0')?.getAttribute('style')).toContain('--vs-category-bg:#245c48');
    expect(editor.querySelector('[data-node-id]')?.getAttribute('style')).toContain('--vs-category-bg:#245c48');
    expect(editor.querySelector('style').textContent).toContain('var(--vs-category-bg) 38%');
    const triggersHeader = editor.querySelector('[data-category="Triggers"]') as HTMLButtonElement;
    expect(triggersHeader.getAttribute('aria-expanded')).toBe('true');
    triggersHeader.click();
    expect((editor.querySelector('[data-category="Triggers"]') as HTMLButtonElement).getAttribute('aria-expanded')).toBe('false');
    expect(editor.querySelector('#vse-category-0')?.hasAttribute('hidden')).toBe(true);
    editor.remove();
  });

  it('mounts an operator plugin editor and applies configuration updates through the draft store', () => {
    const graph = emptyGraph();
    graph.nodes = [{ id: 'custom', type: 'acme.uppercase', typeVersion: 1, position: { x: 20, y: 20 }, config: { prefix: '> ' } }];
    const modulePath = '/plugins/visual-script-nodes/acme/editor.js';
    const definition: NodeDefinition = {
      type: 'acme.uppercase', typeVersion: 1, name: 'Uppercase', description: '', category: 'Custom', icon: 'A',
      inputs: [{ name: 'in', label: 'Input', dataType: 'message' }], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }],
      parameters: [{ name: 'prefix', label: 'Prefix', type: 'string' }], editorModule: modulePath, available: true,
    };
    const editor = document.createElement('visual-script-editor') as any;
    const dispose = vi.fn(); let pluginContext: any;
    editor.pluginEditors.set(modulePath, { mount(container: HTMLElement, context: any) { container.textContent = 'Custom editor mounted'; pluginContext = context; return dispose; } });
    editor.initialize({ id: 'script-1', name: 'Plugin script', description: '', desiredState: 'stopped', latestRevision: 1, createdAt: '', updatedAt: '', outOfDate: false }, graph, [definition]);
    document.body.appendChild(editor);
    editor.selected = 'custom'; editor.render();

    expect(editor.querySelector('[data-plugin-node-editor]')?.textContent).toBe('Custom editor mounted');
    expect(pluginContext.node.config.prefix).toBe('> ');
    pluginContext.updateConfig({ prefix: '# ' });
    expect(editor.draft.value.nodes[0].config.prefix).toBe('# ');
    expect(dispose).toHaveBeenCalled();
    editor.remove();
  });

  it('keeps the selected node highlighted after the inspector renders', () => {
    const editor = document.createElement('visual-script-editor') as any;
    const graph = emptyGraph();
    graph.nodes = [{ id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 24, y: 70 }, config: {} }];
    const manual: NodeDefinition = {
      type: 'core.manual', typeVersion: 1, name: 'Manual', description: 'Starts a controlled editor run', category: 'Triggers', icon: '▶',
      inputs: [], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true,
    };
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'stopped', latestRevision: 1,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, graph, [manual]);
    document.body.appendChild(editor);

    (editor.querySelector('[data-node-id="manual"]') as HTMLElement).click();

    expect(editor.querySelector('.vse-inspector')?.textContent).toContain('Manual');
    expect(editor.querySelector('[data-node-id="manual"]')?.classList.contains('selected')).toBe(true);
    editor.remove();
  });

  it('uses readable select options and suggests known keys for Get Variable', () => {
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'set-temperature', type: 'core.set-context', typeVersion: 1, position: { x: 20, y: 20 }, config: { scope: 'script', key: 'Temperature' } },
      { id: 'increment-count', type: 'core.increment-context', typeVersion: 1, position: { x: 20, y: 120 }, config: { scope: 'script', key: 'Count' } },
      { id: 'set-node-only', type: 'core.set-context', typeVersion: 1, position: { x: 20, y: 220 }, config: { scope: 'node', key: 'Private' } },
      { id: 'get', type: 'core.get-context', typeVersion: 1, position: { x: 300, y: 20 }, config: { scope: 'script', key: '' } },
      { id: 'set-time', type: 'core.set-time-context', typeVersion: 1, position: { x: 20, y: 320 }, config: { key: 'LastSeen', source: 'now' } },
      { id: 'get-time', type: 'core.get-time-context', typeVersion: 1, position: { x: 300, y: 120 }, config: { key: '' } },
    ];
    const contextParameters = [
      { name: 'scope', label: 'Scope', type: 'select', required: true, options: ['node', 'script'], default: 'script' },
      { name: 'key', label: 'Key', type: 'string', required: true },
    ];
    const catalog = [
      { type: 'core.set-context', typeVersion: 1, name: 'Set Variable', description: '', category: 'Variables', icon: '⇤', inputs: [], outputs: [], parameters: contextParameters, available: true },
      { type: 'core.increment-context', typeVersion: 1, name: 'Increment Variable', description: '', category: 'Variables', icon: '+1', inputs: [], outputs: [], parameters: contextParameters, available: true },
      { type: 'core.get-context', typeVersion: 1, name: 'Get Variable', description: '', category: 'Variables', icon: '⇥', inputs: [], outputs: [], parameters: contextParameters, available: true },
      { type: 'core.set-time-context', typeVersion: 1, name: 'Set Time Variable', description: '', category: 'Variables', icon: '⇤◷', inputs: [], outputs: [], parameters: [{ name: 'key', label: 'Key', type: 'string', required: true }], available: true },
      { type: 'core.get-time-context', typeVersion: 1, name: 'Get Time Variable', description: '', category: 'Variables', icon: '⇥◷', inputs: [], outputs: [], parameters: [{ name: 'key', label: 'Key', type: 'string', required: true }], available: true },
    ] as NodeDefinition[];
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'stopped', latestRevision: 1,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, graph, catalog);
    document.body.appendChild(editor);

    (editor.querySelector('[data-node-id="get"]') as HTMLElement).click();

    const keyInput = editor.querySelector<HTMLInputElement>('[data-param="key"]')!;
    const options = [...editor.querySelectorAll<HTMLOptionElement>('#vse-context-key-options option')].map(option => option.value);
    expect(keyInput.getAttribute('list')).toBe('vse-context-key-options');
    expect(options).toEqual(['Count', 'Temperature']);
    expect(options).not.toContain('Private');
    (editor.querySelector('[data-node-id="get-time"]') as HTMLElement).click();
    const timeOptions = [...editor.querySelectorAll<HTMLOptionElement>('#vse-context-key-options option')].map(option => option.value);
    expect(timeOptions).toEqual(['LastSeen']);
    expect(editor.querySelector('style').textContent).toContain('select option{color:var(--content-text);background:var(--widget-bg)}');
    editor.remove();
  });

  it('queues the selected Manual node from its Trigger button while running', async () => {
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'manual-secondary', type: 'core.manual', typeVersion: 1, position: { x: 24, y: 70 }, config: {} },
      { id: 'debug', type: 'core.debug', typeVersion: 1, position: { x: 240, y: 70 }, config: { label: 'Pump status' } },
    ];
    const manual: NodeDefinition = {
      type: 'core.manual', typeVersion: 1, name: 'Manual', description: 'Starts a controlled editor run', category: 'Triggers', icon: '▶',
      inputs: [], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true,
    };
    const acceptedRun = {
      runId: 'run-1', scriptId: 'script-1', activeRevision: 1, triggerNodeId: 'manual-secondary', instanceKey: 'manual',
      startedAt: new Date().toISOString(), status: 'queued', durationMs: 0, nodesExecuted: 0,
    };
    const completedRun = {
      ...acceptedRun, status: 'ok', durationMs: 2, nodesExecuted: 2,
      trace: [
        { sequence: 1, timestamp: new Date().toISOString(), nodeId: 'manual-secondary', nodeType: 'core.manual', port: 'out', status: 'ok', value: null, fields: {} },
        { sequence: 2, timestamp: '2026-08-13T07:08:09.045Z', nodeId: 'debug', nodeType: 'core.debug', port: 'out', status: 'ok', value: 23, fields: { source: 'context' }, formattedTimes: { '$value': '2026-08-15T12:30:00.123Z' } },
      ],
    };
    const fetchMock = vi.fn().mockImplementation(async (url: string) => ({
      ok: true, status: url.endsWith('/run') ? 202 : 200,
      json: async () => url.endsWith('/run') ? acceptedRun : completedRun,
    }));
    vi.stubGlobal('fetch', fetchMock);
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'running', runtimeState: 'running', latestRevision: 1,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, graph, [manual, {
      type: 'core.debug', typeVersion: 1, name: 'Debug', description: '', category: 'Actions', icon: '◎',
      inputs: [{ name: 'in', label: 'Input', dataType: 'message' }], outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true,
    }]);
    document.body.appendChild(editor);

    const trigger = editor.querySelector<HTMLButtonElement>('[data-trigger-node="manual-secondary"]')!;
    expect(trigger).toBeTruthy();
    expect(trigger.disabled).toBe(false);
    trigger.click();
    await vi.waitFor(() => expect(editor.notice).toBe('Manual trigger queued'));

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe('/xact/api/v1/visual-scripts/script-1/run');
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ triggerNodeId: 'manual-secondary', value: null, fields: {} });
    expect(editor.textContent).toContain('Run in progress…');
    await vi.waitFor(() => expect(editor.runs.find((item: any) => item.runId === 'run-1')?.status).toBe('ok'));
    expect(fetchMock.mock.calls[1][0]).toBe('/xact/api/v1/visual-scripts/script-1/runs/run-1');
    expect(editor.querySelector('.vse-trace-header')?.textContent).toContain('Debug · Pump status· ok');
    expect(editor.querySelector('.vse-trace-header time')?.textContent).toMatch(/^\d{2}:\d{2}:\d{2}\.045$/);
    expect(editor.querySelector('.vse-trace-json')?.textContent).toBe('{"value":23,"fields":{"source":"context"},"formattedTimes":{"$value":"2026-08-15T12:30:00.123Z"}}');
    expect(editor.querySelectorAll('.vse-trace-event')).toHaveLength(1);
    (editor.querySelector('[data-format-trace="run-1:2"]') as HTMLButtonElement).click();
    expect(editor.querySelector('[role="dialog"]')).toBeTruthy();
    expect(editor.querySelector('.vse-json-dialog pre')?.textContent).toContain('"value": 23');
    expect(editor.querySelector('.vse-json-dialog pre')?.textContent).toContain('"source": "context"');
    expect(editor.querySelector('.vse-json-dialog pre')?.textContent).toContain('"$value": "2026-08-15T12:30:00.123Z"');
    expect(editor.querySelector('#vse-recent-runs')).toBeNull();
    expect(editor.querySelector('#vse-clear-trace')).toBeTruthy();
    editor.remove();
    vi.unstubAllGlobals();
  });

  it('accumulates Debug entries for the current started session and clears without confirmation', async () => {
    const run = {
      runId: 'run-1', scriptId: 'script-1', activeRevision: 1, triggerNodeId: 'manual', instanceKey: 'manual',
      startedAt: new Date().toISOString(), status: 'ok', durationMs: 2, nodesExecuted: 1,
      trace: [{ sequence: 1, timestamp: '2026-08-13T07:08:10.123Z', nodeId: 'debug', nodeType: 'core.debug', port: 'out', status: 'ok', value: 'new', fields: {} }],
    };
    const olderRun = {
      ...run, runId: 'run-old', status: 'ok',
      trace: [{ sequence: 1, timestamp: '2026-08-13T07:08:09.012Z', nodeId: 'debug', nodeType: 'core.debug', port: 'out', status: 'ok', value: 'old', fields: {} }],
    };
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 204 });
    vi.stubGlobal('fetch', fetchMock);
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'running', runtimeState: 'running', latestRevision: 1,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, emptyGraph(), [], [run, olderRun]);
    document.body.appendChild(editor);
    expect(editor.runs).toEqual([run, olderRun]);
    expect(editor.textContent).not.toContain('Recent runs');
    expect([...editor.querySelectorAll('.vse-trace-json')].map((item: Element) => item.textContent)).toEqual([
      '{"value":"old","fields":{}}',
      '{"value":"new","fields":{}}',
    ]);
    (editor.querySelector('#vse-clear-trace') as HTMLButtonElement).click();
    await vi.waitFor(() => expect(editor.notice).toBe('Trace cleared'));

    expect(document.querySelector('app-dialog #dialog-confirm')).toBeNull();
    expect(fetchMock).toHaveBeenCalledWith('/xact/api/v1/visual-scripts/script-1/runs', expect.objectContaining({ method: 'DELETE' }));
    expect(editor.runs).toEqual([]);
    expect(editor.textContent).toContain('No debug output recorded.');
    editor.remove();
    vi.unstubAllGlobals();
  });

  it('does not restore cleared traces from an in-flight run poll', async () => {
    const queuedRun = {
      runId: 'run-polling', scriptId: 'script-1', activeRevision: 1, triggerNodeId: 'manual', instanceKey: 'manual',
      startedAt: new Date().toISOString(), status: 'running', durationMs: 0, nodesExecuted: 1,
      trace: [{ sequence: 1, timestamp: new Date().toISOString(), nodeId: 'debug', nodeType: 'core.debug', port: 'out', status: 'ok', value: 'before-clear', fields: {} }],
    };
    const completedRun = { ...queuedRun, status: 'ok', trace: [{ ...queuedRun.trace[0], value: 'late-response' }] };
    let resolvePoll: ((response: any) => void) | undefined;
    const fetchMock = vi.fn().mockImplementation(async (url: string, options?: RequestInit) => {
      if (options?.method === 'DELETE') return { ok: true, status: 204 };
      return new Promise(resolve => { resolvePoll = resolve; });
    });
    vi.stubGlobal('fetch', fetchMock);
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'running', runtimeState: 'running', latestRevision: 1,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, emptyGraph(), [], [queuedRun]);
    document.body.appendChild(editor);

    const poll = editor.pollRun(queuedRun.runId, editor.traceGeneration);
    await vi.waitFor(() => expect(resolvePoll).toBeTypeOf('function'));
    (editor.querySelector('#vse-clear-trace') as HTMLButtonElement).click();
    await vi.waitFor(() => expect(editor.notice).toBe('Trace cleared'));
    resolvePoll!({ ok: true, status: 200, json: async () => completedRun });
    await poll;

    expect(editor.runs).toEqual([]);
    expect(editor.querySelectorAll('.vse-trace-event')).toHaveLength(0);
    expect(editor.textContent).toContain('No debug output recorded.');
    editor.remove();
    vi.unstubAllGlobals();
  });

  it('shows Manual Trigger disabled until the script is running', () => {
    const graph = emptyGraph();
    graph.nodes = [{ id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 20, y: 20 }, config: {} }];
    const manual: NodeDefinition = {
      type: 'core.manual', typeVersion: 1, name: 'Manual', description: '', category: 'Triggers', icon: '▶', inputs: [],
      outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true,
    };
    const canvas = document.createElement('visual-script-canvas') as any;
    document.body.appendChild(canvas);
    canvas.setData(graph, [manual], true, '', '', { showManualTrigger: true, manualTriggerEnabled: false });
    const trigger = canvas.querySelector<HTMLButtonElement>('[data-trigger-node="manual"]')!;
    expect(trigger.disabled).toBe(true);
    expect(trigger.title).toContain('Start the script');
    canvas.remove();
  });

  it('queues a Manual trigger from the read-only dashboard graph', async () => {
    const graph = emptyGraph();
    graph.nodes = [{ id: 'manual-dashboard', type: 'core.manual', typeVersion: 1, position: { x: 20, y: 20 }, config: {} }];
    const manual: NodeDefinition = {
      type: 'core.manual', typeVersion: 1, name: 'Manual', description: '', category: 'Triggers', icon: '▶', inputs: [],
      outputs: [{ name: 'out', label: 'Output', dataType: 'message' }], parameters: [], available: true,
    };
    const acceptedRun = {
      runId: 'run-dashboard', scriptId: 'script-1', activeRevision: 1, triggerNodeId: 'manual-dashboard', instanceKey: 'manual',
      startedAt: new Date().toISOString(), status: 'queued', durationMs: 0, nodesExecuted: 0,
    };
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 202, json: async () => acceptedRun });
    vi.stubGlobal('fetch', fetchMock);
    const widget = document.createElement('visual-script-widget') as any;
    widget.loading = false;
    widget.canEdit = true;
    widget.script = { id: 'script-1', name: 'Test', description: '', desiredState: 'running', runtimeState: 'running', latestRevision: 1, createdAt: '', updatedAt: '', outOfDate: false };
    widget.status = { scriptId: 'script-1', desiredState: 'running', runtimeState: 'running', activeRevision: 1, latestRevision: 1, queueDepth: 0, sequence: 1 };
    widget.graph = graph;
    widget.catalog = [manual];
    widget.render();

    widget.querySelector<HTMLButtonElement>('[data-trigger-node="manual-dashboard"]')!.click();
    await vi.waitFor(() => expect(widget.notice).toBe('Manual trigger queued'));
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).triggerNodeId).toBe('manual-dashboard');
    vi.unstubAllGlobals();
  });

  it('finishes a valid response when an older server sends null diagnostics', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ valid: true, diagnostics: null }),
    }));
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'stopped', latestRevision: 0,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, emptyGraph(), []);
    document.body.appendChild(editor);

    await expect(editor.validate()).resolves.toBe(true);
    expect(editor.busy).toBe('');
    expect(editor.notice).toBe('Graph is valid');
    expect(editor.diagnostics).toEqual([]);
    expect(editor.textContent).toContain('Problems (0)');
    expect(editor.querySelector('#vse-validate')).toBeNull();
    expect(editor.querySelector('#vse-run-control')).toBeTruthy();

    editor.remove();
    vi.unstubAllGlobals();
  });

  it('saves and validates before starting the current script', async () => {
    const graph = emptyGraph();
    graph.nodes = [{ id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 24, y: 70 }, config: {} }];
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => ({ revision: 1, graph, diagnostics: [] }) })
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ valid: true, diagnostics: [] }) })
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ scriptId: 'script-1', desiredState: 'running', runtimeState: 'running', activeRevision: 1, latestRevision: 1, queueDepth: 0, sequence: 1 }) });
    vi.stubGlobal('fetch', fetchMock);
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'stopped', latestRevision: 0,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, emptyGraph(), [], [{
      runId: 'old-run', scriptId: 'script-1', activeRevision: 1, triggerNodeId: 'manual', instanceKey: 'manual',
      startedAt: new Date().toISOString(), status: 'ok', durationMs: 1, nodesExecuted: 1,
    }]);
    document.body.appendChild(editor);
    editor.draft.update(graph);

    await editor.toggleRuntime();

    expect(fetchMock.mock.calls.map(call => call[0])).toEqual([
      '/xact/api/v1/visual-scripts/script-1/revisions',
      '/xact/api/v1/visual-scripts/script-1/validate',
      '/xact/api/v1/visual-scripts/script-1/start',
    ]);
    expect(editor.notice).toBe('Script running');
    expect(editor.textContent).toContain('Running');
    expect(editor.textContent).not.toContain('Deploy');
    expect(editor.textContent).not.toContain('Run/Test');
    expect(editor.querySelector('#vse-backup')).toBeTruthy();
    expect(editor.querySelector('#vse-simulate')).toBeTruthy();
    expect(editor.querySelector('#vse-activate')).toBeTruthy();
    expect(editor.runs).toEqual([]);
    expect(editor.textContent).toContain('No debug output recorded.');
    expect(editor.querySelector('#vse-save')?.hasAttribute('disabled')).toBe(true);
    editor.remove();
    vi.unstubAllGlobals();
  });

  it('stops a running script before a quick save without validating', async () => {
    const graph = emptyGraph();
    graph.nodes = [{ id: 'manual', type: 'core.manual', typeVersion: 1, position: { x: 24, y: 70 }, config: {} }];
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => ({ scriptId: 'script-1', desiredState: 'stopped', runtimeState: 'idle', activeRevision: 1, latestRevision: 1, queueDepth: 0, sequence: 2 }) })
      .mockResolvedValueOnce({ ok: true, status: 201, json: async () => ({ revision: 2, graph, diagnostics: [] }) });
    vi.stubGlobal('fetch', fetchMock);
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({
      id: 'script-1', name: 'Test script', description: '', desiredState: 'running', runtimeState: 'running', latestRevision: 1,
      createdAt: '', updatedAt: '', outOfDate: false,
    }, emptyGraph(), []);
    document.body.appendChild(editor);
    editor.draft.update(graph);

    await editor.quickSave();

    expect(fetchMock.mock.calls.map(call => call[0])).toEqual([
      '/xact/api/v1/visual-scripts/script-1/stop',
      '/xact/api/v1/visual-scripts/script-1/revisions',
    ]);
    expect(fetchMock.mock.calls.some(call => String(call[0]).endsWith('/validate'))).toBe(false);
    expect(editor.script.desiredState).toBe('stopped');
    expect(editor.draft.dirty).toBe(false);
    expect(editor.notice).toBe('Saved');
    expect(editor.querySelector('#vse-save')?.hasAttribute('disabled')).toBe(true);
    editor.remove();
    vi.unstubAllGlobals();
  });

  it('hides the dashboard widget picker while the script editor has focus', () => {
    const dashboard = document.createElement('dashboard-container') as any;
    dashboard.innerHTML = '<div id="pc-toolbar"><div class="widget-category-dropdown"></div></div>';
    const toolbar = dashboard.querySelector('#pc-toolbar') as HTMLElement;

    dashboard.handleVisualScriptFocus(new CustomEvent('visual-script-focus-changed', { detail: { active: true } }));
    expect(toolbar.style.display).toBe('none');
    expect(toolbar.querySelector('.widget-category-dropdown').classList.contains('hidden')).toBe(true);

    dashboard.handleVisualScriptFocus(new CustomEvent('visual-script-focus-changed', { detail: { active: false } }));
    expect(toolbar.style.display).toBe('');
  });

  it('renders a tag picker for every tag-path node parameter', () => {
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'changed', type: 'core.tag-changed', typeVersion: 1, position: { x: 20, y: 20 }, config: { pathPattern: 'Plant.Pump.Status' } },
      { id: 'set', type: 'core.set-tag', typeVersion: 1, position: { x: 20, y: 120 }, config: { tagPath: 'Plant.Pump.Setpoint' } },
      { id: 'control', type: 'core.send-control', typeVersion: 1, position: { x: 20, y: 220 }, config: { tagPath: 'Plant.Pump.Enable' } },
    ];
    const node = (type: string, name: string, parameter: string, label: string): NodeDefinition => ({
      type, typeVersion: 1, name, description: '', category: 'Test', icon: '•', inputs: [], outputs: [], available: true,
      parameters: [{ name: parameter, label, type: 'string', required: true }],
    });
    const editor = document.createElement('visual-script-editor') as any;
    editor.initialize({ id: 'script-1', name: 'Test script', description: '', desiredState: 'stopped', latestRevision: 1, createdAt: '', updatedAt: '', outOfDate: false }, graph, [
      node('core.tag-changed', 'Tag Changed', 'pathPattern', 'Tag path'),
      node('core.set-tag', 'Set Tag', 'tagPath', 'Tag path'),
      node('core.send-control', 'Send Control', 'tagPath', 'Control path'),
    ]);
    document.body.appendChild(editor);

    for (const id of ['changed', 'set', 'control']) {
      (editor.querySelector(`[data-node-id="${id}"]`) as HTMLElement).click();
      expect(editor.querySelector<HTMLButtonElement>('[data-tag-picker]')?.getAttribute('aria-label')).toBe('Browse tags');
    }

    editor.remove();
  });

  it('shows compact, meaningful configuration summaries on nodes', () => {
    const graph = emptyGraph();
    graph.nodes = [
      { id: 'changed', type: 'core.tag-changed', typeVersion: 1, position: { x: 20, y: 20 }, config: { pathPattern: 'Plant.Pump.Status.Running', triggerOnStart: true } },
      { id: 'debug', type: 'core.debug', typeVersion: 1, position: { x: 220, y: 20 }, config: { label: 'Pump state received' } },
      { id: 'compare', type: 'core.compare', typeVersion: 1, position: { x: 420, y: 20 }, config: { field: 'pressure', operator: '>', compareTo: 100 } },
    ];
    const definitions: NodeDefinition[] = [
      { type: 'core.tag-changed', typeVersion: 1, name: 'Tag Changed', description: '', category: 'Triggers', icon: '⌁', inputs: [], outputs: [], parameters: [], available: true },
      { type: 'core.debug', typeVersion: 1, name: 'Debug', description: '', category: 'Actions', icon: '◎', inputs: [], outputs: [], parameters: [], available: true },
      { type: 'core.compare', typeVersion: 1, name: 'Compare', description: '', category: 'Conditions', icon: '≷', inputs: [], outputs: [], parameters: [], available: true },
    ];
    const canvas = document.createElement('visual-script-canvas') as any;
    document.body.appendChild(canvas);
    canvas.setData(graph, definitions);

    expect(canvas.querySelector('[data-node-id="changed"] .vsc-summary')?.textContent).toBe('Running · on start');
    expect(canvas.querySelector('[data-node-id="debug"] .vsc-summary')?.textContent).toBe('Pump state received');
    expect(canvas.querySelector('[data-node-id="compare"] .vsc-summary')?.textContent).toBe('pressure > 100');
    expect(canvas.querySelector('style')?.textContent).toContain('font-size:12px');
    expect(canvas.querySelector('style')?.textContent).toContain('white-space:nowrap');
    canvas.remove();
  });

  it('restores an unsaved visual-script draft after its dashboard is rebuilt', () => {
    const script = { id: 'script-1', name: 'Test script', description: '', desiredState: 'stopped', latestRevision: 1, createdAt: '', updatedAt: '', outOfDate: false };
    const original = emptyGraph();
    const changed = emptyGraph();
    changed.nodes.push({ id: 'debug', type: 'core.debug', typeVersion: 1, position: { x: 20, y: 20 }, config: { label: 'Retained draft' } });
    const first = document.createElement('visual-script-editor') as any;
    first.initialize(script, original, []);
    first.draft.update(changed);
    const state = first.getTransientState();

    const restored = document.createElement('visual-script-editor') as any;
    restored.initialize(script, original, []);
    restored.restoreTransientState(state);

    expect(restored.hasUnsavedChanges()).toBe(true);
    expect(restored.getTransientState().graph.nodes[0].config.label).toBe('Retained draft');
  });

  it('snapshots the current dashboard before reconstructing a dashboard tab', async () => {
    const content = document.createElement('app-content') as any;
    const first = document.createElement('dashboard-container') as any;
    const second = document.createElement('dashboard-container') as any;
    first.captureTransientState = vi.fn();
    second.loadDashboard = vi.fn();
    content.replaceChildren(first);
    content.dashboards = new Map([['one', first], ['two', second]]);
    content.activeDashboard = 'one';

    await content.switchToDashboard('two', true);

    expect(first.captureTransientState).toHaveBeenCalledOnce();
    expect(content.firstElementChild).toBe(second);
    expect(second.loadDashboard).toHaveBeenCalledWith('two');
  });

  it('destroys the dashboard grid when a tab is detached so it can rebuild cleanly', () => {
    const dashboard = document.createElement('dashboard-container') as any;
    const grid = { destroy: vi.fn() };
    dashboard.grid = grid;

    dashboard.disconnectedCallback();
    expect(grid.destroy).toHaveBeenCalledWith(false);
    expect(dashboard.grid).toBeNull();
  });

});
