import './canvas';
import type { VisualScriptCanvas } from './canvas';
import { uid } from './canvas';
import { DraftStore } from './draft-store';
import { backupScript, clearRuns, getRun, lifecycle, restoreScript, runManual, saveRevision, updateScript, validateGraph } from './api';
import type { Diagnostic, GraphDocument, GraphEdge, GraphNode, NodeDefinition, RuntimeStatus, TraceEvent, VisualScript, VisualScriptRun } from './types';
import { showConfirm } from '../components/app-dialog';
import { visualScriptCategoryStyle } from './category-colors';

const paletteNodeMime = 'application/x-xact-visual-script-node';

export class VisualScriptEditor extends HTMLElement {
  private script!: VisualScript;
  private catalog: NodeDefinition[] = [];
  private draft!: DraftStore;
  private diagnostics: Diagnostic[] = [];
  private selected = '';
  private selectedEdge = '';
  private runs: VisualScriptRun[] = [];
  private formattedTraceKey: string | null = null;
  private traceGeneration = 0;
  private busy = '';
  private notice = '';
  private paletteDragOffset = { x: 0, y: 0 };
  private collapsedCategories = new Set<string>();

  initialize(script: VisualScript, graph: GraphDocument, catalog: NodeDefinition[], runs: VisualScriptRun[] = []): void {
    this.script = script; this.catalog = catalog; this.runs = runs; this.draft = new DraftStore(graph); this.render();
  }
  connectedCallback(): void { if (this.draft) this.render(); }
  hasUnsavedChanges(): boolean { return this.draft?.dirty || false; }
  async requestClose(): Promise<boolean> {
    return !this.hasUnsavedChanges() || await this.save();
  }

