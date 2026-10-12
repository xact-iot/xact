import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { Window } from 'happy-dom';

const widgetFile = fileURLToPath(new URL('../../plugins/widgets/public-bus-config.js', import.meta.url));

test('manual bus editors capture stop location, route path, and schedule fields', async () => {
  const window = new Window({ url: 'http://localhost' });
  let Widget;
  let map;
  let stopMarker;
  const calls = [];
  let areaRequestFails = false;
  const layer = () => ({
    addTo() { return this; },
    on() { return this; },
    remove() {},
    setLatLng() { return this; },
    getLatLng() { return { lat: 15.4, lng: -61.5 }; },
    setLatLngs() { return this; },
  });
  window.L = {
    map() {
      map = {
        handlers: {},
        attributionControl: { setPrefix(prefix) { map.attributionPrefix = prefix; } },
        on(name, handler) { this.handlers[name] = handler; return this; },
        setView() { return this; },
        fitBounds(bounds, options) { this.bounds = bounds; this.fitOptions = options; return this; },
        invalidateSize() {},
        remove() {},
        panTo() {},
      };
      return map;
    },
    tileLayer: layer,
    marker: layer,
    polyline: layer,
    circleMarker() { stopMarker=layer();stopMarker.handlers={};stopMarker.bindTooltip=()=>stopMarker;stopMarker.on=(name,handler)=>{stopMarker.handlers[name]=handler;return stopMarker};return stopMarker; },
    DomEvent: { stopPropagation() {} },
    latLng: (lat, lng) => ({ lat, lng }),
  };
  window.XACT = {
    registerWidget: (_meta, klass) => { Widget = klass; },
    loadLeaflet: async () => {},
    getOrganisationArea: async () => {
      if (areaRequestFails) throw new Error('Organisation API unavailable');
      return { north: 16, south: 15.5, east: -60.9, west: -61.6 };
    },
  };
  window.eval(readFileSync(widgetFile, 'utf8'));
  window.customElements.define('public-bus-config', Widget);
  const widget = window.document.createElement('public-bus-config');
  widget.permissions = { read: true, manage: true };
  widget.status = { active: false, state: { revision: 0 }, datasets: [], sources: [], feeds: [], jobs: [], assignments: [], area: { north: 15.7, south: 15.1, east: -61.1, west: -61.8 } };
  widget.request = async (operation, payload) => { calls.push({ operation, payload }); return operation==='list_stops'?{rows:[{id:'stop-1',name:'Market',lat:15.3,lon:-61.4}]}:{}; };
  widget.refresh = async () => {};
  widget.render();
  try {
    widget.openEditor();
    await new Promise(resolve => setTimeout(resolve, 30));
    assert.equal(JSON.stringify(map.bounds), JSON.stringify([[15.5, -61.6], [16, -60.9]]));
    assert.equal(JSON.stringify(map.fitOptions), JSON.stringify({ padding: [20, 20] }));
    assert.deepEqual([...widget.editorPortal.querySelectorAll('.bus-coordinates input')].map(input => input.name), ['lat', 'lon']);
    assert.equal(widget.editorPortal.querySelector('.bus-field-wide input').name, 'code');
    assert.equal(widget.editorPortal.querySelector('label.bus-checkbox input').name, 'enabled');
    assert.match(widget.editorPortal.querySelector('style').textContent, /label\.bus-checkbox\{flex-direction:row/);
    assert.equal(map.attributionPrefix, false);
    assert.match(widget.editorPortal.querySelector('style').textContent, /top:50%;left:50%/);
    widget.editorPortal.querySelector('[name=id]').value = 'stop-1';
    widget.editorPortal.querySelector('[name=name]').value = 'Market';
    map.handlers.click({ latlng: { lat: 15.3, lng: -61.4 } });
    await widget.saveEditor();
    assert.equal(calls[0].operation, 'create_stop');
    assert.equal(calls[0].payload.record.lat, 15.3);

    areaRequestFails = true;
    widget.tab = 'routes';
    widget.render();
    widget.openEditor();
    await new Promise(resolve => setTimeout(resolve, 30));
    assert.equal(JSON.stringify(map.bounds), JSON.stringify([[15.1, -61.8], [15.7, -61.1]]));
    widget.editorPortal.querySelector('[name=id]').value = 'route-1';
    widget.editorPortal.querySelector('[name=name]').value = 'Loop';
    stopMarker.handlers.click({});
    map.handlers.click({ latlng: { lat: 15.4, lng: -61.5 } });
    await widget.saveEditor();
    assert.equal(calls.find(call=>call.operation==='create_route').operation, 'create_route');
    assert.equal(JSON.stringify(calls.find(call=>call.operation==='create_route').payload.record.path), '[[15.3,-61.4],[15.4,-61.5]]');

    widget.action('schedule-dialog');
    assert.match(widget.shadowRoot.querySelector('style').textContent, /dialog\{position:fixed;top:50%;left:50%/);
    widget.closeModal();
    widget.tab = 'schedule';
    widget.render();
    widget.openEditor();
    const form = widget.editorPortal.querySelector('form');
    for (const [name, value] of Object.entries({
      route_id: 'route-1', trip_id: 'trip-1', stop_id: 'stop-1',
      service_date: '2026-09-29', sequence: '1', arrival: '08:00:00', departure: '08:01:00',
    })) form.querySelector('[name=' + name + ']').value = value;
    await widget.saveEditor();
    assert.equal(calls.find(call=>call.operation==='create_schedule').operation, 'create_schedule');
    assert.equal(calls.find(call=>call.operation==='create_schedule').payload.record.service_date, '2026-09-29');
  } finally {
    widget.closeEditor();
    await window.happyDOM.abort();
  }
});

test('staged GTFS import sends the archive ID to the database-backed app', async () => {
  const window = new Window({ url: 'http://localhost' });
  let Widget;
  window.XACT = { registerWidget: (_meta, klass) => { Widget = klass; } };
  window.eval(readFileSync(widgetFile, 'utf8'));
  window.customElements.define('public-bus-config', Widget);
  const widget = window.document.createElement('public-bus-config');
  widget.permissions = { read: true, manage: true };
  widget.status = { active: false, state: { revision: 0 }, datasets: [], sources: [], feeds: [], jobs: [], assignments: [] };
  widget.render();
  let sent;
  widget.mutate = async (operation, payload) => { sent = { operation, payload }; };
  try {
    widget.action('import-dialog');
    widget.shadowRoot.querySelector('[data-import=archive]').value = 'vancouver_transit.zip';
    widget.shadowRoot.querySelector('[data-import=route_ids]').value = '1, 2';
    await widget.importFile();
    assert.equal(sent.operation, 'import_start');
    assert.equal(sent.payload.archive, 'vancouver_transit.zip');
    assert.equal(JSON.stringify(sent.payload.route_ids), '["1","2"]');
  } finally {
    await window.happyDOM.abort();
  }
});


test('local GTFS ZIP uploads and imports into the database-backed app', async () => {
  const window = new Window({ url: 'http://localhost' });
  let Widget;
  let uploaded;
  window.XACT = {
    registerWidget: (_meta, klass) => { Widget = klass; },
    applications: { upload: async (_app, file, options) => {
      uploaded = { name: file.name, scope: options.scope };
      return { archive: 'staged.zip' };
    } },
  };
  window.eval(readFileSync(widgetFile, 'utf8'));
  window.customElements.define('public-bus-config', Widget);
  const widget = window.document.createElement('public-bus-config');
  widget.permissions = { read: true, manage: true };
  widget.status = { active: false, state: { revision: 0 }, datasets: [], sources: [], feeds: [], jobs: [], assignments: [] };
  widget.render();
  let sent;
  widget.mutate = async (operation, payload) => { sent = { operation, payload }; };
  try {
    widget.action('import-dialog');
    const input = widget.shadowRoot.querySelector('[data-import=file]');
    Object.defineProperty(input, 'files', { value: [new window.File(['ZIP'], 'vancouver_transit.zip', { type: 'application/zip' })] });
    await widget.importFile();
    assert.equal(uploaded.name, 'vancouver_transit.zip');
    assert.equal(uploaded.scope, 'default');
    assert.equal(sent.operation, 'import_start');
    assert.equal(sent.payload.archive, 'staged.zip');
  } finally {
    await window.happyDOM.abort();
  }
});

test('phone assignment selects a concrete scheduled trip and shows lifecycle outcomes', async()=>{
 const window=new Window({url:'http://localhost'});let Widget;
 window.XACT={registerWidget:(_meta,klass)=>{Widget=klass;}};
 window.eval(readFileSync(widgetFile,'utf8'));window.customElements.define('public-bus-config',Widget);
 const widget=window.document.createElement('public-bus-config');widget.tab='sources';widget.permissions={read:true,manage:true};
 widget.status={active:true,state:{revision:1},service_date:'2026-10-03',reporters:[{id:'driver',vehicle_id:'bus1',enabled:true}],sources:[],assignments:[],lifecycle:[{vehicle_id:'bus1',trip:{trip_id:'t1'},state:'inactive',reason:'inactivity',observed_at:'2026-10-03T10:00:00Z',confidence:'unavailable'}]};
 const trip={trip_id:'t1',route_id:'r1',service_date:'2026-10-03',start_time:'10:00:00'};const calls=[];
 widget.request=async(op,payload)=>{calls.push({op,payload});if(op==='list_routes')return{rows:[{id:'r1',name:'Harbour',short_name:'1'}],total:1};if(op==='list_trip_instances')return{rows:[{trip,departure:'10:00:00',destination:'Terminal',departure_at:1791021600000,end_at:1791022800000}],total:1};return{};};
 await widget.loadAssignmentChoices();widget.render();
 const reporter=widget.shadowRoot.querySelector('[data-assignment=reporter_id]');reporter.value='driver';await widget.assignmentChanged(reporter);
 const selected=widget.shadowRoot.querySelector('[data-assignment=trip_key]');selected.value='t1/2026-10-03/10:00:00';await widget.assignmentChanged(selected);
 widget.assignmentDraft.id='a1';let saved;widget.mutate=async(op,payload)=>{saved={op,payload};};widget.action('assignment');
 assert.equal(saved.op,'set_assignment');assert.equal(saved.payload.vehicle_id,'bus1');assert.equal(JSON.stringify(saved.payload.trip),JSON.stringify(trip));assert.ok(saved.payload.valid_from<saved.payload.valid_to);
 assert.match(widget.shadowRoot.textContent,/inactive inactivity/);assert.equal(calls.find(c=>c.op==='list_trip_instances').payload.route_id,'r1');
 window.close();
});
