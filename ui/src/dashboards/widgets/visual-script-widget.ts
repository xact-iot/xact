import { BaseComponent } from '../../components/base-component';
import { can } from '../../permissions/permissions';
import { registerPermissions } from '../../permissions/registry';
import { showConfirm } from '../../components/app-dialog';
import { createScript, deleteScript, getCatalog, getRevision, getRuns, getScript, getStatus, lifecycle, listScripts } from '../../visual-scripts/api';
import { emptyGraph } from '../../visual-scripts/types';
import type { GraphDocument, NodeDefinition, RuntimeStatus, VisualScript, VisualScriptRun } from '../../visual-scripts/types';
import type { VisualScriptCanvas } from '../../visual-scripts/canvas';
import type { VisualScriptEditor } from '../../visual-scripts/editor';
import '../../visual-scripts/canvas';
import '../../visual-scripts/editor';
import { registerWidgetType } from './widget-registry';

registerPermissions('visual-scripts', 'Visual Scripts', [
  { name: 'view', description: 'View visual script graphs, status, and run summaries' },
  { name: 'edit', description: 'Create, edit, deploy, control, debug, and delete visual scripts' },
], 'Controls tenant-scoped visual automation. Edit grants the complete authoring and runtime lifecycle.');
registerWidgetType({ type: 'visual-script-widget', name: 'Visual Script', icon: '⌘', category: 'System', defaultW: 24, defaultH: 28, minW: 12, minH: 16 });

interface Config { scriptId: string; display: 'editor'|'overview'; showRuntimeStatus: boolean }

export class VisualScriptWidget extends BaseComponent {
  private config: Config = { scriptId: '', display: 'editor', showRuntimeStatus: true };
  private script: VisualScript | null = null;
  private graph: GraphDocument = emptyGraph();
  private catalog: NodeDefinition[] = [];
  private runs: VisualScriptRun[] = [];
  private status: RuntimeStatus | null = null;
  private scripts: VisualScript[] = [];
  private loading = true;
  private error = '';
  private forbidden = false;
  private canEdit = false;
  private editor: VisualScriptEditor | null = null;
  private statusTimer: ReturnType<typeof setInterval> | null = null;

  setConfig(value: Partial<Config>): void { this.config = { ...this.config, ...value }; if (this.isConnected) void this.load(); }
  getConfig(): Config { return { ...this.config }; }
  getPropertySchema(): any[] { return [{ name: 'showRuntimeStatus', label: 'Show runtime status', type: 'checkbox' }, { name: 'display', label: 'Display', type: 'select', context: { options: [{ value: 'editor', label: 'Graph' }, { value: 'overview', label: 'Overview' }] } }]; }
  setDashboardMode(_mode: string): void { /* Graph authoring has an independent mode. */ }
  hasUnsavedChanges(): boolean { return this.editor?.hasUnsavedChanges() || false; }
  async requestClose(): Promise<boolean> {
    if (!this.editor) return true;
    const allowed = await this.editor.requestClose();
    if (allowed) this.closeEditor(true);
    return allowed;
  }

  connectedCallback(): void { super.connectedCallback(); void this.load(); }
  disconnectedCallback(): void { this.clearStatusTimer(); this.closeEditor(false); super.disconnectedCallback(); }

  private async load(): Promise<void> {
    this.loading = true; this.error = ''; this.forbidden = false; this.render();
    const [canView, canEdit] = await Promise.all([can('visual-scripts.view'), can('visual-scripts.edit')]); this.canEdit = canEdit;
    if (!canView && !canEdit) { this.loading = false; this.forbidden = true; this.render(); return; }
    try {
      this.catalog = await getCatalog();
      if (!this.config.scriptId) { this.scripts = await listScripts(); this.script = null; this.loading = false; this.render(); return; }
      this.script = await getScript(this.config.scriptId);
      this.graph = this.script.latestRevision > 0 ? (await getRevision(this.script.id, this.script.latestRevision)).graph : emptyGraph();
      [this.runs, this.status] = await Promise.all([getRuns(this.script.id), getStatus(this.script.id)]);
      this.updateTitle(); this.startStatusTimer();
    } catch (error: any) { this.error = error?.status === 404 ? 'missing' : (error?.message || 'Backend unavailable'); }
    this.loading = false; this.render();
  }

