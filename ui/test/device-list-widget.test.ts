import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const deviceNames = Array.from({ length: 120 }, (_, idx) => `AQ-S-${String(idx + 1).padStart(4, '0')}`);
const parentPath = 'LA_LongBeach.AirQuality';
const absParentPath = `default.${parentPath}`;
const backupParentPath = 'LA_LongBeach.Backup';
const absBackupParentPath = `default.${backupParentPath}`;

let childrenByPath: Record<string, string[]> = {};
let valuesByPath: Record<string, unknown> = {};

const mockStore = {
  startKvWatch: vi.fn(),
  toAbsolute: vi.fn((path: string) => path.startsWith('default.') ? path : `default.${path}`),
  listChildrenNames: vi.fn((path: string) => childrenByPath[path] ?? []),
  getNodeValue: vi.fn((path: string) => valuesByPath[path] ?? (path.endsWith('.meta.name') ? path.split('.').at(-3) : undefined)),
  resolveTagReference: vi.fn((path: string) => valuesByPath[path]),
  baseTagPath: vi.fn((path: string) => String(path).split(':')[0]),
  getNodeType: vi.fn(() => 'leaf'),
  subscribeTagReference: vi.fn((_path: string, callback: (value: unknown) => void) => {
    callback(undefined);
    return vi.fn();
  }),
  subscribe: vi.fn(() => vi.fn()),
  subscribeToTreeChanges: vi.fn(() => vi.fn()),
};

const apiMock = vi.hoisted(() => ({
  listDashboards: vi.fn(async () => [
    { id: 'air-quality-detail', name: 'Air Quality Detail', isCategory: false, sortOrder: 1 },
    { id: 'backup-detail', name: 'Backup Detail', isCategory: false, sortOrder: 2 },
  ]),
}));

vi.mock('../src/store/store', () => ({
  getMirrorStore: () => mockStore,
}));

vi.mock('../src/api', () => ({
  listDashboards: apiMock.listDashboards,
}));

vi.mock('../src/auth', () => ({
  getCurrentUser: () => ({ tenant_id: 'default' }),
}));

import '../src/dashboards/widgets/device-list-widget';

