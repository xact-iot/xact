import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { registerPermissions } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import { queryEvents } from '../../api';
import type { EventEntry, EventFilter } from '../../api';
import { showAlert } from '../../components/app-dialog';

// ─── Registration ─────────────────────────────────────────────────────────────

registerPermissions('events', 'System Events', [
  { name: 'read', description: 'View system event entries' },
], 'Controls access to the Events Viewer widget - roles with read can query and export event records.');

registerWidgetType({
  type: 'events-viewer-widget',
  name: 'Events Viewer',
  icon: '📋',
  category: 'System',
  defaultW: 20,
  defaultH: 28,
  minW: 1,
  minH: 1,
});

// ─── SheetJS loader (CDN, singleton) ─────────────────────────────────────────

let xlsxReady: Promise<void> | null = null;
function loadXlsx(): Promise<void> {
  if (xlsxReady) return xlsxReady;
  xlsxReady = new Promise<void>((resolve, reject) => {
    if ((window as any).XLSX) { resolve(); return; }
    const s = document.createElement('script');
    s.src = 'https://cdn.sheetjs.com/xlsx-0.20.3/package/dist/xlsx.full.min.js';
    s.onload = () => resolve();
    s.onerror = () => { xlsxReady = null; reject(new Error('Failed to load SheetJS')); };
    document.head.appendChild(s);
  });
  return xlsxReady;
}

// ─── Types ───────────────────────────────────────────────────────────────────

type SortCol = 'timestamp' | 'severity' | 'userName' | 'device';
type SortDir = 'asc' | 'desc';
type ResizableCol = 'timestamp' | 'severity' | 'userName' | 'device' | 'message' | 'params';

interface Config {
  timestampColWidthPx?: number;
  severityColWidthPx?: number;
  userNameColWidthPx?: number;
  deviceColWidthPx?: number;
  messageColWidthPx?: number;
  paramsColWidthPx?: number;
}

const PERIODS = [
  { label: 'Previous 1 hr',  hours: 1 },
  { label: 'Previous 3 hrs', hours: 3 },
  { label: 'Previous 6 hrs', hours: 6 },
  { label: 'Previous 12 hrs', hours: 12 },
  { label: 'Previous 24 hrs', hours: 24 },
];
const UPDATE_INTERVALS: { label: string; ms: number }[] = [
  { label: 'Off',  ms: 0 },
  { label: '10 s', ms: 10_000 },
  { label: '30 s', ms: 30_000 },
  { label: '1 m',  ms: 60_000 },
  { label: '5 m',  ms: 300_000 },
];

const ROW_HEIGHT_PX = 38;   // approximate height of one table row (13px font + 10px padding + border)
const CHROME_PX    = 168;   // filters + thead + footer chrome
const MIN_RESIZABLE_COL_WIDTH_PX = 80;
const RESIZABLE_COLUMNS: ResizableCol[] = ['timestamp', 'severity', 'userName', 'device', 'message', 'params'];
const COL_CLASS_BY_ID: Record<ResizableCol, string> = {
  timestamp: 'col-ts',
  severity: 'col-sev',
  userName: 'col-user',
  device: 'col-dev',
  message: 'col-msg',
  params: 'col-prm',
};
const CONFIG_KEY_BY_COL: Record<ResizableCol, keyof Config> = {
  timestamp: 'timestampColWidthPx',
  severity: 'severityColWidthPx',
  userName: 'userNameColWidthPx',
  device: 'deviceColWidthPx',
  message: 'messageColWidthPx',
  params: 'paramsColWidthPx',
};

// ─── Widget ───────────────────────────────────────────────────────────────────

export class EventsViewerWidget extends BaseComponent {
  private config: Config = {};

  // filter state
  private search    = '';
  private severity  = '';
  private startTime = '';
  private endTime   = '';
  private selectedPeriodHours = '';

  // data
  private entries: EventEntry[] = [];
  private loading  = true;
  private error    = '';

  // pagination & sort
  private page     = 0;
  private pageSize = 20;
  private sortCol: SortCol = 'timestamp';
  private sortDir: SortDir = 'desc';

  // auto-update
  private updateIntervalMs = 30_000;
  private intervalHandle: ReturnType<typeof setInterval> | null = null;
  private lastFetchedId = 0;
  private initPromise: Promise<void> | null = null;
  private initialized = false;

  // resize
  private resizeObserver: ResizeObserver | null = null;
  private resizePending = false;
  private colResize: {
    startX: number;
    leftCol: ResizableCol;
    rightCol: ResizableCol;
    leftWidth: number;
    rightWidth: number;
  } | null = null;

  // tooltip
  private tooltip: HTMLElement | null = null;

