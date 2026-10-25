/* Traffic Simulation Studio widget for XACT (docs/feature-specification.md §5.15).
 * Plain JavaScript custom element. Talks to the traffic-studio service through
 * XACT application requests (NATS), the XACT application proxy (REST) and a
 * live WebSocket that bypasses XACT's NATS and RTDB (§4.7). */
(function () {
  'use strict';
  const APP = 'traffic';
  const VEHICLE_ZOOM = 15, DETAIL_ZOOM = 17;
  const SPEEDS = [0.25, 0.5, 1, 2, 5, 10, 20, 0];
  const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const clock = s => { s = Math.max(0, Math.floor(s)) % 86400; return [s / 3600 | 0, (s % 3600) / 60 | 0, s % 60].map(n => String(n).padStart(2, '0')).join(':'); };
  const fmt = (v, d = 0) => Number.isFinite(v) ? Number(v).toLocaleString(undefined, { maximumFractionDigits: d, minimumFractionDigits: d }) : '–';
  const speedLabel = s => s > 0 ? `${s}×` : 'Max';
  const CLASS_COLORS = { passenger: '#3b82f6', taxi: '#eab308', delivery: '#94a3b8', truck: '#ea580c', bus: '#16a34a', motorcycle: '#a855f7', emergency: '#ef4444' };
  const SIGNAL_COLORS = { G: '#16a34a', g: '#4ade80', y: '#f59e0b', r: '#dc2626', u: '#f97316', s: '#dc2626', o: '#9ca3af', O: '#6b7280' };

  // ---------- Service access ----------
  class Api {
    constructor(host) { this.host = host; }
    get scope() { return this.host.config.scope || 'default'; }
    async session() {
      if (!window.XACT?.applications) throw new Error('XACT application integration is not installed.');
      return window.XACT.applications.session(APP);
    }
    async call(op, payload = {}, requestId) {
      if (!window.XACT?.applications) throw new Error('XACT application integration is not installed.');
      const res = await window.XACT.applications.request(APP, op, payload, { scope: this.scope, requestId, timeoutMs: 25000 });
      return res.data;
    }
    token() {
      try {
        const embedded = new URLSearchParams(location.search).get('embedded') === 'dashboard';
        return (embedded ? sessionStorage : localStorage).getItem('xact_auth_token');
      } catch { return null; }
    }
    async rest(path, init = {}) {
      let res;
      if (typeof window.XACT?.applications?.fetch === 'function') res = await window.XACT.applications.fetch(APP, path, init);
      else {
        const headers = new Headers(init.headers || {});
        const tok = this.token();
        if (tok) headers.set('Authorization', 'Bearer ' + tok);
        res = await fetch('/xact/traffic' + path, { ...init, headers, cache: 'no-store' });
      }
      if (!res.ok) {
        let msg = `Request failed (${res.status})`;
        // Service errors are {error:{message}}; XACT proxy errors are {error, message}.
        try { const j = await res.json(); msg = j?.error?.message || j?.message || msg; } catch { /* not JSON */ }
        throw new Error(msg);
      }
      return res;
    }
    wsURL(info) {
      const q = '?ticket=' + encodeURIComponent(info.ticket);
      if (info.url && /^wss?:/i.test(info.url)) return info.url + q;
      if (typeof window.XACT?.applications?.proxyUrl === 'function') return window.XACT.applications.proxyUrl(APP, (info.path || '/ws') + q);
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      return `${proto}//${location.host}${info.url && info.url.startsWith('/') ? info.url : '/xact/traffic/ws'}${q}`;
    }
  }

  // ---------- Live stream client with interpolation (UI-07, UI-25) ----------
  class LiveClient {
    constructor(owner) {
      this.owner = owner; this.ws = null; this.types = []; this.names = new Map(); this.vehicles = new Map();
      this.signals = {}; this.links = new Map(); this.status = null; this.closed = false; this.retry = 0;
      this.interval = 250; this.lastFrameAt = 0; this.origin = [0, 0];
    }
    async connect(runId) {
      this.runId = runId; this.closed = false;
      let info;
      try { info = await this.owner.api.call('open_stream', { run_id: runId }); }
      catch (e) { this.owner.setConn('error', e.message); this.schedule(); return; }
      if (this.closed) return;
      const ws = new WebSocket(this.owner.api.wsURL(info));
      this.ws = ws;
      this.owner.setConn('connecting');
      ws.onopen = () => { this.retry = 0; this.owner.setConn('live'); this.sendViewport(true); };
      ws.onmessage = e => { try { this.onMessage(JSON.parse(e.data)); } catch (err) { console.warn('traffic-studio: bad message', err); } };
      ws.onclose = ev => {
        if (this.ws !== ws) return;
        this.ws = null;
        if (this.closed) return;
        if (ev.code === 4001) { this.owner.setConn('error', 'Session expired; sign in again.'); return; }
        this.owner.setConn('reconnecting');
        this.schedule();
      };
    }
    schedule() {
      if (this.closed) return;
      const delay = Math.min(15000, 500 * 2 ** this.retry++);
      clearTimeout(this.timer);
      this.timer = setTimeout(() => { if (!this.closed && this.runId) this.connect(this.runId); }, delay);
    }
    close() { this.closed = true; clearTimeout(this.timer); if (this.ws) { const w = this.ws; this.ws = null; w.close(); } this.vehicles.clear(); this.links.clear(); this.signals = {}; }
    sendViewport(subscribe) {
      const map = this.owner.map;
      if (!this.ws || this.ws.readyState !== 1 || !map) return;
      // Following a vehicle pans every frame; send at most ~4 updates a second.
      const now = performance.now();
      if (!subscribe && now - (this.lastVP || 0) < 250) {
        clearTimeout(this.vpTimer);
        this.vpTimer = setTimeout(() => this.sendViewport(false), 260);
        return;
      }
      this.lastVP = now;
      const b = map.getBounds();
      this.ws.send(JSON.stringify({ t: subscribe ? 'subscribe' : 'viewport', vp: [b.getWest(), b.getSouth(), b.getEast(), b.getNorth()], z: map.getZoom(), hz: this.owner.streamHz || 4 }));
    }
    onMessage(m) {
      const now = performance.now();
      switch (m.t) {
        case 'types': this.types = m.types || []; break;
        case 'dict': for (const [k, id, t] of m.veh || []) this.names.set(k, { id, type: t }); break;
        case 'veh': {
          if (this.lastFrameAt) this.interval = Math.max(80, Math.min(1500, 0.8 * this.interval + 0.2 * (now - this.lastFrameAt)));
          this.lastFrameAt = now;
          if (m.key) {
            const keep = new Set(m.k || []);
            for (const k of this.vehicles.keys()) if (!keep.has(k)) this.vehicles.delete(k);
          }
          const o = m.o || this.origin; this.origin = o;
          const ks = m.k || [];
          for (let i = 0; i < ks.length; i++) {
            const lon = o[0] + m.x[i] / 1e6, lat = o[1] + m.y[i] / 1e6, h = m.h[i], s = m.s[i] / 10, f = m.f[i];
            const v = this.vehicles.get(ks[i]);
            if (!v) this.vehicles.set(ks[i], { pLon: lon, pLat: lat, pH: h, lon, lat, h, s, f, t0: now, t1: now });
            else {
              const cur = this.position(v, now);
              Object.assign(v, { pLon: cur.lon, pLat: cur.lat, pH: cur.h, lon, lat, h, s, f, t0: now, t1: now + this.interval });
            }
          }
          for (const k of m.rm || []) this.vehicles.delete(k);
          // Vehicles not mentioned in a delta keep moving to their last position.
          break;
        }
        case 'sig': Object.assign(this.signals, m.tl || {}); this.owner.onSignals(); break;
        case 'links': {
          this.links.clear();
          (m.e || []).forEach((e, i) => this.links.set(e, { r: m.r[i] / 100, n: m.n[i], q: m.q[i] }));
          break;
        }
        case 'status': this.status = m.status; this.owner.onStatus(m.status); break;
        case 'event': this.owner.onEvent(m.event); break;
      }
    }
    // Position at render time: interpolate between the previous and newest
    // frame, then extrapolate along the heading for at most one interval.
    position(v, now) {
      const span = Math.max(1, v.t1 - v.t0);
      let a = (now - v.t0) / span;
      if (a <= 1) {
        a = Math.max(0, a);
        let dh = ((v.h - v.pH + 540) % 360) - 180;
        return { lon: v.pLon + (v.lon - v.pLon) * a, lat: v.pLat + (v.lat - v.pLat) * a, h: (v.pH + dh * a + 360) % 360 };
      }
      const extra = Math.min(now - v.t1, this.interval) / 1000;
      const d = v.s * extra, rad = v.h * Math.PI / 180;
      const lat = v.lat + (d * Math.cos(rad)) / 111320;
      const lon = v.lon + (d * Math.sin(rad)) / (111320 * Math.cos(v.lat * Math.PI / 180));
      return { lon, lat, h: v.h };
    }
  }

  // ---------- The widget ----------
  class TrafficStudio extends HTMLElement {
    constructor() {
      super();
      this.attachShadow({ mode: 'open' });
      this.config = { scope: 'default' };
      this.api = new Api(this);
      this.permissions = {}; this.status = null; this.networks = []; this.network = null; this.detail = null;
      this.geometry = null; this.intersections = []; this.selected = null; this.live = null; this.runStatus = null;
      this.events = []; this.layers = { vehicles: true, signals: true, congestion: true, detectors: true, intersections: true };
      this.tool = null; this.drawn = null; this.importDraft = null; this.followKey = null; this.bottomTab = 'events';
      this.timers = []; this.raf = 0; this.conn = { state: 'idle', msg: '' }; this.service = { state: 'unknown', msg: 'Checking the traffic service…' }; this.message = ''; this.error = false;
    }
    static getPropertySchema() { return [{ name: 'scope', type: 'string', label: 'Application scope', default: 'default' }]; }
    setConfig(c) { this.config = { scope: 'default', ...c }; }
    getConfig() { return { ...this.config }; }

    connectedCallback() {
      if (this.started) { this.resume(); return; }
      this.started = true;
      this.renderShell();
      this.init();
      this.keyHandler = e => this.onKey(e);
      this.addEventListener('keydown', this.keyHandler);
      this.tabIndex = 0;
    }
    // Switching dashboard tabs detaches the widget. Keep the map and state,
    // pause timers, drawing and the live stream, and pick up again on return.
    disconnectedCallback() {
      this.saveView();
      this.timers.forEach(clearInterval); this.timers = [];
      cancelAnimationFrame(this.raf); this.raf = 0;
      clearTimeout(this.jobTimer);
      this.resumeRunId = this.live ? this.runId : null;
      if (this.live) { this.live.close(); this.live = null; }
    }
    resume() {
      if (!this.map) return;
      this.startTimers();
      this.startLoop();
      if (this.resumeRunId) { this.watchRun(this.resumeRunId); this.resumeRunId = null; }
      if (this.detail?.job && ['queued', 'running'].includes(this.detail.job.state)) this.watchJob(this.detail.job.id);
      this.fitPending = true;
      requestAnimationFrame(() => this.refit());
    }
    startTimers() {
      this.timers.forEach(clearInterval);
      this.timers = [
        setInterval(() => this.pollService(), 10000),
        setInterval(() => { if (this.service.state !== 'down') this.refreshNetworks(true); }, 15000),
      ];
    }
    startLoop() {
      cancelAnimationFrame(this.raf);
      const loop = () => { this.raf = requestAnimationFrame(loop); this.drawLive(); };
      loop();
    }
    get storageKey() { return 'traffic-studio:' + (this.config.scope || 'default'); }
    // Remember the open network and view for this browser tab, in case XACT
    // rebuilds the widget instead of re-attaching it.
    saveView() {
      if (!this.map) return;
      const size = this.map.getSize();
      if (size.x >= 50 && size.y >= 50) {
        const c = this.map.getCenter();
        this.savedView = { lat: c.lat, lng: c.lng, zoom: this.map.getZoom() };
      }
      try { sessionStorage.setItem(this.storageKey, JSON.stringify({ network: this.network?.id || null, view: this.savedView || null })); } catch { /* storage unavailable */ }
    }
    // Fit the map to what the user is working on: the drawn area, else the
    // open network, else the last view. A hidden map has no size, and fitting
    // it then shows the whole world, so wait until it is visible.
    refit() {
      if (!this.map) return;
      this.map.invalidateSize();
      const size = this.map.getSize();
      if (size.x < 50 || size.y < 50) { this.fitPending = true; return; }
      this.fitPending = false;
      this.sizeCanvas();
      const L = window.L;
      if (this.drawn?.ring) this.map.fitBounds(L.latLngBounds(this.drawn.ring.map(p => [p[1], p[0]])), { padding: [30, 30] });
      else if (this.geometry) { const b = this.geometry.bbox; this.map.fitBounds([[b[1], b[0]], [b[3], b[2]]], { padding: [20, 20] }); }
      else if (this.savedView) this.map.setView([this.savedView.lat, this.savedView.lng], this.savedView.zoom);
      else if (this.initialBounds) this.map.fitBounds(this.initialBounds);
      else this.map.setView([-33.93, 18.45], 12);
    }

    $(sel) { return this.shadowRoot.querySelector(sel); }
    can(p) { const r = { view: 1, operate: 2, engineer: 3, admin: 4 }; return Object.entries(this.permissions).some(([k, v]) => v && r[k] >= r[p]); }
    flash(msg, isError = false) {
      this.message = msg; this.error = isError;
      const el = this.$('.ts-msg');
      if (el) { el.textContent = msg; el.className = 'ts-msg' + (isError ? ' err' : ''); }
      clearTimeout(this.flashTimer);
      if (!isError) this.flashTimer = setTimeout(() => { if (this.message === msg) { this.message = ''; const e = this.$('.ts-msg'); if (e) e.textContent = ''; } }, 6000);
    }

    async init() {
      try {
        const sess = await this.api.session();
        this.permissions = sess.permissions || {};
        if (!this.can('view')) throw new Error('You need the view permission for Traffic Studio.');
        await this.initMap();
      } catch (e) {
        this.flash(e.message || String(e), true);
        this.renderAll();
        return;
      }
      // The service check runs now and every 10 s; the widget loads its data
      // once the service answers, and recovers on its own after an outage.
      await this.pollService();
      this.startTimers();
      // Reopen the network that was open before the widget was rebuilt.
      if (!this.network && this.restore?.network && this.networks.some(n => n.id === this.restore.network)) await this.openNetwork(this.restore.network);
      this.renderAll();
    }

    // Service availability indicator (shown in the menu bar).
    async pollService() {
      const before = this.service?.state;
      try {
        const st = await Promise.race([
          this.api.call('get_status'),
          new Promise((_, fail) => setTimeout(() => fail(new Error('no answer within 8 s')), 8000)),
        ]);
        this.status = st;
        this.streamHz = st.stream_hz || 4;
        this.permissions = st.permissions || this.permissions;
        const bad = Object.entries(st.checks || {}).filter(([, v]) => !String(v).startsWith('ok'));
        this.service = st.ready
          ? { state: 'ok', msg: `Traffic service ${st.service_version} connected · SUMO ${st.sumo_version}` }
          : { state: 'degraded', msg: 'Traffic service not ready: ' + bad.map(([k, v]) => `${k}: ${v}`).join('; ') };
      } catch (e) {
        this.service = { state: 'down', msg: 'Traffic service unreachable: ' + (e.message || e) };
      }
      this.service.checked = new Date();
      this.renderService();
      if (this.service.state !== 'down' && before !== 'ok' && before !== 'degraded') {
        // First contact, or back after an outage: (re)load everything.
        await this.refreshNetworks();
        if (this.status?.sessions?.length && !this.network) this.flash(`${this.status.sessions.length} simulation(s) running. Open a network to watch.`);
        this.renderAll();
      }
    }
    renderService() {
      const el = this.$('.ts-svc');
      if (!el) return;
      const s = this.service || { state: 'unknown', msg: 'Checking the traffic service…' };
      const label = { ok: 'Service connected', degraded: 'Service not ready', down: 'Service unreachable', unknown: 'Checking service…' }[s.state];
      el.className = 'ts-svc ' + s.state;
      el.title = s.msg + (s.checked ? ` (checked ${s.checked.toLocaleTimeString()})` : '');
      el.innerHTML = `<span class="ts-svc-dot"></span>${label}`;
    }

    // ---------- Map ----------
    adoptLeafletCSS() {
      if (this.$('style[data-leaflet]')) return;
      let css = '';
      for (const sheet of Array.from(document.styleSheets)) {
        let rules; try { rules = sheet.cssRules; } catch { continue; }
        for (const r of Array.from(rules || [])) if (r.cssText && r.cssText.includes('leaflet')) css += r.cssText + '\n';
      }
      const style = document.createElement('style');
      style.dataset.leaflet = '1';
      style.textContent = css || LEAFLET_FALLBACK_CSS;
      this.shadowRoot.prepend(style);
    }
    async initMap() {
      await window.XACT.loadLeaflet();
      this.adoptLeafletCSS();
      const L = window.L;
      const el = this.$('.ts-map');
      for (const ev of ['mousedown', 'pointerdown', 'touchstart', 'wheel']) el.addEventListener(ev, e => e.stopPropagation(), { passive: ev !== 'wheel' });
      // As in XACT's Area Map: no attribution control, a zoom level pill instead.
      const map = L.map(el, { zoomControl: true, preferCanvas: true, worldCopyJump: true, attributionControl: false });
      this.map = map;
      this.tiles = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', { maxZoom: 20, maxNativeZoom: 19 }).addTo(map);
      const zoomPill = document.createElement('div');
      zoomPill.className = 'ts-zoom';
      el.appendChild(zoomPill);
      const showZoom = () => { zoomPill.textContent = `Z ${map.getZoom()}`; };
      map.on('zoomend', showZoom);
      try { this.restore = JSON.parse(sessionStorage.getItem(this.storageKey) || 'null'); } catch { this.restore = null; }
      this.savedView = this.restore?.view || null;
      this.baseRenderer = L.canvas({ padding: 0.3 });
      this.networkLayer = L.layerGroup().addTo(map);
      this.junctionLayer = L.layerGroup().addTo(map);
      this.detectorLayer = L.layerGroup().addTo(map);
      this.drawLayer = L.layerGroup().addTo(map);
      // Live canvas above the map panes.
      const canvas = document.createElement('canvas');
      canvas.className = 'ts-live';
      el.appendChild(canvas);
      this.canvas = canvas;
      map.on('zoomstart', () => { canvas.style.opacity = '0'; });
      map.on('zoomend', () => { canvas.style.opacity = '1'; this.styleNetwork(); });
      map.on('moveend', () => { this.live?.sendViewport(false); this.updateZoomLayers(); this.saveView(); });
      map.on('click', e => this.onMapClick(e));
      map.on('mousemove', e => this.onMapMove(e));
      map.on('mousedown', e => this.onMapDown(e));
      map.on('mouseup', e => this.onMapUp(e));
      map.on('dblclick', e => { if (this.tool === 'polygon') { L.DomEvent.stop(e); this.finishPolygon(); } });
      let area = null;
      try { area = typeof window.XACT.getOrganisationArea === 'function' ? await window.XACT.getOrganisationArea() : null; } catch { area = null; }
      const ok = a => a && [a.north, a.south, a.east, a.west].every(Number.isFinite) && a.north > a.south && a.east > a.west;
      if (ok(area)) this.initialBounds = [[area.south, area.west], [area.north, area.east]];
      this.fitPending = true;
      requestAnimationFrame(() => { this.refit(); showZoom(); });
      this.resizeObs = new ResizeObserver(() => {
        map.invalidateSize(); this.sizeCanvas();
        if (this.fitPending) this.refit();
      });
      this.resizeObs.observe(el);
      this.sizeCanvas();
      this.startLoop();
    }
    sizeCanvas() {
      if (!this.canvas || !this.map) return;
      const s = this.map.getSize(), dpr = window.devicePixelRatio || 1;
      this.canvas.width = s.x * dpr; this.canvas.height = s.y * dpr;
      this.canvas.style.width = s.x + 'px'; this.canvas.style.height = s.y + 'px';
    }
    metersPerPixel(lat) { return 40075016.686 * Math.cos(lat * Math.PI / 180) / Math.pow(2, this.map.getZoom() + 8); }

    // ---------- Networks ----------
    async refreshNetworks(quiet) {
      try {
        const data = await this.api.call('list_networks');
        this.networks = data.networks || [];
        if (this.network) {
          const fresh = this.networks.find(n => n.id === this.network.id);
          if (!fresh) { this.closeNetwork(); }
          else if (fresh.revision !== this.network.revision || fresh.status !== this.network.status) await this.openNetwork(fresh.id, true);
          else if (quiet) await this.loadDetail(true);
        }
        this.renderTree();
      } catch (e) { if (!quiet) this.flash(e.message, true); }
    }
    async openNetwork(id, keepView) {
      const prev = this.network?.id;
      await this.loadDetail(false, id);
      if (!this.network) return;
      if (prev !== id) { this.selected = { kind: 'network' }; this.geometry = null; this.closeLive(); }
      if (this.network.status === 'ready' && (!this.geometry || prev !== id)) await this.loadGeometry(!keepView || prev !== id);
      if (this.network.status === 'importing' && this.detail.job) this.watchJob(this.detail.job.id);
      const active = (this.status?.sessions || []).concat(this.detail.runs || []).find(r => (r.network_id === id) && (r.state === 'running' || r.state === 'paused'));
      if (active && !this.live) this.watchRun(active.run_id || active.id);
      this.renderAll();
    }
    async loadDetail(quiet, id) {
      try {
        this.detail = await this.api.call('get_network', { id: id || this.network.id });
        this.network = this.detail.network;
        if (!quiet) this.renderAll(); else { this.renderTree(); this.renderInspector(); }
      } catch (e) { this.flash(e.message, true); }
    }
    closeNetwork() {
      this.closeLive(); this.network = null; this.detail = null; this.geometry = null; this.intersections = [];
      this.networkLayer?.clearLayers(); this.junctionLayer?.clearLayers(); this.detectorLayer?.clearLayers();
      this.selected = null; this.renderAll(); this.saveView();
    }
    async loadGeometry(fit) {
      try {
        const res = await this.api.rest(`/api/v1/networks/${this.network.id}/geometry`);
        this.geometry = await res.json();
        const ints = await this.api.call('list_intersections', { network_id: this.network.id });
        this.intersections = ints.intersections || [];
        this.drawNetwork();
        if (fit) this.refit();
        this.saveView();
      } catch (e) { this.flash('Could not load the network geometry: ' + e.message, true); }
    }
    drawNetwork() {
      const L = window.L, g = this.geometry;
      this.networkLayer.clearLayers(); this.junctionLayer.clearLayers(); this.detectorLayer.clearLayers();
      if (!g) return;
      this.edgeLines = g.edges.map((e, i) => {
        const line = L.polyline(e.shape.map(p => [p[1], p[0]]), { renderer: this.baseRenderer, color: '#64748b', weight: 2, opacity: 0.55, interactive: true });
        line.on('click', ev => { L.DomEvent.stopPropagation(ev); this.select({ kind: 'edge', index: i }); });
        line.addTo(this.networkLayer);
        return line;
      });
      this.junctionMarkers = new Map();
      for (const j of g.junctions || []) {
        const info = this.intersections.find(x => x.tl_id === j.tl);
        const m = L.circleMarker([j.lat, j.lon], { radius: 7, color: '#0f172a', weight: 1.5, fillColor: info && !info.controllable ? '#9ca3af' : '#22c55e', fillOpacity: 0.9 });
        m.bindTooltip(esc(j.name), { direction: 'top' });
        m.on('click', ev => { L.DomEvent.stopPropagation(ev); this.select({ kind: 'intersection', tl: j.tl }); });
        m.addTo(this.junctionLayer);
        this.junctionMarkers.set(j.tl, m);
      }
      for (const d of g.detectors || []) {
        const m = L.circleMarker([d.lat, d.lon], { radius: 3, color: d.purpose === 'stop_line' ? '#0ea5e9' : '#6366f1', weight: 1, fillOpacity: 0.7 });
        m.bindTooltip(`${esc(d.name)}<br>${d.type === 'e2' ? 'Stop-line detector' : 'Advance loop'}`);
        m.addTo(this.detectorLayer);
      }
      this.styleNetwork(); this.updateZoomLayers();
    }
    styleNetwork() {
      if (!this.edgeLines || !this.geometry || !this.map) return;
      const z = this.map.getZoom();
      const c = this.geometry.bbox, mpp = this.metersPerPixel((c[1] + c[3]) / 2);
      this.geometry.edges.forEach((e, i) => {
        const w = z >= DETAIL_ZOOM ? Math.max(3, (e.lanes * 3.2) / mpp) : Math.max(1.5, Math.min(6, e.lanes * (z >= 15 ? 1.6 : 1)));
        this.edgeLines[i].setStyle({ weight: w, opacity: z >= DETAIL_ZOOM ? 0.35 : 0.55, color: z >= DETAIL_ZOOM ? '#94a3b8' : '#64748b' });
      });
    }
    updateZoomLayers() {
      if (!this.map) return;
      const z = this.map.getZoom();
      const showDet = this.layers.detectors && z >= DETAIL_ZOOM, showJ = this.layers.intersections;
      if (showDet && !this.map.hasLayer(this.detectorLayer)) this.detectorLayer.addTo(this.map);
      if (!showDet && this.map.hasLayer(this.detectorLayer)) this.map.removeLayer(this.detectorLayer);
      if (showJ && !this.map.hasLayer(this.junctionLayer)) this.junctionLayer.addTo(this.map);
      if (!showJ && this.map.hasLayer(this.junctionLayer)) this.map.removeLayer(this.junctionLayer);
      // Close in, the signal heads show the state, so the junction marker
      // shrinks and fades to stay clickable without covering them.
      const close = z >= DETAIL_ZOOM;
      for (const m of this.junctionMarkers?.values() || []) m.setStyle({ radius: close ? 4 : 7, fillOpacity: close ? 0.35 : 0.9, opacity: close ? 0.5 : 1 });
    }
    onSignals() {
      if (!this.junctionMarkers || !this.live) return;
      for (const [tl, m] of this.junctionMarkers) {
        const s = this.live.signals[tl];
        if (!s) continue;
        m.setStyle({ fillColor: s.m === 'native' ? '#9ca3af' : s.m === 'fault' ? '#dc2626' : s.m === 'manual' ? '#f59e0b' : '#22c55e' });
      }
      if (this.selected?.kind === 'intersection') this.renderLiveSignal();
    }

    // ---------- Live drawing ----------
    drawLive() {
      const cv = this.canvas, map = this.map;
      if (!cv || !map) return;
      const ctx = cv.getContext('2d'), dpr = window.devicePixelRatio || 1;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, cv.width / dpr, cv.height / dpr);
      const lv = this.live;
      if (!lv || !this.geometry) { this.drawDrawing(ctx); return; }
      const z = map.getZoom(), now = performance.now();
      const pt = (lat, lon) => map.latLngToContainerPoint([lat, lon]);
      // Congestion colouring on edges with vehicles (UI-08).
      if (this.layers.congestion && lv.links.size) {
        ctx.lineCap = 'round'; ctx.lineJoin = 'round';
        for (const [ei, l] of lv.links) {
          const e = this.geometry.edges[ei];
          if (!e) continue;
          ctx.strokeStyle = l.r >= 0.7 ? 'rgba(34,197,94,.85)' : l.r >= 0.4 ? 'rgba(234,179,8,.9)' : l.r >= 0.2 ? 'rgba(249,115,22,.9)' : 'rgba(220,38,38,.95)';
          ctx.lineWidth = z >= DETAIL_ZOOM ? 3 : Math.max(2.5, Math.min(7, e.lanes * 1.8));
          ctx.globalAlpha = z >= VEHICLE_ZOOM ? 0.45 : 1;
          ctx.beginPath();
          e.shape.forEach((p, i) => { const q = pt(p[1], p[0]); i ? ctx.lineTo(q.x, q.y) : ctx.moveTo(q.x, q.y); });
          ctx.stroke();
        }
        ctx.globalAlpha = 1;
      }
      // Signal bars at stop lines (UI-09).
      if (this.layers.signals && z >= DETAIL_ZOOM) this.drawSignalBars(ctx, pt);
      // Vehicles (UI-07, UI-08).
      if (this.layers.vehicles && z >= VEHICLE_ZOOM && lv.vehicles.size) {
        const b = map.getBounds().pad(0.05);
        const detail = z >= DETAIL_ZOOM;
        const mpp = this.metersPerPixel(b.getCenter().lat);
        const flashOn = (now / 400 | 0) % 2 === 0;
        for (const [key, v] of lv.vehicles) {
          const p = lv.position(v, now);
          if (!b.contains([p.lat, p.lon])) continue;
          const q = pt(p.lat, p.lon);
          const info = lv.names.get(key), type = lv.types[info?.type ?? 0] || { class: 'passenger', length: 4.5, width: 1.8 };
          const color = (v.f & 4) ? (flashOn ? '#ef4444' : '#2563eb') : (CLASS_COLORS[type.class] || '#3b82f6');
          ctx.save();
          ctx.translate(q.x, q.y);
          ctx.rotate(p.h * Math.PI / 180);
          if (detail) {
            const L = Math.max(4, type.length / mpp), W = Math.max(2.5, type.width / mpp);
            ctx.fillStyle = color; ctx.strokeStyle = 'rgba(15,23,42,.8)'; ctx.lineWidth = 1;
            roundRect(ctx, -W / 2, -L / 2, W, L, Math.min(W, L) * 0.25); ctx.fill(); ctx.stroke();
            ctx.fillStyle = 'rgba(15,23,42,.55)'; ctx.fillRect(-W / 2 + 1, -L / 2 + L * 0.12, W - 2, L * 0.18); // windscreen
            if (v.f & 1) { ctx.fillStyle = '#ff2d2d'; ctx.fillRect(-W / 2, L / 2 - 1.5, W, 1.5); }
          } else {
            ctx.fillStyle = color; ctx.strokeStyle = 'rgba(15,23,42,.7)'; ctx.lineWidth = 0.75;
            ctx.beginPath(); ctx.moveTo(0, -5); ctx.lineTo(3.5, 4); ctx.lineTo(-3.5, 4); ctx.closePath(); ctx.fill(); ctx.stroke();
          }
          if (this.followKey === key || (this.selected?.kind === 'vehicle' && this.selected.key === key)) {
            ctx.strokeStyle = '#f43f5e'; ctx.lineWidth = 2; ctx.beginPath(); ctx.arc(0, 0, detail ? 12 : 9, 0, Math.PI * 2); ctx.stroke();
          }
          ctx.restore();
          if (this.followKey === key && !this.dragging) this.followTo = [p.lat, p.lon];
        }
        if (this.followTo) { map.panTo(this.followTo, { animate: false }); this.followTo = null; }
      }
      this.drawDrawing(ctx);
    }
    drawSignalBars(ctx, pt) {
      const lv = this.live, sigs = this.geometry.signals || {};
      const mpp = this.metersPerPixel(this.map.getCenter().lat);
      for (const tl in sigs) {
        const st = lv.signals[tl]?.s;
        if (!st) continue;
        const byLane = new Map();
        for (const s of sigs[tl]) { const k = s.lane >= 0 ? s.lane : `${s.lon},${s.lat}`; (byLane.get(k) || byLane.set(k, []).get(k)).push(s); }
        for (const list of byLane.values()) {
          const s0 = list[0], q = pt(s0.lat, s0.lon);
          const w = Math.max(6, s0.w / mpp), seg = w / list.length;
          ctx.save(); ctx.translate(q.x, q.y); ctx.rotate(s0.h * Math.PI / 180);
          list.forEach((s, i) => { ctx.fillStyle = SIGNAL_COLORS[st[s.i]] || '#6b7280'; ctx.fillRect(-w / 2 + i * seg, -2, seg - 0.5, 4); });
          ctx.restore();
        }
      }
    }
    drawDrawing(ctx) {
      // Rubber band for polygon drawing.
      if (this.tool === 'polygon' && this.polyPoints?.length && this.mouseLatLng) {
        const a = this.map.latLngToContainerPoint(this.polyPoints[this.polyPoints.length - 1]);
        const b = this.map.latLngToContainerPoint(this.mouseLatLng);
        ctx.setLineDash([6, 4]); ctx.strokeStyle = '#2563eb'; ctx.lineWidth = 2;
        ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke(); ctx.setLineDash([]);
      }
    }

    // ---------- Map interaction: drawing and selection ----------
    setTool(tool) {
      this.tool = tool;
      this.polyPoints = []; this.rectStart = null;
      if (this.map) {
        if (tool) { this.map.dragging.disable(); this.map.doubleClickZoom.disable(); this.$('.ts-map').classList.add('drawing'); }
        else { this.map.dragging.enable(); this.map.doubleClickZoom.enable(); this.$('.ts-map').classList.remove('drawing'); }
      }
      this.renderToolbar();
    }
    onMapDown(e) {
      if (this.tool !== 'rect') return;
      this.rectStart = e.latlng;
      this.drawLayer.clearLayers();
      this.rectShape = window.L.rectangle([e.latlng, e.latlng], { color: '#2563eb', weight: 2, fillOpacity: 0.1 }).addTo(this.drawLayer);
    }
    onMapMove(e) {
      this.mouseLatLng = e.latlng;
      if (this.tool === 'rect' && this.rectStart && this.rectShape) {
        this.rectShape.setBounds([this.rectStart, e.latlng]);
        this.showArea(window.L.latLngBounds(this.rectStart, e.latlng));
      }
    }
    onMapUp(e) {
      if (this.tool !== 'rect' || !this.rectStart) return;
      const b = window.L.latLngBounds(this.rectStart, e.latlng);
      this.rectStart = null;
      if (Math.abs(b.getEast() - b.getWest()) < 1e-4 || Math.abs(b.getNorth() - b.getSouth()) < 1e-4) { this.flash('Drag to draw an area.'); return; }
      this.setDrawn({ type: 'bbox', coordinates: [[b.getWest(), b.getSouth()], [b.getEast(), b.getNorth()]] }, ringOfBounds(b));
    }
    onMapClick(e) {
      if (this.tool === 'polygon') {
        const pts = this.polyPoints;
        if (pts.length >= 3) {
          const first = this.map.latLngToContainerPoint(pts[0]), here = this.map.latLngToContainerPoint(e.latlng);
          if (first.distanceTo(here) < 10) { this.finishPolygon(); return; }
        }
        pts.push(e.latlng);
        this.drawLayer.clearLayers();
        window.L.polyline(pts, { color: '#2563eb', weight: 2 }).addTo(this.drawLayer);
        pts.forEach(p => window.L.circleMarker(p, { radius: 4, color: '#2563eb', fillOpacity: 1 }).addTo(this.drawLayer));
        if (pts.length >= 3) this.showArea(null, pts);
        return;
      }
      if (this.tool) return;
      // Select a vehicle by proximity.
      if (this.live && this.map.getZoom() >= VEHICLE_ZOOM) {
        const now = performance.now(), here = this.map.latLngToContainerPoint(e.latlng);
        let best = null, bestD = 12;
        for (const [k, v] of this.live.vehicles) {
          const p = this.live.position(v, now), q = this.map.latLngToContainerPoint([p.lat, p.lon]);
          const d = q.distanceTo(here);
          if (d < bestD) { best = k; bestD = d; }
        }
        if (best !== null) { this.select({ kind: 'vehicle', key: best }); return; }
      }
    }
    finishPolygon() {
      const pts = this.polyPoints;
      if (pts.length < 3) { this.flash('A polygon needs at least three points.'); return; }
      const ring = pts.map(p => [p.lng, p.lat]);
      this.setDrawn({ type: 'polygon', coordinates: ring }, ring);
    }
    setDrawn(area, ring) {
      const L = window.L;
      this.drawLayer.clearLayers();
      const closed = ring.length > 1 && ring[0][0] === ring[ring.length - 1][0] && ring[0][1] === ring[ring.length - 1][1];
      const pts = (closed ? ring.slice(0, -1) : ring).map(p => L.latLng(p[1], p[0]));
      const shape = L.polygon(pts, { color: '#2563eb', weight: 2, fillOpacity: 0.12 }).addTo(this.drawLayer);
      this.drawn = { area, km2: areaKm2(ring), ring: pts.map(p => [p.lng, p.lat]).concat([[pts[0].lng, pts[0].lat]]) };
      this.addHandles(area.type, pts, shape);
      this.setTool(null);
      this.renderImportPanel();
    }
    // Drag handles adjust the area after drawing: rectangle corners keep the
    // opposite corner fixed; polygon handles move one vertex.
    addHandles(type, pts, shape) {
      const L = window.L;
      const icon = L.divIcon({ className: 'ts-handle', iconSize: [12, 12] });
      const markers = pts.map(p => L.marker(p, { icon, draggable: true, keyboard: false, bubblingMouseEvents: false }).addTo(this.drawLayer));
      const update = final => {
        const ring = pts.map(p => [p.lng, p.lat]);
        ring.push(ring[0]);
        shape.setLatLngs(pts);
        this.drawn.ring = ring;
        this.drawn.km2 = areaKm2(ring);
        if (type === 'bbox') {
          const b = L.latLngBounds(pts);
          this.drawn.area = { type: 'bbox', coordinates: [[b.getWest(), b.getSouth()], [b.getEast(), b.getNorth()]] };
        } else this.drawn.area = { type: 'polygon', coordinates: ring.slice(0, -1) };
        const el = this.$('.ts-area-readout');
        if (el) el.textContent = `${fmt(this.drawn.km2, 2)} km²`;
        if (final) this.renderImportPanel();
      };
      markers.forEach((m, i) => {
        m.on('drag', () => {
          const p = m.getLatLng();
          if (type === 'bbox') {
            // Corners are SW, SE, NE, NW; the opposite corner stays put.
            const o = pts[(i + 2) % 4];
            const w = Math.min(p.lng, o.lng), e = Math.max(p.lng, o.lng), sLat = Math.min(p.lat, o.lat), n = Math.max(p.lat, o.lat);
            const corners = [[sLat, w], [sLat, e], [n, e], [n, w]];
            corners.forEach((c, j) => { pts[j] = L.latLng(c[0], c[1]); if (j !== i) markers[j].setLatLng(pts[j]); });
            pts[i] = p;
          } else pts[i] = p;
          update(false);
        });
        m.on('dragend', () => {
          if (type === 'bbox') { const b = L.latLngBounds(pts); [b.getSouthWest(), b.getSouthEast(), b.getNorthEast(), b.getNorthWest()].forEach((c, j) => { pts[j] = c; markers[j].setLatLng(c); }); }
          update(true);
        });
      });
    }
    showArea(bounds, pts) {
      const ring = pts ? pts.map(p => [p.lng, p.lat]) : ringOfBounds(bounds);
      const el = this.$('.ts-area-readout');
      if (el) el.textContent = `${fmt(areaKm2(ring), 2)} km²`;
    }

    select(sel) {
      this.selected = sel;
      if (sel.kind === 'vehicle' && this.followKey !== null && this.followKey !== sel.key) this.followKey = null;
      this.renderInspector();
      if (sel.kind === 'intersection') this.loadIntersection(sel.tl);
    }
    async loadIntersection(tl) {
      try {
        const data = await this.api.call('get_intersection', { network_id: this.network.id, id: tl });
        if (this.selected?.kind === 'intersection' && this.selected.tl === tl) { this.selected.data = data; this.renderInspector(); }
      } catch (e) { this.flash(e.message, true); }
    }

    // ---------- Import ----------
    startImport(source) {
      if (!this.can('engineer')) return;
      this.importDraft = { source, name: '', driving_side: 'auto', region: this.status?.default_region || 'za', upload: null };
      this.drawn = null; this.drawLayer.clearLayers();
      this.bottomTab = 'import';
      this.renderAll();
      if (source === 'overpass') { this.setTool('rect'); this.flash('Drag on the map to choose the area, or switch to polygon.'); }
    }
    cancelImportDraft() { this.importDraft = null; this.drawn = null; this.drawLayer.clearLayers(); this.setTool(null); this.renderAll(); }
    async uploadFile(file) {
      const max = (this.status?.max_upload_mb || 200) * 1024 * 1024;
      if (file.size > max) { this.flash(`The file is larger than ${this.status.max_upload_mb} MB.`, true); return; }
      this.flash('Uploading ' + file.name + '…');
      try {
        const res = await this.api.rest('/api/v1/uploads?filename=' + encodeURIComponent(file.name), { method: 'POST', body: file, headers: { 'Content-Type': 'application/octet-stream' } });
        const info = await res.json();
        this.importDraft.upload = info;
        if (!this.importDraft.name) this.importDraft.name = file.name.replace(/\.(osm|xml|gz)/gi, '').replace(/[._-]+/g, ' ').trim();
        const [w, s, e, n] = info.bounds;
        this.map.fitBounds([[s, w], [n, e]]);
        const ring = [[w, s], [e, s], [e, n], [w, n], [w, s]];
        this.setDrawn({ type: 'bbox', coordinates: [[w, s], [e, n]] }, ring);
        this.flash(`Uploaded ${fmt(info.bytes / 1e6, 1)} MB (${fmt(info.nodes)} nodes). Draw a smaller area to clip, or import it all.`);
      } catch (e) { this.flash(e.message, true); }
    }
    async submitImport() {
      const d = this.importDraft;
      const name = (this.$('[name=ts-net-name]')?.value || '').trim();
      d.name = name;
      d.driving_side = this.$('[name=ts-side]')?.value || 'auto';
      if (!name) { this.flash('Give the network a name.', true); return; }
      if (!this.drawn) { this.flash('Draw the area on the map first.', true); return; }
      if (d.source === 'upload' && !d.upload) { this.flash('Upload an OSM file first.', true); return; }
      const payload = { name, area: this.drawn.area, driving_side: d.driving_side, region: d.region, source: d.source };
      if (d.source === 'upload') payload.upload_id = d.upload.upload_id;
      try {
        const res = await this.api.call('import_network', payload, crypto.randomUUID());
        this.importDraft = null; this.drawn = null; this.drawLayer.clearLayers();
        this.flash(`Importing ${res.network.name}…`);
        await this.refreshNetworks();
        await this.openNetwork(res.network.id, true);
        this.bottomTab = 'import';
        this.watchJob(res.job.id);
      } catch (e) { this.flash(e.message, true); }
    }
    watchJob(id) {
      clearTimeout(this.jobTimer);
      const poll = async () => {
        try {
          const job = await this.api.call('get_job', { id });
          if (this.detail) this.detail.job = job;
          this.renderBottom();
          if (job.state === 'queued' || job.state === 'running') { this.jobTimer = setTimeout(poll, 1000); return; }
          if (job.state === 'completed') { this.flash('Import finished. The default scenario is ready to run.'); await this.refreshNetworks(); if (this.network) await this.openNetwork(this.network.id); }
          else this.flash(job.error || 'Import ' + job.state, true);
        } catch (e) { this.jobTimer = setTimeout(poll, 3000); }
      };
      poll();
    }

    // ---------- Simulation ----------
    get activeRun() { return this.runStatus && (this.runStatus.state === 'running' || this.runStatus.state === 'paused') ? this.runStatus : null; }
    scenarioId() { return this.$('[name=ts-scenario]')?.value || this.network?.default_scenario; }
    async startSim() {
      if (!this.can('operate') || !this.network || this.network.status !== 'ready') return;
      try {
        const speed = this.pendingSpeed ?? 1;
        const run = await this.api.call('start_session', { scenario_id: this.scenarioId(), speed }, crypto.randomUUID());
        this.flash('Simulation started.');
        this.events = [];
        this.watchRun(run.id);
        this.loadDetail(true);
      } catch (e) { this.flash(e.message, true); }
    }
    watchRun(runId) {
      this.closeLive();
      this.runId = runId;
      this.live = new LiveClient(this);
      this.live.connect(runId);
      this.api.call('get_run', { id: runId }).then(d => { this.events = (d.events || []).slice(-200); this.runStatus = d.live || this.runStatus; this.renderBottom(); this.renderStatusBar(); }).catch(() => {});
      this.renderAll();
    }
    closeLive() { if (this.live) { this.live.close(); this.live = null; } this.runStatus = null; this.followKey = null; this.setConn('idle'); }
    async control(action, speed) {
      const run = this.runId;
      if (!run || !this.can('operate')) return;
      try {
        const st = await this.api.call('control_session', { run_id: run, action, speed: speed ?? 0 });
        if (st && st.run_id) this.onStatus(st);
        if (action === 'stop') this.flash('Stopping and computing results…');
      } catch (e) { this.flash(e.message, true); }
    }
    togglePause() { const r = this.activeRun; if (!r) { this.startSim(); return; } this.control(r.state === 'paused' ? 'resume' : 'pause'); }
    changeSpeed(dir) {
      const cur = this.activeRun ? this.activeRun.speed : (this.pendingSpeed ?? 1);
      let i = SPEEDS.indexOf(cur); if (i < 0) i = 2;
      i = Math.max(0, Math.min(SPEEDS.length - 1, i + dir));
      this.setSpeed(SPEEDS[i]);
    }
    setSpeed(s) { if (this.activeRun) this.control('speed', s); else { this.pendingSpeed = s; this.renderToolbar(); } }
    onStatus(st) {
      const was = this.runStatus?.state;
      this.runStatus = st;
      this.renderStatusBar(); this.renderToolbar();
      if (st.state !== was) {
        if (['completed', 'failed', 'interrupted'].includes(st.state)) {
          this.flash(st.state === 'completed' ? 'Run finished. Results are in the run summary.' : `Run ${st.state}: ${st.error || ''}`, st.state !== 'completed');
          this.loadDetail(true).then(() => { this.selected = { kind: 'run', id: st.run_id }; this.renderInspector(); this.bottomTab = 'summary'; this.renderBottom(); });
        }
      }
    }
    onEvent(e) { this.events.push(e); if (this.events.length > 300) this.events.shift(); if (this.bottomTab === 'events') this.renderBottom(); }
    setConn(state, msg) { this.conn = { state, msg: msg || '' }; this.renderStatusBar(); }

    onKey(e) {
      // Inside a shadow root the event target is the host; check the real origin.
      if (e.composedPath()[0]?.matches?.('input,select,textarea')) return;
      switch (e.key) {
        case ' ': e.preventDefault(); this.togglePause(); break;
        case '.': this.control('step'); break;
        case '+': case '=': this.changeSpeed(1); break;
        case '-': this.changeSpeed(-1); break;
        case 'f': case 'F': if (this.selected?.kind === 'vehicle') { this.followKey = this.followKey === this.selected.key ? null : this.selected.key; this.renderInspector(); } break;
        case 'Escape': if (this.tool) this.setTool(null); else if (this.importDraft) this.cancelImportDraft(); break;
      }
    }

    // ---------- Rendering ----------
    renderShell() {
      this.shadowRoot.innerHTML = `<style>${STYLE}</style>
        <div class="ts-root">
          <nav class="ts-menubar" role="menubar"></nav>
          <div class="ts-main">
            <aside class="ts-left"><div class="ts-tree"></div><div class="ts-layers"></div></aside>
            <section class="ts-center"><div class="ts-toolbar"></div><div class="ts-map" tabindex="-1"></div></section>
            <aside class="ts-right"><div class="ts-inspector"></div></aside>
          </div>
          <section class="ts-bottom"></section>
          <footer class="ts-status"></footer>
          <div class="ts-msg" role="status" aria-live="polite"></div>
        </div>`;
      this.shadowRoot.addEventListener('click', e => this.onClick(e));
      // XACT's dashboard grid (GridStack) cancels mousedown inside widgets
      // unless the target is a form control, but from inside a shadow root it
      // only sees the host element, so inputs could not take focus and
      // dropdowns would not open. Keep pointer-down events inside the widget.
      const root = this.$('.ts-root');
      for (const ev of ['mousedown', 'pointerdown', 'touchstart']) root.addEventListener(ev, e => e.stopPropagation(), { passive: true });
      // Typing in fields must not reach dashboard or widget shortcuts.
      root.addEventListener('keydown', e => { if (e.composedPath()[0]?.matches?.('input,select,textarea')) e.stopPropagation(); });
      this.shadowRoot.addEventListener('change', e => this.onChange(e));
      this.shadowRoot.addEventListener('input', e => { if (e.target.name === 'ts-net-name' && this.importDraft) this.importDraft.name = e.target.value; });
    }
    renderAll() { this.renderMenu(); this.renderTree(); this.renderLayers(); this.renderToolbar(); this.renderInspector(); this.renderBottom(); this.renderStatusBar(); this.updateZoomLayers(); }

    renderMenu() {
      const eng = this.can('engineer'), op = this.can('operate'), admin = this.can('admin'), ready = this.network?.status === 'ready';
      const run = this.activeRun;
      const item = (act, label, enabled = true, key = '') => `<button class="ts-mi" data-act="${act}" ${enabled ? '' : 'disabled'}><span>${label}</span><kbd>${key}</kbd></button>`;
      const menus = [
        ['File', [eng && item('new-map', 'New network from map…'), eng && item('new-upload', 'Import OSM file…'),
          this.network && item('props', 'Network properties'), ready && item('dl-csv', 'Download signal audit (CSV)'),
          ready && item('dl-geojson', 'Download signal audit (GeoJSON)'), this.network && item('close', 'Close network'),
          admin && this.network && item('delete-net', 'Delete network…')]],
        ['View', [item('toggle-vehicles', (this.layers.vehicles ? '✓ ' : '') + 'Vehicles'), item('toggle-signals', (this.layers.signals ? '✓ ' : '') + 'Signals'),
          item('toggle-congestion', (this.layers.congestion ? '✓ ' : '') + 'Congestion'), item('toggle-detectors', (this.layers.detectors ? '✓ ' : '') + 'Detectors'),
          item('toggle-intersections', (this.layers.intersections ? '✓ ' : '') + 'Intersections'), this.geometry && item('fit', 'Fit network')]],
        ['Simulate', [item('start', 'Start', op && ready && !run, ''), item('pause', run?.state === 'paused' ? 'Resume' : 'Pause', op && !!run, 'Space'),
          item('step', 'Step', op && !!run, '.'), item('stop', 'Stop', op && !!run),
          ...SPEEDS.map(s => item('speed-' + s, `Speed ${speedLabel(s)}${(run ? run.speed : (this.pendingSpeed ?? 1)) === s ? ' ✓' : ''}`, op))]],
        ['Analyze', [item('summary', 'Run summary', !!this.detail?.runs?.length), item('health', 'Network health report', !!this.detail?.versions?.length)]],
        ['Help', [item('shortcuts', 'Keyboard shortcuts'), item('about', 'About')]],
      ];
      this.$('.ts-menubar').innerHTML = menus.map(([name, items]) => `<div class="ts-menu"><button class="ts-mtitle" aria-haspopup="true">${name}</button><div class="ts-mdrop" role="menu">${items.filter(Boolean).join('') || '<span class="ts-mempty">Nothing available</span>'}</div></div>`).join('')
        + `<div class="ts-mspacer"></div><span class="ts-svc" role="status"></span><span class="ts-brand">Traffic Studio</span>`;
      this.renderService();
    }

    renderTree() {
      const el = this.$('.ts-tree');
      if (!el) return;
      const nets = this.networks.map(n => {
        const open = this.network?.id === n.id;
        const badge = n.status === 'ready' ? '' : `<span class="ts-badge ${n.status}">${esc(n.status)}</span>`;
        let inner = '';
        if (open && this.detail) {
          const sc = (this.detail.scenarios || []).map(s => `<li class="${this.selected?.kind === 'scenario' && this.selected.id === s.id ? 'sel' : ''}"><button class="ts-link" data-act="sel-scenario" data-id="${s.id}">▸ ${esc(s.name)}</button></li>`).join('');
          const runs = (this.detail.runs || []).slice(0, 12).map(r => `<li class="${this.selected?.kind === 'run' && this.selected.id === r.id ? 'sel' : ''}"><button class="ts-link" data-act="sel-run" data-id="${r.id}"><span class="ts-dot ${r.state}"></span>${esc(new Date(r.created_at).toLocaleString())}</button></li>`).join('');
          inner = `<div class="ts-sub"><div class="ts-subh">Scenarios</div><ul>${sc || '<li class="muted">None</li>'}</ul>
            <div class="ts-subh">Runs</div><ul>${runs || '<li class="muted">No runs yet</li>'}</ul></div>`;
        }
        return `<li class="${open ? 'open' : ''}"><button class="ts-net" data-act="open-net" data-id="${n.id}">${esc(n.name)} ${badge}</button>${inner}</li>`;
      }).join('');
      el.innerHTML = `<div class="ts-panelh">Networks ${this.can('engineer') ? '<button class="ts-icon" data-act="new-map" title="New network from map">＋</button>' : ''}</div>
        <ul class="ts-nets">${nets || `<li class="muted ts-empty">No networks yet.${this.can('engineer') ? ' Use <b>File → New network from map</b>.' : ''}</li>`}</ul>`;
    }

    renderLayers() {
      const el = this.$('.ts-layers');
      if (!el) return;
      const row = (k, label) => `<label><input type="checkbox" data-layer="${k}" ${this.layers[k] ? 'checked' : ''}> ${label}</label>`;
      el.innerHTML = `<div class="ts-panelh">Layers</div>${row('vehicles', 'Vehicles (zoom 15+)')}${row('signals', 'Signal heads (zoom 17+)')}${row('congestion', 'Congestion')}${row('detectors', 'Detectors (zoom 17+)')}${row('intersections', 'Intersections')}
        <div class="ts-legend"><span style="--c:#22c55e"></span>free <span style="--c:#eab308"></span>slow <span style="--c:#f97316"></span>congested <span style="--c:#dc2626"></span>queued</div>`;
    }

    renderToolbar() {
      const el = this.$('.ts-toolbar');
      if (!el) return;
      const run = this.activeRun, op = this.can('operate'), ready = this.network?.status === 'ready';
      let html = '';
      if (this.tool || this.importDraft) {
        html = `<span class="ts-tool-label">${this.importDraft?.source === 'upload' ? 'Clip area (optional):' : 'Area:'}</span>
          <button class="${this.tool === 'rect' ? 'on' : ''}" data-act="tool-rect">▭ Rectangle</button>
          <button class="${this.tool === 'polygon' ? 'on' : ''}" data-act="tool-polygon">⬠ Polygon</button>
          ${this.tool === 'polygon' ? '<span class="muted">Click points; double-click or click the first point to finish.</span>' : ''}
          <span class="ts-area-readout">${this.drawn ? fmt(this.drawn.km2, 2) + ' km²' : ''}</span>`;
      } else if (this.network) {
        const scs = this.detail?.scenarios || [];
        const curSpeed = run ? run.speed : (this.pendingSpeed ?? 1);
        html = `<select name="ts-scenario" ${run ? 'disabled' : ''} title="Scenario">${scs.map(s => `<option value="${s.id}">${esc(s.name)}</option>`).join('')}</select>
          ${run ? `<button data-act="pause" ${op ? '' : 'disabled'} title="Pause or resume (Space)">${run.state === 'paused' ? '▶ Resume' : '❚❚ Pause'}</button>
                   <button data-act="step" ${op ? '' : 'disabled'} title="Step one control interval (.)">⏭ Step</button>
                   <button data-act="stop" ${op ? '' : 'disabled'} title="Stop and compute results">■ Stop</button>`
            : `<button class="primary" data-act="start" ${op && ready ? '' : 'disabled'} title="Start a simulation">▶ Start</button>`}
          <label class="ts-speed">Speed <select name="ts-speed" ${op ? '' : 'disabled'}>${SPEEDS.map(s => `<option value="${s}" ${s === curSpeed ? 'selected' : ''}>${speedLabel(s)}</option>`).join('')}</select></label>`;
      } else html = `<span class="muted">Open a network from the list${this.can('engineer') ? ', or create one with File → New network from map' : ''}.</span>`;
      el.innerHTML = html;
      const sel = this.$('[name=ts-scenario]');
      if (sel && this.runStatus?.scenario_id) sel.value = this.runStatus.scenario_id;
    }

    renderImportPanel() { this.renderToolbar(); if (this.bottomTab === 'import') this.renderBottom(); this.renderMenu(); }

    renderInspector() {
      const el = this.$('.ts-inspector');
      if (!el) return;
      const s = this.selected;
      let html = '';
      if (!this.network) html = `<div class="ts-panelh">Inspector</div><p class="muted">Select a network, an intersection, a road or a vehicle.</p>`;
      else if (!s || s.kind === 'network') html = this.networkInspector();
      else if (s.kind === 'intersection') html = this.intersectionInspector(s);
      else if (s.kind === 'edge') html = this.edgeInspector(s);
      else if (s.kind === 'vehicle') html = this.vehicleInspector(s);
      else if (s.kind === 'scenario') html = this.scenarioInspector(s);
      else if (s.kind === 'run') html = this.runInspector(s);
      el.innerHTML = html;
    }
    networkInspector() {
      const n = this.network, v = (this.detail?.versions || []).slice(-1)[0];
      const st = v?.stats;
      return `<div class="ts-panelh">${esc(n.name)}</div>
        <dl class="ts-kv"><dt>Status</dt><dd>${esc(n.status)}</dd><dt>Area</dt><dd>${fmt(n.area_km2, 2)} km²</dd>
        <dt>Driving side</dt><dd>${esc(n.driving_side)} (${esc(n.driving_side_source)}${n.country ? ', ' + esc(n.country) : ''})</dd>
        <dt>Region profile</dt><dd>${esc(n.region)}</dd><dt>RTDB name</dt><dd><code>TRAFFIC.${esc(n.device_name)}</code></dd>
        ${st ? `<dt>Roads</dt><dd>${fmt(st.edges)} edges, ${fmt(st.lane_km, 1)} lane-km</dd><dt>Signals</dt><dd>${st.intersections} intersections (${st.controllable} converted)</dd>
        <dt>Detectors</dt><dd>${fmt(st.detectors)}</dd><dt>Default demand</dt><dd>${fmt(st.routed_vehicles)} vehicles</dd><dt>SUMO</dt><dd>${esc(v.sumo_version)}</dd>` : ''}</dl>
        ${n.description ? `<p>${esc(n.description)}</p>` : ''}
        ${v ? `<p class="muted">Signals, detectors and demand were generated automatically and are unverified.</p>` : ''}
        <p class="ts-attr">Map data © OpenStreetMap contributors (ODbL)</p>`;
    }
    intersectionInspector(s) {
      const d = s.data, sum = this.intersections.find(x => x.tl_id === s.tl);
      if (!d) return `<div class="ts-panelh">${esc(sum?.name || 'Intersection')}</div><p class="muted">Loading…</p>`;
      const plan = d.plans?.[0];
      return `<div class="ts-panelh">${esc(d.name)}</div>
        <dl class="ts-kv"><dt>Signal ID</dt><dd><code>${esc(d.tl_id)}</code></dd><dt>RTDB name</dt><dd><code>${esc(d.device_name)}</code></dd>
        <dt>Control</dt><dd>${d.controllable ? 'Fixed time (virtual controller)' : 'SUMO original program'}</dd>
        <dt>Status</dt><dd>${d.verified ? 'Verified' : 'Auto-generated, unverified'}</dd></dl>
        ${d.conversion_error ? `<p class="warn">${esc(d.conversion_error)}</p>` : ''}
        <div class="ts-live-signal"></div>
        ${d.controllable ? `<div class="ts-subh">Stages (${esc(plan?.name || '')}, offset ${fmt(plan?.offset_s)} s)</div>
        <table class="ts-table"><tr><th>Stage</th><th>Groups</th><th>Green</th></tr>${(plan?.stages || []).map(ps => { const st = d.stages.find(x => x.id === ps.stage); return `<tr><td>${esc(ps.stage)}</td><td>${esc((st?.groups || []).join(', '))}</td><td>${fmt(ps.green_s)} s</td></tr>`; }).join('')}</table>
        <div class="ts-subh">Signal groups</div>
        <table class="ts-table"><tr><th>Group</th><th>Movements</th></tr>${d.signal_groups.map(g => `<tr><td>${esc(g.id)}</td><td>${g.links.map(i => { const l = d.links.find(x => x.index === i); return l ? esc(l.dir) : i; }).join(' ')}</td></tr>`).join('')}</table>` : ''}
        <div class="ts-subh">Detectors (${d.detectors.length})</div>
        <ul class="ts-list">${d.detectors.map(x => `<li>${x.type === 'e2' ? '▭' : '●'} ${esc(x.name)}</li>`).join('')}</ul>`;
    }
    renderLiveSignal() {
      const box = this.$('.ts-live-signal'), s = this.selected;
      if (!box || !s?.data || !this.live) return;
      const sig = this.live.signals[s.tl];
      if (!sig) { box.innerHTML = ''; return; }
      box.innerHTML = `<div class="ts-subh">Live</div><dl class="ts-kv"><dt>Stage</dt><dd>${esc(sig.st || '–')} ${sig.m === 'native' ? '(SUMO program)' : ''}</dd>
        <dt>Plan / cycle</dt><dd>${esc(sig.p || '–')} ${sig.c ? fmt(sig.c) + ' s' : ''}</dd>
        <dt>Aspects</dt><dd class="ts-aspects">${[...(sig.s || '')].map(c => `<span style="background:${SIGNAL_COLORS[c] || '#888'}" title="${c}"></span>`).join('')}</dd></dl>`;
    }
    edgeInspector(s) {
      const e = this.geometry?.edges[s.index];
      if (!e) return '';
      const l = this.live?.links.get(s.index);
      return `<div class="ts-panelh">${esc(e.name || 'Unnamed road')}</div>
        <dl class="ts-kv"><dt>Edge</dt><dd><code>${esc(e.id)}</code></dd><dt>Type</dt><dd>${esc(e.type || '–')}</dd><dt>Lanes</dt><dd>${e.lanes}</dd>
        <dt>Speed limit</dt><dd>${fmt(e.speed * 3.6)} km/h</dd>
        ${l ? `<dt>Vehicles</dt><dd>${l.n} (${l.q} queued)</dd><dt>Speed</dt><dd>${fmt(l.r * 100)}% of limit</dd>` : ''}</dl>`;
    }
    vehicleInspector(s) {
      const lv = this.live, v = lv?.vehicles.get(s.key), info = lv?.names.get(s.key);
      if (!v) return `<div class="ts-panelh">Vehicle</div><p class="muted">The vehicle has left the network or the view.</p>`;
      const type = lv.types[info?.type ?? 0];
      return `<div class="ts-panelh">Vehicle ${esc(info?.id || s.key)}</div>
        <dl class="ts-kv"><dt>Type</dt><dd>${esc(type?.id || '–')} (${esc(type?.class || '')})</dd><dt>Speed</dt><dd>${fmt(v.s * 3.6)} km/h</dd>
        <dt>Heading</dt><dd>${fmt(v.h)}°</dd><dt>State</dt><dd>${v.f & 2 ? 'stopped' : 'moving'}${v.f & 1 ? ', braking' : ''}</dd></dl>
        <button data-act="follow">${this.followKey === s.key ? 'Stop following' : 'Follow (F)'}</button>`;
    }
    scenarioInspector(s) {
      const sc = (this.detail?.scenarios || []).find(x => x.id === s.id);
      if (!sc) return '';
      const eng = this.can('engineer'), t = sc.timing, dis = eng ? '' : 'disabled';
      const num = (name, val, step, unit) => `<label class="ts-field"><span>${name}</span><input type="number" step="${step}" data-timing="${name}" value="${val}" ${dis}> ${unit}</label>`;
      return `<div class="ts-panelh">${esc(sc.name)}</div>
        <dl class="ts-kv"><dt>Window</dt><dd>${esc(sc.start_time)} for ${fmt(sc.duration_s / 60)} min, ${fmt(sc.warmup_s / 60)} min warm-up</dd>
        <dt>Day type</dt><dd>${esc(sc.day_type)}${sc.date ? ' (' + esc(sc.date) + ')' : ''}</dd><dt>Demand</dt><dd>${fmt(sc.demand.vehicles)} vehicles, ${esc(sc.demand.method)}, ${sc.demand.calibrated ? 'calibrated' : 'uncalibrated'}</dd>
        <dt>Revision</dt><dd>${sc.revision}</dd></dl>
        <form class="ts-form" data-scenario="${sc.id}" data-revision="${sc.revision}">
          <div class="ts-subh">Signal timing defaults</div>
          ${num('amber_s', t.amber_s, 0.5, 's')}${num('all_red_min_s', t.all_red_min_s, 0.5, 's')}${num('min_green_s', t.min_green_s, 1, 's')}
          ${num('ped_walk_min_s', t.ped_walk_min_s, 1, 's')}${num('ped_clearance_min_s', t.ped_clearance_min_s, 1, 's')}${num('ped_walking_speed_mps', t.ped_walking_speed_mps, 0.1, 'm/s')}
          <div class="ts-subh">Simulation</div>
          <label class="ts-field"><span>Seed</span><input type="number" name="seed" value="${sc.sim.seed}" ${dis}></label>
          <label class="ts-field"><span>Step length</span><input type="number" step="0.1" name="step" value="${sc.sim.step_length_s}" ${dis}> s</label>
          ${eng ? '<button class="primary" data-act="save-scenario" type="button">Save as new revision</button>' : ''}
        </form>`;
    }
    runInspector(s) {
      const r = (this.detail?.runs || []).find(x => x.id === s.id);
      if (!r) return '';
      const live = r.state === 'running' || r.state === 'paused';
      return `<div class="ts-panelh">Run ${esc(new Date(r.created_at).toLocaleString())}</div>
        <dl class="ts-kv"><dt>Scenario</dt><dd>${esc(r.scenario_name)} (rev ${r.scenario_revision})</dd><dt>State</dt><dd><span class="ts-dot ${r.state}"></span>${esc(r.state)}</dd>
        <dt>Seed</dt><dd>${r.seed}</dd><dt>Started by</dt><dd>${esc(r.created_by)}</dd></dl>
        ${r.error ? `<p class="warn">${esc(r.error)}</p>` : ''}
        ${live ? `<button data-act="watch" data-id="${r.id}">Watch live</button>` : ''}
        ${r.summary ? summaryHTML(r.summary) : ''}
        <p><button class="ts-link" data-act="dl-manifest" data-id="${r.id}">Download run manifest</button></p>
        ${!live && this.can('engineer') ? `<button data-act="delete-run" data-id="${r.id}">Delete run</button>` : ''}`;
    }

    renderBottom() {
      const el = this.$('.ts-bottom');
      if (!el) return;
      const tabs = [['events', 'Events'], ['import', 'Import'], ['health', 'Health'], ['summary', 'Run summary']];
      let body = '';
      if (this.bottomTab === 'events') body = this.events.length ? `<ul class="ts-events">${this.events.slice().reverse().map(e => `<li><span class="t">${clock(e.sim_time)}</span><span class="k ${esc(e.kind)}">${esc(e.kind)}</span> ${esc(e.message)}${e.actor ? ` <span class="muted">— ${esc(e.actor)}</span>` : ''}</li>`).join('')}</ul>` : '<p class="muted">Run events appear here.</p>';
      else if (this.bottomTab === 'import') body = this.importBody();
      else if (this.bottomTab === 'health') body = this.healthBody();
      else body = this.summaryBody();
      el.innerHTML = `<div class="ts-tabs">${tabs.map(([k, l]) => `<button class="${this.bottomTab === k ? 'on' : ''}" data-tab="${k}">${l}</button>`).join('')}</div><div class="ts-tabbody">${body}</div>`;
    }
    importBody() {
      const d = this.importDraft;
      if (d) {
        const sides = [['auto', 'Detect automatically'], ['left', 'Left-hand traffic'], ['right', 'Right-hand traffic']];
        const tooBig = d.source === 'overpass' && this.drawn && this.drawn.km2 > (this.status?.max_import_km2 || 25);
        return `<div class="ts-import">
          <label class="ts-field"><span>Name</span><input name="ts-net-name" value="${esc(d.name)}" placeholder="e.g. Rondebosch Main Road" maxlength="120"></label>
          ${d.source === 'upload' ? `<label class="ts-field"><span>OSM file</span><input type="file" name="ts-file" accept=".osm,.xml,.gz"> ${d.upload ? `<span class="muted">${fmt(d.upload.bytes / 1e6, 1)} MB uploaded</span>` : ''}</label>` : ''}
          <label class="ts-field"><span>Driving side</span><select name="ts-side">${sides.map(([v, l]) => `<option value="${v}" ${d.driving_side === v ? 'selected' : ''}>${l}</option>`).join('')}</select></label>
          <p>${this.drawn ? `Area: <b>${fmt(this.drawn.km2, 2)} km²</b>` : 'Draw the area on the map.'} ${tooBig ? `<span class="warn">Downloads are limited to ${this.status.max_import_km2} km²; choose a smaller area or upload an OSM file.</span>` : ''}</p>
          <div class="ts-actions"><button class="primary" data-act="submit-import" ${this.drawn && !tooBig ? '' : 'disabled'}>Import</button> <button data-act="cancel-draft">Cancel</button></div>
          <p class="muted">Road data comes from OpenStreetMap (© OpenStreetMap contributors, ODbL). Signals, detectors and traffic are generated automatically; review them before relying on results.</p></div>`;
      }
      const job = this.detail?.job;
      if (!job) return '<p class="muted">No import for this network.</p>';
      return `<div class="ts-job"><p><b>${esc(job.state)}</b>${job.error ? ` — <span class="warn">${esc(job.error)}</span>` : ''}
        ${(job.state === 'running' || job.state === 'queued') && this.can('engineer') ? ` <button data-act="cancel-job" data-id="${job.id}">Cancel</button>` : ''}</p>
        <ol class="ts-steps">${job.steps.map(s => `<li class="${s.state}"><span class="ts-step-i"></span><b>${esc(s.label)}</b> ${s.detail ? `<span class="muted">${esc(s.detail)}</span>` : ''}</li>`).join('')}</ol>
        ${job.log_excerpt ? `<details><summary>Technical detail</summary><pre>${esc(job.log_excerpt)}</pre></details>` : ''}</div>`;
    }
    healthBody() {
      const v = (this.detail?.versions || []).slice(-1)[0];
      if (!v) return '<p class="muted">No network version yet.</p>';
      const items = v.health?.items || [];
      return `<p>${items.length} findings: ${Object.entries(v.health?.counts || {}).map(([k, n]) => `${n} ${k}`).join(', ')}</p>
        <ul class="ts-health">${items.map((h, i) => `<li class="${esc(h.severity)}"><span class="sev">${esc(h.severity)}</span> ${esc(h.message)} ${h.lat ? `<button class="ts-link" data-act="goto" data-i="${i}">Show</button>` : ''}</li>`).join('')}</ul>`;
    }
    summaryBody() {
      const r = (this.detail?.runs || []).find(x => x.summary);
      if (!r) return '<p class="muted">Finished runs show their results here.</p>';
      return `<p><b>${esc(r.scenario_name)}</b> — ${esc(new Date(r.created_at).toLocaleString())} (${esc(r.state)})</p>${summaryHTML(r.summary)}`;
    }

    renderStatusBar() {
      const el = this.$('.ts-status');
      if (!el) return;
      const st = this.runStatus;
      const conn = { idle: '', connecting: 'connecting…', live: 'live', reconnecting: 'reconnecting…', error: 'error' }[this.conn.state] || '';
      el.innerHTML = st ? `<span class="ts-clock">${clock(st.sim_time)}</span>
        <span>${st.state === 'paused' ? '❚❚ paused' : st.state === 'running' ? '▶ ' + speedLabel(st.speed) : esc(st.state)}${st.state === 'running' ? ` (${fmt(st.real_time_factor, 1)}× actual)` : ''}</span>
        <span>${st.sim_time < st.measure_s ? 'warm-up' : 'measuring'}</span>
        <span>${fmt(st.running)} running · ${fmt(st.inserted)} in · ${fmt(st.arrived)} arrived · ${fmt(st.waiting)} waiting</span>
        <span>${fmt(st.teleports)} teleports</span><span>${fmt(st.mean_speed_kmh, 1)} km/h</span>
        <span class="ts-spacer"></span><span class="ts-conn ${this.conn.state}" title="${esc(this.conn.msg)}">● ${conn}</span><span>${st.viewers || 0} viewing</span>`
        : `<span>${this.network ? esc(this.network.name) : 'No network open'}</span><span class="ts-spacer"></span><span class="ts-conn ${this.conn.state}" title="${esc(this.conn.msg)}">${conn ? '● ' + conn : ''}</span>
           <span class="muted">${this.status ? `service ${esc(this.status.service_version)} · SUMO ${esc(this.status.sumo_version || '?')}` : ''}</span>`;
    }

    // ---------- Events ----------
    onChange(e) {
      const t = e.target;
      if (t.dataset.layer) { this.layers[t.dataset.layer] = t.checked; this.updateZoomLayers(); this.renderMenu(); return; }
      if (t.name === 'ts-speed') { this.setSpeed(Number(t.value)); return; }
      if (t.name === 'ts-file' && t.files?.[0]) { this.uploadFile(t.files[0]); return; }
      if (t.name === 'ts-net-name' && this.importDraft) this.importDraft.name = t.value;
      if (t.name === 'ts-side' && this.importDraft) this.importDraft.driving_side = t.value;
    }
    onClick(e) {
      const tab = e.target.closest('[data-tab]');
      if (tab) { this.bottomTab = tab.dataset.tab; this.renderBottom(); return; }
      const b = e.target.closest('[data-act]');
      if (!b || b.disabled) return;
      const act = b.dataset.act, id = b.dataset.id;
      if (b.classList.contains('ts-mi')) this.shadowRoot.activeElement?.blur(); // close the menu
      if (act.startsWith('speed-')) { this.setSpeed(Number(act.slice(6))); return; }
      if (act.startsWith('toggle-')) { const k = act.slice(7); this.layers[k] = !this.layers[k]; this.updateZoomLayers(); this.renderMenu(); this.renderLayers(); return; }
      switch (act) {
        case 'new-map': this.startImport('overpass'); break;
        case 'new-upload': this.startImport('upload'); break;
        case 'tool-rect': this.setTool('rect'); break;
        case 'tool-polygon': this.setTool('polygon'); this.drawLayer.clearLayers(); break;
        case 'submit-import': this.submitImport(); break;
        case 'cancel-draft': this.cancelImportDraft(); break;
        case 'cancel-job': this.api.call('cancel_import', { id }).then(() => this.flash('Cancelling import…')).catch(err => this.flash(err.message, true)); break;
        case 'open-net': this.openNetwork(id); break;
        case 'close': this.closeNetwork(); break;
        case 'props': this.select({ kind: 'network' }); break;
        case 'fit': if (this.geometry) { const g = this.geometry.bbox; this.map.fitBounds([[g[1], g[0]], [g[3], g[2]]]); } break;
        case 'sel-scenario': this.select({ kind: 'scenario', id }); break;
        case 'sel-run': this.select({ kind: 'run', id }); break;
        case 'start': this.startSim(); break;
        case 'pause': this.togglePause(); break;
        case 'step': this.control('step'); break;
        case 'stop': if (confirm('Stop the simulation and compute results?')) this.control('stop'); break;
        case 'watch': this.watchRun(id); break;
        case 'follow': this.followKey = this.followKey === this.selected.key ? null : this.selected.key; this.renderInspector(); break;
        case 'save-scenario': this.saveScenario(b.closest('form')); break;
        case 'summary': { const r = (this.detail?.runs || []).find(x => x.summary); if (r) this.select({ kind: 'run', id: r.id }); this.bottomTab = 'summary'; this.renderBottom(); break; }
        case 'health': this.bottomTab = 'health'; this.renderBottom(); break;
        case 'goto': { const h = (this.detail.versions.slice(-1)[0].health.items || [])[Number(b.dataset.i)]; if (h) this.map.setView([h.lat, h.lon], 18); break; }
        case 'dl-csv': this.download(`/api/v1/networks/${this.network.id}/signals.csv`, 'traffic-signals.csv'); break;
        case 'dl-geojson': this.download(`/api/v1/networks/${this.network.id}/signals.geojson`, 'traffic-signals.geojson'); break;
        case 'dl-manifest': this.download(`/api/v1/runs/${id}/manifest`, `run-${id}.json`); break;
        case 'delete-run': if (confirm('Delete this run and its results?')) this.api.call('delete_run', { id }).then(() => { this.selected = { kind: 'network' }; this.loadDetail(); }).catch(err => this.flash(err.message, true)); break;
        case 'delete-net': if (confirm(`Delete network "${this.network.name}", its scenarios and runs? This cannot be undone.`)) this.api.call('delete_network', { id: this.network.id }, crypto.randomUUID()).then(() => { this.closeNetwork(); this.refreshNetworks(); this.flash('Network deleted.'); }).catch(err => this.flash(err.message, true)); break;
        case 'shortcuts': alert('Space: start, pause or resume\n.  : step one control interval\n+ / - : faster / slower\nF : follow the selected vehicle\nEsc : cancel the current drawing tool'); break;
        case 'about': alert(`Traffic Simulation Studio\nService ${this.status?.service_version || '?'} · SUMO ${this.status?.sumo_version || '?'}\n\nMap data © OpenStreetMap contributors (ODbL).\nSimulation by Eclipse SUMO (EPL-2.0).`); break;
      }
    }
    async saveScenario(form) {
      const sc = (this.detail?.scenarios || []).find(x => x.id === form.dataset.scenario);
      if (!sc) return;
      const timing = { ...sc.timing };
      form.querySelectorAll('[data-timing]').forEach(i => { timing[i.dataset.timing] = Number(i.value); });
      const changes = { timing, seed: Number(form.querySelector('[name=seed]').value), step_length_s: Number(form.querySelector('[name=step]').value) };
      try {
        await this.api.call('update_scenario', { id: sc.id, revision: Number(form.dataset.revision), changes }, crypto.randomUUID());
        this.flash('Scenario saved as a new revision.');
        await this.loadDetail(true);
        this.renderInspector();
      } catch (e) { this.flash(e.message, true); }
    }
    async download(path, name) {
      try {
        const res = await this.api.rest(path);
        const url = URL.createObjectURL(await res.blob());
        const a = document.createElement('a'); a.href = url; a.download = name; document.body.appendChild(a); a.click(); a.remove();
        setTimeout(() => URL.revokeObjectURL(url), 5000);
      } catch (e) { this.flash(e.message, true); }
    }
  }

  function summaryHTML(s) {
    return `<dl class="ts-kv ts-summary">
      <dt>Measured</dt><dd>${clock(s.measure_from_s)}–${clock(s.measure_to_s)}${s.partial ? ' (partial)' : ''}</dd>
      <dt>Trips</dt><dd>${fmt(s.trips_measured)} completed</dd>
      <dt>Mean travel time</dt><dd>${fmt(s.mean_travel_time_s)} s (85th pct ${fmt(s.p85_travel_time_s)} s)</dd>
      <dt>Mean delay</dt><dd>${fmt(s.mean_delay_s)} s per trip</dd><dt>Mean waiting</dt><dd>${fmt(s.mean_waiting_s)} s</dd>
      <dt>Mean speed</dt><dd>${fmt(s.mean_speed_kmh, 1)} km/h</dd>
      <dt>Vehicle-km / hours</dt><dd>${fmt(s.vehicle_km)} km / ${fmt(s.vehicle_hours, 1)} h</dd><dt>Total delay</dt><dd>${fmt(s.total_delay_hours, 1)} h</dd>
      <dt>Vehicles</dt><dd>${fmt(s.inserted)} inserted, ${fmt(s.running_at_end)} still running, ${fmt(s.waiting_to_insert_at_end)} waiting</dd>
      <dt>Teleports</dt><dd>${fmt(s.teleports)}</dd><dt>Wall time</dt><dd>${fmt(s.wall_s, 1)} s (${fmt(s.mean_real_time_factor)}× real time)</dd>
    </dl>${(s.data_quality_notes || []).map(n => `<p class="warn">${esc(n)}</p>`).join('')}`;
  }
  function roundRect(ctx, x, y, w, h, r) { ctx.beginPath(); ctx.moveTo(x + r, y); ctx.arcTo(x + w, y, x + w, y + h, r); ctx.arcTo(x + w, y + h, x, y + h, r); ctx.arcTo(x, y + h, x, y, r); ctx.arcTo(x, y, x + w, y, r); ctx.closePath(); }
  function ringOfBounds(b) { return [[b.getWest(), b.getSouth()], [b.getEast(), b.getSouth()], [b.getEast(), b.getNorth()], [b.getWest(), b.getNorth()], [b.getWest(), b.getSouth()]]; }
  function areaKm2(ring) {
    if (!ring || ring.length < 3) return 0;
    const lat0 = ring.reduce((a, p) => a + p[1], 0) / ring.length, lon0 = ring.reduce((a, p) => a + p[0], 0) / ring.length;
    const kx = Math.cos(lat0 * Math.PI / 180) * 111.32, ky = 110.574;
    let sum = 0;
    for (let i = 0; i < ring.length; i++) { const a = ring[i], b = ring[(i + 1) % ring.length]; sum += (a[0] - lon0) * kx * (b[1] - lat0) * ky - (b[0] - lon0) * kx * (a[1] - lat0) * ky; }
    return Math.abs(sum) / 2;
  }

  const LEAFLET_FALLBACK_CSS = `.leaflet-pane,.leaflet-tile,.leaflet-marker-icon,.leaflet-marker-shadow,.leaflet-tile-container,.leaflet-pane>svg,.leaflet-pane>canvas,.leaflet-zoom-box,.leaflet-image-layer,.leaflet-layer{position:absolute;left:0;top:0}.leaflet-container{overflow:hidden}.leaflet-tile{user-select:none;visibility:hidden}.leaflet-tile-loaded{visibility:inherit}.leaflet-container img.leaflet-tile{max-width:none!important}.leaflet-pane{z-index:400}.leaflet-tile-pane{z-index:200}.leaflet-overlay-pane{z-index:400}.leaflet-marker-pane{z-index:600}.leaflet-tooltip-pane{z-index:650}.leaflet-popup-pane{z-index:700}.leaflet-control{position:relative;z-index:800;pointer-events:auto}.leaflet-top,.leaflet-bottom{position:absolute;z-index:1000;pointer-events:none}.leaflet-top{top:0}.leaflet-right{right:0}.leaflet-bottom{bottom:0}.leaflet-left{left:0}.leaflet-control-zoom a{display:block;width:30px;height:30px;line-height:30px;text-align:center;background:#fff;color:#000;text-decoration:none;font:bold 18px monospace}.leaflet-left .leaflet-control{margin-left:10px}.leaflet-top .leaflet-control{margin-top:10px}.leaflet-control-attribution{background:rgba(255,255,255,.8);padding:0 5px;font-size:11px}.leaflet-tooltip{position:absolute;padding:4px 6px;background:#fff;border:1px solid #ccc;border-radius:3px;white-space:nowrap;pointer-events:none}`;

  const STYLE = `
  :host{--ts-muted:color-mix(in srgb,var(--content-text,#0f172a) 78%,transparent);display:block;height:100%;min-height:480px;color:var(--content-text,#0f172a);font-family:var(--widget-font-family,system-ui,sans-serif);font-size:13px;outline:none}
  *{box-sizing:border-box}
  button{font:inherit;color:inherit;background:var(--input-bg,#fff);border:1px solid var(--border-color,#cbd5e1);border-radius:4px;padding:3px 9px;cursor:pointer}
  button:hover:not(:disabled){background:var(--sidebar-hover,#e2e8f0)} button:disabled{opacity:.45;cursor:default}
  button.primary{background:var(--accent-color,#2563eb);color:var(--accent-text,#fff);border-color:transparent}
  button.on{outline:2px solid var(--accent-color,#2563eb)}
  input,select{font:inherit;color:inherit;background:var(--input-bg,#fff);border:1px solid var(--border-color,#cbd5e1);border-radius:4px;padding:3px 6px}
  option{background:var(--input-bg,#fff);color:var(--content-text,#0f172a)}
  input::placeholder{color:var(--content-text,#0f172a);opacity:.5}
  code{font-size:12px} .muted{color:var(--ts-muted)} .warn{color:var(--error-color,#b91c1c)}
  .ts-root{display:grid;grid-template-rows:auto 1fr auto auto;height:100%;background:var(--widget-bg,#fff);position:relative}
  .ts-menubar{display:flex;align-items:center;gap:2px;padding:2px 6px;background:var(--widget-header-bg,#f1f5f9);color:var(--widget-header-text,inherit);border-bottom:1px solid var(--border-color,#cbd5e1)}
  .ts-menu{position:relative} .ts-mtitle{border:0;background:transparent;padding:4px 10px}
  .ts-mdrop{display:none;position:absolute;left:0;top:100%;z-index:2000;min-width:240px;background:var(--widget-bg,#fff);border:1px solid var(--border-color,#cbd5e1);border-radius:6px;box-shadow:0 8px 24px rgba(0,0,0,.18);padding:4px}
  .ts-menu:hover .ts-mdrop,.ts-menu:focus-within .ts-mdrop{display:block}
  .ts-mi{display:flex;justify-content:space-between;width:100%;border:0;background:transparent;text-align:left;padding:6px 10px}
  .ts-mi kbd{font:11px monospace;color:var(--ts-muted)} .ts-mempty{display:block;padding:6px 10px;color:var(--ts-muted)}
  .ts-mspacer,.ts-spacer{flex:1} .ts-brand{font-weight:600;opacity:.7;padding-right:6px}
  .ts-svc{display:inline-flex;align-items:center;gap:6px;font-size:12px;margin-right:12px;padding:2px 8px;border-radius:10px;border:1px solid var(--border-color,#cbd5e1);cursor:help}
  .ts-svc-dot{width:9px;height:9px;border-radius:50%;background:#94a3b8}
  .ts-svc.ok .ts-svc-dot{background:#22c55e} .ts-svc.degraded .ts-svc-dot{background:#f59e0b}
  .ts-svc.down .ts-svc-dot{background:#dc2626;animation:pulse 1s infinite} .ts-svc.down{color:var(--error-color,#b91c1c)}
  .ts-main{display:grid;grid-template-columns:230px 1fr 300px;min-height:0}
  .ts-left,.ts-right{overflow:auto;border-right:1px solid var(--border-color,#cbd5e1);padding:8px}
  .ts-right{border-right:0;border-left:1px solid var(--border-color,#cbd5e1)}
  .ts-panelh{font-weight:600;font-size:14px;margin:2px 0 8px;display:flex;justify-content:space-between;align-items:center}
  .ts-icon{padding:0 7px}
  ul{list-style:none;margin:0;padding:0} .ts-nets>li{margin-bottom:4px}
  .ts-net{width:100%;text-align:left;border:0;background:transparent;padding:5px 6px;font-weight:500}
  .ts-nets>li.open>.ts-net{background:var(--sidebar-hover,#e2e8f0)}
  .ts-sub{padding:2px 0 6px 10px} .ts-subh{font-size:11px;text-transform:uppercase;letter-spacing:.04em;color:var(--ts-muted);margin:8px 0 3px}
  .ts-sub li.sel .ts-link{font-weight:600;text-decoration:underline}
  .ts-link{border:0;background:transparent;padding:2px 0;text-align:left;color:var(--accent-color,#2563eb)}
  .ts-badge{font-size:10px;border-radius:8px;padding:1px 6px;background:#fde68a;color:#78350f;margin-left:4px} .ts-badge.failed{background:#fecaca;color:#7f1d1d}
  .ts-empty{padding:6px}
  .ts-layers{margin-top:14px} .ts-layers label{display:block;padding:2px 0}
  .ts-legend{margin-top:6px;font-size:11px;color:var(--ts-muted)} .ts-legend span{display:inline-block;width:14px;height:4px;background:var(--c);margin:0 3px 2px 6px;vertical-align:middle}
  .ts-center{display:grid;grid-template-rows:auto 1fr;min-height:0}
  .ts-toolbar{display:flex;align-items:center;gap:6px;padding:5px 8px;border-bottom:1px solid var(--border-color,#cbd5e1);flex-wrap:wrap;min-height:38px}
  .ts-toolbar select[name=ts-scenario]{max-width:260px} .ts-speed{display:flex;gap:4px;align-items:center}
  .ts-area-readout{font-weight:600;margin-left:auto}
  .ts-map{position:relative;min-height:0;background:#dfe7ef} .ts-map.drawing{cursor:crosshair}
  .ts-zoom{position:absolute;bottom:8px;right:8px;z-index:1000;padding:2px 7px;border-radius:10px;background:rgba(20,20,20,.75);border:1px solid var(--border-color,#cbd5e1);color:#e0e0e0;font-size:11px;font-variant-numeric:tabular-nums;pointer-events:none}
  .ts-handle{background:#fff;border:2px solid #2563eb;border-radius:2px;cursor:move;box-shadow:0 1px 3px rgba(0,0,0,.4)}
  .ts-live{position:absolute;inset:0;pointer-events:none;z-index:450;transition:opacity .1s}
  .ts-kv{display:grid;grid-template-columns:auto minmax(0,1fr);gap:3px 10px;margin:0 0 8px} .ts-kv dt{color:var(--content-text,#0f172a);font-weight:500;opacity:.85} .ts-kv dd{margin:0;min-width:0;overflow-wrap:anywhere}
  .ts-table{width:100%;border-collapse:collapse;font-size:12px} .ts-table th,.ts-table td{text-align:left;padding:2px 4px;border-bottom:1px solid var(--border-color,#e2e8f0)}
  .ts-list li{padding:1px 0;font-size:12px;word-break:break-all}
  .ts-aspects span{display:inline-block;width:9px;height:14px;margin-right:2px;border-radius:2px}
  .ts-field{display:flex;align-items:center;gap:6px;margin:4px 0} .ts-field>span{min-width:150px;color:var(--content-text,#0f172a);font-weight:500} .ts-field input[type=number]{width:80px}
  .ts-import .ts-field input[name=ts-net-name]{width:320px}
  .ts-bottom{height:190px;border-top:1px solid var(--border-color,#cbd5e1);display:grid;grid-template-rows:auto 1fr;min-height:0}
  .ts-tabs{display:flex;gap:2px;padding:4px 6px 0;border-bottom:1px solid var(--border-color,#cbd5e1)} .ts-tabs button{border-bottom:0;border-radius:4px 4px 0 0}
  .ts-tabs button.on{outline:0;background:var(--widget-header-bg,#f1f5f9);font-weight:600}
  .ts-tabbody{overflow:auto;padding:6px 10px}
  .ts-events li{padding:1px 0;font-size:12px} .ts-events .t{font-family:monospace;margin-right:8px;color:var(--ts-muted)}
  .ts-events .k{display:inline-block;min-width:80px;font-weight:600} .ts-events .k.failed{color:var(--error-color,#b91c1c)}
  .ts-steps li{padding:2px 0;display:flex;gap:6px;align-items:center} .ts-step-i{width:10px;height:10px;border-radius:50%;background:#cbd5e1;flex:none}
  .ts-steps li.done .ts-step-i{background:#22c55e} .ts-steps li.running .ts-step-i{background:#3b82f6;animation:pulse 1s infinite} .ts-steps li.failed .ts-step-i{background:#dc2626}
  @keyframes pulse{50%{opacity:.3}}
  pre{white-space:pre-wrap;font-size:11px;max-height:160px;overflow:auto;background:var(--widget-header-bg,#f1f5f9);padding:6px}
  .ts-health li{padding:2px 0;font-size:12px} .ts-health .sev{display:inline-block;min-width:58px;font-weight:600} .ts-health li.warning .sev{color:#b45309} .ts-health li.error .sev{color:var(--error-color,#b91c1c)}
  .ts-status{display:flex;gap:14px;align-items:center;padding:4px 10px;border-top:1px solid var(--border-color,#cbd5e1);background:var(--widget-header-bg,#f1f5f9);font-size:12px;white-space:nowrap;overflow:hidden}
  .ts-clock{font-family:monospace;font-size:14px;font-weight:600}
  .ts-conn.live{color:#16a34a} .ts-conn.reconnecting,.ts-conn.connecting{color:#d97706} .ts-conn.error{color:var(--error-color,#b91c1c)}
  .ts-dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:5px;background:#94a3b8} .ts-dot.running{background:#22c55e} .ts-dot.paused{background:#f59e0b} .ts-dot.failed,.ts-dot.interrupted{background:#dc2626} .ts-dot.completed{background:#3b82f6}
  .ts-msg{position:absolute;left:50%;transform:translateX(-50%);bottom:40px;z-index:3000;max-width:70%;padding:0;border-radius:6px;font-size:13px}
  .ts-msg:not(:empty){padding:8px 14px;background:#0f172a;color:#fff;box-shadow:0 6px 20px rgba(0,0,0,.25)} .ts-msg.err:not(:empty){background:var(--error-color,#b91c1c)}
  .ts-attr{font-size:11px;color:var(--ts-muted)}
  @media (max-width:1100px){.ts-main{grid-template-columns:190px 1fr 250px}}
  `;

  window.XACT.registerWidget({ type: 'traffic-studio', name: 'Traffic Studio', icon: '🚦', defaultW: 24, defaultH: 22, minW: 10, minH: 12 }, TrafficStudio);
})();
