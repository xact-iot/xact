/**
 * device-list-widget - Compact real-time device inventory table.
 *
 * Shows devices grouped by configured parent nodes (tabs).
 * Each tab has its own independent column definitions.
 * Features: sorting, paging, CSV export, live RTDB subscriptions,
 *           fuzzy cross-tab device search with autocomplete.
 *
 * All colours reference theme CSS custom properties - none are hard-coded.
 */

import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { getMirrorStore } from '../../store/store';
import { getCurrentUser } from '../../auth';
import { listDashboards, type DashboardMeta } from '../../api';

// ── Widget registration ────────────────────────────────────────────────────────

registerWidgetType({
  type: 'device-list-widget',
  name: 'Device List',
  icon: '📋',
  category: 'General',
  defaultW: 16,
  defaultH: 20,
  minW: 10,
  minH: 10,
});

// ── Types ──────────────────────────────────────────────────────────────────────

/** A single configurable column definition. */
interface ColumnDef {
  header: string;
  /** Tag path relative to the device node, e.g. "meta.online" or "sign.message". */
  tagPath: string;
  /** How to display the raw tag value. */
  formatter: 'text' | 'number' | 'okfail' | 'cross';
  /** Optional CSS width for the column, e.g. "8rem" or "120px". Empty = auto. */
  width?: string;
}

/** Default columns applied when a tab has no columns configured yet. */
const DEFAULT_COLUMNS: ColumnDef[] = [
  { header: 'Name',        tagPath: 'meta.name',              formatter: 'text'   },
  { header: 'Description', tagPath: 'meta.description',        formatter: 'text'   },
  { header: 'Subtype',     tagPath: 'meta.deviceSubtype',      formatter: 'text'   },
  { header: 'Online',      tagPath: 'meta.online',             formatter: 'okfail' },
  { header: 'In Alarm',    tagPath: 'meta.commonAlarmPresent', formatter: 'cross'  },
];

interface DeviceRow {
  path: string;
  /** Tag values keyed by column tagPath string. */
  values: Record<string, any>;
}

interface Config {
  /** Widget card header text. Blank hides the card title bar in view mode. */
  headerText: string;
  /** Per-tab column definitions, keyed by parentNode path. */
  columns: Record<string, ColumnDef[]>;
  /** Each path is a parent node; its direct children become device rows for that tab. */
  parentNodes: string[];
  showPaging: boolean;
  pageSize: number;
  showXlsxExport: boolean;
  /** Dashboard id opened when any device row is clicked. */
  clickDashboardId: string;
  /** JSON map of parentNode path to dashboard name/id opened on row click. */
  clickDashboards: string;
}

/** Entry in the cross-tab device index, used for search. */
interface GlobalDevice {
  tabPath: string;
  tabLabel: string;
  devicePath: string;
  name: string;
}

/** A fuzzy-matched suggestion, ready to render. */
interface Suggestion extends GlobalDevice {
  score: number;
  ranges: Array<[number, number]>;
}

// ── Widget ─────────────────────────────────────────────────────────────────────

export class DeviceListWidget extends BaseComponent {
  private config: Config = {
    headerText: 'Device List',
    columns: {},
    parentNodes: [],
    showPaging: false,
    pageSize: 20,
    showXlsxExport: false,
    clickDashboardId: '',
    clickDashboards: '{}',
  };

  private static dashboardCache: DashboardMeta[] = [];
  private static dashboardLoadPromise: Promise<DashboardMeta[]> | null = null;

  private orgName = '';
  private tabs: Array<{ path: string; label: string }> = [];
  private selectedPath = '';
  private devices: DeviceRow[] = [];
  private devicePathSet: Set<string> = new Set();
  private sortColumn = '0';
  private sortAsc = true;
  private currentPage = 0;

  // ── Search state ─────────────────────────────────────────────────────────────
  private globalIndex: GlobalDevice[] = [];
  private searchQuery = '';
  private suggestions: Suggestion[] = [];
  private showSuggestions = false;
  private activeSuggestionIdx = -1;
  private highlightedDevicePath = '';
  private highlightTimer: ReturnType<typeof setTimeout> | null = null;

  // ── Subscriptions ─────────────────────────────────────────────────────────────
  private typeTreeUnsub: (() => void) | null = null;
  private storeUnsubs: Map<string, () => void> = new Map();
  private queuedDeviceSubscriptions: string[] = [];
  private queuedDeviceSubscriptionSet: Set<string> = new Set();
  private subscriptionBatchTimer: ReturnType<typeof setTimeout> | null = null;
  private subscriptionBatchGeneration = 0;
  private tablePatchTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly subscriptionBatchSize = 20;

  private dashboards: DashboardMeta[] = [];
  private editMode = false;
  private resizeState: {
    colIdx: number;
    startX: number;
    startWidth: number;
    colEl: HTMLTableColElement | null;
    thEl: HTMLElement | null;
  } | null = null;

  // ── Column helpers ───────────────────────────────────────────────────────────

  /** Returns the column definitions for a tab, falling back to defaults. */
  private getColumnsForTab(tabPath: string): ColumnDef[] {
    const cols = this.config.columns[tabPath];
    return cols && cols.length > 0 ? cols : DEFAULT_COLUMNS.map(c => ({ ...c }));
  }

  private dashboardSelectOptions(selectedId = this.config.clickDashboardId): Array<{ value: string; label: string }> {
    const uniqueDashboards = [...this.dashboards
      .filter(dashboard => !dashboard.isCategory)
      .reduce((byName, dashboard) => {
        const key = dashboard.name.trim().toLowerCase() || String(dashboard.id);
        const existing = byName.get(key);
        if (!existing || String(dashboard.id) === selectedId) byName.set(key, dashboard);
        return byName;
      }, new Map<string, DashboardMeta>())
      .values()];

    const selectedKnown = !selectedId || uniqueDashboards.some(dashboard => String(dashboard.id) === selectedId);

    return [
      { value: '', label: '(none)' },
      ...(!selectedKnown ? [{ value: selectedId, label: `Dashboard ${selectedId}` }] : []),
      ...uniqueDashboards.map(dashboard => ({ value: String(dashboard.id), label: dashboard.name })),
    ];
  }

  // ── Instance property schema (context-aware: one section per tab) ─────────────

