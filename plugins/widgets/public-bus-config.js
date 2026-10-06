/* Public bus application widget. Uses the host dashboard's live theme variables. */
(function () {
  'use strict';
  const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const tabs = [['stops','Bus Stops'],['routes','Bus Routes'],['schedule','Schedule'],['sources','Data Sources']];
  const validArea = area => area && [area.north,area.south,area.east,area.west].every(Number.isFinite) && area.north>area.south && area.east>area.west && area.north<=90 && area.south>=-90 && area.east<=180 && area.west>=-180;
  class PublicBusConfig extends HTMLElement {
    constructor() {
      super(); this.attachShadow({mode:'open'}); this.config={scope:'default'};
      this.tab='stops'; this.modal=null; this.editorPortal=null; this.editorMap=null; this.editorMarkers=[]; this.routePath=[]; this.offset=0; this.rows=[]; this.total=0; this.revision=0;
      this.status=null; this.permissions={}; this.busy=false; this.message='Connecting to the public bus application…'; this.error=false;
      this.filter={query:'',route_id:'',stop_id:'',date:''}; this.generation=0;this.assignmentDraft={};this.assignmentRoutes=[];this.tripChoices=[];this.tripTotal=0;this.tripOffset=0;this.choiceGeneration=0;
    }
    static getPropertySchema() { return [{name:'scope',type:'string',label:'Application scope',default:'default'}]; }
    setConfig(config) { this.config={scope:'default',...config}; if(this.isConnected) this.refresh(); }
    getConfig() { return {...this.config}; }
    connectedCallback() { this.render(); this.refresh(); this.timer=setInterval(()=>this.refresh(false),15000); }
    disconnectedCallback() { clearInterval(this.timer); this.generation++;this.shadowRoot.querySelector('dialog[open]')?.close();this.closeEditor(); }
    async request(operation,payload={},write=false) {
      if(!window.XACT?.applications) throw new Error('XACT application integration is not installed.');
      const result=await window.XACT.applications.request('public_bus',operation,payload,{scope:this.config.scope,revision:write?this.revision:undefined});
      if(Number.isFinite(result.revision)) this.revision=result.revision;
      return result.data;
    }
    async refresh(showBusy=true) {
      if(!showBusy&&(this.busy||this.modal||this.editorPortal)) return;
      const generation=++this.generation;
      if(showBusy) { this.busy=true; this.render(); }
      try {
        const session=await window.XACT.applications.session('public_bus');
        if(generation!==this.generation) return;
        this.permissions=session.permissions||{};
        if(!this.permissions.read) throw new Error('You need read permission for the public bus application.');
        const status=await this.request('get_status'); if(generation!==this.generation) return;
        this.status=status; this.revision=status.state.revision;
        if(this.selectedDataset && !(status.datasets||[]).some(d=>d.id===this.selectedDataset)) this.selectedDataset='';
        if(this.tab!=='sources') {
          const data=await this.request(`list_${this.tab}`,{...this.filter,dataset_id:this.selectedDataset||'',offset:this.offset,limit:50});
          if(generation!==this.generation) return;
          this.rows=data.rows||[]; this.total=data.total||0;
        }
        if(this.tab==='sources') await this.loadAssignmentChoices();
        this.message=status.active?'Configuration connected':'Import a schedule, review it, then activate it.'; this.error=false;
      } catch(error) { if(generation!==this.generation) return; this.message=error.message;this.error=true; }
      finally { if(generation===this.generation) { this.busy=false;if(showBusy||!this.modal) this.render(); } }
    }
    async mutate(op,payload) {
      if(this.busy||!this.permissions.manage) return;
      this.busy=true;this.render();
      try { await this.request(op,payload,true); this.message='Saved';this.error=false;this.busy=false;await this.refresh(); }
      catch(error) { this.message=error.message;this.error=true;this.busy=false;this.render(); }
    }
    render() {
      const disabled=this.busy?'disabled':''; const manage=this.permissions.manage&&!this.busy; const editActive=manage&&(!this.selectedDataset||this.selectedDataset===this.status?.state?.active_dataset);
      const state=this.status?.state||{}; const feeds=this.status?.feeds||[];
      const selected=this.selectedDataset||state.active_dataset||'';
      const oldDialog=this.shadowRoot.querySelector('dialog[open]');
      if(oldDialog) { oldDialog.onclose=null;oldDialog.close(); }
      this.shadowRoot.innerHTML=`<style>
        :host{display:block;height:100%;min-height:0;color:var(--content-text);background:var(--widget-bg);font-family:var(--widget-font-family,inherit);font-size:var(--widget-label-font-size,inherit)}
        *{box-sizing:border-box}.app{display:flex;flex-direction:column;height:100%;gap:10px;padding:12px;overflow:auto}
        .actions{display:flex;justify-content:flex-end;gap:6px;flex-wrap:wrap}p{margin:0}.muted,small{color:var(--footer-text);font-size:.9em}
        button,input,select{font:inherit;color:inherit;border:1px solid var(--border-color);border-radius:5px;padding:7px 9px;background:var(--input-bg,transparent)}
        button{cursor:pointer;background:var(--widget-header-bg)}button:hover:not(:disabled){background:var(--sidebar-hover)}button:focus-visible,input:focus-visible,select:focus-visible{outline:2px solid var(--accent-color);outline-offset:2px}
        button:disabled{opacity:.5;cursor:default}.primary{background:var(--accent-color);color:var(--accent-text);border-color:var(--accent-color)}
        .toolbar,.filters,.pager{display:flex;flex-wrap:wrap;gap:8px;align-items:end}label{display:flex;flex-direction:column;gap:4px;font-size:.9em}label.grow{flex:1;min-width:150px}input,select{min-width:0;max-width:100%}
        .tabs{display:flex;gap:6px;border-bottom:1px solid var(--border-color);padding-bottom:8px;overflow-x:auto;flex:none}.tabs button{white-space:nowrap}.tabs button[aria-selected=true]{color:var(--accent-color);border-color:var(--accent-color);font-weight:600}
        .notice{padding:9px 12px;border:1px solid var(--border-color);border-radius:5px;background:var(--widget-header-bg)}.notice.error{color:var(--error-color);border-color:var(--error-color);background:var(--error-bg)}
        .table-wrap{overflow:auto;flex:1;min-height:160px;border:1px solid var(--border-color);border-radius:5px}table{width:100%;border-collapse:collapse;text-align:left;font-size:.92em}th,td{padding:9px 10px;border-bottom:1px solid var(--border-color);vertical-align:top}th{position:sticky;top:0;background:var(--widget-header-bg);color:var(--widget-header-text,var(--content-text));white-space:nowrap}tbody tr:hover{background:var(--sidebar-hover)}td input{max-width:200px}
        .badge{display:inline-block;padding:2px 6px;border-radius:4px;background:var(--widget-header-bg);color:var(--accent-color)}.source-panel{display:flex;flex-direction:column;gap:12px;padding:10px}.source-row{display:flex;flex-wrap:wrap;align-items:center;gap:8px;padding:8px;border-bottom:1px solid var(--border-color)}.empty{text-align:center;padding:28px;color:var(--footer-text)}.pager{justify-content:space-between}.footnote{font-size:.8em;color:var(--footer-text);line-height:1.4}
        dialog{position:fixed;top:50%;left:50%;right:auto;bottom:auto;transform:translate(-50%,-50%);margin:0;width:min(520px,calc(100vw - 24px));max-height:min(80vh,700px);overflow:auto;border:1px solid var(--border-color);border-radius:8px;background:var(--widget-bg);color:var(--content-text);box-shadow:0 12px 40px #0006;padding:18px}dialog::backdrop{background:#0009}.dialog-body{display:flex;flex-direction:column;gap:12px}.dialog-header{display:flex;align-items:center;justify-content:space-between;gap:12px}.dialog-header h2{font-size:1.1em;margin:0}.dialog-actions{display:flex;gap:8px;justify-content:flex-end;flex-wrap:wrap}.dialog-body input[type=file]{width:100%}
        @media(max-width:600px){.app{padding:10px}.toolbar>*{flex:1}.filters label{flex:1;min-width:100px}.actions button{padding:6px 8px}}
      </style><div class="app" aria-busy="${this.busy}">
        <div class="actions" aria-label="Bus configuration actions"><button data-action="schedule-dialog" ${disabled}>Schedules</button><button data-action="import-dialog" ${!manage?'disabled':''}>Import</button><button data-action="refresh" ${disabled}>Refresh</button></div>
        ${this.error||!this.status?.active? `<div class="notice ${this.error?'error':''}" role="status" aria-live="polite">${escape(this.message)}</div>`:''}
        <div class="tabs" role="tablist" aria-label="Bus configuration tabs">${tabs.map(([id,label])=>`<button role="tab" id="tab-${id}" aria-controls="panel" aria-selected="${this.tab===id}" tabindex="${this.tab===id?'0':'-1'}" data-tab="${id}" ${disabled}>${label}</button>`).join('')}</div>
        ${this.tab==='sources'? `<div class="table-wrap source-panel" id="panel" role="tabpanel" aria-labelledby="tab-sources">${this.sourcePanel(manage)}</div>`:`
          <div class="filters"><label class="grow">Search<input data-field="query" value="${escape(this.filter.query)}" placeholder="Name or ID" ${disabled}></label>${this.tab!=='routes'?`<label>Route ID<input data-field="route_id" value="${escape(this.filter.route_id)}" ${disabled}></label>`:''}${this.tab==='schedule'?`<label>Service date<input type="date" data-field="date" value="${escape(this.filter.date)}" ${disabled}></label><label>Stop ID<input data-field="stop_id" value="${escape(this.filter.stop_id)}" ${disabled}></label>`:''}<button data-action="search" ${disabled}>Apply filters</button><button class="primary" data-action="add-record" ${!editActive?'disabled':''}>Add ${this.tab==='stops'?'stop':this.tab==='routes'?'route':'schedule entry'}</button></div>
          <div class="table-wrap" id="panel" role="tabpanel" aria-labelledby="tab-${this.tab}">${this.table(editActive)}</div>
          <div class="pager"><span class="muted">${this.total?this.offset+1:0}–${Math.min(this.offset+this.rows.length,this.total)} of ${this.total}</span><div><button data-action="previous" ${this.offset===0||this.busy?'disabled':''}>Previous</button> <button data-action="next" ${this.offset+50>=this.total||this.busy?'disabled':''}>Next</button></div></div>`}
        ${feeds.some(f=>f.id==='translink')?'<p class="footnote">Route and arrival data used in this product or service is provided by permission of TransLink. TransLink assumes no responsibility for the accuracy or currency of the Data used in this product or service.</p>':''}
      </div>
      <dialog data-dialog="schedule" aria-label="Schedules">${this.schedulePanel(selected,state,manage)}</dialog>
      <dialog data-dialog="import" aria-label="Import schedule">${this.importPanel(manage)}</dialog>`;
      this.bind();
      if(this.modal) this.shadowRoot.querySelector(`[data-dialog="${this.modal}"]`).showModal();
    }
    schedulePanel(selected,state,manage) {
      const datasets=this.status?.datasets||[];
      const current=datasets.find(d=>d.id===selected);
      const options=datasets.map(d=>`<option value="${escape(d.id)}" ${d.id===selected?'selected':''}>${escape(d.version||d.id.slice(0,8))}${d.start&&d.end?` · ${escape(d.start)}–${escape(d.end)}`:''}${d.id===state.active_dataset?' · Active':''}</option>`).join('');
      return `<div class="dialog-body"><div class="dialog-header"><h2>Schedules</h2><button data-action="close-dialog" aria-label="Close">×</button></div>
        ${datasets.length?`<label>Schedule version<select data-field="dataset"><option value="">Active schedule</option>${options}</select></label>`:'<p class="muted">Manual schedules are edited in the Schedule tab.</p>'}
        ${current?`<p>${escape(current.routes)} routes · ${escape(current.stops)} stops · ${escape(current.trips)} trips</p>${(current.warnings||[]).map(w=>`<p class="muted">${escape(w)}</p>`).join('')}`:''}
        ${datasets.length?`<div class="dialog-actions"><button data-action="rollback" ${!manage||!state.previous_dataset?'disabled':''}>Roll back</button><button class="primary" data-action="activate" ${!manage||!selected||selected===state.active_dataset?'disabled':''}>Activate selected</button></div>`:''}</div>`;
    }
    importPanel(manage) {
      const jobs=this.status?.jobs||[];
      return `<div class="dialog-body"><div class="dialog-header"><h2>Import schedule</h2><button data-action="close-dialog" aria-label="Close">×</button></div>
        <label>Configured feed<select data-import="source_id" ${!manage?'disabled':''}><option value="">Choose a feed</option>${(this.status?.feeds||[]).map(f=>`<option value="${escape(f.id)}">${escape(f.name)}</option>`).join('')}</select></label>
        <label>Or GTFS ZIP file from this computer<input data-import="file" type="file" accept=".zip,application/zip" ${!manage?'disabled':''}></label>
        <label>Or ZIP filename in the app import directory<input data-import="archive" placeholder="vancouver_transit.zip" ${!manage?'disabled':''}></label>
        <label>Route IDs (optional)<input data-import="route_ids" placeholder="Comma-separated; blank imports all bus routes" ${!manage?'disabled':''}></label>
        <p class="muted">Choose a local ZIP, a configured feed, or a staged ZIP filename. Imports appear in Schedules for review and activation.</p>
        <div class="dialog-actions"><button class="primary" data-action="import" ${!manage?'disabled':''}>Import schedule</button></div>
        ${jobs.slice(0,5).map(j=>`<p><span class="badge">${escape(j.status)}</span> ${escape(j.dataset_id?.slice(0,8)||j.id.slice(0,8))} ${escape(j.error||'')}</p>`).join('')}</div>`;
    }
    table(manage) {
      if(!this.rows.length) return '<div class="empty">No records match. Import a schedule or adjust the filters.</div>';
      if(this.tab==='schedule') return `<table><thead><tr><th>Route / Trip</th><th>Destination</th><th>Stop</th><th>Arrival</th><th>Departure</th><th>Service</th><th>Actions</th></tr></thead><tbody>${this.rows.map(r=>`<tr><td>${escape(r.route_id)}<br><small>${escape(r.trip_id)}</small></td><td>${escape(r.headsign)}</td><td>${escape(r.sequence)} · ${escape(r.stop_name)}<br><small>${escape(r.stop_id)}</small></td><td>${escape(r.arrival)||'—'}</td><td>${escape(r.departure)||'—'}</td><td>${escape(r.service_id)}${r.pickup?' · No regular pickup':''}</td><td><button data-edit="${this.rows.indexOf(r)}" ${!manage||r.origin==='gtfs'?'disabled':''}>Edit</button> <button data-delete="${this.rows.indexOf(r)}" ${!manage||r.origin==='gtfs'?'disabled':''}>Delete</button></td></tr>`).join('')}</tbody></table>`;
      return `<table><thead><tr><th>${this.tab==='stops'?'Stop':'Route'}</th><th>${this.tab==='stops'?'Location / Routes':'Type'}</th><th>Display name override</th><th>Enabled</th><th>Actions</th></tr></thead><tbody>${this.rows.map((r,i)=>`<tr><td>${escape(r.name)}<br><small>${escape(r.code||r.short_name||'')} · ${escape(r.id)}</small></td><td>${this.tab==='stops'?`${Number(r.lat).toFixed(5)}, ${Number(r.lon).toFixed(5)}<br><small>${escape((r.routes||[]).join(', '))}</small>`:escape(r.type)}</td><td><input aria-label="Display name for ${escape(r.name)}" data-name="${i}" value="${escape(r.display_name)}" ${!manage?'disabled':''}><button data-save="${i}" ${!manage?'disabled':''}>Save</button></td><td><input aria-label="Enable ${escape(r.name)}" type="checkbox" data-toggle="${i}" ${r.enabled?'checked':''} ${!manage?'disabled':''}></td><td><button data-edit="${i}" ${!manage?'disabled':''}>Edit</button> <button data-delete="${i}" ${!manage?'disabled':''}>Delete</button></td></tr>`).join('')}</tbody></table>`;
    }
    async loadAssignmentChoices() {
      const draft=this.assignmentDraft;
      if(!draft.service_date) draft.service_date=this.status?.service_date||new Date().toISOString().slice(0,10);
      if(this.routeChoiceRevision!==this.revision) {
        const routes=[];let offset=0;let total=1;
        while(offset<total){const page=await this.request('list_routes',{offset,limit:100});routes.push(...(page.rows||[]));total=page.total||0;offset+=100;}
        this.assignmentRoutes=routes;this.routeChoiceRevision=this.revision;
      }
      if(!draft.route_id&&this.assignmentRoutes.length)draft.route_id=this.assignmentRoutes[0].id;
      const generation=++this.choiceGeneration;
      if(!draft.route_id){this.tripChoices=[];this.tripTotal=0;return;}
      const page=await this.request('list_trip_instances',{date:draft.service_date,route_id:draft.route_id,offset:this.tripOffset,limit:100});
      if(generation!==this.choiceGeneration)return;
      this.tripChoices=page.rows||[];this.tripTotal=page.total||0;
      if(!this.tripChoices.some(t=>this.tripChoiceKey(t)===draft.trip_key))draft.trip_key='';
    }
    tripChoiceKey(choice){return [choice.trip.trip_id,choice.trip.service_date,choice.trip.start_time||''].join('/');}
    localDateTime(at){const d=new Date(at);return new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16);}
    sourcePanel(manage) {
      const draft=this.assignmentDraft;const disabled=!manage?'disabled':'';
      const input=(key,label,type='text',readonly='')=>`<label>${label}<input data-assignment="${key}" type="${type}" value="${escape(draft[key]||'')}" ${readonly} ${disabled}></label>`;
      const reporters=(this.status?.reporters||[]).filter(r=>r.enabled);
      return `<div>${(this.status?.sources||[]).map(s=>`<div class="source-row">${escape(s.id)} <span class="badge">${escape(s.kind)} · ${escape(s.purpose)}</span> ${s.enabled?'Enabled':'Disabled'} <button data-source="${escape(s.id)}" ${disabled}>${s.enabled?'Disable':'Enable'}</button></div>`).join('')||'<p class="empty">No sources enrolled. Configure the application bootstrap file.</p>'}</div>
      <div class="toolbar">${input('id','Assignment ID')}
      <label>Driver phone<select data-assignment="reporter_id" ${disabled}><option value="">Choose reporter</option>${reporters.map(r=>`<option value="${escape(r.id)}" ${draft.reporter_id===r.id?'selected':''}>${escape(r.id)} · ${escape(r.vehicle_id)}</option>`).join('')}</select></label>
      ${input('vehicle_id','Vehicle','text','readonly')}
      <label>Route<select data-assignment="route_id" ${disabled}>${this.assignmentRoutes.map(r=>`<option value="${escape(r.id)}" ${draft.route_id===r.id?'selected':''}>${escape(r.short_name||r.id)} · ${escape(r.name)}</option>`).join('')}</select></label>
      ${input('service_date','Service date','date')}
      <label>Scheduled departure<select data-assignment="trip_key" ${disabled}><option value="">Choose trip</option>${this.tripChoices.map(t=>`<option value="${escape(this.tripChoiceKey(t))}" ${draft.trip_key===this.tripChoiceKey(t)?'selected':''}>${escape(t.departure)} · ${escape(t.destination)} · ${escape(t.trip.trip_id)}</option>`).join('')}</select></label>
      ${input('valid_from','Reporting starts','datetime-local')}${input('valid_to','Reporting ends','datetime-local')}
      <button data-action="assignment" ${disabled}>Save assignment</button></div>
      <div class="pager"><span>${this.tripTotal} scheduled departures for this route and date</span><div><button data-action="trip-previous" ${this.tripOffset===0?'disabled':''}>Previous departures</button> <button data-action="trip-next" ${this.tripOffset+100>=this.tripTotal?'disabled':''}>Next departures</button></div></div>
      <div>${(this.status?.assignments||[]).map(a=>`<p>${escape(a.vehicle_id)} · ${escape(a.reporter_id)} → ${escape(a.trip?.trip_id)} <small>${escape(a.trip?.service_date)} ${escape(a.trip?.start_time||'')}</small> <button data-end-assignment="${escape(a.id)}" ${disabled}>End assignment</button></p>`).join('')}</div>
      <div><strong>Bus activity</strong><table><thead><tr><th>Vehicle / trip</th><th>State</th><th>Last observation</th><th>Delay</th><th>Route deviation</th></tr></thead><tbody>${(this.status?.lifecycle||[]).map(t=>`<tr><td>${escape(t.vehicle_id)}<br><small>${escape(t.trip?.trip_id)}</small></td><td>${escape(t.state)} ${escape(t.reason||'')}</td><td>${escape(t.observed_at)}</td><td>${t.state==='active'&&t.confidence!=='unavailable'?escape(t.delay_seconds)+' s':'Unavailable'}</td><td>${t.deviation?'Off route · '+Math.round(t.distance_from_route_m)+' m':escape(t.confidence==='unavailable'?'Unknown':'On route')}</td></tr>`).join('')||'<tr><td colspan="5">Waiting for fresh bus telemetry</td></tr>'}</tbody></table></div>`;
    }
    bind() {
      const root=this.shadowRoot;
      root.querySelectorAll('[data-tab]').forEach(b=>{b.onclick=()=>{this.tab=b.dataset.tab;this.offset=0;this.refresh();};b.onkeydown=e=>{if(!['ArrowLeft','ArrowRight'].includes(e.key)) return;e.preventDefault();const list=tabs.map(([id])=>id);this.tab=list[(list.indexOf(this.tab)+(e.key==='ArrowRight'?1:list.length-1))%list.length];this.offset=0;this.refresh().then(()=>this.shadowRoot.querySelector(`[data-tab="${this.tab}"]`)?.focus());};});
      root.querySelector('[data-field="dataset"]')?.addEventListener('change',e=>{this.selectedDataset=e.target.value;this.offset=0;this.refresh();});
      root.querySelectorAll('[data-action]').forEach(b=>b.onclick=()=>this.action(b.dataset.action));
      root.querySelectorAll('[data-save]').forEach(b=>b.onclick=()=>this.override(Number(b.dataset.save)));
      root.querySelectorAll('[data-edit]').forEach(b=>b.onclick=()=>this.openEditor(this.rows[Number(b.dataset.edit)]));
      root.querySelectorAll('[data-delete]').forEach(b=>b.onclick=()=>this.deleteRecord(this.rows[Number(b.dataset.delete)]));
      root.querySelectorAll('[data-toggle]').forEach(b=>b.onchange=()=>this.override(Number(b.dataset.toggle)));
      root.querySelectorAll('[data-assignment]').forEach(el=>el.onchange=()=>this.assignmentChanged(el));
      root.querySelectorAll('[data-end-assignment]').forEach(el=>el.onclick=()=>this.mutate('end_assignment',{id:el.dataset.endAssignment}));
      root.querySelectorAll('[data-source]').forEach(b=>b.onclick=()=>{const s=this.status.sources.find(s=>s.id===b.dataset.source);this.mutate('set_source',{...s,enabled:!s.enabled});});
      root.querySelectorAll('.filters input').forEach(i=>i.onkeydown=e=>{if(e.key==='Enter') this.action('search');});
      root.querySelectorAll('dialog').forEach(d=>d.onclose=()=>{if(d.isConnected&&this.modal===d.dataset.dialog) this.modal=null;});
    }
    closeModal() { this.modal=null;this.render(); }
    override(index) {const row=this.rows[index];this.mutate('set_override',{kind:this.tab==='stops'?'stop':'route',id:row.id,name:this.shadowRoot.querySelector(`[data-name="${index}"]`).value,enabled:this.shadowRoot.querySelector(`[data-toggle="${index}"]`).checked});}
    action(name) {
      if(name==='schedule-dialog'||name==='import-dialog'){this.modal=name==='schedule-dialog'?'schedule':'import';this.render();return;}
      if(name==='close-dialog')return this.closeModal();
      if(name==='refresh')return this.refresh();if(name==='previous'){this.offset=Math.max(0,this.offset-50);return this.refresh();}if(name==='next'){this.offset+=50;return this.refresh();}
      if(name==='search'){this.shadowRoot.querySelectorAll('.filters [data-field]').forEach(i=>this.filter[i.dataset.field]=i.value.trim());this.offset=0;return this.refresh();}
      if(name==='activate'||name==='rollback'){const payload=name==='activate'?{dataset_id:this.selectedDataset||this.status?.state?.active_dataset}:{};this.closeModal();return this.mutate(name,payload);}
      if(name==='import')return this.importFile();
      if(name==='add-record')return this.openEditor();
      if(name==='trip-previous'||name==='trip-next'){this.tripOffset=Math.max(0,this.tripOffset+(name==='trip-next'?100:-100));this.loadAssignmentChoices().then(()=>this.render()).catch(error=>{this.message=error.message;this.error=true;this.render();});return;}
      if(name==='assignment'){try{const fields=this.assignmentDraft;const choice=this.tripChoices.find(t=>this.tripChoiceKey(t)===fields.trip_key);if(!choice||!fields.reporter_id)throw new Error('Choose a driver phone and scheduled departure.');return this.mutate('set_assignment',{id:fields.id||'assignment_'+crypto.randomUUID(),reporter_id:fields.reporter_id,vehicle_id:fields.vehicle_id,trip:choice.trip,valid_from:new Date(fields.valid_from).toISOString(),valid_to:new Date(fields.valid_to).toISOString()});}catch(error){this.message=error.message||'Enter valid assignment dates and times.';this.error=true;this.render();}}
    }
    async assignmentChanged(el) {
      const key=el.dataset.assignment;this.assignmentDraft[key]=el.value.trim();
      if(key==='reporter_id'){const reporter=(this.status?.reporters||[]).find(r=>r.id===el.value);this.assignmentDraft.vehicle_id=reporter?.vehicle_id||'';this.render();}
      if(key==='trip_key'){const choice=this.tripChoices.find(t=>this.tripChoiceKey(t)===el.value);if(choice){this.assignmentDraft.valid_from=this.localDateTime(choice.departure_at-3600000);this.assignmentDraft.valid_to=this.localDateTime(choice.end_at+7200000);}this.render();}
      if(key==='route_id'||key==='service_date'){this.tripOffset=0;this.assignmentDraft.trip_key='';try{await this.loadAssignmentChoices();this.render();}catch(error){this.message=error.message;this.error=true;this.render();}}
    }
    editorField(name,label,value='',type='text',attributes='') {
      return `<label>${escape(label)}<input name="${name}" type="${type}" value="${escape(value)}" ${attributes}></label>`;
    }
    openEditor(row=null) {
      if(!this.permissions.manage||this.busy||this.tab==='sources'||this.selectedDataset&&this.selectedDataset!==this.status?.state?.active_dataset||row?.origin==='gtfs'&&this.tab==='schedule') return;
      this.closeEditor();
      const kind=this.tab==='stops'?'stop':this.tab==='routes'?'route':'schedule';
      this.editorKind=kind;this.editorOriginal=row;
      this.routePath=Array.isArray(row?.path)?row.path.map(p=>[Number(p[0]),Number(p[1])]):[];
      const title=`${row?'Edit':'Add'} ${kind==='schedule'?'schedule entry':kind}`;
      const stopFields=this.editorField('id','Stop ID',row?.id||'','text',row?'required readonly':'required')+this.editorField('name','Stop name',row?.name||'','text','required')+`<div class="bus-field-wide">${this.editorField('code','Stop code',row?.code||'')}</div>`+`<div class="bus-coordinates">${this.editorField('lat','Latitude',row?.lat??'','number','required step="any" min="-90" max="90"')}${this.editorField('lon','Longitude',row?.lon??'','number','required step="any" min="-180" max="180"')}</div>`;
      const routeFields=this.editorField('id','Route ID',row?.id||'','text',row?'required readonly':'required')+this.editorField('name','Route name',row?.name||'','text','required')+this.editorField('short_name','Short name',row?.short_name||'')+this.editorField('type','Route type',row?.type||'bus');
      const scheduleFields=this.editorField('route_id','Route ID',row?.route_id||'','text','required list="bus-route-ids"')+this.editorField('trip_id','Trip ID',row?.trip_id||'','text','required')+this.editorField('stop_id','Stop ID',row?.stop_id||'','text','required list="bus-stop-ids"')+this.editorField('headsign','Destination',row?.headsign||'')+this.editorField('service_date','Service date',row?.service_date||'','date','required')+this.editorField('sequence','Stop sequence',row?.sequence||1,'number','required min="1" step="1"')+this.editorField('arrival','Arrival (HH:MM:SS)',row?.arrival||'','text','required placeholder="08:30:00"')+this.editorField('departure','Departure (HH:MM:SS)',row?.departure||'','text','required placeholder="08:30:00"')+this.editorField('service_id','Service ID',row?.service_id||'manual');
      const map=`<div class="bus-map" aria-label="${kind==='stop'?'Choose stop location':'Draw route path'}"></div><p class="bus-hint">${kind==='stop'?'Click the map or drag the marker to place the stop.':'Click the map or a stop marker to add route points; drag a point to move it.'}</p>${kind==='route'?'<div class="bus-map-actions"><button type="button" data-map-undo>Undo point</button><button type="button" data-map-clear>Clear path</button><span data-point-count></span></div>':''}`;
      const dialog=document.createElement('dialog');
      dialog.className='bus-edit-dialog';
      dialog.innerHTML=`<style>
        .bus-edit-dialog{position:fixed;top:50%;left:50%;right:auto;bottom:auto;transform:translate(-50%,-50%);margin:0;width:min(760px,calc(100vw - 24px));max-height:90vh;overflow:auto;border:1px solid var(--border-color);border-radius:8px;background:var(--widget-bg);color:var(--content-text);padding:18px;box-shadow:0 12px 40px #0006}
        .bus-edit-dialog::backdrop{background:#0009}.bus-edit-dialog form{display:flex;flex-direction:column;gap:12px}.bus-edit-dialog h2{font-size:1.1em;margin:0}.bus-edit-dialog .bus-editor-header,.bus-edit-dialog .bus-editor-actions,.bus-edit-dialog .bus-map-actions{display:flex;justify-content:space-between;align-items:center;gap:8px}.bus-edit-dialog .bus-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
        .bus-edit-dialog label{display:flex;flex-direction:column;gap:4px;font-size:.9em}.bus-edit-dialog .bus-field-wide,.bus-edit-dialog .bus-coordinates{grid-column:1/-1}.bus-edit-dialog .bus-coordinates{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.bus-edit-dialog label.bus-checkbox{flex-direction:row;align-items:center;align-self:flex-start;gap:8px}.bus-edit-dialog label.bus-checkbox input{width:auto;min-width:auto;margin:0;padding:0}.bus-edit-dialog input{width:100%;min-width:0}.bus-edit-dialog button,.bus-edit-dialog input{font:inherit;color:inherit;border:1px solid var(--border-color);border-radius:5px;padding:7px 9px;background:var(--input-bg,transparent)}.bus-edit-dialog button{cursor:pointer;background:var(--widget-header-bg)}.bus-edit-dialog .primary{background:var(--accent-color);color:var(--accent-text);border-color:var(--accent-color)}
        .bus-edit-dialog .bus-map{height:300px;min-height:230px;border:1px solid var(--border-color);border-radius:5px}.bus-edit-dialog .bus-hint{margin:0;color:var(--footer-text);font-size:.9em}.bus-edit-dialog .bus-editor-error{color:var(--error-color);min-height:1em}.bus-edit-dialog .bus-map-actions{justify-content:flex-start}.bus-edit-dialog button:focus-visible,.bus-edit-dialog input:focus-visible{outline:2px solid var(--accent-color);outline-offset:2px}
        @media(max-width:560px){.bus-edit-dialog .bus-fields{grid-template-columns:1fr}.bus-edit-dialog .bus-map{height:240px}}
      </style><form><div class="bus-editor-header"><h2>${escape(title)}</h2><button type="button" data-editor-close aria-label="Close">×</button></div><div class="bus-fields">${kind==='stop'?stopFields:kind==='route'?routeFields:scheduleFields}</div><datalist id="bus-route-ids"></datalist><datalist id="bus-stop-ids"></datalist>${kind==='schedule'?'<label class="bus-checkbox"><input type="checkbox" name="pickup">No regular pickup</label>':`<label class="bus-checkbox"><input type="checkbox" name="enabled" ${row?.enabled===false?'':'checked'}>Enabled</label>${map}`}<p class="bus-editor-error" role="alert"></p><div class="bus-editor-actions"><button type="button" data-editor-close>Cancel</button><button class="primary" type="submit">Save</button></div></form>`;
      document.body.append(dialog);this.editorPortal=dialog;
      if(kind==='schedule') {dialog.querySelector('[name=pickup]').checked=!!row?.pickup;void this.loadScheduleChoices(dialog);}
      dialog.querySelectorAll('[data-editor-close]').forEach(b=>b.onclick=()=>this.closeEditor());
      dialog.querySelector('form').onsubmit=e=>{e.preventDefault();this.saveEditor();};
      dialog.onclose=()=>{if(this.editorPortal===dialog)this.closeEditor();};
      dialog.showModal();
      if(kind!=='schedule') void this.initEditorMap(dialog,row);
    }
    async loadScheduleChoices(dialog) {
      try {
        const [routes,stops]=await Promise.all([this.request('list_routes',{limit:200}),this.request('list_stops',{limit:200})]);
        if(this.editorPortal!==dialog)return;
        dialog.querySelector('#bus-route-ids').innerHTML=(routes.rows||[]).map(row=>`<option value="${escape(row.id)}" label="${escape(row.name)}"></option>`).join('');
        dialog.querySelector('#bus-stop-ids').innerHTML=(stops.rows||[]).map(row=>`<option value="${escape(row.id)}" label="${escape(row.name)}"></option>`).join('');
      } catch(error) { if(this.editorPortal===dialog)dialog.querySelector('.bus-editor-error').textContent=error.message||'Could not load routes and stops.'; }
    }
    closeEditor() {
      const dialog=this.editorPortal;
      if(!dialog) return;
      this.editorPortal=null;
      if(this.editorMap){this.editorMap.remove();this.editorMap=null;}
      this.editorMarkers=[];
      dialog.onclose=null;
      if(dialog.open)dialog.close();
      dialog.remove();
    }
    async initEditorMap(dialog,row) {
      try {
        const areaRequest=typeof window.XACT.getOrganisationArea==='function'
          ? Promise.resolve().then(()=>window.XACT.getOrganisationArea()).catch(()=>null)
          : Promise.resolve(null);
        await window.XACT.loadLeaflet();
        const apiArea=await areaRequest;
        const organisationArea=validArea(apiArea)?apiArea:validArea(this.status?.area)?this.status.area:null;
        if(this.editorPortal!==dialog) return;
        const L=window.L,container=dialog.querySelector('.bus-map');
        for(const event of ['mousedown','pointerdown','touchstart'])container.addEventListener(event,e=>e.stopPropagation(),{passive:false});
        const map=L.map(container,{zoomControl:true});
        map.attributionControl.setPrefix(false);
        this.editorMap=map;
        L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png',{attribution:'© OpenStreetMap contributors',maxZoom:19}).addTo(map);
        if(this.editorKind==='stop') {
          const lat=Number(row?.lat),lon=Number(row?.lon),placed=row?.lat!=null&&row?.lon!=null&&Number.isFinite(lat)&&Number.isFinite(lon);
          let marker=placed?L.marker([lat,lon],{draggable:true}).addTo(map):null;
          const update=(point)=>{dialog.querySelector('[name=lat]').value=point.lat.toFixed(6);dialog.querySelector('[name=lon]').value=point.lng.toFixed(6);if(marker)marker.setLatLng(point);else{marker=L.marker(point,{draggable:true}).addTo(map);marker.on('dragend',()=>update(marker.getLatLng()));}};
          if(marker)marker.on('dragend',()=>update(marker.getLatLng()));
          map.on('click',e=>update(e.latlng));
          for(const name of ['lat','lon'])dialog.querySelector(`[name="${name}"]`).onchange=()=>{const a=Number(dialog.querySelector('[name=lat]').value),b=Number(dialog.querySelector('[name=lon]').value);if(Number.isFinite(a)&&Number.isFinite(b)&&a>=-90&&a<=90&&b>=-180&&b<=180){update(L.latLng(a,b));map.panTo([a,b]);}};
        } else {
          const line=L.polyline(this.routePath,{color:'#2563eb',weight:4}).addTo(map);
          const draw=()=>{
            line.setLatLngs(this.routePath);
            this.editorMarkers.forEach(marker=>marker.remove());this.editorMarkers=[];
            this.routePath.forEach((point,index)=>{const marker=L.marker(point,{draggable:true,bubblingMouseEvents:false}).addTo(map);marker.on('dragend',()=>{const pos=marker.getLatLng();this.routePath[index]=[pos.lat,pos.lng];line.setLatLngs(this.routePath);});this.editorMarkers.push(marker);});
            dialog.querySelector('[data-point-count]').textContent=`${this.routePath.length} points`;
          };
          map.on('click',e=>{this.routePath.push([e.latlng.lat,e.latlng.lng]);draw();});
          dialog.querySelector('[data-map-undo]').onclick=()=>{this.routePath.pop();draw();};
          dialog.querySelector('[data-map-clear]').onclick=()=>{this.routePath=[];draw();};
          draw();
          if(typeof L.circleMarker==='function') this.request('list_stops',{limit:200}).then(data=>{
            if(this.editorPortal!==dialog||this.editorMap!==map)return;
            (data.rows||[]).forEach(stop=>{
              const lat=Number(stop.lat),lon=Number(stop.lon);
              if(!Number.isFinite(lat)||!Number.isFinite(lon))return;
              L.circleMarker([lat,lon],{radius:5,color:'#f59e0b',fillOpacity:.8,bubblingMouseEvents:false}).addTo(map)
                .bindTooltip(stop.name||stop.id).on('click',e=>{L.DomEvent.stopPropagation(e);this.routePath.push([lat,lon]);draw();});
            });
          }).catch(()=>{});
        }
        // Leaflet needs the visible dialog's dimensions before fitting the organisation.
        requestAnimationFrame(()=>{
          if(this.editorMap!==map)return;
          map.invalidateSize();
          if(organisationArea)map.fitBounds([[organisationArea.south,organisationArea.west],[organisationArea.north,organisationArea.east]],{padding:[20,20]});
          else if(this.editorKind==='route'&&this.routePath.length)map.fitBounds(this.routePath,{padding:[24,24]});
          else if(this.editorKind==='stop'&&row?.lat!=null&&row?.lon!=null)map.setView([Number(row.lat),Number(row.lon)],14);
          else map.setView([20,0],2);
        });
      } catch(error) {
        if(this.editorPortal===dialog)dialog.querySelector('.bus-editor-error').textContent='Map unavailable: '+(error.message||error);
      }
    }
    async saveEditor() {
      const dialog=this.editorPortal;if(!dialog||this.busy)return;
      const form=dialog.querySelector('form');if(!form.reportValidity())return;
      const values=Object.fromEntries(new FormData(form).entries());
      const kind=this.editorKind;
      let record;
      if(kind==='stop')record={id:values.id.trim(),name:values.name.trim(),code:values.code.trim(),lat:Number(values.lat),lon:Number(values.lon),enabled:!!values.enabled,display_name:this.editorOriginal?.display_name||''};
      else if(kind==='route')record={id:values.id.trim(),name:values.name.trim(),short_name:values.short_name.trim(),type:values.type.trim(),path:this.routePath,enabled:!!values.enabled,display_name:this.editorOriginal?.display_name||''};
      else record={id:this.editorOriginal?.id||'',route_id:values.route_id.trim(),trip_id:values.trip_id.trim(),stop_id:values.stop_id.trim(),headsign:values.headsign.trim(),service_date:values.service_date,sequence:Number(values.sequence),arrival:values.arrival.trim(),departure:values.departure.trim(),service_id:values.service_id.trim(),pickup:!!values.pickup};
      if(kind==='route'&&record.path.length<2){dialog.querySelector('.bus-editor-error').textContent='Place at least two points on the map.';return;}
      const op=`${this.editorOriginal?'update':'create'}_${kind}`;
      const save=dialog.querySelector('[type=submit]');save.disabled=true;
      try {
        await this.request(op,{id:this.editorOriginal?.id||'',record},true);
        this.closeEditor();await this.refresh();
      } catch(error) {dialog.querySelector('.bus-editor-error').textContent=error.message||'Could not save.';save.disabled=false;}
    }
    deleteRecord(row) {
      if(!this.permissions.manage||this.busy||!row||row.origin==='gtfs'&&this.tab==='schedule'||this.selectedDataset&&this.selectedDataset!==this.status?.state?.active_dataset)return;
      const kind=this.tab==='stops'?'stop':this.tab==='routes'?'route':'schedule';
      if(!window.confirm(`Delete ${kind} ${row.name||row.trip_id||row.id}?`))return;
      return this.mutate(`delete_${kind}`,{id:row.id});
    }
    async importFile() {
      const source_id=this.shadowRoot.querySelector('[data-import="source_id"]').value.trim();
      const archive=this.shadowRoot.querySelector('[data-import="archive"]').value.trim();
      const file=this.shadowRoot.querySelector('[data-import="file"]').files?.[0];
      const route_ids=this.shadowRoot.querySelector('[data-import="route_ids"]').value.split(',').map(v=>v.trim()).filter(Boolean);
      if(Number(!!source_id)+Number(!!archive)+Number(!!file)!==1){this.message='Choose one configured feed, local ZIP, or staged ZIP filename.';this.error=true;this.closeModal();return;}
      this.closeModal();
      if(file) {
        this.busy=true;this.message='Uploading GTFS ZIP…';this.error=false;this.render();
        try {
          const staged=await window.XACT.applications.upload('public_bus',file,{scope:this.config.scope});
          this.busy=false;
          return this.mutate('import_start',{archive:staged.archive,route_ids});
        } catch(error) {this.busy=false;this.message=error.message||'Could not upload GTFS ZIP.';this.error=true;this.render();return;}
      }
      return this.mutate('import_start',{source_id,archive,route_ids});
    }
  }
  window.XACT.registerWidget({type:'public-bus-config',name:'Public Bus Configuration',icon:'bus',defaultW:18,defaultH:18,minW:6,minH:8},PublicBusConfig);
})();
