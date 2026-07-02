import { BaseComponent } from '../../components/base-component';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { getAuthHeaders } from '../../auth';
import { registerWidgetType } from './widget-registry';
import { init, type ECharts, type EChartsOption } from './echarts-line';
import type { PropertyField } from './widget-properties-dialog';
import { resolveMetricTagPath } from './tag-path-resolver';
import { cloneValue } from '../../utils/clone';

// ── Constants ─────────────────────────────────────────────────────────────────

const API_BASE = '/xact';
const MAX_POINTS = 2000;

// ── Types ─────────────────────────────────────────────────────────────────────

interface DataPoint { t: number; v: unknown; }

interface Config {
  headerText: string;
  tagPrefix: string;
  tagPath: string;
  barColor: string;
  timePeriod: number;
  useUiTimeRange: boolean;
  showZoomControl: boolean;
  refreshInterval: number;
}

const DEFAULT_CONFIG: Config = {
  headerText: 'Status',
  tagPrefix: '',
  tagPath: '',
  barColor: '#22d3ee',
  timePeriod: 24,
  useUiTimeRange: false,
  showZoomControl: true,
  refreshInterval: 0,
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

// ── Widget ────────────────────────────────────────────────────────────────────

export class BinaryStatusLineWidget extends BaseComponent {
  private config: Config = cloneValue(DEFAULT_CONFIG);
  private data: DataPoint[] = [];
  private chart: ECharts | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private subscriptionActive = false;
  private _storeUnsub: (() => void) | null = null;
  private _deviceNameUnsub: (() => void) | null = null;
  private _uiTimeUnsubs: Array<() => void> = [];
  private _refreshTimer: ReturnType<typeof setInterval> | null = null;
  private _applyFrame: number | null = null;

  // Tooltip (ZRender-based)
  private _tip: HTMLDivElement | null = null;
  private _zrMouseMove: ((e: any) => void) | null = null;
  private _zrGlobalOut: (() => void) | null = null;

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = {
      ...this.config,
      ...c,
      showZoomControl: c.showZoomControl ?? this.config.showZoomControl,
      refreshInterval: Number(c.refreshInterval ?? this.config.refreshInterval) || 0,
    };
    this.data = [];
    this.rerender();
  }

  getPropertySchema(): PropertyField[] {
    const inArray = !!(this.config as any).arrayElementPath;
    return [
      { name: 'headerText',     type: 'string',  label: 'Header text',                  default: 'Status' },
      ...(!inArray ? [{ name: 'tagPrefix', type: 'path' as const, label: 'Tag prefix (use * for dashboard device name)', default: '', context: { includeLeaves: false } }] : []),
      { name: 'tagPath',        type: 'path',    label: 'Tag path',                     default: '', context: { includeLeaves: true, rootFromField: 'tagPrefix', stripBrowseRoot: true } },
      { name: 'barColor',       type: 'color',   label: 'Bar color',                    default: '#22d3ee' },
      { name: 'timePeriod',     type: 'number',  label: 'History period (hours)',        default: 24 },
      { name: 'useUiTimeRange', type: 'boolean', label: 'Use UI time range',            default: false },
      { name: 'showZoomControl', type: 'boolean', label: 'Show zoom control',            default: true },
      {
        name: 'refreshInterval', type: 'select', label: 'Refresh interval', default: '0',
        context: { options: [
          { value: '0',   label: 'Off' },
          { value: '30',  label: '30 s' },
          { value: '60',  label: '1 min' },
          { value: '300', label: '5 min' },
          { value: '600', label: '10 min' },
        ]},
      },
    ];
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  protected render(): void {
    this.innerHTML = `
      <div style="position:relative; width:100%; height:100%; box-sizing:border-box;">
        <div id="bsl-chart" style="position:absolute;inset:0;"></div>
      </div>`;
    this.updateCardTitle();
  }

  protected attachEventListeners(): void {
    this.subscriptionActive = true;

    const container = this.querySelector<HTMLDivElement>('#bsl-chart');
    if (!container) return;

    container.addEventListener('mousedown',  e => e.stopPropagation());
    container.addEventListener('touchstart', e => e.stopPropagation(), { passive: true });

    this.chart = init(container, null, { renderer: 'canvas' });
    this.chart.setOption(this.buildChartOption());

    this.resizeObserver = new ResizeObserver(() => requestAnimationFrame(() => this.chart?.resize()));
    this.resizeObserver.observe(container);

    this._attachTooltip(container);

    if (this.config.useUiTimeRange) {
      const uiStore = getUiStore();
      const reload = () => { this.data = []; this.loadData(); };
      this._uiTimeUnsubs.push(
        uiStore.subscribe('timeStart', reload),
        uiStore.subscribe('timeEnd',   reload),
      );
    }

    if (this.config.tagPrefix.includes('*')) {
      let skipInitialDeviceName = getUiStore().get('deviceName') !== '';
      this._deviceNameUnsub = getUiStore().subscribe('deviceName', () => {
        if (skipInitialDeviceName) {
          skipInitialDeviceName = false;
          return;
        }
        this.data = [];
        this.rerender();
      });
    }

    const fullPath = this.resolveTagPath();
    if (fullPath) {
      this._storeUnsub = getMirrorStore().subscribeTagReference(fullPath, (value: any) => {
        if (!this.subscriptionActive) return;
        this.appendLiveValue(value);
      });
    }

    this.loadData();

    if (this.config.refreshInterval > 0) {
      this._refreshTimer = setInterval(() => {
        if (this.subscriptionActive) this.loadData();
      }, this.config.refreshInterval * 1000);
    }
  }

  protected detachEventListeners(): void {
    this.subscriptionActive = false;
    this._storeUnsub?.();
    this._storeUnsub = null;

    if (this._refreshTimer !== null) {
      clearInterval(this._refreshTimer);
      this._refreshTimer = null;
    }
    if (this._applyFrame !== null) {
      cancelAnimationFrame(this._applyFrame);
      this._applyFrame = null;
    }

    this._detachTooltip();

    this.resizeObserver?.disconnect();
    this.resizeObserver = null;
    this.chart?.dispose();
    this.chart = null;
    this._deviceNameUnsub?.();
    this._deviceNameUnsub = null;
    this._uiTimeUnsubs.forEach(u => u());
    this._uiTimeUnsubs = [];
  }

  // ── Custom tooltip (ZRender-based) ──────────────────────────────────────────

  /**
   * Attach a tooltip driven by eCharts' own ZRender mouse events.
   *
   * DOM-level mousemove is unreliable here because:
   *  - chart.containPixel() may not be ready immediately after init
   *  - coordinate frames can mismatch across browsers
   *
   * ZRender events fire entirely within the chart canvas, offsetX/offsetY are
   * already in the canvas coordinate system, and the chart is guaranteed
   * initialised before any ZRender event fires.
   *
   * The eCharts trigger:'axis' tooltip is also unsuitable: it only fires near
   * actual data points, so a binary signal that stays ON for hours (with no
   * intermediate samples) would produce no tooltip over most of its span.
   */
  private _attachTooltip(container: HTMLElement): void {
    const barColor    = this.config.barColor || cssVar('--accent-color') || '#22d3ee';
    const contentText = cssVar('--content-text') || '#e2e8f0';
    const panelBg     = cssVar('--panel-bg') || cssVar('--widget-bg') || '#131f38';
    const borderColor = cssVar('--border-color') || '#2b3a53';
    const axisFs      = parseInt(cssVar('--widget-label-font-size')) || 11;

    const tip = document.createElement('div');
    tip.style.cssText = `
      display:none; position:fixed; pointer-events:none; z-index:9999;
      padding:6px 10px; border-radius:4px;
      background:${panelBg}; border:1px solid ${borderColor};
      font-family:ui-monospace,'Cascadia Code','SF Mono','Menlo','Consolas',monospace;
      font-size:${axisFs}px; color:${contentText};
      white-space:nowrap; line-height:1.7;
      box-shadow:0 4px 16px rgba(0,0,0,0.55);
    `;
    document.body.appendChild(tip);

    this._zrMouseMove = (zrEvent: any) => {
      if (!this.chart || this.data.length === 0) { tip.style.display = 'none'; return; }

      const px = zrEvent.offsetX as number;
      const py = zrEvent.offsetY as number;

      // Hide when cursor is over the dataZoom slider or outside the grid
      let inGrid = false;
      try { inGrid = this.chart.containPixel('grid', [px, py]); } catch { /* ignore */ }
      if (!inGrid) { tip.style.display = 'none'; return; }

      // Convert canvas pixel → [timestamp, value]
      let coords: number[] | null = null;
      try { coords = this.chart.convertFromPixel('grid', [px, py]) as number[]; } catch { /* ignore */ }
      if (!coords || isNaN(coords[0])) { tip.style.display = 'none'; return; }

      const timeMs = coords[0];
      const isOn   = this._getStateAt(timeMs);
      const d      = new Date(timeMs);
      const dateStr = d.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit', year: 'numeric' });
      const timeStr = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
      const stateHtml = isOn
        ? `<span style="color:${barColor};font-weight:600;">ON</span>`
        : `<span style="opacity:0.45;">OFF</span>`;

      tip.innerHTML = `
        <div style="opacity:0.55;margin-bottom:2px;">${dateStr}</div>
        <div style="opacity:0.55;margin-bottom:4px;">${timeStr}</div>
        <div>${stateHtml}</div>
      `;
      tip.style.display = 'block';

      // Position above cursor, clamped to viewport
      const rect  = container.getBoundingClientRect();
      const cx    = rect.left + px;
      const cy    = rect.top  + py;
      const TW    = tip.offsetWidth  || 120;
      const TH    = tip.offsetHeight || 72;
      tip.style.left = `${Math.max(8, Math.min(cx - TW / 2, window.innerWidth  - TW - 8))}px`;
      tip.style.top  = `${Math.max(8, cy - TH - 10)}px`;
    };

    this._zrGlobalOut = () => { tip.style.display = 'none'; };

    this.chart!.getZr().on('mousemove', this._zrMouseMove);
    this.chart!.getZr().on('globalout', this._zrGlobalOut);

    this._tip = tip;
  }

  private _detachTooltip(): void {
    if (this.chart) {
      if (this._zrMouseMove) this.chart.getZr().off('mousemove', this._zrMouseMove);
      if (this._zrGlobalOut) this.chart.getZr().off('globalout', this._zrGlobalOut);
    }
    this._tip?.remove();
    this._tip        = null;
    this._zrMouseMove  = null;
    this._zrGlobalOut  = null;
  }

  /**
   * Binary state at a given timestamp using step-end semantics:
   * the state is that of the last recorded data point with t ≤ timeMs.
   */
  private _getStateAt(timeMs: number): boolean {
    for (let i = this.data.length - 1; i >= 0; i--) {
      if (this.data[i].t <= timeMs) return Boolean(this.data[i].v);
    }
    return false;
  }

  // ── Data fetching ───────────────────────────────────────────────────────────

  private startMs(): number {
    if (this.config.useUiTimeRange) {
      const ts = getUiStore().get('timeStart');
      if (ts !== null) return ts;
    }
    return Date.now() - this.config.timePeriod * 3_600_000;
  }

  private endMs(): number | null {
    if (!this.config.useUiTimeRange) return null;
    return getUiStore().get('timeEnd');
  }

  private xAxisRangeOption(): Pick<any, 'min' | 'max'> | null {
    if (!this.config.useUiTimeRange) return null;
    return {
      min: this.startMs(),
      max: this.endMs() ?? undefined,
    };
  }

  private async loadData(): Promise<void> {
    const dm = this.getDeviceAndMetric();
    if (!dm) return;

    const start = new Date(this.startMs()).toISOString();
    const end = this.endMs();
    const url = `${API_BASE}/api/v1/metrics/${encodeURIComponent(dm.device)}`
      + `?start=${encodeURIComponent(start)}`
      + (end !== null ? `&end=${encodeURIComponent(new Date(end).toISOString())}` : '')
      + `&metrics=${encodeURIComponent(dm.metric)}`;

    try {
      const res = await fetch(url, { headers: getAuthHeaders() });
      if (!this.subscriptionActive || !res.ok) return;
      const json = await res.json();
      const series = json.series?.find((s: any) => s.name === dm.metric);
      if (!series?.data?.length) return;
      this.ingestData((series.data as [number, unknown][]).map(([t, v]) => ({ t, v })), true);
      if (this.subscriptionActive) this.applyData();
    } catch { /* non-fatal */ }
  }

  private appendLiveValue(rawValue: unknown): void {
    const value = this.toBinaryValue(rawValue);
    const last = this.data[this.data.length - 1];
    let timestamp = Date.now();

    if (last && timestamp <= last.t) {
      if (last.v === value) return;
      timestamp = last.t + 1;
    }

    this.ingestData([{ t: timestamp, v: value }]);
    this.scheduleApplyData();
  }

  private ingestData(points: DataPoint[], replace = false): void {
    const cutoff = this.startMs();
    const byTimestamp = new Map<number, number>();

    if (!replace) {
      for (const p of this.data) {
        if (p.t >= cutoff) byTimestamp.set(p.t, p.v ? 1 : 0);
      }
    }
    for (const p of points) {
      if (Number.isFinite(p.t) && p.t >= cutoff) byTimestamp.set(p.t, this.toBinaryValue(p.v));
    }

    this.data = Array.from(byTimestamp, ([t, v]) => ({ t, v }))
      .sort((a, b) => a.t - b.t)
      .slice(-MAX_POINTS);
  }

  private scheduleApplyData(): void {
    if (this._applyFrame !== null) return;
    this._applyFrame = requestAnimationFrame(() => {
      this._applyFrame = null;
      if (this.subscriptionActive) this.applyData();
    });
  }

  private toBinaryValue(value: unknown): number {
    if (typeof value === 'boolean') return value ? 1 : 0;
    if (typeof value === 'number') return value ? 1 : 0;
    const s = String(value ?? '').trim().toLowerCase();
    return s !== '' && s !== '0' && s !== 'false' && s !== 'no' ? 1 : 0;
  }

  // ── Chart rendering ─────────────────────────────────────────────────────────

  /**
   * Push current data into the chart.
   *
   * Draw a step line with a solid area fill underneath it. Short ON pulses can
   * still be sub-pixel wide at a 24 h range, so a scatter overlay marks every
   * ON sample.
   */
  private applyData(): void {
    if (!this.chart) return;
    const barColor   = this.config.barColor || cssVar('--accent-color') || '#22d3ee';
    const normalised: [number, number][] = this.data.map(p => [p.t, p.v ? 1 : 0]);
    const xAxisRange = this.xAxisRangeOption();

    // Extend the last known state to wall-clock time so the bar scrolls even
    // when the underlying value is unchanged (server only writes on change).
    if (normalised.length > 0 && !this.config.useUiTimeRange) {
      normalised.push([Date.now(), normalised[normalised.length - 1][1]]);
    }

    this.chart.setOption({
      ...(xAxisRange ? { xAxis: xAxisRange } : {}),
      series: [
        {
          id: 'status',
          type: 'line',
          step: 'end',
          data: normalised,
          showSymbol: false,
          lineStyle: { width: 1.25, color: barColor, opacity: 0.95 },
          areaStyle: { color: barColor, opacity: 1 },
          emphasis: { disabled: true },
        },
        {
          id: 'status-on-events',
          type: 'scatter',
          data: normalised.filter(([, v]) => v === 1),
          symbolSize: 6,
          itemStyle: { color: barColor, opacity: 0.95 },
          emphasis: { disabled: true },
          tooltip: { show: false },
          z: 5,
        },
      ],
    }, { replaceMerge: ['series'] });
  }

  private buildChartOption(): EChartsOption {
    const contentText = cssVar('--content-text') || '#e2e8f0';
    const borderColor = cssVar('--border-color') || '#2b3a53';
    const axisFs      = parseInt(cssVar('--widget-label-font-size')) || 11;
    const barColor    = this.config.barColor || cssVar('--accent-color') || '#22d3ee';

    return {
      backgroundColor: 'transparent',
      animation: false,
      tooltip: { show: false },   // custom ZRender tooltip used instead
      grid: {
        top: 4,
        bottom: this.config.showZoomControl ? 42 : 20,
        left: 8,
        right: 8,
        containLabel: false,
      },
      xAxis: {
        type: 'time',
        axisLine: { lineStyle: { color: borderColor } },
        axisTick: { lineStyle: { color: borderColor } },
        splitLine: { show: false },
        axisLabel: {
          color: contentText,
          opacity: 0.5,
          fontFamily: "ui-monospace,'Cascadia Code','SF Mono',monospace",
          fontSize: axisFs,
          formatter: (val: number) => {
            const d = new Date(val);
            return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
          },
        },
      },
      yAxis: {
        type: 'value',
        min: 0,
        max: 1,
        show: false,
      },
      dataZoom: this.config.showZoomControl ? [
        {
          type: 'slider',
          bottom: 4,
          height: 20,
          borderColor: borderColor,
          backgroundColor: 'transparent',
          dataBackground: {
            lineStyle: { color: barColor, opacity: 0.3, width: 1 },
            areaStyle: { color: barColor, opacity: 0.1 },
          },
          selectedDataBackground: {
            lineStyle: { color: barColor, opacity: 0.6, width: 1 },
            areaStyle: { color: barColor, opacity: 0.2 },
          },
          handleStyle: { color: barColor, borderWidth: 0 },
          moveHandleStyle: { color: barColor, opacity: 0.5 },
          fillerColor: `color-mix(in srgb, ${barColor} 10%, transparent)`,
          textStyle: { color: contentText, fontSize: 9, fontFamily: "ui-monospace,'Cascadia Code',monospace" },
          labelFormatter: (val: number) => {
            const d = new Date(val);
            return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
          },
        },
      ] : [],
      series: [
        {
          id: 'status',
          type: 'line',
          step: 'end',
          data: [],
          showSymbol: false,
          lineStyle: { width: 1.25, color: barColor, opacity: 0.95 },
          areaStyle: { color: barColor, opacity: 1 },
          emphasis: { disabled: true },
        },
        {
          id: 'status-on-events',
          type: 'scatter',
          data: [],
          symbolSize: 6,
          itemStyle: { color: barColor, opacity: 0.95 },
          emphasis: { disabled: true },
          tooltip: { show: false },
          z: 5,
        },
      ],
    };
  }

  // ── Utility ─────────────────────────────────────────────────────────────────

  private resolveTagPath(): string {
    return resolveMetricTagPath(this.config.tagPrefix, this.config.tagPath);
  }

  private getDeviceAndMetric(): { device: string; metric: string } | null {
    const fullPath = this.resolveTagPath();
    if (!fullPath) return null;
    const parts = getMirrorStore().baseTagPath(fullPath).split('.');
    if (parts.length < 4) return null;
    const metric = parts.slice(-2).join('.');
    const device = parts.slice(1, -2).join('.');
    return device ? { device, metric } : null;
  }

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (card && typeof card.setTitle === 'function') {
      card.setTitle(this.config.headerText ?? 'Status');
    }
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }
}

// ── Registration ──────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'binary-status-line-widget',
  name: 'Binary Status Line',
  icon: '▬',
  category: 'Metrics',
  defaultW: 12,
  defaultH: 3,
  minW: 4,
  minH: 2,
});

customElements.define('binary-status-line-widget', BinaryStatusLineWidget);
