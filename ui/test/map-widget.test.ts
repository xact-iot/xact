import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const apiMock = vi.hoisted(() => ({
  createEventLogEntry: vi.fn(async () => ({})),
  getOrganisation: vi.fn(async () => ({})),
  listDashboards: vi.fn(async () => []),
}));

const registryMock = vi.hoisted(() => ({
  ensureWidgetTypeLoaded: vi.fn(async () => {}),
  ensureWidgetTypesLoaded: vi.fn(async () => {}),
  getAvailableWidgets: vi.fn(() => [
    { type: 'status-table-widget', name: 'Status Table', icon: '▦' },
    { type: 'html-widget', name: 'HTML', icon: 'H' },
    { type: 'area-map-widget', name: 'Area Map', icon: 'M' },
    { type: 'test-map-child', name: 'Test Child', icon: 'T' },
    { type: 'test-device-watch-child', name: 'Device Watch Child', icon: 'D' },
  ]),
  registerWidgetType: vi.fn(),
}));

const iconsMock = vi.hoisted(() => ({
  loadIconSet: vi.fn(async () => {}),
  preloadIconSet: vi.fn(),
  getIconSVG: vi.fn((name: string, color = 'currentColor', size = 24) => (
    name.includes(':')
      ? `<svg data-icon="${name}" width="${size}" height="${size}" style="color:${color}"></svg>`
      : ''
  )),
  isIconSetLoaded: vi.fn(() => true),
}));

const treeDialogMock = vi.hoisted(() => ({
  open: vi.fn(),
}));

const mockStore = {
  startKvWatch: vi.fn(),
  toAbsolute: vi.fn((path: string) => path.startsWith('default.') ? path : `default.${path}`),
  toRelative: vi.fn((path: string) => path.startsWith('default.') ? path.slice('default.'.length) : path),
  listChildrenNames: vi.fn(() => []),
  getNodeValue: vi.fn(),
  getNodeShared: vi.fn(() => ({})),
  resolveTagReference: vi.fn(() => undefined),
  baseTagPath: vi.fn((path: string) => String(path).split(':')[0]),
  getNodeType: vi.fn(() => 'leaf'),
  subscribe: vi.fn(() => vi.fn()),
  subscribeTagReference: vi.fn((_path: string, callback: (value: unknown) => void) => {
    callback(undefined);
    return vi.fn();
  }),
  subscribeToTreeChanges: vi.fn(() => vi.fn()),
};

const mockUiStore = {
  get: vi.fn(() => ''),
  set: vi.fn(),
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
  getOrganisation: apiMock.getOrganisation,
  listDashboards: apiMock.listDashboards,
}));

vi.mock('../src/auth', () => ({
  getCurrentUser: () => ({ tenant_id: 'default' }),
}));

vi.mock('../src/dashboards/widgets/widget-registry', () => ({
  ensureWidgetTypeLoaded: registryMock.ensureWidgetTypeLoaded,
  ensureWidgetTypesLoaded: registryMock.ensureWidgetTypesLoaded,
  getAvailableWidgets: registryMock.getAvailableWidgets,
  registerWidgetType: registryMock.registerWidgetType,
}));

vi.mock('../src/utils/icons', () => ({
  ICON_SETS: [{ prefix: 'mdi' }, { prefix: 'lucide' }, { prefix: 'tabler' }],
  getIconSVG: iconsMock.getIconSVG,
  isIconSetLoaded: iconsMock.isIconSetLoaded,
  loadIconSet: iconsMock.loadIconSet,
  preloadIconSet: iconsMock.preloadIconSet,
}));

vi.mock('../src/components/tree-browser-dialog', () => ({
  getTreeBrowserDialog: () => treeDialogMock,
}));

import '../src/dashboards/widgets/map-widget';

const layer = {
  id: 'aq',
  name: 'Air Quality',
  pathPattern: 'LA_LongBeach.AirQuality.*',
  enabled: true,
  itemType: 'icon',
  iconRules: [],
  defaultGlyph: 'pin',
  defaultColor: '#f59e0b',
};

function createMapMock(overrides: Record<string, any> = {}) {
  const panes: Record<string, any> = {
    'xact-device-layer-stale': { remove: vi.fn(), style: {} },
  };
  return {
    panes,
    createPane: vi.fn((name: string) => {
      panes[name] = { style: {}, remove: vi.fn() };
      return panes[name];
    }),
    getPane: vi.fn((name: string) => panes[name] ?? null),
    getPanes: vi.fn(() => panes),
    on: vi.fn(),
    remove: vi.fn(),
    removeLayer: vi.fn(),
    invalidateSize: vi.fn(),
    fitBounds: vi.fn(),
    setView: vi.fn(),
    getZoom: vi.fn(() => 10),
    flyTo: vi.fn(),
    getBounds: vi.fn(() => ({
      getNorth: () => 45,
      getSouth: () => 40,
      getEast: () => -70,
      getWest: () => -75,
    })),
    ...overrides,
  };
}

function defineTestWidgets(): void {
  if (!customElements.get('test-map-child')) {
    customElements.define('test-map-child', class extends HTMLElement {
      config: Record<string, any> = {};
      editMode: boolean | null = null;
      setEditMode(value: boolean) { this.editMode = value; }
      setConfig(config: Record<string, any>) { this.config = config; }
      getConfig() { return this.config; }
      openConfig() {
        this.dispatchEvent(new CustomEvent('widget-config-save', {
          bubbles: true,
          detail: { config: { savedByChild: true } },
        }));
        this.dispatchEvent(new CustomEvent('widget-config-close', { bubbles: true }));
      }
      static getPropertySchema() { return [{ name: 'title', type: 'string' }]; }
    });
  }
  if (!customElements.get('test-device-watch-child')) {
    customElements.define('test-device-watch-child', class extends HTMLElement {
      config: Record<string, any> = {};
      connectedCallback() {
        (window as any).__deviceWatchChildren = ((window as any).__deviceWatchChildren ?? 0) + 1;
      }
      disconnectedCallback() {
        (window as any).__deviceWatchChildren = ((window as any).__deviceWatchChildren ?? 1) - 1;
      }
      setEditMode() {}
      setConfig(config: Record<string, any>) { this.config = config; }
      getConfig() { return this.config; }
      static getPropertySchema() { return []; }
    });
  }
  if (!customElements.get('widget-properties-dialog')) {
    const TestPropertiesDialog = class extends HTMLElement {
      open = vi.fn((_title: string, _schema: any[], current: Record<string, any>) => {
        this.dispatchEvent(new CustomEvent('properties-updated', {
          detail: { config: { ...current, fromDialog: true } },
        }));
      });
    };
    customElements.define('widget-properties-dialog', TestPropertiesDialog as any);
  }
}

