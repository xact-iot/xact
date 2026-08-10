import type { GraphDocument, GraphEdge, GraphNode, NodeDefinition } from './types';

const nodeWidth = 190;
const headerHeight = 42;
const portStep = 24;

export class VisualScriptCanvas extends HTMLElement {
  private graph: GraphDocument | null = null;
  private definitions = new Map<string, NodeDefinition>();
  private selected = '';
  private pendingOutput: { nodeId: string; port: string } | null = null;
  private readonly = false;
  private drag: { id: string; startX: number; startY: number; x: number; y: number } | null = null;

  setData(graph: GraphDocument, definitions: NodeDefinition[], readonly = false): void {
    this.graph = graph;
    this.definitions = new Map(definitions.map(d => [d.type, d]));
    this.readonly = readonly;
    this.render();
  }

  connectedCallback(): void { this.render(); }
  disconnectedCallback(): void { this.stopDrag(); }

  focusNode(id: string): void {
    this.selected = id;
    this.render();
    this.querySelector(`[data-node-id="${CSS.escape(id)}"]`)?.scrollIntoView({ block: 'center', inline: 'center', behavior: 'smooth' });
  }

  private render(): void {
    if (!this.graph) { this.innerHTML = ''; return; }
    const graph = this.graph;
    const edges = graph.edges.map(edge => this.edgeMarkup(edge)).join('');
    const nodes = graph.nodes.map(node => this.nodeMarkup(node)).join('');
    this.innerHTML = `
      <style>
        visual-script-canvas{display:block;position:relative;overflow:auto;min-height:360px;background-color:var(--content-bg);background-image:linear-gradient(color-mix(in srgb,var(--border-color) 32%,transparent) 1px,transparent 1px),linear-gradient(90deg,color-mix(in srgb,var(--border-color) 32%,transparent) 1px,transparent 1px);background-size:20px 20px;outline:none}
        visual-script-canvas .vsc-surface{position:relative;width:2200px;height:1400px;transform-origin:0 0}
        visual-script-canvas svg{position:absolute;inset:0;width:100%;height:100%;pointer-events:none}
        visual-script-canvas path{fill:none;stroke:var(--accent-color);stroke-width:2;opacity:.62}
        visual-script-canvas .vsc-node{position:absolute;width:${nodeWidth}px;min-height:82px;border:1px solid var(--border-color);border-radius:7px;background:var(--widget-bg);box-shadow:0 5px 16px rgba(0,0,0,.2);font:12px var(--widget-font-family);user-select:none}
        visual-script-canvas .vsc-node.selected{border-color:var(--accent-color);box-shadow:0 0 0 2px color-mix(in srgb,var(--accent-color) 28%,transparent)}
        visual-script-canvas .vsc-head{height:${headerHeight}px;padding:7px 10px;cursor:${this.readonly ? 'default' : 'grab'};display:flex;align-items:center;gap:8px;border-bottom:1px solid color-mix(in srgb,var(--border-color) 55%,transparent)}
        visual-script-canvas .vsc-icon{font-size:17px}.vsc-title{font-weight:650;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vsc-type{font-size:9px;opacity:.5}
        visual-script-canvas .vsc-ports{display:flex;justify-content:space-between;align-items:flex-start;padding:8px 5px;gap:8px}
        visual-script-canvas .vsc-port-col{display:flex;flex-direction:column;gap:5px;max-width:50%}
        visual-script-canvas .vsc-port{border:0;background:none;color:var(--content-text);font:10px var(--widget-font-family);padding:2px;display:flex;align-items:center;gap:4px;cursor:${this.readonly ? 'default' : 'crosshair'};white-space:nowrap}
        visual-script-canvas .vsc-port::before{content:'';width:8px;height:8px;border:2px solid var(--accent-color);border-radius:50%;background:var(--widget-bg)}
        visual-script-canvas .vsc-out{flex-direction:row-reverse;text-align:right}.vsc-out.pending{color:var(--accent-color);font-weight:700}
        visual-script-canvas .vsc-empty{position:absolute;left:50%;top:40%;transform:translate(-50%,-50%);opacity:.5;text-align:center;max-width:320px}
      </style>
      <div class="vsc-surface">
        <svg aria-hidden="true">${edges}</svg>
        ${nodes || '<div class="vsc-empty">Add a Manual trigger from the palette to begin.</div>'}
      </div>`;
    this.bind();
  }