  protected render(): void {
    this.innerHTML = `<style>
      visual-script-widget{display:block;height:100%;min-height:0;font-family:var(--widget-font-family);color:var(--content-text)}
      visual-script-widget .vsw-shell{height:100%;display:flex;flex-direction:column;min-height:0}.vsw-statusbar{display:flex;align-items:center;gap:8px;padding:6px 9px;border-bottom:1px solid var(--border-color);font-size:11px}.vsw-status{padding:2px 7px;border-radius:10px;border:1px solid var(--border-color);text-transform:capitalize}.vsw-status.running{color:var(--status-good-color,#4ade80)}.vsw-status.error{color:var(--status-bad-color,#f87171)}.vsw-status.paused{color:var(--status-warning-color,#fbbf24)}
      visual-script-widget button,visual-script-widget input,visual-script-widget select{font:12px var(--widget-font-family);color:var(--content-text);background:color-mix(in srgb,var(--content-text) 5%,transparent);border:1px solid var(--border-color);border-radius:4px;padding:5px 8px}visual-script-widget button{cursor:pointer}.vsw-grow{flex:1}.vsw-center{height:100%;display:flex;align-items:center;justify-content:center;padding:20px;text-align:center}.vsw-panel{width:min(460px,95%);padding:20px;border:1px solid var(--border-color);border-radius:8px;background:var(--widget-bg)}.vsw-panel h2{font-size:16px;margin:0 0 7px}.vsw-panel p{font-size:12px;opacity:.65}.vsw-row{display:flex;gap:6px;margin-top:10px}.vsw-row input,.vsw-row select{flex:1;min-width:0}.vsw-overview{display:grid;grid-template-columns:repeat(auto-fit,minmax(130px,1fr));gap:8px;padding:10px}.vsw-stat{border:1px solid var(--border-color);border-radius:5px;padding:9px}.vsw-stat small{display:block;opacity:.55}.vsw-stat b{font-size:15px}
    </style>${this.bodyMarkup()}`;
    if (this.script && !this.loading && !this.error && this.config.display === 'editor') this.querySelector<VisualScriptCanvas>('#vsw-canvas')?.setData(this.graph, this.catalog, true);
    this.attachEventListeners();
  }

  private bodyMarkup(): string {
    if (this.loading) return '<div class="vsw-center"><div><div style="font-size:24px">◇</div><p>Loading visual script…</p></div></div>';
    if (this.forbidden) return '<div class="vsw-center"><div class="vsw-panel"><h2>Visual script unavailable</h2><p>You need visual-scripts.view or visual-scripts.edit permission to view this automation.</p></div></div>';
    if (this.error === 'missing') return `<div class="vsw-center"><div class="vsw-panel"><h2>Script not found</h2><p>The attached script was deleted or is not available in this organisation.</p>${this.canEdit?'<button id="vsw-detach">Detach reference</button>':''}</div></div>`;
    if (this.error) return `<div class="vsw-center"><div class="vsw-panel"><h2>Backend unavailable</h2><p>${esc(this.error)}</p><button id="vsw-retry">Retry</button></div></div>`;
    if (!this.script) return `<div class="vsw-center"><div class="vsw-panel"><h2>No script attached</h2><p>Create a tenant-scoped script or attach one from the script library.</p>${this.canEdit?'<div class="vsw-row"><input id="vsw-new-name" placeholder="New script name"><button id="vsw-create">Create script</button></div>':''}<div class="vsw-row"><select id="vsw-existing"><option value="">Select existing script…</option>${this.scripts.map(s=>`<option value="${esc(s.id)}">${esc(s.name)}</option>`).join('')}</select><button id="vsw-attach">Attach</button></div></div></div>`;
    const state = this.status?.runtimeState || this.script.runtimeState || (this.script.activeRevision ? this.script.desiredState : 'draft');
    return `<div class="vsw-shell">${this.config.showRuntimeStatus||this.canEdit?`<div class="vsw-statusbar">${this.config.showRuntimeStatus?`<span class="vsw-status ${esc(state)}">${esc(state)}</span><span>Draft v${this.script.latestRevision || '—'} / Active ${this.script.activeRevision ? `v${this.script.activeRevision}` : '—'}</span>${this.script.outOfDate?'<span class="vsw-status paused">Out of date</span>':''}`:''}<span class="vsw-grow"></span>${this.canEdit?`<button id="vsw-edit">Edit Script</button><button id="vsw-run-control" ${!this.script.activeRevision?'disabled':''}>${state==='running'?'Pause':state==='paused'?'Resume':'Start'}</button><button id="vsw-stop" ${!this.script.activeRevision?'disabled':''}>Stop</button><button id="vsw-delete">Delete</button>`:''}</div>`:''}
      ${this.config.display==='overview'?`<div class="vsw-overview"><div class="vsw-stat"><small>Nodes</small><b>${this.graph.nodes.length}</b></div><div class="vsw-stat"><small>Connections</small><b>${this.graph.edges.length}</b></div><div class="vsw-stat"><small>Last run</small><b>${this.runs[0]?.status||'—'}</b></div><div class="vsw-stat"><small>Queue</small><b>${this.status?.queueDepth||0}</b></div></div>`:'<visual-script-canvas id="vsw-canvas" class="vsw-grow" aria-label="Read-only visual script graph"></visual-script-canvas>'}</div>`;
  }