  private ensureTooltip(): void {
    if (this.tooltip) return;
    this.tooltip = document.createElement('div');
    this.tooltip.className = 'evw-tooltip';
    this.tooltip.style.cssText = `
      position: fixed; pointer-events: none; z-index: 9999;
      display: none; max-width: 500px; padding: 6px 10px; border-radius: 4px;
      background: var(--panel-bg, #111); border: 1px solid var(--border-color);
      color: var(--content-text); font-size: var(--widget-label-font-size);
      white-space: pre-wrap; word-break: break-word;
      box-shadow: 0 4px 12px rgba(0,0,0,0.4);
    `;
    document.body.appendChild(this.tooltip);
  }

  setConfig(c: Partial<Config> & Record<string, any>): void {
    const next: Config = {};
    for (const col of RESIZABLE_COLUMNS) {
      const key = CONFIG_KEY_BY_COL[col];
      if (Number.isFinite(Number(c[key]))) {
        Object.assign(next, {
          [key]: Math.max(MIN_RESIZABLE_COL_WIDTH_PX, Math.round(Number(c[key]))),
        });
      }
    }
    this.config = next;
    this.rerender();
  }

  getConfig(): Config {
    return { ...this.config };
  }

  // ── Lifecycle ──────────────────────────────────────────────────────────────

  connectedCallback(): void {
    super.connectedCallback();
    this.setupResizeObserver();
    if (!this.initPromise) {
      this.initPromise = this.init();
    } else if (this.initialized) {
      this.startAutoUpdate();
    }
  }

  private async init(): Promise<void> {
    if (!await can('events.read')) {
      this.innerHTML = `<div style="padding:2rem;text-align:center;opacity:.4;font-size:.8rem">Insufficient permissions</div>`;
      this.initialized = true;
      return;
    }
    await this.fetch();
    this.initialized = true;
    if (this.isConnected) this.startAutoUpdate();
  }

  // ── Data fetching ──────────────────────────────────────────────────────────

  private buildFilter(): EventFilter {
    const f: EventFilter = { limit: 1000 };
    if (this.search)    f.search    = this.search;
    if (this.severity)  f.severity  = this.severity;
    if (this.startTime) f.startTime = new Date(this.startTime).toISOString();
    if (this.endTime)   f.endTime   = new Date(this.endTime).toISOString();
    return f;
  }

  private async fetch(): Promise<void> {
    this.loading = true;
    this.error   = '';
    this.rerender();
    try {
      this.entries = await queryEvents(this.buildFilter());
      this.lastFetchedId = this.entries.length
        ? this.entries[0].id   // newest first from server
        : 0;
      this.page = 0;
    } catch (e: any) {
      this.error = e?.message ?? 'Failed to load events';
    } finally {
      this.loading = false;
    }
    this.rerender();
  }

  /** Incremental update - fetch only records with id > lastFetchedId. */
  private async fetchIncremental(): Promise<void> {
    if (!this.lastFetchedId) { await this.fetch(); return; }
    try {
      const f = this.buildFilter();
      f.afterId = this.lastFetchedId;
      const fresh = await queryEvents(f);
      if (fresh.length) {
        this.entries = [...fresh, ...this.entries].slice(0, 1000);
        this.lastFetchedId = fresh[0].id;
        this.rerender();
      }
    } catch { /* silent on incremental errors */ }
  }

  private startAutoUpdate(): void {
    this.stopAutoUpdate();
    if (this.updateIntervalMs > 0) {
      this.intervalHandle = setInterval(() => this.fetchIncremental(), this.updateIntervalMs);
    }
  }

  private stopAutoUpdate(): void {
    if (this.intervalHandle !== null) {
      clearInterval(this.intervalHandle);
      this.intervalHandle = null;
    }
  }

  private setupResizeObserver(): void {
    if (this.resizeObserver) return;
    this.resizeObserver = new ResizeObserver(() => {
      if (this.resizePending) return;
      this.resizePending = true;
      requestAnimationFrame(() => {
        this.resizePending = false;
        const prev = this.pageSize;
        this.recalcPageSize();
        if (this.pageSize !== prev) this.rerender();
      });
    });
    this.resizeObserver.observe(this);
  }

  // ── Sorting & pagination ───────────────────────────────────────────────────

  private getSorted(): EventEntry[] {
    const dir = this.sortDir === 'asc' ? 1 : -1;
    return [...this.entries].sort((a, b) => {
      let av: string, bv: string;
      switch (this.sortCol) {
        case 'timestamp': av = a.timestamp;         bv = b.timestamp;         break;
        case 'severity':  av = a.severity;          bv = b.severity;          break;
        case 'userName':  av = a.userName ?? '';    bv = b.userName ?? '';    break;
        case 'device':    av = a.device;            bv = b.device;            break;
        default:          av = a.timestamp;         bv = b.timestamp;
      }
      return av < bv ? -dir : av > bv ? dir : 0;
    });
  }