  getPropertySchema(): Array<Record<string, any>> {
    const nodeFields = this.config.parentNodes.filter(Boolean).flatMap((path, i) => {
      const label = path.split('.').pop() ?? path;
      return [
        {
          name: `__node_${i}`,
          type: 'section',
          label,
          description: path,
        },
        {
          name: `__click_${i}`,
          type: 'select',
          label: 'Row click dashboard',
          description: 'Dashboard opened when a row in this device node is clicked.',
          default: '',
          context: {
            options: this.dashboardSelectOptions(this.clickDashboardForPath(path)),
          },
        },
        {
          name: `__col_${i}`,
          type: 'column-list',
          label: 'Columns',
          description: `Column definitions for the ${label} tab.`,
          default: DEFAULT_COLUMNS.map(c => ({ ...c })),
          // parentNodeDepth tells the dialog how many leading path segments to strip
          // when a tag is selected from the browser, yielding a relative path.
          context: { parentNodeDepth: path.split('.').length },
        },
      ];
    });

    return [
      {
        name: 'headerText',
        type: 'string',
        label: 'Header text',
        description: 'Widget heading shown in the card title bar. Leave blank to hide the title bar in view mode.',
        default: 'Device List',
      },
      {
        name: 'parentNodes',
        type: 'path-list',
        label: 'Device Parent Nodes',
        description: "Each selected node becomes a tab. The node's direct children are the device rows.",
        default: [],
      },
      ...nodeFields,
      {
        name: 'showPaging',
        type: 'boolean',
        label: 'Show Paging Controls',
        default: false,
      },
      {
        name: 'pageSize',
        type: 'number',
        label: 'Rows Per Page',
        default: 20,
      },
      {
        name: 'showXlsxExport',
        type: 'boolean',
        label: 'Show CSV Export Button',
        default: false,
      },
    ];
  }

  setConfig(c: Partial<Config> & Record<string, any>): void {
    // Extract __col_N and __click_N fields (written by the properties dialog) separately.
    // We need parentNodes resolved first so we know which path each index maps to.
    const colUpdates: Array<{ idx: number; defs: ColumnDef[] }> = [];
    const clickUpdates: Array<{ idx: number; dashboardId: string }> = [];
    const rest: Record<string, any> = {};

    for (const [key, val] of Object.entries(c)) {
      const m = key.match(/^__col_(\d+)$/);
      if (m) {
        colUpdates.push({ idx: parseInt(m[1], 10), defs: val as ColumnDef[] });
      } else {
        const clickMatch = key.match(/^__click_(\d+)$/);
        if (clickMatch) {
          clickUpdates.push({ idx: parseInt(clickMatch[1], 10), dashboardId: String(val ?? '') });
        } else if (!key.match(/^__node_(\d+)$/)) {
          rest[key] = val;
        }
      }
    }

    if (rest.clickPanels !== undefined && rest.clickDashboards === undefined) {
      rest.clickDashboards = rest.clickPanels;
      delete rest.clickPanels;
    }
    if (rest.clickDashboardId !== undefined) {
      rest.clickDashboardId = String(rest.clickDashboardId ?? '');
    }

    // Merge non-col fields (parentNodes, showPaging, ...).
    this.config = { ...this.config, ...rest };

    // If a full columns map was persisted (e.g. loaded from server), merge it.
    if (rest.columns && typeof rest.columns === 'object' && !Array.isArray(rest.columns)) {
      this.config.columns = { ...this.config.columns, ...(rest.columns as Record<string, ColumnDef[]>) };
    }

    // Map __col_N to the parentNode at that index (using now-updated parentNodes).
    for (const { idx, defs } of colUpdates) {
      const path = this.config.parentNodes[idx];
      if (path) this.config.columns[path] = defs;
    }

    if (clickUpdates.length > 0) {
      const map = this.parseClickDashboards();
      for (const { idx, dashboardId } of clickUpdates) {
        const path = this.config.parentNodes[idx];
        if (!path) continue;
        map[path] = dashboardId;
      }
      this.config.clickDashboards = JSON.stringify(map);
    }

    // Apply default columns to any tab that has none yet.
    for (const path of this.config.parentNodes.filter(Boolean)) {
      if (!this.config.columns[path] || this.config.columns[path].length === 0) {
        this.config.columns[path] = DEFAULT_COLUMNS.map(col => ({ ...col }));
      }
    }

    this.buildTabs();
    this.buildGlobalIndex();

    if (!this.tabs.some(t => t.path === this.selectedPath)) {
      this.selectedPath = this.tabs[0]?.path ?? '';
      this.sortColumn = '0';
      this.setDevices([]);
      if (this.selectedPath) {
        this.loadDevicesForPath(this.selectedPath);
      }
    } else {
      // Column config may have changed - rebuild rows and re-subscribe.
      this.setDevices(this.devices.map(d => this.buildRow(d.path)));
      this.subscribeAllDevices();
    }
  }

  getConfig(): Record<string, any> {
    const base: Record<string, any> = {
      headerText: this.config.headerText,
      columns: this.config.columns,
      parentNodes: this.config.parentNodes,
      showPaging: this.config.showPaging,
      pageSize: this.config.pageSize,
      showXlsxExport: this.config.showXlsxExport,
      clickDashboardId: this.config.clickDashboardId,
      clickDashboards: this.config.clickDashboards,
    };
    // Inject __col_N for the dialog to pre-populate each column-list field.
    this.config.parentNodes.filter(Boolean).forEach((path, i) => {
      base[`__col_${i}`] = this.getColumnsForTab(path);
      base[`__click_${i}`] = this.clickDashboardForPath(path);
    });
    return base;
  }

  setEditMode(editing: boolean): void {
    this.editMode = editing;
    this.rerender();
  }

  setDashboardMode(mode: string): void {
    this.setEditMode(mode === 'edit');
  }

  // ── Lifecycle ────────────────────────────────────────────────────────────────

  connectedCallback(): void {
    const user = getCurrentUser();
    this.orgName = user?.tenant_id ?? 'default';
    this.init();
  }

  disconnectedCallback(): void {
    this.detachEventListeners();
    window.removeEventListener('pointermove', this.handleColumnResizeMove);
    window.removeEventListener('pointerup', this.handleColumnResizeEnd);
    this.resizeState = null;
    this.typeTreeUnsub?.();
    this.typeTreeUnsub = null;
    this.cancelQueuedDeviceSubscriptions();
    this.unsubscribeDeviceTags();
    if (this.tablePatchTimer) {
      clearTimeout(this.tablePatchTimer);
      this.tablePatchTimer = null;
    }
    if (this.highlightTimer) clearTimeout(this.highlightTimer);
  }

  rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  // ── Initialisation ───────────────────────────────────────────────────────────

  private async init(): Promise<void> {
    const store = getMirrorStore();
    store.startKvWatch(this.orgName);

    // Apply defaults for any unconfigured tabs.
    for (const path of this.config.parentNodes.filter(Boolean)) {
      if (!this.config.columns[path] || this.config.columns[path].length === 0) {
        this.config.columns[path] = DEFAULT_COLUMNS.map(col => ({ ...col }));
      }
    }

    this.buildTabs();

    if (this.tabs.length > 0 && !this.selectedPath) {
      this.selectedPath = this.tabs[0].path;
    }

    if (this.selectedPath) {
      await this.loadDevicesForPath(this.selectedPath);
    }

    void this.loadDashboards();
    this.buildGlobalIndex();
    this.render();
    this.attachEventListeners();
  }

  private async loadDashboards(): Promise<void> {
    try {
      this.dashboards = await DeviceListWidget.fetchDashboards();
    } catch (err) {
      console.error('DeviceListWidget: failed to load dashboards:', err);
      this.dashboards = [];
    } finally {
      if (this.isConnected) this.rerender();
    }
  }