  protected attachEventListeners(): void {
    const click = (selector: string, handler: () => void) => { const element = this.querySelector<HTMLElement>(selector); if (element) element.onclick = handler; };
    click('#vsw-retry',()=>void this.load()); click('#vsw-create',()=>void this.create()); click('#vsw-attach',()=>this.attachExisting()); click('#vsw-detach',()=>this.attachScript(''));
    click('#vsw-edit',()=>this.openEditor()); click('#vsw-run-control',()=>void this.toggleRuntime()); click('#vsw-stop',()=>void this.control('stop')); click('#vsw-delete',()=>void this.removeScript());
  }
  protected detachEventListeners(): void { /* Rendered nodes are discarded together. */ }

  private async create(): Promise<void> { const input=this.querySelector<HTMLInputElement>('#vsw-new-name');const name=input?.value.trim();if(!name)return;try{const script=await createScript(name);this.attachScript(script.id)}catch(error:any){this.error=error?.message||'Create failed';this.render()} }
  private attachExisting(): void { const id=this.querySelector<HTMLSelectElement>('#vsw-existing')?.value;if(id)this.attachScript(id) }
  private attachScript(id:string):void{this.config.scriptId=id;this.emit('widget-config-save',{config:this.getConfig(),forceDirty:true});void this.load()}
  private openEditor():void{if(!this.script||this.editor)return;this.editor=document.createElement('visual-script-editor') as VisualScriptEditor;this.editor.initialize(this.script,this.graph,this.catalog,this.runs);this.editor.addEventListener('visual-script-editor-close',()=>this.closeEditor(true));this.editor.addEventListener('visual-script-updated',(event:Event)=>{this.script=(event as CustomEvent).detail.script});document.body.appendChild(this.editor);this.emit('visual-script-focus-changed',{active:true})}
  private closeEditor(reload:boolean):void{if(!this.editor)return;this.editor.remove();this.editor=null;this.emit('visual-script-focus-changed',{active:false});if(reload)void this.load()}
  private async toggleRuntime():Promise<void>{const state=this.status?.runtimeState;if(state==='running')await this.control('pause');else if(state==='paused')await this.control('resume');else await this.control('start')}
  private async control(action:'start'|'pause'|'resume'|'stop'):Promise<void>{if(!this.script)return;try{this.status=await lifecycle(this.script.id,action);this.render()}catch(error:any){this.error=error?.message||'Command failed';this.render()}}
  private async removeScript():Promise<void>{if(!this.script)return;const confirmed=await showConfirm(`Delete visual script "${this.script.name}"? This stops it and removes all revisions and run history.`,{title:'Delete visual script',confirmLabel:'Delete',cancelLabel:'Keep',tone:'danger'});if(!confirmed)return;try{await deleteScript(this.script.id);this.attachScript('')}catch(error:any){this.error=error?.message||'Delete failed';this.render()}}
  private async refreshStatus():Promise<void>{if(!this.script||document.hidden)return;try{const next=await getStatus(this.script.id);if(!this.status||next.sequence>=this.status.sequence){this.status=next;this.render()}}catch{/* retain last snapshot */}}
  private startStatusTimer():void{this.clearStatusTimer();this.statusTimer=setInterval(()=>void this.refreshStatus(),10000)}
  private clearStatusTimer():void{if(this.statusTimer){clearInterval(this.statusTimer);this.statusTimer=null}}
  private updateTitle():void{(this.closest('widget-card') as any)?.setTitle?.(this.script?.name||'Visual Script')}
}
function esc(value:any):string{return String(value??'').replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]!))}
if(!customElements.get('visual-script-widget'))customElements.define('visual-script-widget',VisualScriptWidget);