  private render(): void {
    if (!this.draft) return;
    const graph = this.draft.value; const selectedNode = graph.nodes.find(n => n.id === this.selected); const selectedConnection = graph.edges.find(edge => edge.id === this.selectedEdge); const definition = this.catalog.find(d => d.type === selectedNode?.type);
    const grouped = new Map<string, NodeDefinition[]>(); for (const item of this.catalog) { const list = grouped.get(item.category) || []; list.push(item); grouped.set(item.category, list); }
    this.innerHTML = `
      <style>
        visual-script-editor{position:fixed;inset:58px 18px 18px 252px;z-index:3100;display:grid;grid-template-rows:auto minmax(0,1fr) minmax(130px,22%);background:var(--content-bg);border:1px solid var(--border-color);border-radius:8px;box-shadow:0 18px 60px rgba(0,0,0,.45);overflow:hidden;color:var(--content-text);font-family:var(--widget-font-family)}
        visual-script-editor .vse-toolbar{display:flex;flex-wrap:wrap;align-items:center;gap:6px;padding:7px 9px;border-bottom:1px solid var(--border-color);background:var(--widget-bg)}
        visual-script-editor button,visual-script-editor input,visual-script-editor select,visual-script-editor textarea{font:13px var(--widget-font-family);color:var(--content-text);background:color-mix(in srgb,var(--content-text) 5%,transparent);border:1px solid var(--border-color);border-radius:4px;padding:5px 8px}
        visual-script-editor select option{color:var(--content-text);background:var(--widget-bg)}
        visual-script-editor button{cursor:pointer} visual-script-editor button:hover:not(:disabled){border-color:var(--accent-color)} visual-script-editor button:disabled{opacity:.35;cursor:not-allowed}
        visual-script-editor .primary{background:color-mix(in srgb,var(--accent-color) 22%,transparent);border-color:var(--accent-color)}
        visual-script-editor .vse-title{font-size:15px;font-weight:700;min-width:110px}.vse-current{font-size:12px;opacity:.72;margin-right:auto}.vse-badge{font-size:11px;padding:3px 7px;border-radius:10px;background:color-mix(in srgb,var(--accent-color) 18%,transparent);text-transform:capitalize}
        visual-script-editor .vse-menu{position:relative}.vse-menu>summary{list-style:none;cursor:pointer;border:1px solid var(--border-color);border-radius:4px;padding:5px 8px;font-size:13px}.vse-menu>summary::-webkit-details-marker{display:none}.vse-menu-panel{position:absolute;right:0;top:calc(100% + 5px);z-index:5;min-width:190px;padding:6px;background:var(--modal-bg,var(--widget-bg));border:1px solid var(--border-color);border-radius:6px;box-shadow:0 10px 30px rgba(0,0,0,.35)}.vse-menu-panel button,.vse-menu-panel label{box-sizing:border-box;display:flex;width:100%;align-items:center;gap:8px;padding:7px 8px;font-size:13px}.vse-menu-panel button{border:0;text-align:left}.vse-menu-panel input{margin-left:auto;width:auto}
        visual-script-editor .vse-main{display:grid;grid-template-columns:180px minmax(360px,1fr) 230px;min-height:0}.vse-side{overflow:auto;padding:10px;background:var(--widget-bg)}.vse-palette{border-right:1px solid var(--border-color)}.vse-inspector{border-left:1px solid var(--border-color);font-size:13px}.vse-inspector h3{font-size:12px}.vse-inspector .vse-node-description{font-size:13px;line-height:1.4}.vse-inspector .vse-empty{font-size:13px;line-height:1.45}.vse-inspector button,.vse-inspector input,.vse-inspector select,.vse-inspector textarea{font-size:13px;line-height:1.3}
        visual-script-editor h3{font-size:12px;letter-spacing:.08em;text-transform:uppercase;opacity:.72;margin:11px 2px 6px}.vse-category-header{width:100%;margin:11px 0 6px;padding:5px 7px;display:flex;align-items:center;gap:6px;text-align:left;font-size:12px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;background:var(--vs-category-bg);color:var(--vs-category-text);border-color:var(--vs-category-border);border-left-width:3px}.vse-category-header .vse-category-chevron{font-size:10px;transition:transform .12s}.vse-category-header[aria-expanded="false"] .vse-category-chevron{transform:rotate(-90deg)}.vse-palette-item{width:100%;min-height:30px;text-align:left;margin:3px 0;display:flex;gap:7px;align-items:center;cursor:grab;background:color-mix(in srgb,var(--vs-category-bg) 38%,var(--widget-bg));border-color:color-mix(in srgb,var(--vs-category-border) 72%,var(--border-color));color:var(--content-text)}.vse-palette-item:hover:not(:disabled){background:color-mix(in srgb,var(--vs-category-bg) 52%,var(--widget-bg));border-color:var(--vs-category-border)}.vse-palette-item:active{cursor:grabbing}.vse-palette-item.dragging{opacity:.45}.vse-palette-item span:last-child{overflow:hidden;text-overflow:ellipsis}.vse-field{display:block;margin:10px 0}.vse-field>span{display:block;font-size:12px;opacity:.75;margin-bottom:4px}.vse-inspector .vse-field>span,.vse-inspector .vse-field small{font-size:12px;line-height:1.4}.vse-field input,.vse-field select,.vse-field textarea{width:100%;box-sizing:border-box}.vse-field textarea{min-height:72px;resize:vertical;font-family:monospace}
        visual-script-editor #vse-canvas.palette-drop-target{box-shadow:inset 0 0 0 3px var(--accent-color);background-color:color-mix(in srgb,var(--accent-color) 7%,var(--content-bg))}
        visual-script-editor .vse-bottom{display:grid;grid-template-columns:1fr 1fr;border-top:1px solid var(--border-color);min-height:0}.vse-bottom>section{overflow:auto;padding:8px 11px}.vse-bottom>section+section{border-left:1px solid var(--border-color)}.vse-problem,.vse-empty-message{font-size:13px;line-height:1.4;padding:5px 6px;border-radius:3px}.vse-problem{cursor:pointer}.vse-problem:hover{background:color-mix(in srgb,var(--content-text) 7%,transparent)}.vse-trace-toolbar{display:flex;align-items:center;gap:6px;margin-bottom:5px}.vse-trace-toolbar h3{margin-right:auto}.vse-trace-toolbar button{font-size:12px;padding:3px 7px}.vse-trace-event{margin:4px 0;border:1px solid var(--border-color);border-radius:4px;overflow:hidden;font-size:12px}.vse-trace-header{display:flex;align-items:center;gap:5px;min-height:25px;padding:2px 5px;white-space:nowrap;overflow:hidden}.vse-trace-header>span{overflow:hidden;text-overflow:ellipsis}.vse-trace-header button{margin-left:auto;flex:none;padding:2px 6px;font-size:11px}.vse-trace-json{display:block;margin:0;padding:4px 6px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;border-top:1px solid var(--border-color);background:color-mix(in srgb,var(--content-text) 5%,transparent);font:11px/1.3 monospace}.vse-json-popup{position:absolute;inset:0;z-index:20;display:flex;align-items:center;justify-content:center;padding:24px;background:rgba(2,6,23,.68)}.vse-json-dialog{display:flex;flex-direction:column;width:min(720px,90%);max-height:80%;border:1px solid var(--border-color);border-radius:8px;background:var(--modal-bg,var(--widget-bg));box-shadow:0 18px 55px rgba(0,0,0,.5)}.vse-json-dialog header{display:flex;align-items:center;gap:10px;padding:9px 11px;border-bottom:1px solid var(--border-color)}.vse-json-dialog h2{font-size:14px;margin:0 auto 0 0}.vse-json-dialog pre{margin:0;padding:14px;overflow:auto;white-space:pre-wrap;word-break:break-word;font:13px/1.5 monospace;color:var(--modal-text,var(--content-text))}.error{color:var(--status-bad-color,#f87171)}.warning{color:var(--status-warning-color,#fbbf24)}
        visual-script-editor .vse-notice{font-size:12px;max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vse-spacer{flex:1}
        @media(max-width:1200px){visual-script-editor{left:12px}.vse-main{grid-template-columns:170px minmax(340px,1fr) 220px}}
        @media(max-width:820px){visual-script-editor .vse-main{grid-template-columns:160px 1fr}.vse-inspector{position:absolute;right:0;top:52px;bottom:22%;width:220px;z-index:2;box-shadow:-6px 0 20px rgba(0,0,0,.3)}}
        @media(max-width:700px){visual-script-editor .vse-main,visual-script-editor .vse-bottom{display:none}visual-script-editor{left:12px;right:12px;bottom:auto;min-height:180px}visual-script-editor::after{content:'Use a larger screen to edit visual scripts.';padding:40px;text-align:center;opacity:.65}}
      </style>
      <div class="vse-toolbar">
        <button id="vse-back" class="primary">Exit edit</button><span class="vse-title">${esc(this.script.name)}</span><span class="vse-badge">${this.stateLabel()}</span>
        <span class="vse-current">${this.draft.dirty ? 'Unsaved changes' : 'Saved'}${this.script.simulation ? ' • Simulation' : ''}${this.script.activate ? ' • Starts with XACT' : ''}</span>
        <button id="vse-undo" ${!this.draft.canUndo?'disabled':''}>Undo</button><button id="vse-redo" ${!this.draft.canRedo?'disabled':''}>Redo</button>
        <button id="vse-save" class="primary" ${this.busy||!this.draft.dirty?'disabled':''}>Save</button>
        <button id="vse-run-control" ${this.busy?'disabled':''}>${this.script.desiredState==='running'?'Stop':'Start'}</button><button id="vse-pause" ${this.script.desiredState!=='running'||this.busy?'disabled':''}>Pause</button>
        <details class="vse-menu"><summary>Script ▾</summary><div class="vse-menu-panel"><button id="vse-backup">Backup</button><button id="vse-restore" ${!this.script.hasBackup?'disabled':''}>Restore</button><label>Simulate<input id="vse-simulate" type="checkbox" ${this.script.simulation?'checked':''}></label><label>Activate<input id="vse-activate" type="checkbox" ${this.script.activate?'checked':''}></label></div></details>
        <span class="vse-notice ${this.notice.startsWith('Error:')?'error':''}" title="${esc(this.notice)}" aria-live="polite">${esc(this.notice)}</span>
      </div>
      <div class="vse-main">
        <aside class="vse-side vse-palette"><input id="vse-search" placeholder="Search nodes" aria-label="Search node palette" style="width:100%;box-sizing:border-box">
          ${[...grouped].map(([category, items], index) => { const collapsed=this.collapsedCategories.has(category);const panelId=`vse-category-${index}`;const categoryStyle=visualScriptCategoryStyle(category);return `<button class="vse-category-header" data-category="${esc(category)}" aria-expanded="${!collapsed}" aria-controls="${panelId}" style="${categoryStyle}"><span class="vse-category-chevron" aria-hidden="true">▼</span><span>${esc(category)}</span></button><div id="${panelId}" class="vse-category-items" style="${categoryStyle}" ${collapsed?'hidden':''}>${items.map(d => `<button class="vse-palette-item" data-add="${esc(d.type)}" draggable="${d.available?'true':'false'}" aria-label="Drag ${esc(d.name)} node to canvas" title="${esc(d.description)}${d.description?' — ':''}Drag to the canvas" ${!d.available?'disabled':''}><span>${esc(d.icon)}</span><span>${esc(d.name)}</span></button>`).join('')}</div>` }).join('')}
        </aside>
        <visual-script-canvas id="vse-canvas" tabindex="0" aria-label="Visual script graph canvas"></visual-script-canvas>
        <aside class="vse-side vse-inspector">${selectedConnection ? this.connectionInspectorMarkup(selectedConnection, graph) : this.inspectorMarkup(selectedNode, definition)}</aside>
      </div>
      <div class="vse-bottom">
        <section><h3>Problems (${this.diagnostics.length})</h3>${this.diagnostics.length ? this.diagnostics.map(d => `<div class="vse-problem ${d.severity}" data-focus="${esc(d.nodeId || '')}"><b>${esc(d.severity.toUpperCase())}</b> ${esc(d.message)}</div>`).join('') : '<div class="vse-empty-message" style="opacity:.6">The script is validated automatically before it starts.</div>'}</section>
        <section>${this.traceMarkup()}</section>
      </div>${this.tracePopupMarkup()}`;
    const canvas = this.querySelector<VisualScriptCanvas>('#vse-canvas'); canvas?.setData(graph, this.catalog, false, this.selected, this.selectedEdge, { showManualTrigger: true, manualTriggerEnabled: this.script.desiredState === 'running' && !this.busy }); this.bind();
  }

