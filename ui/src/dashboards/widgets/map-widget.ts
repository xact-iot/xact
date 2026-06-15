/**
 * area-map-widget - Interactive map widget for the XACT dashboard system.
 *
 * Shows organisation-scoped device layers on an OpenStreetMap base map with
 * optional TomTom traffic overlays.  Devices are driven by live RTDB tag
 * values; markers can be icon-based or template-rendered div elements.
 */

import { BaseComponent } from '../../components/base-component';
import { ensureWidgetTypeLoaded, ensureWidgetTypesLoaded, getAvailableWidgets, getWidgetMeta, registerWidgetType } from './widget-registry';
import { getMirrorStore } from '../../store/store';
import { getOrganisation, listDashboards } from '../../api';
import type { DashboardMeta, OrgArea } from '../../api';
import { getCurrentUser } from '../../auth';
import type { HtmlEditor } from '../../components/html-editor';
import '../../components/html-editor'; // register <html-editor> custom element
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import { ICON_SETS, getIconSVG, isIconSetLoaded, loadIconSet } from '../../utils/icons';
import '../../components/icon-picker';
import { getUiStore } from '../../store/ui-store';
import './status-table-widget'; // register <status-table-widget> custom element
import './widget-properties-dialog';
import type { WidgetPropertiesDialog } from './widget-properties-dialog';

// ── Widget registration ────────────────────────────────────────────────────────

registerWidgetType({
  type: 'area-map-widget',
  name: 'Area Map',
  icon: '🗺️',
  category: 'General',
  defaultW: 12,
  defaultH: 20,
  minW: 8,
  minH: 12,
});

// ── Leaflet singleton ──────────────────────────────────────────────────────────

let leafletReady: Promise<void> | null = null;

function loadLeaflet(): Promise<void> {
  if (leafletReady) return leafletReady;
  leafletReady = new Promise<void>((resolve, reject) => {
    if ((window as any).L) { resolve(); return; }

    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.css';
    document.head.appendChild(link);

    const script = document.createElement('script');
    script.src = 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.js';
    script.onload = () => resolve();
    script.onerror = () => { leafletReady = null; reject(new Error('Failed to load Leaflet')); };
    document.head.appendChild(script);
  });
  return leafletReady;
}

// ── Config types ───────────────────────────────────────────────────────────────

type RuleCond = 'eq' | 'ne' | 'gt' | 'lt' | 'gte' | 'lte';
type Animation = 'none' | 'pulse' | 'shake';

interface IconRule {
  tag: string;
  cond: RuleCond;
  value: string;
  glyph: string;
  color: string;
  animation: Animation;
  size?: number;
}

interface LayerConfig {
  id: string;
  name: string;
  pathPattern: string;
  enabled: boolean;
  itemType: 'icon' | 'plugin';
  pluginType?: string;
  pluginConfig?: any;
  iconRules?: IconRule[];
  defaultGlyph?: string;
  defaultColor?: string;
  defaultSize?: number;
  /** Template rendered as a div marker when zoom >= zoomThreshold; shown as hover tooltip when zoomed out. */
  divTemplate?: string;
  /** @deprecated migrated to divTemplate */ divTemplateIn?: string;
  zoomThreshold?: number;
  refreshInterval?: number;
  offsetX?: number;
  offsetY?: number;
  /** Widget type shown in the device click side panel. */
  sidePanelWidgetType?: string;
  /** Config object for sidePanelWidgetType shown in the device click side panel. */
  sidePanelWidgetConfig?: any;
  /** Dashboard opened when the device name in the click side panel is clicked. */
  detailDashboardId?: string;
  /** Config object for status-table-widget shown as the zoomed-in marker (all devices in layer). */
  divWidgetConfig?: any;
  /** Widget type shown as the zoomed-in marker. Legacy divWidgetConfig implies status-table-widget. */
  zoomWidgetType?: string;
  /** Config object for zoomWidgetType shown as the zoomed-in marker. */
  zoomWidgetConfig?: any;
  /** Width (px) of the div widget card. Default 280. */
  divWidgetWidth?: number;
  /** Which zoomed-in mode the layer editor radio is set to. Persisted independently of divWidgetConfig/divTemplate content. */
  zoomedMode?: 'div-template' | 'status-table' | 'widget';
}

interface SavedBounds {
  north: number; south: number; east: number; west: number;
}

interface MapWidgetConfig {
  tomtomApiKey: string;
  trafficStyle: string;
  incidentStyle: string;
  showTraffic: boolean;
  showIncidents: boolean;
  heading: string;
  /** @deprecated Blank heading now hides the card header in view mode. */
  showHeading?: boolean;
  showSearch: boolean;
  showLegend: boolean;
  baseOpacity: number;
  savedBounds: SavedBounds | null;
  layers: LayerConfig[];
}

// ── Device tracking ────────────────────────────────────────────────────────────

interface DeviceEntry {
  marker: any;          // Leaflet marker
  layer: LayerConfig;
  unsubs: Array<() => void>;
  interval?: ReturnType<typeof setInterval>;
  /** Tag paths accessed by the div template - subscribed for reactive re-render. */
  divTagPaths: Set<string>;
  /** Last rendered icon HTML - used to skip unnecessary setIcon calls that would destroy embedded widgets. */
  lastIconHtml?: string;
  /** Mounted zoomed-in widget; null when zoomed out. */
  divWidgetEl?: HTMLElement | null;
  /** Mounted zoomed-in widget in the hover tip; only present when zoomed out. */
  hoverWidgetEl?: HTMLElement | null;
}

interface MapLayerPluginState {
  instance: {
    updateDevices(devicePaths: string[]): void;
    setConfig?(config: Record<string, any>): void;
    remove(): void;
  };
  devicePaths: Set<string>;
}

// ── CSS animations (injected once) ─────────────────────────────────────────────

let animStyleInjected = false;
function ensureAnimStyles(): void {
  if (animStyleInjected) return;
  animStyleInjected = true;
  const style = document.createElement('style');
  style.textContent = `
    @keyframes xact-map-pulse {
      0%   { transform: scale(1); opacity: 1; }
      50%  { transform: scale(1.3); opacity: 0.7; }
      100% { transform: scale(1); opacity: 1; }
    }
    @keyframes xact-map-shake {
      0%, 100% { transform: translateX(0); }
      20%  { transform: translateX(-4px); }
      40%  { transform: translateX(4px); }
      60%  { transform: translateX(-3px); }
      80%  { transform: translateX(3px); }
    }
    .xact-map-icon-wrap { cursor: pointer; transform-origin: bottom center; }
    .xact-map-anim-pulse { animation: xact-map-pulse 1.5s ease-in-out infinite; }
    .xact-map-anim-shake { animation: xact-map-shake 0.8s ease-in-out infinite; }
    .leaflet-marker-icon { overflow: visible !important; }
    .xact-map-marker-root { position: relative; overflow: visible; }
    .xact-map-hover-tip { display: none; position: absolute; bottom: 100%; left: 50%; transform: translateX(-50%); margin-bottom: 8px; pointer-events: none; z-index: 9999; }
    .xact-map-marker-root:hover .xact-map-hover-tip { display: block; }
    .xact-map-marker-selected .xact-map-icon-wrap,
    .xact-map-marker-selected .xact-map-dw-card {
      outline: 5px solid var(--accent-color, #f59e0b);
      outline-offset: 5px;
      box-shadow: 0 0 0 3px rgba(0,0,0,0.9), 0 0 28px color-mix(in srgb, var(--accent-color, #f59e0b) 80%, transparent) !important;
    }
    .xact-map-marker-selected .xact-map-icon-wrap {
      border-radius: 999px;
    }
  `;
  document.head.appendChild(style);
}

// ── Shared UI style constants ─────────────────────────────────────────────────

const fieldStyle = `padding:4px 6px;font-size:13px;background:var(--content-bg,#1a1a1a);border:1px solid var(--border-color);border-radius:3px;color:var(--content-text,#f0f0f0);`;
const labelStyle = `display:block;font-size:11px;opacity:0.5;margin-bottom:3px;text-transform:uppercase;letter-spacing:0.04em;`;
/** Top-level section heading - accent coloured, clearly distinct from field labels */
const sectionHeadStyle = `display:block;font-size:13px;font-weight:600;letter-spacing:0.01em;color:var(--accent-color);margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid color-mix(in srgb,var(--accent-color) 18%,var(--border-color));`;
/** Sub-section heading used inside the layer editor card */
const subHeadStyle = `font-size:10px;font-weight:700;letter-spacing:0.09em;text-transform:uppercase;color:var(--accent-color);opacity:0.75;`;

const DEFAULT_DIV_TEMPLATE = `<div style="background:#1a1a1a;border:1px solid #333;border-radius:6px;padding:8px 12px;font-size:12px;font-family:monospace;min-width:160px;">
  <div style="font-weight:600;color:#f59e0b;margin-bottom:6px;">\${deviceName}</div>
  <div style="display:flex;justify-content:space-between;gap:16px;">
    <span style="opacity:0.6;">Temp</span>
    <span style="color:\${tag('temperature') > 100 ? '#ef4444' : '#22c55e'}">\${tag('temperature')} °C</span>
  </div>
  <div style="display:flex;justify-content:space-between;gap:16px;">
    <span style="opacity:0.6;">Online</span>
    <span>\${tag('online') ? '✓' : '✗'}</span>
  </div>
</div>`;

const TOMTOM_FLOW_STYLES = ['relative0', 'relative0-dark', 'absolute', 'relative', 'relative-delay', 'reduced-sensitivity'];
const TOMTOM_INCIDENT_STYLES = ['s0', 's0-dark', 's1', 's2', 's3', 'night'];
const KNOWN_ICON_PREFIXES = new Set(ICON_SETS.map(set => set.prefix));
const MAP_CONFIG_OVERLAY_Z_INDEX = 19000;
const DEVICE_LAYER_TOP_Z_INDEX = 900;
const DEVICE_LAYER_STEP_Z_INDEX = 10;
const DEVICE_LAYER_HOVER_Z_INDEX = 950;

// ── Helpers ────────────────────────────────────────────────────────────────────