  private static fetchDashboards(): Promise<DashboardMeta[]> {
    if (DeviceListWidget.dashboardCache.length > 0) {
      return Promise.resolve(DeviceListWidget.dashboardCache);
    }
    if (!DeviceListWidget.dashboardLoadPromise) {
      DeviceListWidget.dashboardLoadPromise = listDashboards()
        .then(dashboards => {
          DeviceListWidget.dashboardCache = dashboards;
          return dashboards;
        })
        .finally(() => {
          DeviceListWidget.dashboardLoadPromise = null;
        });
    }
    return DeviceListWidget.dashboardLoadPromise;
  }

  private buildTabs(): void {
    this.tabs = this.config.parentNodes
      .filter(p => !!p)
      .map(p => ({ path: p, label: p.split('.').pop() ?? p }));
  }

  // ── Tag path resolution ───────────────────────────────────────────────────────

  /**
   * Converts a column tagPath (relative like "sign.message" or absolute like
   * "NASA.ISS.sign.message") into a full store path for a given device.
   * When an absolute path from a sibling device is detected, the device segment
   * is swapped so the tag resolves for `devicePath`.
   */
  private resolveTagPath(devicePath: string, tagPath: string): string {
    const store = getMirrorStore();
    if (tagPath.startsWith(devicePath + '.')) return store.toAbsolute(tagPath);

    for (const parentNode of this.config.parentNodes) {
      if (tagPath.startsWith(parentNode + '.')) {
        const afterParent = tagPath.slice(parentNode.length + 1);
        const dot = afterParent.indexOf('.');
        if (dot !== -1) {
          return store.toAbsolute(`${devicePath}.${afterParent.slice(dot + 1)}`);
        }
      }
    }

    return store.toAbsolute(`${devicePath}.${tagPath}`);
  }

  // ── Global device index (powers search across all tabs) ──────────────────────

  private buildGlobalIndex(): void {
    const store = getMirrorStore();
    this.globalIndex = [];
    for (const tab of this.tabs) {
      const absTabPath = store.toAbsolute(tab.path);
      for (const name of store.listChildrenNames(absTabPath)) {
        const devicePath = `${tab.path}.${name}`;
        const absDevicePath = store.toAbsolute(devicePath);
        const displayName = store.getNodeValue(`${absDevicePath}.meta.name`) ?? name;
        this.globalIndex.push({ tabPath: tab.path, tabLabel: tab.label, devicePath, name: String(displayName) });
      }
    }
  }

  // ── Fuzzy search ─────────────────────────────────────────────────────────────

  private fuzzyMatch(query: string, target: string): { score: number; ranges: Array<[number, number]> } | null {
    const q = query.toLowerCase();
    const t = target.toLowerCase();
    let qi = 0, score = 0, consecutive = 0;
    const ranges: Array<[number, number]> = [];
    let rangeStart = -1;

    for (let ti = 0; ti < t.length && qi < q.length; ti++) {
      if (t[ti] === q[qi]) {
        if (rangeStart === -1) rangeStart = ti;
        score += 10 + consecutive * 5;
        if (ti === 0 || t[ti - 1] === ' ' || t[ti - 1] === '.' || t[ti - 1] === '_') score += 8;
        consecutive++;
        qi++;
      } else {
        if (rangeStart !== -1) { ranges.push([rangeStart, ti - 1]); rangeStart = -1; }
        consecutive = 0;
      }
    }
    if (qi < q.length) return null;
    if (rangeStart !== -1) ranges.push([rangeStart, t.length - 1]);
    if (t.startsWith(q)) score += 50;
    return { score, ranges };
  }

  private getSuggestions(query: string): Suggestion[] {
    if (!query) return [];
    this.buildGlobalIndex();
    const results: Suggestion[] = [];
    for (const entry of this.globalIndex) {
      const m = this.fuzzyMatch(query, entry.name);
      if (m) results.push({ ...entry, score: m.score, ranges: m.ranges });
    }
    return results.sort((a, b) => b.score - a.score).slice(0, 8);
  }

  private highlightName(name: string, ranges: Array<[number, number]>): string {
    let out = '', last = 0;
    for (const [s, e] of ranges) {
      out += this.escHtml(name.slice(last, s));
      out += `<mark class="dlw-match">${this.escHtml(name.slice(s, e + 1))}</mark>`;
      last = e + 1;
    }
    return out + this.escHtml(name.slice(last));
  }

  // ── Device loading ───────────────────────────────────────────────────────────

  private async loadDevicesForPath(parentPath: string): Promise<void> {
    this.typeTreeUnsub?.();
    this.typeTreeUnsub = null;
    this.cancelQueuedDeviceSubscriptions();
    this.unsubscribeDeviceTags();

    if (!parentPath) { this.setDevices([]); return; }

    const store = getMirrorStore();
    const absPath = store.toAbsolute(parentPath);
    const deviceNames = store.listChildrenNames(absPath);

    this.setDevices(deviceNames.map(name => this.buildRow(`${parentPath}.${name}`)));
    this.queueDeviceSubscriptions(deviceNames.map(name => `${parentPath}.${name}`));

    this.typeTreeUnsub = store.subscribeToTreeChanges(absPath, (changedPath) => {
      const depth = changedPath.split('.').length;
      if (depth !== absPath.split('.').length + 1) return;
      const updated = store.listChildrenNames(absPath);
      this.setDevices(updated.map(n => this.buildRow(`${parentPath}.${n}`)));
      this.subscribeAllDevices();
      this.buildGlobalIndex();
      this.rerender();
    });
  }

  private buildRow(devicePath: string): DeviceRow {
    const store = getMirrorStore();
    const tabPath = this.config.parentNodes.find(p => devicePath.startsWith(p + '.')) ?? this.selectedPath;
    const columns = this.getColumnsForTab(tabPath);
    const values: Record<string, any> = {};
    for (const col of columns) {
      values[col.tagPath] = store.resolveTagReference(this.resolveTagPath(devicePath, col.tagPath));
    }
    return { path: devicePath, values };
  }

  private subscribeDeviceTags(devicePath: string): void {
    const store = getMirrorStore();
    const updateRow = () => {
      const idx = this.devices.findIndex(d => d.path === devicePath);
      if (idx === -1) return;
      this.devices[idx] = this.buildRow(devicePath);
      this.scheduleTablePatch();
    };
    const tabPath = this.config.parentNodes.find(p => devicePath.startsWith(p + '.')) ?? this.selectedPath;
    for (const col of this.getColumnsForTab(tabPath)) {
      const fullPath = this.resolveTagPath(devicePath, col.tagPath);
      if (!this.shouldSubscribeTagReference(fullPath) || this.storeUnsubs.has(fullPath)) continue;
      let initialCallback = true;
      const unsubscribe = store.subscribeTagReference(fullPath, () => {
        if (initialCallback) {
          initialCallback = false;
          return;
        }
        updateRow();
      });
      this.storeUnsubs.set(fullPath, unsubscribe);
    }
    // Always subscribe to meta.name for the search index.
    const namePath = store.toAbsolute(`${devicePath}.meta.name`);
    if (!this.storeUnsubs.has(namePath)) {
      const hadInitialName = store.getNodeValue(namePath) !== undefined;
      let skipInitialName = hadInitialName;
      const unsubscribe = store.subscribe(namePath, () => {
        if (skipInitialName) {
          skipInitialName = false;
          return;
        }
        this.buildGlobalIndex();
      });
      this.storeUnsubs.set(namePath, unsubscribe);
    }
  }