  private connectionInspectorMarkup(edge: GraphEdge, graph: GraphDocument): string {
    const name = (id: string) => { const node = graph.nodes.find(item => item.id === id); return this.catalog.find(item => item.type === node?.type)?.name || node?.type || 'Unknown node'; };
    return `<h3>Connection</h3><div class="vse-node-description"><b>${esc(name(edge.from.nodeId))}</b> ${esc(edge.from.port)} → <b>${esc(name(edge.to.nodeId))}</b> ${esc(edge.to.port)}</div><p class="vse-empty" style="opacity:.65">Drag either endpoint handle to reconnect it. Press Delete or Backspace to remove the connection.</p>`;
  }

  private inspectorMarkup(node?: GraphNode, definition?: NodeDefinition): string {
    if (!node || !definition) return '<h3>Inspector</h3><div class="vse-empty" style="opacity:.55">Select a node to configure it.</div>';
    return `<h3>${esc(definition.name)}</h3>${definition.description ? `<div class="vse-node-description" style="opacity:.65">${esc(definition.description)}</div>` : ''}${(definition.parameters || []).map(p => {
      const value = node.config[p.name] ?? p.default;
      if (p.type === 'boolean') return `<label class="vse-field"><span>${esc(p.label)}</span><input type="checkbox" data-param="${esc(p.name)}" data-type="boolean" ${value?'checked':''}></label>`;
      if (p.type === 'select') return `<label class="vse-field"><span>${esc(p.label)}</span><select data-param="${esc(p.name)}" data-type="select">${(p.options||[]).map(o=>`<option ${value===o?'selected':''}>${esc(o)}</option>`).join('')}</select></label>`;
      if (p.type === 'json') return `<label class="vse-field"><span>${esc(p.label)}</span><textarea data-param="${esc(p.name)}" data-type="json">${esc(value === undefined ? '' : JSON.stringify(value, null, 2))}</textarea></label>`;
      if (node.type === 'core.get-context' && p.name === 'key') {
        const keys = this.contextKeyOptions(node);
        return `<label class="vse-field"><span>${esc(p.label)}</span><input data-param="key" data-type="string" type="text" value="${esc(value ?? '')}" list="vse-context-key-options"><datalist id="vse-context-key-options">${keys.map(key=>`<option value="${esc(key)}"></option>`).join('')}</datalist><small style="opacity:.5">Choose a key written by a variable node in this scope, or enter a new key.</small></label>`;
      }
      return `<label class="vse-field"><span>${esc(p.label)}</span><input data-param="${esc(p.name)}" data-type="${esc(p.type)}" type="${p.type==='number'?'number':'text'}" value="${esc(value ?? '')}">${p.description?`<small style="opacity:.5">${esc(p.description)}</small>`:''}</label>`;
    }).join('')}<button id="vse-delete-node" class="error">Delete node</button>`;
  }

