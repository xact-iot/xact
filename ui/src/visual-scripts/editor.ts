import './canvas';
import type { VisualScriptCanvas } from './canvas';
import { uid } from './canvas';
import { DraftStore } from './draft-store';
import { deployRevision, getRuns, lifecycle, runManual, saveRevision, validateGraph } from './api';
import type { Diagnostic, GraphDocument, GraphNode, NodeDefinition, RuntimeStatus, VisualScript, VisualScriptRun } from './types';
import { showChoice } from '../components/app-dialog';

export class VisualScriptEditor extends HTMLElement {
  private script!: VisualScript;
  private catalog: NodeDefinition[] = [];
  private draft!: DraftStore;
  private diagnostics: Diagnostic[] = [];
  private selected = '';
  private runs: VisualScriptRun[] = [];
  private busy = '';
  private notice = '';

  initialize(script: VisualScript, graph: GraphDocument, catalog: NodeDefinition[], runs: VisualScriptRun[] = []): void {
    this.script = script; this.catalog = catalog; this.runs = runs; this.draft = new DraftStore(graph); this.render();
  }
  connectedCallback(): void { if (this.draft) this.render(); }
  hasUnsavedChanges(): boolean { return this.draft?.dirty || false; }
  async requestClose(): Promise<boolean> {
    if (!this.hasUnsavedChanges()) return true;
    const choice = await showChoice('The visual script draft has unsaved changes.', { title: 'Unsaved script', choices: [{ value: 'keep', label: 'Keep editing', role: 'secondary' }, { value: 'discard', label: 'Discard changes', role: 'danger' }, { value: 'save', label: 'Save draft', role: 'primary' }] });
    if (choice === 'save') return await this.save();
    return choice === 'discard';
  }