  private nodeMarkup(node: GraphNode): string {
    const definition = this.definitions.get(node.type);
    const inputs = (definition?.inputs || []).map(port => `<button class="vsc-port vsc-in" data-node="${esc(node.id)}" data-port="${esc(port.name)}" data-kind="in" aria-label="Connect to ${esc(definition?.name || node.type)} ${esc(port.label)}">${esc(port.label)}</button>`).join('');
    const outputs = (definition?.outputs || []).map(port => `<button class="vsc-port vsc-out ${this.pendingOutput?.nodeId === node.id && this.pendingOutput.port === port.name ? 'pending' : ''}" data-node="${esc(node.id)}" data-port="${esc(port.name)}" data-kind="out" aria-label="Connect from ${esc(definition?.name || node.type)} ${esc(port.label)}">${esc(port.label)}</button>`).join('');
    return `<section class="vsc-node ${this.selected === node.id ? 'selected' : ''}" tabindex="0" data-node-id="${esc(node.id)}" style="left:${node.position.x}px;top:${node.position.y}px" aria-label="${esc(definition?.name || node.type)} node">
      <div class="vsc-head"><span class="vsc-icon">${esc(definition?.icon || '◇')}</span><span><div class="vsc-title">${esc(definition?.name || node.type)}</div><div class="vsc-type">${esc(node.id)}</div></span></div>
      <div class="vsc-ports"><div class="vsc-port-col">${inputs}</div><div class="vsc-port-col">${outputs}</div></div>
    </section>`;
  }

  private edgeMarkup(edge: GraphEdge): string {
    if (!this.graph) return '';
    const from = this.graph.nodes.find(n => n.id === edge.from.nodeId);
    const to = this.graph.nodes.find(n => n.id === edge.to.nodeId);
    if (!from || !to) return '';
    const fromDef = this.definitions.get(from.type); const toDef = this.definitions.get(to.type);
    const outIndex = Math.max(0, (fromDef?.outputs || []).findIndex(p => p.name === edge.from.port));
    const inIndex = Math.max(0, (toDef?.inputs || []).findIndex(p => p.name === edge.to.port));
    const x1 = from.position.x + nodeWidth, y1 = from.position.y + headerHeight + 18 + outIndex * portStep;
    const x2 = to.position.x, y2 = to.position.y + headerHeight + 18 + inIndex * portStep;
    const bend = Math.max(55, Math.abs(x2 - x1) * .45);
    return `<path d="M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}"><title>${esc(edge.from.nodeId)} ${esc(edge.from.port)} to ${esc(edge.to.nodeId)} ${esc(edge.to.port)}</title></path>`;
  }

  private bind(): void {
    this.querySelectorAll<HTMLElement>('.vsc-node').forEach(node => {
      node.addEventListener('click', event => { if ((event.target as HTMLElement).closest('.vsc-port')) return; this.select(node.dataset.nodeId || ''); });
      node.addEventListener('keydown', event => this.onNodeKey(event, node.dataset.nodeId || ''));
      node.querySelector('.vsc-head')?.addEventListener('pointerdown', event => this.startDrag(event as PointerEvent, node.dataset.nodeId || ''));
    });
    this.querySelectorAll<HTMLButtonElement>('.vsc-port').forEach(port => port.addEventListener('click', event => { event.stopPropagation(); this.portClick(port); }));
    this.querySelector('.vsc-surface')?.addEventListener('click', event => { if ((event.target as HTMLElement).classList.contains('vsc-surface')) this.select(''); });
  }