  private contextKeyOptions(node: GraphNode): string[] {
    const scope = String(node.config.scope ?? 'script');
    const writers = new Set(['core.set-context', 'core.increment-context']);
    return [...new Set(this.draft.value.nodes
      .filter(candidate => writers.has(candidate.type) && String(candidate.config.scope ?? 'script') === scope)
      .map(candidate => String(candidate.config.key ?? '').trim())
      .filter(Boolean))].sort((a, b) => a.localeCompare(b));
  }

  private traceMarkup(): string {
    const entries = this.debugTraceEntries();
    const running = this.runs.some(run => run.status === 'queued' || run.status === 'running');
    const toolbar = `<div class="vse-trace-toolbar"><h3>Debug trace</h3><button id="vse-clear-trace" class="error" ${this.busy||!this.runs.length?'disabled':''}>Clear trace</button></div>`;
    if (!entries.length) return `<div class="vse-trace">${toolbar}<div class="vse-empty-message" style="opacity:.6">${running ? 'Run in progress…' : 'No debug output recorded.'}</div></div>`;
    return `<div class="vse-trace">${toolbar}${entries.map(({ key, event }) => {
      const node = this.draft.value.nodes.find(item => item.id === event.nodeId);
      const name = this.catalog.find(item => item.type === node?.type)?.name || event.nodeType;
      const payload = JSON.stringify({ value: event.value ?? null, fields: event.fields ?? {} });
      return `<div class="vse-trace-event"><div class="vse-trace-header"><time datetime="${esc(event.timestamp)}">${esc(formatTraceTime(event.timestamp))}</time><b>${esc(name)}</b><span>· ${esc(event.status)}${event.message?` · ${esc(event.message)}`:''}</span><button data-format-trace="${esc(key)}" title="Show formatted JSON">Format JSON</button></div><code class="vse-trace-json" title="${esc(payload)}">${esc(payload)}</code></div>`;
    }).join('')}${running?'<div class="vse-empty-message" style="opacity:.6">Run in progress…</div>':''}</div>`;
  }

