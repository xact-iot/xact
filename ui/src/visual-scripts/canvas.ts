import type { GraphDocument, GraphEdge, GraphNode, NodeDefinition } from './types';
import { visualScriptCategoryStyle } from './category-colors';

const nodeWidth = 150;
const headerHeight = 32;
const summaryHeight = 26;
const portStep = 22;
type Endpoint = { nodeId: string; port: string };
type Point = { x: number; y: number };
type CanvasOptions = { showManualTrigger?: boolean; manualTriggerEnabled?: boolean };

export class VisualScriptCanvas extends HTMLElement {
  private graph: GraphDocument | null = null;
  private definitions = new Map<string, NodeDefinition>();
  private selected = '';
  private selectedEdge = '';
  private pendingOutput: Endpoint | null = null;
  private readonly = false;
  private options: CanvasOptions = {};
  private drag: { id: string; startX: number; startY: number; x: number; y: number; moved: boolean } | null = null;
  private connectionDrag: { from: Endpoint; x: number; y: number } | null = null;
  private rewireDrag: { edgeId: string; end: 'from'|'to'; fixed: Point } | null = null;

  setData(graph: GraphDocument, definitions: NodeDefinition[], readonly = false, selected = '', selectedEdge = '', options: CanvasOptions = {}): void {
    this.graph = graph;
    this.definitions = new Map(definitions.map(d => [d.type, d]));
    this.readonly = readonly;
    this.selected = selected;
    this.selectedEdge = selectedEdge;
    this.options = options;
    this.render();
  }

