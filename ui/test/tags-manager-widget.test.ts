import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const apiMock = vi.hoisted(() => ({
  updateNode: vi.fn(async () => {}),
  loadNode: vi.fn(async () => ({ name: 'Device', description: 'Pump device', type: 'Device' })),
  loadTag: vi.fn(async () => null),
  createNode: vi.fn(async () => {}),
  deleteNode: vi.fn(async () => {}),
  createTag: vi.fn(async () => {}),
  deleteTag: vi.fn(async () => {}),
  updateTag: vi.fn(async () => {}),
  updateTagValue: vi.fn(async () => {}),
  debugTagPipeline: vi.fn(async () => ({
    steps: [{ name: 'scale', input: 2, output: 4 }],
    finalOutput: 4,
  })),
  loadBlockSchemas: vi.fn(async () => [
    { type: 'publish', label: 'Publish', params: {} },
    { type: 'scale', label: 'Scale', params: { factor: { type: 'number', default: 1 } } },
  ]),
  listNotificationProfiles: vi.fn(async () => []),
}));

const dialogMock = vi.hoisted(() => ({
  showAlert: vi.fn(async () => {}),
  treeOpen: vi.fn(),
}));

const permissionMock = vi.hoisted(() => ({
  can: vi.fn(async () => true),
}));

let childrenByPath: Record<string, string[]> = {};
let nodeTypes: Record<string, 'node' | 'leaf' | 'unknown'> = {};
let sharedByPath: Record<string, any> = {};
let configByPath: Record<string, any> = {};
let valuesByPath: Record<string, any> = {};
let statusByPath: Record<string, string> = {};
let timestampsByPath: Record<string, number> = {};
let canRead = true;
let canWrite = true;

const mockStore = {
  loadMatchingTags: vi.fn(async () => {}),
  ensureChildren: vi.fn(async () => {}),
  loadSelectedPaths: vi.fn(async () => {}),
  subscribeToTreeChanges: vi.fn(() => vi.fn()),
  loadTreeFromAPI: vi.fn(async () => {}),
  listChildrenNames: vi.fn((path: string) => childrenByPath[path] ?? []),
  getNodeType: vi.fn((path: string) => nodeTypes[path] ?? 'node'),
  getNodeShared: vi.fn((path: string) => sharedByPath[path] ?? {}),
  getNodeConfig: vi.fn((path: string) => configByPath[path] ?? {}),
  getIsArray: vi.fn((path: string) => !!configByPath[path]?.isArray),
  subscribe: vi.fn((path: string, callback: (value: any) => void) => {
    callback(valuesByPath[path]);
    return vi.fn();
  }),
  getNodeValue: vi.fn((path: string) => valuesByPath[path]),
  getNodeTimestamp: vi.fn((path: string) => timestampsByPath[path] ?? 0),
  getNodeStatus: vi.fn((path: string) => statusByPath[path] ?? ''),
  removeNode: vi.fn(),
};

vi.mock('../src/store/store', () => ({
  getMirrorStore: () => mockStore,
}));

vi.mock('../src/store/ui-store', () => ({
  getUiStore: () => ({ get: () => 'UTC' }),
}));

vi.mock('../src/auth', () => ({
  getCurrentUser: () => ({ tenant_id: 'default' }),
}));

vi.mock('../src/api', () => apiMock);

vi.mock('../src/components/app-dialog', () => ({
  showAlert: dialogMock.showAlert,
}));

vi.mock('../src/components/tree-browser-dialog', () => ({
  getTreeBrowserDialog: () => ({ open: dialogMock.treeOpen }),
}));

vi.mock('../src/permissions/permissions', () => ({
  can: permissionMock.can,
}));

import '../src/dashboards/widgets/tags-manager-widget';