  private subscribeAllDevices(): void {
    this.cancelQueuedDeviceSubscriptions();
    this.unsubscribeDeviceTags();
    this.queueDeviceSubscriptions(this.devices.map(d => d.path));
  }

  private shouldSubscribeTagReference(path: string): boolean {
    const store = getMirrorStore();
    const basePath = store.baseTagPath(path);
    if (!basePath) return false;
    if (store.getNodeType(basePath) !== 'unknown') return true;

    const parentPath = basePath.split('.').slice(0, -1).join('.');
    return !parentPath || store.getNodeType(parentPath) === 'unknown';
  }

  private queueDeviceSubscriptions(devicePaths: string[]): void {
    for (const devicePath of devicePaths) {
      if (!devicePath || this.queuedDeviceSubscriptionSet.has(devicePath)) continue;
      this.queuedDeviceSubscriptionSet.add(devicePath);
      this.queuedDeviceSubscriptions.push(devicePath);
    }
    this.scheduleSubscriptionBatch();
  }

  private scheduleSubscriptionBatch(): void {
    if (this.subscriptionBatchTimer || this.queuedDeviceSubscriptions.length === 0) return;
    const generation = this.subscriptionBatchGeneration;
    this.subscriptionBatchTimer = setTimeout(() => this.processSubscriptionBatch(generation), 0);
  }

  private processSubscriptionBatch(generation: number): void {
    this.subscriptionBatchTimer = null;
    if (generation !== this.subscriptionBatchGeneration) return;

    let processed = 0;
    while (processed < this.subscriptionBatchSize && this.queuedDeviceSubscriptions.length > 0) {
      const devicePath = this.queuedDeviceSubscriptions.shift()!;
      this.queuedDeviceSubscriptionSet.delete(devicePath);
      if (this.devicePathSet.has(devicePath)) {
        this.subscribeDeviceTags(devicePath);
      }
      processed++;
    }

    this.scheduleSubscriptionBatch();
  }

  private cancelQueuedDeviceSubscriptions(): void {
    if (this.subscriptionBatchTimer) {
      clearTimeout(this.subscriptionBatchTimer);
      this.subscriptionBatchTimer = null;
    }
    this.queuedDeviceSubscriptions = [];
    this.queuedDeviceSubscriptionSet.clear();
    this.subscriptionBatchGeneration++;
  }

  private unsubscribeDeviceTags(): void {
    this.storeUnsubs.forEach(unsub => unsub());
    this.storeUnsubs.clear();
  }

  private scheduleTablePatch(): void {
    if (this.tablePatchTimer) return;
    this.tablePatchTimer = setTimeout(() => {
      this.tablePatchTimer = null;
      this.patchTableBody();
    }, 16);
  }

  private setDevices(devices: DeviceRow[]): void {
    this.devices = devices;
    this.devicePathSet = new Set(devices.map(d => d.path));
  }

  // ── Table patch ──────────────────────────────────────────────────────────────

  private patchTableBody(): void {
    const tbody = this.querySelector<HTMLElement>('.dlw-tbody');
    if (!tbody) { this.rerender(); return; }
    const sorted = this.getSortedDevices();
    const paged = this.getPagedDevices(sorted);
    tbody.innerHTML = paged.length === 0
      ? `<tr><td colspan="${this.colCount}" class="dlw-empty">No devices</td></tr>`
      : this.renderRows(paged);
    this.attachRowListeners();
  }

  // ── Sort / page helpers ──────────────────────────────────────────────────────

  private getSortedDevices(): DeviceRow[] {
    const colIdx = parseInt(this.sortColumn, 10);
    const col = this.getColumnsForTab(this.selectedPath)[colIdx];
    return [...this.devices].sort((a, b) => {
      const va = col ? (a.values[col.tagPath] ?? '') : '';
      const vb = col ? (b.values[col.tagPath] ?? '') : '';
      const cmp = (typeof va === 'number' && typeof vb === 'number')
        ? va - vb : String(va).localeCompare(String(vb));
      return this.sortAsc ? cmp : -cmp;
    });
  }

  private getPagedDevices(sorted: DeviceRow[]): DeviceRow[] {
    if (!this.config.showPaging) return sorted;
    const start = this.currentPage * this.config.pageSize;
    return sorted.slice(start, start + this.config.pageSize);
  }

  private get colCount(): number { return this.getColumnsForTab(this.selectedPath).length; }

  private parseClickDashboards(): Record<string, string> {
    try { return JSON.parse(this.config.clickDashboards || '{}'); } catch { return {}; }
  }

  private clickDashboardForPath(path: string): string {
    const map = this.parseClickDashboards();
    return Object.prototype.hasOwnProperty.call(map, path) ? (map[path] || '') : (this.config.clickDashboardId || '');
  }

  private rowClickDashboard(): string {
    return this.clickDashboardForPath(this.selectedPath);
  }

  // ── Search: select a device ──────────────────────────────────────────────────

  private async selectDevice(suggestion: Suggestion): Promise<void> {
    this.searchQuery = '';
    this.suggestions = [];
    this.showSuggestions = false;
    this.activeSuggestionIdx = -1;

    this.highlightedDevicePath = suggestion.devicePath;
    if (this.highlightTimer) clearTimeout(this.highlightTimer);
    this.highlightTimer = setTimeout(() => {
      this.highlightedDevicePath = '';
      this.highlightTimer = null;
      this.querySelector('.dlw-highlighted')?.classList.remove('dlw-highlighted');
    }, 3000);

    if (suggestion.tabPath !== this.selectedPath) {
      this.selectedPath = suggestion.tabPath;
      this.currentPage = 0;
      this.sortColumn = '0';
      this.setDevices([]);
      await this.loadDevicesForPath(suggestion.tabPath);
      this.rerender();
      this.scrollToHighlighted();
    } else {
      if (this.config.showPaging) {
        const sorted = this.getSortedDevices();
        const idx = sorted.findIndex(d => d.path === suggestion.devicePath);
        if (idx !== -1) this.currentPage = Math.floor(idx / this.config.pageSize);
      }
      this.rerender();
      this.scrollToHighlighted();
    }
  }

  private scrollToHighlighted(): void {
    requestAnimationFrame(() => {
      this.querySelector<HTMLElement>('.dlw-highlighted')?.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    });
  }

  // ── Render ───────────────────────────────────────────────────────────────────