  connectedCallback(): void { this.render(); }
  disconnectedCallback(): void { this.stopDrag(); this.stopConnectionDrag(); this.stopRewireDrag(); }

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
        visual-script-canvas svg{position:absolute;inset:0;width:100%;height:100%;pointer-events:none}.vsc-edges{z-index:0}.vsc-overlay{z-index:2}
        visual-script-canvas path{fill:none;stroke:var(--accent-color);stroke-width:2;opacity:.62}.vsc-edge-hit{stroke:transparent;stroke-width:14;opacity:0;pointer-events:${this.readonly ? 'none' : 'stroke'};cursor:${this.readonly ? 'default' : 'pointer'}}.vsc-edge-line{pointer-events:none}.vsc-edge.selected .vsc-edge-line{stroke-width:4;opacity:1;filter:drop-shadow(0 0 3px var(--accent-color))}
        visual-script-canvas path.vsc-draft-edge{stroke-dasharray:6 4;opacity:.9}
        visual-script-canvas .vsc-edge-handle{fill:#ede6d9;stroke:var(--accent-color);stroke-width:3;pointer-events:all;cursor:grab;touch-action:none}.vsc-edge-handle:hover{r:7}.vsc-edge-handle:active{cursor:grabbing}
        visual-script-canvas .vsc-node{z-index:1;position:absolute;width:${nodeWidth}px;min-height:58px;overflow:hidden;border:1px solid #b8aa93;border-radius:6px;background:#ede6d9;color:#3d3428;box-shadow:0 4px 12px rgba(61,52,40,.22);font:12px var(--widget-font-family);user-select:none}
        visual-script-canvas .vsc-node.selected{border-color:var(--accent-color);outline:2px solid var(--accent-color);outline-offset:1px;box-shadow:0 0 0 3px color-mix(in srgb,var(--accent-color) 32%,transparent),0 6px 16px rgba(61,52,40,.3)}
        visual-script-canvas .vsc-head{box-sizing:border-box;height:${headerHeight}px;padding:5px 8px;cursor:${this.readonly ? 'default' : 'grab'};display:flex;align-items:center;gap:6px;background:var(--vs-category-bg);color:var(--vs-category-text);border-bottom:1px solid var(--vs-category-border)}
        visual-script-canvas .vsc-head>span:last-child{min-width:0;flex:1}.vsc-icon{font-size:14px}.vsc-title{font-size:13px;line-height:1.2;font-weight:650;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vsc-summary{box-sizing:border-box;height:${summaryHeight}px;padding:5px 7px;border-bottom:1px solid color-mix(in srgb,#3d3428 18%,transparent);font-size:12px;line-height:15px;color:#665a49;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
        visual-script-canvas .vsc-ports{display:flex;justify-content:space-between;align-items:flex-start;padding:5px 3px;gap:4px}
        visual-script-canvas .vsc-port-col{display:flex;flex-direction:column;gap:2px;max-width:50%}
        visual-script-canvas .vsc-port{border:0;background:none;color:inherit;font:12px var(--widget-font-family);padding:2px;display:flex;align-items:center;gap:3px;cursor:${this.readonly ? 'default' : 'crosshair'};white-space:nowrap}
        visual-script-canvas .vsc-port::before{content:'';width:6px;height:6px;border:2px solid var(--accent-color);border-radius:50%;background:#ede6d9}
        visual-script-canvas .vsc-out{flex-direction:row-reverse;text-align:right;cursor:${this.readonly ? 'default' : 'grab'};touch-action:none}.vsc-out.pending{color:var(--accent-color);font-weight:700;cursor:grabbing}
        visual-script-canvas .vsc-manual-action{display:flex;justify-content:center;padding:0 5px 5px}.vsc-manual-trigger{min-width:70px;padding:3px 9px;border:1px solid #998b73;border-radius:4px;background:#fffaf0;color:#3d3428;font:600 12px var(--widget-font-family);cursor:pointer}.vsc-manual-trigger:hover:not(:disabled){border-color:var(--accent-color);box-shadow:0 0 0 1px var(--accent-color)}.vsc-manual-trigger:disabled{opacity:.45;cursor:not-allowed}
        visual-script-canvas.connecting .vsc-in,visual-script-canvas.rewiring-to .vsc-in,visual-script-canvas.rewiring-from .vsc-out{color:var(--accent-color)}visual-script-canvas .vsc-port.drop-target{font-weight:700;background:color-mix(in srgb,var(--accent-color) 18%,transparent);border-radius:4px}
        visual-script-canvas .vsc-empty{position:absolute;left:50%;top:40%;transform:translate(-50%,-50%);opacity:.5;text-align:center;max-width:320px}
      </style>
      <div class="vsc-surface">
        <svg class="vsc-edges" aria-label="Connections">${edges}</svg>
        ${nodes || '<div class="vsc-empty">Drag a Manual trigger from the palette to begin.</div>'}
        <svg class="vsc-overlay" aria-label="Selected connection controls">${this.edgeHandlesMarkup()}</svg>
      </div>`;
    this.scrollGraphIntoView();
    this.bind();
  }

  private scrollGraphIntoView(): void {
    if (!this.graph?.nodes.length) return;
    const minX = Math.min(...this.graph.nodes.map(node => node.position.x));
    const minY = Math.min(...this.graph.nodes.map(node => node.position.y));
    this.scrollLeft = Math.max(0, minX - 24);
    this.scrollTop = Math.max(0, minY - 24);
  }

  private nodeMarkup(node: GraphNode): string {
    const definition = this.definitions.get(node.type);
    const summary = this.nodeSummary(node);
    const inputs = (definition?.inputs || []).map(port => `<button class="vsc-port vsc-in" data-node="${esc(node.id)}" data-port="${esc(port.name)}" data-kind="in" aria-label="Connect to ${esc(definition?.name || node.type)} ${esc(port.label)}">${esc(port.label)}</button>`).join('');
    const outputs = (definition?.outputs || []).map(port => `<button class="vsc-port vsc-out ${this.pendingOutput?.nodeId === node.id && this.pendingOutput.port === port.name ? 'pending' : ''}" data-node="${esc(node.id)}" data-port="${esc(port.name)}" data-kind="out" aria-label="Connect from ${esc(definition?.name || node.type)} ${esc(port.label)}">${esc(port.label)}</button>`).join('');
    const manualTrigger = node.type === 'core.manual' && this.options.showManualTrigger ? `<div class="vsc-manual-action"><button class="vsc-manual-trigger" data-trigger-node="${esc(node.id)}" ${this.options.manualTriggerEnabled ? '' : 'disabled'} title="${this.options.manualTriggerEnabled ? 'Run the script from this Manual trigger' : 'Start the script to enable this trigger'}">Trigger</button></div>` : '';
    return `<section class="vsc-node ${this.selected === node.id ? 'selected' : ''}" tabindex="0" data-node-id="${esc(node.id)}" style="left:${node.position.x}px;top:${node.position.y}px;${visualScriptCategoryStyle(definition?.category)}" aria-label="${esc(definition?.name || node.type)} node">
      <div class="vsc-head"><span class="vsc-icon">${esc(definition?.icon || '◇')}</span><span><div class="vsc-title">${esc(definition?.name || node.type)}</div></span></div>
      ${summary ? `<div class="vsc-summary" title="${esc(summary)}">${esc(summary)}</div>` : ''}
      <div class="vsc-ports"><div class="vsc-port-col">${inputs}</div><div class="vsc-port-col">${outputs}</div></div>
      ${manualTrigger}
    </section>`;
  }

  private edgeMarkup(edge: GraphEdge): string {
    const points = this.edgePoints(edge); if (!points) return '';
    const d = curvePath(points.from, points.to);
    const label = `${this.nodeLabel(edge.from.nodeId)} ${edge.from.port} to ${this.nodeLabel(edge.to.nodeId)} ${edge.to.port}`;
    return `<g class="vsc-edge ${this.selectedEdge === edge.id ? 'selected' : ''}" data-edge-id="${esc(edge.id)}"><path class="vsc-edge-hit" d="${d}" tabindex="${this.readonly ? '-1' : '0'}" aria-label="${esc(label)}"><title>${esc(label)}</title></path><path class="vsc-edge-line" d="${d}"></path></g>`;
  }

  private edgeHandlesMarkup(): string {
    if (this.readonly || !this.graph || !this.selectedEdge) return '';
    const edge = this.graph.edges.find(item => item.id === this.selectedEdge); const points = edge && this.edgePoints(edge);
    if (!edge || !points) return '';
    return `<circle class="vsc-edge-handle" data-edge-id="${esc(edge.id)}" data-end="from" cx="${points.from.x}" cy="${points.from.y}" r="6" aria-label="Move connection source"></circle><circle class="vsc-edge-handle" data-edge-id="${esc(edge.id)}" data-end="to" cx="${points.to.x}" cy="${points.to.y}" r="6" aria-label="Move connection target"></circle>`;
  }

  private edgePoints(edge: GraphEdge): { from: Point; to: Point } | null {
    const from = this.endpointPoint(edge.from, 'out'); const to = this.endpointPoint(edge.to, 'in');
    return from && to ? { from, to } : null;
  }

  private endpointPoint(endpoint: Endpoint, kind: 'in'|'out'): Point | null {
    const node = this.graph?.nodes.find(item => item.id === endpoint.nodeId); if (!node) return null;
    const definition = this.definitions.get(node.type); const ports = kind === 'out' ? definition?.outputs : definition?.inputs;
    const index = Math.max(0, (ports || []).findIndex(port => port.name === endpoint.port));
    return { x: node.position.x + (kind === 'out' ? nodeWidth : 0), y: node.position.y + headerHeight + (this.nodeSummary(node) ? summaryHeight : 0) + 18 + index * portStep };
  }

  private nodeSummary(node: GraphNode): string {
    const value = (key: string) => node.config[key];
    const field = String(value('field') || '$value');
    const source = value('source') === 'configured' ? compactValue(value('value')) : value('source') === 'field' ? String(value('field') || '$value') : '$value';
    switch (node.type) {
      case 'core.tag-changed': return `${tagName(value('pathPattern')) || 'tag path'}${value('triggerOnStart') ? ' · on start' : ''}`;
      case 'core.rising-edge': case 'core.falling-edge': return tagName(value('pathPattern')) || 'tag path';
      case 'core.timer': return `Every ${value('interval') || 'interval'}`;
      case 'core.startup': return value('delay') && value('delay') !== '0s' ? `After ${value('delay')}` : 'When script starts';
      case 'core.compare': return `${field} ${value('operator') || '=='} ${compactValue(value('compareTo'))}`;
      case 'core.in-range': return `${field} ∈ ${compactValue(value('minimum'))}…${compactValue(value('maximum'))}`;
      case 'core.not': return `NOT ${field}`;
      case 'core.and': case 'core.or': return compactValue(value('fields')) || 'Message fields';
      case 'core.compare-times': return `${value('leftField') || '$value'} ${value('operator') || 'before'} ${value('rightSource') || 'now'}`;
      case 'core.set-field': return `${value('field') || '$value'} ← ${compactValue(value('value'))}`;
      case 'core.select-field': return `Use ${field}`;
      case 'core.current-time': return `→ ${value('outputField') || '$value'}`;
      case 'core.time-since': return `${field} → ${value('unit') || 'milliseconds'}`;
      case 'core.multiply': return `${field} × ${compactValue(value('factor'))}`;
      case 'core.divide': return `${field} ÷ ${compactValue(value('divisor'))}`;
      case 'core.average': return compactValue(value('fields')) || 'Message value';
      case 'core.clamp': return `${field}: ${compactValue(value('minimum'))}…${compactValue(value('maximum'))}`;
      case 'core.scale': return `${compactValue(value('inputMin'))}…${compactValue(value('inputMax'))} → ${compactValue(value('outputMin'))}…${compactValue(value('outputMax'))}`;
      case 'core.get-context': case 'core.delete-context': return `${value('scope') || 'script'} · ${value('key') || 'key'}`;
      case 'core.set-context': case 'core.increment-context': return `${value('scope') || 'script'} · ${value('key') || 'key'} ← ${source}`;
      case 'core.set-time-context': return `${value('key') || 'key'} ← ${value('source') || 'now'}`;
      case 'core.get-time-context': return `${value('key') || 'key'} → ${value('outputField') || '$value'}`;
      case 'core.set-tag': return `${tagName(value('tagPath')) || 'tag path'} ← ${source}`;
      case 'core.send-control': return `${value('deviceName') ? `${value('deviceName')}.` : ''}${tagName(value('tagPath')) || 'control path'} ← ${source}`;
      case 'core.send-notification': return `${value('severity') || 'INFO'} · ${value('profile') || 'profile'}`;
      case 'core.log-event': return `${value('severity') || 'INFO'} · ${value('message') || 'message'}`;
      case 'core.debug': return String(value('label') || 'Message trace');
      default: return '';
    }
  }

  private nodeLabel(id: string): string {
    const node = this.graph?.nodes.find(item => item.id === id); return this.definitions.get(node?.type || '')?.name || node?.type || id;
  }

  private bind(): void {
    this.querySelectorAll<HTMLElement>('.vsc-node').forEach(node => {
      node.addEventListener('click', event => { if ((event.target as HTMLElement).closest('.vsc-port,.vsc-manual-trigger')) return; this.select(node.dataset.nodeId || ''); });
      node.addEventListener('keydown', event => this.onNodeKey(event, node.dataset.nodeId || ''));
      node.querySelector('.vsc-head')?.addEventListener('pointerdown', event => this.startDrag(event as PointerEvent, node.dataset.nodeId || ''));
    });
    this.querySelectorAll<HTMLButtonElement>('.vsc-port').forEach(port => {
      port.addEventListener('click', event => { event.stopPropagation(); this.portClick(port); });
      if (port.dataset.kind === 'out') port.addEventListener('pointerdown', event => this.startConnectionDrag(event, port));
    });
    this.querySelectorAll<HTMLButtonElement>('.vsc-manual-trigger').forEach(button => button.addEventListener('click', event => {
      event.stopPropagation();
      if (button.disabled) return;
      this.dispatchEvent(new CustomEvent('visual-manual-trigger', { detail: { nodeId: button.dataset.triggerNode || '' }, bubbles: true }));
    }));
    this.querySelectorAll<SVGPathElement>('.vsc-edge-hit').forEach(path => {
      path.addEventListener('click', event => { event.stopPropagation(); this.selectEdge(path.closest<SVGGElement>('.vsc-edge')?.dataset.edgeId || ''); });
      path.addEventListener('keydown', event => { if (isDeleteKey(event)) { event.preventDefault(); this.removeEdge(path.closest<SVGGElement>('.vsc-edge')?.dataset.edgeId || ''); } });
    });
    this.querySelectorAll<SVGCircleElement>('.vsc-edge-handle').forEach(handle => handle.addEventListener('pointerdown', event => this.startRewireDrag(event, handle)));
    this.querySelector('.vsc-surface')?.addEventListener('click', event => { if ((event.target as HTMLElement).classList.contains('vsc-surface')) this.select(''); });
  }

  private select(id: string): void { this.selected = id; this.selectedEdge = ''; this.dispatchEvent(new CustomEvent('visual-node-selected', { detail: { nodeId: id }, bubbles: true })); this.render(); }
  private selectEdge(id: string): void { if (this.readonly || !id) return; this.selected = ''; this.selectedEdge = id; this.dispatchEvent(new CustomEvent('visual-edge-selected', { detail: { edgeId: id }, bubbles: true })); this.render(); this.querySelector<SVGPathElement>(`.vsc-edge[data-edge-id="${CSS.escape(id)}"] .vsc-edge-hit`)?.focus(); }

  private onNodeKey(event: KeyboardEvent, id: string): void {
    if (this.readonly) return;
    if (isDeleteKey(event)) { event.preventDefault(); this.removeNode(id); return; }
    const delta: Record<string, [number, number]> = { ArrowLeft: [-10, 0], ArrowRight: [10, 0], ArrowUp: [0, -10], ArrowDown: [0, 10] };
    if (delta[event.key]) { event.preventDefault(); const node = this.graph?.nodes.find(n => n.id === id); if (node) { node.position.x = Math.max(0, node.position.x + delta[event.key][0]); node.position.y = Math.max(0, node.position.y + delta[event.key][1]); this.changed(); } }
  }

  private startDrag(event: PointerEvent, id: string): void {
    if (this.readonly || event.button !== 0 || !this.graph) return;
    const node = this.graph.nodes.find(n => n.id === id); if (!node) return;
    event.preventDefault(); this.drag = { id, startX: event.clientX, startY: event.clientY, x: node.position.x, y: node.position.y, moved: false };
    document.addEventListener('pointermove', this.moveDrag); document.addEventListener('pointerup', this.endDrag);
  }
  private moveDrag = (event: PointerEvent): void => { if (!this.drag || !this.graph) return; const node = this.graph.nodes.find(n => n.id === this.drag!.id); if (!node) return; const x = Math.max(0, Math.round((this.drag.x + event.clientX - this.drag.startX) / 10) * 10); const y = Math.max(0, Math.round((this.drag.y + event.clientY - this.drag.startY) / 10) * 10); if (x === node.position.x && y === node.position.y) return; node.position.x = x; node.position.y = y; this.drag.moved = true; const el = this.querySelector<HTMLElement>(`[data-node-id="${CSS.escape(node.id)}"]`); if (el) { el.style.left = `${node.position.x}px`; el.style.top = `${node.position.y}px`; } };
  private endDrag = (): void => { if (this.drag?.moved) this.changed(); this.stopDrag(); };
  private stopDrag(): void { this.drag = null; document.removeEventListener('pointermove', this.moveDrag); document.removeEventListener('pointerup', this.endDrag); }

  private startConnectionDrag(event: PointerEvent, port: HTMLButtonElement): void {
    if (this.readonly || event.button !== 0 || !this.graph) return;
    event.preventDefault(); event.stopPropagation();
    const surface = this.querySelector<HTMLElement>('.vsc-surface'); if (!surface) return;
    this.stopRewireDrag();
    const surfaceRect = surface.getBoundingClientRect(); const portRect = port.getBoundingClientRect();
    this.pendingOutput = null;
    this.connectionDrag = {
      from: { nodeId: port.dataset.node || '', port: port.dataset.port || '' },
      x: portRect.left + portRect.width / 2 - surfaceRect.left,
      y: portRect.top + portRect.height / 2 - surfaceRect.top,
    };
    this.classList.add('connecting'); port.classList.add('pending');
    surface.querySelector('svg.vsc-overlay')?.insertAdjacentHTML('beforeend', '<path class="vsc-draft-edge" aria-hidden="true"></path>');
    this.setDraftPath({ x: this.connectionDrag.x, y: this.connectionDrag.y }, { x: this.connectionDrag.x, y: this.connectionDrag.y });
    document.addEventListener('pointermove', this.moveConnectionDrag);
    document.addEventListener('pointerup', this.endConnectionDrag);
    document.addEventListener('pointercancel', this.cancelConnectionDrag);
  }

  private moveConnectionDrag = (event: PointerEvent): void => {
    if (!this.connectionDrag) return;
    const point = this.surfacePoint(event.clientX, event.clientY); if (!point) return;
    this.setDraftPath({ x: this.connectionDrag.x, y: this.connectionDrag.y }, point);
    this.querySelectorAll('.vsc-in.drop-target').forEach(input => input.classList.remove('drop-target'));
    const input = this.portFromEvent(event, 'in');
    if (input && this.canConnect(this.connectionDrag.from, input)) input.classList.add('drop-target');
  };

  private endConnectionDrag = (event: PointerEvent): void => {
    if (!this.connectionDrag || !this.graph) return;
    const from = this.connectionDrag.from;
    const input = this.portFromEvent(event, 'in');
    const endpoint = input ? { nodeId: input.dataset.node || '', port: input.dataset.port || '' } : null;
    const connect = !!input && !!endpoint && this.canConnect(from, input);
    this.stopConnectionDrag();
    if (!connect || !endpoint) return;
    const id = uid('edge'); this.graph.edges.push({ id, from, to: endpoint }); this.selectedEdge = id;
    this.changed();
  };
  private cancelConnectionDrag = (): void => this.stopConnectionDrag();

  private startRewireDrag(event: PointerEvent, handle: SVGCircleElement): void {
    if (this.readonly || event.button !== 0 || !this.graph) return;
    const edge = this.graph.edges.find(item => item.id === handle.dataset.edgeId); const end = handle.dataset.end as 'from'|'to'; const points = edge && this.edgePoints(edge);
    if (!edge || !points || (end !== 'from' && end !== 'to')) return;
    event.preventDefault(); event.stopPropagation(); this.stopConnectionDrag();
    this.rewireDrag = { edgeId: edge.id, end, fixed: end === 'from' ? points.to : points.from };
    this.classList.add(end === 'from' ? 'rewiring-from' : 'rewiring-to'); handle.classList.add('pending');
    this.querySelector('svg.vsc-overlay')?.insertAdjacentHTML('beforeend', '<path class="vsc-draft-edge" aria-hidden="true"></path>');
    this.setDraftPath(points.from, points.to);
    document.addEventListener('pointermove', this.moveRewireDrag);
    document.addEventListener('pointerup', this.endRewireDrag);
    document.addEventListener('pointercancel', this.cancelRewireDrag);
  }

  private moveRewireDrag = (event: PointerEvent): void => {
    if (!this.rewireDrag) return;
    const point = this.surfacePoint(event.clientX, event.clientY); if (!point) return;
    this.setDraftPath(this.rewireDrag.end === 'from' ? point : this.rewireDrag.fixed, this.rewireDrag.end === 'to' ? point : this.rewireDrag.fixed);
    this.querySelectorAll('.vsc-port.drop-target').forEach(port => port.classList.remove('drop-target'));
    const port = this.portFromEvent(event, this.rewireDrag.end === 'from' ? 'out' : 'in');
    if (port && this.canRewire(this.rewireDrag, port)) port.classList.add('drop-target');
  };

  private endRewireDrag = (event: PointerEvent): void => {
    if (!this.rewireDrag || !this.graph) return;
    const drag = this.rewireDrag; const edge = this.graph.edges.find(item => item.id === drag.edgeId);
    const port = this.portFromEvent(event, drag.end === 'from' ? 'out' : 'in');
    const endpoint = port ? { nodeId: port.dataset.node || '', port: port.dataset.port || '' } : null;
    const reconnect = !!edge && !!port && !!endpoint && this.canRewire(drag, port);
    this.stopRewireDrag();
    if (!edge || !endpoint || !reconnect) return;
    if (drag.end === 'from') edge.from = endpoint; else edge.to = endpoint;
    this.selectedEdge = edge.id; this.changed();
  };
  private cancelRewireDrag = (): void => this.stopRewireDrag();

  private stopRewireDrag(): void {
    this.rewireDrag = null; this.classList.remove('rewiring-from', 'rewiring-to');
    this.querySelectorAll('.vsc-port.drop-target,.vsc-edge-handle.pending').forEach(item => item.classList.remove('drop-target', 'pending'));
    this.querySelector('.vsc-draft-edge')?.remove();
    document.removeEventListener('pointermove', this.moveRewireDrag);
    document.removeEventListener('pointerup', this.endRewireDrag);
    document.removeEventListener('pointercancel', this.cancelRewireDrag);
  }

  private portFromEvent(event: PointerEvent, kind: 'in'|'out'): HTMLButtonElement | null {
    const selector = kind === 'in' ? '.vsc-in' : '.vsc-out';
    const direct = event.target instanceof Element ? event.target.closest<HTMLButtonElement>(selector) : null;
    if (direct && this.contains(direct)) return direct;
    const hit = document.elementFromPoint?.(event.clientX, event.clientY)?.closest<HTMLButtonElement>(selector);
    return hit && this.contains(hit) ? hit : null;
  }

  private canConnect(from: { nodeId: string; port: string }, input: HTMLButtonElement): boolean {
    return this.canConnectEndpoints(from, { nodeId: input.dataset.node || '', port: input.dataset.port || '' });
  }

  private canRewire(drag: { edgeId: string; end: 'from'|'to' }, port: HTMLButtonElement): boolean {
    const edge = this.graph?.edges.find(item => item.id === drag.edgeId); if (!edge) return false;
    const endpoint = { nodeId: port.dataset.node || '', port: port.dataset.port || '' };
    const current = drag.end === 'from' ? edge.from : edge.to;
    if (endpoint.nodeId === current.nodeId && endpoint.port === current.port) return false;
    return drag.end === 'from' ? this.canConnectEndpoints(endpoint, edge.to, edge.id) : this.canConnectEndpoints(edge.from, endpoint, edge.id);
  }

  private canConnectEndpoints(from: Endpoint, to: Endpoint, ignoreEdge = ''): boolean {
    if (!this.graph || !from.nodeId || !to.nodeId || from.nodeId === to.nodeId) return false;
    const fromNode = this.graph.nodes.find(node => node.id === from.nodeId); const toNode = this.graph.nodes.find(node => node.id === to.nodeId);
    const fromDef = this.definitions.get(fromNode?.type || ''); const toDef = this.definitions.get(toNode?.type || '');
    const output = (fromDef?.outputs || []).find(port => port.name === from.port); const input = (toDef?.inputs || []).find(port => port.name === to.port);
    if (!output || !input || output.dataType !== input.dataType) return false;
    if (this.graph.edges.some(edge => edge.id !== ignoreEdge && edge.from.nodeId === from.nodeId && edge.from.port === from.port && edge.to.nodeId === to.nodeId && edge.to.port === to.port)) return false;
    return !this.createsCycle(from.nodeId, to.nodeId, ignoreEdge);
  }

  private createsCycle(fromNode: string, toNode: string, ignoreEdge: string): boolean {
    if (!this.graph) return false;
    const outgoing = new Map<string, string[]>();
    for (const edge of this.graph.edges) { if (edge.id === ignoreEdge) continue; const next = outgoing.get(edge.from.nodeId) || []; next.push(edge.to.nodeId); outgoing.set(edge.from.nodeId, next); }
    const queue = [toNode]; const visited = new Set<string>();
    while (queue.length) { const id = queue.shift()!; if (id === fromNode) return true; if (visited.has(id)) continue; visited.add(id); queue.push(...(outgoing.get(id) || [])); }
    return false;
  }

  private surfacePoint(clientX: number, clientY: number): { x: number; y: number } | null {
    const surface = this.querySelector<HTMLElement>('.vsc-surface'); if (!surface) return null;
    const rect = surface.getBoundingClientRect(); return { x: clientX - rect.left, y: clientY - rect.top };
  }

  private setDraftPath(from: Point, to: Point): void {
    this.querySelector<SVGPathElement>('.vsc-draft-edge')?.setAttribute('d', curvePath(from, to));
  }

  private stopConnectionDrag(): void {
    this.connectionDrag = null; this.classList.remove('connecting');
    this.querySelectorAll('.vsc-in.drop-target,.vsc-out.pending').forEach(port => port.classList.remove('drop-target', 'pending'));
    this.querySelector('.vsc-draft-edge')?.remove();
    document.removeEventListener('pointermove', this.moveConnectionDrag);
    document.removeEventListener('pointerup', this.endConnectionDrag);
    document.removeEventListener('pointercancel', this.cancelConnectionDrag);
  }

  private portClick(port: HTMLButtonElement): void {
    if (this.readonly || !this.graph) return;
    const endpoint = { nodeId: port.dataset.node || '', port: port.dataset.port || '' };
    if (port.dataset.kind === 'out') { this.pendingOutput = endpoint; this.render(); return; }
    if (!this.pendingOutput || !this.canConnectEndpoints(this.pendingOutput, endpoint)) return;
    const id = uid('edge'); this.graph.edges.push({ id, from: this.pendingOutput, to: endpoint }); this.pendingOutput = null; this.selectedEdge = id; this.changed();
  }
  private removeNode(id: string): void { if (!this.graph) return; this.graph.nodes = this.graph.nodes.filter(n => n.id !== id); this.graph.edges = this.graph.edges.filter(e => e.from.nodeId !== id && e.to.nodeId !== id); this.selected = ''; this.selectedEdge = ''; this.changed(); }
  private removeEdge(id: string): void { if (this.readonly || !this.graph || !id) return; const count = this.graph.edges.length; this.graph.edges = this.graph.edges.filter(edge => edge.id !== id); if (this.graph.edges.length === count) return; this.selectedEdge = ''; this.changed(); }
  private changed(): void { this.dispatchEvent(new CustomEvent('visual-graph-change', { detail: { graph: this.graph, selectedEdge: this.selectedEdge }, bubbles: true })); this.render(); }
}

export function uid(prefix: string): string { return `${prefix}_${crypto.randomUUID?.() || Math.random().toString(36).slice(2)}`.replace(/-/g, '').slice(0, 36); }
function compactValue(value: any): string {
  if (value === undefined || value === null || value === '') return '';
  if (typeof value === 'string') return value;
  try { return JSON.stringify(value); } catch { return String(value); }
}
function tagName(path: any): string {
  const normalized = String(path ?? '').replace(/:[^.]+$/, '');
  return normalized.split(/[./]/).filter(Boolean).at(-1) || '';
}
function curvePath(from: Point, to: Point): string { const bend = Math.max(55, Math.abs(to.x - from.x) * .45); return `M ${from.x} ${from.y} C ${from.x + bend} ${from.y}, ${to.x - bend} ${to.y}, ${to.x} ${to.y}`; }
function isDeleteKey(event: KeyboardEvent): boolean { return event.key === 'Delete' || event.key === 'Backspace' || event.key === 'Decimal' || event.code === 'NumpadDecimal' || event.code === 'NumpadDelete'; }
function esc(value: any): string { return String(value ?? '').replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!)); }
if (!customElements.get('visual-script-canvas')) customElements.define('visual-script-canvas', VisualScriptCanvas);
