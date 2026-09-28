/* Public bus application widget. Uses the host dashboard's live theme variables. */
(function () {
  'use strict';
  const escape = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  class PublicBusConfig extends HTMLElement {
    constructor() {
      super(); this.attachShadow({mode:'open'}); this.config={scope:'default'};
      this.tab='stops'; this.offset=0; this.rows=[]; this.total=0; this.revision=0;
      this.status=null; this.permissions={}; this.busy=false; this.message='Connecting to the public bus application…'; this.error=false;
      this.filter={query:'',route_id:'',stop_id:'',date:''}; this.generation=0;
    }
    static getPropertySchema() { return [{name:'scope',type:'string',label:'Application scope',default:'default'}]; }
    setConfig(config) { this.config={scope:'default',...config}; if(this.isConnected) this.refresh(); }
    getConfig() { return {...this.config}; }
    connectedCallback() { this.render(); this.refresh(); this.timer=setInterval(()=>this.refresh(false),15000); }
    disconnectedCallback() { clearInterval(this.timer); this.generation++; }
    async request(operation,payload={},write=false) {
      if(!window.XACT?.applications) throw new Error('XACT application integration is not installed.');
      const result=await window.XACT.applications.request('public_bus',operation,payload,{scope:this.config.scope,revision:write?this.revision:undefined});
      if(Number.isFinite(result.revision)) this.revision=result.revision;
      return result.data;
    }
    async refresh(showBusy=true) {
      if(this.busy&&!showBusy) return;
      const generation=++this.generation;
      if(showBusy) { this.busy=true; this.render(); }
      try {
        const session=await window.XACT.applications.session('public_bus');
        if(generation!==this.generation) return;
        this.permissions=session.permissions||{};
        if(!this.permissions.read) throw new Error('You need read permission for the public bus application.');
        const status=await this.request('get_status'); if(generation!==this.generation) return;
        this.status=status; this.revision=status.state.revision;
        const data=await this.request(`list_${this.tab}`,{...this.filter,dataset_id:this.dataset||'',offset:this.offset,limit:50});
        if(generation!==this.generation) return;
        this.rows=data.rows||[]; this.total=data.total||0;
        this.message=status.active?'Configuration connected':'Import a schedule, review it, then activate it.'; this.error=false;
      } catch(error) { if(generation!==this.generation) return; this.message=error.message;this.error=true; }
      finally { if(generation===this.generation) { this.busy=false;this.render(); } }
    }
    async mutate(op,payload) {
      if(this.busy||!this.permissions.manage) return;
      this.busy=true;this.render();
      try { await this.request(op,payload,true); this.message='Saved';this.error=false;this.busy=false;await this.refresh(); }
      catch(error) { this.message=error.message;this.error=true;this.busy=false;this.render(); }
    }
    render() {
      const disabled=this.busy?'disabled':''; const manage=this.permissions.manage&&!this.busy;
      const state=this.status?.state||{}; const datasets=this.status?.datasets||[];const feeds=this.status?.feeds||[];
      const selected=this.dataset||state.active_dataset||'';
      const options=datasets.map(d=>`<option value="${escape(d.id)}" ${d.id===selected?'selected':''}>${escape(d.version||d.id.slice(0,8))} · ${escape(d.start)}–${escape(d.end)}${d.id===state.active_dataset?' · Active':''}</option>`).join('');
      this.shadowRoot.innerHTML=`<style>
        :host{display:block;height:100%;min-height:0;color:var(--content-text);background:var(--widget-bg);font-family:var(--widget-font-family,inherit);font-size:var(--widget-label-font-size,inherit)}
        *{box-sizing:border-box} .app{display:flex;flex-direction:column;height:100%;gap:12px;padding:16px;overflow:auto}
        header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}h2{font-size:1.1em;margin:0 0 5px}p{margin:0}.muted,small{color:var(--footer-text);font-size:.9em}
        button,input,select{font:inherit;color:inherit;border:1px solid var(--border-color);border-radius:5px;padding:7px 9px;background:var(--input-bg,transparent)}
        button{cursor:pointer;background:var(--widget-header-bg)}button:hover:not(:disabled){background:var(--sidebar-hover)}button:focus-visible,input:focus-visible,select:focus-visible,summary:focus-visible{outline:2px solid var(--accent-color);outline-offset:2px}
        button:disabled{opacity:.5;cursor:default}.primary{background:var(--accent-color);color:var(--accent-text);border-color:var(--accent-color)}
        .toolbar,.filters,.pager{display:flex;flex-wrap:wrap;gap:8px;align-items:end}label{display:flex;flex-direction:column;gap:4px;font-size:.9em}label.grow{flex:1;min-width:150px}input,select{min-width:0;max-width:100%}
        .tabs{display:flex;gap:6px;border-bottom:1px solid var(--border-color);padding-bottom:8px}.tabs button[aria-selected=true]{color:var(--accent-color);border-color:var(--accent-color);font-weight:600}
        .notice{padding:9px 12px;border:1px solid var(--border-color);border-radius:5px;background:var(--widget-header-bg)}.notice.error{color:var(--error-color);border-color:var(--error-color);background:var(--error-bg)}
        .table-wrap{overflow:auto;flex:1;min-height:160px;border:1px solid var(--border-color);border-radius:5px}table{width:100%;border-collapse:collapse;text-align:left;font-size:.92em}th,td{padding:9px 10px;border-bottom:1px solid var(--border-color);vertical-align:top}th{position:sticky;top:0;background:var(--widget-header-bg);color:var(--widget-header-text,var(--content-text));white-space:nowrap}tbody tr:hover{background:var(--sidebar-hover)}td input{max-width:200px}
        .badge{display:inline-block;padding:2px 6px;border-radius:4px;background:var(--widget-header-bg);color:var(--accent-color)}details{border:1px solid var(--border-color);border-radius:5px;padding:10px}summary{cursor:pointer;font-weight:600}.detail-body{padding-top:12px;display:flex;flex-direction:column;gap:10px}.empty{text-align:center;padding:28px;color:var(--footer-text)}.pager{justify-content:space-between}.footnote{font-size:.8em;color:var(--footer-text);line-height:1.4}
        @media(max-width:600px){.app{padding:10px}.toolbar>*{flex:1}.tabs button{flex:1;padding:7px 4px}.filters label{flex:1;min-width:100px}}
      </style><div class="app" aria-busy="${this.busy}">
        <header><div><h2>Public bus configuration</h2><p class="muted">Stops, routes and schedules · ${escape(this.status?.environment||'Connecting')}</p></div><button data-action="refresh" ${disabled}>Refresh</button></header>
        <div class="notice ${this.error?'error':''}" role="status" aria-live="polite">${escape(this.message)}</div>
        <div class="toolbar"><label class="grow">Schedule version<select data-field="dataset" ${disabled}><option value="">Active schedule</option>${options}</select></label><button class="primary" data-action="activate" ${!manage||!selected||selected===state.active_dataset?'disabled':''}>Activate selected</button><button data-action="rollback" ${!manage||!state.previous_dataset?'disabled':''}>Roll back</button></div>
        ${this.importPanel(feeds,manage)}
        <div class="tabs" role="tablist" aria-label="Bus configuration tabs">${[['stops','Bus Stops'],['routes','Bus Routes'],['schedule','Schedule']].map(([id,label])=>`<button role="tab" id="tab-${id}" aria-controls="panel" aria-selected="${this.tab===id}" tabindex="${this.tab===id?'0':'-1'}" data-tab="${id}" ${disabled}>${label}</button>`).join('')}</div>
        <div class="filters"><label class="grow">Search<input data-field="query" value="${escape(this.filter.query)}" placeholder="Name or ID" ${disabled}></label>${this.tab!=='routes'?`<label>Route ID<input data-field="route_id" value="${escape(this.filter.route_id)}" ${disabled}></label>`:''}${this.tab==='schedule'?`<label>Service date<input type="date" data-field="date" value="${escape(this.filter.date)}" ${disabled}></label><label>Stop ID<input data-field="stop_id" value="${escape(this.filter.stop_id)}" ${disabled}></label>`:''}<button data-action="search" ${disabled}>Apply filters</button></div>
        <div class="table-wrap" id="panel" role="tabpanel" aria-labelledby="tab-${this.tab}">${this.table(manage)}</div>
        <div class="pager"><span class="muted">${this.total?this.offset+1:0}–${Math.min(this.offset+this.rows.length,this.total)} of ${this.total}</span><div><button data-action="previous" ${this.offset===0||this.busy?'disabled':''}>Previous</button> <button data-action="next" ${this.offset+50>=this.total||this.busy?'disabled':''}>Next</button></div></div>
        ${this.sourcePanel(manage)}
        ${feeds.some(f=>f.id==='translink')?'<p class="footnote">Route and arrival data used in this product or service is provided by permission of TransLink. TransLink assumes no responsibility for the accuracy or currency of the Data used in this product or service.</p>':''}
      </div>`;
      this.bind();
    }
    importPanel(feeds,manage) {
      const jobs=this.status?.jobs||[]; const selected=(this.status?.datasets||[]).find(d=>d.id===(this.dataset||this.status?.state?.active_dataset));
      return `<details><summary>Import and validation ${jobs.some(j=>['queued','running'].includes(j.status))?'· Import in progress':''}</summary><div class="detail-body"><div class="toolbar"><label class="grow">Configured feed<select data-import="source_id"><option value="">Choose a feed</option>${feeds.map(f=>`<option value="${escape(f.id)}">${escape(f.name)}</option>`).join('')}</select></label><label class="grow">Or staged ZIP filename<input data-import="archive" placeholder="operator-schedule.zip"></label><label class="grow">Route IDs (optional)<input data-import="route_ids" placeholder="Comma-separated; blank imports all bus routes"></label><button class="primary" data-action="import" ${!manage?'disabled':''}>Download / import</button></div><p class="muted">The application fetches configured feeds directly. Local ZIPs must already be in its import directory. Imports are staged for review.</p>${selected?`<p>${selected.routes} routes · ${selected.stops} stops · ${selected.trips} trips</p>${(selected.warnings||[]).map(w=>`<p class="muted">${escape(w)}</p>`).join('')}`:''}${jobs.slice(0,5).map(j=>`<p><span class="badge">${escape(j.status)}</span> ${escape(j.dataset_id?.slice(0,8)||j.id.slice(0,8))} ${escape(j.error||'')}</p>`).join('')}</div></details>`;
    }
    table(manage) {
      if(!this.rows.length) return '<div class="empty">No records match. Import a schedule or adjust the filters.</div>';
      if(this.tab==='schedule') return `<table><thead><tr><th>Route / Trip</th><th>Destination</th><th>Stop</th><th>Arrival</th><th>Departure</th><th>Service</th></tr></thead><tbody>${this.rows.map(r=>`<tr><td>${escape(r.route_id)}<br><small>${escape(r.trip_id)}</small></td><td>${escape(r.headsign)}</td><td>${escape(r.sequence)} · ${escape(r.stop_name)}<br><small>${escape(r.stop_id)}</small></td><td>${escape(r.arrival)||'—'}</td><td>${escape(r.departure)||'—'}</td><td>${escape(r.service_id)}${r.pickup?' · No regular pickup':''}</td></tr>`).join('')}</tbody></table>`;
      return `<table><thead><tr><th>${this.tab==='stops'?'Stop':'Route'}</th><th>${this.tab==='stops'?'Location / Routes':'Type'}</th><th>Display name override</th><th>Enabled</th></tr></thead><tbody>${this.rows.map((r,i)=>`<tr><td>${escape(r.name)}<br><small>${escape(r.code||r.short_name||'')} · ${escape(r.id)}</small></td><td>${this.tab==='stops'?`${Number(r.lat).toFixed(5)}, ${Number(r.lon).toFixed(5)}<br><small>${escape((r.routes||[]).join(', '))}</small>`:escape(r.type)}</td><td><input aria-label="Display name for ${escape(r.name)}" data-name="${i}" value="${escape(r.display_name)}" ${!manage?'disabled':''}><button data-save="${i}" ${!manage?'disabled':''}>Save</button></td><td><input aria-label="Enable ${escape(r.name)}" type="checkbox" data-toggle="${i}" ${r.enabled?'checked':''} ${!manage?'disabled':''}></td></tr>`).join('')}</tbody></table>`;
    }
    sourcePanel(manage) {
      return `<details><summary>Data sources and phone assignments</summary><div class="detail-body">${(this.status?.sources||[]).map(s=>`<div>${escape(s.id)} <span class="badge">${escape(s.kind)} · ${escape(s.purpose)}</span> ${s.enabled?'Enabled':'Disabled'} <button data-source="${escape(s.id)}" ${!manage?'disabled':''}>${s.enabled?'Disable':'Enable'}</button></div>`).join('')||'<p class="muted">No sources enrolled. Configure the application bootstrap file.</p>'}<div class="toolbar">${[['id','Assignment ID'],['reporter_id','Reporter ID'],['vehicle_id','Vehicle ID'],['trip_id','Trip ID']].map(([k,label])=>`<label>${label}<input data-assignment="${k}"></label>`).join('')}<label>Service date<input type="date" data-assignment="service_date"></label><label>Valid from<input type="datetime-local" data-assignment="valid_from"></label><label>Valid to<input type="datetime-local" data-assignment="valid_to"></label><button data-action="assignment" ${!manage?'disabled':''}>Save assignment</button></div>${(this.status?.assignments||[]).map(a=>`<p>${escape(a.vehicle_id)} · ${escape(a.reporter_id)} → ${escape(a.trip?.trip_id)} <small>${escape(a.trip?.service_date)}</small></p>`).join('')}</div></details>`;
    }
    bind() {
      const root=this.shadowRoot;
      root.querySelectorAll('[data-tab]').forEach(b=>{b.onclick=()=>{this.tab=b.dataset.tab;this.offset=0;this.refresh();};b.onkeydown=e=>{if(!['ArrowLeft','ArrowRight'].includes(e.key)) return;e.preventDefault();const list=['stops','routes','schedule'];this.tab=list[(list.indexOf(this.tab)+(e.key==='ArrowRight'?1:2))%3];this.offset=0;this.refresh().then(()=>this.shadowRoot.querySelector(`[data-tab="${this.tab}"]`)?.focus());};});
      root.querySelector('[data-field="dataset"]').onchange=e=>{this.dataset=e.target.value;this.offset=0;this.refresh();};
      root.querySelectorAll('[data-action]').forEach(b=>b.onclick=()=>this.action(b.dataset.action));
      root.querySelectorAll('[data-save]').forEach(b=>b.onclick=()=>this.override(Number(b.dataset.save)));
      root.querySelectorAll('[data-toggle]').forEach(b=>b.onchange=()=>this.override(Number(b.dataset.toggle)));
      root.querySelectorAll('[data-source]').forEach(b=>b.onclick=()=>{const s=this.status.sources.find(s=>s.id===b.dataset.source);this.mutate('set_source',{...s,enabled:!s.enabled});});
      root.querySelectorAll('.filters input').forEach(i=>i.onkeydown=e=>{if(e.key==='Enter') this.action('search');});
    }
    override(index) {const row=this.rows[index];this.mutate('set_override',{kind:this.tab==='stops'?'stop':'route',id:row.id,name:this.shadowRoot.querySelector(`[data-name="${index}"]`).value,enabled:this.shadowRoot.querySelector(`[data-toggle="${index}"]`).checked});}
    action(name) {
      if(name==='refresh')return this.refresh();if(name==='previous'){this.offset=Math.max(0,this.offset-50);return this.refresh();}if(name==='next'){this.offset+=50;return this.refresh();}
      if(name==='search'){this.shadowRoot.querySelectorAll('.filters [data-field]').forEach(i=>this.filter[i.dataset.field]=i.value.trim());this.offset=0;return this.refresh();}
      if(name==='activate')return this.mutate('activate',{dataset_id:this.dataset||this.status?.state?.active_dataset});if(name==='rollback')return this.mutate('rollback',{});
      if(name==='import'){const payload={};this.shadowRoot.querySelectorAll('[data-import]').forEach(i=>payload[i.dataset.import]=i.value.trim());payload.route_ids=payload.route_ids.split(',').map(s=>s.trim()).filter(Boolean);return this.mutate('import_start',payload);}
      if(name==='assignment'){try{const fields={};this.shadowRoot.querySelectorAll('[data-assignment]').forEach(i=>fields[i.dataset.assignment]=i.value.trim());return this.mutate('set_assignment',{id:fields.id,reporter_id:fields.reporter_id,vehicle_id:fields.vehicle_id,trip:{trip_id:fields.trip_id,service_date:fields.service_date},valid_from:new Date(fields.valid_from).toISOString(),valid_to:new Date(fields.valid_to).toISOString()});}catch{this.message='Enter valid assignment dates and times.';this.error=true;this.render();}}
    }
  }
  window.XACT.registerWidget({type:'public-bus-config',name:'Public Bus Configuration',icon:'bus',defaultW:18,defaultH:18,minW:6,minH:8},PublicBusConfig);
})();