async function flushMicrotasks(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function seedTree(): void {
  childrenByPath = {
    default: ['Area'],
    'default.Area': ['Device'],
    'default.Area.Device': ['meta', 'temperature', 'enabled', 'mode'],
    'default.Area.Device.meta': ['serial'],
  };
  nodeTypes = {
    default: 'node',
    'default.Area': 'node',
    'default.Area.Device': 'node',
    'default.Area.Device.meta': 'node',
    'default.Area.Device.meta.serial': 'leaf',
    'default.Area.Device.temperature': 'leaf',
    'default.Area.Device.enabled': 'leaf',
    'default.Area.Device.mode': 'leaf',
  };
  sharedByPath = {
    'default.Area': { description: 'Production area' },
    'default.Area.Device': { description: 'Pump device' },
    'default.Area.Device.temperature': { description: 'Process temp', units: 'C', deadband: 0.2 },
    'default.Area.Device.enabled': { description: 'Enabled' },
    'default.Area.Device.mode': {
      description: 'Mode',
      enumValues: { '0': 'Off', '1': 'Auto' },
      pipeline: [{ type: 'scale', parameters: { factor: 2 } }],
    },
  };
  configByPath = {
    default: { type: 'Organisation' },
    'default.Area.Device': { type: 'Device' },
    'default.Area.Device.temperature': { type: 1 },
    'default.Area.Device.enabled': { type: 3 },
    'default.Area.Device.mode': { type: 4 },
  };
  valuesByPath = {
    'default.Area.Device.temperature': 21.5,
    'default.Area.Device.enabled': true,
    'default.Area.Device.mode': 1,
  };
  statusByPath = {
    'default.Area.Device.temperature': 'N',
    'default.Area.Device.enabled': 'A',
    'default.Area.Device.mode': 'S',
  };
  timestampsByPath = {
    'default.Area.Device.temperature': 1_700_000_000_000,
    'default.Area.Device.enabled': 1_700_000_001_000,
    'default.Area.Device.mode': 1_700_000_002_000,
  };
}

describe('tags-manager-widget search', () => {
  beforeEach(() => {
    Element.prototype.scrollIntoView = vi.fn();
    permissionMock.can.mockImplementation(async (permission: string) => permission === 'tags.read' ? canRead : canWrite);
    mockStore.getNodeStatus.mockImplementation((path: string) => statusByPath[path] ?? '');
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    document.body.innerHTML = '';
    canRead = true;
    canWrite = true;
  });

  it('keeps focus and caret after search input re-renders the tree', async () => {
    vi.useFakeTimers();
    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();

    const input = widget.querySelector<HTMLInputElement>('#tv-search');
    expect(input).not.toBeNull();
    input!.focus();
    input!.value = 'pump';
    input!.setSelectionRange(2, 2);
    input!.dispatchEvent(new Event('input', { bubbles: true }));

    vi.advanceTimersByTime(200);
    await flushMicrotasks();

    const rerenderedInput = widget.querySelector<HTMLInputElement>('#tv-search');
    expect(document.activeElement).toBe(rerenderedInput);
    expect(rerenderedInput?.value).toBe('pump');
    expect(rerenderedInput?.selectionStart).toBe(2);
    expect(rerenderedInput?.selectionEnd).toBe(2);
  });

  it('renders and filters the tree, subscribes leaves, and handles tree deletion callbacks', async () => {
    seedTree();
    vi.useFakeTimers();
    let treeCallback: ((path: string, data: any) => void) | undefined;
    mockStore.subscribeToTreeChanges.mockImplementation((_path: string, callback: (path: string, data: any) => void) => {
      treeCallback = callback;
      return vi.fn();
    });
    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    expect(widget.textContent).toContain('Area');
    widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device"]')!.click();
    expect(widget.textContent).toContain('temperature');
    expect(widget.textContent).toContain('21.5');
    expect(widget.textContent).toContain('C');
    expect(mockStore.subscribe).toHaveBeenCalledWith('default.Area.Device.temperature', expect.any(Function));

    widget.querySelector<HTMLElement>('[data-status="A"]')!.click();
    expect(widget.textContent).not.toContain('temperature');
    expect(widget.textContent).toContain('enabled');
    widget.querySelector<HTMLElement>('[data-status="A"]')!.click();

    const search = widget.querySelector<HTMLInputElement>('#tv-search')!;
    search.value = 'mode';
    search.dispatchEvent(new Event('input', { bubbles: true }));
    vi.advanceTimersByTime(200);
    await flushMicrotasks();
    expect(widget.textContent).toContain('mode');

    widget['searchQuery'] = '';
    childrenByPath['default.Area.Device'] = [];
    treeCallback?.('default.Area.Device', null);
    vi.advanceTimersByTime(50);
    expect(widget.textContent).not.toContain('mode');
  });

  it('does not render unresolved or deleted-device placeholders as float tags', async () => {
    seedTree();
    childrenByPath['default.Area'].push('bus_pending', 'bus_deleted');
    nodeTypes['default.Area.bus_pending'] = 'unknown';
    nodeTypes['default.Area.bus_deleted'] = 'unknown';
    // A late value subscription may also have created descendants.
    childrenByPath['default.Area.bus_deleted'] = ['meta'];
    nodeTypes['default.Area.bus_deleted.meta'] = 'unknown';
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    widget.expandedNodes.add('default.Area');
    widget.rerender();
    expect(widget.textContent).not.toContain('bus_pending');
    expect(widget.textContent).not.toContain('bus_deleted');
    expect(mockStore.subscribe).not.toHaveBeenCalledWith('default.Area.bus_pending', expect.any(Function));
    expect(widget.countMatchingLeaves('default.Area')).toBe(4);

    // Once node metadata arrives, the device appears as an expandable group.
    nodeTypes['default.Area.bus_pending'] = 'node';
    childrenByPath['default.Area.bus_pending'] = ['meta'];
    nodeTypes['default.Area.bus_pending.meta'] = 'node';
    widget.rerender();
    expect(widget.querySelector('[data-node-path="default.Area.bus_pending"]')).not.toBeNull();
    expect(mockStore.subscribe).not.toHaveBeenCalledWith('default.Area.bus_pending', expect.any(Function));
  });

  it('coalesces 400,000 descendant deletes into one refresh and resubscribes recreated tags', async () => {
    seedTree();
    vi.useFakeTimers();
    let treeCallback: (path: string, data: any) => void = () => {};
    mockStore.subscribeToTreeChanges.mockImplementation((_path: string, callback: typeof treeCallback) => {
      treeCallback = callback;
      return vi.fn();
    });
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();
    widget.expandedNodes.add('default.Area');
    widget.expandedNodes.add('default.Area.Device');
    widget.rerender();
    widget.subscribedPaths.add('default.Other.tag');
    widget.valueCache.set('default.Other.tag', { value: 1 });
    const render = vi.spyOn(widget, 'rerender');
    childrenByPath['default.Area'] = [];
    treeCallback('default.Area.Device', null);
    for (let i = 0; i < 400_000; i++) treeCallback(`default.Area.Device.stop${i}.name`, null);
    expect(render).not.toHaveBeenCalled();
    expect(widget.pendingDeletedPaths.size).toBe(1);
    vi.advanceTimersByTime(50);
    expect(render).toHaveBeenCalledTimes(1);
    expect(widget.textContent).not.toContain('temperature');
    expect(widget.subscribedPaths.has('default.Area.Device.temperature')).toBe(false);
    expect(widget.valueCache.has('default.Area.Device.temperature')).toBe(false);
    expect(widget.valueCache.has('default.Other.tag')).toBe(true);
    const subscriptions = mockStore.subscribe.mock.calls.length;
    childrenByPath['default.Area'] = ['Device'];
    treeCallback('default.Area.Device', {});
    vi.advanceTimersByTime(50);
    expect(mockStore.subscribe.mock.calls.length).toBeGreaterThan(subscriptions);
    expect(widget.textContent).toContain('temperature');
    treeCallback('default.Area.Device', {});
    widget.remove();
    vi.advanceTimersByTime(50);
    expect(render).toHaveBeenCalledTimes(2);
  });

  it('shows pending deletion, prevents duplicate requests, and restores controls on failure', async () => {
    let rejectDelete: (error: Error) => void = () => {};
    apiMock.deleteTag.mockImplementationOnce(() => new Promise<void>((_resolve, reject) => { rejectDelete = reject; }));
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    widget.deleteTarget = { type: 'tag', path: 'default.Area.tag' };
    widget.isDeleteConfirmOpen = true;
    widget.rerender();
    const deletion = widget.handleConfirmDelete();
    expect(widget.querySelector('#delete-confirm-yes').textContent).toBe('Deleting…');
    expect(widget.querySelector('#delete-confirm-yes').disabled).toBe(true);
    expect(widget.querySelector('#delete-confirm-no').disabled).toBe(true);
    await widget.handleConfirmDelete();
    widget.closeDeleteConfirm();
    expect(apiMock.deleteTag).toHaveBeenCalledTimes(1);
    expect(widget.isDeleteConfirmOpen).toBe(true);
    rejectDelete(new Error('blocked'));
    await deletion;
    expect(widget.querySelector('#delete-confirm-yes').disabled).toBe(false);
    expect(widget.querySelector('#delete-confirm-yes').textContent).toBe('Delete');
  });

  it('pages large branches while keeping search across all tags', async () => {
    vi.useFakeTimers();
    const leafNames = Array.from({ length: 110 }, (_, i) => `tag${String(i).padStart(3, '0')}`);
    const nodeNames = Array.from({ length: 110 }, (_, i) => `node${String(i).padStart(3, '0')}`);
    childrenByPath = {
      default: ['Area'],
      'default.Area': [...leafNames, ...nodeNames],
    };
    nodeTypes = { default: 'node', 'default.Area': 'node' };
    for (const name of leafNames) nodeTypes[`default.Area.${name}`] = 'leaf';
    for (const name of nodeNames) nodeTypes[`default.Area.${name}`] = 'node';
    valuesByPath = {};
    statusByPath = {};
    timestampsByPath = {};

    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!.click();

    expect(widget.querySelectorAll('.tv-leaf-row')).toHaveLength(100);
    expect(widget.querySelectorAll('.tv-node-row')).toHaveLength(101);
    expect(mockStore.subscribe).toHaveBeenCalledTimes(100);
    expect(widget.textContent).toContain('Show more tags (10 remaining)');
    expect(widget.textContent).toContain('Show more nodes (10 remaining)');

    const search = widget.querySelector<HTMLInputElement>('#tv-search')!;
    search.value = 'tag109';
    search.dispatchEvent(new Event('input', { bubbles: true }));
    vi.advanceTimersByTime(200);
    await flushMicrotasks();
    expect(widget.querySelectorAll('.tv-leaf-row')).toHaveLength(1);
    expect(widget.textContent).toContain('tag109');

    const clearSearch = widget.querySelector<HTMLInputElement>('#tv-search')!;
    clearSearch.value = '';
    clearSearch.dispatchEvent(new Event('input', { bubbles: true }));
    vi.advanceTimersByTime(200);
    await flushMicrotasks();
    widget.querySelector<HTMLElement>('.tv-show-more[data-kind="leaf"]')!.click();
    expect(widget.querySelectorAll('.tv-leaf-row')).toHaveLength(110);
    expect(new Set(mockStore.subscribe.mock.calls.map(([path]) => path)).size).toBe(110);

    widget.querySelector<HTMLElement>('.tv-show-more[data-kind="node"]')!.click();
    expect(widget.querySelectorAll('.tv-node-row')).toHaveLength(111);
  }, 10_000);

  it('filters UNDEF status without including NORMAL tags', async () => {
    seedTree();
    valuesByPath['default.Area.Device.meta.serial'] = 'SN-001';
    statusByPath['default.Area.Device.meta.serial'] = 'U';

    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device.meta"]')!.click();

    expect(widget.textContent).toContain('temperature');
    expect(widget.textContent).toContain('serial');

    widget.querySelector<HTMLElement>('[data-status="U"]')!.click();

    expect(widget.textContent).not.toContain('temperature');
    const rows = Array.from(widget.querySelectorAll<HTMLElement>('.tv-leaf-row'));
    expect(rows).toHaveLength(1);
    expect(rows[0].textContent).toContain('serial');
    expect(rows[0].textContent).toContain('UNDEF');
    expect(rows[0].textContent).not.toContain('NORMAL');
  });

  it('marks collapsed branches that contain tags matching the active status filter', async () => {
    seedTree();
    statusByPath['default.Area.Device.meta.serial'] = 'U';

    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.querySelector<HTMLElement>('[data-status="U"]')!.click();

    const areaRow = widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!;
    expect(areaRow.querySelector('.tv-status-match-badge')?.textContent).toContain('1 UNDEF');
    expect(widget.querySelector('[data-node-path="default.Area.Device"]')).toBeNull();

    areaRow.click();
    const deviceRow = widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device"]')!;
    expect(deviceRow.querySelector('.tv-status-match-badge')?.textContent).toContain('1 UNDEF');

    deviceRow.click();
    const metaRow = widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device.meta"]')!;
    expect(metaRow.querySelector('.tv-status-match-badge')?.textContent).toContain('1 UNDEF');
  });

  it('does not mark NORMAL branches as UNDEF when REST status is omitted', async () => {
    seedTree();
    delete statusByPath['default.Area.Device.temperature'];
    delete statusByPath['default.Area.Device.meta.serial'];

    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.querySelector<HTMLElement>('[data-status="U"]')!.click();

    const areaRow = widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!;
    expect(areaRow.querySelector('.tv-status-match-badge')).toBeNull();
  });

  it('removes expanded rows from the active UNDEF filter when live status becomes NORMAL', async () => {
    seedTree();
    statusByPath['default.Area.Device.temperature'] = 'U';
    let temperatureStatusReads = 0;
    mockStore.getNodeStatus.mockImplementation((path: string) => {
      if (path !== 'default.Area.Device.temperature') return statusByPath[path] ?? '';
      temperatureStatusReads += 1;
      return temperatureStatusReads === 1 ? 'U' : 'N';
    });

    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.querySelector<HTMLElement>('[data-status="U"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device"]')!.click();

    expect(widget.textContent).not.toContain('temperature');
    expect(Array.from(widget.querySelectorAll<HTMLElement>('.tv-leaf-row'))).toHaveLength(0);
  });

  it('opens node modals, validates input, browses templates, and creates child nodes', async () => {
    seedTree();
    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area"] [data-action="add-child"]')!.click();
    expect(widget.querySelector('#node-editor-modal')?.textContent).toContain('Add Child Node');

    widget.querySelector<HTMLFormElement>('#node-edit-form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    expect(widget.textContent).toContain('Name is required');

    widget.querySelector<HTMLInputElement>('#node-name')!.value = 'New_Device';
    widget.querySelector<HTMLTextAreaElement>('#node-description')!.value = 'Created from test';
    widget.querySelector<HTMLElement>('#node-template-browse')!.click();
    expect(dialogMock.treeOpen).toHaveBeenCalledWith('Templates', 'Select Template', expect.any(Function));
    dialogMock.treeOpen.mock.calls.at(-1)![2]('default.Templates.Pump');
    expect(widget.querySelector<HTMLInputElement>('#node-template')!.value).toBe('default.Templates.Pump');

    widget.querySelector<HTMLFormElement>('#node-edit-form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flushMicrotasks();
    expect(apiMock.createNode).toHaveBeenCalledWith('default.Area.New_Device', 'Standard');
  });

  it('adds array tags, edits enum tags, and supports pipeline debugging', async () => {
    seedTree();
    const widget = document.createElement('tags-manager-widget');
    const gridItem = document.createElement('div');
    gridItem.className = 'grid-stack-item';
    const card = document.createElement('widget-card');
    const widgetBody = document.createElement('div');
    widgetBody.className = 'widget-body';
    card.appendChild(widgetBody);
    gridItem.appendChild(card);
    document.body.appendChild(gridItem);
    widgetBody.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device"]')!.click();
    widget.querySelector<HTMLElement>('.tv-add-tag-btn[data-parent-path="default.Area.Device"]')!.click();
    await flushMicrotasks();
    let tagModal = widget.querySelector<HTMLElement>('#tag-editor-modal')!;
    expect(tagModal.textContent).toContain('Add Tag');
    expect(tagModal.textContent).toContain('Publish');
    expect(Number(tagModal.style.zIndex)).toBeGreaterThan(100);
    expect(Number(widgetBody.style.zIndex)).toBeGreaterThan(100);
    expect(Number(gridItem.style.zIndex)).toBeGreaterThan(3000);

    widget.querySelector<HTMLInputElement>('#tag-name')!.value = 'samples';
    widget.querySelector<HTMLSelectElement>('#tag-type')!.value = 'float';
    const arrayToggle = widget.querySelector<HTMLInputElement>('#tag-is-array')!;
    arrayToggle.checked = true;
    arrayToggle.dispatchEvent(new Event('change', { bubbles: true }));
    widget.querySelector<HTMLInputElement>('#tag-array-size')!.value = '2';
    widget.querySelector<HTMLFormElement>('#tag-edit-form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flushMicrotasks();

    expect(apiMock.createNode).toHaveBeenCalledWith('default.Area.Device.samples', undefined, true);
    expect(apiMock.createTag).toHaveBeenCalledWith('default.Area.Device.samples.0', 'float', expect.objectContaining({
      deadband: 0,
      pipeline: [{ type: 'publish', parameters: {} }],
    }));
    expect(apiMock.createTag).toHaveBeenCalledWith('default.Area.Device.samples.1', 'float', expect.objectContaining({
      deadband: 0,
      pipeline: [{ type: 'publish', parameters: {} }],
    }));

    widget.querySelector<HTMLElement>('[data-leaf-path="default.Area.Device.temperature"] .tv-edit-tag[data-action="edit-tag"]')!.click();
    await flushMicrotasks();
    tagModal = widget.querySelector<HTMLElement>('#tag-editor-modal')!;
    expect(Number(tagModal.style.zIndex)).toBeGreaterThan(100);
    widget.querySelector<HTMLSelectElement>('#tag-type')!.value = 'enum';
    widget.querySelector<HTMLSelectElement>('#tag-type')!.dispatchEvent(new Event('change', { bubbles: true }));
    expect(widget.textContent).toContain('Enum Values');
    widget.querySelector<HTMLElement>('#enum-value-add')!.click();
    expect(widget.querySelectorAll('.enum-value-row').length).toBeGreaterThan(1);
    widget.querySelector<HTMLInputElement>('#tag-name')!.value = 'mode';
    widget.querySelector<HTMLFormElement>('#tag-edit-form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    await flushMicrotasks();
    expect(apiMock.updateTag).toHaveBeenCalledWith('default.Area.Device.temperature', expect.objectContaining({ name: 'mode' }));

    widget.querySelector<HTMLElement>('[data-leaf-path="default.Area.Device.temperature"] .tv-debug-tag[data-action="debug-tag"]')!.click();
    widget.querySelector<HTMLInputElement>('#debug-input')!.value = '{"value":2}';
    widget.querySelector<HTMLElement>('#debug-run')!.click();
    await flushMicrotasks();
    await flushMicrotasks();
    expect(apiMock.debugTagPipeline).toHaveBeenCalledWith('default.Area.Device.temperature', { value: 2 });
    expect(widget.textContent).toContain('Final output');
  });

  it('loads and displays an inherited template pipeline from the tag API', async () => {
    seedTree();
    const path = 'default.LA_LongBeach.AirQuality.AQ-B-0001.air.aqi';
    configByPath[path] = { type: 1 };
    sharedByPath[path] = {};
    apiMock.loadTag.mockResolvedValueOnce({
      path,
      config: { type: 1, templateName: 'Templates.AirQualityBackup' },
      shared: {
        description: 'Composite air quality index',
        units: 'AQI',
        pipelineInherited: true,
        pipeline: [
          { type: 'scale', parameters: { factor: 2 } },
          { type: 'publish', parameters: {} },
        ],
      },
    });

    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await widget['openEditTagModal'](path);

    expect(apiMock.loadTag).toHaveBeenCalledWith(path);
    expect(widget.querySelector('#tag-editor-modal')?.textContent).toContain('from template');
    expect(widget.querySelectorAll('.pipeline-block')).toHaveLength(2);
    expect(widget.querySelector('#pipeline-override')).not.toBeNull();
    expect(widget.querySelector('#pipeline-add-block')).toBeNull();
  });

  it('edits live values, confirms deletes, and handles failures', async () => {
    seedTree();
    const widget = document.createElement('tags-manager-widget');
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.querySelector<HTMLElement>('[data-node-path="default.Area"]')!.click();
    widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device"]')!.click();
    widget.querySelector<HTMLElement>('.tv-value-cell[data-leaf-path="default.Area.Device.temperature"]')!.click();
    const valueModal = widget.querySelector<HTMLElement>('#value-edit-modal')!;
    expect(valueModal.textContent).toContain('Edit Value');
    expect(Number(valueModal.style.zIndex)).toBeGreaterThan(100);
    widget.querySelector<HTMLInputElement>('#value-edit-input')!.value = '22.75';
    widget.querySelector<HTMLElement>('#value-edit-accept')!.click();
    await flushMicrotasks();
    expect(apiMock.updateTagValue).toHaveBeenCalledWith('default.Area.Device.temperature', 22.75);

    widget.querySelector<HTMLElement>('[data-leaf-path="default.Area.Device.temperature"] .tv-delete-tag[data-action="delete-tag"]')!.click();
    expect(widget.querySelector('#delete-confirm-modal')?.textContent).toContain('Delete tag');
    widget.querySelector<HTMLElement>('#delete-confirm-yes')!.click();
    await flushMicrotasks();
    expect(apiMock.deleteTag).toHaveBeenCalledWith('default.Area.Device.temperature');
    expect(mockStore.removeNode).toHaveBeenCalledWith('default.Area.Device.temperature');

    apiMock.deleteNode.mockRejectedValueOnce(new Error('blocked'));
    widget['deleteTarget'] = { type: 'node', path: 'default.Area.Device' };
    widget['isDeleteConfirmOpen'] = true;
    widget.rerender();
    widget.querySelector<HTMLElement>('#delete-confirm-yes')!.click();
    await flushMicrotasks();
    expect(dialogMock.showAlert).toHaveBeenCalledWith(expect.stringContaining('Failed to delete'), expect.objectContaining({ title: 'Delete failed' }));

  });

  it('updates live cells without replacing the list or interrupting scrolling', async () => {
    seedTree();
    vi.useFakeTimers();
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();
    widget.setTransientState({ expandedNodes: ['default.Area', 'default.Area.Device'], scrollTop: 120 });
    const body = widget.querySelector('#tv-tree-body');
    const row = widget.querySelector('tr[data-leaf-path="default.Area.Device.temperature"]');
    const callback = mockStore.subscribe.mock.calls.find(([path]) => path === 'default.Area.Device.temperature')![1];
    const treeCallback = mockStore.subscribeToTreeChanges.mock.calls.at(-1)![1];
    const oldTimestamp = widget.querySelector('[data-node-path="default.Area.Device"] .tv-node-timestamp').textContent;
    timestampsByPath['default.Area.Device.temperature'] += 60_000;
    statusByPath['default.Area.Device.temperature'] = 'A';
    callback(42);
    treeCallback('default.Area.Device.temperature', { type: 'value', value: 42, status: 'A' });
    body.scrollTop = 240;
    vi.advanceTimersByTime(50);
    await flushMicrotasks();
    expect(widget.querySelector('#tv-tree-body')).toBe(body);
    expect(widget.querySelector('tr[data-leaf-path="default.Area.Device.temperature"]')).toBe(row);
    expect(row.textContent).toContain('42');
    expect(row.textContent).toContain('ALARM');
    expect(body.scrollTop).toBe(240);
    expect(widget.querySelector('[data-node-path="default.Area.Device"] .tv-node-timestamp').textContent).not.toBe(oldTimestamp);

    // A value event can also introduce a new tag and must refresh the structure.
    childrenByPath['default.Area.Device'].push('new_tag');
    nodeTypes['default.Area.Device.new_tag'] = 'leaf';
    treeCallback('default.Area.Device.new_tag', { type: 'value', value: 1 });
    vi.advanceTimersByTime(50);
    expect(widget.textContent).toContain('new_tag');
    expect(widget.querySelector('#tv-tree-body').scrollTop).toBe(240);
  });

  it('keeps filtered lists intact until a tag changes filter membership', async () => {
    seedTree();
    vi.useFakeTimers();
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();
    widget.setTransientState({ expandedNodes: ['default.Area', 'default.Area.Device'], statusFilter: 'A', scrollTop: 80 });
    const body = widget.querySelector('#tv-tree-body');
    const treeCallback = mockStore.subscribeToTreeChanges.mock.calls.at(-1)![1];
    treeCallback('default.Area.Device.enabled', { type: 'value', value: false, status: 'A' });
    vi.advanceTimersByTime(50);
    expect(widget.querySelector('#tv-tree-body')).toBe(body);
    statusByPath['default.Area.Device.temperature'] = 'A';
    treeCallback('default.Area.Device.temperature', { type: 'value', value: 45, status: 'A' });
    vi.advanceTimersByTime(50);
    expect(widget.querySelector('tr[data-leaf-path="default.Area.Device.temperature"]')).not.toBeNull();
    expect(widget.querySelector('#tv-tree-body').scrollTop).toBe(80);
  });

  it('does not overwrite a newer scroll position after a structural render', async () => {
    seedTree();
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();
    widget.setTransientState({ expandedNodes: ['default.Area', 'default.Area.Device'], scrollTop: 120 });
    widget.rerender();
    const body = widget.querySelector('#tv-tree-body');
    expect(body.scrollTop).toBe(120);
    body.scrollTop = 260;
    await flushMicrotasks();
    expect(body.scrollTop).toBe(260);
  });

  it('restores the expanded tree, filters, and scroll position after reconstruction', async () => {
    seedTree();
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    await flushMicrotasks();

    widget.setTransientState({
      expandedNodes: ['default.Area', 'default.Area.Device'],
      searchQuery: 'temp',
      statusFilter: 'A',
      scrollTop: 47,
    });
    await flushMicrotasks();

    const state = widget.getTransientState();
    expect(state.expandedNodes).toEqual(['default.Area', 'default.Area.Device']);
    expect(state.searchQuery).toBe('temp');
    expect(state.statusFilter).toBe('A');
    expect(state.scrollTop).toBe(47);
    expect(widget.querySelector<HTMLInputElement>('#tv-search')?.value).toBe('temp');
  });
});

describe('tags-manager-widget demand loading lifecycle', () => {
  it('releases visible tag callbacks on collapse and disconnect', async () => {
    seedTree();
    const widget = document.createElement('tags-manager-widget') as any;
    document.body.appendChild(widget);
    await flushMicrotasks();
    widget.setTransientState({ expandedNodes: ['default.Area', 'default.Area.Device', 'default.Area.Device.meta'] });
    await flushMicrotasks();
    const releases = mockStore.subscribe.mock.results.map(result => result.value);
    expect(releases.length).toBeGreaterThan(0);
    widget.querySelector<HTMLElement>('[data-node-path="default.Area.Device.meta"]')!.click();
    const metaIndex = mockStore.subscribe.mock.calls.findIndex(([path]) => path.endsWith('.meta.serial'));
    expect(mockStore.subscribe.mock.results[metaIndex].value).toHaveBeenCalled();
    widget.remove();
    for (const release of releases) expect(release).toHaveBeenCalled();
    expect(mockStore.loadTreeFromAPI).not.toHaveBeenCalled();
  });
});