  private tracePopupMarkup(): string {
    if (this.formattedTraceKey === null) return '';
    const found = this.debugTraceEntries().find(item => item.key === this.formattedTraceKey);
    if (!found) return '';
    const event = found.event;
    const node = this.draft.value.nodes.find(item => item.id === event.nodeId);
    const name = this.catalog.find(item => item.type === node?.type)?.name || event.nodeType;
    const payload = JSON.stringify({ value: event.value ?? null, fields: event.fields ?? {} }, null, 2);
    return `<div class="vse-json-popup" id="vse-json-popup"><div class="vse-json-dialog" role="dialog" aria-modal="true" aria-labelledby="vse-json-title"><header><h2 id="vse-json-title">${esc(name)} output</h2><button id="vse-json-close" aria-label="Close formatted JSON">Close</button></header><pre>${esc(payload)}</pre></div></div>`;
  }

  private debugTraceEntries(): Array<{ key: string; event: TraceEvent }> {
    return this.runs.flatMap(run => (run.trace ?? [])
      .filter(event => event.nodeType === 'core.debug')
      .map(event => ({ key: `${run.runId}:${event.sequence}`, event })))
      .sort((left, right) => new Date(left.event.timestamp).getTime() - new Date(right.event.timestamp).getTime());
  }

  private bind(): void {
    this.querySelector('#vse-back')?.addEventListener('click', () => void this.exitEdit()); this.querySelector('#vse-undo')?.addEventListener('click',()=>{this.draft.undo();this.render()}); this.querySelector('#vse-redo')?.addEventListener('click',()=>{this.draft.redo();this.render()});
    this.querySelector('#vse-save')?.addEventListener('click',()=>void this.quickSave());
    this.querySelector('#vse-run-control')?.addEventListener('click',()=>void this.toggleRuntime()); this.querySelector('#vse-pause')?.addEventListener('click',()=>void this.changeState('pause'));
    this.querySelector('#vse-backup')?.addEventListener('click',()=>void this.backup()); this.querySelector('#vse-restore')?.addEventListener('click',()=>void this.restore());
    this.querySelector('#vse-simulate')?.addEventListener('change',event=>void this.setOption('simulation',(event.target as HTMLInputElement).checked));this.querySelector('#vse-activate')?.addEventListener('change',event=>void this.setOption('activate',(event.target as HTMLInputElement).checked));
    this.querySelectorAll<HTMLElement>('[data-category]').forEach(header=>header.addEventListener('click',()=>{const category=header.dataset.category;if(!category)return;if(this.collapsedCategories.has(category))this.collapsedCategories.delete(category);else this.collapsedCategories.add(category);this.render()}));
    this.querySelectorAll<HTMLElement>('[data-add]').forEach(item => {
      item.addEventListener('dragstart', event => this.startPaletteDrag(event as DragEvent, item));
      item.addEventListener('dragend', () => this.finishPaletteDrag());
    });
    const canvas = this.querySelector<VisualScriptCanvas>('#vse-canvas');
    canvas?.addEventListener('dragenter', event => this.acceptPaletteDrag(event as DragEvent));
    canvas?.addEventListener('dragover', event => this.acceptPaletteDrag(event as DragEvent));
    canvas?.addEventListener('dragleave', event => { if (!canvas.contains((event as DragEvent).relatedTarget as Node | null)) canvas.classList.remove('palette-drop-target'); });
    canvas?.addEventListener('drop', event => this.dropPaletteNode(event as DragEvent, canvas));
    canvas?.addEventListener('visual-manual-trigger', event => void this.triggerManual((event as CustomEvent).detail.nodeId));
    this.querySelector('#vse-search')?.addEventListener('input',event=>{const query=(event.target as HTMLInputElement).value.toLowerCase();this.querySelectorAll<HTMLElement>('.vse-palette-item').forEach(item=>item.style.display=item.textContent?.toLowerCase().includes(query)?'':'none')});
    this.querySelector('#vse-canvas')?.addEventListener('visual-node-selected',(event:Event)=>{this.selected=(event as CustomEvent).detail.nodeId;this.selectedEdge='';this.render()});
    this.querySelector('#vse-canvas')?.addEventListener('visual-edge-selected',(event:Event)=>{this.selected='';this.selectedEdge=(event as CustomEvent).detail.edgeId;const edge=this.draft.value.edges.find(item=>item.id===this.selectedEdge);const inspector=this.querySelector<HTMLElement>('.vse-inspector');if(edge&&inspector)inspector.innerHTML=this.connectionInspectorMarkup(edge,this.draft.value)});
    this.querySelector('#vse-canvas')?.addEventListener('visual-graph-change',(event:Event)=>{const detail=(event as CustomEvent).detail;this.selectedEdge=detail.selectedEdge??this.selectedEdge;this.draft.update(detail.graph);this.render()});
    this.querySelectorAll<HTMLElement>('[data-focus]').forEach(item=>item.addEventListener('click',()=>{const id=item.dataset.focus;if(id){this.selected=id;this.render();this.querySelector<VisualScriptCanvas>('#vse-canvas')?.focusNode(id)}}));
    this.querySelector('#vse-clear-trace')?.addEventListener('click',()=>void this.clearTrace());
    this.querySelectorAll<HTMLElement>('[data-format-trace]').forEach(item=>item.addEventListener('click',()=>{this.formattedTraceKey=item.dataset.formatTrace??null;this.render()}));
    this.querySelector('#vse-json-close')?.addEventListener('click',()=>{this.formattedTraceKey=null;this.render()});
    this.querySelector('#vse-json-popup')?.addEventListener('click',event=>{if(event.target===event.currentTarget){this.formattedTraceKey=null;this.render()}});
    this.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement>('[data-param]').forEach(input=>input.addEventListener('change',()=>this.updateParameter(input)));
    this.querySelector('#vse-delete-node')?.addEventListener('click',()=>this.deleteSelected());
  }

