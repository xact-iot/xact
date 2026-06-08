import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

let childrenByPath: Record<string, string[]> = {};
let nodeTypes: Record<string, 'node' | 'leaf' | 'unknown'> = {};
let configByPath: Record<string, any> = {};
let arrayPaths: Set<string>;
let treeCallback: ((path: string, data: any) => void) | undefined;
const unsubscribeTree = vi.fn();

const mockStore = {
  getOrg: vi.fn(() => 'default'),
  toAbsolute: vi.fn((path: string) => {
    if (!path) return '';
    return path === 'default' || path.startsWith('default.') ? path : `default.${path}`;
  }),
  toRelative: vi.fn((path: string) => {
    if (path === 'default') return '';
    return path.startsWith('default.') ? path.slice('default.'.length) : path;
  }),
  baseTagPath: vi.fn((path: string) => String(path).split(':')[0]),
  nodeExists: vi.fn((path: string) => path === '' || path in childrenByPath || path in nodeTypes),
  listChildrenNames: vi.fn((path: string) => childrenByPath[path] ?? []),
  getNodeType: vi.fn((path: string) => nodeTypes[path] ?? 'unknown'),
  getNodeConfig: vi.fn((path: string) => configByPath[path] ?? {}),
  getIsArray: vi.fn((path: string) => arrayPaths.has(path)),
  subscribeToTreeChanges: vi.fn((_path: string, callback: (path: string, data: any) => void) => {
    treeCallback = callback;
    return unsubscribeTree;
  }),
};

vi.mock('../src/store/store', () => ({
  getMirrorStore: () => mockStore,
}));

import { getTreeBrowserDialog, TreeBrowserDialog } from '../src/components/tree-browser-dialog';

function seedTree(): void {
  childrenByPath = {
    default: ['Area', 'Templates'],
    'default.Area': ['Device', 'ArrayNode'],
    'default.Area.Device': ['enabled', 'temperature', 'meta'],
    'default.Area.Device.meta': ['serial'],
    'default.Area.ArrayNode': ['0', '1'],
    'default.Templates': ['Pump'],
  };
  nodeTypes = {
    default: 'node',
    'default.Area': 'node',
    'default.Area.Device': 'node',
    'default.Area.Device.enabled': 'leaf',
    'default.Area.Device.temperature': 'leaf',
    'default.Area.Device.meta': 'node',
    'default.Area.Device.meta.serial': 'leaf',
    'default.Area.ArrayNode': 'node',
    'default.Area.ArrayNode.0': 'leaf',
    'default.Area.ArrayNode.1': 'leaf',
    'default.Templates': 'node',
    'default.Templates.Pump': 'node',
  };
  configByPath = {
    'default.Area.Device.enabled': { type: 3 },
    'default.Area.Device.temperature': { type: 1 },
    'default.Area.Device.meta.serial': { type: 2 },
    'default.Area.ArrayNode.0': { type: 0 },
    'default.Area.ArrayNode.1': { type: 0 },
  };
  arrayPaths = new Set(['default.Area.ArrayNode']);
  treeCallback = undefined;
  unsubscribeTree.mockClear();
  vi.clearAllMocks();
}

function createDialog(): TreeBrowserDialog {
  const dialog = document.createElement('tree-browser-dialog') as TreeBrowserDialog;
  document.body.appendChild(dialog);
  return dialog;
}

function click(el: Element): void {
  el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
}

