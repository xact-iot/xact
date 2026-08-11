import './canvas';
import type { VisualScriptCanvas } from './canvas';
import { uid } from './canvas';
import { DraftStore } from './draft-store';
import { backupScript, lifecycle, restoreScript, saveRevision, updateScript, validateGraph } from './api';
import type { Diagnostic, GraphDocument, GraphEdge, GraphNode, NodeDefinition, RuntimeStatus, VisualScript, VisualScriptRun } from './types';
import { showConfirm } from '../components/app-dialog';

export class VisualScriptEditor extends HTMLElement {
  private script!: VisualScript;
  private catalog: NodeDefinition[] = [];
  private draft!: DraftStore;
  private diagnostics: Diagnostic[] = [];
  private selected = '';
  private selectedEdge = '';
  private runs: VisualScriptRun[] = [];
  private busy = '';
  private notice = '';

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
        visual-script-editor button{cursor:pointer} visual-script-editor button:hover:not(:disabled){border-color:var(--accent-color)} visual-script-editor button:disabled{opacity:.35;cursor:not-allowed}
        visual-script-editor .primary{background:color-mix(in srgb,var(--accent-color) 22%,transparent);border-color:var(--accent-color)}
        visual-script-editor .vse-title{font-size:15px;font-weight:700;min-width:110px}.vse-current{font-size:12px;opacity:.72;margin-right:auto}.vse-badge{font-size:11px;padding:3px 7px;border-radius:10px;background:color-mix(in srgb,var(--accent-color) 18%,transparent);text-transform:capitalize}
        visual-script-editor .vse-menu{position:relative}.vse-menu>summary{list-style:none;cursor:pointer;border:1px solid var(--border-color);border-radius:4px;padding:5px 8px;font-size:13px}.vse-menu>summary::-webkit-details-marker{display:none}.vse-menu-panel{position:absolute;right:0;top:calc(100% + 5px);z-index:5;min-width:190px;padding:6px;background:var(--modal-bg,var(--widget-bg));border:1px solid var(--border-color);border-radius:6px;box-shadow:0 10px 30px rgba(0,0,0,.35)}.vse-menu-panel button,.vse-menu-panel label{box-sizing:border-box;display:flex;width:100%;align-items:center;gap:8px;padding:7px 8px;font-size:13px}.vse-menu-panel button{border:0;text-align:left}.vse-menu-panel input{margin-left:auto;width:auto}
        visual-script-editor .vse-main{display:grid;grid-template-columns:180px minmax(360px,1fr) 230px;min-height:0}.vse-side{overflow:auto;padding:10px;background:var(--widget-bg)}.vse-palette{border-right:1px solid var(--border-color)}.vse-inspector{border-left:1px solid var(--border-color);font-size:13px}.vse-inspector h3{font-size:12px}.vse-inspector .vse-node-description{font-size:13px;line-height:1.4}.vse-inspector .vse-empty{font-size:13px;line-height:1.45}.vse-inspector button,.vse-inspector input,.vse-inspector select,.vse-inspector textarea{font-size:13px;line-height:1.3}
        visual-script-editor h3{font-size:12px;letter-spacing:.08em;text-transform:uppercase;opacity:.72;margin:11px 2px 6px}.vse-palette-item{width:100%;min-height:30px;text-align:left;margin:3px 0;display:flex;gap:7px;align-items:center}.vse-palette-item span:last-child{overflow:hidden;text-overflow:ellipsis}.vse-field{display:block;margin:10px 0}.vse-field>span{display:block;font-size:12px;opacity:.75;margin-bottom:4px}.vse-inspector .vse-field>span,.vse-inspector .vse-field small{font-size:12px;line-height:1.4}.vse-field input,.vse-field select,.vse-field textarea{width:100%;box-sizing:border-box}.vse-field textarea{min-height:72px;resize:vertical;font-family:monospace}
        visual-script-editor .vse-bottom{display:grid;grid-template-columns:1fr 1fr;border-top:1px solid var(--border-color);min-height:0}.vse-bottom>section{overflow:auto;padding:8px 11px}.vse-bottom>section+section{border-left:1px solid var(--border-color)}.vse-problem,.vse-run,.vse-empty-message{font-size:13px;line-height:1.4;padding:5px 6px;border-radius:3px}.vse-problem,.vse-run{cursor:pointer}.vse-problem:hover,.vse-run:hover{background:color-mix(in srgb,var(--content-text) 5%,transparent)}.error{color:var(--status-bad-color,#f87171)}.warning{color:var(--status-warning-color,#fbbf24)}
        visual-script-editor .vse-notice{font-size:12px;max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vse-spacer{flex:1}
        @media(max-width:1200px){visual-script-editor{left:12px}.vse-main{grid-template-columns:170px minmax(340px,1fr) 220px}}
        @media(max-width:820px){visual-script-editor .vse-main{grid-template-columns:160px 1fr}.vse-inspector{position:absolute;right:0;top:52px;bottom:22%;width:220px;z-index:2;box-shadow:-6px 0 20px rgba(0,0,0,.3)}}
        @media(max-width:700px){visual-script-editor .vse-main,visual-script-editor .vse-bottom{display:none}visual-script-editor{left:12px;right:12px;bottom:auto;min-height:180px}visual-script-editor::after{content:'Use a larger screen to edit visual scripts.';padding:40px;text-align:center;opacity:.65}}
      </style>
      <div class="vse-toolbar">
        <button id="vse-back" class="primary">Exit edit</button><span class="vse-title">${esc(this.script.name)}</span><span class="vse-badge">${this.stateLabel()}</span>
        <span class="vse-current">${this.draft.dirty ? 'Unsaved changes' : 'Saved'}${this.script.simulation ? ' • Simulation' : ''}${this.script.activate ? ' • Starts with XACT' : ''}</span>
        <button id="vse-undo" ${!this.draft.canUndo?'disabled':''}>Undo</button><button id="vse-redo" ${!this.draft.canRedo?'disabled':''}>Redo</button>
        <button id="vse-save" class="primary" ${this.busy||(!this.draft.dirty&&this.script.desiredState!=='running')?'disabled':''}>Save</button>
        <button id="vse-run-control" ${this.busy?'disabled':''}>${this.script.desiredState==='running'?'Stop':'Start'}</button><button id="vse-pause" ${this.script.desiredState!=='running'||this.busy?'disabled':''}>Pause</button>
        <details class="vse-menu"><summary>Script ▾</summary><div class="vse-menu-panel"><button id="vse-backup">Backup</button><button id="vse-restore" ${!this.script.hasBackup?'disabled':''}>Restore</button><label>Simulate<input id="vse-simulate" type="checkbox" ${this.script.simulation?'checked':''}></label><label>Activate<input id="vse-activate" type="checkbox" ${this.script.activate?'checked':''}></label></div></details>
        <span class="vse-notice ${this.notice.startsWith('Error:')?'error':''}" title="${esc(this.notice)}" aria-live="polite">${esc(this.notice)}</span>
      </div>
      <div class="vse-main">
        <aside class="vse-side vse-palette"><input id="vse-search" placeholder="Search nodes" aria-label="Search node palette" style="width:100%;box-sizing:border-box">
          ${[...grouped].map(([category, items]) => `<h3>${esc(category)}</h3>${items.map(d => `<button class="vse-palette-item" data-add="${esc(d.type)}" title="${esc(d.description)}" ${!d.available?'disabled':''}><span>${esc(d.icon)}</span><span>${esc(d.name)}</span></button>`).join('')}`).join('')}
        </aside>
        <visual-script-canvas id="vse-canvas" tabindex="0" aria-label="Visual script graph canvas"></visual-script-canvas>
        <aside class="vse-side vse-inspector">${selectedConnection ? this.connectionInspectorMarkup(selectedConnection, graph) : this.inspectorMarkup(selectedNode, definition)}</aside>
      </div>
      <div class="vse-bottom">
        <section><h3>Problems (${this.diagnostics.length})</h3>${this.diagnostics.length ? this.diagnostics.map(d => `<div class="vse-problem ${d.severity}" data-focus="${esc(d.nodeId || '')}"><b>${esc(d.severity.toUpperCase())}</b> ${esc(d.message)}</div>`).join('') : '<div class="vse-empty-message" style="opacity:.6">The script is validated automatically before it starts.</div>'}</section>
        <section><h3>Recent runs</h3>${this.runs.length ? this.runs.map(run => `<div class="vse-run"><b>${esc(run.status)}</b> · ${run.nodesExecuted} nodes · ${run.durationMs} ms<br><span style="opacity:.6">${esc(new Date(run.startedAt).toLocaleString())}</span></div>`).join('') : '<div class="vse-empty-message" style="opacity:.6">No runs yet.</div>'}</section>
      </div>`;
    const canvas = this.querySelector<VisualScriptCanvas>('#vse-canvas'); canvas?.setData(graph, this.catalog, false, this.selected, this.selectedEdge); this.bind();
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
      return `<label class="vse-field"><span>${esc(p.label)}</span><input data-param="${esc(p.name)}" data-type="${esc(p.type)}" type="${p.type==='number'?'number':'text'}" value="${esc(value ?? '')}">${p.description?`<small style="opacity:.5">${esc(p.description)}</small>`:''}</label>`;
    }).join('')}<button id="vse-delete-node" class="error">Delete node</button>`;
  }

  private bind(): void {
    this.querySelector('#vse-back')?.addEventListener('click', () => void this.exitEdit()); this.querySelector('#vse-undo')?.addEventListener('click',()=>{this.draft.undo();this.render()}); this.querySelector('#vse-redo')?.addEventListener('click',()=>{this.draft.redo();this.render()});
    this.querySelector('#vse-save')?.addEventListener('click',()=>void this.quickSave());
    this.querySelector('#vse-run-control')?.addEventListener('click',()=>void this.toggleRuntime()); this.querySelector('#vse-pause')?.addEventListener('click',()=>void this.changeState('pause'));
    this.querySelector('#vse-backup')?.addEventListener('click',()=>void this.backup()); this.querySelector('#vse-restore')?.addEventListener('click',()=>void this.restore());
    this.querySelector('#vse-simulate')?.addEventListener('change',event=>void this.setOption('simulation',(event.target as HTMLInputElement).checked));this.querySelector('#vse-activate')?.addEventListener('change',event=>void this.setOption('activate',(event.target as HTMLInputElement).checked));
    this.querySelectorAll<HTMLElement>('[data-add]').forEach(button=>button.addEventListener('click',()=>this.addNode(button.dataset.add!)));
    this.querySelector('#vse-search')?.addEventListener('input',event=>{const query=(event.target as HTMLInputElement).value.toLowerCase();this.querySelectorAll<HTMLElement>('.vse-palette-item').forEach(item=>item.style.display=item.textContent?.toLowerCase().includes(query)?'':'none')});
    this.querySelector('#vse-canvas')?.addEventListener('visual-node-selected',(event:Event)=>{this.selected=(event as CustomEvent).detail.nodeId;this.selectedEdge='';this.render()});
    this.querySelector('#vse-canvas')?.addEventListener('visual-edge-selected',(event:Event)=>{this.selected='';this.selectedEdge=(event as CustomEvent).detail.edgeId;const edge=this.draft.value.edges.find(item=>item.id===this.selectedEdge);const inspector=this.querySelector<HTMLElement>('.vse-inspector');if(edge&&inspector)inspector.innerHTML=this.connectionInspectorMarkup(edge,this.draft.value)});
    this.querySelector('#vse-canvas')?.addEventListener('visual-graph-change',(event:Event)=>{const detail=(event as CustomEvent).detail;this.selectedEdge=detail.selectedEdge??this.selectedEdge;this.draft.update(detail.graph);this.render()});
    this.querySelectorAll<HTMLElement>('[data-focus]').forEach(item=>item.addEventListener('click',()=>{const id=item.dataset.focus;if(id){this.selected=id;this.render();this.querySelector<VisualScriptCanvas>('#vse-canvas')?.focusNode(id)}}));
    this.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement>('[data-param]').forEach(input=>input.addEventListener('change',()=>this.updateParameter(input)));
    this.querySelector('#vse-delete-node')?.addEventListener('click',()=>this.deleteSelected());
  }

  private addNode(type: string): void { const definition=this.catalog.find(d=>d.type===type);if(!definition)return;const graph=this.draft.value;const config:Record<string,any>={};(definition.parameters || []).forEach(p=>{if(p.default!==undefined)config[p.name]=p.default});graph.nodes.push({id:uid('node'),type,typeVersion:definition.typeVersion,position:{x:24+(graph.nodes.length%3)*210,y:70+Math.floor(graph.nodes.length/3)*110},config});this.draft.update(graph);this.selected=graph.nodes.at(-1)!.id;this.selectedEdge='';this.render() }
  private updateParameter(input: HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement): void { const graph=this.draft.value;const node=graph.nodes.find(n=>n.id===this.selected);if(!node)return;const type=input.dataset.type;let value:any=input.value;try{if(type==='number')value=Number(value);else if(type==='boolean')value=(input as HTMLInputElement).checked;else if(type==='json')value=value.trim()===''?null:JSON.parse(value)}catch{this.notice='Error: invalid JSON value';this.render();return}node.config={...node.config,[input.dataset.param!]:value};this.draft.update(graph);this.render() }
  private deleteSelected():void{const graph=this.draft.value;graph.nodes=graph.nodes.filter(n=>n.id!==this.selected);graph.edges=graph.edges.filter(e=>e.from.nodeId!==this.selected&&e.to.nodeId!==this.selected);this.selected='';this.selectedEdge='';this.draft.update(graph);this.render()}
  private async validate():Promise<boolean>{try{this.busy='validate';this.notice='Validating…';this.render();const result=await validateGraph(this.script.id,this.draft.value);const diagnostics=result.diagnostics??[];this.diagnostics=diagnostics;this.notice=result.valid?'Graph is valid':`${diagnostics.filter(d=>d.severity==='error').length} error(s)`;this.busy='';this.render();return result.valid}catch(error){this.failed(error);return false}}
  private async save():Promise<boolean>{if(!this.draft.dirty)return true;try{this.busy='save';this.notice='Saving…';this.render();const revision=await saveRevision(this.script.id,this.script.latestRevision,this.draft.value);this.script.latestRevision=revision.revision;this.diagnostics=revision.diagnostics??[];this.draft.markSaved(revision.graph);this.busy='';this.notice='Saved';this.dispatchEvent(new CustomEvent('visual-script-updated',{bubbles:true,detail:{script:this.script}}));this.render();return true}catch(error){this.failed(error);return false}}
  private async quickSave():Promise<void>{if(this.script.desiredState==='running'&&!await this.changeState('stop'))return;if(await this.save()){this.notice='Saved';this.render()}}
  private async toggleRuntime():Promise<void>{if(this.script.desiredState==='running'){await this.changeState('stop');return}if(this.draft.dirty&&!await this.save())return;if(!await this.validate())return;await this.changeState(this.script.desiredState==='paused'?'resume':'start')}
  private async changeState(action:'start'|'pause'|'resume'|'stop'):Promise<boolean>{try{this.busy=action;this.notice=action==='start'||action==='resume'?'Validating and starting…':action==='pause'?'Pausing…':'Stopping…';this.render();const status:RuntimeStatus=await lifecycle(this.script.id,action);this.script.desiredState=status.desiredState as any;this.script.runtimeState=status.runtimeState;this.script.activeRevision=status.activeRevision;this.busy='';this.notice=`Script ${this.stateLabel().toLowerCase()}`;this.dispatchEvent(new CustomEvent('visual-script-updated',{bubbles:true,detail:{script:this.script}}));this.render();return true}catch(error){this.failed(error);return false}}
  private async backup():Promise<void>{if(this.draft.dirty&&!await this.save())return;try{this.busy='backup';this.notice='Creating backup…';this.render();this.script=await backupScript(this.script.id);this.busy='';this.notice='Backup created';this.render()}catch(error){this.failed(error)}}
  private async restore():Promise<void>{if(!this.script.hasBackup)return;const confirmed=await showConfirm('Restore the backup? This overwrites the current script and leaves it idle.',{title:'Restore script backup',confirmLabel:'Restore',cancelLabel:'Keep current'});if(!confirmed)return;try{this.busy='restore';this.notice='Restoring backup…';this.render();const revision=await restoreScript(this.script.id);this.script.latestRevision=revision.revision;this.script.desiredState='stopped';this.script.runtimeState='idle';this.diagnostics=revision.diagnostics??[];this.draft.markSaved(revision.graph);this.selected='';this.selectedEdge='';this.busy='';this.notice='Backup restored';this.render()}catch(error){this.failed(error)}}
  private async setOption(option:'simulation'|'activate',value:boolean):Promise<void>{try{this.script=await updateScript(this.script.id,{[option]:value});this.notice=option==='simulation'?(value?'Simulation enabled':'Simulation disabled'):(value?'Activate enabled':'Activate disabled');this.render()}catch(error){this.failed(error)}}
  private stateLabel():string{const state=this.script.runtimeState||this.script.desiredState;return state==='running'?'Running':state==='paused'?'Paused':'Idle'}
  private failed(error:any):void{this.busy='';this.notice=`Error: ${error?.message||error}`;this.render()}
  private async exitEdit():Promise<void>{if(await this.requestClose())this.dispatchEvent(new CustomEvent('visual-script-editor-close',{bubbles:true}))}
}
function esc(value:any):string{return String(value??'').replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!))}
if(!customElements.get('visual-script-editor'))customElements.define('visual-script-editor',VisualScriptEditor);