  protected render(): void {
    this.updateCardTitle();

    const sorted = this.getSortedDevices();
    const paged = this.getPagedDevices(sorted);
    const totalPages = Math.max(1, Math.ceil(sorted.length / this.config.pageSize));

    this.innerHTML = `
      <style>
        .dlw-root {
          display: flex; flex-direction: column; height: 100%; overflow: hidden;
          font-family: inherit; font-size: 0.8125rem; color: var(--content-text);
        }
        .dlw-tabs {
          display: flex; align-items: stretch;
          background: var(--widget-header-bg);
          border-bottom: 1px solid var(--widget-border); flex-shrink: 0;
        }
        .dlw-tabs-list {
          display: flex; flex: 1; overflow-x: auto; scrollbar-width: none;
        }
        .dlw-tabs-list::-webkit-scrollbar { display: none; }
        .dlw-tab {
          padding: 0.5rem 1rem; font-size: 0.6875rem; font-weight: 600;
          letter-spacing: 0.07em; text-transform: uppercase; color: var(--footer-text);
          cursor: pointer; border-bottom: 2px solid transparent;
          white-space: nowrap; flex-shrink: 0;
          transition: color 0.12s, border-color 0.12s, background-color 0.12s;
          user-select: none;
        }
        .dlw-tab:hover {
          color: var(--content-text);
          background: color-mix(in srgb, var(--accent-color) 4%, transparent);
        }
        .dlw-tab.active {
          color: var(--accent-color); border-bottom-color: var(--accent-color);
          background: color-mix(in srgb, var(--accent-color) 7%, transparent);
        }
        .dlw-search-wrap {
          flex-shrink: 0; display: flex; align-items: center; position: relative;
          border-left: 1px solid var(--widget-border); padding: 0 0.5rem;
        }
        .dlw-search-icon {
          position: absolute; left: 1rem; pointer-events: none;
          opacity: 0.4; font-size: 0.75rem; line-height: 1;
        }
        .dlw-search-input {
          background: transparent; border: 1px solid transparent;
          color: var(--content-text); font-size: 0.75rem; font-family: inherit;
          padding: 0.25rem 0.5rem 0.25rem 1.625rem; width: 10rem; outline: none;
          border-radius: 0.25rem; transition: border-color 0.15s, background-color 0.15s, width 0.2s;
        }
        .dlw-search-input::placeholder { color: var(--footer-text); }
        .dlw-search-input:focus {
          border-color: var(--accent-color);
          background: color-mix(in srgb, var(--accent-color) 5%, var(--widget-header-bg));
          width: 14rem;
        }
        .dlw-search-clear {
          position: absolute; right: 0.875rem; background: none; border: none;
          color: var(--footer-text); cursor: pointer; font-size: 0.75rem;
          padding: 0; line-height: 1; opacity: 0.6; display: none;
        }
        .dlw-search-clear.visible { display: block; }
        .dlw-search-clear:hover { opacity: 1; color: var(--content-text); }
        .dlw-suggestions {
          position: absolute; top: calc(100% + 2px); right: 0.5rem; left: 0.5rem;
          z-index: 200; background: var(--modal-bg); border: 1px solid var(--widget-border);
          border-radius: 0.375rem; box-shadow: 0 6px 20px var(--widget-shadow);
          overflow-y: auto; max-height: 16rem; display: none;
        }
        .dlw-suggestions.open { display: block; }
        .dlw-suggestion {
          display: flex; align-items: baseline; gap: 0.5rem;
          padding: 0.4rem 0.75rem; cursor: pointer;
          border-bottom: 1px solid color-mix(in srgb, var(--widget-border) 35%, transparent);
          transition: background 0.1s;
        }
        .dlw-suggestion:last-child { border-bottom: none; }
        .dlw-suggestion:hover,
        .dlw-suggestion.dlw-active { background: color-mix(in srgb, var(--accent-color) 10%, transparent); }
        .dlw-suggestion-name {
          font-size: 0.8125rem; color: var(--content-text); flex: 1;
          overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
        }
        .dlw-suggestion-tab {
          font-size: 0.625rem; font-weight: 600; letter-spacing: 0.07em;
          text-transform: uppercase; color: var(--accent-color); opacity: 0.7; flex-shrink: 0;
        }
        .dlw-match {
          background: color-mix(in srgb, var(--accent-color) 22%, transparent);
          color: var(--accent-color); border-radius: 2px; padding: 0 1px;
        }
        .dlw-no-results {
          padding: 0.75rem; text-align: center; font-size: 0.75rem;
          color: var(--footer-text); font-style: italic;
        }
        .dlw-table-wrap {
          flex: 1; overflow: auto; scrollbar-width: thin;
          scrollbar-color: var(--widget-border) transparent;
        }
        .dlw-table-wrap::-webkit-scrollbar { width: 5px; height: 5px; }
        .dlw-table-wrap::-webkit-scrollbar-track { background: transparent; }
        .dlw-table-wrap::-webkit-scrollbar-thumb { background: var(--widget-border); border-radius: 2px; }
        .dlw-table {
          width: max-content; max-width: none;
          border-collapse: collapse; table-layout: fixed;
        }
        .dlw-table thead { position: sticky; top: 0; z-index: 1; }
        .dlw-table th {
          position: relative;
          background: var(--widget-header-bg); color: var(--footer-text);
          font-size: 0.625rem; font-weight: 600; letter-spacing: 0.09em;
          text-transform: uppercase; padding: 0.5rem 0.875rem; text-align: left;
          border-bottom: 1px solid var(--widget-border); white-space: nowrap;
          cursor: pointer; user-select: none; transition: color 0.12s;
        }
        .dlw-table th:hover { color: var(--content-text); }
        .dlw-table th.dlw-sorted { color: var(--accent-color); }
        .dlw-sort-icon {
          display: inline-block; margin-left: 4px; font-size: 0.5rem;
          opacity: 0.45; vertical-align: middle;
        }
        .dlw-sorted .dlw-sort-icon { opacity: 1; }
        .dlw-col-resizer {
          position: absolute; top: 0; right: -4px; width: 9px; height: 100%;
          cursor: col-resize; display: none; z-index: 2;
          touch-action: none;
        }
        .dlw-editing .dlw-col-resizer { display: block; }
        .dlw-col-resizer::after {
          content: ''; position: absolute; top: 18%; bottom: 18%; left: 4px;
          width: 2px; border-radius: 999px;
          background: color-mix(in srgb, var(--accent-color) 42%, var(--widget-border));
          box-shadow: 0 0 0 1px color-mix(in srgb, var(--content-text) 10%, transparent);
        }
        .dlw-col-resizer:hover::after,
        .dlw-col-resizer.dlw-resizing::after {
          background: var(--accent-color);
          box-shadow: 0 0 0 1px color-mix(in srgb, var(--accent-color) 24%, transparent),
                      0 0 8px color-mix(in srgb, var(--accent-color) 35%, transparent);
        }
        .dlw-table td {
          padding: 0.375rem 0.875rem;
          border-bottom: 1px solid color-mix(in srgb, var(--widget-border) 50%, transparent);
          vertical-align: middle; white-space: nowrap;
          overflow: hidden; text-overflow: ellipsis;
        }
        .dlw-table tbody tr:hover td { background: var(--dlw-row-hover); }
        .dlw-row.dlw-clickable { cursor: pointer; }
        .dlw-row.dlw-clickable:hover td {
          background: color-mix(in srgb, var(--accent-color) 8%, var(--widget-bg));
        }
        @keyframes dlw-row-flash {
          0%   { background: color-mix(in srgb, var(--accent-color) 35%, var(--widget-bg)); }
          100% { background: color-mix(in srgb, var(--accent-color) 12%, var(--widget-bg)); }
        }
        .dlw-row.dlw-highlighted td {
          background: color-mix(in srgb, var(--accent-color) 12%, var(--widget-bg));
          animation: dlw-row-flash 0.7s ease-out forwards;
          outline: 1px solid color-mix(in srgb, var(--accent-color) 35%, transparent);
          outline-offset: -1px;
        }
        .dlw-fmt-text {
          display: block; overflow: hidden; text-overflow: ellipsis;
          white-space: nowrap; color: var(--content-text);
        }
        .dlw-fmt-number {
          font-variant-numeric: tabular-nums;
          font-family: ui-monospace, 'Fira Code', 'Cascadia Code', monospace;
          font-size: 0.75rem; color: var(--dlw-kpi-text);
        }
        .dlw-col-number { text-align: right; }
        .dlw-col-okfail, .dlw-col-cross { text-align: center; }
        .dlw-dim { color: var(--dlw-text-dim); }
        .dlw-status {
          display: inline-flex; align-items: center; justify-content: center;
          width: 1.25rem; font-size: 0.8rem; font-weight: 700;
        }
        .dlw-status-good    { color: var(--status-good-color); }
        .dlw-status-bad     { color: var(--status-bad-color); }
        .dlw-status-unknown { color: var(--status-unknown-color); }
        .dlw-empty {
          text-align: center; padding: 3rem; color: var(--dlw-text-dim);
          font-size: 0.75rem; letter-spacing: 0.1em; text-transform: uppercase;
        }
        .dlw-placeholder {
          flex: 1; display: flex; align-items: center; justify-content: center;
          color: var(--dlw-text-dim); font-size: 0.75rem;
          letter-spacing: 0.1em; text-transform: uppercase;
        }
        .dlw-footer {
          display: flex; align-items: center; gap: 0.5rem;
          padding: 0.375rem 0.875rem; background: var(--widget-header-bg);
          border-top: 1px solid var(--widget-border); flex-shrink: 0; min-height: 2.25rem;
        }
        .dlw-count {
          font-size: 0.6875rem; font-weight: 600; letter-spacing: 0.07em;
          color: var(--content-text); text-transform: uppercase;
        }
        .dlw-spacer { flex: 1; }
        .dlw-paging { display: flex; align-items: center; gap: 0.5rem; }
        .dlw-page-info {
          font-size: 0.6875rem; letter-spacing: 0.05em; color: var(--content-text);
          min-width: 4.5rem; text-align: center;
        }
        .dlw-page-btn {
          background: transparent; border: 1px solid var(--widget-border);
          color: var(--content-text); padding: 0.125rem 0.625rem;
          font-size: 0.6875rem; font-family: inherit; cursor: pointer;
          border-radius: 0.25rem; line-height: 1.6;
          transition: border-color 0.12s, color 0.12s;
        }
        .dlw-page-btn:hover:not(:disabled) { border-color: var(--accent-color); color: var(--accent-color); }
        .dlw-page-btn:disabled { opacity: 0.25; cursor: default; }
        .dlw-export-btn {
          background: transparent; border: 1px solid var(--widget-border);
          color: var(--footer-text); padding: 0.125rem 0.75rem;
          font-size: 0.625rem; font-family: inherit; letter-spacing: 0.09em;
          text-transform: uppercase; font-weight: 600; cursor: pointer;
          border-radius: 0.25rem; transition: border-color 0.12s, color 0.12s, background-color 0.12s;
        }
        .dlw-export-btn:hover {
          border-color: var(--accent-color); color: var(--accent-color);
          background: color-mix(in srgb, var(--accent-color) 8%, transparent);
        }
      </style>

      <div class="dlw-root${this.editMode ? ' dlw-editing' : ''}">
        ${this.renderTabs()}
        ${this.tabs.length === 0
          ? '<div class="dlw-placeholder">No parent nodes configured</div>'
          : `
            <div class="dlw-table-wrap">
              <table class="dlw-table">
                <colgroup>${this.renderColgroup()}</colgroup>
                <thead><tr>${this.renderHeaders()}</tr></thead>
                <tbody class="dlw-tbody">
                  ${paged.length === 0
                    ? `<tr><td colspan="${this.colCount}" class="dlw-empty">No devices</td></tr>`
                    : this.renderRows(paged)}
                </tbody>
              </table>
            </div>
            ${this.renderFooter(sorted.length, totalPages)}
          `}
      </div>
    `;
  }

