import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { PropertyField } from '../src/dashboards/widgets/widget-properties-dialog';

const treeDialogMock = vi.hoisted(() => ({
  open: vi.fn(),
}));

const storeMock = vi.hoisted(() => ({
  toAbsolute: vi.fn((path: string) => path.startsWith('default.') ? path : `default.${path}`),
  listChildrenNames: vi.fn((path: string) => path === 'default.LA_LongBeach.AirQuality' ? ['AQ-S-0140'] : []),
}));

const uiStoreMock = vi.hoisted(() => ({
  get: vi.fn(() => ''),
}));

vi.mock('../src/components/tree-browser-dialog', () => ({
  getTreeBrowserDialog: () => treeDialogMock,
}));

vi.mock('../src/store/store', () => ({
  getMirrorStore: () => storeMock,
}));

vi.mock('../src/store/ui-store', () => ({
  getUiStore: () => uiStoreMock,
}));

import '../src/dashboards/widgets/widget-properties-dialog';

function createDialog(): any {
  const dialog = document.createElement('widget-properties-dialog') as any;
  document.body.appendChild(dialog);
  return dialog;
}

function portal(): HTMLElement {
  const form = document.querySelector<HTMLElement>('#wpd-form');
  if (!form) throw new Error('properties dialog form not found');
  return form.closest('div')?.parentElement ?? document.body;
}

function submitDialog(): void {
  const form = document.querySelector<HTMLFormElement>('#wpd-form');
  if (!form) throw new Error('form not found');
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
}

function updatesFor(dialog: HTMLElement): CustomEvent[] {
  const updates: CustomEvent[] = [];
  dialog.addEventListener('properties-updated', ((event: CustomEvent) => updates.push(event)) as EventListener);
  return updates;
}

function closedFor(dialog: HTMLElement): CustomEvent[] {
  const closed: CustomEvent[] = [];
  dialog.addEventListener('properties-closed', ((event: CustomEvent) => closed.push(event)) as EventListener);
  return closed;
}