function flush(): Promise<void> {
  return Promise.resolve().then(() => Promise.resolve());
}

describe('area-map-widget coordinate loading', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    defineTestWidgets();
    Element.prototype.scrollIntoView = vi.fn();
    globalThis.requestAnimationFrame = vi.fn((cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    }) as any;
    (window as any).L = {
      map: vi.fn(() => createMapMock()),
      tileLayer: vi.fn(() => ({
        addTo: vi.fn().mockReturnThis(),
        setOpacity: vi.fn(),
      })),
      divIcon: vi.fn((options) => ({ options })),
      marker: vi.fn((_position, options) => {
        const el = document.createElement('div');
        el.innerHTML = options?.icon?.options?.html ?? '';
        const marker = {
          options,
          handlers: {} as Record<string, () => void>,
          addTo: vi.fn().mockReturnThis(),
          on: vi.fn((event: string, handler: () => void) => {
            marker.handlers[event] = handler;
            return marker;
          }),
          remove: vi.fn(),
          setLatLng: vi.fn(),
          setIcon: vi.fn((icon) => {
            marker.options.icon = icon;
            el.innerHTML = icon?.options?.html ?? '';
          }),
          getElement: vi.fn(() => el),
        };
        return marker;
      }),
    };
    (window as any).XACT = undefined;
  });

  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = '';
    delete (window as any).L;
    delete (window as any).XACT;
  });

  it('hydrates missing coordinates with per-device subscriptions', async () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.map = { getPane: vi.fn(() => ({ style: {} })), getZoom: vi.fn(() => 10) };
    mockStore.getNodeValue.mockReturnValue(undefined);
    const callbacks: Record<string, () => void> = {};
    const unsubLat = vi.fn();
    const unsubLon = vi.fn();
    mockStore.subscribeTagReference.mockImplementation((path: string, callback: () => void) => {
      callbacks[path] = callback;
      callback();
      return path.endsWith('.meta.lat') ? unsubLat : unsubLon;
    });

    await widget.addDevice(layer, 'default.LA_LongBeach.AirQuality.AQ-B-0149');
    await flush();

    expect((window as any).L.marker).not.toHaveBeenCalled();
    expect(mockStore.subscribeTagReference).toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-B-0149.meta.lat',
      expect.any(Function),
    );
    expect(mockStore.subscribeTagReference).toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-B-0149.meta.lon',
      expect.any(Function),
    );

    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    callbacks['default.LA_LongBeach.AirQuality.AQ-B-0149.meta.lat']?.();
    await flush();

    expect((window as any).L.marker).toHaveBeenCalledWith(
      [33.7701, -118.1937],
      expect.objectContaining({ icon: expect.anything() }),
    );
    expect(unsubLat).toHaveBeenCalled();
    expect(unsubLon).toHaveBeenCalled();
  });

  it('creates markers without subscribing to coordinate tags', async () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.map = { getPane: vi.fn(() => ({ style: {} })), getZoom: vi.fn(() => 10) };
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });

    await widget.addDevice(layer, 'default.LA_LongBeach.AirQuality.AQ-B-0149');

    expect((window as any).L.marker).toHaveBeenCalledWith(
      [33.7701, -118.1937],
      expect.objectContaining({ icon: expect.anything() }),
    );
    expect(mockStore.subscribe).not.toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-B-0149.meta.lon',
      expect.any(Function),
    );
    expect(mockStore.subscribe).not.toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-B-0149.meta.lat',
      expect.any(Function),
    );
  });

  it('resolves wildcard patterns recursively and subscribes icon-rule tags', async () => {
    mockStore.listChildrenNames.mockImplementation((path: string) => {
      if (path === 'default.LA_LongBeach') return ['AirQuality'];
      if (path === 'default.LA_LongBeach.AirQuality') return ['AQ-B-0149', 'AQ-B-0150'];
      return [];
    });
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    let online = false;
    mockStore.resolveTagReference.mockImplementation((path: string) => path.endsWith('.meta.online') ? online : undefined);
    mockStore.getNodeType.mockReturnValue('leaf');
    let ruleCallback: (() => void) | undefined;
    const unsub = vi.fn();
    mockStore.subscribeTagReference.mockImplementation((_path: string, callback: () => void) => {
      ruleCallback = callback;
      return unsub;
    });
    const widget = document.createElement('area-map-widget') as any;
    widget.map = { getPane: vi.fn(() => ({ style: {} })), getZoom: vi.fn(() => 10) };

    expect(widget.resolvePattern('LA_LongBeach.*.*')).toEqual([
      'default.LA_LongBeach.AirQuality.AQ-B-0149',
      'default.LA_LongBeach.AirQuality.AQ-B-0150',
    ]);

    await widget.addDevice({
      ...layer,
      iconRules: [{ tag: 'meta.online', cond: 'eq', value: 'false', glyph: 'OFF', size: 32, color: '#ef4444', animation: 'shake' }],
    }, 'default.LA_LongBeach.AirQuality.AQ-B-0149');

    expect((window as any).L.divIcon).toHaveBeenCalledWith(expect.objectContaining({
      html: expect.stringContaining('OFF'),
      iconSize: [32, 32],
    }));
    expect(mockStore.subscribeTagReference).toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-B-0149.meta.online',
      expect.any(Function),
    );
    ruleCallback?.();
    online = true;
    ruleCallback?.();
    const marker = (window as any).L.marker.mock.results[0].value;
    expect(marker.setIcon).toHaveBeenCalled();

    widget.removeDevice('default.LA_LongBeach.AirQuality.AQ-B-0149');
    expect(unsub).toHaveBeenCalled();
    expect(marker.remove).toHaveBeenCalled();
  });

  it('uses plugin renderers and assigns map panes when plugin objects omit one', async () => {
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    const pluginObj = {
      options: {},
      addTo: vi.fn().mockReturnThis(),
      on: vi.fn().mockReturnThis(),
      remove: vi.fn(),
    };
    const renderer = vi.fn(() => pluginObj);
    (window as any).XACT = { getMapItemType: vi.fn(() => renderer) };
    const widget = document.createElement('area-map-widget') as any;
    widget.map = { getPane: vi.fn(() => ({ style: {} })), getZoom: vi.fn(() => 10) };

    await widget.addDevice({ ...layer, itemType: 'plugin', pluginType: 'custom-marker' }, 'default.LA_LongBeach.AirQuality.AQ-B-0149');

    expect(renderer).toHaveBeenCalledWith('default.LA_LongBeach.AirQuality.AQ-B-0149', mockStore, (window as any).L);
    expect(pluginObj.options.pane).toBe('xact-device-layer-aq');
    expect(pluginObj.addTo).toHaveBeenCalledWith(widget.map);
    expect(pluginObj.on).toHaveBeenCalledWith('click', expect.any(Function));
  });

  it('renders legend controls and search results that fly to selected devices', async () => {
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    const widget = document.createElement('area-map-widget') as any;
    widget.map = { getPane: vi.fn(() => ({ style: {} })), getZoom: vi.fn(() => 10), flyTo: vi.fn() };
    widget.config = { ...widget.config, showLegend: true, showSearch: true, layers: [layer] };
    widget.render();
    widget.attachEventListeners();
    await widget.addDevice(layer, 'default.LA_LongBeach.AirQuality.AQ-B-0149');
    widget.updateLegend();

    const marker = (window as any).L.marker.mock.results[0].value;
    const layerToggle = widget.querySelector<HTMLInputElement>('input[data-layer-id="aq"]')!;
    layerToggle.checked = false;
    layerToggle.dispatchEvent(new Event('change', { bubbles: true }));
    expect(marker.remove).toHaveBeenCalled();
    layerToggle.checked = true;
    layerToggle.dispatchEvent(new Event('change', { bubbles: true }));
    expect(marker.addTo).toHaveBeenCalledWith(widget.map);

    const input = widget.querySelector<HTMLInputElement>('#search-input')!;
    input.value = '0149';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    expect(widget.querySelector('#search-dropdown')?.textContent).toContain('AQ-B-0149');
    widget.querySelector<HTMLElement>('.search-result')!.click();
    expect(widget.map.flyTo).toHaveBeenCalledWith([33.7701, -118.1937], 16);
    expect(input.value).toBe('');
  });

  it('opens the device side panel, updates UI context, emits dashboard opens, and closes cleanly', async () => {
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      if (path.endsWith('.kpi.online')) return true;
      return undefined;
    });
    mockStore.getNodeShared.mockImplementation((path: string) => path.endsWith('AQ-B-0149') ? { description: 'Beach sensor' } : {});
    mockStore.listChildrenNames.mockImplementation((path: string) => {
      if (path.endsWith('AQ-B-0149')) return ['meta', 'kpi'];
      if (path.endsWith('.kpi')) return ['online'];
      return [];
    });
    const widget = document.createElement('area-map-widget') as any;
    const opened = vi.fn();
    widget.addEventListener('dashboard-open', opened);
    widget.map = { getPane: vi.fn(() => ({ style: {} })), getZoom: vi.fn(() => 10) };
    widget.config = { ...widget.config, layers: [layer] };
    widget.render();
    await widget.addDevice({ ...layer, detailDashboardId: 'detail-dashboard' }, 'default.LA_LongBeach.AirQuality.AQ-B-0149');

    widget.onDeviceClick('default.LA_LongBeach.AirQuality.AQ-B-0149');
    const panel = widget.querySelector<HTMLElement>('#device-panel')!;
    expect(panel.style.display).toBe('flex');
    expect(panel.textContent).toContain('Beach sensor');
    expect(panel.textContent).toContain('online');
    expect(mockUiStore.set).toHaveBeenCalledWith('deviceName', 'AQ-B-0149');
    expect(mockUiStore.set).toHaveBeenCalledWith('deviceType', 'AirQuality');

    panel.querySelector<HTMLElement>('#dp-device-link')!.click();
    expect(opened).toHaveBeenCalledWith(expect.objectContaining({
      detail: { dashboard: 'detail-dashboard', id: 'detail-dashboard', devicePath: 'default.LA_LongBeach.AirQuality.AQ-B-0149' },
    }));

    panel.querySelector<HTMLElement>('#dp-close')!.click();
    expect(panel.style.display).toBe('none');
  });

  it('clears the previous side-panel widget before updating device context on another click', async () => {
    const sidePanelChildrenAtDeviceNameSet: number[] = [];
    mockUiStore.set.mockImplementation((key: string) => {
      if (key === 'deviceName') {
        sidePanelChildrenAtDeviceNameSet.push(widget.querySelector<HTMLElement>('#dp-body')?.children.length ?? 0);
      }
    });

    const widget = document.createElement('area-map-widget') as any;
    const sideLayer = {
      ...layer,
      sidePanelWidgetType: 'test-device-watch-child',
      sidePanelWidgetConfig: { tagPrefix: '*' },
    };
    const markerEntry = (layerConfig: any) => ({
      marker: { getElement: vi.fn(() => document.createElement('div')) },
      layer: layerConfig,
      unsubs: [],
      divTagPaths: new Set(),
    });
    widget.map = { getPane: vi.fn(() => ({ style: {} })), getZoom: vi.fn(() => 10) };
    widget.config = { ...widget.config, layers: [sideLayer] };
    widget.render();
    widget.devices.set('default.LA_LongBeach.AirQuality.AQ-S-0150', markerEntry(sideLayer));
    widget.renderClickPanel('default.LA_LongBeach.AirQuality.AQ-S-0150');
    widget.querySelector<HTMLElement>('#dp-body')!.appendChild(document.createElement('test-device-watch-child'));
    expect(widget.querySelector('test-device-watch-child')).toBeTruthy();

    widget.onDeviceClick('default.LA_LongBeach.AirQuality.AQ-S-0150');
    await flush();

    expect(sidePanelChildrenAtDeviceNameSet).toEqual([0]);
  });

  it('saves current bounds through setConfig, emits persistence, and returns cloned config', () => {
    const widget = document.createElement('area-map-widget') as any;
    const saved = vi.fn();
    widget.addEventListener('widget-config-save', saved);
    widget.map = createMapMock({
      getBounds: vi.fn(() => ({
        getNorth: () => 10,
        getSouth: () => 1,
        getEast: () => 20,
        getWest: () => 2,
      })),
    });

    widget.setConfig({ heading: 'Site Map', layers: [{ ...layer }], _saveBoundsNow: true });
    const config = widget.getConfig();
    config.layers[0].name = 'Mutated';

    expect(saved).toHaveBeenCalledWith(expect.objectContaining({
      detail: expect.objectContaining({
        forceDirty: true,
        config: expect.objectContaining({
          heading: 'Site Map',
          savedBounds: { north: 10, south: 1, east: 20, west: 2 },
        }),
      }),
    }));
    expect(widget.getConfig().layers[0].name).toBe('Air Quality');
  });

  it('reads organisation bounds from the store, subscribes until bounds arrive, and fits saved/default views', () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock();
    widget.render();

    mockStore.getNodeValue.mockReturnValueOnce(0).mockReturnValueOnce(0).mockReturnValueOnce(0).mockReturnValueOnce(0);
    expect(widget.readOrgAreaFromStore('default')).toBe(false);

    mockStore.getNodeValue
      .mockReturnValueOnce(55)
      .mockReturnValueOnce(50)
      .mockReturnValueOnce(-1)
      .mockReturnValueOnce(-5);
    expect(widget.readOrgAreaFromStore('default')).toBe(true);
    widget.fitOrgBounds();
    expect(widget.map.fitBounds).toHaveBeenCalledWith([[50, -5], [55, -1]], { padding: [20, 20] });
    expect(widget.querySelector('#map-zoom')?.textContent).toBe('Z 10');

    widget.config.savedBounds = { north: 45, south: 40, east: 11, west: 5 };
    widget.fitOrgBounds();
    expect(widget.map.fitBounds).toHaveBeenLastCalledWith([[40, 5], [45, 11]]);

    widget.config.savedBounds = undefined;
    widget.orgArea = null;
    widget.fitOrgBounds();
    expect(widget.map.setView).toHaveBeenCalledWith([54.5, -2.0], 6);

    const unsub = vi.fn();
    let callback: (() => void) | undefined;
    mockStore.subscribeToTreeChanges.mockImplementationOnce((_path: string, cb: () => void) => {
      callback = cb;
      return unsub;
    });
    mockStore.getNodeValue.mockReturnValue(undefined);
    widget.syncOrgAreaFromStore('default');
    expect(mockStore.subscribeToTreeChanges).toHaveBeenCalledWith('default.meta', expect.any(Function));

    mockStore.getNodeValue
      .mockReset()
      .mockReturnValueOnce(60)
      .mockReturnValueOnce(59)
      .mockReturnValueOnce(2)
      .mockReturnValueOnce(1);
    callback?.();
    expect(unsub).toHaveBeenCalled();
    expect(widget.map.fitBounds).toHaveBeenLastCalledWith([[59, 1], [60, 2]], { padding: [20, 20] });
  });

  it('initializes and replaces TomTom traffic and incident tile layers', () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock();
    widget.trafficLayer = { old: 'traffic' };
    widget.incidentLayer = { old: 'incident' };
    widget.config = {
      ...widget.config,
      tomtomApiKey: 'a key/with spaces',
      showTraffic: true,
      showIncidents: true,
      trafficStyle: 'absolute',
      incidentStyle: 's3',
    };

    widget.initTomTomLayers();

    expect(widget.map.removeLayer).toHaveBeenCalledWith({ old: 'traffic' });
    expect(widget.map.removeLayer).toHaveBeenCalledWith({ old: 'incident' });
    expect((window as any).L.tileLayer).toHaveBeenCalledWith(
      expect.stringContaining('flow/absolute'),
      expect.objectContaining({ opacity: 0.7, pane: 'xact-tomtom-pane' }),
    );
    expect((window as any).L.tileLayer).toHaveBeenCalledWith(
      expect.stringContaining('incidents/s3'),
      expect.objectContaining({ opacity: 0.8, pane: 'xact-tomtom-pane' }),
    );
    expect((window as any).L.tileLayer.mock.calls[0][0]).toContain('key=a%20key%2Fwith%20spaces');
  });

  it('creates active layer panes, orders z-indexes, and removes stale panes', () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock();
    widget.config = {
      ...widget.config,
      layers: [
        { ...layer, id: 'top' },
        { ...layer, id: 'lower' },
      ],
    };

    widget.ensureDeviceLayerPanes();

    expect(widget.map.panes['xact-device-layer-stale'].remove).toHaveBeenCalled();
    expect(widget.map.createPane).toHaveBeenCalledWith('xact-device-layer-top');
    expect(widget.map.createPane).toHaveBeenCalledWith('xact-device-layer-lower');
    expect(widget.map.panes['xact-device-layer-top'].style.zIndex).toBe('900');
    expect(widget.map.panes['xact-device-layer-lower'].style.zIndex).toBe('890');
  });

  it('raises a hovered marker layer above other device layers while showing hover widgets', async () => {
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock();
    const topLayer = { ...layer, id: 'top' };
    const lowerLayer = {
      ...layer,
      id: 'lower',
      zoomWidgetType: 'test-map-child',
      zoomWidgetConfig: { tagPrefix: '*' },
      zoomThreshold: 13,
    };
    widget.config = {
      ...widget.config,
      layers: [topLayer, lowerLayer],
    };
    widget.ensureDeviceLayerPanes();

    await widget.addDevice(lowerLayer, 'default.LA_LongBeach.AirQuality.AQ-B-0149');
    const marker = widget.devices.get('default.LA_LongBeach.AirQuality.AQ-B-0149').marker;

    marker.handlers.mouseover();

    expect(widget.map.panes['xact-device-layer-lower'].style.zIndex).toBe('950');
    expect(widget.map.panes['xact-device-layer-top'].style.zIndex).toBe('900');
    expect(widget.devices.get('default.LA_LongBeach.AirQuality.AQ-B-0149').hoverWidgetEl).toBeInstanceOf(HTMLElement);

    marker.handlers.mouseout();

    expect(widget.map.panes['xact-device-layer-top'].style.zIndex).toBe('900');
    expect(widget.map.panes['xact-device-layer-lower'].style.zIndex).toBe('890');
  });

  it('refreshes layers by clearing existing devices/plugins/subscriptions and skipping disabled layers', async () => {
    const widget = document.createElement('area-map-widget') as any;
    const deviceUnsub = vi.fn();
    const layerUnsub = vi.fn();
    const marker = { remove: vi.fn() };
    const plugin = { remove: vi.fn() };
    widget.map = createMapMock();
    widget.devices.set('default.Device.A', { marker, layer, unsubs: [deviceUnsub], divTagPaths: new Set() });
    widget.mapLayerPlugins.set('plugin', { instance: plugin, devicePaths: new Set(['default.Device.A']) });
    widget.layerUnsubs.set('aq', [layerUnsub]);
    widget.config = {
      ...widget.config,
      layers: [
        { ...layer, id: 'enabled' },
        { ...layer, id: 'disabled', enabled: false },
      ],
    };
    widget.loadConfiguredIconSets = vi.fn(async () => {});
    widget.loadConfiguredWidgetTypes = vi.fn(async () => {});
    widget.ensureDeviceLayerPanes = vi.fn();
    widget.initLayer = vi.fn(async () => {});
    widget.updateLegend = vi.fn();

    await widget.refreshLayers();

    expect(deviceUnsub).toHaveBeenCalled();
    expect(marker.remove).toHaveBeenCalled();
    expect(plugin.remove).toHaveBeenCalled();
    expect(layerUnsub).toHaveBeenCalled();
    expect(widget.initLayer).toHaveBeenCalledWith(expect.objectContaining({ id: 'enabled' }));
    expect(widget.initLayer).not.toHaveBeenCalledWith(expect.objectContaining({ id: 'disabled' }));
    expect(widget.updateLegend).toHaveBeenCalled();
  });

  it('initializes a layer, responds to tree changes, and updates/removes devices', async () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock();
    mockStore.listChildrenNames.mockImplementation((path: string) => (
      path === 'default.LA_LongBeach.AirQuality' ? ['AQ-B-0149'] : []
    ));
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    let treeChange: ((path: string, data: any) => void) | undefined;
    mockStore.subscribeToTreeChanges.mockImplementation((_path: string, cb: (path: string, data: any) => void) => {
      treeChange = cb;
      return vi.fn();
    });

    await widget.initLayer(layer);

    expect(mockStore.subscribeToTreeChanges).toHaveBeenCalledWith('default.LA_LongBeach.AirQuality', expect.any(Function));
    expect(widget.devices.has('default.LA_LongBeach.AirQuality.AQ-B-0149')).toBe(true);

    const existingMarker = widget.devices.get('default.LA_LongBeach.AirQuality.AQ-B-0149').marker;
    treeChange?.('default.LA_LongBeach.AirQuality.AQ-B-0149.meta.lat', { value: 34 });
    expect(existingMarker.setLatLng).toHaveBeenCalledWith([33.7701, -118.1937]);

    treeChange?.('default.LA_LongBeach.AirQuality.AQ-B-0150', { type: 'node' });
    expect(widget.devices.has('default.LA_LongBeach.AirQuality.AQ-B-0150')).toBe(true);

    treeChange?.('default.LA_LongBeach.AirQuality.AQ-B-0150', null);
    expect(widget.devices.has('default.LA_LongBeach.AirQuality.AQ-B-0150')).toBe(false);
  });

  it('mounts map-layer plugins with helpers, requestSave, bounds, and device updates', () => {
    const widget = document.createElement('area-map-widget') as any;
    const saved = vi.fn();
    widget.addEventListener('widget-config-save', saved);
    widget.map = createMapMock();
    widget.orgArea = { north: 3, south: 1, east: 4, west: 2 };
    const instance = {
      updateDevices: vi.fn(),
      remove: vi.fn(),
      setConfig: vi.fn(),
    };
    let api: any;
    const plugin = {
      name: 'Heatmap',
      defaultConfig: { radius: 5 },
      create: vi.fn((receivedApi) => {
        api = receivedApi;
        return instance;
      }),
    };
    (window as any).XACT = { getMapLayerType: vi.fn(() => plugin) };
    const pluginLayer = { ...layer, id: 'heat', itemType: 'plugin', pluginType: 'heatmap', pluginConfig: { radius: 9 } };

    widget.mountMapLayerPlugin(pluginLayer, ['default.Area.Device.A']);

    expect(plugin.create).toHaveBeenCalledWith(expect.objectContaining({
      map: widget.map,
      store: mockStore,
      layer: pluginLayer,
      pane: 'xact-device-layer-heat',
      config: { radius: 9 },
    }));
    expect(instance.updateDevices).toHaveBeenCalledWith(['default.Area.Device.A']);
    expect(api.getBounds()).toEqual({ north: 3, south: 1, east: 4, west: 2 });
    expect(api.resolveDeviceTag('default.Area.Device.A', 'meta.lat')).toBe('default.Area.Device.A.meta.lat');
    expect(api.resolveDeviceTag('default.Area.Device.A', 'default.Other.tag')).toBe('default.Other.tag');

    api.requestSave({ opacity: 0.5 });
    expect(pluginLayer.pluginConfig).toEqual({ radius: 9, opacity: 0.5 });
    expect(saved).toHaveBeenCalledWith(expect.objectContaining({ detail: expect.objectContaining({ forceDirty: true }) }));

    widget.updateMapLayerPluginDevices(pluginLayer, 'default.Area.Device.B', true);
    expect(instance.updateDevices).toHaveBeenLastCalledWith(['default.Area.Device.A', 'default.Area.Device.B']);
    widget.updateMapLayerPluginDevices(pluginLayer, 'default.Area.Device.A', false);
    expect(instance.updateDevices).toHaveBeenLastCalledWith(['default.Area.Device.B']);
  });

  it('logs map-layer plugin creation errors without storing plugin state', () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock();
    const err = new Error('plugin failed');
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    (window as any).XACT = {
      getMapLayerType: vi.fn(() => ({ create: vi.fn(() => { throw err; }) })),
    };

    widget.mountMapLayerPlugin({ ...layer, id: 'bad', pluginType: 'bad-plugin' }, ['default.Area.Device.A']);

    expect(console.error).toHaveBeenCalledWith('Map layer plugin error:', 'bad-plugin', err);
    expect(widget.mapLayerPlugins.has('bad')).toBe(false);
  });

  it('mounts zoom widgets into markers and hover cards with device-specific config', async () => {
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock({ getZoom: vi.fn(() => 15) });
    const zoomLayer = {
      ...layer,
      zoomWidgetType: 'test-map-child',
      zoomWidgetConfig: { tagPrefix: '*', label: 'Zoom' },
      zoomThreshold: 13,
      divWidgetWidth: 320,
    };

    await widget.addDevice(zoomLayer, 'default.LA_LongBeach.AirQuality.AQ-B-0149');
    const entry = widget.devices.get('default.LA_LongBeach.AirQuality.AQ-B-0149');

    expect(entry.divWidgetEl).toBeInstanceOf(HTMLElement);
    expect((entry.divWidgetEl as any).config).toEqual({
      tagPrefix: 'AQ-B-0149',
      label: 'Zoom',
    });
    expect((entry.divWidgetEl as any).editMode).toBe(false);

    widget.map.getZoom.mockReturnValue(10);
    widget.updateDeviceMarker('default.LA_LongBeach.AirQuality.AQ-B-0149');
    widget.mountHoverWidget('default.LA_LongBeach.AirQuality.AQ-B-0149');

    expect(entry.hoverWidgetEl).toBeInstanceOf(HTMLElement);
    expect((entry.hoverWidgetEl as any).config.tagPrefix).toBe('AQ-B-0149');
  });

  it('evaluates div templates, subscribes template tags, and renders fallback content on template errors', async () => {
    let message = 'Ready';
    let templateCallback: (() => void) | undefined;
    mockStore.getNodeValue.mockImplementation((path: string) => {
      if (path.endsWith('.meta.lat')) return 33.7701;
      if (path.endsWith('.meta.lon')) return -118.1937;
      return undefined;
    });
    mockStore.getNodeShared.mockImplementation((path: string) => path.endsWith('AQ-B-0149') ? { description: 'Sensor one' } : {});
    mockStore.resolveTagReference.mockImplementation((path: string) => path.endsWith('.sign.message') ? message : undefined);
    mockStore.subscribeTagReference.mockImplementation((_path: string, cb: () => void) => {
      templateCallback = cb;
      return vi.fn();
    });
    const widget = document.createElement('area-map-widget') as any;
    widget.map = createMapMock({ getZoom: vi.fn(() => 14) });
    const templateLayer = {
      ...layer,
      divTemplate: '${deviceName} ${deviceDescription} ${tag("sign.message")}',
      zoomThreshold: 13,
    };

    expect(widget.renderTemplateContent({ ...templateLayer, divTemplate: '${deviceName} ${deviceDescription} ${tag(sign.message)}' }, 'default.LA_LongBeach.AirQuality.AQ-B-0149')).toContain('AQ-B-0149 Sensor one Ready');
    await widget.addDevice(templateLayer, 'default.LA_LongBeach.AirQuality.AQ-B-0149');
    expect(mockStore.subscribeTagReference).toHaveBeenCalledWith(
      'default.LA_LongBeach.AirQuality.AQ-B-0149.sign.message',
      expect.any(Function),
    );

    templateCallback?.();
    message = 'Running';
    templateCallback?.();
    const marker = (window as any).L.marker.mock.results.at(-1)!.value;
    expect(marker.setIcon).toHaveBeenCalled();

    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const fallback = widget.renderTemplateContent({ ...templateLayer, divTemplate: '${(() => { throw new Error("bad") })()}' }, 'default.LA_LongBeach.AirQuality.AQ-B-0149');
    expect(fallback).toContain('AQ-B-0149');
    expect(console.error).toHaveBeenCalledWith(
      '[map-widget] Div template error for "default.LA_LongBeach.AirQuality.AQ-B-0149":',
      expect.any(Error),
      '\nTemplate:',
      '${(() => { throw new Error("bad") })()}',
    );
  });

  it('saves zoom and side-panel widget config updates back to layers and mounted widgets', async () => {
    const widget = document.createElement('area-map-widget') as any;
    const saved = vi.fn();
    widget.addEventListener('widget-config-save', saved);
    const zoomLayer = {
      ...layer,
      zoomWidgetType: 'test-map-child',
      zoomWidgetConfig: { tagPrefix: '*', old: true },
      sidePanelWidgetType: 'test-map-child',
      sidePanelWidgetConfig: { tagPrefix: '*', side: true },
    };
    const divWidgetEl = document.createElement('test-map-child') as any;
    const hoverWidgetEl = document.createElement('test-map-child') as any;
    widget.devices.set('default.Area.Device.A', {
      marker: { getElement: vi.fn(() => document.createElement('div')) },
      layer: zoomLayer,
      unsubs: [],
      divTagPaths: new Set(),
      divWidgetEl,
      hoverWidgetEl,
    });

    await widget.openZoomWidgetConfig(zoomLayer);

    expect(zoomLayer.zoomWidgetConfig).toEqual({ savedByChild: true });
    expect(divWidgetEl.config).toEqual({ savedByChild: true });
    expect(hoverWidgetEl.config).toEqual({ savedByChild: true });
    expect(saved).toHaveBeenCalledWith(expect.objectContaining({ detail: expect.objectContaining({ forceDirty: true }) }));

    await widget.openSidePanelWidgetConfig(zoomLayer);

    expect(zoomLayer.sidePanelWidgetConfig).toEqual({ savedByChild: true });
    expect(saved).toHaveBeenCalledTimes(2);
  });

  it('configures map-layer plugins through a properties dialog and updates live plugin instances', () => {
    const widget = document.createElement('area-map-widget') as any;
    const saved = vi.fn();
    widget.addEventListener('widget-config-save', saved);
    const pluginLayer = { ...layer, id: 'heat', pluginType: 'heatmap', pluginConfig: { radius: 4 } };
    const instance = { setConfig: vi.fn(), updateDevices: vi.fn() };
    widget.mapLayerPlugins.set('heat', { instance, devicePaths: new Set(['default.Area.Device.A']) });
    (window as any).XACT = {
      getMapLayerType: vi.fn(() => ({
        defaultConfig: { opacity: 1 },
        getPropertySchema: () => [{ name: 'radius', type: 'number' }],
      })),
    };

    widget.openMapLayerPluginConfig(pluginLayer);
    const dialog = document.body.querySelector('widget-properties-dialog')!;
    dialog.dispatchEvent(new CustomEvent('properties-updated', {
      detail: { config: { radius: 8 } },
    }));

    expect(pluginLayer.pluginConfig).toEqual({ opacity: 1, radius: 8 });
    expect(instance.setConfig).toHaveBeenCalledWith({ opacity: 1, radius: 8 });
    expect(instance.updateDevices).toHaveBeenCalledWith(['default.Area.Device.A']);
    expect(saved).toHaveBeenCalledWith(expect.objectContaining({ detail: expect.objectContaining({ forceDirty: true }) }));
    expect(document.body.querySelector('widget-properties-dialog')).toBeNull();
  });

  it('collects icon layer editor values, browse callbacks, rules, widget selectors, and dashboard links', () => {
    mockStore.listChildrenNames.mockImplementation((path: string) => (
      path === 'default.LA_LongBeach.AirQuality' ? ['AQ-B-0149'] : []
    ));
    const widget = document.createElement('area-map-widget') as any;
    const editableLayer = {
      ...layer,
      iconRules: [{ tag: 'meta.online', cond: 'eq', value: 'true', glyph: 'mdi:check', size: 20, color: '#22c55e', animation: 'none' }],
      defaultGlyph: 'mdi:map-marker',
      defaultColor: '#f59e0b',
    };
    widget.config = { ...widget.config, layers: [editableLayer] };
    widget.cfgEditLayerId = 'aq';
    widget.dashboards = [
      { id: 'dash-1', name: 'Device Detail', isCategory: false },
      { id: 'dash-dup', name: 'Device Detail', isCategory: false },
      { id: 'cat-1', name: 'Device Detail', isCategory: true },
    ];
    widget.dashboardsLoaded = true;

    const overlay = document.createElement('div');
    overlay.innerHTML = widget.renderConfigPanelHtml();
    document.body.appendChild(overlay);
    widget.attachConfigListeners(overlay);
    const dashboardOptionLabels = [...overlay.querySelectorAll<HTMLOptionElement>('#le-detail-dashboard option')].map(o => o.textContent);
    expect(dashboardOptionLabels.filter(label => label === 'Device Detail')).toHaveLength(1);

    overlay.querySelector<HTMLInputElement>('#le-name')!.value = 'Updated Layer';
    overlay.querySelector<HTMLInputElement>('#le-pattern')!.value = 'LA_LongBeach.AirQuality.*';
    (overlay.querySelector('#le-default-glyph') as any).value = 'lucide:map-pin';
    overlay.querySelector<HTMLInputElement>('#le-default-size')!.value = '36';
    overlay.querySelector<HTMLInputElement>('#le-default-color')!.value = '#123456';
    overlay.querySelector<HTMLInputElement>('#le-offset-x')!.value = '4';
    overlay.querySelector<HTMLInputElement>('#le-offset-y')!.value = '8';
    overlay.querySelector<HTMLInputElement>('#le-zoom-threshold')!.value = '12';
    overlay.querySelector<HTMLInputElement>('#le-refresh-interval')!.value = '250';
    overlay.querySelector<HTMLInputElement>('#le-dw-width')!.value = '360';
    overlay.querySelector<HTMLSelectElement>('#le-zoom-widget-type')!.value = 'test-map-child';
    overlay.querySelector<HTMLSelectElement>('#le-side-panel-widget-type')!.value = 'html-widget';
    overlay.querySelector<HTMLSelectElement>('#le-detail-dashboard')!.value = 'dash-1';

    overlay.querySelector<HTMLInputElement>('.rule-tag')!.value = 'meta.mode';
    overlay.querySelector<HTMLSelectElement>('.rule-cond')!.value = 'ne';
    overlay.querySelector<HTMLInputElement>('.rule-value')!.value = 'manual';
    (overlay.querySelector('.rule-glyph') as any).value = 'tabler:alert';
    overlay.querySelector<HTMLInputElement>('.rule-size')!.value = '28';
    overlay.querySelector<HTMLInputElement>('.rule-color')!.value = '#abcdef';
    overlay.querySelector<HTMLSelectElement>('.rule-anim')!.value = 'pulse';

    overlay.querySelector<HTMLElement>('#le-pattern-browse')!.click();
    expect(treeDialogMock.open).toHaveBeenCalledWith('', 'Select Path Pattern', expect.any(Function), false, 'LA_LongBeach.AirQuality');
    treeDialogMock.open.mock.calls.at(-1)![2]('Selected.Parent');
    expect(overlay.querySelector<HTMLInputElement>('#le-pattern')!.value).toBe('Selected.Parent.*');

    overlay.querySelector<HTMLElement>('.rule-tag-browse')!.click();
    expect(treeDialogMock.open).toHaveBeenLastCalledWith('default.LA_LongBeach.AirQuality.AQ-B-0149', 'Select Tag', expect.any(Function), true);
    treeDialogMock.open.mock.calls.at(-1)![2]('LA_LongBeach.AirQuality.AQ-B-0149.meta.status');
    expect(overlay.querySelector<HTMLInputElement>('.rule-tag')!.value).toBe('meta.status');

    widget.collectLayerFromPanel(overlay);

    expect(editableLayer).toMatchObject({
      name: 'Updated Layer',
      pathPattern: 'Selected.Parent.*',
      itemType: 'icon',
      defaultGlyph: 'lucide:map-pin',
      defaultColor: '#123456',
      defaultSize: 36,
      offsetX: 4,
      offsetY: 8,
      zoomThreshold: 12,
      refreshInterval: 250,
      divWidgetWidth: 360,
      zoomWidgetType: 'test-map-child',
      sidePanelWidgetType: 'html-widget',
      detailDashboardId: 'dash-1',
    });
    expect(editableLayer.iconRules).toEqual([{
      tag: 'meta.status',
      cond: 'ne',
      value: 'manual',
      glyph: 'tabler:alert',
      size: 28,
      color: '#abcdef',
      animation: 'pulse',
    }]);
  });

  it('deduplicates dashboard link options by name while preserving the selected duplicate id', () => {
    const widget = document.createElement('area-map-widget') as any;
    widget.config = {
      ...widget.config,
      layers: [{ ...layer, detailDashboardId: 'dash-2' }],
    };
    widget.cfgEditLayerId = 'aq';
    widget.dashboards = [
      { id: 'dash-1', name: 'Device Detail', isCategory: false },
      { id: 'dash-2', name: 'Device Detail', isCategory: false },
      { id: 'dash-3', name: 'Operations', isCategory: false },
    ];
    widget.dashboardsLoaded = true;

    const overlay = document.createElement('div');
    overlay.innerHTML = widget.renderConfigPanelHtml();
    const options = [...overlay.querySelectorAll<HTMLOptionElement>('#le-detail-dashboard option')];

    expect(options.filter(option => option.textContent === 'Device Detail')).toHaveLength(1);
    expect(options.find(option => option.textContent === 'Device Detail')?.value).toBe('dash-2');
    expect(overlay.querySelector<HTMLSelectElement>('#le-detail-dashboard')?.value).toBe('dash-2');
  });

  it('drives config panel add, edit, move, delete, save, bounds, opacity, and back controls', async () => {
    vi.useFakeTimers();
    apiMock.listDashboards.mockResolvedValue([{ id: 'dash-1', name: 'Dashboard 1', isCategory: false }]);
    const widget = document.createElement('area-map-widget') as any;
    const saved = vi.fn();
    widget.addEventListener('widget-config-save', saved);
    widget.map = createMapMock();
    widget.baseLayer = { setOpacity: vi.fn() };
    widget.config = {
      ...widget.config,
      heading: 'Old',
      baseOpacity: 1,
      layers: [
        { ...layer, id: 'first', name: 'First' },
        { ...layer, id: 'second', name: 'Second' },
      ],
    };
    widget.initTomTomLayers = vi.fn();
    widget.refreshLayers = vi.fn(async () => {});
    widget.updateLegend = vi.fn();
    widget.updateCardTitle = vi.fn();

    await widget.openConfigPanel();
    let overlay = document.querySelector<HTMLElement>('#map-cfg-overlay')!;
    expect(Number(overlay.style.zIndex)).toBeGreaterThan(3001);

    overlay.querySelector<HTMLInputElement>('#cfg-base-opacity')!.value = '0.45';
    overlay.querySelector<HTMLInputElement>('#cfg-base-opacity')!.dispatchEvent(new Event('input', { bubbles: true }));
    expect(widget.baseLayer.setOpacity).toHaveBeenCalledWith(0.45);
    expect(overlay.querySelector('#cfg-base-opacity-val')?.textContent).toBe('45%');

    overlay.querySelector<HTMLElement>('#cfg-save-bounds')!.click();
    expect(widget.config.savedBounds).toEqual({ north: 45, south: 40, east: -70, west: -75 });
    expect(overlay.querySelector('#cfg-bounds-label')?.textContent).toContain('N:45.0000');
    vi.advanceTimersByTime(2000);
    expect(overlay.querySelector('#cfg-save-bounds')?.textContent).toBe('Save current view');

    overlay.querySelector<HTMLElement>('#cfg-tomtom-toggle')!.click();
    overlay = document.querySelector<HTMLElement>('#map-cfg-overlay')!;
    expect(overlay.querySelector<HTMLElement>('#cfg-tomtom-body')!.style.display).toBe('grid');

    overlay.querySelector<HTMLElement>('#cfg-add-layer')!.click();
    overlay = document.querySelector<HTMLElement>('#map-cfg-overlay')!;
    expect(widget.config.layers).toHaveLength(3);
    expect(overlay.querySelector('#cfg-layer-editor')?.textContent).toContain('New Layer');

    overlay.querySelector<HTMLElement>('#cfg-close-layer')!.click();
    overlay = document.querySelector<HTMLElement>('#map-cfg-overlay')!;
    expect(overlay.querySelector('#cfg-layer-editor')).toBeNull();

    overlay.querySelector<HTMLElement>('.cfg-edit-layer[data-id="second"]')!.click();
    overlay = document.querySelector<HTMLElement>('#map-cfg-overlay')!;
    expect(overlay.querySelector('#cfg-layer-editor')?.textContent).toContain('Second');

    overlay.querySelector<HTMLElement>('.cfg-move-layer[data-id="second"][data-dir="-1"]')!.click();
    overlay = document.querySelector<HTMLElement>('#map-cfg-overlay')!;
    expect(widget.config.layers[0].id).toBe('second');

    overlay.querySelector<HTMLElement>('.cfg-del-layer[data-id="first"]')!.click();
    overlay = document.querySelector<HTMLElement>('#map-cfg-overlay')!;
    expect(widget.config.layers.some((l: any) => l.id === 'first')).toBe(false);

    overlay.querySelector<HTMLInputElement>('#cfg-heading')!.value = 'Updated';
    overlay.querySelector<HTMLInputElement>('#cfg-tomtom-key')!.value = 'tomtom-key';
    overlay.querySelector<HTMLInputElement>('#cfg-show-traffic')!.checked = true;
    overlay.querySelector<HTMLSelectElement>('#cfg-traffic-style')!.value = 'absolute';
    overlay.querySelector<HTMLInputElement>('#cfg-show-incidents')!.checked = true;
    overlay.querySelector<HTMLSelectElement>('#cfg-incident-style')!.value = 's3';
    overlay.querySelector<HTMLElement>('#cfg-save')!.click();
    await flush();

    expect(widget.config).toMatchObject({
      heading: 'Updated',
      baseOpacity: 0.45,
      tomtomApiKey: 'tomtom-key',
      showTraffic: true,
      trafficStyle: 'absolute',
      showIncidents: true,
      incidentStyle: 's3',
    });
    expect(widget.initTomTomLayers).toHaveBeenCalled();
    expect(widget.refreshLayers).toHaveBeenCalled();
    expect(saved).toHaveBeenCalledWith(expect.objectContaining({ detail: expect.objectContaining({ forceDirty: true }) }));
    expect(document.querySelector('#map-cfg-overlay')).toBeNull();
    vi.useRealTimers();

    await widget.openConfigPanel();
    document.querySelector<HTMLElement>('#cfg-back')!.click();
    expect(document.querySelector('#map-cfg-overlay')).toBeNull();
    expect(widget.cfgOpen).toBe(false);
  });

  it('collects plugin layer editor values and resets plugin config when plugin type changes', () => {
    (window as any).XACT = {
      listMapLayerTypes: vi.fn(() => ['heatmap', 'traffic']),
      getMapLayerType: vi.fn((name: string) => ({
        name,
        defaultConfig: { radius: 5 },
        getPropertySchema: () => [{ name: 'radius', type: 'number' }],
      })),
    };
    const widget = document.createElement('area-map-widget') as any;
    const pluginLayer = {
      ...layer,
      itemType: 'plugin',
      pluginType: 'heatmap',
      pluginConfig: { old: true },
    };
    widget.config = { ...widget.config, layers: [pluginLayer] };
    widget.cfgEditLayerId = 'aq';

    const overlay = document.createElement('div');
    overlay.innerHTML = widget.renderConfigPanelHtml();
    document.body.appendChild(overlay);
    widget.attachConfigListeners(overlay);

    expect(overlay.querySelector('#le-plugin-configure')).not.toBeNull();
    overlay.querySelector<HTMLInputElement>('#le-name')!.value = 'Plugin Layer';
    overlay.querySelector<HTMLInputElement>('#le-pattern')!.value = 'Plugin.Devices.*';
    overlay.querySelector<HTMLSelectElement>('#le-plugin-type')!.value = 'traffic';

    widget.collectLayerFromPanel(overlay);

    expect(pluginLayer).toMatchObject({
      name: 'Plugin Layer',
      pathPattern: 'Plugin.Devices.*',
      itemType: 'plugin',
      pluginType: 'traffic',
      pluginConfig: {},
    });
  });
});