  private renderTabs(): string {
    const tabsHtml = this.tabs.map(tab =>
      `<div class="dlw-tab ${tab.path === this.selectedPath ? 'active' : ''}"
            data-path="${this.escHtml(tab.path)}"
            title="${this.escHtml(tab.path)}">${this.escHtml(tab.label)}</div>`,
    ).join('');

    const suggestionsHtml = this.showSuggestions
      ? `<div class="dlw-suggestions open">
           ${this.suggestions.length === 0
             ? '<div class="dlw-no-results">No matches found</div>'
             : this.suggestions.map((s, i) => `
                 <div class="dlw-suggestion${i === this.activeSuggestionIdx ? ' dlw-active' : ''}" data-idx="${i}">
                   <span class="dlw-suggestion-name">${this.highlightName(s.name, s.ranges)}</span>
                   <span class="dlw-suggestion-tab">${this.escHtml(s.tabLabel)}</span>
                 </div>`).join('')}
         </div>`
      : '';

    return `
      <div class="dlw-tabs">
        <div class="dlw-tabs-list">${tabsHtml}</div>
        <div class="dlw-search-wrap">
          <span class="dlw-search-icon">🔍</span>
          <input class="dlw-search-input" type="text" placeholder="Search devices…"
                 autocomplete="off" spellcheck="false" value="${this.escHtml(this.searchQuery)}">
          <button class="dlw-search-clear${this.searchQuery ? ' visible' : ''}"
                  type="button" title="Clear search">✕</button>
          ${suggestionsHtml}
        </div>
      </div>`;
  }

  private renderColgroup(): string {
    return this.getColumnsForTab(this.selectedPath)
      .map(col => col.width ? `<col style="width:${this.escHtml(col.width)}">` : '<col>')
      .join('');
  }

  private isCentered(formatter: ColumnDef['formatter']): boolean {
    return formatter === 'okfail' || formatter === 'cross';
  }