  private select(id: string): void { this.selected = id; this.dispatchEvent(new CustomEvent('visual-node-selected', { detail: { nodeId: id }, bubbles: true })); this.render(); }

  private onNodeKey(event: KeyboardEvent, id: string): void {
    if (this.readonly) return;
    if (event.key === 'Delete' || event.key === 'Backspace') { event.preventDefault(); this.removeNode(id); return; }
    const delta: Record<string, [number, number]> = { ArrowLeft: [-10, 0], ArrowRight: [10, 0], ArrowUp: [0, -10], ArrowDown: [0, 10] };
    if (delta[event.key]) { event.preventDefault(); const node = this.graph?.nodes.find(n => n.id === id); if (node) { node.position.x = Math.max(0, node.position.x + delta[event.key][0]); node.position.y = Math.max(0, node.position.y + delta[event.key][1]); this.changed(); } }
  }

  private startDrag(event: PointerEvent, id: string): void {
    if (this.readonly || event.button !== 0 || !this.graph) return;
    const node = this.graph.nodes.find(n => n.id === id); if (!node) return;
    event.preventDefault(); this.drag = { id, startX: event.clientX, startY: event.clientY, x: node.position.x, y: node.position.y };
    document.addEventListener('pointermove', this.moveDrag); document.addEventListener('pointerup', this.endDrag);
  }
  private moveDrag = (event: PointerEvent): void => { if (!this.drag || !this.graph) return; const node = this.graph.nodes.find(n => n.id === this.drag!.id); if (!node) return; node.position.x = Math.max(0, Math.round((this.drag.x + event.clientX - this.drag.startX) / 10) * 10); node.position.y = Math.max(0, Math.round((this.drag.y + event.clientY - this.drag.startY) / 10) * 10); const el = this.querySelector<HTMLElement>(`[data-node-id="${CSS.escape(node.id)}"]`); if (el) { el.style.left = `${node.position.x}px`; el.style.top = `${node.position.y}px`; } };
  private endDrag = (): void => { if (this.drag) this.changed(); this.stopDrag(); };
  private stopDrag(): void { this.drag = null; document.removeEventListener('pointermove', this.moveDrag); document.removeEventListener('pointerup', this.endDrag); }

  private portClick(port: HTMLButtonElement): void {
    if (this.readonly || !this.graph) return;
    const endpoint = { nodeId: port.dataset.node || '', port: port.dataset.port || '' };
    if (port.dataset.kind === 'out') { this.pendingOutput = endpoint; this.render(); return; }
    if (!this.pendingOutput || this.pendingOutput.nodeId === endpoint.nodeId) return;
    if (this.graph.edges.some(e => e.from.nodeId === this.pendingOutput!.nodeId && e.from.port === this.pendingOutput!.port && e.to.nodeId === endpoint.nodeId && e.to.port === endpoint.port)) return;
    this.graph.edges.push({ id: uid('edge'), from: this.pendingOutput, to: endpoint }); this.pendingOutput = null; this.changed();
  }
  private removeNode(id: string): void { if (!this.graph) return; this.graph.nodes = this.graph.nodes.filter(n => n.id !== id); this.graph.edges = this.graph.edges.filter(e => e.from.nodeId !== id && e.to.nodeId !== id); this.selected = ''; this.changed(); }
  private changed(): void { this.dispatchEvent(new CustomEvent('visual-graph-change', { detail: { graph: this.graph }, bubbles: true })); this.render(); }
}

export function uid(prefix: string): string { return `${prefix}_${crypto.randomUUID?.() || Math.random().toString(36).slice(2)}`.replace(/-/g, '').slice(0, 36); }
function esc(value: any): string { return String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!)); }
if (!customElements.get('visual-script-canvas')) customElements.define('visual-script-canvas', VisualScriptCanvas);
