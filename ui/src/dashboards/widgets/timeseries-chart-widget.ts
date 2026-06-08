import { BaseComponent } from '../../components/base-component';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { getAuthHeaders } from '../../auth';
import { registerWidgetType } from './widget-registry';
import { graphic, init, type ECharts, type EChartsOption, type SeriesOption, type XAXisComponentOption, type YAXisComponentOption } from './echarts-line';
import type { PropertyField } from './widget-properties-dialog';
import { resolveMetricTagPath } from './tag-path-resolver';

// ── Constants ─────────────────────────────────────────────────────────────────

const API_BASE = '/xact';
const MAX_POINTS = 2000;

// Default palette - five colours complementary to the project theme
const DEFAULT_COLORS = ['#60a5fa', '#22d3ee', '#f59e0b', '#fb7185', '#a3e635'];

// ── Types ─────────────────────────────────────────────────────────────────────

interface DataPoint { t: number; v: number; }

interface SeriesCfg {
  enabled: boolean;
  name: string;
  tagPrefix: string;
  tagPath: string;
  color: string;
  yAxis: 'left' | 'right';
  gradientFill: boolean;
  smooth: boolean;
}

interface Config {
  headerText: string;
  timePeriod: number;        // hours of history to show initially
  useUiTimeRange: boolean;   // use global UI time range instead
  refreshInterval: number;   // seconds between auto-refreshes (0 = off)
  showZoomControl: boolean;  // show the ECharts dataZoom slider
  showGrid: boolean;         // show chart grid lines
  backgroundImage: string;   // URL for chart background (optional)
  series: SeriesCfg[];       // exactly 5 slots
}

function defaultSeries(i: number): SeriesCfg {
  return {
    enabled: false,
    name: `Series ${i + 1}`,
    tagPrefix: '',
    tagPath: '',
    color: DEFAULT_COLORS[i],
    yAxis: 'left',
    gradientFill: true,
    smooth: false,
  };
}

const DEFAULT_CONFIG: Config = {
  headerText: 'Chart',
  timePeriod: 24,
  useUiTimeRange: false,
  refreshInterval: 0,
  showZoomControl: true,
  showGrid: false,
  backgroundImage: '',
  series: Array.from({ length: 5 }, (_, i) => defaultSeries(i)),
};

// ── Helpers ───────────────────────────────────────────────────────────────────

