import { describe, expect, it, vi } from 'vitest';
import { DraftStore } from '../src/visual-scripts/draft-store';
import { emptyGraph } from '../src/visual-scripts/types';
import type { NodeDefinition } from '../src/visual-scripts/types';
import '../src/visual-scripts/canvas';

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
    canvas.remove();
  });
});
