/**
 * Plugin Loader - discovers and loads widget plugins from the XACT server.
 *
 * Before any plugin script is executed, this module sets up the `window.XACT`
 * bridge so plugins can register themselves without importing internal modules.
 *
 * Plugin scripts call:
 *   window.XACT.registerWidget(meta, MyElementClass)
 *   const store = window.XACT.getMirrorStore()
 */

import { getMirrorStore, type MirrorStore } from '../store/store';
import { registerWidgetType, type WidgetTypeMeta } from '../dashboards/widgets/widget-registry';
import { themeManager, type ThemeDefinition, type WidgetDecorationDefinition } from '../themes/theme-manager';
import { getAuthHeaders } from '../auth';

const BASE_URL = '/xact';

// ── Types exported for plugin authors via JSDoc ───────────────────────────────

/**
 * A plugin-supplied renderer for a custom map item type.
 * @param devicePath - Dot-separated RTDB path to the device node
 * @param store      - MirrorStore singleton for reading tag values
 * @param L          - Leaflet library reference
 * @returns A Leaflet layer (e.g. L.marker, L.circleMarker) or null to skip
 */
export type MapItemRenderer = (devicePath: string, store: MirrorStore, L: any) => any | null;

// Module-level registry for custom map item types
const mapItemRenderers = new Map<string, MapItemRenderer>();

export interface MapLayerPluginContext {
  map: any;
  L: any;
  store: MirrorStore;
  layer: any;
  config: Record<string, any>;
  getBounds(): { north: number; south: number; east: number; west: number };
  resolveDeviceTag(devicePath: string, tagPath: string): string;
  requestSave(config: Record<string, any>): void;
}

export interface MapLayerPluginInstance {
  updateDevices(devicePaths: string[]): void;
  setConfig?(config: Record<string, any>): void;
  remove(): void;
}

export interface MapLayerPlugin {
  name?: string;
  defaultConfig?: Record<string, any>;
  getPropertySchema?(): Array<Record<string, any>>;
  create(context: MapLayerPluginContext): MapLayerPluginInstance;
}

const mapLayerPlugins = new Map<string, MapLayerPlugin>();

/**
 * Bridge exposed on window.XACT for use by plugin scripts.
 * Plugin scripts (plain JS) access this after the loader has initialised it.
 */
export interface XACTBridge {
  /**
   * Register a widget with XACT.
   * Calls customElements.define() and adds the widget to the 'Custom' category
   * in the dashboard editor toolbar.
   *
   * @param meta  - Widget metadata (type, name, icon, defaultW, defaultH, …)
   * @param klass - The HTMLElement subclass implementing the widget
   *
   * The class may expose:
   *   static getPropertySchema(): Array<{name, type, label, description?, default?}>
   *     Supported types: 'string' | 'number' | 'boolean'
   *     When present, the gear icon appears on the widget card and the
   *     built-in WidgetPropertiesDialog is shown automatically.
   *
   *   setConfig(config: Record<string, any>): void
   *     Called with saved config when the widget is placed or the dashboard loads.
   *
   *   rerender(): void   (optional)
   *     Called after setConfig() when properties are updated live.
   */
  registerWidget(meta: Omit<WidgetTypeMeta, 'category'>, klass: CustomElementConstructor): void;

  /**
   * Returns the MirrorStore singleton - the only interface for NATS/RTDB data.
   * Call store.subscribeTagReference(path, cb) for picker-style paths such as
   * tag.value:description, or store.subscribe(path, cb) for raw tag values only.
   */
  getMirrorStore: typeof getMirrorStore;

  /**
   * Register a custom map item renderer for use in Area Map widget layers.
   * @param name     - Unique type name (matches pluginType in LayerConfig)
   * @param renderer - Function that returns a Leaflet layer for a device
   */
  registerMapItemType(name: string, renderer: MapItemRenderer): void;

  /**
   * Look up a registered map item renderer by name.
   */
  getMapItemType(name: string): MapItemRenderer | undefined;

  /**
   * Register a layer-level map plugin. Unlike map item renderers, these receive
   * the full matched device set and manage their own aggregate Leaflet layer.
   */
  registerMapLayerType(name: string, plugin: MapLayerPlugin): void;