function esc(s: string): string {
  return (s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function genId(): string {
  return Math.random().toString(36).slice(2, 9);
}

function cloneConfig<T>(value: T): T {
  if (typeof structuredClone === 'function') return structuredClone(value);
  return JSON.parse(JSON.stringify(value));
}

function iconSetPrefix(glyph: string): string {
  if (!glyph || !glyph.includes(':')) return '';
  const prefix = glyph.slice(0, glyph.indexOf(':'));
  return KNOWN_ICON_PREFIXES.has(prefix) ? prefix : '';
}

/** Normalise any CSS colour string to a 7-char #rrggbb for <input type="color">. */
function toHexColor(c: string): string {
  if (!c) return '#f59e0b';
  const s = c.trim();
  if (/^#[0-9a-fA-F]{6}$/.test(s)) return s;
  if (/^#[0-9a-fA-F]{3}$/.test(s))
    return '#' + s[1]+s[1]+s[2]+s[2]+s[3]+s[3];
  return '#f59e0b';
}

function fuzzyScore(text: string, query: string): number {
  if (!query) return 0;
  text = text.toLowerCase();
  query = query.toLowerCase();
  let score = 0;
  let ti = 0;
  let streak = 0;
  for (let qi = 0; qi < query.length; qi++) {
    let found = false;
    while (ti < text.length) {
      if (text[ti] === query[qi]) {
        streak++;
        score += streak;
        ti++;
        found = true;
        break;
      }
      streak = 0;
      ti++;
    }
    if (!found) return -1;
  }
  return score;
}

function evaluateCond(actual: any, cond: RuleCond, expected: string): boolean {
  const num = parseFloat(expected);
  const actualNum = typeof actual === 'number' ? actual : parseFloat(String(actual));
  switch (cond) {
    case 'eq': return String(actual) === expected || actual === true && expected === 'true' || actual === false && expected === 'false';
    case 'ne': return String(actual) !== expected;
    case 'gt': return !isNaN(actualNum) && !isNaN(num) && actualNum > num;
    case 'lt': return !isNaN(actualNum) && !isNaN(num) && actualNum < num;
    case 'gte': return !isNaN(actualNum) && !isNaN(num) && actualNum >= num;
    case 'lte': return !isNaN(actualNum) && !isNaN(num) && actualNum <= num;
  }
}

function formatTagValue(val: any): string {
  if (val === null || val === undefined) return '-';
  if (typeof val === 'boolean') return val ? '✓' : '✗';
  if (typeof val === 'number') {
    return Number.isInteger(val) ? String(val) : val.toFixed(2);
  }
  return String(val);
}

// ── Widget ─────────────────────────────────────────────────────────────────────

export class AreaMapWidget extends BaseComponent {
  private static dashboardCache: DashboardMeta[] = [];
  private static dashboardLoadPromise: Promise<DashboardMeta[]> | null = null;

  // ── Property schema (used by dashboard-container to show header gear icon) ──────

  getPropertySchema() {
    const boundsDesc = this.config.savedBounds
      ? `Saved: N ${this.config.savedBounds.north.toFixed(4)}  S ${this.config.savedBounds.south.toFixed(4)}  E ${this.config.savedBounds.east.toFixed(4)}  W ${this.config.savedBounds.west.toFixed(4)}`
      : 'No bounds saved - map will open at the organisation\'s area';
    return [
      {
        name: 'heading',
        type: 'string',
        label: 'Heading',
        description: 'Widget heading shown in the card title bar. Leave blank to hide the title bar in view mode.',
        default: '',
      },
      {
        name: 'showSearch',
        type: 'boolean',
        label: 'Show search',
        description: 'Display the device search input on the map',
        default: true,
      },
      {
        name: 'showLegend',
        type: 'boolean',
        label: 'Show legend',
        description: 'Display the layer legend on the map',
        default: true,
      },
      {
        name: '_saveBoundsNow',
        type: 'boolean',
        label: 'Save current view as initial bounds',
        description: boundsDesc,
        default: false,
      },
    ];
  }
  private config: MapWidgetConfig = {
    tomtomApiKey: '',
    trafficStyle: 'relative0',
    incidentStyle: 'night',
    showTraffic: false,
    showIncidents: false,
    heading: '',
    showSearch: true,
    showLegend: true,
    baseOpacity: 1,
    savedBounds: null,
    layers: [],
  };

  // Map handles
  private map: any = null;
  private baseLayer: any = null;
  private trafficLayer: any = null;
  private incidentLayer: any = null;

  // Device tracking
  private devices = new Map<string, DeviceEntry>();
  private pendingDeviceUnsubs = new Map<string, Array<() => void>>();
  private layerUnsubs = new Map<string, Array<() => void>>();
  private mapLayerPlugins = new Map<string, MapLayerPluginState>();

  // Org area + live subscription for meta-tag updates
  private orgArea: OrgArea | null = null;
  private orgMetaUnsub: (() => void) | undefined;

  // Observers that call invalidateSize() when the widget is resized or dragged
  private _resizeObserver: ResizeObserver | null = null;
  private _mutationObserver: MutationObserver | null = null;


  // Config panel state
  private cfgOpen = false;
  private cfgEditLayerId: string | null = null;
  private cfgTomTomCollapsed = true;

  // Search
  private searchQuery = '';
  private selectedDevicePath: string | null = null;
  private hoverRaisedLayerId: string | null = null;

  // Legend collapsed state
  private legendCollapsed = false;

  // Edit mode (forwarded from dashboard-container)
  private _editMode = false;

  // Dashboard list used by the layer editor's detail-dashboard selector.
  private dashboards: DashboardMeta[] = [];

  // ── Lifecycle ──────────────────────────────────────────────────────────────

  connectedCallback(): void {
    super.connectedCallback();
    this.loadConfiguredIconSets();
    this.loadDashboards();
    ensureAnimStyles();
    this.updateCardTitle();
    this.loadAndInit();
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this.destroyMap();
  }

  setConfig(c: Partial<MapWidgetConfig> & { _saveBoundsNow?: boolean }): void {
    if (c._saveBoundsNow && this.map) {
      const b = this.map.getBounds();
      this.config.savedBounds = {
        north: b.getNorth(), south: b.getSouth(),
        east: b.getEast(),   west: b.getWest(),
      };
    }
    const { _saveBoundsNow: _, ...rest } = c;
    this.config = cloneConfig({ ...this.config, ...rest });
    if (c.layers !== undefined) {
      this.config.layers = cloneConfig(c.layers);
    }
    this.loadConfiguredIconSets();
    if (this.map && c.layers !== undefined) {
      void this.refreshLayers();
    }
    if (c._saveBoundsNow && this.map) {
      // Persist the captured bounds back to dashboard-container so they are saved
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
    }
    this.updateCardTitle();
  }

  getConfig(): MapWidgetConfig {
    return cloneConfig(this.config);
  }

  setEditMode(editing: boolean): void {
    this._editMode = editing;
    const btn = this.querySelector<HTMLElement>('#map-layers-btn');
    if (btn) btn.style.display = editing ? 'flex' : 'none';
    const sidePanelGear = this.querySelector<HTMLElement>('#dp-gear');
    if (sidePanelGear) sidePanelGear.style.display = editing ? 'inline-block' : 'none';
    this.querySelectorAll<HTMLElement>('.xact-map-dw-gear').forEach(g => {
      g.style.display = editing ? 'inline-block' : 'none';
    });
    this.updateCardTitle();
  }

  rerender(): void {
    this.destroyMap();
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    this.loadAndInit();
    // Restore layers button visibility after DOM is rebuilt
    const btn = this.querySelector<HTMLElement>('#map-layers-btn');
    if (btn) btn.style.display = this._editMode ? 'flex' : 'none';
    this.updateCardTitle();
  }

  private async loadAndInit(): Promise<void> {
    // Start the KV watcher immediately so device lat/lon values stream in while
    // Leaflet is loading.  Without this, values arrive after refreshLayers() runs
    // and markers are either missing or placed at (0,0).
    const orgName = getCurrentUser()?.tenant_id;
    if (orgName) getMirrorStore().startKvWatch(orgName);

    try {
      if (orgName) {
        // Fetch the current org directly so we always get the right one.
        const org = await getOrganisation(orgName);
        // Use the DB area as an immediate fallback while the RTDB values load.
        if (org.area) this.orgArea = org.area;
        // Subscribe to live meta-tag updates from the RTDB.
        this.syncOrgAreaFromStore(orgName);
      }
    } catch { /* ignore - we'll use default view */ }
    await this.initMap();
  }

  private async loadDashboards(): Promise<void> {
    try {
      this.dashboards = await AreaMapWidget.fetchDashboards();
    } catch (err) {
      console.error('MapWidget: failed to load dashboards:', err);
      this.dashboards = [];
    }
  }

  private static fetchDashboards(): Promise<DashboardMeta[]> {
    if (AreaMapWidget.dashboardCache.length > 0) {
      return Promise.resolve(AreaMapWidget.dashboardCache);
    }
    if (!AreaMapWidget.dashboardLoadPromise) {
      AreaMapWidget.dashboardLoadPromise = listDashboards()
        .then(dashboards => {
          AreaMapWidget.dashboardCache = dashboards;
          return dashboards;
        })
        .finally(() => {
          AreaMapWidget.dashboardLoadPromise = null;
        });
    }
    return AreaMapWidget.dashboardLoadPromise;
  }

  private collectIconSetPrefixes(): string[] {
    const prefixes = new Set<string>(['mdi']);
    for (const layer of this.config.layers) {
      const defaultPrefix = iconSetPrefix(layer.defaultGlyph ?? '');
      if (defaultPrefix) prefixes.add(defaultPrefix);
      for (const rule of layer.iconRules ?? []) {
        const rulePrefix = iconSetPrefix(rule.glyph ?? '');
        if (rulePrefix) prefixes.add(rulePrefix);
      }
    }
    return [...prefixes];
  }

  private async loadConfiguredIconSets(): Promise<void> {
    const prefixes = this.collectIconSetPrefixes();
    await Promise.all(prefixes.map(prefix => loadIconSet(prefix)));
    if (!this.isConnected) return;
    for (const [devicePath] of this.devices) {
      this.updateDeviceMarker(devicePath);
    }
  }

  // Read north/south/east/west from the MirrorStore for the given org.
  // If the values are present, updates orgArea and returns true.
  private readOrgAreaFromStore(orgName: string): boolean {
    const store = getMirrorStore();
    const n = store.getNodeValue(`${orgName}.meta.north`);
    const s = store.getNodeValue(`${orgName}.meta.south`);
    const e = store.getNodeValue(`${orgName}.meta.east`);
    const w = store.getNodeValue(`${orgName}.meta.west`);
    // Reject if any value is missing or if all are zero (unset RTDB defaults).
    if (typeof n !== 'number' || typeof s !== 'number' ||
        typeof e !== 'number' || typeof w !== 'number') return false;
    if (n === 0 && s === 0 && e === 0 && w === 0) return false;
    this.orgArea = { north: n, south: s, east: e, west: w };
    return true;
  }

  // Attempt an immediate read; if values aren't ready yet, subscribe to
  // orgName.meta and apply the bounds as soon as they arrive.
  private syncOrgAreaFromStore(orgName: string): void {
    if (this.readOrgAreaFromStore(orgName)) return;

    this.orgMetaUnsub = getMirrorStore().subscribeToTreeChanges(
      `${orgName}.meta`,
      () => {
        if (!this.readOrgAreaFromStore(orgName)) return;
        // Values arrived - cancel subscription and fit the map if ready.
        this.orgMetaUnsub?.();
        this.orgMetaUnsub = undefined;
        if (this.map) this.fitOrgBounds();
      }
    );
  }

  /** Fit the map to the org bounding box (or a broad fallback view). */
  private fitOrgBounds(): void {
    if (!this.map) return;
    this.map.invalidateSize(false);
    if (this.config.savedBounds) {
      const { north, south, east, west } = this.config.savedBounds;
      this.map.fitBounds([[south, west], [north, east]]);
    } else if (this.orgArea) {
      const { north, south, east, west } = this.orgArea;
      this.map.fitBounds([[south, west], [north, east]], { padding: [20, 20] });
    } else {
      this.map.setView([54.5, -2.0], 6);
    }
    const zoomEl = this.querySelector<HTMLElement>('#map-zoom');
    if (zoomEl) zoomEl.textContent = `Z ${this.map.getZoom()}`;
  }

  // ── Map lifecycle ──────────────────────────────────────────────────────────

  private destroyMap(): void {
    // Disconnect layout observers
    this._resizeObserver?.disconnect();
    this._resizeObserver = null;
    this._mutationObserver?.disconnect();
    this._mutationObserver = null;

    // Cancel pending org area subscription
    this.orgMetaUnsub?.();
    this.orgMetaUnsub = undefined;

    // Clear all device subscriptions
    for (const unsubs of this.pendingDeviceUnsubs.values()) {
      unsubs.forEach(fn => fn());
    }
    this.pendingDeviceUnsubs.clear();
    for (const entry of this.devices.values()) {
      entry.unsubs.forEach(fn => fn());
      if (entry.interval) clearInterval(entry.interval);
    }
    this.devices.clear();
    for (const state of this.mapLayerPlugins.values()) {
      state.instance.remove();
    }
    this.mapLayerPlugins.clear();
    this.selectedDevicePath = null;

    for (const unsubs of this.layerUnsubs.values()) {
      unsubs.forEach(fn => fn());
    }
    this.layerUnsubs.clear();

    if (this.map) {
      this.map.remove();
      this.map = null;
      this.trafficLayer = null;
      this.incidentLayer = null;
    }
  }

  private async initMap(): Promise<void> {
    await loadLeaflet();
    const L = (window as any).L;
    if (!L) return;

    const container = this.querySelector<HTMLElement>('#xact-map');
    if (!container) return;

    // Prevent map events from reaching GridStack drag handler
    ['mousedown', 'pointerdown', 'touchstart'].forEach(evt => {
      container.addEventListener(evt, (e: Event) => e.stopPropagation(), { passive: false });
    });

    this.map = L.map(container, { zoomControl: true, attributionControl: false });
    this.map.createPane('xact-tomtom-pane');
    this.map.getPane('xact-tomtom-pane')!.style.zIndex = '250';

    // OSM base layer
    this.baseLayer = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '© <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
      maxZoom: 19,
      opacity: this.config.baseOpacity ?? 1,
    }).addTo(this.map);

    // TomTom layers
    this.initTomTomLayers();

    // Device layers
    await this.refreshLayers();

    // Zoom change → update div icons and zoom indicator
    this.map.on('zoomend', () => this.onZoomChange());

    // Wait one paint frame so GridStack has applied the widget's CSS dimensions.
    // Without this, the container may still be 0×0 and fitBounds forces maxZoom (19).
    await new Promise<void>(resolve => requestAnimationFrame(() => resolve()));
    if (!this.map) return; // widget may have been destroyed during the frame wait

    // Poll on every animation frame until the container dimensions have stabilised
    // (same non-zero size for two consecutive frames).  This handles both the initial
    // layout and the "navigate away then back" case where GridStack's CSS transition
    // animation means the container height grows gradually - calling fitBounds while
    // the height is still near-zero causes Leaflet to clamp to maxZoom (19).
    let prevW = -1, prevH = -1;
    const tryFit = () => {
      if (!this.map || !this.isConnected) return; // destroyed or removed from DOM
      const w = container.offsetWidth, h = container.offsetHeight;
      if (w > 0 && h > 0 && w === prevW && h === prevH) {
        this.fitOrgBounds(); // dimensions stable - safe to fit
        return;
      }
      prevW = w; prevH = h;
      requestAnimationFrame(tryFit);
    };
    tryFit();

    // Invalidate map size when the widget container is resized (covers GridStack resize/drag).
    this._resizeObserver = new ResizeObserver(() => {
      if (this.map) this.map.invalidateSize(false);
    });
    this._resizeObserver.observe(container);

    // Invalidate map size when the .grid-stack-item ancestor style changes
    // (covers GridStack drag - which modifies top/left/transform on that element)
    const gsItem = this.closest('.grid-stack-item');
    if (gsItem) {
      this._mutationObserver = new MutationObserver(() => {
        if (this.map) this.map.invalidateSize(false);
      });
      this._mutationObserver.observe(gsItem, { attributes: true, attributeFilter: ['style'] });
    }
  }

  // ── TomTom layers ──────────────────────────────────────────────────────────

  private initTomTomLayers(): void {
    const L = (window as any).L;
    if (!L || !this.map) return;

    // Remove existing
    if (this.trafficLayer) { this.map.removeLayer(this.trafficLayer); this.trafficLayer = null; }
    if (this.incidentLayer) { this.map.removeLayer(this.incidentLayer); this.incidentLayer = null; }

    const key = this.config.tomtomApiKey;
    if (!key) return;

    if (this.config.showTraffic) {
      this.trafficLayer = L.tileLayer(
        `https://api.tomtom.com/traffic/map/4/tile/flow/${this.config.trafficStyle || 'relative0'}/{z}/{x}/{y}.png?key=${encodeURIComponent(key)}`,
        { opacity: 0.7, maxZoom: 22, pane: 'xact-tomtom-pane' }
      ).addTo(this.map);
    }

    if (this.config.showIncidents) {
      this.incidentLayer = L.tileLayer(
        `https://api.tomtom.com/traffic/map/4/tile/incidents/${this.config.incidentStyle || 's0'}/{z}/{x}/{y}.png?key=${encodeURIComponent(key)}`,
        { opacity: 0.8, maxZoom: 22, pane: 'xact-tomtom-pane' }
      ).addTo(this.map);
    }
  }

  // ── Device layers ──────────────────────────────────────────────────────────

  private async refreshLayers(): Promise<void> {
    await this.loadConfiguredIconSets();
    await this.loadConfiguredWidgetTypes();
    this.ensureDeviceLayerPanes();

    // Clear existing device layers
    for (const unsubs of this.pendingDeviceUnsubs.values()) {
      unsubs.forEach(fn => fn());
    }
    this.pendingDeviceUnsubs.clear();
    for (const entry of this.devices.values()) {
      entry.unsubs.forEach(fn => fn());
      if (entry.interval) clearInterval(entry.interval);
      if (this.map) entry.marker.remove();
    }
    this.devices.clear();
    for (const state of this.mapLayerPlugins.values()) {
      state.instance.remove();
    }
    this.mapLayerPlugins.clear();

    for (const unsubs of this.layerUnsubs.values()) {
      unsubs.forEach(fn => fn());
    }
    this.layerUnsubs.clear();

    for (const layer of this.config.layers) {
      if (layer.enabled === false) continue;
      await this.initLayer(layer);
    }
    this.updateLegend();
  }

  private getLayerPaneName(layer: LayerConfig): string {
    return `xact-device-layer-${layer.id}`;
  }

  private ensureDeviceLayerPanes(): void {
    if (!this.map) return;
    const activeLayerIds = new Set(this.config.layers.map(layer => layer.id));
    const panes = this.map.getPanes?.() ?? {};

    Object.keys(panes).forEach(name => {
      if (!name.startsWith('xact-device-layer-')) return;
      const layerId = name.substring('xact-device-layer-'.length);
      if (!activeLayerIds.has(layerId)) {
        panes[name].remove();
      }
    });

    this.config.layers.forEach((layer, index) => {
      const paneName = this.getLayerPaneName(layer);
      if (!this.map.getPane(paneName)) {
        this.map.createPane(paneName);
      }
      const pane = this.map.getPane(paneName);
      if (pane) pane.style.zIndex = String(this.getLayerPaneZIndex(layer, index));
    });
  }

  private getLayerPaneZIndex(layer: LayerConfig, index = this.config.layers.findIndex(l => l.id === layer.id)): number {
    if (this.hoverRaisedLayerId === layer.id) return DEVICE_LAYER_HOVER_Z_INDEX;
    const layerIndex = index >= 0 ? index : 0;
    return DEVICE_LAYER_TOP_Z_INDEX - layerIndex * DEVICE_LAYER_STEP_Z_INDEX;
  }

  private setHoverRaisedLayer(layer: LayerConfig, raised: boolean): void {
    if (!this.map) return;
    this.hoverRaisedLayerId = raised ? layer.id : (this.hoverRaisedLayerId === layer.id ? null : this.hoverRaisedLayerId);
    this.config.layers.forEach((candidate, index) => {
      const pane = this.map.getPane(this.getLayerPaneName(candidate));
      if (pane) pane.style.zIndex = String(this.getLayerPaneZIndex(candidate, index));
    });
  }

  private async initLayer(layer: LayerConfig): Promise<void> {
    const paths = this.resolvePattern(layer.pathPattern);

    // Subscribe to tree changes before adding existing devices.  The KV watcher
    // can still be streaming the first snapshot while the map initializes.
    const store = getMirrorStore();
    const absPattern = store.toAbsolute(layer.pathPattern);
    const parts = absPattern.split('.');
    const starIdx = parts.indexOf('*');
    const parentPath = starIdx > 0 ? parts.slice(0, starIdx).join('.') : '';

    if (parentPath) {
      const unsub = store.subscribeToTreeChanges(parentPath, (changedPath: string, data: any) => {
        // changedPath may be a deep descendant (e.g. "org.Parent.device.meta.lat").
        // We only care about the direct child of parentPath as the device path.
        const afterParent = changedPath.slice(parentPath.length + 1);
        if (!afterParent || afterParent === changedPath) return;
        const changedParts = afterParent.split('.');
        const devicePath = parentPath + '.' + changedParts[0];
        const isDirectDeviceChange = changedParts.length === 1;
        if (data === null && isDirectDeviceChange) {
          if (this.hasMapLayerPlugin(layer)) {
            this.updateMapLayerPluginDevices(layer, devicePath, false);
          } else {
            this.removeDevice(devicePath);
          }
        } else {
          if (this.hasMapLayerPlugin(layer)) {
            this.updateMapLayerPluginDevices(layer, devicePath, true);
          } else if (this.devices.has(devicePath)) {
            if (changedParts[1] === 'meta' && (changedParts[2] === 'lat' || changedParts[2] === 'lon')) {
              this.updateDevicePosition(devicePath);
            }
          } else {
            this.addDevice(layer, devicePath);
          }
        }
      });
      if (!this.layerUnsubs.has(layer.id)) {
        this.layerUnsubs.set(layer.id, []);
      }
      this.layerUnsubs.get(layer.id)!.push(unsub);
    }

    if (this.hasMapLayerPlugin(layer)) {
      this.mountMapLayerPlugin(layer, paths);
    } else {
      for (const devicePath of paths) {
        await this.addDevice(layer, devicePath);
      }
    }
  }

  private getMapLayerPlugin(layer: LayerConfig): any {
    if (!layer.pluginType) return null;
    return (window as any).XACT?.getMapLayerType?.(layer.pluginType) ?? null;
  }

  private hasMapLayerPlugin(layer: LayerConfig): boolean {
    return !!this.getMapLayerPlugin(layer);
  }

  private getPluginBounds(): SavedBounds {
    if (this.config.savedBounds) return this.config.savedBounds;
    if (this.orgArea) return this.orgArea;
    if (this.map) {
      const b = this.map.getBounds();
      return { north: b.getNorth(), south: b.getSouth(), east: b.getEast(), west: b.getWest() };
    }
    return { north: 85, south: -85, east: 180, west: -180 };
  }

  private resolveDeviceTag(devicePath: string, tagPath: string): string {
    const trimmed = String(tagPath ?? '').trim();
    if (!trimmed) return devicePath;
    const orgPrefix = `${devicePath.split('.')[0]}.`;
    return trimmed.startsWith(orgPrefix) ? trimmed : `${devicePath}.${trimmed}`;
  }

  private mountMapLayerPlugin(layer: LayerConfig, devicePaths: string[]): void {
    if (!this.map) return;
    const plugin = this.getMapLayerPlugin(layer);
    if (!plugin || typeof plugin.create !== 'function') return;
    const config = { ...(plugin.defaultConfig ?? {}), ...(layer.pluginConfig ?? {}) };
    const paneName = this.getLayerPaneName(layer);
    try {
      const instance = plugin.create({
        map: this.map,
        L: (window as any).L,
        store: getMirrorStore(),
        layer,
        pane: paneName,
        paneName,
        config,
        getBounds: () => this.getPluginBounds(),
        resolveDeviceTag: (devicePath: string, tagPath: string) => this.resolveDeviceTag(devicePath, tagPath),
        requestSave: (nextConfig: Record<string, any>) => {
          layer.pluginConfig = { ...(layer.pluginConfig ?? {}), ...(nextConfig ?? {}) };
          this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
        },
      });
      this.mapLayerPlugins.set(layer.id, { instance, devicePaths: new Set(devicePaths) });
      instance.updateDevices(devicePaths);
    } catch (err) {
      console.error('Map layer plugin error:', layer.pluginType, err);
    }
  }

  private updateMapLayerPluginDevices(layer: LayerConfig, devicePath: string, present: boolean): void {
    const state = this.mapLayerPlugins.get(layer.id);
    if (!state) return;
    if (present) state.devicePaths.add(devicePath);
    else state.devicePaths.delete(devicePath);
    state.instance.updateDevices([...state.devicePaths]);
  }

  private resolvePattern(pattern: string): string[] {
    if (!pattern) return [];
    const store = getMirrorStore();
    // Convert to absolute for store operations (idempotent for legacy full paths)
    const absPattern = store.toAbsolute(pattern);
    const parts = absPattern.split('.');
    const starIdx = parts.indexOf('*');
    if (starIdx === -1) return [absPattern];

    const prefix = parts.slice(0, starIdx).join('.');
    const suffix = parts.slice(starIdx + 1);

    const children = store.listChildrenNames(prefix);
    const results: string[] = [];
    for (const child of children) {
      const childPath = prefix ? `${prefix}.${child}` : child;
      if (suffix.length === 0) {
        results.push(childPath);
      } else if (suffix[0] === '*') {
        const subPattern = childPath + '.' + suffix.join('.');
        results.push(...this.resolvePattern(subPattern));
      } else {
        results.push(childPath + '.' + suffix.join('.'));
      }
    }
    return results;
  }

  private async addDevice(layer: LayerConfig, devicePath: string): Promise<void> {
    if (this.devices.has(devicePath)) return;
    if (!this.map) return;

    const L = (window as any).L;
    const store = getMirrorStore();

    const position = this.readDevicePosition(devicePath);
    if (!position) {
      this.watchPendingDevicePosition(layer, devicePath);
      return;
    }

    // Handle plugin type
    if (layer.itemType === 'plugin' && layer.pluginType) {
      const renderer = (window as any).XACT?.getMapItemType?.(layer.pluginType);
      if (renderer) {
        try {
          const leafletObj = renderer(devicePath, store, L);
          if (leafletObj) {
            if (leafletObj.options && !leafletObj.options.pane) {
              leafletObj.options.pane = this.getLayerPaneName(layer);
            }
            leafletObj.addTo(this.map);
            leafletObj.on('click', () => this.onDeviceClick(devicePath));
            const unsubs: Array<() => void> = [];
            this.devices.set(devicePath, { marker: leafletObj, layer, unsubs, divTagPaths: new Set() });
            this.subscribeToDevicePosition(devicePath, unsubs);
          }
        } catch (e) {
          console.error('Map plugin renderer error:', e);
        }
      }
      return;
    }

    // Create Leaflet marker
    const rule = this.evaluateRules(layer.iconRules || [], devicePath);
    const iconHtml = this.makeIconHtml(rule, layer, devicePath);
    const { iconSize, iconAnchor } = this.getIconGeometry(rule, layer);

    const icon = L.divIcon({
      className: '',
      html: iconHtml,
      iconSize,
      iconAnchor,
    });

    const marker = L.marker(position, { icon, pane: this.getLayerPaneName(layer) });
    marker.addTo(this.map);
    marker.on('click', () => this.onDeviceClick(devicePath));
    marker.on('mouseover', () => {
      this.setHoverRaisedLayer(layer, true);
      this.mountHoverWidget(devicePath);
    });
    marker.on('mouseout', () => this.setHoverRaisedLayer(layer, false));

    const unsubs: Array<() => void> = [];

    // Subscribe to icon rule tags
    const orgPrefix = devicePath.split('.')[0] + '.';
    for (const rule of (layer.iconRules || [])) {
      const tagPath = rule.tag.startsWith(orgPrefix) ? rule.tag : devicePath + '.' + rule.tag;
      if (!this.shouldSubscribeTagReference(tagPath)) continue;
      let initialCallback = true;
      const unsub = store.subscribeTagReference(tagPath, () => {
        if (initialCallback) {
          initialCallback = false;
          return;
        }
        this.updateDeviceMarker(devicePath);
      });
      unsubs.push(unsub);
    }

    // Refresh interval
    let interval: ReturnType<typeof setInterval> | undefined;
    if ((layer.refreshInterval ?? 0) > 0) {
      interval = setInterval(() => this.updateDeviceMarker(devicePath), layer.refreshInterval!);
    }

    this.devices.set(devicePath, { marker, layer, unsubs, interval, divTagPaths: new Set(), lastIconHtml: iconHtml });
    this.subscribeToDevicePosition(devicePath, unsubs);

    if (this.hasZoomWidget(layer)) {
      this.scheduleZoomWidgetMount(devicePath);
    }

    // Subscribe to every tag path referenced in the div template so the marker
    // re-renders whenever a value changes - even when zoomed out.
    this.subscribeToTemplateTags(layer, devicePath);
  }

  private watchPendingDevicePosition(layer: LayerConfig, devicePath: string): void {
    if (this.pendingDeviceUnsubs.has(devicePath) || this.devices.has(devicePath)) return;

    const store = getMirrorStore();
    const unsubs: Array<() => void> = [];
    this.pendingDeviceUnsubs.set(devicePath, unsubs);

    const tryAdd = () => {
      if (!this.pendingDeviceUnsubs.has(devicePath) || this.devices.has(devicePath) || !this.map) return;
      if (!this.readDevicePosition(devicePath)) return;
      const pendingUnsubs = this.pendingDeviceUnsubs.get(devicePath) ?? [];
      this.pendingDeviceUnsubs.delete(devicePath);
      pendingUnsubs.forEach(fn => fn());
      void this.addDevice(layer, devicePath);
    };
    const scheduleTryAdd = () => queueMicrotask(tryAdd);

    unsubs.push(store.subscribeTagReference(`${devicePath}.meta.lat`, scheduleTryAdd));
    unsubs.push(store.subscribeTagReference(`${devicePath}.meta.lon`, scheduleTryAdd));
    scheduleTryAdd();
  }

  private subscribeToDevicePosition(devicePath: string, unsubs: Array<() => void>): void {
    const store = getMirrorStore();
    const subscribeCoordinate = (path: string) => {
      let initialCallback = true;
      unsubs.push(store.subscribeTagReference(path, () => {
        if (initialCallback) {
          initialCallback = false;
          return;
        }
        this.updateDevicePosition(devicePath);
      }));
    };
    subscribeCoordinate(`${devicePath}.meta.lat`);
    subscribeCoordinate(`${devicePath}.meta.lon`);
  }

  private readDevicePosition(devicePath: string): [number, number] | null {
    const store = getMirrorStore();
    const rawLat = store.getNodeValue(devicePath + '.meta.lat');
    const rawLon = store.getNodeValue(devicePath + '.meta.lon');
    if (rawLat === undefined || rawLat === null || rawLat === '' || rawLon === undefined || rawLon === null || rawLon === '') {
      return null;
    }
    const lat = Number(rawLat);
    const lon = Number(rawLon);
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
    return [lat, lon];
  }

  private updateDevicePosition(devicePath: string): void {
    const entry = this.devices.get(devicePath);
    if (!entry) return;
    const position = this.readDevicePosition(devicePath);
    if (position && typeof entry.marker?.setLatLng === 'function') {
      entry.marker.setLatLng(position);
    }
  }

  private shouldSubscribeTagReference(path: string): boolean {
    const store = getMirrorStore();
    const basePath = store.baseTagPath(path);
    if (!basePath) return false;
    if (store.getNodeType(basePath) !== 'unknown') return true;

    const parentPath = basePath.split('.').slice(0, -1).join('.');
    return !parentPath || store.getNodeType(parentPath) === 'unknown';
  }

  private removeDevice(devicePath: string): void {
    const pendingUnsubs = this.pendingDeviceUnsubs.get(devicePath);
    if (pendingUnsubs) {
      pendingUnsubs.forEach(fn => fn());
      this.pendingDeviceUnsubs.delete(devicePath);
    }
    const entry = this.devices.get(devicePath);
    if (!entry) return;
    entry.unsubs.forEach(fn => fn());
    if (entry.interval) clearInterval(entry.interval);
    if (this.map) entry.marker.remove();
    this.devices.delete(devicePath);
    if (this.selectedDevicePath === devicePath) this.selectedDevicePath = null;
  }

  private getZoomWidgetType(layer: LayerConfig): string {
    return layer.zoomWidgetType || (layer.divWidgetConfig ? 'status-table-widget' : '');
  }

  private getZoomWidgetConfig(layer: LayerConfig): Record<string, any> {
    return layer.zoomWidgetConfig ?? layer.divWidgetConfig ?? {};
  }

  private hasZoomWidget(layer: LayerConfig): boolean {
    return !!this.getZoomWidgetType(layer);
  }

  private getSidePanelWidgetType(layer: LayerConfig): string {
    return layer.sidePanelWidgetType || '';
  }

  private getSidePanelWidgetConfig(layer: LayerConfig): Record<string, any> {
    return layer.sidePanelWidgetConfig ?? {};
  }

  private async loadConfiguredWidgetTypes(): Promise<void> {
    const types: string[] = [];
    for (const layer of this.config.layers) {
      const zoomType = this.getZoomWidgetType(layer);
      const sideType = this.getSidePanelWidgetType(layer);
      if (zoomType) types.push(zoomType);
      if (sideType) types.push(sideType);
    }
    await ensureWidgetTypesLoaded(types);
  }

  private hasSidePanelWidget(layer: LayerConfig): boolean {
    return !!this.getSidePanelWidgetType(layer);
  }

  private configForDevice(config: Record<string, any>, devicePath: string, widgetType = ''): Record<string, any> {
    const deviceName = devicePath.split('.').pop() ?? devicePath;
    const orgName = devicePath.split('.')[0] ?? '';
    const cfg = { ...(config ?? {}) };
    if (cfg.tagPrefix) {
      const prefix = String(cfg.tagPrefix).replace('*', deviceName);
      cfg.tagPrefix = devicePath.startsWith(prefix + '.') ? devicePath : prefix;
    } else if (widgetType === 'status-table-widget') {
      cfg.tagPrefix = devicePath;
    }
    if (widgetType === 'html-widget') {
      cfg.tagPrefix = cfg.tagPrefix || devicePath;
      cfg.devicePath = devicePath;
      cfg.deviceName = deviceName;
      cfg.orgName = cfg.orgName || orgName;
    }
    return cfg;
  }

  private createZoomWidget(layer: LayerConfig, devicePath: string): HTMLElement | null {
    const type = this.getZoomWidgetType(layer);
    if (!type) return null;
    if (!customElements.get(type)) {
      void ensureWidgetTypeLoaded(type).then(() => {
        if (this.isConnected) this.refreshLayers().then(() => this.updateLegend());
      }).catch(err => console.error('MapWidget: cannot load zoom widget', type, err));
      return null;
    }
    try {
      const widget = document.createElement(type) as any;
      widget.style.cssText = type === 'html-widget'
        ? 'display:block;width:100%;min-height:0;'
        : 'display:block;width:100%;height:100%;';
      if (typeof widget.setEditMode === 'function') widget.setEditMode(false);
      if (typeof widget.setConfig === 'function') {
        widget.setConfig(this.configForDevice(this.getZoomWidgetConfig(layer), devicePath, type));
      }
      return widget as HTMLElement;
    } catch (err) {
      console.error('MapWidget: cannot create zoom widget', type, err);
      return null;
    }
  }

  private mountDivWidget(devicePath: string): void {
    const entry = this.devices.get(devicePath);
    if (!entry || !this.hasZoomWidget(entry.layer)) return;
    const markerEl = entry.marker.getElement() as HTMLElement | null;
    if (!markerEl) return;
    const body = markerEl.querySelector<HTMLElement>('.xact-map-dw-body');
    if (!body) return;

    // Mount widget once; subsequent calls for the same device are no-ops
    if (!entry.divWidgetEl || !body.contains(entry.divWidgetEl)) {
      const widget = this.createZoomWidget(entry.layer, devicePath);
      if (!widget) return;
      body.replaceChildren(widget);
      entry.divWidgetEl = widget;
    }

    // Re-attach gear listener (DOM is fresh after setIcon)
    const gear = markerEl.querySelector<HTMLElement>('.xact-map-dw-gear');
    if (gear) {
      const fresh = gear.cloneNode(true) as HTMLElement;
      gear.parentNode?.replaceChild(fresh, gear);
      fresh.addEventListener('click', (e) => {
        e.stopPropagation();
        void this.openZoomWidgetConfig(entry.layer);
      });
    }
  }

  private updateDeviceMarker(devicePath: string): void {
    const entry = this.devices.get(devicePath);
    if (!entry || !this.map) return;
    const L = (window as any).L;
    const rule = this.evaluateRules(entry.layer.iconRules || [], devicePath);
    const iconHtml = this.makeIconHtml(rule, entry.layer, devicePath);
    if (iconHtml === entry.lastIconHtml) {
      // Preserve live embedded widgets, but still recover if the initial mount
      // ran before Leaflet attached the marker element to the DOM.
      if (this.hasZoomWidget(entry.layer)) {
        this.mountDivWidget(devicePath);
        this.mountHoverWidget(devicePath);
      }
      this.applySelectedMarker();
      return;
    }
    entry.lastIconHtml = iconHtml;
    const { iconSize, iconAnchor } = this.getIconGeometry(rule, entry.layer);
    entry.marker.setIcon(L.divIcon({
      className: '',
      html: iconHtml,
      iconSize,
      iconAnchor,
    }));
    if (this.hasZoomWidget(entry.layer)) {
      this.mountDivWidget(devicePath);
      this.mountHoverWidget(devicePath);
    }
    this.applySelectedMarker();
  }

  private mountHoverWidget(devicePath: string): void {
    const entry = this.devices.get(devicePath);
    if (!entry || !this.hasZoomWidget(entry.layer)) return;
    const zoom = this.map?.getZoom() ?? 0;
    if (zoom >= (entry.layer.zoomThreshold ?? 13)) return; // card is the marker; hover tip not used
    const markerEl = entry.marker.getElement() as HTMLElement | null;
    if (!markerEl) return;
    const body = markerEl.querySelector<HTMLElement>('.xact-map-hover-body');
    if (!body) return;
    if (!entry.hoverWidgetEl || !body.contains(entry.hoverWidgetEl)) {
      const widget = this.createZoomWidget(entry.layer, devicePath);
      if (!widget) return;
      body.replaceChildren(widget);
      entry.hoverWidgetEl = widget;
    }
  }

  private scheduleZoomWidgetMount(devicePath: string, attempts = 6): void {
    if (attempts <= 0 || !this.devices.has(devicePath)) return;
    requestAnimationFrame(() => {
      const entry = this.devices.get(devicePath);
      if (!entry) return;

      this.mountDivWidget(devicePath);
      this.mountHoverWidget(devicePath);

      const markerEl = entry.marker?.getElement?.() as HTMLElement | null;
      const zoom = this.map?.getZoom() ?? 0;
      const threshold = entry.layer.zoomThreshold ?? 13;
      const needsHover = this.hasZoomWidget(entry.layer) && zoom < threshold && !entry.hoverWidgetEl;
      const needsDiv = this.hasZoomWidget(entry.layer) && zoom >= threshold && !entry.divWidgetEl;
      if (!markerEl || needsHover || needsDiv) {
        this.scheduleZoomWidgetMount(devicePath, attempts - 1);
      }
    });
  }

  private onZoomChange(): void {
    if (!this.map) return;
    const zoomEl = this.querySelector<HTMLElement>('#map-zoom');
    if (zoomEl) zoomEl.textContent = `Z ${this.map.getZoom()}`;
    const zoom = this.map.getZoom();
    for (const [, entry] of this.devices) {
      if (this.hasZoomWidget(entry.layer) && zoom < (entry.layer.zoomThreshold ?? 13)) {
        entry.divWidgetEl = null;
        entry.hoverWidgetEl = null;
      }
    }
    for (const [devicePath, entry] of this.devices) {
      if (entry.layer.itemType === 'icon' || (entry.layer.itemType as string) === 'div') {
        this.updateDeviceMarker(devicePath);
      }
    }
  }

  private getIconGeometry(rule: IconRule | null, layer: LayerConfig): { iconSize: [number, number]; iconAnchor: [number, number] } {
    const offsetX = layer.offsetX ?? 0;
    const offsetY = layer.offsetY ?? 0;
    const zoom = this.map?.getZoom() ?? 0;
    const template = layer.divTemplate ?? layer.divTemplateIn ?? '';

    if (this.hasZoomWidget(layer) && zoom >= (layer.zoomThreshold ?? 13)) {
      return { iconSize: [0, 0], iconAnchor: [-offsetX, -offsetY] };
    }

    if (template && zoom >= (layer.zoomThreshold ?? 13)) {
      // Div-template marker - keep legacy CSS-transform positioning
      return { iconSize: [0, 0], iconAnchor: [-offsetX, -offsetY] };
    }

    const size = rule?.size ?? layer.defaultSize ?? 24;
    return {
      iconSize: [size, size],
      iconAnchor: [Math.floor(size / 2) - offsetX, size - offsetY],
    };
  }

  private evaluateRules(rules: IconRule[], devicePath: string): IconRule | null {
    const store = getMirrorStore();
    const orgPrefix = devicePath.split('.')[0] + '.';
    for (const rule of rules) {
      const tagPath = rule.tag.startsWith(orgPrefix) ? rule.tag : devicePath + '.' + rule.tag;
      const actual = store.resolveTagReference(tagPath);
      if (evaluateCond(actual, rule.cond, rule.value)) {
        return rule;
      }
    }
    return null;
  }

  private makeIconHtml(rule: IconRule | null, layer: LayerConfig, devicePath: string): string {
    const template = layer.divTemplate ?? layer.divTemplateIn ?? '';
    const zoom = this.map?.getZoom() ?? 0;
    const threshold = layer.zoomThreshold ?? 13;

    const selectedClass = this.selectedDevicePath === devicePath ? ' xact-map-marker-selected' : '';

    // Zoomed widget mode - stable card HTML; live widget is mounted by mountDivWidget()
    if (this.hasZoomWidget(layer) && zoom >= threshold) {
      const w = layer.divWidgetWidth ?? 280;
      const name = esc(devicePath.split('.').pop() ?? devicePath);
      return `<div class="xact-map-marker-root${selectedClass}"><div class="xact-map-dw-card" style="transform:translate(-50%,-100%);cursor:default;`
           + `width:${w}px;background:var(--panel-bg,#1a1a1a);border:1px solid var(--border-color);color:#f3f4f6;`
           + `border-radius:6px;overflow:hidden;box-shadow:0 8px 24px rgba(0,0,0,0.55);">`
           + `<div class="xact-map-dw-header" style="display:flex;align-items:center;`
           + `justify-content:space-between;padding:6px 10px;border-bottom:1px solid var(--border-color);">`
           + `<span style="font-size:12px;font-weight:700;color:#fff;">${name}</span>`
           + `<button class="xact-map-dw-gear" title="Configure widget"`
           + ` style="font-size:12px;padding:2px 6px;border-radius:3px;cursor:pointer;`
           + `background:color-mix(in srgb,var(--accent-color) 12%,transparent);`
           + `border:1px solid color-mix(in srgb,var(--accent-color) 28%,transparent);`
           + `color:var(--accent-color);display:${this._editMode ? 'inline-block' : 'none'};">⚙</button></div>`
           + `<div class="xact-map-dw-body"></div></div></div>`;
    }

    // Zoomed in past threshold - render the div template as the marker
    if (template && zoom >= threshold) {
      return this.evaluateTemplate(layer, devicePath);
    }

    const glyph = rule?.glyph ?? layer.defaultGlyph ?? 'mdi:map-marker';
    const color = rule?.color ?? layer.defaultColor ?? '#f59e0b';
    const anim = rule?.animation ?? 'none';
    const size = rule?.size ?? layer.defaultSize ?? 24;

    const animClass = anim === 'pulse' ? 'xact-map-anim-pulse'
                    : anim === 'shake' ? 'xact-map-anim-shake'
                    : '';

    const svg = getIconSVG(glyph, color, size);
    const prefix = iconSetPrefix(glyph);
    if (!svg && prefix && !isIconSetLoaded(prefix)) {
      loadIconSet(prefix).then(() => {
        if (isIconSetLoaded(prefix) && this.devices.has(devicePath)) this.updateDeviceMarker(devicePath);
      });
    }
    const fallbackGlyph = prefix ? '📍' : glyph;
    const glyphHtml = svg
      ? svg
      : `<span style="font-size:${size}px;text-shadow:0 1px 3px rgba(0,0,0,0.5);line-height:1;">${fallbackGlyph}</span>`;
    const iconDiv = `<div class="xact-map-icon-wrap ${animClass}" style="color:${color};text-align:center;line-height:1;">${glyphHtml}</div>`;

    // Hover tooltip sits above the icon; positioned relative to the marker root which matches icon dimensions
    let hoverContent: string;
    if (template) {
      const name = esc(devicePath.split('.').pop() ?? devicePath);
      hoverContent = `<div class="xact-map-hover-card" style="background:var(--panel-bg,#1a1a1a);border:1px solid var(--border-color);border-radius:6px;overflow:hidden;box-shadow:0 8px 24px rgba(0,0,0,0.55);min-width:${layer.divWidgetWidth ?? 280}px;color:#f3f4f6;">`
        + `<div class="xact-map-dw-header" style="display:flex;align-items:center;justify-content:space-between;padding:6px 10px;border-bottom:1px solid var(--border-color);">`
        + `<span style="font-size:12px;font-weight:700;color:#fff;">${name}</span></div>`
        + `<div>${this.renderTemplateContent(layer, devicePath)}</div></div>`;
    } else if (this.hasZoomWidget(layer)) {
      // Zoomed widget mode - mount a live widget into this placeholder after marker creation
      const name = esc(devicePath.split('.').pop() ?? devicePath);
      hoverContent = `<div class="xact-map-hover-card" style="background:var(--panel-bg,#1a1a1a);border:1px solid var(--border-color);border-radius:6px;overflow:hidden;box-shadow:0 8px 24px rgba(0,0,0,0.55);min-width:${layer.divWidgetWidth ?? 280}px;color:#f3f4f6;">`
        + `<div class="xact-map-dw-header" style="display:flex;align-items:center;justify-content:space-between;padding:6px 10px;border-bottom:1px solid var(--border-color);">`
        + `<span style="font-size:12px;font-weight:700;color:#fff;">${name}</span></div>`
        + `<div class="xact-map-hover-body"></div></div>`;
    } else {
      hoverContent = '';
    }
    const hoverTip = hoverContent ? `<div class="xact-map-hover-tip">${hoverContent}</div>` : '';

    return `<div class="xact-map-marker-root${selectedClass}" style="width:${size}px;height:${size}px;">${iconDiv}${hoverTip}</div>`;
  }


  /**
   * Parse all `tag('...')` / `tag("...")` literals from the div template and
   * call store.subscribe() for each one.  When a value changes the callback
   * calls updateDeviceMarker() so the div re-renders with fresh data.
   *
   * We do this statically (regex on the template string) rather than via a
   * runtime side-effect inside evaluateTemplate(), because Node.subscribe()
   * fires the callback *immediately* if a value already exists - triggering a
   * recursive updateDeviceMarker() mid-addDevice() before all subscriptions
   * are registered.  Parsing the template string first is reliable and cheap.
   */
  private subscribeToTemplateTags(layer: LayerConfig, devicePath: string): void {
    const template = layer.divTemplate ?? layer.divTemplateIn ?? '';
    if (!template) return;
    const entry = this.devices.get(devicePath);
    if (!entry) return;

    const store = getMirrorStore();
    const orgPrefix = devicePath.split('.')[0] + '.';

    // Match tag('...'), tag("..."), tag(`...`) - static paths only.
    const tagRe = /\btag\s*\(\s*['"`]([^'"`]+)['"`]\s*\)/g;
    let m: RegExpExecArray | null;
    while ((m = tagRe.exec(template)) !== null) {
      const relPath = m[1];
      const fullPath = relPath.startsWith(orgPrefix) ? relPath : devicePath + '.' + relPath;
      if (entry.divTagPaths.has(fullPath)) continue;
      if (!this.shouldSubscribeTagReference(fullPath)) continue;
      entry.divTagPaths.add(fullPath);
      let initialCallback = true;
      const unsub = store.subscribeTagReference(fullPath, () => {
        if (initialCallback) {
          initialCallback = false;
          return;
        }
        if (this.devices.has(devicePath)) this.updateDeviceMarker(devicePath);
      });
      entry.unsubs.push(unsub);
    }
  }

  private renderTemplateContent(layer: LayerConfig, devicePath: string): string {
    const store = getMirrorStore();
    const template = layer.divTemplate ?? layer.divTemplateIn ?? '';
    const deviceName = devicePath.split('.').pop() ?? devicePath;
    const deviceDescription = store.getNodeShared(devicePath)?.description ?? '';
    const orgPrefix = devicePath.split('.')[0] + '.';
    const tagFn = (relPath: string): any => {
      const fullPath = relPath.startsWith(orgPrefix) ? relPath : devicePath + '.' + relPath;
      return store.resolveTagReference(fullPath) ?? '';
    };
    // Auto-quote bare dotted paths in tag() calls: tag(sign.message) → tag('sign.message')
    const normalised = template.replace(
      /\btag\(([a-zA-Z_$][a-zA-Z0-9_$]*(?:\.[a-zA-Z_$][a-zA-Z0-9_$]*)+(?::[a-zA-Z_][a-zA-Z0-9_-]*)?)\)/g,
      "tag('$1')"
    );
    try {
      // eslint-disable-next-line no-new-func
      return new Function('deviceName', 'deviceDescription', 'tag', `return \`${normalised}\``)(
        deviceName, deviceDescription, tagFn
      );
    } catch (err) {
      console.error(`[map-widget] Div template error for "${devicePath}":`, err, '\nTemplate:', template);
      return `<span style="background:#1a1a1a;padding:4px 6px;border-radius:4px;font-size:11px;color:#f59e0b;">${esc(deviceName)}</span>`;
    }
  }

  private evaluateTemplate(layer: LayerConfig, devicePath: string): string {
    const content = this.renderTemplateContent(layer, devicePath);
    const selectedClass = this.selectedDevicePath === devicePath ? ' xact-map-marker-selected' : '';
    return `<div class="xact-map-marker-root${selectedClass}" style="transform:translate(-50%,-100%);cursor:pointer;"><div class="xact-map-dw-card">${content}</div></div>`;
  }

  private async openZoomWidgetConfig(layer: LayerConfig): Promise<void> {
    const type = this.getZoomWidgetType(layer);
    if (!type) return;
    await ensureWidgetTypeLoaded(type);
    if (!customElements.get(type)) return;

    const tmp = document.createElement(type) as any;
    tmp.style.cssText = 'position:fixed;left:-9999px;top:-9999px;width:320px;height:240px;overflow:hidden;';
    document.body.appendChild(tmp);
    if (typeof tmp.setConfig === 'function') tmp.setConfig(this.getZoomWidgetConfig(layer));

    const saveConfig = (config: Record<string, any>) => {
      layer.zoomWidgetType = type;
      layer.zoomWidgetConfig = config;
      layer.divWidgetConfig = type === 'status-table-widget' ? config : undefined;
      for (const [devPath, devEntry] of this.devices) {
        if (devEntry.layer.id !== layer.id) continue;
        const cfg = this.configForDevice(config, devPath, type);
        if (devEntry.divWidgetEl && typeof (devEntry.divWidgetEl as any).setConfig === 'function') {
          (devEntry.divWidgetEl as any).setConfig(cfg);
        }
        if (devEntry.hoverWidgetEl && typeof (devEntry.hoverWidgetEl as any).setConfig === 'function') {
          (devEntry.hoverWidgetEl as any).setConfig(cfg);
        }
      }
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
    };

    tmp.addEventListener('widget-config-close', () => tmp.remove(), { once: true });
    tmp.addEventListener('widget-config-save', (e: CustomEvent) => {
      saveConfig(e.detail?.config ?? (typeof tmp.getConfig === 'function' ? tmp.getConfig() : {}));
      tmp.remove();
    }, { once: true });

    if (typeof tmp.openConfig === 'function') {
      tmp.openConfig();
      return;
    }

    if (typeof tmp.getPropertySchema === 'function') {
      const schema = tmp.getPropertySchema();
      if (schema?.length) {
        const dialog = document.createElement('widget-properties-dialog') as WidgetPropertiesDialog;
        document.body.appendChild(dialog);
        const onPropertiesUpdated = ((e: CustomEvent) => {
          saveConfig({ ...this.getZoomWidgetConfig(layer), ...(e.detail?.config ?? {}) });
          dialog.remove();
          tmp.remove();
        }) as EventListener;
        dialog.addEventListener('properties-updated', onPropertiesUpdated, { once: true });
        dialog.open('map-zoom-widget', schema, this.getZoomWidgetConfig(layer), false, getWidgetMeta(type)?.name ?? 'Widget Properties');
        return;
      }
    }

    tmp.remove();
  }

  private async openSidePanelWidgetConfig(layer: LayerConfig): Promise<void> {
    const type = this.getSidePanelWidgetType(layer);
    if (!type) return;
    await ensureWidgetTypeLoaded(type);
    if (!customElements.get(type)) return;

    const tmp = document.createElement(type) as any;
    tmp.style.cssText = 'position:fixed;left:-9999px;top:-9999px;width:360px;height:420px;overflow:hidden;';
    document.body.appendChild(tmp);
    if (typeof tmp.setConfig === 'function') tmp.setConfig(this.getSidePanelWidgetConfig(layer));

    const saveConfig = (config: Record<string, any>) => {
      layer.sidePanelWidgetType = type;
      layer.sidePanelWidgetConfig = config;
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
      if (this.selectedDevicePath) this.renderClickPanel(this.selectedDevicePath);
    };

    tmp.addEventListener('widget-config-close', () => tmp.remove(), { once: true });
    tmp.addEventListener('widget-config-save', (e: CustomEvent) => {
      saveConfig(e.detail?.config ?? (typeof tmp.getConfig === 'function' ? tmp.getConfig() : {}));
      tmp.remove();
    }, { once: true });

    if (typeof tmp.openConfig === 'function') {
      tmp.openConfig();
      return;
    }

    if (typeof tmp.getPropertySchema === 'function') {
      const schema = tmp.getPropertySchema();
      if (schema?.length) {
        const dialog = document.createElement('widget-properties-dialog') as WidgetPropertiesDialog;
        document.body.appendChild(dialog);
        const onPropertiesUpdated = ((e: CustomEvent) => {
          saveConfig({ ...this.getSidePanelWidgetConfig(layer), ...(e.detail?.config ?? {}) });
          dialog.remove();
          tmp.remove();
        }) as EventListener;
        dialog.addEventListener('properties-updated', onPropertiesUpdated, { once: true });
        dialog.open('map-side-panel-widget', schema, this.getSidePanelWidgetConfig(layer), false, getWidgetMeta(type)?.name ?? 'Widget Properties');
        return;
      }
    }

    tmp.remove();
  }

  private openMapLayerPluginConfig(layer: LayerConfig): void {
    const plugin = this.getMapLayerPlugin(layer);
    if (!plugin || typeof plugin.getPropertySchema !== 'function') return;
    const schema = plugin.getPropertySchema();
    if (!schema?.length) return;

    const dialog = document.createElement('widget-properties-dialog') as WidgetPropertiesDialog;
    document.body.appendChild(dialog);
    const currentConfig = { ...(plugin.defaultConfig ?? {}), ...(layer.pluginConfig ?? {}) };
    const onPropertiesUpdated = ((e: CustomEvent) => {
      layer.pluginConfig = { ...currentConfig, ...(e.detail?.config ?? {}) };
      const state = this.mapLayerPlugins.get(layer.id);
      if (state?.instance.setConfig) {
        state.instance.setConfig(layer.pluginConfig);
        state.instance.updateDevices([...state.devicePaths]);
      }
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
      dialog.remove();
    }) as EventListener;
    dialog.addEventListener('properties-updated', onPropertiesUpdated, { once: true });
    dialog.open('map-layer-plugin', schema as any, currentConfig, false, 'Map Layer Plugin');
  }

  // ── Device click panel ─────────────────────────────────────────────────────

  private onDeviceClick(devicePath: string): void {
    this.clearClickPanelWidget();
    this.setUiDeviceContext(devicePath);
    this.selectedDevicePath = devicePath;
    this.applySelectedMarker();
    const entry = this.devices.get(devicePath);
    this.renderClickPanel(devicePath);
    const panel = this.querySelector<HTMLElement>('#device-panel');
    if (!panel) return;
    panel.style.display = entry ? 'flex' : 'none';
  }

  private clearClickPanelWidget(): void {
    const body = this.querySelector<HTMLElement>('#dp-body');
    if (body) body.replaceChildren();
  }

  private setUiDeviceContext(devicePath: string): void {
    const deviceName = devicePath.split('.').pop() ?? devicePath;
    const ui = getUiStore();
    ui.set('deviceName', deviceName);
    const relativePath = getMirrorStore().toRelative(devicePath);
    const parts = relativePath.split('.').filter(Boolean);
    const deviceType = parts.length >= 2 ? parts[parts.length - 2] : '';
    if (deviceType) ui.set('deviceType', deviceType);
  }

  private openDetailDashboard(devicePath: string, dashboardId: string): void {
    if (!dashboardId) return;
    this.setUiDeviceContext(devicePath);
    this.emit('dashboard-open', { dashboard: dashboardId, id: dashboardId, devicePath });
  }

  private applySelectedMarker(): void {
    for (const [path, entry] of this.devices) {
      const markerEl = entry.marker?.getElement?.() as HTMLElement | null;
      const root = markerEl?.querySelector<HTMLElement>('.xact-map-marker-root');
      if (root) root.classList.toggle('xact-map-marker-selected', path === this.selectedDevicePath);
    }
  }

  private renderTagGroupsHtml(devicePath: string): string {
    const store = getMirrorStore();
    const groups = store.listChildrenNames(devicePath).filter(g => g !== 'meta');
    const kpiGroup = groups.includes('kpi') ? ['kpi'] : [];
    const otherGroups = groups.filter(g => g !== 'kpi').sort();
    const orderedGroups = [...kpiGroup, ...otherGroups];

    if (orderedGroups.length === 0) {
      return '<div style="color:#cbd5e1;font-style:italic;font-size:12px;">No tag groups found</div>';
    }

    return orderedGroups.map(group => {
      const groupPath = devicePath + '.' + group;
      const tags = store.listChildrenNames(groupPath);
      const rowsHtml = tags.map(tagName => {
        const tagPath = groupPath + '.' + tagName;
        const val = store.getNodeValue(tagPath);
        const displayVal = formatTagValue(val);
        const valStyle = typeof val === 'boolean'
          ? `color:${val ? '#4ade80' : '#f87171'};`
          : '';
        return `
          <tr>
            <td style="padding:5px 8px;color:#d1d5db;border-right:1px solid var(--border-color);white-space:nowrap;">${esc(tagName)}</td>
            <td style="padding:5px 8px;font-family:'IBM Plex Mono',monospace;color:#f9fafb;${valStyle}">${esc(displayVal)}</td>
          </tr>`;
      }).join('');

      return `
        <div class="dp-group" style="margin-bottom:12px;">
          <div style="font-size:11px;text-transform:uppercase;letter-spacing:0.05em;color:var(--accent-color);font-weight:700;margin-bottom:5px;padding:0 4px;">${esc(group)}</div>
          <table style="width:100%;border-collapse:collapse;font-size:12px;border:1px solid var(--border-color);border-radius:4px;overflow:hidden;background:rgba(255,255,255,0.03);">
            ${rowsHtml || '<tr><td colspan="2" style="padding:5px 8px;color:#cbd5e1;font-style:italic;">No tags</td></tr>'}
          </table>
        </div>`;
    }).join('');
  }

  private renderClickPanel(devicePath: string): void {
    const panel = this.querySelector<HTMLElement>('#device-panel');
    if (!panel) return;

    const store = getMirrorStore();
    const deviceName = devicePath.split('.').pop() ?? devicePath;
    const shared = store.getNodeShared(devicePath);
    const description = shared?.description ?? '';
    const entry = this.devices.get(devicePath);
    const sidePanelWidgetType = entry ? this.getSidePanelWidgetType(entry.layer) : '';
    const hasSidePanelWidget = !!sidePanelWidgetType;
    const canConfigureSidePanelWidget = this._editMode && hasSidePanelWidget;
    const detailDashboardId = entry?.layer.detailDashboardId ? String(entry.layer.detailDashboardId) : '';
    const deviceNameHtml = detailDashboardId
      ? `<button id="dp-device-link" type="button" title="Click to view device details"
                 style="display:inline;padding:0;margin:0;border:0;background:transparent;color:#fff;font:inherit;font-weight:700;font-size:15px;text-align:left;cursor:pointer;text-decoration:underline;text-decoration-thickness:1px;text-underline-offset:3px;">
            ${esc(deviceName)}
          </button>`
      : `<div style="font-weight:700;font-size:15px;color:#fff;">${esc(deviceName)}</div>`;

    panel.innerHTML = `
      <div style="display:flex;flex-direction:column;height:100%;font-size:13px;">
        <!-- Header -->
        <div style="display:flex;align-items:center;justify-content:space-between;padding:12px 16px;border-bottom:1px solid var(--border-color);flex-shrink:0;background:rgba(255,255,255,0.04);">
          <div>
            ${deviceNameHtml}
            ${description ? `<div style="color:#d1d5db;font-size:12px;margin-top:2px;">${esc(description)}</div>` : ''}
          </div>
          <div style="display:flex;gap:6px;align-items:center;">
            <button id="dp-gear" title="${canConfigureSidePanelWidget ? 'Configure side panel widget' : 'Side panel widget configuration is available in edit mode'}" ${canConfigureSidePanelWidget ? '' : 'disabled'}
                    style="display:${this._editMode ? 'inline-block' : 'none'};font-size:14px;line-height:1;padding:3px 8px;border-radius:4px;cursor:${canConfigureSidePanelWidget ? 'pointer' : 'not-allowed'};background:color-mix(in srgb,var(--accent-color) 12%,transparent);border:1px solid color-mix(in srgb,var(--accent-color) 28%,transparent);color:var(--accent-color);opacity:${canConfigureSidePanelWidget ? '1' : '0.45'};">⚙</button>
            <button id="dp-close"
                    style="font-size:20px;line-height:1;padding:2px 8px;border-radius:4px;cursor:pointer;background:color-mix(in srgb,var(--border-color) 40%,transparent);border:1px solid var(--border-color);color:var(--content-text);"
                    title="Close">&times;</button>
          </div>
        </div>
        <!-- Body -->
        <div id="dp-body" style="flex:1;overflow-y:auto;color:#f8fafc;${hasSidePanelWidget ? '' : 'padding:12px 16px;'}">
          ${hasSidePanelWidget ? '' : this.renderTagGroupsHtml(devicePath)}
        </div>
      </div>
    `;

    panel.querySelector('#dp-close')?.addEventListener('click', () => {
      panel.style.display = 'none';
      this.selectedDevicePath = null;
      this.applySelectedMarker();
    });

    panel.querySelector('#dp-device-link')?.addEventListener('click', () => {
      this.openDetailDashboard(devicePath, detailDashboardId);
    });

    let sideWidget: any = null;

    const mountWidget = async () => {
      const body = panel.querySelector<HTMLElement>('#dp-body');
      if (!body || !entry) return;
      body.innerHTML = '';
      body.style.padding = '0';
      const type = this.getSidePanelWidgetType(entry.layer);
      if (!type) return;
      body.innerHTML = `<div style="padding:12px;font-size:12px;color:#cbd5e1;">Loading widget...</div>`;
      await ensureWidgetTypeLoaded(type);
      if (!body.isConnected || this.selectedDevicePath !== devicePath) return;
      if (!customElements.get(type)) {
        body.innerHTML = `<div style="padding:12px;font-size:12px;color:#fca5a5;">Unable to load widget.</div>`;
        return;
      }
      sideWidget = document.createElement(type);
      (sideWidget as HTMLElement).style.cssText = 'display:block;height:100%;color:#f8fafc;--content-text:#f8fafc;--footer-text:#cbd5e1;';
      body.replaceChildren(sideWidget);
      if (typeof sideWidget.setEditMode === 'function') sideWidget.setEditMode(false);
      if (typeof sideWidget.setConfig === 'function') {
        sideWidget.setConfig(this.configForDevice(this.getSidePanelWidgetConfig(entry.layer), devicePath, type));
      }
      // Intercept setConfig so saves from the config dialog are persisted back to the layer
      if (typeof sideWidget.setConfig === 'function' && typeof sideWidget.getConfig === 'function') {
        const origSetConfig = sideWidget.setConfig.bind(sideWidget);
        sideWidget.setConfig = (c: any) => {
          origSetConfig(c);
          entry.layer.sidePanelWidgetConfig = sideWidget.getConfig();
          this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
        };
      }
    };

    if (hasSidePanelWidget) void mountWidget();

    panel.querySelector('#dp-gear')?.addEventListener('click', () => {
      if (!entry) return;
      if (this._editMode && this.hasSidePanelWidget(entry.layer)) {
        void this.openSidePanelWidgetConfig(entry.layer);
      }
    });
  }

  // ── Legend ─────────────────────────────────────────────────────────────────

  private updateLegend(): void {
    const legend = this.querySelector<HTMLElement>('#map-legend');
    if (!legend) return;
    legend.innerHTML = this.renderLegendHtml();
    this.attachLegendListeners();
  }

  private renderLegendHtml(): string {
    const layers = this.config.layers;
    const hasTraffic = !!this.config.tomtomApiKey;

    return `
      <div style="background:rgba(20,20,20,0.85);backdrop-filter:blur(4px);border-radius:6px;border:1px solid var(--border-color);padding:6px 10px;min-width:140px;max-width:200px;font-size:11px;color:#e0e0e0;">
        <div id="legend-toggle" style="cursor:pointer;display:flex;align-items:center;justify-content:space-between;margin-bottom:${this.legendCollapsed ? '0' : '6px'};user-select:none;">
          <span style="font-weight:600;opacity:0.8;">Legend</span>
          <span style="opacity:0.5;">${this.legendCollapsed ? '▲' : '▼'}</span>
        </div>
        ${this.legendCollapsed ? '' : `
        <div id="legend-body">
          ${layers.map(l => `
            <label style="display:flex;align-items:center;gap:6px;cursor:pointer;padding:2px 0;">
              <input type="checkbox" data-layer-id="${esc(l.id)}" ${l.enabled !== false ? 'checked' : ''} style="cursor:pointer;">
              <span style="width:8px;height:8px;border-radius:50%;background:${esc(l.defaultColor ?? '#f59e0b')};flex-shrink:0;"></span>
              <span style="opacity:0.85;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${esc(l.name)}</span>
            </label>`).join('')}
          ${hasTraffic ? `
            <div style="margin-top:6px;padding-top:6px;border-top:1px solid var(--border-color);">
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;padding:2px 0;">
                <input type="checkbox" id="legend-traffic" ${this.config.showTraffic ? 'checked' : ''}>
                <span>🚦 Traffic</span>
              </label>
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;padding:2px 0;">
                <input type="checkbox" id="legend-incidents" ${this.config.showIncidents ? 'checked' : ''}>
                <span>⚠️ Incidents</span>
              </label>
            </div>` : ''}
        </div>`}
      </div>
    `;
  }

  private attachLegendListeners(): void {
    const legend = this.querySelector<HTMLElement>('#map-legend');
    if (!legend) return;

    legend.querySelector('#legend-toggle')?.addEventListener('click', () => {
      this.legendCollapsed = !this.legendCollapsed;
      this.updateLegend();
    });

    legend.querySelectorAll<HTMLInputElement>('input[data-layer-id]').forEach(cb => {
      cb.addEventListener('change', () => {
        const layerId = cb.dataset.layerId!;
        const layer = this.config.layers.find(l => l.id === layerId);
        if (layer) {
          layer.enabled = cb.checked;
          this.updateLayerVisibility(layer);
        }
      });
    });

    legend.querySelector<HTMLInputElement>('#legend-traffic')?.addEventListener('change', (e) => {
      this.config.showTraffic = (e.target as HTMLInputElement).checked;
      this.initTomTomLayers();
    });

    legend.querySelector<HTMLInputElement>('#legend-incidents')?.addEventListener('change', (e) => {
      this.config.showIncidents = (e.target as HTMLInputElement).checked;
      this.initTomTomLayers();
    });
  }

  private updateLayerVisibility(layer: LayerConfig): void {
    for (const [devicePath, entry] of this.devices) {
      if (entry.layer.id !== layer.id) continue;
      if (layer.enabled) {
        entry.marker.addTo(this.map);
        // Leaflet recreates the icon DOM on re-add, so the body is fresh/empty.
        // Clear lastIconHtml so updateDeviceMarker does a full pass and remounts
        // any embedded status-table-widget.
        entry.lastIconHtml = '';
        this.updateDeviceMarker(devicePath);
      } else {
        entry.marker.remove();
      }
    }
  }

  // ── Search ─────────────────────────────────────────────────────────────────

  private doSearch(query: string): void {
    this.searchQuery = query;
    const results: Array<{ path: string; score: number }> = [];

    for (const devicePath of this.devices.keys()) {
      const name = devicePath.split('.').pop() ?? devicePath;
      const score = fuzzyScore(name + ' ' + devicePath, query);
      if (score >= 0) {
        results.push({ path: devicePath, score });
      }
    }

    results.sort((a, b) => b.score - a.score);
    this.renderSearchResults(results.slice(0, 8));
  }

  private renderSearchResults(results: Array<{ path: string; score: number }>): void {
    const dropdown = this.querySelector<HTMLElement>('#search-dropdown');
    if (!dropdown) return;

    if (!this.searchQuery || results.length === 0) {
      dropdown.style.display = 'none';
      dropdown.innerHTML = '';
      return;
    }

    dropdown.style.display = 'block';
    dropdown.innerHTML = results.map(r => {
      const name = r.path.split('.').pop() ?? r.path;
      return `
        <div class="search-result" data-path="${esc(r.path)}"
             style="padding:6px 10px;cursor:pointer;border-bottom:1px solid var(--border-color);font-size:12px;">
          <div style="font-weight:500;">${esc(name)}</div>
          <div style="opacity:0.5;font-size:10px;font-family:'IBM Plex Mono',monospace;">${esc(r.path)}</div>
        </div>`;
    }).join('');

    dropdown.querySelectorAll<HTMLElement>('.search-result').forEach(el => {
      el.addEventListener('click', () => {
        const path = el.dataset.path!;
        const store = getMirrorStore();
        const lat = store.getNodeValue(path + '.meta.lat');
        const lon = store.getNodeValue(path + '.meta.lon');
        if (lat !== undefined && lon !== undefined) {
          this.map?.flyTo([lat, lon], 16);
        }
        dropdown.style.display = 'none';
        const input = this.querySelector<HTMLInputElement>('#search-input');
        if (input) input.value = '';
        this.searchQuery = '';
      });

      el.addEventListener('mouseenter', () => {
        el.style.background = 'color-mix(in srgb,var(--accent-color) 10%,transparent)';
      });
      el.addEventListener('mouseleave', () => {
        el.style.background = '';
      });
    });
  }

  // ── Config panel ───────────────────────────────────────────────────────────

  private renderConfigPanelHtml(): string {
    const cfg = this.config;
    const editLayer = cfg.layers.find(l => l.id === this.cfgEditLayerId) ?? null;

    const layerListHtml = cfg.layers.map((l, idx) => `
      <div style="display:flex;align-items:center;gap:6px;padding:6px;border-radius:4px;border:1px solid var(--border-color);margin-bottom:4px;background:var(--surface-tint);">
        <span style="flex:1;font-size:14px;">${esc(l.name || l.id)}</span>
        <button class="cfg-move-layer" data-id="${esc(l.id)}" data-dir="-1" title="Move layer up"
                ${idx === 0 ? 'disabled' : ''}
                style="width:26px;height:24px;display:flex;align-items:center;justify-content:center;font-size:13px;padding:0;border-radius:3px;border:1px solid var(--border-color);background:transparent;color:inherit;cursor:${idx === 0 ? 'not-allowed' : 'pointer'};opacity:${idx === 0 ? '0.35' : '1'};">
          ▲
        </button>
        <button class="cfg-move-layer" data-id="${esc(l.id)}" data-dir="1" title="Move layer down"
                ${idx === cfg.layers.length - 1 ? 'disabled' : ''}
                style="width:26px;height:24px;display:flex;align-items:center;justify-content:center;font-size:13px;padding:0;border-radius:3px;border:1px solid var(--border-color);background:transparent;color:inherit;cursor:${idx === cfg.layers.length - 1 ? 'not-allowed' : 'pointer'};opacity:${idx === cfg.layers.length - 1 ? '0.35' : '1'};">
          ▼
        </button>
        <button class="cfg-edit-layer" data-id="${esc(l.id)}"
                style="font-size:13px;padding:2px 8px;border-radius:3px;border:1px solid var(--border-color);background:transparent;color:inherit;cursor:pointer;">
          Edit
        </button>
        <button class="cfg-del-layer" data-id="${esc(l.id)}"
                style="font-size:13px;padding:2px 8px;border-radius:3px;border:1px solid rgba(239,68,68,0.35);color:#f87171;background:transparent;cursor:pointer;">
          ✕
        </button>
      </div>`).join('');

    const layerEditHtml = editLayer ? this.renderLayerEditHtml(editLayer) : '';

    const savedBoundsLabel = cfg.savedBounds
      ? `N:${cfg.savedBounds.north.toFixed(4)} S:${cfg.savedBounds.south.toFixed(4)} E:${cfg.savedBounds.east.toFixed(4)} W:${cfg.savedBounds.west.toFixed(4)}`
      : 'No bounds saved - will use organisation area';
    const flowStyleOptions = TOMTOM_FLOW_STYLES
      .map(s => `<option value="${s}" ${s === (cfg.trafficStyle || 'relative0') ? 'selected' : ''}>${s}</option>`)
      .join('');
    const incidentStyleOptions = TOMTOM_INCIDENT_STYLES
      .map(s => `<option value="${s}" ${s === (cfg.incidentStyle || 's0') ? 'selected' : ''}>${s}</option>`)
      .join('');

    return `
      <div id="map-cfg-panel" style="position:absolute;inset:0;z-index:2000;display:flex;flex-direction:column;overflow:hidden;">
        <div style="height:100%;display:flex;flex-direction:column;overflow:hidden;">

          <!-- Body -->
          <div style="flex:1;overflow-y:auto;padding:16px;">
          <div style="max-width:680px;margin:0 auto;">

            <!-- General settings -->
            <div style="margin-bottom:28px;">
              <span style="${sectionHeadStyle}">General</span>
              <div style="display:grid;grid-template-columns:1fr;gap:8px;align-items:end;margin-bottom:10px;">
                <div>
                  <label style="display:block;font-size:12px;opacity:0.6;margin-bottom:4px;">Widget heading</label>
                  <input id="cfg-heading" type="text" value="${esc(cfg.heading)}" placeholder="e.g. Site Overview"
                         style="width:100%;padding:6px 8px;font-size:14px;background:var(--input-bg);border:1px solid var(--border-color);border-radius:4px;color:inherit;box-sizing:border-box;">
                </div>
              </div>
              <div style="display:flex;align-items:center;gap:10px;margin-bottom:10px;">
                <label style="font-size:12px;opacity:0.6;white-space:nowrap;min-width:90px;">Base map opacity</label>
                <input id="cfg-base-opacity" type="range" min="0" max="1" step="0.05"
                       value="${cfg.baseOpacity ?? 1}"
                       style="flex:1;accent-color:var(--accent-color);cursor:pointer;">
                <span id="cfg-base-opacity-val" style="font-size:12px;font-family:'IBM Plex Mono',monospace;opacity:0.7;min-width:32px;text-align:right;">${Math.round((cfg.baseOpacity ?? 1) * 100)}%</span>
              </div>
              <div style="display:flex;align-items:center;gap:10px;padding:10px 12px;border-radius:4px;background:var(--surface-tint);border:1px solid var(--border-color);">
                <div style="flex:1;">
                  <div style="font-size:12px;opacity:0.6;margin-bottom:2px;">Saved map bounds</div>
                  <div id="cfg-bounds-label" style="font-size:11px;font-family:'IBM Plex Mono',monospace;opacity:0.85;">${esc(savedBoundsLabel)}</div>
                </div>
                <button id="cfg-save-bounds" style="flex-shrink:0;padding:5px 12px;font-size:13px;background:color-mix(in srgb,var(--accent-color) 15%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:4px;cursor:pointer;">
                  Save current view
                </button>
              </div>
            </div>

            <!-- TomTom layers -->
            <div style="margin-bottom:28px;">
              <button id="cfg-tomtom-toggle" type="button"
                      style="width:100%;display:flex;align-items:center;gap:8px;background:transparent;border:none;cursor:pointer;text-align:left;padding:0;margin:0 0 12px 0;">
                <span style="color:var(--accent-color);font-size:11px;line-height:1;">${this.cfgTomTomCollapsed ? '▶' : '▼'}</span>
                <span style="${sectionHeadStyle}flex:1;margin-bottom:0;">TomTom Traffic</span>
              </button>
              <div id="cfg-tomtom-body" style="display:${this.cfgTomTomCollapsed ? 'none' : 'grid'};grid-template-columns:1fr;gap:10px;">
                <div>
                  <label style="display:block;font-size:12px;opacity:0.6;margin-bottom:4px;">API key</label>
                  <input id="cfg-tomtom-key" type="password" value="${esc(cfg.tomtomApiKey ?? '')}" placeholder="TomTom API key"
                         style="width:100%;padding:6px 8px;font-size:14px;background:var(--input-bg);border:1px solid var(--border-color);border-radius:4px;color:inherit;box-sizing:border-box;">
                  <div style="margin-top:4px;font-size:11px;color:#cbd5e1;">
                    Get a key from <a href="https://developer.tomtom.com/knowledgebase/platform/articles/how-to-get-an-tomtom-api-key/" target="_blank" rel="noopener noreferrer" style="color:var(--accent-color);">TomTom Developer</a>.
                  </div>
                </div>
                <div style="display:grid;grid-template-columns:auto minmax(0,1fr);gap:10px;align-items:end;padding:10px 12px;border-radius:4px;background:var(--surface-tint);border:1px solid var(--border-color);">
                  <label style="display:flex;align-items:center;gap:6px;font-size:13px;cursor:pointer;padding-bottom:6px;">
                    <input type="checkbox" id="cfg-show-traffic" ${cfg.showTraffic ? 'checked' : ''} style="accent-color:var(--accent-color);">
                    Traffic flow
                  </label>
                  <div>
                    <label style="display:block;font-size:12px;opacity:0.6;margin-bottom:4px;">Flow style</label>
                    <select id="cfg-traffic-style" style="${fieldStyle}width:100%;">${flowStyleOptions}</select>
                  </div>
                </div>
                <div style="display:grid;grid-template-columns:auto minmax(0,1fr);gap:10px;align-items:end;padding:10px 12px;border-radius:4px;background:var(--surface-tint);border:1px solid var(--border-color);">
                  <label style="display:flex;align-items:center;gap:6px;font-size:13px;cursor:pointer;padding-bottom:6px;">
                    <input type="checkbox" id="cfg-show-incidents" ${cfg.showIncidents ? 'checked' : ''} style="accent-color:var(--accent-color);">
                    Incidents
                  </label>
                  <div>
                    <label style="display:block;font-size:12px;opacity:0.6;margin-bottom:4px;">Incident style</label>
                    <select id="cfg-incident-style" style="${fieldStyle}width:100%;">${incidentStyleOptions}</select>
                  </div>
                </div>
              </div>
            </div>

            <!-- Layer list -->
            <div style="margin-bottom:28px;">
              <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;padding-bottom:8px;border-bottom:1px solid color-mix(in srgb,var(--accent-color) 18%,var(--border-color));">
                <span style="font-size:13px;font-weight:600;letter-spacing:0.01em;color:var(--accent-color);">Device Layers <span style="font-size:11px;font-weight:400;opacity:0.65;">top to bottom</span></span>
                <button id="cfg-add-layer" style="font-size:13px;padding:3px 10px;background:color-mix(in srgb,var(--accent-color) 15%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:4px;cursor:pointer;">
                  + Add Layer
                </button>
              </div>
              <div id="cfg-layer-list">${layerListHtml}</div>
            </div>

            <!-- Back / Save actions -->
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:28px;padding:12px 14px;border-radius:6px;background:var(--surface-tint);border:1px solid var(--border-color);">
              <button id="cfg-back" style="display:flex;align-items:center;gap:6px;font-size:14px;background:transparent;border:none;color:var(--accent-color);cursor:pointer;">
                ← Back to map
              </button>
              <button id="cfg-save" style="padding:6px 20px;background:var(--accent-color);color:var(--accent-text);border:none;border-radius:4px;font-size:14px;font-weight:600;cursor:pointer;">
                Save
              </button>
            </div>

            <!-- Layer editor --> 
            ${editLayer ? `<div id="cfg-layer-editor" style="border-radius:6px;border:1px solid color-mix(in srgb,var(--accent-color) 25%,var(--border-color));background:var(--surface-tint);">
              <div style="display:flex;align-items:center;justify-content:space-between;padding:10px 14px;background:color-mix(in srgb,var(--accent-color) 8%,transparent);border-bottom:1px solid color-mix(in srgb,var(--accent-color) 18%,var(--border-color));">
                <div>
                  <span style="font-size:13px;font-weight:600;color:var(--accent-color);">Edit Layer</span>
                  <span style="font-size:13px;font-weight:400;color:var(--accent-color);opacity:0.6;"> - ${esc(editLayer.name)}</span>
                </div>
                <button id="cfg-close-layer" title="Close editor" style="background:transparent;border:none;color:var(--accent-color);opacity:0.6;cursor:pointer;font-size:18px;line-height:1;padding:0 2px;">&times;</button>
              </div>
              <div style="padding:16px;">
                ${layerEditHtml}
              </div>
            </div>` : ''}

          </div>
          </div>
        </div>
      </div>
    `;
  }

  private renderLayerEditHtml(layer: LayerConfig): string {
    // Treat legacy 'div' layers as 'icon' in the editor
    const effectiveType = (layer.itemType as string) === 'div' ? 'icon' : layer.itemType;
    const isPlugin = effectiveType === 'plugin';

    const itemTypeOpts = (['icon', 'plugin'] as const).map(t =>
      `<option value="${t}" ${effectiveType === t ? 'selected' : ''}>${t}</option>`
    ).join('');
    const zoomWidgetType = this.getZoomWidgetType(layer);
    const sidePanelWidgetType = this.getSidePanelWidgetType(layer);
    const detailDashboardId = String(layer.detailDashboardId ?? '');
    const widgetTypeOptions = (selectedType: string) => [
      '<option value="">(none)</option>',
      ...getAvailableWidgets()
        .filter(w => w.type !== 'area-map-widget')
        .map(w => `<option value="${w.type}" ${selectedType === w.type ? 'selected' : ''}>${w.icon} ${esc(w.name)}</option>`),
    ].join('');
    const dashboardOptions = () => {
      const availableDashboards = this.dashboards.filter(dashboard => !dashboard.isCategory);
      const selectedKnown = availableDashboards.some(dashboard => String(dashboard.id) === detailDashboardId);
      const uniqueDashboards = [...availableDashboards
        .reduce((byName, dashboard) => {
          const key = dashboard.name.trim().toLowerCase() || String(dashboard.id);
          const existing = byName.get(key);
          if (!existing || String(dashboard.id) === detailDashboardId) byName.set(key, dashboard);
          return byName;
        }, new Map<string, DashboardMeta>())
        .values()];
      return [
        '<option value="">(none)</option>',
        ...(!selectedKnown && detailDashboardId
          ? [`<option value="${esc(detailDashboardId)}" selected>Dashboard ${esc(detailDashboardId)}</option>`]
          : []),
        ...uniqueDashboards.map(dashboard => {
          const selected = String(dashboard.id) === detailDashboardId ? 'selected' : '';
          return `<option value="${esc(String(dashboard.id))}" ${selected}>${esc(dashboard.name)}</option>`;
        }),
      ].join('');
    };
    const zoomWidgetOptions = widgetTypeOptions(zoomWidgetType);
    const sidePanelWidgetOptions = widgetTypeOptions(sidePanelWidgetType);
    const detailDashboardOptions = dashboardOptions();
    const hasZoomConfig = !!Object.keys(this.getZoomWidgetConfig(layer)).length;
    const hasSidePanelConfig = !!Object.keys(this.getSidePanelWidgetConfig(layer)).length;
    const mapLayerPlugin = this.getMapLayerPlugin(layer);
    const mapLayerPluginNames: string[] = (window as any).XACT?.listMapLayerTypes?.() ?? [];
    const pluginTypeOptions = [
      '<option value="">(select plugin)</option>',
      ...mapLayerPluginNames.map(name => `<option value="${esc(name)}" ${layer.pluginType === name ? 'selected' : ''}>${esc((window as any).XACT?.getMapLayerType?.(name)?.name || name)}</option>`),
    ].join('');
    const hasPluginConfig = !!Object.keys(layer.pluginConfig ?? {}).length;
    const canConfigurePlugin = !!mapLayerPlugin?.getPropertySchema;

    const rulesHtml = (layer.iconRules ?? []).map((r, idx) => `
      <div class="rule-row" data-idx="${idx}" style="display:grid;grid-template-columns:1fr 80px 80px 60px 52px 34px 80px 28px;gap:4px;margin-bottom:4px;align-items:center;overflow-x:auto">
        <div style="display:flex;gap:2px;align-items:center;">
          <input class="rule-tag" type="text" value="${esc(r.tag)}" placeholder="tag path" style="${fieldStyle}flex:1;min-width:0;">
          <button class="rule-tag-browse" data-idx="${idx}" title="Browse tags"
                  style="flex-shrink:0;width:24px;height:26px;display:flex;align-items:center;justify-content:center;background:color-mix(in srgb,var(--border-color) 40%,transparent);border:1px solid var(--border-color);border-radius:3px;cursor:pointer;font-size:12px;padding:0;">✏️</button>
        </div>
        <select class="rule-cond" style="${fieldStyle}">
          ${(['eq','ne','gt','lt','gte','lte'] as RuleCond[]).map(c => `<option value="${c}" ${r.cond === c ? 'selected' : ''}>${c}</option>`).join('')}
        </select>
        <input class="rule-value" type="text" value="${esc(r.value)}" placeholder="value" style="${fieldStyle}">
        <icon-picker class="rule-glyph" value="${esc(r.glyph || 'mdi:circle')}"></icon-picker>
        <input class="rule-size" type="number" value="${r.size ?? 24}" min="8" max="96" title="Icon size (px)" style="${fieldStyle}width:100%;">
        <input type="color" class="rule-color" value="${toHexColor(r.color)}"
               style="width:34px;height:26px;padding:1px 2px;cursor:pointer;border:1px solid var(--border-color);border-radius:3px;background:var(--content-bg);">
        <select class="rule-anim" style="${fieldStyle}">
          ${(['none','pulse','shake'] as Animation[]).map(a => `<option value="${a}" ${r.animation === a ? 'selected' : ''}>${a}</option>`).join('')}
        </select>
        <button class="rule-del" data-idx="${idx}" style="background:transparent;border:none;color:#f87171;cursor:pointer;font-size:14px;">✕</button>
      </div>`).join('');

    return `
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-bottom:12px;">
        <div>
          <label style="${labelStyle}">Layer Name</label>
          <input id="le-name" type="text" value="${esc(layer.name)}" style="${fieldStyle}width:100%;">
        </div>
        <div>
          <label style="${labelStyle}">Path Pattern</label>
          <div style="display:flex;gap:4px;">
            <input id="le-pattern" type="text" value="${esc(layer.pathPattern)}" placeholder="e.g. buses.*" style="${fieldStyle}flex:1;min-width:0;">
            <button id="le-pattern-browse" title="Browse tree" style="padding:4px 8px;font-size:12px;background:color-mix(in srgb,var(--accent-color) 10%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:3px;cursor:pointer;white-space:nowrap;">&#128194;</button>
          </div>
        </div>
        <div>
          <label style="${labelStyle}">Item Type</label>
          <select id="le-item-type" style="${fieldStyle}width:100%;">${itemTypeOpts}</select>
        </div>
        ${isPlugin ? `
        <div>
          <label style="${labelStyle}">Plugin Type</label>
          <select id="le-plugin-type" style="${fieldStyle}width:100%;">${pluginTypeOptions}</select>
          <div style="display:flex;align-items:center;gap:8px;margin-top:6px;">
            <button id="le-plugin-configure" ${canConfigurePlugin ? '' : 'disabled'} style="font-size:13px;padding:2px 8px;background:color-mix(in srgb,var(--accent-color) 10%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:3px;cursor:${canConfigurePlugin ? 'pointer' : 'not-allowed'};opacity:${canConfigurePlugin ? '1' : '0.45'};">
              Configure Plugin
            </button>
            ${mapLayerPlugin
              ? `<span style="color:#d1d5db;font-size:11px;">${hasPluginConfig ? 'Config saved' : 'No config'}</span>`
              : `<span style="color:#fbbf24;font-size:11px;">Per-device renderer or unloaded layer plugin</span>`}
          </div>
        </div>` : `
        <div>
          <label style="${labelStyle}">Default Glyph</label>
          <div style="display:flex;gap:6px;align-items:center;">
            <icon-picker id="le-default-glyph" value="${esc(layer.defaultGlyph ?? 'mdi:map-marker')}"></icon-picker>
            <input id="le-default-size" type="number" value="${layer.defaultSize ?? 24}" min="8" max="96" title="Default icon size (px)" style="${fieldStyle}width:56px;">
          </div>
        </div>
        <div style="display:flex;gap:8px;align-items:flex-end;">
          <div>
            <label style="${labelStyle}">Default Color</label>
            <input type="color" id="le-default-color" value="${toHexColor(layer.defaultColor ?? '#f59e0b')}"
                   style="display:block;width:52px;height:30px;padding:1px 2px;cursor:pointer;border:1px solid var(--border-color);border-radius:3px;background:var(--content-bg);">
          </div>
          <div style="flex:1;">
            <label style="${labelStyle}">Offset X</label>
            <input id="le-offset-x" type="number" value="${layer.offsetX ?? 0}" style="${fieldStyle}width:100%;">
          </div>
          <div style="flex:1;">
            <label style="${labelStyle}">Offset Y</label>
            <input id="le-offset-y" type="number" value="${layer.offsetY ?? 0}" style="${fieldStyle}width:100%;">
          </div>
        </div>`}
      </div>

      ${!isPlugin ? `
      <div style="margin-bottom:16px;">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;padding-bottom:6px;border-bottom:1px solid color-mix(in srgb,var(--accent-color) 12%,var(--border-color));">
          <span style="${subHeadStyle}">Icon Rules <span style="font-size:14px;font-weight:400;opacity:0.7;letter-spacing:0;text-transform:none;">· used when zoomed out</span></span>
          <button id="le-add-rule" style="font-size:13px;padding:2px 8px;background:color-mix(in srgb,var(--accent-color) 10%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:3px;cursor:pointer;">+ Rule</button>
        </div>
        <div style="overflow-x:auto;padding-bottom:2px;">
          <div id="le-rules-list" style="min-width:420px;">${rulesHtml}</div>
        </div>
      </div>
      <div style="margin-bottom:12px;padding-top:16px;border-top:1px solid color-mix(in srgb,var(--accent-color) 8%,var(--border-color));">
        <div style="display:flex;gap:8px;margin-bottom:16px;">
          <div style="flex:1;">
            <label style="${labelStyle}">Zoom Threshold (icon → zoomed marker)</label>
            <input id="le-zoom-threshold" type="number" value="${layer.zoomThreshold ?? 13}" min="1" max="20" style="${fieldStyle}width:100%;">
          </div>
          <div style="flex:1;">
            <label style="${labelStyle}">Refresh Interval (ms, 0 = off)</label>
            <input id="le-refresh-interval" type="number" value="${layer.refreshInterval ?? 0}" min="0" style="${fieldStyle}width:100%;">
          </div>
        </div>
        <div id="le-section-zoom-widget">
          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">
            <span style="${subHeadStyle}">Zoomed Widget <span style="font-size:14px;font-weight:400;opacity:0.7;letter-spacing:0;text-transform:none;">· hover tooltip when zoomed out, marker when zoomed in</span></span>
            <button id="le-dw-configure" ${zoomWidgetType ? '' : 'disabled'} style="font-size:13px;padding:2px 8px;background:color-mix(in srgb,var(--accent-color) 10%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:3px;cursor:${zoomWidgetType ? 'pointer' : 'not-allowed'};opacity:${zoomWidgetType ? '1' : '0.45'};">
              Configure Widget
            </button>
          </div>
          <div style="display:grid;grid-template-columns:minmax(0,1fr) 96px auto;gap:8px;align-items:end;">
            <div>
              <label style="${labelStyle}">Widget Type</label>
              <select id="le-zoom-widget-type" style="${fieldStyle}width:100%;">${zoomWidgetOptions}</select>
            </div>
            <div>
              <label style="${labelStyle}">Width (px)</label>
              <input id="le-dw-width" type="number" value="${layer.divWidgetWidth ?? 280}" min="160" max="600" style="${fieldStyle}width:100%;">
            </div>
            ${hasZoomConfig
              ? `<span style="color:#d1d5db;font-size:11px;padding-bottom:6px;">Config saved</span>`
              : `<span style="color:#d1d5db;font-size:11px;padding-bottom:6px;">No config</span>`}
          </div>
        </div>
      </div>
      <div style="margin-top:16px;padding-top:16px;border-top:1px solid color-mix(in srgb,var(--accent-color) 8%,var(--border-color));">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">
          <span style="${subHeadStyle}">Side Dashboard Widget <span style="font-size:14px;font-weight:400;opacity:0.7;letter-spacing:0;text-transform:none;">· shown when a device is clicked</span></span>
          <button id="le-side-panel-configure" ${sidePanelWidgetType ? '' : 'disabled'} style="font-size:13px;padding:2px 8px;background:color-mix(in srgb,var(--accent-color) 10%,transparent);color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 30%,transparent);border-radius:3px;cursor:${sidePanelWidgetType ? 'pointer' : 'not-allowed'};opacity:${sidePanelWidgetType ? '1' : '0.45'};">
            Configure Widget
          </button>
        </div>
        <div style="display:grid;grid-template-columns:minmax(0,1fr);gap:8px;align-items:end;margin-bottom:10px;">
          <div>
            <label style="${labelStyle}">Detail Dashboard</label>
            <select id="le-detail-dashboard" style="${fieldStyle}width:100%;">${detailDashboardOptions}</select>
          </div>
        </div>
        <div style="display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;align-items:end;">
          <div>
            <label style="${labelStyle}">Widget Type</label>
            <select id="le-side-panel-widget-type" style="${fieldStyle}width:100%;">${sidePanelWidgetOptions}</select>
          </div>
          ${hasSidePanelConfig
            ? `<span style="color:#d1d5db;font-size:11px;padding-bottom:6px;">Config saved</span>`
            : `<span style="color:#d1d5db;font-size:11px;padding-bottom:6px;">No config</span>`}
        </div>
      </div>` : ''}
    `;
  }

  private setCardActionsVisible(visible: boolean): void {
    const actions = this.closest('widget-card')?.querySelector<HTMLElement>('.wc-actions');
    if (actions) actions.style.display = visible ? 'flex' : 'none';
  }

  private async openConfigPanel(): Promise<void> {
    this.cfgOpen = true;
    this.cfgEditLayerId = null;
    this.setCardActionsVisible(false);
    await this.loadDashboards();
    const overlay = document.createElement('div');
    overlay.id = 'map-cfg-overlay';
    overlay.style.cssText = `position:fixed;inset:0;z-index:${MAP_CONFIG_OVERLAY_Z_INDEX};background:var(--widget-bg);color:var(--content-text);overflow:hidden;font-size:14px;`;
    document.body.appendChild(overlay);
    overlay.innerHTML = this.renderConfigPanelHtml();
    this.attachConfigListeners(overlay);
    // Initialise html-editor values for div templates
    this.initDivEditors(overlay);
  }

  private initDivEditors(overlay: HTMLElement): void {
    const editLayer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
    if (!editLayer || (editLayer.itemType === 'plugin')) return;

    const ed = overlay.querySelector<HtmlEditor>('#le-div-template');
    if (ed) {
      const existing = editLayer.divTemplate ?? editLayer.divTemplateIn ?? '';
      const value = existing || DEFAULT_DIV_TEMPLATE;
      // CodeMirror needs a tick to attach
      requestAnimationFrame(() => {
        ed.setValue(value);
        ed.refresh();
      });
    }
  }

  private attachConfigListeners(overlay: HTMLElement): void {
    overlay.querySelector('#cfg-back')?.addEventListener('click', () => {
      overlay.remove();
      this.cfgOpen = false;
      this.cfgEditLayerId = null;
      this.setCardActionsVisible(true);
    });

    overlay.querySelector<HTMLInputElement>('#cfg-base-opacity')?.addEventListener('input', (e) => {
      const val = parseFloat((e.target as HTMLInputElement).value);
      const label = overlay.querySelector<HTMLElement>('#cfg-base-opacity-val');
      if (label) label.textContent = Math.round(val * 100) + '%';
      if (this.baseLayer) this.baseLayer.setOpacity(val);
    });

    overlay.querySelector('#cfg-tomtom-toggle')?.addEventListener('click', () => {
      this.cfgTomTomCollapsed = !this.cfgTomTomCollapsed;
      this.collectConfigFromPanel(overlay);
      overlay.innerHTML = this.renderConfigPanelHtml();
      this.attachConfigListeners(overlay);
      this.initDivEditors(overlay);
    });

    overlay.querySelector('#cfg-save-bounds')?.addEventListener('click', () => {
      if (!this.map) return;
      const b = this.map.getBounds();
      this.config.savedBounds = {
        north: b.getNorth(), south: b.getSouth(),
        east: b.getEast(),  west: b.getWest(),
      };
      const label = overlay.querySelector<HTMLElement>('#cfg-bounds-label');
      const btn = overlay.querySelector<HTMLElement>('#cfg-save-bounds');
      if (label) {
        const { north, south, east, west } = this.config.savedBounds;
        label.textContent = `N:${north.toFixed(4)} S:${south.toFixed(4)} E:${east.toFixed(4)} W:${west.toFixed(4)}`;
      }
      if (btn) { btn.textContent = 'Saved ✓'; setTimeout(() => { btn.textContent = 'Save current view'; }, 2000); }
    });

    overlay.querySelector('#cfg-save')?.addEventListener('click', () => {
      this.collectConfigFromPanel(overlay);
      overlay.remove();
      this.cfgOpen = false;
      this.cfgEditLayerId = null;
      this.setCardActionsVisible(true);
      // Apply changes to live map
      this.initTomTomLayers();
      this.refreshLayers().then(() => this.updateLegend());
      this.updateCardTitle();
      // Notify dashboard-container to persist the new config (no rerender triggered)
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
    });

    overlay.querySelector('#cfg-add-layer')?.addEventListener('click', () => {
      const newLayer: LayerConfig = {
        id: genId(),
        name: 'New Layer',
        pathPattern: '',
        enabled: true,
        itemType: 'icon',
        iconRules: [
          { tag: 'meta.online', cond: 'ne', value: 'true', glyph: '🔴', color: '#ef4444', animation: 'pulse' },
        ],
        defaultGlyph: '📍',
        defaultColor: '#f59e0b',
      };
      this.config.layers.push(newLayer);
      this.cfgEditLayerId = newLayer.id;
      overlay.innerHTML = this.renderConfigPanelHtml();
      this.attachConfigListeners(overlay);
      this.initDivEditors(overlay);
    });

    overlay.querySelector('#cfg-close-layer')?.addEventListener('click', () => {
      this.collectLayerFromPanel(overlay);
      this.cfgEditLayerId = null;
      overlay.innerHTML = this.renderConfigPanelHtml();
      this.attachConfigListeners(overlay);
    });

    overlay.querySelectorAll<HTMLElement>('.cfg-edit-layer').forEach(btn => {
      btn.addEventListener('click', () => {
        // Save current layer edits before switching
        this.collectLayerFromPanel(overlay);
        this.cfgEditLayerId = btn.dataset.id!;
        overlay.innerHTML = this.renderConfigPanelHtml();
        this.attachConfigListeners(overlay);
        this.initDivEditors(overlay);
      });
    });

    overlay.querySelectorAll<HTMLButtonElement>('.cfg-move-layer').forEach(btn => {
      btn.addEventListener('click', () => {
        if (btn.disabled) return;
        this.collectLayerFromPanel(overlay);
        const id = btn.dataset.id!;
        const dir = parseInt(btn.dataset.dir ?? '0', 10);
        this.moveConfigLayer(id, dir);
        overlay.innerHTML = this.renderConfigPanelHtml();
        this.attachConfigListeners(overlay);
        this.initDivEditors(overlay);
      });
    });

    overlay.querySelectorAll<HTMLElement>('.cfg-del-layer').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = btn.dataset.id!;
        this.config.layers = this.config.layers.filter(l => l.id !== id);
        if (this.cfgEditLayerId === id) this.cfgEditLayerId = null;
        overlay.innerHTML = this.renderConfigPanelHtml();
        this.attachConfigListeners(overlay);
      });
    });

    overlay.querySelector<HTMLSelectElement>('#le-zoom-widget-type')?.addEventListener('change', () => {
      this.collectLayerFromPanel(overlay);
      overlay.innerHTML = this.renderConfigPanelHtml();
      this.attachConfigListeners(overlay);
      this.initDivEditors(overlay);
    });

    overlay.querySelector<HTMLSelectElement>('#le-side-panel-widget-type')?.addEventListener('change', () => {
      this.collectLayerFromPanel(overlay);
      overlay.innerHTML = this.renderConfigPanelHtml();
      this.attachConfigListeners(overlay);
      this.initDivEditors(overlay);
    });

    // Layer item-type change → re-render editor section
    overlay.querySelector<HTMLSelectElement>('#le-item-type')?.addEventListener('change', (e) => {
      const newType = (e.target as HTMLSelectElement).value as LayerConfig['itemType'];
      const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
      if (layer) {
        this.collectLayerFromPanel(overlay);
        layer.itemType = newType;
        overlay.innerHTML = this.renderConfigPanelHtml();
        this.attachConfigListeners(overlay);
        this.initDivEditors(overlay);
      }
    });

    // Path Pattern browse button - open full tree, set input to selected path + '.*'
    overlay.querySelector<HTMLElement>('#le-pattern-browse')?.addEventListener('click', () => {
      const input = overlay.querySelector<HTMLInputElement>('#le-pattern');
      if (!input) return;
      const expandTo = (input.value ?? '').trim().replace(/\.\*.*$/, '');
      getTreeBrowserDialog().open('', 'Select Path Pattern', (selectedPath) => {
        input.value = selectedPath + '.*';
      }, /* includeLeaves= */ false, expandTo);
    });

    // Add icon rule
    overlay.querySelector('#le-add-rule')?.addEventListener('click', () => {
      const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
      if (layer) {
        this.collectLayerFromPanel(overlay);
        (layer.iconRules ??= []).push({ tag: '', cond: 'eq', value: '', glyph: '🔴', size: 24, color: '#ef4444', animation: 'none' });
        overlay.innerHTML = this.renderConfigPanelHtml();
        this.attachConfigListeners(overlay);
      }
    });

    // Delete icon rule
    overlay.querySelectorAll<HTMLElement>('.rule-del').forEach(btn => {
      btn.addEventListener('click', () => {
        const idx = parseInt(btn.dataset.idx!);
        const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
        if (layer?.iconRules) {
          this.collectLayerFromPanel(overlay);
          layer.iconRules.splice(idx, 1);
          overlay.innerHTML = this.renderConfigPanelHtml();
          this.attachConfigListeners(overlay);
        }
      });
    });

    // Tag browse buttons - open tree browser rooted at an example device from the pattern
    overlay.querySelectorAll<HTMLElement>('.rule-tag-browse').forEach(btn => {
      btn.addEventListener('click', () => {
        const idx = parseInt(btn.dataset.idx!);
        const row = overlay.querySelector<HTMLElement>(`.rule-row[data-idx="${idx}"]`);
        const input = row?.querySelector<HTMLInputElement>('.rule-tag');
        if (!input) return;

        const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
        const devicePaths = layer ? this.resolvePattern(layer.pathPattern) : [];
        const exampleDevice = devicePaths[0] ?? '';

        getTreeBrowserDialog().open(exampleDevice, 'Select Tag', (selectedPath) => {
          // Return relative path by stripping the example device prefix
          // Tree-browser returns org-relative paths; convert device path to relative for comparison
          const relDevice = getMirrorStore().toRelative(exampleDevice);
          const relPath = relDevice && selectedPath.startsWith(relDevice + '.')
            ? selectedPath.substring(relDevice.length + 1)
            : selectedPath;
          input.value = relPath;
        }, /* includeLeaves= */ true);
      });
    });

    // Zoom widget configure button
    overlay.querySelector<HTMLElement>('#le-dw-configure')?.addEventListener('click', () => {
      const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
      if (!layer) return;
      this.collectLayerFromPanel(overlay);
      void this.openZoomWidgetConfig(layer);
      // Re-render overlay when config dialog closes to update "Config saved ✓" status
      const onClose = (e: Event) => {
        const target = e.target as HTMLElement;
        if (target.parentElement === document.body) {
          document.body.removeEventListener('widget-config-close', onClose, true);
          overlay.innerHTML = this.renderConfigPanelHtml();
          this.attachConfigListeners(overlay);
          this.initDivEditors(overlay);
        }
      };
      document.body.addEventListener('widget-config-close', onClose, true);
    });

    overlay.querySelector<HTMLElement>('#le-side-panel-configure')?.addEventListener('click', () => {
      const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
      if (!layer) return;
      this.collectLayerFromPanel(overlay);
      void this.openSidePanelWidgetConfig(layer);
      const onClose = (e: Event) => {
        const target = e.target as HTMLElement;
        if (target.parentElement === document.body) {
          document.body.removeEventListener('widget-config-close', onClose, true);
          overlay.innerHTML = this.renderConfigPanelHtml();
          this.attachConfigListeners(overlay);
          this.initDivEditors(overlay);
        }
      };
      document.body.addEventListener('widget-config-close', onClose, true);
    });

    overlay.querySelector<HTMLElement>('#le-plugin-configure')?.addEventListener('click', () => {
      const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
      if (!layer) return;
      this.collectLayerFromPanel(overlay);
      this.openMapLayerPluginConfig(layer);
      const onClose = (e: Event) => {
        const target = e.target as HTMLElement;
        if (target.parentElement === document.body) {
          document.body.removeEventListener('widget-config-close', onClose, true);
          overlay.innerHTML = this.renderConfigPanelHtml();
          this.attachConfigListeners(overlay);
          this.initDivEditors(overlay);
        }
      };
      document.body.addEventListener('widget-config-close', onClose, true);
    });

  }

  private collectConfigFromPanel(overlay: HTMLElement): void {
    this.config.heading = (overlay.querySelector<HTMLInputElement>('#cfg-heading')?.value ?? '').trim();
    this.config.baseOpacity = parseFloat(overlay.querySelector<HTMLInputElement>('#cfg-base-opacity')?.value ?? '1');
    this.config.tomtomApiKey = (overlay.querySelector<HTMLInputElement>('#cfg-tomtom-key')?.value ?? '').trim();
    this.config.showTraffic = overlay.querySelector<HTMLInputElement>('#cfg-show-traffic')?.checked ?? false;
    this.config.trafficStyle = overlay.querySelector<HTMLSelectElement>('#cfg-traffic-style')?.value ?? 'relative0';
    this.config.showIncidents = overlay.querySelector<HTMLInputElement>('#cfg-show-incidents')?.checked ?? false;
    this.config.incidentStyle = overlay.querySelector<HTMLSelectElement>('#cfg-incident-style')?.value ?? 's0';
    this.collectLayerFromPanel(overlay);
  }

  private moveConfigLayer(id: string, direction: number): void {
    const from = this.config.layers.findIndex(layer => layer.id === id);
    if (from === -1) return;
    const to = from + direction;
    if (to < 0 || to >= this.config.layers.length) return;
    const [layer] = this.config.layers.splice(from, 1);
    this.config.layers.splice(to, 0, layer);
  }

  private collectLayerFromPanel(overlay: HTMLElement): void {
    const layer = this.config.layers.find(l => l.id === this.cfgEditLayerId);
    if (!layer) return;

    layer.name = overlay.querySelector<HTMLInputElement>('#le-name')?.value ?? layer.name;
    layer.pathPattern = overlay.querySelector<HTMLInputElement>('#le-pattern')?.value ?? layer.pathPattern;
    // Migrate legacy 'div' type to 'icon' on next save
    const selectedType = overlay.querySelector<HTMLSelectElement>('#le-item-type')?.value ?? layer.itemType;
    layer.itemType = (selectedType === 'div' ? 'icon' : selectedType) as LayerConfig['itemType'];
    const previousPluginType = layer.pluginType ?? '';
    layer.pluginType = overlay.querySelector<HTMLSelectElement>('#le-plugin-type')?.value ?? layer.pluginType;
    if ((layer.pluginType ?? '') !== previousPluginType) {
      layer.pluginConfig = {};
    }
    layer.defaultGlyph = (overlay.querySelector('#le-default-glyph') as any)?.value ?? layer.defaultGlyph;
    layer.defaultColor = overlay.querySelector<HTMLInputElement>('#le-default-color')?.value ?? layer.defaultColor;
    layer.defaultSize = parseInt(overlay.querySelector<HTMLInputElement>('#le-default-size')?.value ?? '24') || 24;
    layer.offsetX = parseFloat(overlay.querySelector<HTMLInputElement>('#le-offset-x')?.value ?? '0') || 0;
    layer.offsetY = parseFloat(overlay.querySelector<HTMLInputElement>('#le-offset-y')?.value ?? '0') || 0;

    if (layer.itemType === 'icon') {
      layer.zoomThreshold = parseInt(overlay.querySelector<HTMLInputElement>('#le-zoom-threshold')?.value ?? '13') || 13;
      layer.refreshInterval = parseInt(overlay.querySelector<HTMLInputElement>('#le-refresh-interval')?.value ?? '0') || 0;
      layer.divWidgetWidth = parseInt(overlay.querySelector<HTMLInputElement>('#le-dw-width')?.value ?? '280') || 280;
      const previousZoomWidgetType = this.getZoomWidgetType(layer);
      const selectedZoomWidgetType = overlay.querySelector<HTMLSelectElement>('#le-zoom-widget-type')?.value ?? previousZoomWidgetType;
      if (selectedZoomWidgetType) {
        layer.zoomedMode = 'widget';
        layer.zoomWidgetType = selectedZoomWidgetType;
        layer.divTemplate = '';
        if (previousZoomWidgetType !== selectedZoomWidgetType) {
          layer.zoomWidgetConfig = {};
        } else {
          layer.zoomWidgetConfig = this.getZoomWidgetConfig(layer);
        }
        layer.divWidgetConfig = selectedZoomWidgetType === 'status-table-widget' ? layer.zoomWidgetConfig : undefined;
      } else {
        layer.zoomedMode = 'widget';
        layer.zoomWidgetType = undefined;
        layer.zoomWidgetConfig = undefined;
        layer.divWidgetConfig = undefined;
        layer.divTemplate = '';
      }
      const previousSidePanelWidgetType = this.getSidePanelWidgetType(layer);
      const selectedSidePanelWidgetType = overlay.querySelector<HTMLSelectElement>('#le-side-panel-widget-type')?.value ?? previousSidePanelWidgetType;
      const selectedDetailDashboardId = overlay.querySelector<HTMLSelectElement>('#le-detail-dashboard')?.value ?? layer.detailDashboardId ?? '';
      layer.detailDashboardId = selectedDetailDashboardId ? selectedDetailDashboardId : undefined;
      if (selectedSidePanelWidgetType) {
        layer.sidePanelWidgetType = selectedSidePanelWidgetType;
        if (previousSidePanelWidgetType !== selectedSidePanelWidgetType) {
          layer.sidePanelWidgetConfig = {};
        } else {
          layer.sidePanelWidgetConfig = this.getSidePanelWidgetConfig(layer);
        }
      } else {
        layer.sidePanelWidgetType = undefined;
        layer.sidePanelWidgetConfig = undefined;
      }
      const rows = overlay.querySelectorAll<HTMLElement>('.rule-row');
      layer.iconRules = Array.from(rows).map(row => ({
        tag: row.querySelector<HTMLInputElement>('.rule-tag')?.value ?? '',
        cond: (row.querySelector<HTMLSelectElement>('.rule-cond')?.value ?? 'eq') as RuleCond,
        value: row.querySelector<HTMLInputElement>('.rule-value')?.value ?? '',
        glyph: (row.querySelector('.rule-glyph') as any)?.value ?? 'mdi:circle',
        size: parseInt(row.querySelector<HTMLInputElement>('.rule-size')?.value ?? '24') || 24,
        color: row.querySelector<HTMLInputElement>('.rule-color')?.value ?? '#f59e0b',
        animation: (row.querySelector<HTMLSelectElement>('.rule-anim')?.value ?? 'none') as Animation,
      }));
    }
  }

  // ── Render ─────────────────────────────────────────────────────────────────

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (!card) return;
    const title = (this.config.heading || '').trim();
    if (typeof card.setTitle === 'function') card.setTitle(title);
  }

  protected render(): void {
    this.innerHTML = `
      <div id="map-root" style="position:relative;width:100%;height:100%;overflow:hidden;">
        <div id="xact-map" style="position:absolute;top:0;left:0;width:100%;height:100%;"></div>

        <!-- Legend (Leaflet will position this via CSS) -->
        ${this.config.showLegend ? `<div id="map-legend" style="position:absolute;bottom:28px;left:10px;z-index:1000;"></div>` : ''}

        <!-- Search -->
        ${this.config.showSearch ? `
        <div id="map-search" style="position:absolute;top:10px;left:50px;z-index:1000;width:240px;">
          <input id="search-input" type="text" placeholder="Search devices…"
                 style="width:100%;padding:6px 10px;font-size:12px;border-radius:20px;border:1px solid var(--border-color);background:rgba(20,20,20,0.85);backdrop-filter:blur(4px);color:#e0e0e0;box-sizing:border-box;outline:none;">
          <div id="search-dropdown"
               style="display:none;position:absolute;top:calc(100% + 4px);left:0;right:0;background:rgba(20,20,20,0.95);border:1px solid var(--border-color);border-radius:6px;backdrop-filter:blur(4px);max-height:280px;overflow-y:auto;z-index:1001;color:#e0e0e0;"></div>
        </div>` : ''}

        <!-- Device detail panel -->
        <div id="device-panel"
             style="display:none;position:absolute;top:0;right:0;width:360px;height:100%;background:rgba(20,20,20,0.95);backdrop-filter:blur(8px);border-left:1px solid var(--border-color);z-index:1002;overflow:hidden;flex-direction:column;color:#e0e0e0;">
        </div>

        <!-- Zoom indicator -->
        <div id="map-zoom"
             style="position:absolute;bottom:8px;right:8px;z-index:1000;padding:2px 7px;border-radius:10px;background:rgba(20,20,20,0.75);backdrop-filter:blur(4px);border:1px solid var(--border-color);color:#e0e0e0;font-size:11px;font-variant-numeric:tabular-nums;pointer-events:none;"></div>

        <!-- Layers button (visible only in edit mode) -->
        <button id="map-layers-btn"
                style="position:absolute;top:10px;right:10px;z-index:1001;height:28px;padding:0 10px;border-radius:14px;background:rgba(20,20,20,0.85);backdrop-filter:blur(4px);border:1px solid var(--border-color);color:#e0e0e0;cursor:pointer;font-size:11px;display:none;align-items:center;gap:4px;"
                title="Manage device layers">
          ⚙ Layers
        </button>

      </div>
    `;
  }

  // ── Event listeners ────────────────────────────────────────────────────────

  protected attachEventListeners(): void {
    this.querySelector('#map-layers-btn')?.addEventListener('click', () => {
      if (!this.cfgOpen) this.openConfigPanel();
    });

    const searchInput = this.querySelector<HTMLInputElement>('#search-input');
    searchInput?.addEventListener('input', () => {
      this.doSearch(searchInput.value.trim());
    });

    searchInput?.addEventListener('blur', () => {
      setTimeout(() => {
        const dropdown = this.querySelector<HTMLElement>('#search-dropdown');
        if (dropdown) dropdown.style.display = 'none';
      }, 200);
    });

    searchInput?.addEventListener('focus', () => {
      if (this.searchQuery) {
        this.doSearch(this.searchQuery);
      }
    });
  }

  protected detachEventListeners(): void { /* innerHTML replacement handles DOM cleanup */ }
}

customElements.define('area-map-widget', AreaMapWidget);
