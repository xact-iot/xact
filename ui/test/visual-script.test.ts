import { describe, expect, it, vi } from 'vitest';
import { DraftStore } from '../src/visual-scripts/draft-store';
import { emptyGraph } from '../src/visual-scripts/types';
import type { NodeDefinition } from '../src/visual-scripts/types';
import '../src/visual-scripts/canvas';
import '../src/visual-scripts/editor';
import '../src/dashboards/dashboard-container';

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
    expect(canvasStyles).toContain('background:var(--widget-header-surface,var(--widget-header-bg))');
    expect(canvasStyles).toContain('color:var(--widget-header-text,var(--content-text))');
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

  it('adds parameterless nodes when an older catalog sends null parameters', () => {
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

    expect(() => editor.querySelector('[data-add="core.manual"]').click()).not.toThrow();
    expect(editor.draft.value.nodes).toHaveLength(1);
    expect(editor.draft.value.nodes[0]).toMatchObject({ type: 'core.manual', position: { x: 24, y: 70 }, config: {} });
    expect(editor.querySelector('style').textContent).toContain('z-index:3100');
    expect(editor.querySelector('style').textContent).toContain('grid-template-columns:180px minmax(360px,1fr) 230px');
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
    }, emptyGraph(), []);
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
});