  private render(): void {
    if (!this.draft) return;
    const graph = this.draft.value; const selectedNode = graph.nodes.find(n => n.id === this.selected); const definition = this.catalog.find(d => d.type === selectedNode?.type);
    const grouped = new Map<string, NodeDefinition[]>(); for (const item of this.catalog) { const list = grouped.get(item.category) || []; list.push(item); grouped.set(item.category, list); }
    this.innerHTML = `
      <style>
        visual-script-editor{position:fixed;inset:58px 18px 18px 270px;z-index:2600;display:grid;grid-template-rows:auto 1fr minmax(120px,24%);background:var(--content-bg);border:1px solid var(--border-color);border-radius:8px;box-shadow:0 18px 60px rgba(0,0,0,.45);overflow:hidden;color:var(--content-text);font-family:var(--widget-font-family)}
        visual-script-editor .vse-toolbar{display:flex;align-items:center;gap:6px;padding:7px 9px;border-bottom:1px solid var(--border-color);background:var(--widget-bg)}
        visual-script-editor button,visual-script-editor input,visual-script-editor select,visual-script-editor textarea{font:12px var(--widget-font-family);color:var(--content-text);background:color-mix(in srgb,var(--content-text) 5%,transparent);border:1px solid var(--border-color);border-radius:4px;padding:5px 8px}
        visual-script-editor button{cursor:pointer} visual-script-editor button:hover:not(:disabled){border-color:var(--accent-color)} visual-script-editor button:disabled{opacity:.35;cursor:not-allowed}
        visual-script-editor .primary{background:color-mix(in srgb,var(--accent-color) 22%,transparent);border-color:var(--accent-color)}
        visual-script-editor .vse-title{font-weight:700;min-width:130px}.vse-revisions{font-size:10px;opacity:.65;margin-right:auto}.vse-badge{font-size:10px;padding:2px 6px;border-radius:10px;background:color-mix(in srgb,var(--accent-color) 18%,transparent)}
        visual-script-editor .vse-main{display:grid;grid-template-columns:220px minmax(300px,1fr) 280px;min-height:0}.vse-side{overflow:auto;padding:9px;background:var(--widget-bg)}.vse-palette{border-right:1px solid var(--border-color)}.vse-inspector{border-left:1px solid var(--border-color)}
        visual-script-editor h3{font-size:10px;letter-spacing:.1em;text-transform:uppercase;opacity:.6;margin:9px 2px 5px}.vse-palette-item{width:100%;text-align:left;margin:2px 0;display:flex;gap:7px;align-items:center}.vse-palette-item span:last-child{overflow:hidden;text-overflow:ellipsis}.vse-field{display:block;margin:9px 0}.vse-field>span{display:block;font-size:10px;opacity:.65;margin-bottom:3px}.vse-field input,.vse-field select,.vse-field textarea{width:100%;box-sizing:border-box}.vse-field textarea{min-height:72px;resize:vertical;font-family:monospace}
        visual-script-editor .vse-bottom{display:grid;grid-template-columns:1fr 1fr;border-top:1px solid var(--border-color);min-height:0}.vse-bottom>section{overflow:auto;padding:7px 10px}.vse-bottom>section+section{border-left:1px solid var(--border-color)}.vse-problem,.vse-run{font-size:11px;padding:4px 6px;border-radius:3px;cursor:pointer}.vse-problem:hover,.vse-run:hover{background:color-mix(in srgb,var(--content-text) 5%,transparent)}.error{color:var(--status-bad-color,#f87171)}.warning{color:var(--status-warning-color,#fbbf24)}
        visual-script-editor .vse-notice{font-size:10px;max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.vse-spacer{flex:1}
        @media(max-width:1000px){visual-script-editor{left:16px}.vse-main{grid-template-columns:170px 1fr}.vse-inspector{position:absolute;right:0;top:48px;bottom:24%;width:270px;z-index:2;box-shadow:-6px 0 20px rgba(0,0,0,.3)}}
        @media(max-width:700px){visual-script-editor .vse-main,visual-script-editor .vse-bottom{display:none}visual-script-editor{left:12px;right:12px;bottom:auto;min-height:180px}visual-script-editor::after{content:'Use a larger screen to edit visual scripts.';padding:40px;text-align:center;opacity:.65}}
      </style>
      <div class="vse-toolbar">
        <button id="vse-back" aria-label="Back to dashboard">← Back</button><span class="vse-title">${esc(this.script.name)}</span><span class="vse-badge">Editing draft</span>
        <span class="vse-revisions">Draft v${this.script.latestRevision}${this.script.activeRevision ? ` / Active v${this.script.activeRevision}` : ' / Not deployed'}${this.draft.dirty ? ' • Unsaved' : ''}</span>
        <button id="vse-undo" ${!this.draft.canUndo?'disabled':''}>Undo</button><button id="vse-redo" ${!this.draft.canRedo?'disabled':''}>Redo</button>
        <button id="vse-validate">Validate</button><button id="vse-save" class="primary" ${this.busy?'disabled':''}>${this.busy==='save'?'Saving…':'Save'}</button><button id="vse-deploy" ${this.busy?'disabled':''}>Deploy</button>
        <button id="vse-run" ${!this.script.activeRevision||this.busy?'disabled':''}>Run/Test</button>
        <button id="vse-start" ${!this.script.activeRevision?'disabled':''}>Start</button><button id="vse-pause" ${!this.script.activeRevision?'disabled':''}>Pause</button><button id="vse-stop" ${!this.script.activeRevision?'disabled':''}>Stop</button>
        <span class="vse-notice ${this.notice.startsWith('Error:')?'error':''}" title="${esc(this.notice)}" aria-live="polite">${esc(this.notice)}</span>
      </div>
      <div class="vse-main">
        <aside class="vse-side vse-palette"><input id="vse-search" placeholder="Search nodes" aria-label="Search node palette" style="width:100%;box-sizing:border-box">
          ${[...grouped].map(([category, items]) => `<h3>${esc(category)}</h3>${items.map(d => `<button class="vse-palette-item" data-add="${esc(d.type)}" title="${esc(d.description)}" ${!d.available?'disabled':''}><span>${esc(d.icon)}</span><span>${esc(d.name)}</span></button>`).join('')}`).join('')}
        </aside>
        <visual-script-canvas id="vse-canvas" tabindex="0" aria-label="Visual script graph canvas"></visual-script-canvas>
        <aside class="vse-side vse-inspector">${this.inspectorMarkup(selectedNode, definition)}</aside>
      </div>
      <div class="vse-bottom">
        <section><h3>Problems (${this.diagnostics.length})</h3>${this.diagnostics.length ? this.diagnostics.map(d => `<div class="vse-problem ${d.severity}" data-focus="${esc(d.nodeId || '')}"><b>${esc(d.severity.toUpperCase())}</b> ${esc(d.message)}</div>`).join('') : '<div style="font-size:11px;opacity:.55">Validate the graph to check for problems.</div>'}</section>
        <section><h3>Recent runs</h3>${this.runs.length ? this.runs.map(run => `<div class="vse-run"><b>${esc(run.status)}</b> · v${run.activeRevision} · ${run.nodesExecuted} nodes · ${run.durationMs} ms<br><span style="opacity:.55">${esc(new Date(run.startedAt).toLocaleString())}</span></div>`).join('') : '<div style="font-size:11px;opacity:.55">No manual runs yet.</div>'}</section>
      </div>`;
    const canvas = this.querySelector<VisualScriptCanvas>('#vse-canvas'); canvas?.setData(graph, this.catalog, false); this.bind();
  }

