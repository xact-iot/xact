import { afterEach, describe, expect, it, vi } from 'vitest';
import { getUiStore } from '../src/store/ui-store';
import '../src/dashboards/widgets/timeseries-chart-widget';

const series = Array.from({ length: 5 }, (_, i) => ({
  enabled: i === 0,
  name: '',
  tagPrefix: '',
  tagPath: `Device.metric${i + 1}`,
  color: ['#60a5fa', '#22d3ee', '#f59e0b', '#fb7185', '#a3e635'][i],
  yAxis: 'left',
  gradientFill: true,
  smooth: false,
}));

describe('timeseries-chart-widget', () => {
  afterEach(() => {
    ['--panel-bg', '--widget-bg', '--content-bg', '--content-text', '--border-color'].forEach(name => {
      document.documentElement.style.removeProperty(name);
    });
    getUiStore().set('timeStart', null);
    getUiStore().set('timeEnd', null);
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders series tag prefixes as browsable path fields', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;

    const schema = widget.getPropertySchema();
    const prefixField = schema.find((field: any) => field.name === 'series1TagPrefix') as any;
    const tagPathField = schema.find((field: any) => field.name === 'series1TagPath') as any;
    const gridField = schema.find((field: any) => field.name === 'showGrid') as any;
    const smoothField = schema.find((field: any) => field.name === 'series1Smooth') as any;

    expect(gridField).toMatchObject({
      type: 'boolean',
      label: 'Show grid',
      default: false,
    });
    expect(prefixField).toMatchObject({
      type: 'path',
      label: 'Tag prefix (use * for dashboard device name)',
      context: { includeLeaves: false },
    });
    expect(tagPathField).toMatchObject({
      type: 'path',
      label: 'Tag path',
      context: { includeLeaves: true, rootFromField: 'series1TagPrefix', stripBrowseRoot: true },
    });
    expect(smoothField).toMatchObject({
      type: 'boolean',
      label: 'Smooth line',
      default: false,
    });
  });

  it('applies grid visibility and per-series line smoothing to chart options', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      showGrid: true,
      backgroundImage: '',
      series: series.map((s, i) => i === 0 ? { ...s, smooth: true } : s),
    };

    const option = widget.buildChartOption();

    expect(option.xAxis.splitLine.show).toBe(true);
    expect(option.xAxis.splitLine.lineStyle).toMatchObject({ color: '#e2e8f0', opacity: 0.22 });
    expect(option.yAxis[0].splitLine.show).toBe(true);
    expect(option.yAxis[0].splitLine.lineStyle).toMatchObject({ color: '#e2e8f0', opacity: 0.22 });
    expect(option.series[0].smooth).toBe(true);
  });

  it('flattens grid and smooth config from the properties dialog', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;

    widget.setConfig({
      showGrid: true,
      series1Enabled: true,
      series1Smooth: true,
    });

    expect(widget.config.showGrid).toBe(true);
    expect(widget.config.series[0].smooth).toBe(true);
  });

  it('hides the legend and top legend gap when enabled series names are blank', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series,
    };

    const option = widget.buildChartOption();

    expect(option.legend.show).toBe(false);
    expect(option.legend.data).toEqual([]);
    expect(option.grid.top).toBe(20);
  });

  it('shows the legend when at least one enabled series has a non-blank name', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series: series.map((s, i) => i === 0 ? { ...s, name: 'Flow' } : s),
    };

    const option = widget.buildChartOption();

    expect(option.legend.show).toBe(true);
    expect(option.legend.data).toEqual([{
      name: 'Flow',
      textStyle: { color: '#60a5fa' },
    }]);
    expect(option.grid.top).toBe(44);
  });

  it('uses high-contrast x-axis labels for dark themes', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series,
    };

    const option = widget.buildChartOption();

    expect(option.xAxis.axisLabel.opacity).toBe(0.82);
    expect(option.xAxis.axisLine.lineStyle.opacity).toBe(0.55);
  });

  it('softens the x-axis and grid for light theme surfaces', () => {
    document.documentElement.style.setProperty('--widget-bg', '#ffffff');
    document.documentElement.style.setProperty('--content-text', '#111827');
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      showGrid: true,
      backgroundImage: '',
      series,
    };

    const option = widget.buildChartOption();

    expect(option.xAxis.axisLabel).toMatchObject({ color: '#111827', opacity: 0.68 });
    expect(option.xAxis.axisLine.lineStyle).toMatchObject({ color: '#111827', opacity: 0.32 });
    expect(option.xAxis.axisTick.lineStyle).toMatchObject({ color: '#111827', opacity: 0.26 });
    expect(option.xAxis.splitLine.lineStyle).toMatchObject({ color: '#111827', opacity: 0.14 });
    expect(option.yAxis[0].splitLine.lineStyle).toMatchObject({ color: '#111827', opacity: 0.14 });
  });

  it('contains labels and uses a compact gutter for a single left y-axis', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series,
    };

    const option = widget.buildChartOption();

    expect(option.grid.left).toBe(4);
    expect(option.grid.right).toBe(14);
    expect(option.grid.containLabel).toBe(true);
  });

  it('contains labels and uses a compact gutter for a single right y-axis', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series: series.map((s, i) => i === 0 ? { ...s, yAxis: 'right' } : s),
    };

    const option = widget.buildChartOption();

    expect(option.grid.left).toBe(14);
    expect(option.grid.right).toBe(4);
    expect(option.grid.containLabel).toBe(true);
  });

  it('keeps compact grid gutters when multiple y-axes are on the same side', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series: series.map((s, i) => i < 3 ? { ...s, enabled: true, name: `S${i + 1}`, yAxis: 'left' } : s),
    };

    const option = widget.buildChartOption();

    expect(option.grid.left).toBe(4);
    expect(option.grid.right).toBe(14);
    expect(option.yAxis.slice(0, 3).map((axis: any) => axis.offset)).toEqual([0, 36, 72]);
  });

  it('keeps compact grid gutters when y-axes are split across both sides', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series: series.map((s, i) => i < 3
        ? { ...s, enabled: true, name: `S${i + 1}`, yAxis: i === 2 ? 'right' : 'left' }
        : s),
    };

    const option = widget.buildChartOption();

    expect(option.grid.left).toBe(4);
    expect(option.grid.right).toBe(4);
    expect(option.yAxis.map((axis: any) => ({ position: axis.position, offset: axis.offset }))).toEqual([
      { position: 'left', offset: 0 },
      { position: 'left', offset: 36 },
      { position: 'right', offset: 0 },
    ]);
  });

  it('passes the UI time range end to the metrics range query and pins the x-axis', async () => {
    const start = Date.UTC(2026, 0, 1, 0, 0, 0);
    const end = Date.UTC(2026, 0, 8, 0, 0, 0);
    getUiStore().set('timeStart', start);
    getUiStore().set('timeEnd', end);

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        series: [{ name: 'flow.rate', data: [[start + 1000, 12]] }],
      }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: true,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series: series.map((s, i) => i === 0 ? {
        ...s,
        tagPath: 'default.Pump.flow.rate',
      } : s),
    };
    widget.subscriptionActive = true;

    await widget.loadAllSeries();

    const url = new URL(fetchMock.mock.calls[0][0], 'https://example.test');
    expect(url.searchParams.get('start')).toBe(new Date(start).toISOString());
    expect(url.searchParams.get('end')).toBe(new Date(end).toISOString());

    const option = widget.buildChartOption();
    expect(option.xAxis.min).toBe(start);
    expect(option.xAxis.max).toBe(end);
  });

  it('does not collapse UI time range data to the rolling point cap on incremental merge', () => {
    getUiStore().set('timeStart', 1);
    getUiStore().set('timeEnd', null);

    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: true,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series,
    };
    widget.seriesData = [
      Array.from({ length: 2000 }, (_, i) => ({ t: i + 1, v: i })),
      [], [], [], [],
    ];

    const merged = widget.mergeSeriesData(0, [{ t: 2001, v: 2001 }]);

    expect(merged).toHaveLength(2001);
    expect(merged[0].t).toBe(1);
    expect(merged[2000].t).toBe(2001);
  });

  it('coalesces incremental fetches per device while one is already in flight', async () => {
    let resolveFetch!: (value: any) => void;
    const firstFetch = new Promise(resolve => { resolveFetch = resolve; });
    const response = {
      ok: true,
      json: async () => ({ series: [] }),
    };
    const fetchMock = vi.fn()
      .mockReturnValueOnce(firstFetch)
      .mockResolvedValue(response);
    vi.stubGlobal('fetch', fetchMock);

    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series: series.map((s, i) => i === 0 ? {
        ...s,
        tagPath: 'default.Pump.flow.rate',
      } : s),
    };
    widget.subscriptionActive = true;
    widget.lastTs.set('Pump', '2026-01-01T00:00:00.000Z');

    const first = widget.fetchIncrementalForSeries(0);
    await widget.fetchIncrementalForSeries(0);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(widget._incrementalQueuedByDevice.has('Pump')).toBe(true);

    resolveFetch(response);
    await first;
    await Promise.resolve();

    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('does not start a duplicate range load from live tag callbacks while the initial load is in flight', async () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.config = {
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series: series.map((s, i) => i === 0 ? {
        ...s,
        tagPath: 'default.Pump.flow.rate',
      } : s),
    };
    widget.subscriptionActive = true;
    widget._loadAllAbort = new AbortController();
    widget.loadAllSeries = vi.fn();

    await widget.fetchIncrementalForSeries(0);

    expect(widget.loadAllSeries).not.toHaveBeenCalled();
  });

  it('does not render or load data when configured before being connected', () => {
    const widget = document.createElement('timeseries-chart-widget') as any;
    widget.rerender = vi.fn();

    widget.setConfig({
      headerText: 'Chart',
      timePeriod: 24,
      useUiTimeRange: false,
      refreshInterval: 0,
      showZoomControl: true,
      backgroundImage: '',
      series,
    });

    expect(widget.rerender).not.toHaveBeenCalled();
  });

  it('debounces UI time range changes into a single range reload', () => {
    vi.useFakeTimers();
    try {
      const widget = document.createElement('timeseries-chart-widget') as any;
      widget.seriesData = [[{ t: 1, v: 1 }], [], [], [], []];
      widget.lastTs.set('Pump', '2026-01-01T00:00:00.000Z');
      widget.loadAllSeries = vi.fn();

      widget.scheduleUiTimeReload();
      widget.scheduleUiTimeReload();

      expect(widget.loadAllSeries).not.toHaveBeenCalled();
      vi.runOnlyPendingTimers();

      expect(widget.loadAllSeries).toHaveBeenCalledTimes(1);
      expect(widget.lastTs.size).toBe(0);
      expect(widget.seriesData).toEqual([[], [], [], [], []]);
    } finally {
      vi.useRealTimers();
    }
  });
});