describe('widget-properties-dialog', () => {
  beforeEach(() => {
    treeDialogMock.open.mockReset();
    storeMock.toAbsolute.mockClear();
    storeMock.listChildrenNames.mockClear();
    uiStoreMock.get.mockClear();
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        prefix: 'mdi',
        icons: { pump: { body: '<path d="M0 0h1v1z"/>' } },
      }),
    } as Response)));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    document.querySelectorAll('widget-properties-dialog').forEach(el => el.remove());
    document.body.querySelectorAll('.fixed.inset-0').forEach(el => el.remove());
  });

  it('renders standard field types and submits normalized values', () => {
    const dialog = createDialog();
    const updates = updatesFor(dialog);
    const schema: PropertyField[] = [
      { name: 'title', type: 'string', label: 'Title', default: 'Chart' },
      { name: 'enabled', type: 'boolean', label: 'Enabled', default: true },
      { name: 'refresh', type: 'number', label: 'Refresh', default: 30, context: { min: 1, max: 300, step: 5 } },
      { name: 'tagPath', type: 'path', label: 'Tag path', context: { includeLeaves: true } },
      { name: 'color', type: 'color', label: 'Color', default: '#ff0000' },
      { name: 'icon', type: 'icon', label: 'Icon', default: 'mdi:pump' },
      { name: 'mode', type: 'select', label: 'Mode', default: 'compact', context: { options: [
        { value: 'compact', label: 'Compact' },
        { value: 'full', label: 'Full' },
      ] } },
    ];

    dialog.open('widget-1', schema, {
      title: 'Trend',
      enabled: false,
      refresh: 60,
      tagPath: 'Plant.Pump.Temp',
      color: '#00ff00',
      icon: 'mdi:water',
      mode: 'full',
    });

    expect(document.querySelector('#wpd-form')).toBeTruthy();
    expect(document.querySelector<HTMLInputElement>('#prop-refresh')?.min).toBe('1');
    expect(document.querySelector<HTMLInputElement>('#prop-refresh')?.max).toBe('300');
    expect(document.querySelector<HTMLInputElement>('#prop-refresh')?.step).toBe('5');

    document.querySelector<HTMLInputElement>('#prop-title')!.value = '';
    document.querySelector<HTMLInputElement>('#prop-enabled')!.checked = true;
    document.querySelector<HTMLInputElement>('#prop-refresh')!.value = '';
    document.querySelector<HTMLInputElement>('#prop-tagPath')!.value = 'Plant.Pump.Flow';
    document.querySelector<HTMLInputElement>('#prop-color')!.value = '#123456';
    (document.querySelector('#prop-icon') as any).value = 'mdi:gauge';
    document.querySelector<HTMLSelectElement>('#prop-mode')!.value = 'compact';

    submitDialog();

    expect(updates).toHaveLength(1);
    expect(updates[0].detail).toEqual({
      widgetId: 'widget-1',
      config: {
        title: '',
        enabled: true,
        refresh: 30,
        tagPath: 'Plant.Pump.Flow',
        color: '#123456',
        icon: 'mdi:gauge',
        mode: 'compact',
      },
    });
    expect(document.querySelector('#wpd-form')).toBeNull();
  });

  it('uses the supplied dialog title instead of the generic heading', () => {
    const dialog = createDialog();

    dialog.open('gauge-widget', [
      { name: 'headerText', type: 'string', label: 'Header text' },
    ], { headerText: 'Gauge' }, false, 'Gauge');

    expect(document.querySelector('h3')?.textContent).toBe('Gauge');
  });

  it('toggles named sections while preserving edited form state', () => {
    const dialog = createDialog();
    const updates = updatesFor(dialog);
    dialog.open('widget-2', [
      { name: 'always', type: 'string', label: 'Always' },
      { name: 'advanced', type: 'section', label: 'Advanced', description: 'Advanced fields' },
      { name: 'inside', type: 'string', label: 'Inside section', default: 'default' },
    ], { always: 'visible', inside: 'before' });

    const sectionBody = () => document.querySelector<HTMLElement>('.wpd-section-body')!;
    expect(sectionBody().style.display).toBe('none');

    document.querySelector<HTMLInputElement>('#prop-always')!.value = 'changed';
    document.querySelector<HTMLButtonElement>('.wpd-section-hdr')!.click();
    expect(sectionBody().style.display).toBe('');
    expect(document.querySelector<HTMLInputElement>('#prop-always')!.value).toBe('changed');

    document.querySelector<HTMLInputElement>('#prop-inside')!.value = 'after';
    submitDialog();
    expect(updates[0].detail.config).toMatchObject({ always: 'changed', inside: 'after' });
  });

  it('renders section toggles as prominent headers', () => {
    const dialog = createDialog();
    dialog.open('widget-sections', [
      { name: 'series1', type: 'section', label: 'Series 1 - AQ index' },
      { name: 'series1Name', type: 'string', label: 'Name' },
    ], { series1Name: 'AQ index' });

    const header = document.querySelector<HTMLElement>('.wpd-section-hdr')!;
    const style = header.getAttribute('style') ?? '';
    expect(header.textContent).toContain('Series 1 - AQ index');
    expect(style).toContain('border-left:4px solid var(--accent-color)');
    expect(style).toContain('linear-gradient');
    expect(header.className).toContain('py-2');
  });

  it('disables dependent fields until their boolean source is enabled', () => {
    const dialog = createDialog();
    dialog.open('widget-dependent', [
      { name: 'showSparkline', type: 'boolean', label: 'Show sparkline', default: true },
      {
        name: 'refreshInterval',
        type: 'select',
        label: 'Refresh interval',
        default: '0',
        context: {
          disabledUnlessField: 'showSparkline',
          options: [
            { value: '0', label: 'Off' },
            { value: '60', label: '1 min' },
          ],
        },
      },
    ], { showSparkline: false, refreshInterval: '60' });

    const checkbox = document.querySelector<HTMLInputElement>('#prop-showSparkline')!;
    const select = document.querySelector<HTMLSelectElement>('#prop-refreshInterval')!;

    expect(select.disabled).toBe(true);
    checkbox.checked = true;
    checkbox.dispatchEvent(new Event('change', { bubbles: true }));
    expect(select.disabled).toBe(false);
    checkbox.checked = false;
    checkbox.dispatchEvent(new Event('change', { bubbles: true }));
    expect(select.disabled).toBe(true);
  });

  it('adds, browses, removes, and filters path-list values', () => {
    const dialog = createDialog();
    const updates = updatesFor(dialog);
    dialog.open('widget-list', [
      { name: 'paths', type: 'path-list', label: 'Paths', default: [] },
    ], { paths: ['Plant.Pump.Temp'] });

    document.querySelector<HTMLButtonElement>('.wpd-add-list-btn')!.click();
    let inputs = Array.from(document.querySelectorAll<HTMLInputElement>('.wpd-list-input'));
    expect(inputs).toHaveLength(2);
    inputs[0].value = 'Plant.Pump.Flow';
    inputs[1].value = '';

    document.querySelector<HTMLButtonElement>('.wpd-browse-list-btn[data-index="1"]')!.click();
    expect(treeDialogMock.open).toHaveBeenCalledWith('', 'Select Node', expect.any(Function), false, '');
    const browseCallback = treeDialogMock.open.mock.calls.at(-1)![2] as (path: string) => void;
    browseCallback('Plant.Pump.Speed');

    inputs = Array.from(document.querySelectorAll<HTMLInputElement>('.wpd-list-input'));
    expect(inputs.map(input => input.value)).toEqual(['Plant.Pump.Flow', 'Plant.Pump.Speed']);

    document.querySelector<HTMLButtonElement>('.wpd-remove-list-btn[data-index="0"]')!.click();
    inputs = Array.from(document.querySelectorAll<HTMLInputElement>('.wpd-list-input'));
    expect(inputs.map(input => input.value)).toEqual(['Plant.Pump.Speed']);

    document.querySelector<HTMLButtonElement>('.wpd-add-list-btn')!.click();
    submitDialog();
    expect(updates[0].detail.config.paths).toEqual(['Plant.Pump.Speed']);
  });

  it('adds, edits, moves, collapses, browses, and removes column-list rows', () => {
    const dialog = createDialog();
    const updates = updatesFor(dialog);
    dialog.open('widget-columns', [
      {
        name: 'columns',
        type: 'column-list',
        label: 'Columns',
        description: 'Displayed table columns',
        context: { parentNodeDepth: 2 },
      },
    ], {
      columns: [
        { header: 'Temp', tagPath: 'temperature', formatter: 'number', width: '6rem' },
        { header: 'State', tagPath: 'state', formatter: 'text', width: '8rem' },
      ],
    });

    const headers = () => Array.from(document.querySelectorAll<HTMLInputElement>('.wpd-col-header')).map(input => input.value);
    expect(headers()).toEqual(['Temp', 'State']);

    document.querySelector<HTMLInputElement>('.wpd-col-header[data-index="0"]')!.value = 'Temperature';
    document.querySelector<HTMLInputElement>('.wpd-col-width[data-index="0"]')!.value = '7rem';
    document.querySelector<HTMLButtonElement>('.wpd-move-col-down-btn[data-index="0"]')!.click();
    expect(headers()).toEqual(['State', 'Temperature']);

    document.querySelector<HTMLButtonElement>('.wpd-move-col-up-btn[data-index="1"]')!.click();
    expect(headers()).toEqual(['Temperature', 'State']);

    document.querySelector<HTMLButtonElement>('.wpd-add-col-btn')!.click();
    expect(document.querySelectorAll('.wpd-col-row')).toHaveLength(3);

    document.querySelector<HTMLButtonElement>('.wpd-browse-col-btn[data-index="2"]')!.click();
    expect(treeDialogMock.open).toHaveBeenCalledWith('', 'Select Tag', expect.any(Function), true);
    const browseCallback = treeDialogMock.open.mock.calls.at(-1)![2] as (path: string) => void;
    browseCallback('Org.Device.Pump1.sign.message');
    expect(document.querySelector<HTMLInputElement>('.wpd-col-tagpath[data-index="2"]')!.value).toBe('sign.message');

    document.querySelector<HTMLButtonElement>('.wpd-col-section-hdr')!.click();
    expect(document.querySelector<HTMLElement>('.wpd-col-section-body')!.style.display).toBe('none');
    document.querySelector<HTMLButtonElement>('.wpd-col-section-hdr')!.click();
    expect(document.querySelector<HTMLElement>('.wpd-col-section-body')!.style.display).toBe('');

    document.querySelector<HTMLButtonElement>('.wpd-remove-col-btn[data-index="1"]')!.click();
    submitDialog();

    expect(updates[0].detail.config.columns).toEqual([
      { header: 'Temperature', tagPath: 'temperature', formatter: 'number', width: '7rem' },
      { header: '', tagPath: 'sign.message', formatter: 'text', width: '' },
    ]);
  });

  it('browses a single path field with includeLeaves and current expand target', () => {
    const dialog = createDialog();
    dialog.open('widget-path', [
      { name: 'tag', type: 'path', label: 'Tag', context: { includeLeaves: true } },
    ], { tag: 'Plant.Pump.*.Temp' });

    document.querySelector<HTMLButtonElement>('.wpd-browse-btn')!.click();
    expect(treeDialogMock.open).toHaveBeenCalledWith('', 'Select Tag', expect.any(Function), true, 'Plant.Pump', 'Plant.Pump');
    const callback = treeDialogMock.open.mock.calls.at(-1)![2] as (path: string) => void;
    callback('Plant.Pump.Flow');
    expect(document.querySelector<HTMLInputElement>('#prop-tag')!.value).toBe('Plant.Pump.Flow');
  });

  it('browses a path field under a wildcard prefix and stores the relative tag path', () => {
    const dialog = createDialog();
    dialog.open('timeseries-widget', [
      { name: 'series1TagPrefix', type: 'path', label: 'Tag prefix', context: { includeLeaves: false } },
      {
        name: 'series1TagPath',
        type: 'path',
        label: 'Tag path',
        context: { includeLeaves: true, rootFromField: 'series1TagPrefix', stripBrowseRoot: true },
      },
    ], {
      series1TagPrefix: 'LA_LongBeach.AirQuality.*',
      series1TagPath: 'air.aqi',
    });

    document.querySelector<HTMLButtonElement>('.wpd-browse-btn[data-prop="series1TagPath"]')!.click();

    expect(treeDialogMock.open).toHaveBeenCalledWith(
      'LA_LongBeach.AirQuality.AQ-S-0140',
      'Select Tag',
      expect.any(Function),
      true,
      'LA_LongBeach.AirQuality.AQ-S-0140.air.aqi',
      'LA_LongBeach.AirQuality.AQ-S-0140.air.aqi',
    );

    const callback = treeDialogMock.open.mock.calls.at(-1)![2] as (path: string) => void;
    callback('LA_LongBeach.AirQuality.AQ-S-0140.air.pm25');

    expect(document.querySelector<HTMLInputElement>('#prop-series1TagPath')!.value).toBe('air.pm25');
  });

  it('uses hidden root field values when stripping browsed tag paths', () => {
    const dialog = createDialog();
    dialog.open('array-child', [
      {
        name: 'tagPath',
        type: 'path',
        label: 'Tag path',
        context: { includeLeaves: true, rootFromField: 'tagPrefix', stripBrowseRoot: true },
      },
    ], {
      tagPrefix: 'WaterWorks.PUMP.PUMP_01',
      tagPath: 'sensors.status',
    });

    document.querySelector<HTMLButtonElement>('.wpd-browse-btn[data-prop="tagPath"]')!.click();

    expect(treeDialogMock.open).toHaveBeenCalledWith(
      'WaterWorks.PUMP.PUMP_01',
      'Select Tag',
      expect.any(Function),
      true,
      'WaterWorks.PUMP.PUMP_01.sensors.status',
      'WaterWorks.PUMP.PUMP_01.sensors.status',
    );

    const callback = treeDialogMock.open.mock.calls.at(-1)![2] as (path: string) => void;
    callback('WaterWorks.PUMP.PUMP_01.sensors.flow');

    expect(document.querySelector<HTMLInputElement>('#prop-tagPath')!.value).toBe('sensors.flow');
  });

  it('read-only mode disables editing, hides apply, and only emits closed', () => {
    const dialog = createDialog();
    const updates = updatesFor(dialog);
    const closed = closedFor(dialog);
    dialog.open('readonly-widget', [
      { name: 'title', type: 'string', label: 'Title' },
      { name: 'enabled', type: 'boolean', label: 'Enabled' },
      { name: 'paths', type: 'path-list', label: 'Paths' },
      { name: 'columns', type: 'column-list', label: 'Columns' },
    ], {
      title: 'Read only',
      enabled: true,
      paths: ['Plant.Pump.Temp'],
      columns: [{ header: 'Temp', tagPath: 'temp', formatter: 'text', width: '' }],
    }, true);

    expect(portal().textContent).toContain('Read only');
    expect(document.querySelector<HTMLButtonElement>('button[type="submit"]')).toBeNull();
    expect(Array.from(document.querySelectorAll<HTMLInputElement | HTMLButtonElement | HTMLSelectElement>('input, button, select'))
      .filter(el => !['wpd-close', 'wpd-cancel'].includes(el.id))
      .every(el => el.disabled || el.classList.contains('wpd-col-section-hdr'))).toBe(true);

    submitDialog();
    expect(updates).toHaveLength(0);
    expect(closed).toHaveLength(1);
    expect(closed[0].detail.widgetId).toBe('readonly-widget');
  });

  it('close and disconnect clean up the portal and emit properties-closed', () => {
    const dialog = createDialog();
    const closed = closedFor(dialog);
    dialog.open('widget-close', [{ name: 'title', type: 'string', label: 'Title' }], { title: 'Close me' });
    expect(document.querySelector('#wpd-form')).toBeTruthy();

    document.querySelector<HTMLButtonElement>('#wpd-close')!.click();
    expect(closed).toHaveLength(1);
    expect(closed[0].detail.widgetId).toBe('widget-close');
    expect(document.querySelector('#wpd-form')).toBeNull();

    dialog.open('widget-close-2', [{ name: 'title', type: 'string', label: 'Title' }], { title: 'Remove me' });
    expect(document.querySelector('#wpd-form')).toBeTruthy();
    dialog.remove();
    expect(document.querySelector('#wpd-form')).toBeNull();
  });
});
