import { getAuthHeaders } from '../../auth';

// ── Constants ─────────────────────────────────────────────────────────────────

const API_BASE = '/xact';
const MAX_POINTS = 300;
const FETCH_TIMEOUT_MS = 15_000;

// ── Types ─────────────────────────────────────────────────────────────────────

interface DataPoint { t: number; v: number; }

// ── Helper ────────────────────────────────────────────────────────────────────

function esc(s: string): string {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// ── SparklineWidget ───────────────────────────────────────────────────────────

/**
 * <sparkline-widget> - Reusable time-series sparkline component.
 *
 * Attributes:
 *   device      - device path in dot-notation as stored in metric_devices.name
 *                 (e.g. "Pumps.PMP001")
 *   metric      - metric/tag name (e.g. "flow_rate")
 *   time-period - hours of history to fetch on connect (default: 48)
 *   color       - CSS colour for the line/fill (default: var(--accent-color))
 *   units       - optional units label shown in the hover tooltip
 *
 * Public methods:
 *   triggerUpdate() - fetch data since the last known timestamp (incremental).
 *                     Falls back to a full range fetch if no data is held yet.
 */
export class SparklineWidget extends HTMLElement {
  private _data: DataPoint[] = [];
  private _lastTs: string | null = null;
  private _connected = false;
  private _refreshTimer: ReturnType<typeof setInterval> | null = null;
  private _renderFrame: number | null = null;
  private _fetchAbort: AbortController | null = null;

  // Tooltip state lives on the instance, not on a DOM element, so cleanup is
  // always reliable regardless of how many times the inner DOM is replaced.
  private _tip: HTMLDivElement | null = null;
  private _tipTarget: HTMLElement | null = null;
  private _onMove: ((e: MouseEvent) => void) | null = null;
  private _onLeave: (() => void) | null = null;

  static get observedAttributes() {
    return ['device', 'metric', 'time-period', 'color', 'units', 'refresh-interval'];
  }

  // ── Attribute accessors ─────────────────────────────────────────────────────

  get device(): string          { return this.getAttribute('device') || ''; }
  get metric(): string          { return this.getAttribute('metric') || ''; }
  get timePeriod(): number      { return parseInt(this.getAttribute('time-period') || '48', 10); }
  get color(): string           { return this.getAttribute('color') || 'var(--accent-color)'; }
  get units(): string           { return this.getAttribute('units') || ''; }
  get refreshInterval(): number { return parseInt(this.getAttribute('refresh-interval') || '0', 10); }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  connectedCallback(): void {
    this._connected = true;
    this._renderDOM();
    this._attachTooltip();
    this._fetchRange();
    this._startTimer();
  }

  disconnectedCallback(): void {
    this._connected = false;
    this._stopTimer();
    this._abortFetch();
    if (this._renderFrame !== null) {
      cancelAnimationFrame(this._renderFrame);
      this._renderFrame = null;
    }
    this._cleanupTooltip();
  }

  attributeChangedCallback(name: string, old: string | null, next: string | null): void {
    if (old === next || !this._connected) return;

    if (name === 'refresh-interval') {
      this._stopTimer();
      this._startTimer();
    } else if (name === 'color' || name === 'units') {
      // Visual-only change - re-render SVG, keep data
      this._renderDOM();
      this._attachTooltip();
    } else {
      // Data-affecting change - reset and refetch
      this._abortFetch();
      this._data = [];
      this._lastTs = null;
      this._renderDOM();
      this._attachTooltip();
      this._fetchRange();
    }
  }

  // ── Public API ──────────────────────────────────────────────────────────────

  /**
   * Fetch new data points since the last known timestamp.
   * Called by the parent widget whenever a live tag value arrives.
   */
  triggerUpdate(): void {
    if (!this._connected || !this.device || !this.metric) return;
    if (this._lastTs) {
      this._fetchSince();
    } else {
      this._fetchRange();
    }
  }

  /**
   * Add the live value already delivered through the mirror-store subscription.
   *
   * This keeps sparklines responsive without turning every tag update into a
   * historical metrics API request. Timer refreshes still reconcile persisted
   * history when a refresh interval is configured.
   */
  appendLiveValue(rawValue: unknown, timestamp = Date.now()): void {
    if (!this._connected) return;

    const value = Number(rawValue);
    if (!Number.isFinite(value)) return;

    const last = this._data[this._data.length - 1];
    if (last && timestamp <= last.t) {
      if (last.v === value) return;
      timestamp = last.t + 1;
    }

    this._ingestData([{ t: timestamp, v: value }]);
    this._scheduleRender();
  }

  // ── Timer ───────────────────────────────────────────────────────────────────

  private _startTimer(): void {
    this._stopTimer();
    const secs = this.refreshInterval;
    if (secs <= 0) return;
    this._refreshTimer = setInterval(() => this.triggerUpdate(), secs * 1000);
  }

  private _stopTimer(): void {
    if (this._refreshTimer !== null) {
      clearInterval(this._refreshTimer);
      this._refreshTimer = null;
    }
  }

  // ── Data fetching ───────────────────────────────────────────────────────────

  private async _fetchRange(): Promise<void> {
    const { device, metric, timePeriod } = this;
    if (!device || !metric) return;
    if (this._fetchAbort) return;

    const start = new Date(Date.now() - timePeriod * 3_600_000).toISOString();
    const url = `${API_BASE}/api/v1/metrics/${encodeURIComponent(device)}`
      + `?start=${encodeURIComponent(start)}&metrics=${encodeURIComponent(metric)}`;
    const controller = this._beginFetch();

    try {
      const res = await fetch(url, { headers: getAuthHeaders(), signal: controller.signal });
      if (!this._connected || !res.ok) return;
      const json = await res.json();
      const series = json.series?.find((s: any) => s.name === metric);
      if (!series?.data?.length) return;

      this._ingestData((series.data as [number, number][]).map(([t, v]) => ({ t, v })), true);
      this._renderDOM();
      this._attachTooltip();
    } catch { /* network / parse errors are non-fatal */ }
    finally {
      this._endFetch(controller);
    }
  }

  private async _fetchSince(): Promise<void> {
    const { device, metric } = this;
    if (!device || !metric || !this._lastTs) return;
    if (this._fetchAbort) return;

    const url = `${API_BASE}/api/v1/metrics/${encodeURIComponent(device)}/since`
      + `?after=${encodeURIComponent(this._lastTs)}`
      + `&start_metric=${encodeURIComponent(metric)}`
      + `&metrics=${encodeURIComponent(metric)}`;
    const controller = this._beginFetch();

    try {
      const res = await fetch(url, { headers: getAuthHeaders(), signal: controller.signal });
      if (!this._connected || !res.ok) return;
      const json = await res.json();
      const series = json.series?.find((s: any) => s.name === metric);
      if (!series?.data?.length) return;

      this._ingestData((series.data as [number, number][]).map(([t, v]) => ({ t, v })), true);
      this._renderDOM();
      this._attachTooltip();
    } catch { /* network / parse errors are non-fatal */ }
    finally {
      this._endFetch(controller);
    }
  }

  private _beginFetch(): AbortController {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), FETCH_TIMEOUT_MS);
    controller.signal.addEventListener('abort', () => window.clearTimeout(timeout), { once: true });
    this._fetchAbort = controller;
    return controller;
  }

  private _endFetch(controller: AbortController): void {
    if (this._fetchAbort === controller) this._fetchAbort = null;
  }

  private _abortFetch(): void {
    this._fetchAbort?.abort();
    this._fetchAbort = null;
  }

  private _ingestData(points: DataPoint[], updateFetchCursor = false): void {
    const cutoff = Date.now() - this.timePeriod * 3_600_000;
    const byTimestamp = new Map<number, number>();

    for (const p of this._data) {
      if (p.t >= cutoff) byTimestamp.set(p.t, p.v);
    }
    for (const p of points) {
      if (p.t >= cutoff && Number.isFinite(p.v)) byTimestamp.set(p.t, p.v);
    }

    this._data = Array.from(byTimestamp, ([t, v]) => ({ t, v }))
      .sort((a, b) => a.t - b.t)
      .slice(-MAX_POINTS);

    if (updateFetchCursor && points.length > 0) {
      const latestFetchedTs = Math.max(...points.map(p => p.t));
      if (Number.isFinite(latestFetchedTs)) {
        this._lastTs = new Date(latestFetchedTs).toISOString();
      }
    }
  }

  private _scheduleRender(): void {
    if (this._renderFrame !== null) return;
    this._renderFrame = requestAnimationFrame(() => {
      this._renderFrame = null;
      if (!this._connected) return;
      this._renderDOM();
      this._attachTooltip();
    });
  }

  // ── Rendering ───────────────────────────────────────────────────────────────

  private _svgHtml(): string {
    const color = this.color;

    if (this._data.length < 2) {
      return `<div class="sl-bg" style="
        height:38px; display:flex; align-items:center;
        font-size:0.55rem; letter-spacing:0.1em; text-transform:uppercase;
        color:var(--content-text); opacity:0.18;
        font-family:ui-sans-serif,system-ui,sans-serif;
      ">collecting data\u2026</div>`;
    }

    const W = 400, H = 36, P = 1;

    // Append a synthetic "now" point at the last known value so the sparkline
    // visually advances to wall-clock time even when the value is unchanged.
    const renderData: DataPoint[] = this._data.length > 0
      ? [...this._data, { t: Date.now(), v: this._data[this._data.length - 1].v }]
      : this._data;

    const vals = renderData.map(d => d.v);
    const min = Math.min(...vals), max = Math.max(...vals), span = max - min || 1;
    const toX = (i: number) => P + (i / (renderData.length - 1)) * (W - P * 2);
    const toY = (v: number) => H - P - ((v - min) / span) * (H - P * 2);

    const pts = renderData
      .map((d, i) => `${toX(i).toFixed(1)},${toY(d.v).toFixed(1)}`)
      .join(' ');
    const area = `M${P},${H} `
      + renderData.map((d, i) => `L${toX(i).toFixed(1)},${toY(d.v).toFixed(1)}`).join(' ')
      + ` L${(W - P).toFixed(1)},${H} Z`;

    const uid = this.getAttribute('id') || 'sl0';

    return `
      <div class="sl-bg" style="
        position:relative; height:38px; border-radius:3px;
        background:${color}08;
      ">
        <svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none"
             style="width:100%;height:38px;display:block;"
             xmlns="http://www.w3.org/2000/svg">
          <defs>
            <linearGradient id="sl-fill-${uid}" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%"   stop-color="${color}" stop-opacity="0.22"/>
              <stop offset="100%" stop-color="${color}" stop-opacity="0.01"/>
            </linearGradient>
          </defs>
          <path d="${area}" fill="url(#sl-fill-${uid})"/>
          <polyline points="${pts}"
            fill="none" stroke="${color}" stroke-width="1.5"
            stroke-linecap="round" stroke-linejoin="round"
            vector-effect="non-scaling-stroke"/>
        </svg>
      </div>`;
  }

  /** Replace inner HTML. Always cleans up the tooltip first so no orphans are left behind. */
  private _renderDOM(): void {
    this._cleanupTooltip();
    this.innerHTML = this._svgHtml();
  }

  // ── Tooltip ─────────────────────────────────────────────────────────────────

  /** Remove the tooltip div and detach its event listeners. Safe to call multiple times. */
  private _cleanupTooltip(): void {
    if (this._onMove && this._tipTarget) {
      this._tipTarget.removeEventListener('mousemove', this._onMove);
    }
    if (this._onLeave && this._tipTarget) {
      this._tipTarget.removeEventListener('mouseleave', this._onLeave);
    }
    if (this._tip) {
      this._tip.remove();
    }
    this._tip = null;
    this._tipTarget = null;
    this._onMove = null;
    this._onLeave = null;
  }

  /** Attach a fresh tooltip to the current .sl-bg element. Cleans up any prior tooltip first. */
  private _attachTooltip(): void {
    this._cleanupTooltip();

    const bg = this.querySelector<HTMLElement>('.sl-bg');
    if (!bg) return;

    const color = this.color;
    const tip = document.createElement('div');
    tip.style.cssText = `
      display:none; position:fixed; pointer-events:none; z-index:9999;
      padding:6px 10px; border-radius:4px;
      background:var(--panel-bg,#111); border:1px solid ${color};
      font-family:ui-monospace,'Cascadia Code','SF Mono','Menlo','Consolas',monospace;
      font-size:0.6rem; font-weight:400; color:${color};
      white-space:nowrap; letter-spacing:0.04em; line-height:1.7;
      box-shadow:0 4px 16px rgba(0,0,0,0.55);
    `;
    document.body.appendChild(tip);

    const onMove = (e: MouseEvent) => {
      if (this._data.length < 2) { tip.style.display = 'none'; return; }
      // Use the same render dataset (with synthetic "now" point) so the tooltip
      // reflects the extended timeline shown in the SVG.
      const data: DataPoint[] = [...this._data, { t: Date.now(), v: this._data[this._data.length - 1].v }];

      const rect = bg.getBoundingClientRect();
      const frac = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
      const pt = data[Math.round(frac * (data.length - 1))];
      const d = new Date(pt.t);

      const u = this.units;
      const valStr = (Number.isInteger(pt.v) ? String(pt.v) : pt.v.toFixed(2))
        + (u ? `\u2009${u}` : '');

      tip.innerHTML = `
        <div style="opacity:0.6">${esc(d.toLocaleDateString(undefined, { year:'numeric', month:'2-digit', day:'2-digit' }))}</div>
        <div style="opacity:0.6">${esc(d.toLocaleTimeString(undefined, { hour:'2-digit', minute:'2-digit', second:'2-digit' }))}</div>
        <div style="font-size:0.7rem;margin-top:2px">${esc(valStr)}</div>
      `;

      const TW = 110, TH = 68;
      const left = Math.max(8, Math.min(e.clientX - TW / 2, window.innerWidth - TW - 8));
      tip.style.left = `${left}px`;
      tip.style.top = `${Math.max(8, e.clientY - TH - 10)}px`;
      tip.style.display = 'block';
    };

    const onLeave = () => { tip.style.display = 'none'; };

    bg.addEventListener('mousemove', onMove);
    bg.addEventListener('mouseleave', onLeave);

    this._tip = tip;
    this._tipTarget = bg;
    this._onMove = onMove;
    this._onLeave = onLeave;
  }
}

// ── Registration ──────────────────────────────────────────────────────────────

customElements.define('sparkline-widget', SparklineWidget);