  private inspectorMarkup(node?: GraphNode, definition?: NodeDefinition): string {
    if (!node || !definition) return '<h3>Inspector</h3><div style="font-size:11px;opacity:.55">Select a node to configure it.</div>';
    return `<h3>${esc(definition.name)}</h3><div style="font-size:10px;opacity:.55">${esc(node.id)} · ${esc(node.type)}</div>${definition.parameters.map(p => {
      const value = node.config[p.name] ?? p.default;
      if (p.type === 'boolean') return `<label class="vse-field"><span>${esc(p.label)}</span><input type="checkbox" data-param="${esc(p.name)}" data-type="boolean" ${value?'checked':''}></label>`;
      if (p.type === 'select') return `<label class="vse-field"><span>${esc(p.label)}</span><select data-param="${esc(p.name)}" data-type="select">${(p.options||[]).map(o=>`<option ${value===o?'selected':''}>${esc(o)}</option>`).join('')}</select></label>`;
      if (p.type === 'json') return `<label class="vse-field"><span>${esc(p.label)}</span><textarea data-param="${esc(p.name)}" data-type="json">${esc(value === undefined ? '' : JSON.stringify(value, null, 2))}</textarea></label>`;
      return `<label class="vse-field"><span>${esc(p.label)}</span><input data-param="${esc(p.name)}" data-type="${esc(p.type)}" type="${p.type==='number'?'number':'text'}" value="${esc(value ?? '')}">${p.description?`<small style="opacity:.5">${esc(p.description)}</small>`:''}</label>`;
    }).join('')}<button id="vse-delete-node" class="error">Delete node</button>`;
  }

  private bind(): void {
    this.querySelector('#vse-back')?.addEventListener('click', () => void this.close()); this.querySelector('#vse-undo')?.addEventListener('click',()=>{this.draft.undo();this.render()}); this.querySelector('#vse-redo')?.addEventListener('click',()=>{this.draft.redo();this.render()});
    this.querySelector('#vse-save')?.addEventListener('click',()=>void this.save()); this.querySelector('#vse-validate')?.addEventListener('click',()=>void this.validate()); this.querySelector('#vse-deploy')?.addEventListener('click',()=>void this.deploy()); this.querySelector('#vse-run')?.addEventListener('click',()=>void this.run());
    this.querySelector('#vse-start')?.addEventListener('click',()=>void this.changeState('start')); this.querySelector('#vse-pause')?.addEventListener('click',()=>void this.changeState('pause')); this.querySelector('#vse-stop')?.addEventListener('click',()=>void this.changeState('stop'));
    this.querySelectorAll<HTMLElement>('[data-add]').forEach(button=>button.addEventListener('click',()=>this.addNode(button.dataset.add!)));
    this.querySelector('#vse-search')?.addEventListener('input',event=>{const query=(event.target as HTMLInputElement).value.toLowerCase();this.querySelectorAll<HTMLElement>('.vse-palette-item').forEach(item=>item.style.display=item.textContent?.toLowerCase().includes(query)?'':'none')});
    this.querySelector('#vse-canvas')?.addEventListener('visual-node-selected',(event:Event)=>{this.selected=(event as CustomEvent).detail.nodeId;this.render()});
    this.querySelector('#vse-canvas')?.addEventListener('visual-graph-change',(event:Event)=>{this.draft.update((event as CustomEvent).detail.graph);this.render()});
    this.querySelectorAll<HTMLElement>('[data-focus]').forEach(item=>item.addEventListener('click',()=>{const id=item.dataset.focus;if(id){this.selected=id;this.render();this.querySelector<VisualScriptCanvas>('#vse-canvas')?.focusNode(id)}}));
    this.querySelectorAll<HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement>('[data-param]').forEach(input=>input.addEventListener('change',()=>this.updateParameter(input)));
    this.querySelector('#vse-delete-node')?.addEventListener('click',()=>this.deleteSelected());
  }