  private recalcPageSize(): void {
    const h = this.clientHeight;
    if (h > 0) {
      const rows = Math.max(5, Math.floor((h - CHROME_PX) / ROW_HEIGHT_PX));
      if (rows !== this.pageSize) {
        this.pageSize = rows;
        this.page = 0;
      }
    }
  }

  // ── Rerender ───────────────────────────────────────────────────────────────

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  // ── Render ─────────────────────────────────────────────────────────────────

  render(): void {
    this.recalcPageSize();
    const sorted = this.getSorted();
    const totalPages = Math.max(1, Math.ceil(sorted.length / this.pageSize));
    if (this.page >= totalPages) this.page = totalPages - 1;
    const pageRows  = sorted.slice(this.page * this.pageSize, (this.page + 1) * this.pageSize);
    const colStyle = (col: ResizableCol) => {
      const width = this.config[CONFIG_KEY_BY_COL[col]];
      return width ? ` style="width:${width}px"` : '';
    };

    this.innerHTML = `
<style>
  events-viewer-widget {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    font-family: var(--widget-font-family);
    font-size: var(--widget-label-font-size);
    color: var(--content-text);
    background: var(--widget-bg);
  }

  /* ── Filter bar ── */
  .evw-filters {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 8px 10px;
    background: var(--widget-header-bg);
    border-bottom: 1px solid var(--widget-border);
    align-items: center;
    flex-shrink: 0;
  }
  .evw-filters input,
  .evw-filters select {
    background: var(--content-bg);
    border: 1px solid var(--border-color);
    color: var(--content-text);
    font-family: var(--widget-font-family);
    font-size: var(--widget-label-font-size);
    padding: 3px 7px;
    border-radius: 3px;
    outline: none;
    height: 28px;
    transition: border-color .15s;
  }
  .evw-filters input:focus,
  .evw-filters select:focus {
    border-color: var(--accent-color);
  }
  .evw-filters input[type="search"] { width: 170px; }
  .evw-filters select.evw-sel-sev   { width: 110px; }
  .evw-filters select.evw-sel-upd   { width: 80px; }
  .evw-filters input[type="datetime-local"] {
    width: 230px;
    padding-right: 28px;
    box-sizing: border-box;
    color-scheme: dark;
  }
  .evw-filters input[type="datetime-local"]::-webkit-calendar-picker-indicator {
    opacity: 0;
    cursor: pointer;
  }
  .evw-date-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
  }
  .evw-picker-btn {
    position: absolute;
    right: 7px;
    width: 16px;
    height: 16px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--accent-color);
    opacity: .95;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
  }
  .evw-picker-btn:hover,
  .evw-picker-btn:focus-visible {
    opacity: 1;
    outline: none;
  }
  .evw-filters .evw-period-row {
    display: flex; gap: 6px; align-items: center; flex-wrap: wrap;
  }
  .evw-filters label {
    font-size: var(--widget-label-font-size);
    color: var(--footer-text);
    margin-right: 2px;
    white-space: nowrap;
  }
  .evw-btn-fetch {
    background: var(--accent-color);
    color: var(--accent-text);
    border: none;
    border-radius: 3px;
    padding: 3px 12px;
    font-family: var(--widget-font-family);
    font-size: var(--widget-label-font-size);
    cursor: pointer;
    height: 28px;
    transition: background .15s;
  }
  .evw-btn-fetch:hover { background: var(--accent-hover); }

  /* ── Table ── */
  .evw-table-wrap {
    flex: 1;
    overflow-x: auto;
    overflow-y: hidden;
  }
  .evw-table {
    width: 100%;
    border-collapse: collapse;
    table-layout: fixed;
  }
  .evw-table col.col-ts   { width: 152px; }
  .evw-table col.col-sev  { width: 68px; }
  .evw-table col.col-user { width: 110px; }
  .evw-table col.col-dev  { width: 110px; }
  .evw-table col.col-msg  { /* flex */ }
  .evw-table col.col-prm  { width: 190px; }

  .evw-table thead th {
    position: sticky;
    top: 0;
    background: var(--widget-header-bg);
    border-bottom: 1px solid var(--widget-border);
    padding: 6px 8px;
    text-align: left;
    font-size: var(--widget-label-font-size);
    font-weight: var(--widget-label-bold-font-weight);
    letter-spacing: .05em;
    text-transform: uppercase;
    color: var(--footer-text);
    white-space: nowrap;
    z-index: 1;
    user-select: none;
  }
  .evw-table thead th.sortable { cursor: pointer; }
  .evw-table thead th.sortable:hover { color: var(--accent-color); }
  .evw-table thead th .sort-arrow {
    display: inline-block;
    margin-left: 3px;
    opacity: .5;
  }
  .evw-table thead th.active-sort { color: var(--accent-color); }
  .evw-table thead th.active-sort .sort-arrow { opacity: 1; }
  .evw-th-message { position: sticky; }
  .evw-col-resizer {
    position: absolute;
    top: 0;
    right: -4px;
    width: 8px;
    height: 100%;
    cursor: col-resize;
    z-index: 2;
    touch-action: none;
  }
  .evw-col-resizer::after {
    content: "";
    position: absolute;
    top: 20%;
    bottom: 20%;
    left: 3px;
    width: 2px;
    border-radius: 2px;
    background: color-mix(in srgb, var(--border-color) 70%, transparent);
  }
  .evw-col-resizer:hover::after,
  .evw-col-resizer.resizing::after {
    background: var(--accent-color);
  }

  .evw-table tbody tr {
    border-bottom: 1px solid color-mix(in srgb, var(--border-color) 50%, transparent);
    transition: background .1s;
  }
  .evw-table tbody tr:nth-child(even) {
    background: color-mix(in srgb, var(--accent-color) 3%, transparent);
  }
  .evw-table tbody tr:hover {
    background: color-mix(in srgb, var(--accent-color) 8%, var(--widget-bg));
  }
  .evw-table tbody td {
    padding: 5px 8px;
    vertical-align: top;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--widget-label-font-size);
    font-weight: var(--widget-label-font-weight);
    line-height: 1.45;
  }
  .evw-table tbody td.col-msg { white-space: normal; word-break: break-word; }
  .evw-table tbody td.col-prm { font-size: var(--widget-label-font-size); }

  /* Severity badges */
  .sev-badge {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: var(--widget-label-font-size);
    font-weight: var(--widget-label-bold-font-weight);
    letter-spacing: .04em;
    text-transform: uppercase;
    white-space: nowrap;
  }
  .sev-DEBUG    { background: color-mix(in srgb,#64748b 18%,transparent); color:#94a3b8; border:1px solid #475569; }
  .sev-INFO     { background: color-mix(in srgb,var(--accent-color) 15%,transparent); color:var(--accent-color); border:1px solid color-mix(in srgb,var(--accent-color) 40%,transparent); }
  .sev-WARN     { background: color-mix(in srgb,var(--status-warn-color) 15%,transparent); color:var(--status-warn-color); border:1px solid color-mix(in srgb,var(--status-warn-color) 40%,transparent); }
  .sev-ERROR    { background: color-mix(in srgb,var(--status-bad-color) 15%,transparent); color:var(--status-bad-color); border:1px solid color-mix(in srgb,var(--status-bad-color) 40%,transparent); }
  .sev-CRITICAL { background: #ff1744; color: #fff; border:1px solid #b71c1c; font-weight: 700; letter-spacing: .06em; }

  /* Params chips */
  .param-chip {
    display: inline-block;
    background: color-mix(in srgb,var(--border-color) 60%,transparent);
    border-radius: 2px;
    padding: 0 5px;
    margin: 1px 2px 1px 0;
    font-size: var(--widget-label-font-size);
    color: var(--footer-text);
    white-space: nowrap;
  }
  .param-chip .pk { color: var(--content-text); opacity:.7; }
  .param-chip .pv { color: var(--accent-color); }

  /* Empty / loading / error states */
  .evw-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 80px;
    color: var(--footer-text);
    font-size: var(--widget-label-font-size);
    letter-spacing: .04em;
  }
  .evw-spinner {
    width: 14px; height: 14px;
    border: 2px solid var(--border-color);
    border-top-color: var(--accent-color);
    border-radius: 50%;
    animation: evw-spin .7s linear infinite;
    margin-right: 8px;
    flex-shrink: 0;
  }
  @keyframes evw-spin { to { transform: rotate(360deg); } }

  /* ── Footer ── */
  .evw-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 5px 10px;
    background: var(--widget-header-bg);
    border-top: 1px solid var(--widget-border);
    flex-shrink: 0;
    gap: 8px;
  }
  .evw-footer-left { display: flex; align-items: center; gap: 8px; }
  .evw-footer-right { display: flex; align-items: center; gap: 6px; }

  .evw-export-btn {
    background: transparent;
    border: 1px solid var(--border-color);
    color: var(--content-text);
    font-family: var(--widget-font-family);
    font-size: var(--widget-label-font-size);
    padding: 2px 10px;
    border-radius: 3px;
    cursor: pointer;
    transition: border-color .15s, color .15s;
    display: flex; align-items: center; gap: 4px;
  }
  .evw-export-btn:hover { border-color: var(--accent-color); color: var(--accent-color); }

  .evw-count { font-size: var(--widget-label-font-size); color: var(--content-text); opacity: .82; }

  .evw-page-btn {
    background: transparent;
    border: 1px solid var(--border-color);
    color: var(--content-text);
    font-family: var(--widget-font-family);
    font-size: var(--widget-label-font-size);
    width: 26px; height: 26px;
    border-radius: 3px;
    cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    transition: border-color .15s, color .15s;
    padding: 0;
  }
  .evw-page-btn:hover:not(:disabled) { border-color: var(--accent-color); color: var(--accent-color); }
  .evw-page-btn:disabled { opacity: .3; cursor: default; }
  .evw-page-info { font-size: var(--widget-label-font-size); color: var(--content-text); opacity: .82; min-width: 70px; text-align: center; }
</style>

<!-- Filter bar -->
<div class="evw-filters">
  <input type="search" class="evw-search" placeholder="Search events…" value="${this.esc(this.search)}">
  <select class="evw-sel-sev">
    ${['', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL'].map(s =>
      `<option value="${s}" ${this.severity === s ? 'selected' : ''}>${s || 'All severities'}</option>`
    ).join('')}
  </select>
  <label>From</label>
  <span class="evw-date-wrap">
    <input type="datetime-local" class="evw-start" lang="en-GB" value="${this.startTime}">
    <button type="button" class="evw-picker-btn" data-target=".evw-start" aria-label="Open start date picker" title="Open date picker">
      ${this.calendarIcon()}
    </button>
  </span>
  <label>To</label>
  <span class="evw-date-wrap">
    <input type="datetime-local" class="evw-end" lang="en-GB" value="${this.endTime}">
    <button type="button" class="evw-picker-btn" data-target=".evw-end" aria-label="Open end date picker" title="Open date picker">
      ${this.calendarIcon()}
    </button>
  </span>
  <div class="evw-period-row">
    <select class="evw-sel-period">
      <option value="">- period -</option>
      ${PERIODS.map(p => {
        const value = String(p.hours);
        return `<option value="${value}" ${this.selectedPeriodHours === value ? 'selected' : ''}>${p.label}</option>`;
      }).join('')}
    </select>
    <select class="evw-sel-upd">
      ${UPDATE_INTERVALS.map(u =>
        `<option value="${u.ms}" ${this.updateIntervalMs === u.ms ? 'selected' : ''}>${u.label}</option>`
      ).join('')}
    </select>
    <button class="evw-btn-fetch">↺ Fetch</button>
  </div>
</div>

<!-- Table -->
<div class="evw-table-wrap">
  ${this.loading ? `
    <div class="evw-state"><div class="evw-spinner"></div>Loading…</div>
  ` : this.error ? `
    <div class="evw-state" style="color:var(--status-bad-color)">⚠ ${this.esc(this.error)}</div>
  ` : sorted.length === 0 ? `
    <div class="evw-state">No events match the current filter</div>
  ` : `
    <table class="evw-table">
      <colgroup>
        <col class="col-ts"${colStyle('timestamp')}>
        <col class="col-sev"${colStyle('severity')}>
        <col class="col-user"${colStyle('userName')}>
        <col class="col-dev"${colStyle('device')}>
        <col class="col-msg"${colStyle('message')}>
        <col class="col-prm"${colStyle('params')}>
      </colgroup>
      <thead>
        <tr>
          ${this.thSort('timestamp', 'Timestamp', true)}
          ${this.thSort('severity',  'Severity', true)}
          ${this.thSort('userName',  'User', true)}
          ${this.thSort('device',    'Device', true)}
          <th data-col="message">Message${this.resizer('message')}</th>
          <th data-col="params">Parameters</th>
        </tr>
      </thead>
      <tbody>
        ${pageRows.map(e => this.renderRow(e)).join('')}
      </tbody>
    </table>
  `}
</div>

<!-- Footer -->
<div class="evw-footer">
  <div class="evw-footer-left">
    <button class="evw-export-btn" title="Export to XLSX">
      <svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor">
        <path d="M8 11L3 6h3V1h4v5h3L8 11z"/>
        <path d="M1 13h14v2H1v-2z"/>
      </svg>
      Export XLSX
    </button>
    <span class="evw-count">${sorted.length.toLocaleString()} records</span>
  </div>
  <div class="evw-footer-right">
    <button class="evw-page-btn evw-prev" ${this.page === 0 ? 'disabled' : ''}>‹</button>
    <span class="evw-page-info">
      ${sorted.length === 0 ? '-' : `${this.page + 1} / ${totalPages}`}
    </span>
    <button class="evw-page-btn evw-next" ${this.page >= totalPages - 1 ? 'disabled' : ''}>›</button>
  </div>
</div>`;
  }