async function flushMicrotasks(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe('device-list-widget parent configuration', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    apiMock.listDashboards.mockResolvedValue([
      { id: 'air-quality-detail', name: 'Air Quality Detail', isCategory: false, sortOrder: 1 },
      { id: 'backup-detail', name: 'Backup Detail', isCategory: false, sortOrder: 2 },
    ]);
    childrenByPath = {
      [absParentPath]: deviceNames,
      [absBackupParentPath]: ['BK-0001'],
    };
    valuesByPath = {};
    Element.prototype.scrollIntoView = vi.fn();
  });

  it('exposes row click dashboards as per-node dashboard select fields', async () => {
    const widget = document.createElement('device-list-widget') as any;
    document.body.appendChild(widget);
    widget.dashboards = [
      { id: 'air-quality-detail', name: 'Air Quality Detail', isCategory: false, sortOrder: 1 },
      { id: 'category', name: 'Category', isCategory: true, sortOrder: 2 },
    ];
    widget.setConfig({
      parentNodes: [parentPath, backupParentPath],
      clickDashboards: JSON.stringify({ [backupParentPath]: 'backup-detail' }),
    });

    const schema = widget.getPropertySchema();
    const field = schema.find((item: any) => item.name === '__click_0');
    const backupField = schema.find((item: any) => item.name === '__click_1');

    expect(field).toMatchObject({
      type: 'select',
      label: 'Row click dashboard',
      context: {
        options: expect.arrayContaining([
          { value: '', label: '(none)' },
          { value: 'air-quality-detail', label: 'Air Quality Detail' },
        ]),
      },
    });
    expect(backupField).toMatchObject({
      type: 'select',
      label: 'Row click dashboard',
      default: '',
    });
    expect(widget.getConfig().__click_1).toBe('backup-detail');
    expect(schema.some((item: any) => item.name === 'clickDashboardId')).toBe(false);
    expect(schema.some((item: any) => item.name === 'clickDashboards')).toBe(false);
  });

  it('exposes and applies a configurable widget header text', () => {
    const widget = document.createElement('device-list-widget') as any;
    const setTitle = vi.fn();
    const setHeaderVisible = vi.fn();
    vi.spyOn(widget, 'closest').mockReturnValue({ setTitle, setHeaderVisible } as any);
    document.body.appendChild(widget);

    const field = widget.getPropertySchema().find((item: any) => item.name === 'headerText');
    widget.setConfig({ headerText: 'Coastal Sensors' });
    widget.rerender();

    expect(field).toMatchObject({
      type: 'string',
      label: 'Header text',
      default: 'Device List',
    });
    expect(widget.getConfig().headerText).toBe('Coastal Sensors');
    expect(setTitle).toHaveBeenLastCalledWith('Coastal Sensors');
    expect(setHeaderVisible).toHaveBeenLastCalledWith(true);
  });

  it('allows a node dashboard to be explicitly cleared when legacy global row click exists', () => {
    const widget = document.createElement('device-list-widget') as any;
    document.body.appendChild(widget);

    widget.setConfig({
      parentNodes: [parentPath],
      clickDashboardId: 'air-quality-detail',
      __click_0: '',
    });

    expect(widget.getConfig().__click_0).toBe('');
    expect(JSON.parse(widget.getConfig().clickDashboards)).toEqual({ [parentPath]: '' });
  });

  afterEach(() => {
    document.body.innerHTML = '';
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('batches row subscriptions instead of subscribing every child synchronously', () => {
    const widget = document.createElement('device-list-widget') as any;
    document.body.appendChild(widget);
    const patchSpy = vi.spyOn(widget, 'patchTableBody');

    widget.setConfig({ parentNodes: [parentPath] });

    expect(mockStore.subscribeTagReference).not.toHaveBeenCalled();

    vi.advanceTimersToNextTimer();

    expect(mockStore.subscribeTagReference.mock.calls.length).toBeGreaterThan(0);
    expect(mockStore.subscribeTagReference.mock.calls.length).toBeLessThanOrEqual(100);
    expect(patchSpy).not.toHaveBeenCalled();
  });

  it('renders configured columns, formats values, sorts rows, pages, and emits row clicks', async () => {
    childrenByPath[absParentPath] = ['AQ-S-0002', 'AQ-S-0001', 'AQ-S-0003'];
    valuesByPath = {
      [`default.${parentPath}.AQ-S-0001.meta.name`]: 'Alpha',
      [`default.${parentPath}.AQ-S-0001.metrics.pm25`]: 3.456,
      [`default.${parentPath}.AQ-S-0001.meta.online`]: true,
      [`default.${parentPath}.AQ-S-0001.meta.commonAlarmPresent`]: false,
      [`default.${parentPath}.AQ-S-0002.meta.name`]: 'Bravo',
      [`default.${parentPath}.AQ-S-0002.metrics.pm25`]: 10,
      [`default.${parentPath}.AQ-S-0002.meta.online`]: false,
      [`default.${parentPath}.AQ-S-0002.meta.commonAlarmPresent`]: true,
      [`default.${parentPath}.AQ-S-0003.meta.name`]: 'Charlie',
      [`default.${parentPath}.AQ-S-0003.metrics.pm25`]: null,
      [`default.${parentPath}.AQ-S-0003.meta.online`]: undefined,
      [`default.${parentPath}.AQ-S-0003.meta.commonAlarmPresent`]: false,
    };
    const widget = document.createElement('device-list-widget') as any;
    const opened = vi.fn();
    widget.addEventListener('dashboard-open', opened);
    document.body.appendChild(widget);

    widget.setConfig({
      parentNodes: [parentPath],
      showPaging: true,
      pageSize: 2,
      __click_0: 'air-quality-detail',
      __col_0: [
        { header: 'Name', tagPath: 'meta.name', formatter: 'text' },
        { header: 'PM2.5', tagPath: 'metrics.pm25', formatter: 'number' },
        { header: 'Online', tagPath: 'meta.online', formatter: 'okfail' },
        { header: 'Alarm', tagPath: 'meta.commonAlarmPresent', formatter: 'cross' },
      ],
    });
    await widget.loadDevicesForPath(parentPath);
    widget.rerender();

    expect([...widget.querySelectorAll('.dlw-row')].map((row: Element) => row.textContent)).toEqual([
      expect.stringContaining('Alpha'),
      expect.stringContaining('Bravo'),
    ]);
    expect(widget.textContent).toContain('3.46');
    expect(widget.textContent).toContain('✓');
    expect(widget.textContent).toContain('✗');
    expect(widget.querySelector('.dlw-page-info')?.textContent).toBe('1 / 2');

    widget.querySelector<HTMLElement>('.dlw-next')!.click();
    expect(widget.querySelector('.dlw-row')?.textContent).toContain('Charlie');

    widget.querySelector<HTMLElement>('th[data-col="1"]')!.click();
    expect(widget.querySelector('.dlw-row')?.textContent).toContain('Charlie');
    widget.querySelector<HTMLElement>('th[data-col="1"]')!.click();
    expect(widget.querySelector('.dlw-row')?.textContent).toContain('Bravo');

    widget.querySelector<HTMLElement>('.dlw-row')!.click();
    expect(opened).toHaveBeenCalledWith(expect.objectContaining({
      detail: {
        dashboard: 'air-quality-detail',
        id: 'air-quality-detail',
        devicePath: `${parentPath}.AQ-S-0002`,
      },
    }));
  });

  it('uses the selected node row click dashboard and saves resized column widths', async () => {
    childrenByPath[absParentPath] = ['AQ-S-0001'];
    childrenByPath[absBackupParentPath] = ['BK-0001'];
    valuesByPath = {
      [`default.${parentPath}.AQ-S-0001.meta.name`]: 'Alpha',
      [`default.${backupParentPath}.BK-0001.meta.name`]: 'Backup',
    };
    const widget = document.createElement('device-list-widget') as any;
    const opened = vi.fn();
    const saved = vi.fn();
    widget.addEventListener('dashboard-open', opened);
    widget.addEventListener('widget-config-save', saved);
    document.body.appendChild(widget);

    widget.setConfig({
      parentNodes: [parentPath, backupParentPath],
      __click_0: 'air-quality-detail',
      __click_1: 'backup-detail',
      __col_0: [{ header: 'Name', tagPath: 'meta.name', formatter: 'text' }],
      __col_1: [{ header: 'Name', tagPath: 'meta.name', formatter: 'text' }],
    });
    widget.setDashboardMode('edit');
    await widget.loadDevicesForPath(parentPath);
    widget.rerender();

    widget.querySelector<HTMLElement>('.dlw-row')!.click();
    expect(opened).toHaveBeenLastCalledWith(expect.objectContaining({
      detail: expect.objectContaining({ dashboard: 'air-quality-detail' }),
    }));

    widget.querySelectorAll<HTMLElement>('.dlw-tab')[1].click();
    await flushMicrotasks();
    await widget.loadDevicesForPath(backupParentPath);
    widget.rerender();
    widget.querySelector<HTMLElement>('.dlw-row')!.click();
    expect(opened).toHaveBeenLastCalledWith(expect.objectContaining({
      detail: expect.objectContaining({ dashboard: 'backup-detail' }),
    }));

    const th = widget.querySelector<HTMLElement>('th[data-col="0"]')!;
    vi.spyOn(th, 'getBoundingClientRect').mockReturnValue({
      width: 100,
      height: 20,
      top: 0,
      right: 100,
      bottom: 20,
      left: 0,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect);

    const resizer = widget.querySelector<HTMLElement>('.dlw-col-resizer')!;
    resizer.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, clientX: 100 }));
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 160 }));
    window.dispatchEvent(new PointerEvent('pointerup', { clientX: 160 }));

    expect(saved).toHaveBeenCalledWith(expect.objectContaining({
      detail: expect.objectContaining({
        forceDirty: true,
        config: expect.objectContaining({
          columns: expect.objectContaining({
            [backupParentPath]: [expect.objectContaining({ width: '160px' })],
          }),
        }),
      }),
    }));
  });

  it('searches across tabs, selects a suggestion, clears search, and highlights the device', async () => {
    vi.useRealTimers();
    childrenByPath[absParentPath] = ['AQ-S-0001'];
    childrenByPath[absBackupParentPath] = ['BK-0001'];
    valuesByPath = {
      [`default.${parentPath}.AQ-S-0001.meta.name`]: 'Alpha Sensor',
      [`default.${backupParentPath}.BK-0001.meta.name`]: 'Backup Pump',
    };
    const widget = document.createElement('device-list-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({ parentNodes: [parentPath, backupParentPath], pageSize: 5 });
    await widget.loadDevicesForPath(parentPath);
    widget.rerender();

    const input = widget.querySelector<HTMLInputElement>('.dlw-search-input')!;
    input.value = 'pump';
    input.dispatchEvent(new Event('input', { bubbles: true }));

    expect(widget.querySelector('.dlw-suggestions')?.textContent).toContain('Backup Pump');
    widget.querySelector<HTMLElement>('.dlw-suggestion')!.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    await flushMicrotasks();
    await flushMicrotasks();

    expect(widget.querySelector('.dlw-tab.active')?.textContent).toBe('Backup');
    expect(widget.querySelector('.dlw-row.dlw-highlighted')?.textContent).toContain('Backup Pump');

    const searchAfterSelect = widget.querySelector<HTMLInputElement>('.dlw-search-input')!;
    searchAfterSelect.value = 'zzzz';
    searchAfterSelect.dispatchEvent(new Event('input', { bubbles: true }));
    expect(widget.querySelector('.dlw-no-results')?.textContent).toContain('No matches found');
    widget.querySelector<HTMLElement>('.dlw-search-clear')!.click();
    expect(widget.querySelector('.dlw-suggestions')).toBeNull();
  });

  it('exports sorted rows as CSV and migrates legacy clickPanels config', async () => {
    childrenByPath[absParentPath] = ['AQ-S-0001'];
    valuesByPath = {
      [`default.${parentPath}.AQ-S-0001.meta.name`]: 'Quote " Sensor',
      [`default.${parentPath}.AQ-S-0001.metrics.pm25`]: 12,
    };
    const objectUrl = 'blob:csv';
    const blobs: Array<{ parts: unknown[]; options: BlobPropertyBag | undefined }> = [];
    vi.stubGlobal('Blob', class {
      parts: unknown[];
      options: BlobPropertyBag | undefined;
      constructor(parts: unknown[], options?: BlobPropertyBag) {
        this.parts = parts;
        this.options = options;
        blobs.push(this);
      }
    });
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue(objectUrl);
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    const widget = document.createElement('device-list-widget') as any;
    document.body.appendChild(widget);

    widget.setConfig({
      parentNodes: [parentPath],
      showXlsxExport: true,
      clickPanels: JSON.stringify({ [parentPath]: 'legacy-detail' }),
      __col_0: [
        { header: 'Name', tagPath: 'meta.name', formatter: 'text' },
        { header: 'PM', tagPath: 'metrics.pm25', formatter: 'number' },
      ],
    });
    await widget.loadDevicesForPath(parentPath);
    widget.rerender();
    widget.querySelector<HTMLElement>('.dlw-export-xlsx')!.click();

    expect(widget.getConfig().clickDashboards).toBe(JSON.stringify({ [parentPath]: 'legacy-detail' }));
    expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
    expect(anchorClick).toHaveBeenCalled();
    expect(revokeObjectURL).toHaveBeenCalledWith(objectUrl);

    const csv = String(blobs[0].parts[0]);
    expect(csv).toContain('"Name","PM"');
    expect(csv).toContain('"Quote "" Sensor","12"');
  });
});