  private startPaletteDrag(event: DragEvent, item: HTMLElement): void {
    const type = item.dataset.add;
    if (!type || !event.dataTransfer) { event.preventDefault(); return; }
    const rect = item.getBoundingClientRect();
    this.paletteDragOffset = { x: Math.max(0, event.clientX - rect.left), y: Math.max(0, event.clientY - rect.top) };
    event.dataTransfer.effectAllowed = 'copy';
    event.dataTransfer.setData(paletteNodeMime, type);
    event.dataTransfer.setData('text/plain', type);
    item.classList.add('dragging');
    this.classList.add('palette-dragging');
  }
  private acceptPaletteDrag(event: DragEvent): void {
    if (!this.classList.contains('palette-dragging')) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy';
    (event.currentTarget as HTMLElement).classList.add('palette-drop-target');
  }
  private dropPaletteNode(event: DragEvent, canvas: VisualScriptCanvas): void {
    if (!this.classList.contains('palette-dragging')) return;
    event.preventDefault();
    event.stopPropagation();
    const type = event.dataTransfer?.getData(paletteNodeMime) || event.dataTransfer?.getData('text/plain') || '';
    const rect = canvas.getBoundingClientRect();
    const position = {
      x: Math.max(0, Math.round((event.clientX - rect.left + canvas.scrollLeft - this.paletteDragOffset.x) / 10) * 10),
      y: Math.max(0, Math.round((event.clientY - rect.top + canvas.scrollTop - this.paletteDragOffset.y) / 10) * 10),
    };
    this.finishPaletteDrag();
    this.addNode(type, position);
  }
  private finishPaletteDrag(): void {
    this.classList.remove('palette-dragging');
    this.querySelectorAll('.vse-palette-item.dragging').forEach(item => item.classList.remove('dragging'));
    this.querySelector('#vse-canvas')?.classList.remove('palette-drop-target');
  }
  private addNode(type: string, position: { x: number; y: number }): void { const definition=this.catalog.find(d=>d.type===type&&d.available);if(!definition)return;const graph=this.draft.value;const config:Record<string,any>={};(definition.parameters || []).forEach(p=>{if(p.default!==undefined)config[p.name]=p.default});graph.nodes.push({id:uid('node'),type,typeVersion:definition.typeVersion,position,config});this.draft.update(graph);this.selected=graph.nodes.at(-1)!.id;this.selectedEdge='';this.render() }
  private async triggerManual(triggerNodeId:string):Promise<void>{if(this.busy||this.script.desiredState!=='running'||!triggerNodeId)return;try{this.busy='trigger';this.notice='Queuing manual trigger…';this.render();const run=await runManual(this.script.id,triggerNodeId);this.runs=[run,...this.runs.filter(item=>item.runId!==run.runId)];this.formattedTraceKey=null;this.busy='';this.notice='Manual trigger queued';this.render();void this.pollRun(run.runId,this.traceGeneration)}catch(error){this.failed(error)}}
  private async clearTrace():Promise<void>{if(!this.runs.length)return;const previousRuns=this.runs;this.traceGeneration++;this.runs=[];this.formattedTraceKey=null;this.busy='clear-trace';this.notice='Clearing trace…';this.render();try{await clearRuns(this.script.id);this.busy='';this.notice='Trace cleared';this.render()}catch(error){this.runs=previousRuns;this.failed(error)}}
  private async pollRun(runId:string,generation:number):Promise<void>{for(const delay of [100,200,400,800,1500,2500]){await new Promise(resolve=>setTimeout(resolve,delay));if(!this.isConnected||generation!==this.traceGeneration)return;try{const run=await getRun(this.script.id,runId);if(generation!==this.traceGeneration)return;this.runs=[run,...this.runs.filter(item=>item.runId!==runId)];this.notice=run.status==='queued'||run.status==='running'?'Manual trigger running':`Manual run ${run.status}`;this.render();if(run.status!=='queued'&&run.status!=='running')return}catch{return}}}
  private updateParameter(input: HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement): void { const graph=this.draft.value;const node=graph.nodes.find(n=>n.id===this.selected);if(!node)return;const type=input.dataset.type;let value:any=input.value;try{if(type==='number')value=Number(value);else if(type==='boolean')value=(input as HTMLInputElement).checked;else if(type==='json')value=value.trim()===''?null:JSON.parse(value)}catch{this.notice='Error: invalid JSON value';this.render();return}node.config={...node.config,[input.dataset.param!]:value};this.draft.update(graph);this.render() }
  private deleteSelected():void{const graph=this.draft.value;graph.nodes=graph.nodes.filter(n=>n.id!==this.selected);graph.edges=graph.edges.filter(e=>e.from.nodeId!==this.selected&&e.to.nodeId!==this.selected);this.selected='';this.selectedEdge='';this.draft.update(graph);this.render()}
  private async validate():Promise<boolean>{try{this.busy='validate';this.notice='Validating…';this.render();const result=await validateGraph(this.script.id,this.draft.value);const diagnostics=result.diagnostics??[];this.diagnostics=diagnostics;this.notice=result.valid?'Graph is valid':`${diagnostics.filter(d=>d.severity==='error').length} error(s)`;this.busy='';this.render();return result.valid}catch(error){this.failed(error);return false}}
  private async save():Promise<boolean>{if(!this.draft.dirty)return true;try{this.busy='save';this.notice='Saving…';this.render();const revision=await saveRevision(this.script.id,this.script.latestRevision,this.draft.value);this.script.latestRevision=revision.revision;this.diagnostics=revision.diagnostics??[];this.draft.markSaved(revision.graph);this.busy='';this.notice='Saved';this.dispatchEvent(new CustomEvent('visual-script-updated',{bubbles:true,detail:{script:this.script}}));this.render();return true}catch(error){this.failed(error);return false}}
  private async quickSave():Promise<void>{if(this.script.desiredState==='running'&&!await this.changeState('stop'))return;if(await this.save()){this.notice='Saved';this.render()}}
  private async toggleRuntime():Promise<void>{if(this.script.desiredState==='running'){await this.changeState('stop');return}if(this.draft.dirty&&!await this.save())return;if(!await this.validate())return;await this.changeState(this.script.desiredState==='paused'?'resume':'start')}
  private async changeState(action:'start'|'pause'|'resume'|'stop'):Promise<boolean>{try{this.busy=action;this.notice=action==='start'||action==='resume'?'Validating and starting…':action==='pause'?'Pausing…':'Stopping…';this.render();const status:RuntimeStatus=await lifecycle(this.script.id,action);this.script.desiredState=status.desiredState as any;this.script.runtimeState=status.runtimeState;this.script.activeRevision=status.activeRevision;if(action==='start'){this.traceGeneration++;this.runs=[]}this.busy='';this.notice=`Script ${this.stateLabel().toLowerCase()}`;this.dispatchEvent(new CustomEvent('visual-script-updated',{bubbles:true,detail:{script:this.script}}));this.render();return true}catch(error){this.failed(error);return false}}
  private async backup():Promise<void>{if(this.draft.dirty&&!await this.save())return;try{this.busy='backup';this.notice='Creating backup…';this.render();this.script=await backupScript(this.script.id);this.busy='';this.notice='Backup created';this.render()}catch(error){this.failed(error)}}
  private async restore():Promise<void>{if(!this.script.hasBackup)return;const confirmed=await showConfirm('Restore the backup? This overwrites the current script and leaves it idle.',{title:'Restore script backup',confirmLabel:'Restore',cancelLabel:'Keep current'});if(!confirmed)return;try{this.busy='restore';this.notice='Restoring backup…';this.render();const revision=await restoreScript(this.script.id);this.script.latestRevision=revision.revision;this.script.desiredState='stopped';this.script.runtimeState='idle';this.diagnostics=revision.diagnostics??[];this.draft.markSaved(revision.graph);this.selected='';this.selectedEdge='';this.busy='';this.notice='Backup restored';this.render()}catch(error){this.failed(error)}}
  private async setOption(option:'simulation'|'activate',value:boolean):Promise<void>{try{this.script=await updateScript(this.script.id,{[option]:value});this.notice=option==='simulation'?(value?'Simulation enabled':'Simulation disabled'):(value?'Activate enabled':'Activate disabled');this.render()}catch(error){this.failed(error)}}
  private stateLabel():string{const state=this.script.runtimeState||this.script.desiredState;return state==='running'?'Running':state==='paused'?'Paused':'Idle'}
  private failed(error:any):void{this.busy='';this.notice=`Error: ${error?.message||error}`;this.render()}
  private async exitEdit():Promise<void>{if(await this.requestClose())this.dispatchEvent(new CustomEvent('visual-script-editor-close',{bubbles:true}))}
}
function esc(value:any):string{return String(value??'').replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!))}
function formatTraceTime(value:string):string{const date=new Date(value);if(Number.isNaN(date.getTime()))return '--:--:--.---';const pad=(part:number,size=2)=>String(part).padStart(size,'0');return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(),3)}`}
if(!customElements.get('visual-script-editor'))customElements.define('visual-script-editor',VisualScriptEditor);