  // ── Render helpers ──────────────────────────────────────────────────────────

  private thSort(col: SortCol, label: string, resizeAfter = false): string {
    const active = this.sortCol === col;
    const arrow  = active ? (this.sortDir === 'asc' ? '▲' : '▼') : '⇕';
    return `<th class="sortable${active ? ' active-sort' : ''}" data-sort="${col}" data-col="${col}">
      ${label}<span class="sort-arrow">${arrow}</span>${resizeAfter ? this.resizer(col) : ''}
    </th>`;
  }

  private resizer(col: ResizableCol): string {
    const idx = RESIZABLE_COLUMNS.indexOf(col);
    const next = RESIZABLE_COLUMNS[idx + 1];
    return next
      ? `<span class="evw-col-resizer" data-col="${col}" title="Resize ${this.columnLabel(col)} and ${this.columnLabel(next)} columns"></span>`
      : '';
  }

  private columnLabel(col: ResizableCol): string {
    switch (col) {
      case 'timestamp': return 'Timestamp';
      case 'severity': return 'Severity';
      case 'userName': return 'User';
      case 'device': return 'Device';
      case 'message': return 'Message';
      case 'params': return 'Parameters';
    }
  }

  private renderRow(e: EventEntry): string {
    const ts = this.formatTs(e.timestamp);
    const sev = this.esc(e.severity);
    const user = this.esc(e.userName || (e.userId ? `uid:${e.userId}` : '-'));
    const dev  = this.esc(e.device || '-');
    const msg  = this.esc(e.message);
    const params = e.params && Object.keys(e.params).length > 0
      ? Object.entries(e.params).map(([k, v]) =>
          `<span class="param-chip"><span class="pk">${this.esc(k)}=</span><span class="pv">${this.esc(String(v))}</span></span>`
        ).join('')
      : '';
    return `<tr>
      <td style="color:var(--content-text);opacity:.85;font-size:10.5px">${ts}</td>
      <td><span class="sev-badge sev-${sev}">${sev}</span></td>
      <td style="color:var(--content-text);opacity:.85">${user}</td>
      <td style="color:var(--content-text);opacity:.85">${dev}</td>
      <td class="col-msg">${msg}</td>
      <td class="col-prm">${params}</td>
    </tr>`;
  }

