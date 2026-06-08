import { afterEach, describe, expect, it, vi } from 'vitest';

const devices = ['AQ-S-0001', 'AQ-S-0002', 'AQ-B-0001', 'AQ-B-0002'];
const mockStore = {
  toAbsolute: vi.fn((path: string) => `default.${path}`),
  listChildrenNames: vi.fn(() => devices),
  resolveTagReference: vi.fn((path: string) => {
    if (path.endsWith('.AQ-B-0001:description')) return 'Beach sensor';
    if (path.endsWith('.AQ-B-0002:description')) return 'Roadside monitor';
    if (path.endsWith('.meta.description')) return 'Ignored meta description';
    return undefined;
  }),
  subscribeToTreeChanges: vi.fn(() => vi.fn()),
};

let selectedDevice = '';
const mockUiStore = {
  get: vi.fn((key: string) => key === 'deviceName' ? selectedDevice : ''),
  set: vi.fn((key: string, value: string) => {
    if (key === 'deviceName') selectedDevice = value;
  }),
};

vi.mock('../src/store/store', () => ({
  getMirrorStore: () => mockStore,
}));

vi.mock('../src/store/ui-store', () => ({
  getUiStore: () => mockUiStore,
}));

vi.mock('../src/api', () => ({
  listDashboards: vi.fn(async () => []),
}));

import '../src/dashboards/widgets/dashboard-nav-widget';

function createWidget(): HTMLElement {
  const widget = document.createElement('dashboard-nav-widget') as any;
  document.body.appendChild(widget);
  widget.setConfig({ deviceParentPath: 'LA_LongBeach.AirQuality' });
  return widget;
}

function optionLabels(widget: HTMLElement): string[] {
  return [...widget.querySelectorAll<HTMLElement>('.dnw-option-name')].map(el => el.textContent ?? '');
}

describe('dashboard-nav-widget device picker', () => {
  afterEach(() => {
    selectedDevice = '';
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('filters device options as the user types', () => {
    const widget = createWidget();
    const input = widget.querySelector<HTMLInputElement>('.dnw-combo-input')!;

    input.focus();
    input.value = 'AQ-B';
    input.dispatchEvent(new Event('input', { bubbles: true }));

    expect(optionLabels(widget)).toEqual(['AQ-B-0001', 'AQ-B-0002']);
    expect(widget.querySelector('.dnw-options')?.classList.contains('open')).toBe(true);
  });

  it('shows device descriptions next to device names in the dropdown', () => {
    const widget = createWidget();
    const input = widget.querySelector<HTMLInputElement>('.dnw-combo-input')!;

    input.focus();
    input.value = 'Beach';
    input.dispatchEvent(new Event('input', { bubbles: true }));

    const option = widget.querySelector<HTMLElement>('.dnw-option')!;
    expect(option.dataset.device).toBe('AQ-B-0001');
    expect(option.querySelector('.dnw-option-name')?.textContent).toBe('AQ-B-0001');
    expect(option.querySelector('.dnw-option-description')?.textContent).toBe('Beach sensor');
    expect(option.title).toBe('AQ-B-0001 - Beach sensor');
  });

  it('uses the device description metadata selector instead of meta.description', () => {
    const widget = createWidget();
    const input = widget.querySelector<HTMLInputElement>('.dnw-combo-input')!;

    input.focus();
    input.value = 'Roadside';
    input.dispatchEvent(new Event('input', { bubbles: true }));

    const option = widget.querySelector<HTMLElement>('.dnw-option')!;
    expect(option.dataset.device).toBe('AQ-B-0002');
    expect(option.querySelector('.dnw-option-name')?.textContent).toBe('AQ-B-0002');
    expect(option.querySelector('.dnw-option-description')?.textContent).toBe('Roadside monitor');
    expect(mockStore.resolveTagReference).not.toHaveBeenCalledWith(expect.stringContaining('.meta.description'));
  });

  it('keeps the selected device text when the input receives focus', () => {
    selectedDevice = 'AQ-B-0001';
    const widget = createWidget();
    const input = widget.querySelector<HTMLInputElement>('.dnw-combo-input')!;

    expect(input.value).toBe('AQ-B-0001');

    input.focus();
    expect(input.value).toBe('AQ-B-0001');

    input.value = 'AQ-B-000';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    expect(optionLabels(widget)).toEqual(['AQ-B-0001', 'AQ-B-0002']);
  });

  it('selects a filtered device and updates the dashboard variable', () => {
    const widget = createWidget();
    const input = widget.querySelector<HTMLInputElement>('.dnw-combo-input')!;
    const opened = vi.fn();
    widget.addEventListener('dashboard-open', opened);

    input.focus();
    input.value = 'AQ-B-0002';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    widget.querySelector<HTMLElement>('.dnw-option')?.click();

    expect(mockUiStore.set).toHaveBeenCalledWith('deviceName', 'AQ-B-0002');
    expect(selectedDevice).toBe('AQ-B-0002');
    expect(input.value).toBe('AQ-B-0002');
    expect(widget.querySelector('.dnw-options')?.classList.contains('open')).toBe(false);
    expect(opened).toHaveBeenCalledWith(expect.objectContaining({
      detail: expect.objectContaining({
        dashboard: '',
        devicePath: 'LA_LongBeach.AirQuality.AQ-B-0002',
      }),
    }));
  });

  it('deduplicates target dashboard options by name while preserving the selected duplicate id', () => {
    const widget = document.createElement('dashboard-nav-widget') as any;
    widget.dashboards = [
      { id: 'dash-1', name: 'Device Detail', isCategory: false },
      { id: 'dash-2', name: 'Device Detail', isCategory: false },
      { id: 'dash-3', name: 'Operations', isCategory: false },
      { id: 'cat-1', name: 'Device Detail', isCategory: true },
    ];
    widget.setConfig({ targetDashboardId: 'dash-2' });

    const field = widget.getPropertySchema().find((f: any) => f.name === 'targetDashboardId') as any;
    const options = field.context.options as Array<{ value: string; label: string }>;

    expect(options.filter(option => option.label === 'Device Detail')).toHaveLength(1);
    expect(options.find(option => option.label === 'Device Detail')?.value).toBe('dash-2');
    expect(options.map(option => option.label)).toEqual(['Current dashboard', 'Device Detail', 'Operations']);
  });
});