  /**
   * Look up a registered layer-level map plugin by name.
   */
  getMapLayerType(name: string): MapLayerPlugin | undefined;

  /**
   * List registered layer-level map plugin names.
   */
  listMapLayerTypes(): string[];

  /**
   * Register a theme plugin.
   * Injects the supplied CSS into the document and makes the theme available
   * in the Preferences dialog.  The CSS should contain a single
   * `[data-theme="<id>"] { … }` block defining all XACT CSS variables.
   *
   * @param definition - { id, name, preview } - id must be unique across all themes
   * @param cssText    - Full CSS text for the theme
   */
  registerTheme(definition: ThemeDefinition, cssText: string): void;

  /**
   * Register a widget decoration plugin.
   * Injects CSS for widget frame appearance independent of theme colours.
   * The CSS should target `[data-widget-decoration="<id>"]`.
   *
   * @param definition - { id, name, description } - id must be unique
   * @param cssText    - CSS text for the decoration variables/rules
   */
  registerWidgetDecoration(definition: WidgetDecorationDefinition, cssText: string): void;
}

declare global {
  interface Window {
    XACT: XACTBridge;
  }
}

// ── Bridge setup ──────────────────────────────────────────────────────────────

function setupBridge(): void {
  window.XACT = {
    registerWidget(meta, klass) {
      if (!customElements.get(meta.type)) {
        customElements.define(meta.type, klass);
      }
      // Force category to 'Custom' regardless of what the plugin specifies
      registerWidgetType({ ...meta, category: 'Custom' });
    },
    getMirrorStore,
    registerMapItemType(name, renderer) {
      mapItemRenderers.set(name, renderer);
    },
    getMapItemType(name) {
      return mapItemRenderers.get(name);
    },
    registerMapLayerType(name, plugin) {
      mapLayerPlugins.set(name, plugin);
    },
    getMapLayerType(name) {
      return mapLayerPlugins.get(name);
    },
    listMapLayerTypes() {
      return [...mapLayerPlugins.keys()];
    },
    registerTheme(definition, cssText) {
      themeManager.registerTheme(definition, cssText);
    },
    registerWidgetDecoration(definition, cssText) {
      themeManager.registerWidgetDecoration(definition, cssText);
    },
  };
}

// ── Plugin discovery ──────────────────────────────────────────────────────────

interface PluginDescriptor {
  name: string;
  url: string;
}

async function fetchPluginList(endpoint: string): Promise<PluginDescriptor[]> {
  try {
    const res = await fetch(`${BASE_URL}${endpoint}`, {
      headers: getAuthHeaders(),
    });
    if (!res.ok) return [];
    return res.json();
  } catch {
    return [];
  }
}

// ── Script loading ────────────────────────────────────────────────────────────

function loadScript(url: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = url;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`Failed to load plugin script: ${url}`));
    document.head.appendChild(script);
  });
}

async function loadPluginGroup(plugins: PluginDescriptor[], label: string): Promise<void> {
  if (plugins.length === 0) return;

  const results = await Promise.allSettled(
    plugins.map(p => loadScript(`${BASE_URL}${p.url}`))
  );

  for (let i = 0; i < results.length; i++) {
    const r = results[i];
    if (r.status === 'rejected') {
      console.warn(`XACT ${label} plugins: failed to load "${plugins[i].name}":`, r.reason);
    }
  }
}

// ── Public API ────────────────────────────────────────────────────────────────

/**
 * Initialise the XACT bridge and load all plugins (widgets + themes) discovered
 * on the server.  Call this after authentication and permission loading.
 */
export async function initPlugins(): Promise<void> {
  setupBridge();

  const [widgetPlugins, mapLayerPluginList, themePlugins] = await Promise.all([
    fetchPluginList('/api/v1/plugins/widgets'),
    fetchPluginList('/api/v1/plugins/map-layer'),
    fetchPluginList('/api/v1/plugins/themes'),
  ]);

  await Promise.all([
    loadPluginGroup(widgetPlugins, 'widget'),
    loadPluginGroup(mapLayerPluginList, 'map-layer'),
    loadPluginGroup(themePlugins, 'theme'),
  ]);
}