  private formatTs(iso: string): string {
    try {
      const d = new Date(iso);
      const pad = (n: number) => String(n).padStart(2, '0');
      return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ` +
             `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
    } catch { return iso; }
  }

  private esc(s: string): string {
    return (s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  private calendarIcon(): string {
    return `
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" fill="none"
           stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M8 2v4"></path>
        <path d="M16 2v4"></path>
        <rect x="3" y="4" width="18" height="18" rx="2"></rect>
        <path d="M3 10h18"></path>
      </svg>`;
  }

  // ── Event listeners ────────────────────────────────────────────────────────

  attachEventListeners(): void {
    this.querySelector<HTMLInputElement>('.evw-search')?.addEventListener('input', this._onSearch);
    this.querySelector<HTMLSelectElement>('.evw-sel-sev')?.addEventListener('change', this._onSev);
    this.querySelector<HTMLInputElement>('.evw-start')?.addEventListener('change', this._onStart);
    this.querySelector<HTMLInputElement>('.evw-end')?.addEventListener('change', this._onEnd);
    this.querySelectorAll<HTMLButtonElement>('.evw-picker-btn').forEach(btn =>
      btn.addEventListener('click', this._onPickerClick)
    );
    this.querySelector<HTMLSelectElement>('.evw-sel-period')?.addEventListener('change', this._onPeriod);
    this.querySelector<HTMLSelectElement>('.evw-sel-upd')?.addEventListener('change', this._onUpdate);
    this.querySelector<HTMLButtonElement>('.evw-btn-fetch')?.addEventListener('click', this._onFetch);

    this.querySelectorAll<HTMLTableCellElement>('th[data-sort]').forEach(th =>
      th.addEventListener('click', this._onSort)
    );

    this.querySelector('.evw-prev')?.addEventListener('click', this._onPrev);
    this.querySelector('.evw-next')?.addEventListener('click', this._onNext);
    this.querySelector('.evw-export-btn')?.addEventListener('click', this._onExport);
    this.querySelectorAll('.evw-col-resizer').forEach(handle =>
      handle.addEventListener('pointerdown', this._onColumnResizeStart as EventListener)
    );

    this.querySelector('.evw-table-wrap')?.addEventListener('mouseover', this._onCellHover as EventListener);
    this.querySelector('.evw-table-wrap')?.addEventListener('mouseout', this._onCellHoverEnd as EventListener);
    this.querySelector('.evw-table-wrap')?.addEventListener('mousemove', this._onCellHoverMove as EventListener);
  }

  detachEventListeners(): void {
    this.querySelector<HTMLInputElement>('.evw-search')?.removeEventListener('input', this._onSearch);
    this.querySelector<HTMLSelectElement>('.evw-sel-sev')?.removeEventListener('change', this._onSev);
    this.querySelector<HTMLInputElement>('.evw-start')?.removeEventListener('change', this._onStart);
    this.querySelector<HTMLInputElement>('.evw-end')?.removeEventListener('change', this._onEnd);
    this.querySelectorAll<HTMLButtonElement>('.evw-picker-btn').forEach(btn =>
      btn.removeEventListener('click', this._onPickerClick)
    );
    this.querySelector<HTMLSelectElement>('.evw-sel-period')?.removeEventListener('change', this._onPeriod);
    this.querySelector<HTMLSelectElement>('.evw-sel-upd')?.removeEventListener('change', this._onUpdate);
    this.querySelector<HTMLButtonElement>('.evw-btn-fetch')?.removeEventListener('click', this._onFetch);
    this.querySelectorAll<HTMLTableCellElement>('th[data-sort]').forEach(th =>
      th.removeEventListener('click', this._onSort)
    );
    this.querySelector('.evw-prev')?.removeEventListener('click', this._onPrev);
    this.querySelector('.evw-next')?.removeEventListener('click', this._onNext);
    this.querySelector('.evw-export-btn')?.removeEventListener('click', this._onExport);
    this.querySelectorAll('.evw-col-resizer').forEach(handle =>
      handle.removeEventListener('pointerdown', this._onColumnResizeStart as EventListener)
    );
    document.removeEventListener('pointermove', this._onColumnResizeMove);
    document.removeEventListener('pointerup', this._onColumnResizeEnd);

    this.querySelector('.evw-table-wrap')?.removeEventListener('mouseover', this._onCellHover as EventListener);
    this.querySelector('.evw-table-wrap')?.removeEventListener('mouseout', this._onCellHoverEnd as EventListener);
    this.querySelector('.evw-table-wrap')?.removeEventListener('mousemove', this._onCellHoverMove as EventListener);
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this.stopAutoUpdate();
    this.resizeObserver?.disconnect();
    this.resizeObserver = null;
    this.endColumnResize(false);
  }

  // ── Bound event handlers ───────────────────────────────────────────────────

  private _onSearch = (e: Event): void => {
    this.search = (e.target as HTMLInputElement).value;
  };

  private _onSev = (e: Event): void => {
    this.severity = (e.target as HTMLSelectElement).value;
  };

  private _onStart = (e: Event): void => {
    this.startTime = (e.target as HTMLInputElement).value;
    this.selectedPeriodHours = '';
  };

  private _onEnd = (e: Event): void => {
    this.endTime = (e.target as HTMLInputElement).value;
    this.selectedPeriodHours = '';
  };

  private _onPickerClick = (e: Event): void => {
    const selector = (e.currentTarget as HTMLElement).dataset.target;
    const input = selector ? this.querySelector<HTMLInputElement>(selector) : null;
    if (!input) return;
    input.focus();
    const pickerInput = input as HTMLInputElement & { showPicker?: () => void };
    if (typeof pickerInput.showPicker === 'function') {
      try { pickerInput.showPicker(); } catch { /* focus fallback is enough */ }
    }
  };

  private _onPeriod = (e: Event): void => {
    this.selectedPeriodHours = (e.target as HTMLSelectElement).value;
    const h = Number(this.selectedPeriodHours);
    if (!h) return;
    const now  = new Date();
    const from = new Date(now.getTime() - h * 3_600_000);
    const fmt = (d: Date) => {
      const pad = (n: number) => String(n).padStart(2, '0');
      return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
    };
    this.startTime = fmt(from);
    this.endTime   = fmt(now);
    this.rerender();
  };

  private _onUpdate = (e: Event): void => {
    this.updateIntervalMs = Number((e.target as HTMLSelectElement).value);
    this.startAutoUpdate();
  };

  private _onFetch = (): void => {
    this.fetch();
  };

  private _onSort = (e: Event): void => {
    if ((e.target as HTMLElement).closest('.evw-col-resizer')) return;
    const col = (e.currentTarget as HTMLElement).dataset.sort as SortCol;
    if (this.sortCol === col) {
      this.sortDir = this.sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      this.sortCol = col;
      this.sortDir = col === 'timestamp' ? 'desc' : 'asc';
    }
    this.page = 0;
    this.rerender();
  };

  private _onPrev = (): void => { this.page = Math.max(0, this.page - 1); this.rerender(); };
  private _onNext = (): void => {
    const max = Math.max(0, Math.ceil(this.entries.length / this.pageSize) - 1);
    this.page = Math.min(max, this.page + 1);
    this.rerender();
  };

  private _onColumnResizeStart = (e: PointerEvent): void => {
    const leftCol = (e.currentTarget as HTMLElement).dataset.col as ResizableCol | undefined;
    if (!leftCol) return;
    const leftIdx = RESIZABLE_COLUMNS.indexOf(leftCol);
    const rightCol = RESIZABLE_COLUMNS[leftIdx + 1];
    if (leftIdx < 0 || !rightCol) return;

    const leftTh = this.querySelector<HTMLElement>(`th[data-col="${leftCol}"]`);
    const rightTh = this.querySelector<HTMLElement>(`th[data-col="${rightCol}"]`);
    if (!leftTh || !rightTh) return;

    e.preventDefault();
    e.stopPropagation();

    const leftWidth = leftTh.getBoundingClientRect().width;
    const rightWidth = rightTh.getBoundingClientRect().width;
    if (leftWidth <= 0 || rightWidth <= 0) return;

    this.colResize = {
      startX: e.clientX,
      leftCol,
      rightCol,
      leftWidth,
      rightWidth,
    };
    (e.currentTarget as HTMLElement).classList.add('resizing');
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    document.addEventListener('pointermove', this._onColumnResizeMove);
    document.addEventListener('pointerup', this._onColumnResizeEnd);
  };

  private _onColumnResizeMove = (e: PointerEvent): void => {
    if (!this.colResize) return;
    e.preventDefault();

    const total = this.colResize.leftWidth + this.colResize.rightWidth;
    const maxLeftWidth = Math.max(MIN_RESIZABLE_COL_WIDTH_PX, total - MIN_RESIZABLE_COL_WIDTH_PX);
    const leftWidth = Math.min(
      maxLeftWidth,
      Math.max(MIN_RESIZABLE_COL_WIDTH_PX, this.colResize.leftWidth + e.clientX - this.colResize.startX)
    );
    const rightWidth = total - leftWidth;
    this.config = {
      ...this.config,
      [CONFIG_KEY_BY_COL[this.colResize.leftCol]]: Math.round(leftWidth),
      [CONFIG_KEY_BY_COL[this.colResize.rightCol]]: Math.round(rightWidth),
    };
    this.applyColumnWidths();
  };

  private _onColumnResizeEnd = (): void => {
    this.endColumnResize(true);
  };

  private endColumnResize(save: boolean): void {
    if (!this.colResize) return;
    this.colResize = null;
    this.querySelectorAll('.evw-col-resizer').forEach(handle => handle.classList.remove('resizing'));
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
    document.removeEventListener('pointermove', this._onColumnResizeMove);
    document.removeEventListener('pointerup', this._onColumnResizeEnd);
    if (save) {
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
    }
  }

  private applyColumnWidths(): void {
    for (const col of RESIZABLE_COLUMNS) {
      const width = this.config[CONFIG_KEY_BY_COL[col]];
      const tableCol = this.querySelector<HTMLTableColElement>(`col.${COL_CLASS_BY_ID[col]}`);
      if (tableCol && width) {
        tableCol.style.width = `${width}px`;
      }
    }
  }

  private _onExport = async (): Promise<void> => {
    try {
      await loadXlsx();
      const XLSX = (window as any).XLSX;
      const sorted = this.getSorted();
      const rows = sorted.map(e => ({
        Timestamp:    this.formatTs(e.timestamp),
        Severity:     e.severity,
        Organisation: e.orgName || '',
        User:         e.userName || (e.userId ? String(e.userId) : ''),
        Device:       e.device,
        Message:      e.message,
        Parameters:   e.params ? JSON.stringify(e.params) : '',
      }));
      const ws = XLSX.utils.json_to_sheet(rows);
      const wb = XLSX.utils.book_new();
      XLSX.utils.book_append_sheet(wb, ws, 'Events');
      XLSX.writeFile(wb, `events-${new Date().toISOString().slice(0,19).replace(/:/g,'-')}.xlsx`);
    } catch (err: any) {
      await showAlert(`Export failed: ${err?.message ?? err}`, {
        title: 'Export failed',
        tone: 'danger',
      });
    }
  };

  private _onCellHover = (e: MouseEvent): void => {
    this.ensureTooltip();
    const target = e.target as HTMLElement;
    const cell = target.closest('td');
    if (!cell || !this.tooltip) return;
    if (cell.scrollWidth <= cell.clientWidth && cell.textContent && cell.textContent.length < 80) {
      this.tooltip.style.display = 'none';
      return;
    }
    const rawText = cell.textContent ?? '';
    const fullText = rawText.trim();
    this.tooltip.textContent = fullText;
    this.tooltip.style.display = 'block';
  };

  private _onCellHoverEnd = (): void => {
    if (this.tooltip) this.tooltip.style.display = 'none';
  };

  private _onCellHoverMove = (e: MouseEvent): void => {
    this.ensureTooltip();
    if (!this.tooltip || this.tooltip.style.display === 'none') return;
    const TW = this.tooltip.offsetWidth;
    const TH = this.tooltip.offsetHeight;
    const x = e.clientX;
    const y = e.clientY;
    this.tooltip.style.left = `${Math.max(8, Math.min(x + 12, window.innerWidth - TW - 8))}px`;
    this.tooltip.style.top = `${Math.max(8, Math.min(y - TH - 8, window.innerHeight - TH - 8))}px`;
  };
}

customElements.define('events-viewer-widget', EventsViewerWidget);