  private renderHeaders(): string {
    return this.getColumnsForTab(this.selectedPath).map((col, i) => {
      const active = this.sortColumn === String(i);
      const icon = active ? (this.sortAsc ? '▲' : '▼') : '⇅';
      const center = this.isCentered(col.formatter) ? ' style="text-align:center"' : '';
      return `<th class="${active ? 'dlw-sorted' : ''}" data-col="${i}"${center}>${
        this.escHtml(col.header)
      }<span class="dlw-sort-icon">${icon}</span><span class="dlw-col-resizer" data-col="${i}" title="Resize column"></span></th>`;
    }).join('');
  }

  private renderRows(devices: DeviceRow[]): string {
    const clickable = !!this.rowClickDashboard();
    const cols = this.getColumnsForTab(this.selectedPath);
    return devices.map(d => {
      const isHighlighted = d.path === this.highlightedDevicePath;
      const classes = ['dlw-row', clickable ? 'dlw-clickable' : '', isHighlighted ? 'dlw-highlighted' : '']
        .filter(Boolean).join(' ');
      return `
        <tr class="${classes}" data-path="${this.escHtml(d.path)}">
          ${cols.map(col => {
            const center = this.isCentered(col.formatter) ? ' style="text-align:center"' : '';
            return `<td class="dlw-col-${col.formatter}"${center}>${this.renderCellValue(d.values[col.tagPath], col.formatter)}</td>`;
          }).join('')}
        </tr>`;
    }).join('');
  }

  private renderCellValue(val: any, formatter: ColumnDef['formatter']): string {
    switch (formatter) {
      case 'number': {
        if (val === undefined || val === null) return '<span class="dlw-dim">-</span>';
        const n = parseFloat(String(val));
        if (isNaN(n)) return `<span class="dlw-fmt-number">${this.escHtml(String(val))}</span>`;
        return `<span class="dlw-fmt-number">${this.escHtml(parseFloat(n.toFixed(2)).toString())}</span>`;
      }
      case 'okfail':
        if (val === undefined || val === null)
          return `<span class="dlw-status dlw-status-unknown" title="Unknown">–</span>`;
        return val
          ? `<span class="dlw-status dlw-status-good" title="OK">✓</span>`
          : `<span class="dlw-status dlw-status-bad"  title="Fail">✗</span>`;
      case 'cross':
        if (!val) return '';
        return `<span class="dlw-status dlw-status-bad" title="Active">✗</span>`;
      case 'text':
      default: {
        if (val === undefined || val === null) return '<span class="dlw-dim">-</span>';
        const str = String(val);
        return `<span class="dlw-fmt-text" title="${this.escHtml(str)}">${this.escHtml(str)}</span>`;
      }
    }
  }

  private renderFooter(total: number, totalPages: number): string {
    const showFooter = this.config.showPaging || this.config.showXlsxExport;
    if (!showFooter) return '';
    return `
      <div class="dlw-footer">
        ${this.config.showPaging ? `
          <div class="dlw-paging">
            <button class="dlw-page-btn dlw-prev" ${this.currentPage === 0 ? 'disabled' : ''}>◀</button>
            <span class="dlw-page-info">${this.currentPage + 1} / ${totalPages}</span>
            <button class="dlw-page-btn dlw-next" ${this.currentPage >= totalPages - 1 ? 'disabled' : ''}>▶</button>
          </div>` : ''}
        <span class="dlw-spacer"></span>
        <span class="dlw-count">${total}&thinsp;device${total !== 1 ? 's' : ''}</span>
        ${this.config.showXlsxExport
          ? '<button class="dlw-export-btn dlw-export-xlsx">CSV</button>'
          : ''}
      </div>`;
  }

