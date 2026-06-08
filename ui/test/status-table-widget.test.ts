import { afterEach, describe, expect, it, vi } from 'vitest';

const apiMock = vi.hoisted(() => ({
  createEventLogEntry: vi.fn(async () => ({})),
}));

const treeDialogMock = vi.hoisted(() => ({
  open: vi.fn(),
}));

const mockStore = {
  toAbsolute: vi.fn((path: string) => path.startsWith('default.') ? path : `default.${path}`),
  toRelative: vi.fn((path: string) => path.startsWith('default.') ? path.slice('default.'.length) : path),
  baseTagPath: vi.fn((path: string) => String(path).split(':')[0]),
  listChildrenNames: vi.fn((path: string) => path === 'default.LA_LongBeach.AirQuality' ? ['AQ-S-0140'] : []),
  getNodeType: vi.fn((path: string) => path.includes('.power.') ? 'unknown' : 'leaf'),
  getNodeValue: vi.fn((path: string) => valueByPath[path]),
  getNodeShared: vi.fn((path: string) => path.endsWith('.particulate.pm25') ? { units: 'ug/m3' } : {}),
  resolveTagReference: vi.fn((path: string) => valueByPath[path]),
  subscribeTagReference: vi.fn((path: string, callback: (value: unknown) => void) => {
    callback(valueByPath[path]);
    return vi.fn();
  }),
  request: vi.fn(async () => ({ success: true, message: 'ok' })),
};

let valueByPath: Record<string, unknown> = {};

const mockUiStore = {
  get: vi.fn((key: string) => {
    if (key === 'deviceName') return 'AQ-S-0140';
    if (key === 'serverTimezone') return 'UTC';
    return '';
  }),
  subscribe: vi.fn(() => vi.fn()),
};

vi.mock('../src/store/store', () => ({
  getMirrorStore: () => mockStore,
}));

vi.mock('../src/store/ui-store', () => ({
  getUiStore: () => mockUiStore,
}));

vi.mock('../src/api', () => ({
  createEventLogEntry: apiMock.createEventLogEntry,
}));

vi.mock('../src/components/tree-browser-dialog', () => ({
  getTreeBrowserDialog: () => treeDialogMock,
}));

vi.mock('../src/auth', () => ({
  getCurrentUser: () => ({ tenant_id: 'default' }),
}));

import '../src/dashboards/widgets/status-table-widget';

