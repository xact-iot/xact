import { describe, expect, it, vi } from 'vitest';
import { getUiStore } from '../src/store/ui-store';
import '../src/dashboards/widgets/big-number-widget';
import '../src/dashboards/widgets/gauge-widget';
import '../src/dashboards/widgets/binary-status-line-widget';

const metricWidgets = [
  ['big-number-widget', 'Big number'],
  ['gauge-widget', 'Gauge'],
  ['binary-status-line-widget', 'Binary status line'],
] as const;

describe('metric widget tag selection schemas', () => {
  it.each(metricWidgets)('%s uses relative tag browsing under its tag prefix', (tagName) => {
    const widget = document.createElement(tagName) as any;
    const schema = widget.getPropertySchema();
    const prefixField = schema.find((field: any) => field.name === 'tagPrefix');
    const tagPathField = schema.find((field: any) => field.name === 'tagPath');

    expect(prefixField).toMatchObject({
      type: 'path',
      label: 'Tag prefix (use * for dashboard device name)',
      context: { includeLeaves: false },
    });
    expect(tagPathField).toMatchObject({
      type: 'path',
      label: 'Tag path',
      context: { includeLeaves: true, rootFromField: 'tagPrefix', stripBrowseRoot: true },
    });
  });

  it('binary-status-line-widget exposes and applies the zoom control setting', () => {
    const widget = document.createElement('binary-status-line-widget') as any;
    const field = widget.getPropertySchema().find((item: any) => item.name === 'showZoomControl');

    expect(field).toMatchObject({
      type: 'boolean',
      label: 'Show zoom control',
      default: true,
    });

    expect(widget.buildChartOption().dataZoom).toHaveLength(1);
    expect(widget.buildChartOption().grid.bottom).toBe(42);

    widget.config = { ...widget.config, showZoomControl: false };
    expect(widget.config.showZoomControl).toBe(false);
    expect(widget.buildChartOption().dataZoom).toEqual([]);
    expect(widget.buildChartOption().grid.bottom).toBe(20);
  });

  it('binary-status-line-widget ingests boolean and string metric values from history', () => {
    const widget = document.createElement('binary-status-line-widget') as any;
    const now = Date.now();
    widget.ingestData([
      { t: now - 4000, v: false },
      { t: now - 3000, v: true },
      { t: now - 2000, v: 'false' },
      { t: now - 1000, v: '1' },
      { t: now, v: 'no' },
    ], true);

    expect(widget.data).toEqual([
      { t: now - 4000, v: 0 },
      { t: now - 3000, v: 1 },
      { t: now - 2000, v: 0 },
      { t: now - 1000, v: 1 },
      { t: now, v: 0 },
    ]);
  });

  it('binary-status-line-widget renders a solid filled step line and short ON pulse markers', () => {
    const widget = document.createElement('binary-status-line-widget') as any;
    const setOption = vi.fn();
    widget.chart = { setOption };
    widget.subscriptionActive = true;
    widget.data = [
      { t: 1780739234360, v: 0 },
      { t: 1780739312827, v: 1 },
      { t: 1780739353587, v: 0 },
    ];

    widget.applyData();

    const option = setOption.mock.calls[0][0];
    expect(option.series[0]).toMatchObject({
      id: 'status',
      type: 'line',
      step: 'end',
      areaStyle: { opacity: 1 },
      lineStyle: { opacity: 0.95 },
    });
    expect(option.series[1]).toMatchObject({
      id: 'status-on-events',
      type: 'scatter',
      symbolSize: 6,
      data: [[1780739312827, 1]],
    });
  });

  it('binary-status-line-widget pins the x-axis and range query to the UI time range', async () => {
    const start = Date.UTC(2026, 5, 5, 10, 0, 0);
    const end = Date.UTC(2026, 5, 6, 10, 0, 0);
    getUiStore().set('timeStart', start);
    getUiStore().set('timeEnd', end);

    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        series: [{ name: 'status.sensorFault', data: [[start + 1000, 1], [start + 2000, 0]] }],
      }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const setOption = vi.fn();
    const widget = document.createElement('binary-status-line-widget') as any;
    widget.config = {
      ...widget.config,
      tagPath: 'LA_LongBeach.AirQuality.AQ-B-0001.status.sensorFault',
      useUiTimeRange: true,
    };
    widget.chart = { setOption };
    widget.subscriptionActive = true;

    await widget.loadData();

    const url = new URL(fetchMock.mock.calls[0][0], 'https://example.test');
    expect(url.searchParams.get('start')).toBe(new Date(start).toISOString());
    expect(url.searchParams.get('end')).toBe(new Date(end).toISOString());
    expect(setOption.mock.calls[0][0].xAxis).toEqual({ min: start, max: end });
    vi.unstubAllGlobals();
    getUiStore().set('timeStart', null);
    getUiStore().set('timeEnd', null);
  });

  it.each([
    ['big-number-widget'],
    ['gauge-widget'],
  ] as const)('%s clears refresh interval when sparkline is disabled', (tagName) => {
    const widget = document.createElement(tagName) as any;

    widget.setConfig({ showSparkline: true, refreshInterval: '60' });
    expect(widget.config.refreshInterval).toBe(60);

    widget.setConfig({ showSparkline: false, refreshInterval: '60' });
    expect(widget.config.showSparkline).toBe(false);
    expect(widget.config.refreshInterval).toBe(0);

    widget.setConfig({ refreshInterval: '300' });
    expect(widget.config.refreshInterval).toBe(0);
  });

  it.each([
    ['big-number-widget'],
    ['gauge-widget'],
  ] as const)('%s disables refresh interval field unless sparkline is shown', (tagName) => {
    const widget = document.createElement(tagName) as any;
    const field = widget.getPropertySchema().find((item: any) => item.name === 'refreshInterval');

    expect(field).toMatchObject({
      type: 'select',
      label: 'Refresh interval',
      context: { disabledUnlessField: 'showSparkline' },
    });
  });
});