  private escHtml(s: string): string {
    return String(s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (card && typeof card.setTitle === 'function') {
      card.setTitle(this.config.headerText ?? 'Device List');
    }
    if (card && typeof card.setHeaderVisible === 'function') {
      card.setHeaderVisible(true);
    }
  }

  // ── Event listeners ──────────────────────────────────────────────────────────

  protected attachEventListeners(): void {
    this.querySelectorAll<HTMLElement>('.dlw-tab').forEach(tab =>
      tab.addEventListener('click', this.handleTabClick),
    );
    this.querySelectorAll<HTMLElement>('.dlw-table th').forEach(th =>
      th.addEventListener('click', this.handleHeaderClick),
    );
    this.querySelectorAll<HTMLElement>('.dlw-col-resizer').forEach(resizer =>
      resizer.addEventListener('pointerdown', this.handleColumnResizeStart),
    );
    this.attachRowListeners();
    this.querySelector('.dlw-prev')?.addEventListener('click', this.handlePrevPage);
    this.querySelector('.dlw-next')?.addEventListener('click', this.handleNextPage);
    this.querySelector('.dlw-export-xlsx')?.addEventListener('click', this.handleExportXlsx);

    const input = this.querySelector<HTMLInputElement>('.dlw-search-input');
    input?.addEventListener('input', this.handleSearchInput);
    input?.addEventListener('keydown', this.handleSearchKeyDown);
    input?.addEventListener('focus', this.handleSearchFocus);
    input?.addEventListener('blur', this.handleSearchBlur);
    this.querySelector('.dlw-search-clear')?.addEventListener('click', this.handleSearchClear);
    this.querySelectorAll<HTMLElement>('.dlw-suggestion').forEach(el =>
      el.addEventListener('mousedown', this.handleSuggestionMousedown),
    );
  }

  private attachRowListeners(): void {
    if (!this.rowClickDashboard()) return;
    this.querySelectorAll<HTMLElement>('.dlw-row.dlw-clickable').forEach(row =>
      row.addEventListener('click', this.handleRowClick),
    );
  }

  protected detachEventListeners(): void { /* innerHTML replacement clears all listeners */ }

  // ── Handlers ─────────────────────────────────────────────────────────────────

  private handleTabClick = (e: Event): void => {
    const path = (e.currentTarget as HTMLElement).dataset.path ?? '';
    if (!path || path === this.selectedPath) return;
    this.selectedPath = path;
    this.currentPage = 0;
    this.sortColumn = '0';
    this.sortAsc = true;
    this.setDevices([]);
    this.searchQuery = '';
    this.showSuggestions = false;
    this.loadDevicesForPath(path).then(() => this.rerender());
  };

  private handleHeaderClick = (e: Event): void => {
    if ((e.target as HTMLElement).closest('.dlw-col-resizer')) return;
    const col = (e.currentTarget as HTMLElement).dataset.col ?? '';
    if (!col) return;
    this.sortColumn === col ? (this.sortAsc = !this.sortAsc) : ((this.sortColumn = col), (this.sortAsc = true));
    this.currentPage = 0;
    this.querySelectorAll<HTMLElement>('.dlw-table th').forEach((th, i) => {
      const active = String(i) === this.sortColumn;
      th.classList.toggle('dlw-sorted', active);
      const icon = th.querySelector<HTMLElement>('.dlw-sort-icon');
      if (icon) icon.textContent = active ? (this.sortAsc ? '▲' : '▼') : '⇅';
    });
    this.patchTableBody();
  };

  private handleRowClick = (e: Event): void => {
    const path = (e.currentTarget as HTMLElement).dataset.path ?? '';
    const dashboard = this.rowClickDashboard();
    if (dashboard) this.emit('dashboard-open', { dashboard, id: dashboard, devicePath: path });
  };

  private handleColumnResizeStart = (e: PointerEvent): void => {
    if (!this.editMode) return;
    e.preventDefault();
    e.stopPropagation();

    const handle = e.currentTarget as HTMLElement;
    const colIdx = parseInt(handle.dataset.col ?? '-1', 10);
    if (colIdx < 0) return;

    const thEl = handle.closest('th') as HTMLElement | null;
    const colEl = this.querySelectorAll<HTMLTableColElement>('.dlw-table col').item(colIdx) ?? null;
    const startWidth = thEl?.getBoundingClientRect().width ?? colEl?.getBoundingClientRect().width ?? 120;
    this.resizeState = { colIdx, startX: e.clientX, startWidth, colEl, thEl };
    handle.classList.add('dlw-resizing');

    window.addEventListener('pointermove', this.handleColumnResizeMove);
    window.addEventListener('pointerup', this.handleColumnResizeEnd, { once: true });
  };

  private handleColumnResizeMove = (e: PointerEvent): void => {
    if (!this.resizeState) return;
    const width = this.clampColumnWidth(this.resizeState.startWidth + e.clientX - this.resizeState.startX);
    if (this.resizeState.colEl) this.resizeState.colEl.style.width = `${width}px`;
    if (this.resizeState.thEl) this.resizeState.thEl.style.width = `${width}px`;
  };

  private handleColumnResizeEnd = (e: PointerEvent): void => {
    window.removeEventListener('pointermove', this.handleColumnResizeMove);
    this.querySelectorAll('.dlw-col-resizer.dlw-resizing').forEach(el => el.classList.remove('dlw-resizing'));
    if (!this.resizeState) return;

    const width = this.clampColumnWidth(this.resizeState.startWidth + e.clientX - this.resizeState.startX);
    const cols = this.getColumnsForTab(this.selectedPath).map(col => ({ ...col }));
    if (cols[this.resizeState.colIdx]) {
      cols[this.resizeState.colIdx].width = `${width}px`;
      this.config.columns[this.selectedPath] = cols;
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
    }
    this.resizeState = null;
    this.rerender();
  };

  private clampColumnWidth(width: number): number {
    return Math.max(48, Math.min(800, Math.round(width)));
  }

  private handlePrevPage = (): void => {
    if (this.currentPage > 0) { this.currentPage--; this.rerender(); }
  };

  private handleNextPage = (): void => {
    const total = Math.ceil(this.devices.length / this.config.pageSize);
    if (this.currentPage < total - 1) { this.currentPage++; this.rerender(); }
  };

  private handleExportXlsx = (): void => {
    const sorted = this.getSortedDevices();
    const cols = this.getColumnsForTab(this.selectedPath);
    const headers = cols.map(c => c.header);
    const rows = sorted.map(d => cols.map(c => {
      const val = d.values[c.tagPath];
      return val === undefined || val === null ? '' : String(val);
    }));
    const csv = [headers, ...rows]
      .map(row => row.map(cell => `"${String(cell).replace(/"/g, '""')}"`).join(','))
      .join('\r\n');
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    const label = this.tabs.find(t => t.path === this.selectedPath)?.label ?? 'devices';
    a.download = `${label}-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  // ── Search handlers ───────────────────────────────────────────────────────────

  private handleSearchInput = (e: Event): void => {
    this.searchQuery = (e.target as HTMLInputElement).value;
    this.activeSuggestionIdx = -1;
    this.suggestions = this.getSuggestions(this.searchQuery);
    this.showSuggestions = this.searchQuery.length > 0;
    this.patchSearchUI();
  };

  private handleSearchKeyDown = (e: KeyboardEvent): void => {
    if (!this.showSuggestions) return;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      this.activeSuggestionIdx = Math.min(this.activeSuggestionIdx + 1, this.suggestions.length - 1);
      this.patchSuggestionHighlight();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      this.activeSuggestionIdx = Math.max(this.activeSuggestionIdx - 1, -1);
      this.patchSuggestionHighlight();
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const idx = this.activeSuggestionIdx >= 0 ? this.activeSuggestionIdx : 0;
      if (this.suggestions[idx]) this.selectDevice(this.suggestions[idx]);
    } else if (e.key === 'Escape') {
      this.handleSearchClear();
    }
  };

  private handleSearchFocus = (): void => {
    if (this.searchQuery) { this.showSuggestions = true; this.patchSearchUI(); }
  };

  private handleSearchBlur = (): void => {
    setTimeout(() => { this.showSuggestions = false; this.patchSearchUI(); }, 150);
  };

  private handleSearchClear = (): void => {
    this.searchQuery = '';
    this.suggestions = [];
    this.showSuggestions = false;
    this.activeSuggestionIdx = -1;
    this.patchSearchUI();
    this.querySelector<HTMLInputElement>('.dlw-search-input')?.focus();
  };

  private handleSuggestionMousedown = (e: Event): void => {
    e.preventDefault();
    const idx = parseInt((e.currentTarget as HTMLElement).dataset.idx ?? '0', 10);
    if (this.suggestions[idx]) this.selectDevice(this.suggestions[idx]);
  };

  // ── Search UI patch ─────────────────────────────────────────────────────────

  private patchSearchUI(): void {
    const wrap = this.querySelector<HTMLElement>('.dlw-search-wrap');
    if (!wrap) { this.rerender(); return; }
    wrap.querySelector('.dlw-suggestions')?.remove();
    const clearBtn = wrap.querySelector<HTMLElement>('.dlw-search-clear');
    if (clearBtn) clearBtn.classList.toggle('visible', !!this.searchQuery);
    if (!this.showSuggestions) return;
    const div = document.createElement('div');
    div.className = 'dlw-suggestions open';
    div.innerHTML = this.suggestions.length === 0
      ? '<div class="dlw-no-results">No matches found</div>'
      : this.suggestions.map((s, i) => `
          <div class="dlw-suggestion${i === this.activeSuggestionIdx ? ' dlw-active' : ''}" data-idx="${i}">
            <span class="dlw-suggestion-name">${this.highlightName(s.name, s.ranges)}</span>
            <span class="dlw-suggestion-tab">${this.escHtml(s.tabLabel)}</span>
          </div>`).join('');
    wrap.appendChild(div);
    div.querySelectorAll<HTMLElement>('.dlw-suggestion').forEach(el =>
      el.addEventListener('mousedown', this.handleSuggestionMousedown),
    );
  }

  private patchSuggestionHighlight(): void {
    this.querySelectorAll<HTMLElement>('.dlw-suggestion').forEach((el, i) => {
      el.classList.toggle('dlw-active', i === this.activeSuggestionIdx);
      if (i === this.activeSuggestionIdx) el.scrollIntoView({ block: 'nearest' });
    });
  }
}

customElements.define('device-list-widget', DeviceListWidget);