describe('status-table-widget optional tags', () => {
  afterEach(() => {
    vi.clearAllMocks();
    mockStore.listChildrenNames.mockImplementation((path: string) => path === 'default.LA_LongBeach.AirQuality' ? ['AQ-S-0140'] : []);
    mockStore.getNodeType.mockImplementation((path: string) => path.includes('.power.') ? 'unknown' : 'leaf');
    mockStore.getNodeValue.mockImplementation((path: string) => valueByPath[path]);
    mockUiStore.get.mockImplementation((key: string) => {
      if (key === 'deviceName') return 'AQ-S-0140';
      if (key === 'serverTimezone') return 'UTC';
      return '';
    });
    document.body.innerHTML = '';
    valueByPath = {};
    document.querySelectorAll('.stw-command-dialog').forEach(el => el.remove());
  });

  it('does not subscribe to configured rows whose resolved tag is absent from the tree', () => {
    const widget = document.createElement('status-table-widget') as any;
    document.body.appendChild(widget);

    widget.setConfig({
      tagPrefix: 'LA_LongBeach.AirQuality.AQ-S-0140',
      rows: [
        {
          id: 'pm25',
          label: 'PM2.5',
          col2: { type: 'value', tagPath: 'particulate.pm25', formatter: 'number' },
          col3: { type: 'none' },
        },
        {
          id: 'battery',
          label: 'Battery voltage',
          col2: { type: 'value', tagPath: 'power.batteryVoltage', formatter: 'number' },
          col3: { type: 'none' },
        },
      ],
    });

    expect(mockStore.subscribeTagReference).toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm25',
      expect.any(Function),
    );
    expect(mockStore.subscribeTagReference).not.toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-S-0140.power.batteryVoltage',
      expect.any(Function),
    );
  });

  it('renders text, number, bar, date, icon, and hide-condition cells', () => {
    valueByPath = {
      'default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm25': 42.125,
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.mode': 'RUN',
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.fill': 75,
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.updated': 1_700_000_000_000,
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.status': 'ok',
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.hidden': 'yes',
    };
    const widget = document.createElement('status-table-widget') as any;
    document.body.appendChild(widget);

    widget.setConfig({
      tagPrefix: 'LA_LongBeach.AirQuality.AQ-S-0140',
      hideTagPath: 'meta.hidden',
      hideTagValue: 'yes',
      rows: [
        {
          id: 'pm25',
          label: 'PM2.5',
          bold: true,
          col2: {
            type: 'value',
            tagPath: 'particulate.pm25',
            formatter: 'number',
            colorBandsEnabled: true,
            colorBandsThreshold1: 20,
            colorBandsThreshold2: 40,
            colorBandsColor1: '#22c55e',
            colorBandsColor2: '#f59e0b',
            colorBandsColor3: '#ef4444',
          },
          col3: { type: 'value', tagPath: 'meta.mode', formatter: 'text' },
        },
        {
          id: 'fill',
          label: 'Fill',
          col2: { type: 'value', tagPath: 'meta.fill', formatter: 'bar', upperLimit: 100 },
          col3: { type: 'value', tagPath: 'meta.updated', formatter: 'date/time' },
        },
        {
          id: 'status',
          label: 'Status',
          col2: {
            type: 'value',
            tagPath: 'meta.status',
            formatter: 'icon',
            iconMap: [{ value: 'ok', icon: 'plain-ok', color: '#22c55e', size: 24, animation: 'pulse' }],
          },
          col3: { type: 'none' },
        },
      ],
    });

    expect(widget.textContent).toContain('42.13');
    expect(widget.textContent).toContain('ug/m3');
    expect(widget.textContent).toContain('RUN');
    expect(widget.innerHTML).toContain('width:75.0%');
    expect(widget.textContent).toContain('2023');
    expect(widget.querySelector('.stw-icon-value.stw-icon-pulse')?.textContent).toContain('ok');
    expect(widget.style.display).toBe('none');
  });

  it('updates affected cells from subscription callbacks and reacts to wildcard device changes', () => {
    let pmCallback: ((value: unknown) => void) | undefined;
    const unsub = vi.fn();
    mockStore.subscribeTagReference.mockImplementation((path: string, callback: (value: unknown) => void) => {
      if (path.endsWith('.particulate.pm25')) pmCallback = callback;
      callback(valueByPath[path]);
      return unsub;
    });
    valueByPath = {
      'default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm25': 1,
    };
    const widget = document.createElement('status-table-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      tagPrefix: 'LA_LongBeach.AirQuality.*',
      rows: [
        {
          id: 'pm25',
          label: 'PM2.5',
          col2: { type: 'value', tagPath: 'particulate.pm25', formatter: 'number' },
          col3: { type: 'none' },
        },
      ],
    });

    expect(widget.textContent).toContain('1');
    valueByPath['default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm25'] = 9.5;
    pmCallback?.(9.5);
    expect(widget.textContent).toContain('9.50');

    const deviceSub = mockUiStore.subscribe.mock.calls.at(-1)?.[1] as (value: string) => void;
    deviceSub('AQ-S-9999');
    expect(unsub).toHaveBeenCalled();
  });

  it('executes confirmed commands, records event log entries, and shows failures', async () => {
    crypto.randomUUID = vi.fn(() => 'cmd-1') as any;
    const widget = document.createElement('status-table-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      tagPrefix: 'LA_LongBeach.AirQuality.AQ-S-0140',
      rows: [
        {
          id: 'fan',
          label: 'Fan',
          col2: {
            type: 'command',
            tagPath: 'controls.fan',
            commandPath: 'controls.fan',
            input: 'number',
            commandValue: 3,
            min: 0,
            max: 10,
            timeoutSeconds: 2,
          },
          col3: { type: 'none' },
        },
      ],
    });

    const input = widget.querySelector<HTMLInputElement>('.stw-command-field')!;
    input.value = '5';
    input.dispatchEvent(new Event('change', { bubbles: true }));
    widget.querySelector<HTMLButtonElement>('.stw-command-button')!.click();
    document.querySelector<HTMLElement>('.stw-command-confirm')!.click();
    await Promise.resolve();
    await Promise.resolve();

    expect(mockStore.request).toHaveBeenCalledWith(
      'xact.command.default.LA_LongBeach.AirQuality.AQ-S-0140',
      { id: 'cmd-1', 'controls.fan': 5 },
      2000,
    );
    expect(document.querySelector('.stw-command-dialog')?.textContent).toContain('Command succeeded');
    expect(apiMock.createEventLogEntry).toHaveBeenCalledWith(expect.objectContaining({
      severity: 'INFO',
      device: 'AQ-S-0140',
      params: { value: 5 },
    }));

    document.querySelector<HTMLElement>('.stw-command-close')!.click();
    mockStore.request.mockRejectedValueOnce(new Error('timeout'));
    widget.querySelector<HTMLButtonElement>('.stw-command-button')!.click();
    document.querySelector<HTMLElement>('.stw-command-confirm')!.click();
    await Promise.resolve();
    await Promise.resolve();

    expect(document.querySelector('.stw-command-dialog')?.textContent).toContain('Driver did not respond');
    expect(apiMock.createEventLogEntry).toHaveBeenLastCalledWith(expect.objectContaining({ severity: 'ERROR' }));
  });

  it('edits rows in the config overlay and supports tree browse callbacks', () => {
    const widget = document.createElement('status-table-widget') as any;
    const saved = vi.fn();
    widget.addEventListener('widget-config-save', saved);
    document.body.appendChild(widget);
    widget.setConfig({
      headerText: 'Status',
      tagPrefix: 'LA_LongBeach.AirQuality.*',
      rows: [
        {
          id: 'pm25',
          label: 'PM2.5',
          col2: { type: 'value', tagPath: 'particulate.pm25', formatter: 'number' },
          col3: { type: 'none' },
        },
      ],
    });

    widget.openConfig();
    const overlay = document.querySelector<HTMLElement>('#stw-backdrop')!.parentElement!;
    overlay.querySelector<HTMLInputElement>('#stw-header')!.value = 'Updated';
    overlay.querySelector<HTMLInputElement>('#stw-prefix')!.value = 'LA_LongBeach.AirQuality.AQ-S-0140';
    overlay.querySelector<HTMLInputElement>('.stw-r-label')!.value = 'Fine Particles';

    overlay.querySelector<HTMLElement>('#stw-browse-prefix')!.click();
    expect(treeDialogMock.open).toHaveBeenCalledWith('', 'Select Node', expect.any(Function), false, 'LA_LongBeach.AirQuality.AQ-S-0140');
    treeDialogMock.open.mock.calls.at(-1)![2]('Selected.Node');
    expect(overlay.querySelector<HTMLInputElement>('#stw-prefix')!.value).toBe('Selected.Node');

    overlay.querySelector<HTMLElement>('.stw-browse-row')!.click();
    treeDialogMock.open.mock.calls.at(-1)![2]('Selected.Node.particulate.pm10');
    expect(overlay.querySelector<HTMLInputElement>('.stw-c-tag')!.value).toBe('particulate.pm10');

    overlay.querySelector<HTMLElement>('#stw-add-row')!.click();
    expect(document.querySelectorAll('.stw-row-editor')).toHaveLength(2);
    document.querySelector<HTMLElement>('#stw-cfg-save')!.click();

    expect(saved).toHaveBeenCalledWith(expect.objectContaining({
      detail: expect.objectContaining({
        forceDirty: true,
        config: expect.objectContaining({
          headerText: 'Updated',
          tagPrefix: 'Selected.Node',
        }),
      }),
    }));
    expect(widget.textContent).toContain('Fine Particles');
  });

  it('uses a representative device when browsing rows for a wildcard prefix without active device context', () => {
    mockUiStore.get.mockImplementation((key: string) => key === 'serverTimezone' ? 'UTC' : '');
    const widget = document.createElement('status-table-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      headerText: 'Status',
      tagPrefix: 'LA_LongBeach.AirQuality.*',
      rows: [
        {
          id: 'pm25',
          label: 'PM2.5',
          col2: { type: 'value', tagPath: 'particulate.pm25', formatter: 'number' },
          col3: { type: 'none' },
        },
      ],
    });

    widget.openConfig();
    const overlay = document.querySelector<HTMLElement>('#stw-backdrop')!.parentElement!;
    overlay.querySelector<HTMLElement>('.stw-browse-row')!.click();

    expect(treeDialogMock.open).toHaveBeenLastCalledWith(
      'LA_LongBeach.AirQuality.AQ-S-0140',
      'Select Tag',
      expect.any(Function),
      true,
      'default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm25',
      'default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm25',
    );

    treeDialogMock.open.mock.calls.at(-1)![2]('LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm10');
    expect(overlay.querySelector<HTMLInputElement>('.stw-c-tag')!.value).toBe('particulate.pm10');
  });

  it('appends value rows from a selected tag group using relative paths', () => {
    mockStore.listChildrenNames.mockImplementation((path: string) => {
      if (path === 'default.LA_LongBeach.AirQuality') return ['AQ-S-0140'];
      if (path === 'default.LA_LongBeach.AirQuality.AQ-S-0140') return ['meta'];
      if (path === 'default.LA_LongBeach.AirQuality.AQ-S-0140.meta') return ['online_status', 'deviceMode', 'updated'];
      return [];
    });
    mockStore.getNodeType.mockImplementation((path: string) => {
      if (path === 'default.LA_LongBeach.AirQuality.AQ-S-0140.meta') return 'node';
      return 'leaf';
    });
    valueByPath = {
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.online_status': true,
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.deviceMode': 'RUN',
      'default.LA_LongBeach.AirQuality.AQ-S-0140.meta.updated': 1700000000000,
    };
    const widget = document.createElement('status-table-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      headerText: 'Status',
      tagPrefix: 'LA_LongBeach.AirQuality.*',
      rows: [],
    });

    widget.openConfig();
    const overlay = document.querySelector<HTMLElement>('#stw-backdrop')!.parentElement!;
    overlay.querySelector<HTMLElement>('#stw-row-wizard')!.click();

    expect(document.querySelector<HTMLElement>('.stw-wizard-option')?.textContent).toContain('Meta');
    document.querySelector<HTMLElement>('.stw-wizard-option[data-path="LA_LongBeach.AirQuality.AQ-S-0140.meta"]')!.click();

    const rows = Array.from(document.querySelectorAll<HTMLElement>('.stw-row-editor'));
    expect(rows).toHaveLength(3);
    expect(rows.map(row => row.querySelector<HTMLInputElement>('.stw-r-label')?.value)).toEqual(['Online Status', 'Device Mode', 'Updated']);
    expect(rows.map(row => row.querySelector<HTMLInputElement>('.stw-c-tag')?.value)).toEqual(['meta.online_status', 'meta.deviceMode', 'meta.updated']);
    expect(rows.map(row => row.querySelector<HTMLSelectElement>('.stw-c-fmt')?.value)).toEqual(['text', 'text', 'number']);
    expect(rows.every(row => row.querySelector<HTMLElement>('.stw-col-editor[data-col="col3"] .stw-c-tag') === null)).toBe(true);
  });

  it('offers top-level tags as a pseudo group for array element tables', () => {
    mockStore.listChildrenNames.mockImplementation((path: string) => {
      if (path === 'default.WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0') return ['discharge_pres_kpa', 'run_status', 'alarms'];
      if (path === 'default.WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0.alarms') return ['highPressure'];
      return [];
    });
    mockStore.getNodeType.mockImplementation((path: string) => {
      if (path === 'default.WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0.alarms') return 'node';
      return 'leaf';
    });
    valueByPath = {
      'default.WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0.discharge_pres_kpa': 330,
      'default.WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0.run_status': 'RUN',
    };
    const widget = document.createElement('status-table-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      headerText: 'Pump',
      tagPrefix: 'WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0',
      rows: [],
    });

    widget.openConfig();
    const overlay = document.querySelector<HTMLElement>('#stw-backdrop')!.parentElement!;
    overlay.querySelector<HTMLElement>('#stw-row-wizard')!.click();

    const optionLabels = Array.from(document.querySelectorAll<HTMLElement>('.stw-wizard-option')).map(el => el.textContent);
    expect(optionLabels.some(label => label?.includes('Top Level Tags'))).toBe(true);
    expect(optionLabels.some(label => label?.includes('Alarms'))).toBe(true);

    document.querySelector<HTMLElement>('.stw-wizard-option[data-path="WaterWorks.BOOSTER_STATION.NORTH_BOOSTER.pumps.0"]')!.click();

    const rows = Array.from(document.querySelectorAll<HTMLElement>('.stw-row-editor'));
    expect(rows).toHaveLength(2);
    expect(rows.map(row => row.querySelector<HTMLInputElement>('.stw-r-label')?.value)).toEqual(['Discharge Pres Kpa', 'Run Status']);
    expect(rows.map(row => row.querySelector<HTMLInputElement>('.stw-c-tag')?.value)).toEqual(['discharge_pres_kpa', 'run_status']);
    expect(rows.map(row => row.querySelector<HTMLSelectElement>('.stw-c-fmt')?.value)).toEqual(['number', 'text']);
  });
});