  private addNode(type: string): void { const definition=this.catalog.find(d=>d.type===type);if(!definition)return;const graph=this.draft.value;const config:Record<string,any>={};definition.parameters.forEach(p=>{if(p.default!==undefined)config[p.name]=p.default});graph.nodes.push({id:uid('node'),type,typeVersion:definition.typeVersion,position:{x:260+(graph.nodes.length%4)*220,y:100+Math.floor(graph.nodes.length/4)*150},config});this.draft.update(graph);this.selected=graph.nodes.at(-1)!.id;this.render() }
  private updateParameter(input: HTMLInputElement|HTMLSelectElement|HTMLTextAreaElement): void { const graph=this.draft.value;const node=graph.nodes.find(n=>n.id===this.selected);if(!node)return;const type=input.dataset.type;let value:any=input.value;try{if(type==='number')value=Number(value);else if(type==='boolean')value=(input as HTMLInputElement).checked;else if(type==='json')value=value.trim()===''?null:JSON.parse(value)}catch{this.notice='Error: invalid JSON value';this.render();return}node.config={...node.config,[input.dataset.param!]:value};this.draft.update(graph);this.render() }
  private deleteSelected():void{const graph=this.draft.value;graph.nodes=graph.nodes.filter(n=>n.id!==this.selected);graph.edges=graph.edges.filter(e=>e.from.nodeId!==this.selected&&e.to.nodeId!==this.selected);this.selected='';this.draft.update(graph);this.render()}
  private async validate():Promise<boolean>{try{this.busy='validate';this.notice='Validating…';this.render();const result=await validateGraph(this.script.id,this.draft.value);this.diagnostics=result.diagnostics;this.notice=result.valid?'Graph is valid':`${result.diagnostics.filter(d=>d.severity==='error').length} error(s)`;this.busy='';this.render();return result.valid}catch(error){this.failed(error);return false}}
  private async save():Promise<boolean>{try{this.busy='save';this.notice='Saving draft…';this.render();const revision=await saveRevision(this.script.id,this.script.latestRevision,this.draft.value);this.script.latestRevision=revision.revision;this.diagnostics=revision.diagnostics;this.draft.markSaved(revision.graph);this.busy='';this.notice=`Saved draft v${revision.revision}`;this.dispatchEvent(new CustomEvent('visual-script-updated',{bubbles:true,detail:{script:this.script}}));this.render();return true}catch(error){this.failed(error);return false}}
  private async deploy():Promise<void>{if(this.draft.dirty&&!await this.save())return;if(!await this.validate())return;try{this.busy='deploy';this.notice='Deploying…';this.render();await deployRevision(this.script.id,this.script.latestRevision);this.script.activeRevision=this.script.latestRevision;this.script.outOfDate=false;this.busy='';this.notice=`Deployed v${this.script.latestRevision}`;this.render()}catch(error){this.failed(error)}}
  private async run():Promise<void>{let raw=window.prompt('Manual input value (JSON)', 'null');if(raw===null)return;let value:any;try{value=JSON.parse(raw)}catch{this.notice='Error: manual input must be JSON';this.render();return}try{this.busy='run';this.notice='Running…';this.render();const result=await runManual(this.script.id,value);this.runs=[result,...await getRuns(this.script.id)].slice(0,20);this.busy='';this.notice=result.status==='ok'?'Run completed':`Error: ${result.message}`;this.render()}catch(error){this.failed(error)}}
  private async changeState(action:'start'|'pause'|'stop'):Promise<void>{try{const status:RuntimeStatus=await lifecycle(this.script.id,action);this.script.desiredState=status.desiredState as any;this.notice=`Script ${status.runtimeState}`;this.render()}catch(error){this.failed(error)}}
  private failed(error:any):void{this.busy='';this.notice=`Error: ${error?.message||error}`;this.render()}
  private async close():Promise<void>{if(await this.requestClose())this.dispatchEvent(new CustomEvent('visual-script-editor-close',{bubbles:true}))}
}
function esc(value:any):string{return String(value??'').replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!))}
if(!customElements.get('visual-script-editor'))customElements.define('visual-script-editor',VisualScriptEditor);