describe('tree-browser-dialog', () => {
  beforeEach(() => {
    seedTree();
    Element.prototype.scrollIntoView = vi.fn();
  });

  afterEach(() => {
    document.body.innerHTML = '';
    vi.clearAllMocks();
  });

  it('opens at the current org root, expands ancestors, marks selected leaves, and reacts to tree changes', () => {
    const selected: string[] = [];
    const dialog = createDialog();

    dialog.open('', 'Select Tag', path => selected.push(path), true, 'Area.Device.temperature', 'Area.Device.temperature:N');

    expect(mockStore.toAbsolute).toHaveBeenCalledWith('Area.Device.temperature');
    expect(mockStore.subscribeToTreeChanges).toHaveBeenCalledWith('default', expect.any(Function));
    expect(dialog.textContent).toContain('Select Tag');
    expect(dialog.textContent).toContain('Click a node or tag to select it');
    expect(dialog.textContent).toContain('Area');
    expect(dialog.textContent).toContain('Device');
    expect(dialog.textContent).toContain('temperature');
    expect(dialog.querySelector('[data-path="default.Area.Device.temperature"]')?.getAttribute('data-selected')).toBe('true');
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled();

    childrenByPath['default.Area.Device'].push('pressure');
    nodeTypes['default.Area.Device.pressure'] = 'leaf';
    configByPath['default.Area.Device.pressure'] = { type: 1 };
    treeCallback?.('default.Area.Device.pressure', { type: 'leaf' });

    expect(dialog.textContent).toContain('pressure');
  });

  it('selects nodes immediately in node-only mode and hides leaves', () => {
    const onSelect = vi.fn();
    const dialog = createDialog();

    dialog.open('Area', 'Select Node', onSelect, false, 'Area.Device');

    expect(dialog.textContent).toContain('Device');
    expect(dialog.textContent).not.toContain('temperature');

    const deviceName = dialog.querySelector<HTMLElement>('[data-select="default.Area.Device"]')!;
    click(deviceName);

    expect(onSelect).toHaveBeenCalledWith('Area.Device');
    expect(dialog.innerHTML).toBe('');
    expect(unsubscribeTree).toHaveBeenCalled();
  });

  it('toggles folders from row clicks and shows array badges and numeric array child names', () => {
    const dialog = createDialog();
    dialog.open('Area', 'Select Tag', vi.fn(), true);

    expect(dialog.textContent).toContain('[2]');
    expect(dialog.textContent).not.toContain('[0]');

    click(dialog.querySelector<HTMLElement>('[data-path="default.Area.ArrayNode"]')!);

    expect(dialog.textContent).toContain('[0]');
    expect(dialog.textContent).toContain('[1]');

    click(dialog.querySelector<HTMLElement>('[data-path="default.Area.ArrayNode"]')!);
    expect(dialog.textContent).not.toContain('[0]');
  });

  it('opens the leaf suffix picker, returns relative tag references, and supports back navigation', () => {
    const onSelect = vi.fn();
    const dialog = createDialog();
    dialog.open('Area', 'Select Tag', onSelect, true, 'Area.Device.temperature');

    click(dialog.querySelector<HTMLElement>('[data-path="default.Area.Device.temperature"]')!);

    expect(dialog.textContent).toContain('Choose what to return for this tag');
    expect(dialog.textContent).toContain('Area.Device.temperature');
    expect(dialog.textContent).toContain('Description');
    expect(dialog.textContent).toContain('Alarm');

    click(dialog.querySelector<HTMLElement>('#tbd-back')!);
    expect(dialog.textContent).toContain('Click a node or tag to select it');

    click(dialog.querySelector<HTMLElement>('[data-path="default.Area.Device.temperature"]')!);
    click(dialog.querySelector<HTMLElement>('[data-result="default.Area.Device.temperature:description"]')!);

    expect(onSelect).toHaveBeenCalledWith('Area.Device.temperature:description');
    expect(dialog.innerHTML).toBe('');
  });

  it('returns plain relative paths for the value suffix and status suffixes strip only the org prefix', () => {
    const onSelect = vi.fn();
    const dialog = createDialog();
    dialog.open('Area', 'Select Tag', onSelect, true, 'Area.Device.enabled');

    click(dialog.querySelector<HTMLElement>('[data-path="default.Area.Device.enabled"]')!);
    click(dialog.querySelector<HTMLElement>('[data-result="default.Area.Device.enabled"]')!);
    expect(onSelect).toHaveBeenCalledWith('Area.Device.enabled');

    dialog.open('Area', 'Select Tag', onSelect, true, 'Area.Device.enabled');
    click(dialog.querySelector<HTMLElement>('[data-path="default.Area.Device.enabled"]')!);
    click(dialog.querySelector<HTMLElement>('[data-result="default.Area.Device.enabled:A"]')!);
    expect(onSelect).toHaveBeenLastCalledWith('Area.Device.enabled:A');
  });

  it('renders a missing-root message and closes via cancel, close, backdrop, and disconnect', () => {
    const dialog = createDialog();
    dialog.open('Missing.Root', 'Select Node', vi.fn(), false);

    expect(dialog.textContent).toContain('No "default.Missing.Root" node found');
    click(dialog.querySelector<HTMLElement>('#tbd-cancel')!);
    expect(dialog.innerHTML).toBe('');
    expect(unsubscribeTree).toHaveBeenCalledTimes(1);

    dialog.open('Area', 'Select Node', vi.fn(), false);
    click(dialog.querySelector<HTMLElement>('#tbd-close')!);
    expect(dialog.innerHTML).toBe('');

    dialog.open('Area', 'Select Node', vi.fn(), false);
    click(dialog.querySelector<HTMLElement>('.fixed')!);
    expect(dialog.innerHTML).toBe('');

    dialog.open('Area', 'Select Node', vi.fn(), false);
    dialog.remove();
    expect(unsubscribeTree).toHaveBeenCalled();
  });

  it('reuses the singleton dialog while attached and creates a replacement after removal', () => {
    const first = getTreeBrowserDialog();
    const second = getTreeBrowserDialog();

    expect(first).toBe(second);
    expect(document.body.contains(first)).toBe(true);

    first.remove();
    const third = getTreeBrowserDialog();

    expect(third).not.toBe(first);
    expect(document.body.contains(third)).toBe(true);
  });
});