function hexToRgba(hex: string, alpha: number): string {
  const h = hex.replace('#', '');
  const r = parseInt(h.substring(0, 2), 16);
  const g = parseInt(h.substring(2, 4), 16);
  const b = parseInt(h.substring(4, 6), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

/** Read a CSS variable value from :root at the time of call. */
function cssVar(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

type Rgb = { r: number; g: number; b: number };

function parseCssColor(value: string): Rgb | null {
  const color = value.trim();
  if (!color) return null;

  if (color.startsWith('#')) {
    const h = color.slice(1);
    const full = h.length === 3
      ? h.split('').map(ch => `${ch}${ch}`).join('')
      : h;
    if (full.length !== 6 || !/^[0-9a-f]{6}$/i.test(full)) return null;
    return {
      r: parseInt(full.slice(0, 2), 16),
      g: parseInt(full.slice(2, 4), 16),
      b: parseInt(full.slice(4, 6), 16),
    };
  }

  const rgb = color.match(/^rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)/i);
  if (!rgb) return null;
  return {
    r: Math.max(0, Math.min(255, Number(rgb[1]))),
    g: Math.max(0, Math.min(255, Number(rgb[2]))),
    b: Math.max(0, Math.min(255, Number(rgb[3]))),
  };
}

function relativeLuminance({ r, g, b }: Rgb): number {
  const convert = (channel: number) => {
    const c = channel / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * convert(r) + 0.7152 * convert(g) + 0.0722 * convert(b);
}

function isLightThemeSurface(): boolean {
  const surface = cssVar('--panel-bg') || cssVar('--widget-bg') || cssVar('--content-bg') || '#131f38';
  const rgb = parseCssColor(surface);
  return rgb ? relativeLuminance(rgb) > 0.5 : false;
}

// ── Widget ────────────────────────────────────────────────────────────────────

export class TimeseriesChartWidget extends BaseComponent {
  private config: Config = structuredClone(DEFAULT_CONFIG);

  /** Live data storage - one DataPoint[] per series slot (0-4). */
  private seriesData: DataPoint[][] = Array.from({ length: 5 }, () => []);

  /** Last fetched timestamp per device path, used for incremental fetches. */
  private lastTs: Map<string, string> = new Map();

  private chart: ECharts | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private subscriptionActive = false;
  private _storeUnsubs: Array<() => void> = [];
  private _deviceNameUnsub: (() => void) | null = null;
  private _uiTimeUnsubs: Array<() => void> = [];
  private _refreshTimer: ReturnType<typeof setInterval> | null = null;
  private _loadAllAbort: AbortController | null = null;
  private _incrementalAbortByDevice: Map<string, AbortController> = new Map();
  private _incrementalQueuedByDevice: Set<string> = new Set();
  private _uiTimeReloadTimer: ReturnType<typeof setTimeout> | null = null;

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    // Flatten series1*/series2* etc back into the series array when
    // the property dialog serialises them as flat keys.
    const series: SeriesCfg[] = Array.from({ length: 5 }, (_, i) => {
      const k = `series${i + 1}`;
      const existing = this.config.series[i] ?? defaultSeries(i);
      return {
        enabled:      c[`${k}Enabled`]      ?? existing.enabled,
        name:         c[`${k}Name`]         ?? existing.name,
        tagPrefix:    c[`${k}TagPrefix`]    ?? existing.tagPrefix,
        tagPath:      c[`${k}TagPath`]      ?? existing.tagPath,
        color:        c[`${k}Color`]        ?? existing.color,
        yAxis:       (c[`${k}YAxis`]        ?? existing.yAxis) as 'left' | 'right',
        gradientFill: c[`${k}GradientFill`] ?? existing.gradientFill,
        smooth:       c[`${k}Smooth`]       ?? existing.smooth,
      };
    });

    this.config = {
      headerText:      c.headerText      ?? this.config.headerText,
      timePeriod:      c.timePeriod      ?? this.config.timePeriod,
      useUiTimeRange:  c.useUiTimeRange  ?? this.config.useUiTimeRange,
      refreshInterval: Number(c.refreshInterval ?? this.config.refreshInterval) || 0,
      showZoomControl: c.showZoomControl ?? this.config.showZoomControl,
      showGrid:        c.showGrid        ?? this.config.showGrid,
      backgroundImage: c.backgroundImage ?? this.config.backgroundImage,
      series,
    };

    this.seriesData = Array.from({ length: 5 }, () => []);
    this.lastTs.clear();
    if (this.isConnected) this.rerender();
  }

  getPropertySchema(): PropertyField[] {
    const inArray = !!(this.config as any).arrayElementPath;
    const fields: PropertyField[] = [
      { name: 'headerText', type: 'string', label: 'Header text', default: 'Chart' },
      { name: 'timePeriod', type: 'number', label: 'History period (hours)', default: 24 },
      { name: 'useUiTimeRange', type: 'boolean', label: 'Use UI time range', default: false },
      { name: 'showZoomControl', type: 'boolean', label: 'Show zoom control', default: true },
      { name: 'showGrid', type: 'boolean', label: 'Show grid', default: false },
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
      {
        name: 'backgroundImage',
        type: 'string',
        label: 'Background image URL',
        description: 'Optional URL to display behind the chart',
        default: '',
      },
    ];

    for (let i = 0; i < 5; i++) {
      const k = `series${i + 1}`;
      const defaultName = `Series ${i + 1}`;
      const customName  = this.config.series[i]?.name ?? defaultName;
      const sectionLabel = customName !== defaultName ? `${defaultName} - ${customName}` : defaultName;
      fields.push(
        { name: k,               type: 'section', label: sectionLabel },
        { name: `${k}Enabled`,   type: 'boolean', label: 'Enabled',                           default: false },
        { name: `${k}Name`,      type: 'string',  label: 'Name',                              default: `Series ${i + 1}` },
        ...(!inArray ? [{ name: `${k}TagPrefix`, type: 'path' as const, label: 'Tag prefix (use * for dashboard device name)', default: '', context: { includeLeaves: false } }] : []),
        { name: `${k}TagPath`,   type: 'path',    label: 'Tag path',                          default: '', context: { includeLeaves: true, rootFromField: `${k}TagPrefix`, stripBrowseRoot: true } },
        { name: `${k}Color`,     type: 'color',   label: 'Color',                             default: DEFAULT_COLORS[i] },
        {
          name: `${k}YAxis`, type: 'select', label: 'Y axis', default: 'left',
          context: { options: [{ value: 'left', label: 'Left' }, { value: 'right', label: 'Right' }] },
        },
        { name: `${k}GradientFill`, type: 'boolean', label: 'Gradient fill', default: true },
        { name: `${k}Smooth`,       type: 'boolean', label: 'Smooth line',   default: false },
      );
    }

    return fields;
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  protected render(): void {
    const { backgroundImage } = this.config;
    const bgStyle = backgroundImage
      ? `background-image:url(${JSON.stringify(backgroundImage)});background-size:cover;background-position:center;`
      : '';

    this.innerHTML = `
      <div style="
        position:relative; width:100%; height:100%; box-sizing:border-box;
        ${bgStyle}
      ">
        <div id="tcw-chart" style="position:absolute;inset:0;"></div>
      </div>`;

    this.updateCardTitle();
  }

  protected attachEventListeners(): void {
    this.subscriptionActive = true;

    const container = this.querySelector<HTMLDivElement>('#tcw-chart');
    if (!container) return;

    // ── Prevent chart interactions from triggering GridStack widget drag ─────
    // GridStack listens for mousedown/touchstart on the widget element; stopping
    // propagation here means the dataZoom slider and chart panning are handled
    // exclusively by eCharts and never reach the drag handler.
    container.addEventListener('mousedown',  e => e.stopPropagation());
    container.addEventListener('touchstart', e => e.stopPropagation(), { passive: true });

    // ── Initialise eCharts ───────────────────────────────────────────────────
    this.chart = init(container, null, { renderer: 'canvas' });
    this.chart.setOption(this.buildChartOption());

    // ── ResizeObserver ───────────────────────────────────────────────────────
    this.resizeObserver = new ResizeObserver(() => requestAnimationFrame(() => this.chart?.resize()));
    this.resizeObserver.observe(container);

    // ── Subscribe to UI time range if enabled ────────────────────────────────
    if (this.config.useUiTimeRange) {
      const uiStore = getUiStore();
      let initialisingUiTime = true;
      const reload = () => {
        if (initialisingUiTime) return;
        this.scheduleUiTimeReload();
      };
      this._uiTimeUnsubs.push(
        uiStore.subscribe('timeStart', reload),
        uiStore.subscribe('timeEnd', reload),
      );
      initialisingUiTime = false;
    }

    // ── Subscribe to device name changes ────────────────────────────────────
    const hasPrefixedSeries = this.config.series.some(s => s.enabled && s.tagPrefix.includes('*'));
    if (hasPrefixedSeries) {
      let skipInitialDeviceName = getUiStore().get('deviceName') !== '';
      this._deviceNameUnsub = getUiStore().subscribe('deviceName', () => {
        if (skipInitialDeviceName) {
          skipInitialDeviceName = false;
          return;
        }
        this.seriesData = Array.from({ length: 5 }, () => []);
        this.lastTs.clear();
        this.rerender();
      });
    }

    // ── Initial data load ────────────────────────────────────────────────────
    this.loadAllSeries();

    // ── Subscribe to tag values for incremental updates ──────────────────────
    this.config.series.forEach((s, i) => {
      if (!s.enabled) return;
      const fullPath = this.resolveTagPath(i);
      if (!fullPath) return;
      this._storeUnsubs.push(getMirrorStore().subscribeTagReference(fullPath, () => {
        if (!this.subscriptionActive) return;
        this.fetchIncrementalForSeries(i);
      }));
    });

    // ── Periodic refresh timer ───────────────────────────────────────────────
    if (this.config.refreshInterval > 0) {
      this._refreshTimer = setInterval(() => {
        if (this.subscriptionActive) this.loadAllSeries();
      }, this.config.refreshInterval * 1000);
    }
  }

  protected detachEventListeners(): void {
    this.subscriptionActive = false;
    this._storeUnsubs.forEach(unsub => unsub());
    this._storeUnsubs = [];

    if (this._refreshTimer !== null) {
      clearInterval(this._refreshTimer);
      this._refreshTimer = null;
    }

    this.resizeObserver?.disconnect();
    this.resizeObserver = null;

    this.chart?.dispose();
    this.chart = null;

    this._deviceNameUnsub?.();
    this._deviceNameUnsub = null;

    this._uiTimeUnsubs.forEach(u => u());
    this._uiTimeUnsubs = [];

    if (this._uiTimeReloadTimer !== null) {
      clearTimeout(this._uiTimeReloadTimer);
      this._uiTimeReloadTimer = null;
    }

    this.abortMetricRequests();
  }

  // ── Data fetching ────────────────────────────────────────────────────────

  /** Compute the start time for the initial history load. */
  private startMs(): number {
    if (this.config.useUiTimeRange) {
      const ts = getUiStore().get('timeStart');
      if (ts !== null) return ts;
    }
    return Date.now() - this.config.timePeriod * 3_600_000;
  }

  /** Compute the optional end time for UI-controlled ranges. */
  private endMs(): number | null {
    if (!this.config.useUiTimeRange) return null;
    return getUiStore().get('timeEnd');
  }

  private xAxisRangeOption(): Pick<XAXisComponentOption, 'min' | 'max'> | null {
    if (!this.config.useUiTimeRange) return null;
    return {
      min: this.startMs(),
      max: this.endMs() ?? undefined,
    };
  }

  /** Load all enabled series from scratch (initial load or range change). */
  private async loadAllSeries(): Promise<void> {
    this._loadAllAbort?.abort();
    const controller = new AbortController();
    this._loadAllAbort = controller;

    // Group enabled series by device path to minimise API calls.
    const byDevice = new Map<string, number[]>();
    this.config.series.forEach((s, i) => {
      if (!s.enabled) return;
      const dm = this.getDeviceAndMetric(i);
      if (!dm) return;
      const existing = byDevice.get(dm.device) ?? [];
      existing.push(i);
      byDevice.set(dm.device, existing);
    });

    const start = new Date(this.startMs()).toISOString();
    const endMs = this.endMs();
    const end = endMs !== null ? new Date(endMs).toISOString() : null;

    await Promise.all([...byDevice.entries()].map(async ([device, indices]) => {
      const metrics = indices
        .map(i => this.getDeviceAndMetric(i)!.metric)
        .join(',');

      let url = `${API_BASE}/api/v1/metrics/${encodeURIComponent(device)}`
        + `?start=${encodeURIComponent(start)}`
        + `&metrics=${encodeURIComponent(metrics)}`
        + `&max_points=${MAX_POINTS}`;
      if (end) url += `&end=${encodeURIComponent(end)}`;

      try {
        const res = await fetch(url, { headers: getAuthHeaders(), signal: controller.signal });
        if (!this.subscriptionActive || controller.signal.aborted || !res.ok) return;
        const json = await res.json();
        if (!this.subscriptionActive || controller.signal.aborted) return;

        indices.forEach(i => {
          const dm = this.getDeviceAndMetric(i)!;
          const series = json.series?.find((s: any) => s.name === dm.metric);
          if (!series?.data?.length) return;
          const pts: DataPoint[] = (series.data as [number, number][]).map(([t, v]) => ({ t, v }));
          this.seriesData[i] = pts;

          // Track the latest timestamp for this device
          const latestTs = new Date(pts[pts.length - 1].t).toISOString();
          const existing = this.lastTs.get(device);
          if (!existing || latestTs > existing) this.lastTs.set(device, latestTs);
        });
      } catch (err: any) {
        if (err?.name !== 'AbortError') { /* non-fatal */ }
      }
    }));

    if (this._loadAllAbort === controller) this._loadAllAbort = null;
    if (this.subscriptionActive && !controller.signal.aborted) this.applyAllSeries();
  }

  /** Fetch new points since lastTs for the device a given series belongs to. */
  private async fetchIncrementalForSeries(seriesIdx: number): Promise<void> {
    const dm = this.getDeviceAndMetric(seriesIdx);
    if (!dm) return;

    if (this.config.useUiTimeRange && this.endMs() !== null) return;

    const after = this.lastTs.get(dm.device);
    if (!after) {
      if (!this._loadAllAbort) this.loadAllSeries();
      return;
    }

    if (this._incrementalAbortByDevice.has(dm.device)) {
      this._incrementalQueuedByDevice.add(dm.device);
      return;
    }

    const controller = new AbortController();
    this._incrementalAbortByDevice.set(dm.device, controller);

    // Collect all series on this device to update them together
    const indices = this.config.series.reduce<number[]>((acc, s, i) => {
      if (!s.enabled) return acc;
      const d2 = this.getDeviceAndMetric(i);
      if (d2?.device === dm.device) acc.push(i);
      return acc;
    }, []);

    const metrics = indices.map(i => this.getDeviceAndMetric(i)!.metric).join(',');

    const url = `${API_BASE}/api/v1/metrics/${encodeURIComponent(dm.device)}/since`
      + `?after=${encodeURIComponent(after)}`
      + `&start_metric=${encodeURIComponent(dm.metric)}`
      + `&metrics=${encodeURIComponent(metrics)}`;

    try {
      const res = await fetch(url, { headers: getAuthHeaders(), signal: controller.signal });
      if (!this.subscriptionActive || controller.signal.aborted || !res.ok) return;
      const json = await res.json();
      if (!this.subscriptionActive || controller.signal.aborted) return;

      let anyNew = false;
      indices.forEach(i => {
        const m = this.getDeviceAndMetric(i)!;
        const series = json.series?.find((s: any) => s.name === m.metric);
        if (!series?.data?.length) return;
        const pts: DataPoint[] = (series.data as [number, number][]).map(([t, v]) => ({ t, v }));
        const merged = this.mergeSeriesData(i, pts);
        if (merged.length === 0) return;
        this.seriesData[i] = merged;
        anyNew = true;

        const latestTs = new Date(this.seriesData[i][this.seriesData[i].length - 1].t).toISOString();
        const existing = this.lastTs.get(dm.device);
        if (!existing || latestTs > existing) this.lastTs.set(dm.device, latestTs);
      });

      if (anyNew && this.subscriptionActive && !controller.signal.aborted) this.applyAllSeries();
    } catch (err: any) {
      if (err?.name !== 'AbortError') { /* non-fatal */ }
    } finally {
      if (this._incrementalAbortByDevice.get(dm.device) === controller) {
        this._incrementalAbortByDevice.delete(dm.device);
      }
      if (
        this._incrementalQueuedByDevice.delete(dm.device)
        && this.subscriptionActive
        && !controller.signal.aborted
      ) {
        const nextIdx = this.config.series.findIndex((s, i) => {
          if (!s.enabled) return false;
          return this.getDeviceAndMetric(i)?.device === dm.device;
        });
        if (nextIdx >= 0) void this.fetchIncrementalForSeries(nextIdx);
      }
    }
  }

  private abortMetricRequests(): void {
    this._loadAllAbort?.abort();
    this._loadAllAbort = null;
    this._incrementalAbortByDevice.forEach(controller => controller.abort());
    this._incrementalAbortByDevice.clear();
    this._incrementalQueuedByDevice.clear();
  }

  private scheduleUiTimeReload(): void {
    if (this._uiTimeReloadTimer !== null) {
      clearTimeout(this._uiTimeReloadTimer);
    }
    this._uiTimeReloadTimer = setTimeout(() => {
      this._uiTimeReloadTimer = null;
      this.seriesData = Array.from({ length: 5 }, () => []);
      this.lastTs.clear();
      this.loadAllSeries();
    }, 0);
  }

  private mergeSeriesData(seriesIdx: number, points: DataPoint[]): DataPoint[] {
    const start = this.startMs();
    const end = this.endMs();
    const byTimestamp = new Map<number, number>();

    for (const p of this.seriesData[seriesIdx]) {
      if (p.t >= start && (end === null || p.t <= end)) byTimestamp.set(p.t, p.v);
    }

    for (const p of points) {
      if (!Number.isFinite(p.t) || !Number.isFinite(p.v)) continue;
      if (p.t >= start && (end === null || p.t <= end)) byTimestamp.set(p.t, p.v);
    }

    const merged = Array.from(byTimestamp, ([t, v]) => ({ t, v }))
      .sort((a, b) => a.t - b.t);

    return this.config.useUiTimeRange ? merged : merged.slice(-MAX_POINTS);
  }

  // ── Chart rendering ────────────────────────────────────────────────────────

  /** Push all current seriesData into the live chart. */
  private applyAllSeries(): void {
    if (!this.chart) return;
    const seriesOption = this.buildSeriesOption();
    const xAxisRange = this.xAxisRangeOption();
    this.chart.setOption({
      ...(xAxisRange ? { xAxis: xAxisRange } : {}),
      series: seriesOption,
    }, { replaceMerge: ['series'] });
  }

  private buildSeriesOption(): SeriesOption[] {
    return this.config.series.map((s, i) => {
      if (!s.enabled) return { id: `s${i}`, data: [] };
      const data: [number, number][] = this.seriesData[i].map(p => [p.t, p.v]);

      // Extend the last known value to the current wall-clock time so the line
      // advances visually on each refresh tick even when the value is unchanged.
      if (data.length > 0 && !this.config.useUiTimeRange) {
        data.push([Date.now(), data[data.length - 1][1]]);
      }

      return { id: `s${i}`, data };
    });
  }

  /** Build the full eCharts option from scratch. */
  private buildChartOption(): EChartsOption {
    const contentText  = cssVar('--content-text')  || '#e2e8f0';
    const borderColor  = cssVar('--border-color')   || '#2b3a53';
    const panelBg      = cssVar('--panel-bg')       || cssVar('--widget-bg') || '#131f38';
    const axisFs       = parseInt(cssVar('--widget-label-font-size'))      || 13;
    const legendFs     = parseInt(cssVar('--widget-label-bold-font-size')) || 14;
    const lightSurface = isLightThemeSurface();
    const gridLineOpacity = lightSurface ? 0.14 : 0.22;
    const xAxisLabelOpacity = lightSurface ? 0.68 : 0.82;
    const xAxisLineOpacity = lightSurface ? 0.32 : 0.55;
    const xAxisTickOpacity = lightSurface ? 0.26 : 0.45;

    const enabledSeries = this.config.series.filter(s => s.enabled);
    const legendSeries = enabledSeries.filter(s => s.name.trim() !== '');
    const showLegend = legendSeries.length > 0;

    // Build one Y axis per enabled series so each gets its own scale and color.
    // Multiple axes on the same side are offset so they don't overlap. Keep the
    // grid edge compact; ECharts' containLabel accounts for the axis labels, and
    // growing grid.left/right by every axis offset creates large empty gutters.
    const AXIS_LABEL_EDGE_GUTTER = 4;
    const AXIS_OFFSET = 36;
    const yAxes: YAXisComponentOption[] = [];
    const seriesYAxisIdx: number[] = new Array(5).fill(0);
    let leftCount = 0, rightCount = 0;

    this.config.series.forEach((s, i) => {
      if (!s.enabled) return;
      const isLeft   = s.yAxis !== 'right';
      const sideCount = isLeft ? leftCount : rightCount;
      const offset   = sideCount * AXIS_OFFSET;

      seriesYAxisIdx[i] = yAxes.length;
      yAxes.push({
        type: 'value',
        position: s.yAxis as 'left' | 'right',
        offset,
        axisLine: { show: sideCount > 0, lineStyle: { color: s.color, opacity: 0.4 } },
        axisTick: { show: false },
        // Only the first axis on the chart shows grid lines
        splitLine: {
          show: this.config.showGrid && yAxes.length === 0,
          lineStyle: { color: contentText, opacity: gridLineOpacity },
        },
        axisLabel: {
          color: s.color,
          opacity: 0.75,
          fontFamily: "ui-monospace,'Cascadia Code','SF Mono',monospace",
          fontSize: axisFs,
        },
      });

      if (isLeft) leftCount++; else rightCount++;
    });

    if (yAxes.length === 0) yAxes.push({ type: 'value' });

    // Map each series to a yAxisIndex
    const seriesOptions: SeriesOption[] = this.config.series.map((s, i) => {
      if (!s.enabled) return { id: `s${i}`, type: 'line', data: [] };

      const color = s.color;
      const yAxisIndex = seriesYAxisIdx[i];

      return {
        id: `s${i}`,
        type: 'line',
        name: s.name,
        yAxisIndex,
        data: this.seriesData[i].map(p => [p.t, p.v]),
        showSymbol: false,
        smooth: s.smooth,
        sampling: 'lttb',
        lineStyle: { color, width: 1.5 },
        itemStyle: { color },
        areaStyle: s.gradientFill ? {
          color: new graphic.LinearGradient(0, 0, 0, 1, [
            { offset: 0, color: hexToRgba(color, 0.35) },
            { offset: 1, color: hexToRgba(color, 0.02) },
          ]),
        } : undefined,
        emphasis: { disabled: true },
      };
    });

    return {
      backgroundColor: 'transparent',
      animation: false,
      grid: {
        top: showLegend ? 44 : 20,
        bottom: this.config.showZoomControl ? 52 : 24,
        left:  leftCount  > 0 ? AXIS_LABEL_EDGE_GUTTER : 14,
        right: rightCount > 0 ? AXIS_LABEL_EDGE_GUTTER : 14,
        containLabel: true,
      },
      legend: {
        show: showLegend,
        top: 6,
        left: 'center',
        textStyle: {
          color: contentText,
          fontFamily: "ui-monospace,'Cascadia Code','SF Mono',monospace",
          fontSize: legendFs,
        },
        icon: 'rect',
        itemWidth: 12,
        itemHeight: 4,
        data: legendSeries.map(s => ({
          name: s.name,
          textStyle: { color: s.color },
        })),
      },
      tooltip: {
        trigger: 'axis',
        axisPointer: {
          type: 'cross',
          lineStyle:  { color: contentText, opacity: 0.3, width: 1 },
          crossStyle: { color: contentText, opacity: 0.3, width: 1 },
        },
        backgroundColor: panelBg,
        borderColor: borderColor,
        borderWidth: 1,
        textStyle: {
          color: contentText,
          fontFamily: "ui-monospace,'Cascadia Code','SF Mono',monospace",
          fontSize: legendFs,
        },
        formatter: (params: any) => {
          if (!Array.isArray(params) || params.length === 0) return '';
          const d = new Date(params[0].value[0]);
          const dateStr = d.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit', year: 'numeric' });
          const timeStr = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
          const rows = params
            .filter((p: any) => p.value?.[1] !== undefined)
            .map((p: any) => {
              const v = p.value[1];
              const vStr = Number.isInteger(v) ? String(v) : v.toFixed(2);
              return `<div style="display:flex;gap:8px;align-items:center;margin-top:3px;">
                <span style="display:inline-block;width:10px;height:3px;background:${p.color};border-radius:2px;flex-shrink:0;"></span>
                <span style="opacity:0.7;flex:1;">${p.seriesName}</span>
                <span style="font-weight:500;">${vStr}</span>
              </div>`;
            }).join('');
          return `<div style="font-size:10px;opacity:0.55;margin-bottom:4px;">${dateStr} ${timeStr}</div>${rows}`;
        },
      },
      xAxis: {
        type: 'time',
        ...(this.xAxisRangeOption() ?? {}),
        axisLine: { lineStyle: { color: contentText, opacity: xAxisLineOpacity } },
        axisTick: { lineStyle: { color: contentText, opacity: xAxisTickOpacity } },
        splitLine: {
          show: this.config.showGrid,
          lineStyle: { color: contentText, opacity: gridLineOpacity },
        },
        axisLabel: {
          color: contentText,
          opacity: xAxisLabelOpacity,
          fontFamily: "ui-monospace,'Cascadia Code','SF Mono',monospace",
          fontSize: axisFs,
          formatter: (val: number) => {
            const d = new Date(val);
            const hh = String(d.getHours()).padStart(2, '0');
            const mm = String(d.getMinutes()).padStart(2, '0');
            return `${hh}:${mm}`;
          },
        },
      },
      yAxis: yAxes,
      dataZoom: this.config.showZoomControl ? [
        {
          type: 'slider',
          bottom: 6,
          height: 22,
          borderColor: borderColor,
          backgroundColor: 'transparent',
          dataBackground: {
            lineStyle: { color: cssVar('--accent-color') || '#60a5fa', opacity: 0.3, width: 1 },
            areaStyle: { color: cssVar('--accent-color') || '#60a5fa', opacity: 0.06 },
          },
          selectedDataBackground: {
            lineStyle: { color: cssVar('--accent-color') || '#60a5fa', opacity: 0.6, width: 1 },
            areaStyle: { color: cssVar('--accent-color') || '#60a5fa', opacity: 0.12 },
          },
          handleStyle: { color: cssVar('--accent-color') || '#60a5fa', borderWidth: 0 },
          moveHandleStyle: { color: cssVar('--accent-color') || '#60a5fa', opacity: 0.5 },
          fillerColor: `color-mix(in srgb, ${cssVar('--accent-color') || '#60a5fa'} 10%, transparent)`,
          textStyle: { color: contentText, fontSize: 9, fontFamily: "ui-monospace,'Cascadia Code',monospace" },
          labelFormatter: (val: number) => {
            const d = new Date(val);
            return `${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`;
          },
        },
      ] : [],
      series: seriesOptions,
    };
  }

  // ── Utility ─────────────────────────────────────────────────────────────────

  private resolveTagPath(seriesIdx: number): string {
    const s = this.config.series[seriesIdx];
    if (!s) return '';
    return resolveMetricTagPath(s.tagPrefix, s.tagPath);
  }

  private getDeviceAndMetric(seriesIdx: number): { device: string; metric: string } | null {
    const fullPath = this.resolveTagPath(seriesIdx);
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
      card.setTitle(this.config.headerText ?? 'Chart');
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
  type: 'timeseries-chart-widget',
  name: 'Timeseries Chart',
  icon: '📈',
  category: 'Metrics',
  defaultW: 12,
  defaultH: 8,
  minW: 6,
  minH: 4,
});

customElements.define('timeseries-chart-widget', TimeseriesChartWidget);
