// Import styles
import './styles.css';

// Global error handler
window.onerror = function(msg, url, line, col) {
  console.error('Global error:', msg, 'at', url, ':', line, ':', col);
  document.body.innerHTML = '<div style="padding: 20px; color: red;"><h1>Error</h1><pre>' + msg + '</pre></div>';
  return false;
};

// Import icon utilities
import './components/icon-picker';
import { preloadIconSet } from './utils/icons';
preloadIconSet('mdi');

// Register built-in widget metadata and lazy loaders.
import './dashboards/widgets/builtin-widgets';

// Import components (they auto-register)
import './components/app-sidebar';
import './components/app-header';
import './components/app-footer';
import './components/app-content';
import './components/preferences-dialog';
import './components/app-dialog';

// Import dashboards (they auto-register)
import './dashboards/dashboard-config-editor';
import {
  createStarterHelpWidgets,
  createStarterTagViewWidgets,
  type DashboardConfig,
} from './dashboards/dashboard-config-editor';
import { configsToMenuItems } from './dashboards/dashboard-menu';
import { resolveDashboardForDeviceSubtype } from './dashboards/dashboard-selection';
// TabData type is used internally by AppHeader.setTabs()

// Import authentication
import { initializeAuth, getAuthHeaders, logout, getCurrentUser } from './auth';
import './components/login-page';
import './components/profile-dialog';

// Import reactive store
import { getMirrorStore } from './store/store';
import { getUiStore } from './store/ui-store';

// Import API wrapper
import { ApiError, setAuthHeadersProvider, listDashboards, getDashboard, createDashboard, updateDashboard, deleteDashboard, fetchHealth, fetchNATSConfig } from './api';
import type { DashboardMeta } from './api';

// Import permissions
import { loadPermissions } from './permissions/permissions';
import { registerPermissions } from './permissions/registry';

// Import plugin loader
import { initPlugins } from './plugins/loader';

// Register server-side RTDB resources so they appear in the Permissions widget
registerPermissions('nodes', 'RTDB Nodes', [
  { name: 'read',  description: 'Read node structure and metadata' },
  { name: 'write', description: 'Create, update, and delete nodes' },
], 'Server-side access to the real-time database node tree - controls who can read node structure and metadata, or create, edit, and delete nodes.');
registerPermissions('tags', 'RTDB Tags', [
  { name: 'read',  description: 'Read tag values and metadata' },
  { name: 'write', description: 'Create, update, and delete tags' },
], 'Server-side access to RTDB tag data - controls who can read live tag values or create, update, and delete tag definitions.');
registerPermissions('logs', 'System Logs', [
  { name: 'read', description: 'Query system log entries' },
  { name: 'write', description: 'Create system log entries' },
], 'Server-side access to the log store - controls who can query, view, and create system log entries.');
registerPermissions('profile', 'User Profile', [
  { name: 'change', description: 'Change own profile and password' },
], 'Controls whether users can update their own profile details and password.');

// CRITICAL: Set up auth headers provider BEFORE importing components
// Components may make API calls during initialization
setAuthHeadersProvider(getAuthHeaders);

// Theme is initialized by the ThemeManager singleton constructor (imported by preferences-dialog)

// Store connection configuration
const NATS_CONFIG = {
  kvBucket: (import.meta as any).env.VITE_NATS_KV_BUCKET || 'rtdb',
};

// Initialize store connection
async function initializeStore(): Promise<void> {
  try {
    const store = getMirrorStore();

    // Fetch WebSocket NATS credentials from the REST API (authenticated endpoint)
    const natsCfg = await fetchNATSConfig();
    const wsUrl = natsCfg.natsWsUrl || `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}${natsCfg.natsWsPath}`;
    await store.storeConnectNats(wsUrl, NATS_CONFIG.kvBucket, natsCfg.username, natsCfg.password);

    // Load tree structure and metadata from REST API
    // Use depth=-1 to fetch entire subtree in a single request (instead of thousands of sequential requests)
    await store.loadTreeFromAPI('', -1);
  } catch (err) {
    console.error('XACT: Failed to initialize store:', err);
  }
}

// Disconnect store on page unload
window.addEventListener('beforeunload', () => {
  const store = getMirrorStore();
  store.storeDisconnectNats();
});

// Convert server DashboardMeta list to DashboardConfig tree for the editor/sidebar
function dashboardMetaToConfigs(dashboards: DashboardMeta[]): DashboardConfig[] {
  const topLevel: DashboardConfig[] = [];
  const byId = new Map<number, DashboardConfig>();
  const childMap = new Map<number, DashboardConfig[]>();

  // First pass: create DashboardConfig objects
  for (const p of dashboards) {
    const config: DashboardConfig = {
      id: `server:${p.id}`,
      serverId: p.id,
      name: p.name,
      description: p.description,
      icon: p.icon,
      dashboardTag: '',
      variation: p.variation,
      deviceType: p.deviceType,
      permission: p.permission || '',
    };
    if (p.isCategory) config.children = [];
    byId.set(p.id, config);

    if (p.parentId) {
      if (!childMap.has(p.parentId)) childMap.set(p.parentId, []);
      childMap.get(p.parentId)!.push(config);
    } else {
      topLevel.push(config);
    }
  }

  // Second pass: attach children.
  for (const [parentId, children] of childMap) {
    const parent = byId.get(parentId);
    if (parent) {
      parent.children = children;
    }
  }

  return topLevel;
}

function hasWidgets(widgets: unknown): boolean {
  return Array.isArray(widgets) && widgets.length > 0;
}

async function backfillStarterDashboards(dashboards: DashboardMeta[]): Promise<boolean> {
  const starterHelp = dashboards.find(p => !p.isCategory && ['Dashboard', 'DASHBOARD', 'Help'].includes(p.name));
  const starterTagView = dashboards.find(p => !p.isCategory && p.name === 'Tag View');
  const updates: Promise<boolean>[] = [];

  if (starterHelp) {
    updates.push((async () => {
      const dashboard = await getDashboard(starterHelp.id);
      if (hasWidgets(dashboard.widgets)) return false;

      await updateDashboard(starterHelp.id, {
        name: 'DASHBOARD',
        description: 'XACT help manual',
        icon: 'mdi:view-dashboard',
        widgets: createStarterHelpWidgets(),
      } as any);
      return true;
    })());
  }

  if (starterTagView) {
    updates.push((async () => {
      const dashboard = await getDashboard(starterTagView.id);
      if (hasWidgets(dashboard.widgets)) return false;

      await updateDashboard(starterTagView.id, {
        widgets: createStarterTagViewWidgets(),
      } as any);
      return true;
    })());
  }

  if (updates.length === 0) return false;
  const results = await Promise.all(updates);
  const changed = results.some(Boolean);
  if (changed) console.log('XACT: Backfilled starter dashboard widgets');
  return changed;
}

// Save dashboard configs to server.
// Diffs by the server-assigned numeric id (serverId) so that renames are
// treated as updates - not as a delete + create - preserving widget data.
async function saveDashboardConfigs(configs: DashboardConfig[], reconcileTarget = configs): Promise<void> {
  try {
    // Get current server state: map numeric id → DashboardMeta for O(1) lookup.
    const existing = await listDashboards();
    const existingById = new Map(existing.map(p => [p.id, p]));

    // Flatten configs to ordered rows, carrying serverId through.
    type DashboardPayload = {
      name: string; description: string; icon: string; variation: string;
      deviceType: string; permission: string; isCategory: boolean; sortOrder: number;
      parentId?: number | null; widgets?: any[];
    };
    type FlatConfig = { config: DashboardConfig; payload: DashboardPayload; parent?: DashboardConfig };
    const flat: FlatConfig[] = [];
    let order = 0;
    for (const c of configs) {
      flat.push({
        config: c,
        payload: {
          name: c.name, description: c.description, icon: c.icon,
          variation: c.variation, deviceType: c.deviceType, permission: c.permission || '',
          isCategory: c.children !== undefined,
          sortOrder: order++, parentId: null,
        },
      });
      if (c.children) {
        for (const child of c.children) {
          flat.push({
            config: child,
            payload: {
              name: child.name, description: child.description, icon: child.icon,
              variation: child.variation, deviceType: child.deviceType, permission: child.permission || '',
              isCategory: child.children !== undefined,
              sortOrder: order++,
            },
            parent: c,
          });
        }
      }
    }

    const resolveExistingMeta = (config: DashboardConfig): DashboardMeta | undefined => {
      if (config.serverId !== undefined && existingById.has(config.serverId)) {
        return existingById.get(config.serverId);
      }
      return undefined;
    };

    // Delete dashboards whose serverId no longer appears in the new config list.
    const newServerIds = new Set<number>();
    for (const { config } of flat) {
      const existingMeta = resolveExistingMeta(config);
      if (existingMeta) {
        newServerIds.add(existingMeta.id);
      }
    }
    for (const [id, meta] of existingById) {
      if (!newServerIds.has(id)) {
        await deleteDashboard(meta.id);
      }
    }

    // Create or update dashboards (parents first so parent_id can resolve on create).
    // Widgets are never sent on update so existing widget data is preserved.
    for (const { config, payload, parent } of flat) {
      payload.parentId = parent ? (parent.serverId ?? null) : null;
      const existingMeta = resolveExistingMeta(config);
      if (existingMeta) {
        await updateDashboard(existingMeta.id, payload as any);
      } else {
        const created = await createDashboard({ ...payload, widgets: config.widgets ?? [] } as any);
        config.serverId = created.id;
      }
    }

    reconcileDashboardServerIds(configs, reconcileTarget);
  } catch (err) {
    console.error('XACT: Failed to save dashboard configs:', err);
  }
}

function reconcileDashboardServerIds(savedConfigs: DashboardConfig[], targetConfigs: DashboardConfig[]): void {
  const savedByLocalId = new Map<string, number>();
  const collect = (configs: DashboardConfig[]) => {
    for (const config of configs) {
      if (config.serverId !== undefined) savedByLocalId.set(config.id, config.serverId);
      if (config.children) collect(config.children);
    }
  };
  const apply = (configs: DashboardConfig[]) => {
    for (const config of configs) {
      const serverId = savedByLocalId.get(config.id);
      if (serverId !== undefined) config.serverId = serverId;
      if (config.children) apply(config.children);
    }
  };
  collect(savedConfigs);
  apply(targetConfigs);
}

async function loadDashboardMenuFromServer(
  editor: import('./dashboards/dashboard-config-editor').DashboardConfigEditor | null,
  sidebar: import('./components/app-sidebar').AppSidebar | null,
): Promise<void> {
  let serverDashboards = await listDashboards();
  if (serverDashboards.length > 0) {
    if (await backfillStarterDashboards(serverDashboards)) {
      serverDashboards = await listDashboards();
    }
    const configs = dashboardMetaToConfigs(serverDashboards);
    editor?.setConfigs(configs);
    sidebar?.setMenuItems(configsToMenuItems(configs));
    return;
  }

  // No dashboards on server yet - persist editor defaults with their starter widgets.
  if (editor) {
    const configs = editor.getConfigs();
    await saveDashboardConfigs(configs);
    sidebar?.setMenuItems(configsToMenuItems(configs));
  }
}

async function recoverFromStaleSession(
  app: HTMLElement,
  header: import('./components/app-header').AppHeader | null,
): Promise<void> {
  logout();
  header?.clearUser();
  app.querySelector('app-header')?.clearUser();
  await waitForLogin();

  const authedUser = getCurrentUser();
  if (authedUser?.tenant_id) {
    getUiStore().set('orgName', authedUser.tenant_id);
  }
  await loadPermissions();
}

function isAuthApiError(err: unknown): boolean {
  return err instanceof ApiError && (err.status === 401 || err.status === 403);
}

// TypeScript declarations for custom elements
declare global {
  interface HTMLElementTagNameMap {
    'app-sidebar': import('./components/app-sidebar').AppSidebar;
    'app-header': import('./components/app-header').AppHeader;
    'app-footer': import('./components/app-footer').AppFooter;
    'app-content': import('./components/app-content').AppContent;
    'preferences-dialog': import('./components/preferences-dialog').PreferencesDialog;
  }
}

// Show the login page overlay and wait for successful authentication.
function waitForLogin(): Promise<void> {
  return new Promise((resolve) => {
    const loginPage = document.createElement('login-page') as HTMLElement;
    document.body.appendChild(loginPage);
    loginPage.addEventListener('auth-success', () => {
      document.body.removeChild(loginPage);
      resolve();
    }, { once: true });
  });
}

// App initialization
document.addEventListener('DOMContentLoaded', async () => {
  const app = document.getElementById('app');
  if (!app) {
    console.error('XACT: #app element not found');
    return;
  }

  // Show login page if no valid token exists
  if (!initializeAuth()) {
    app.querySelector('app-header')?.clearUser();
    await waitForLogin();
  }

  // Seed the UI store with the authenticated user's org
  const authedUser = getCurrentUser();
  if (authedUser?.tenant_id) {
    getUiStore().set('orgName', authedUser.tenant_id);
  }

  // Fetch server timezone so widgets can display times in the server's zone
  try {
    const health = await fetchHealth();
    if (health.timezone) {
      getUiStore().set('serverTimezone', health.timezone);
    }
  } catch (err) {
    console.warn('XACT: Failed to fetch server timezone:', err);
  }

  // Load permissions before rendering UI
  await loadPermissions();

  // Initialize reactive store connection
  await initializeStore();

  // Load widget plugins from server
  await initPlugins();

  // Hide loading indicator
  const loading = document.getElementById('loading');
  if (loading) {
    loading.classList.add('hidden');
  }

  const sidebar = app.querySelector('app-sidebar') as import('./components/app-sidebar').AppSidebar;
  const header = app.querySelector('app-header') as import('./components/app-header').AppHeader;
  const content = app.querySelector('app-content') as import('./components/app-content').AppContent;
  let editor: import('./dashboards/dashboard-config-editor').DashboardConfigEditor | null = null;

  // ── Tab state management ──
  interface TabInfo { id: string; dashboardId: string; title: string; }
  let tabs: TabInfo[] = [];
  let activeTabId = '';
  let tabCounter = 0;

  function createTab(dashboardId: string, title: string): TabInfo {
    const tab: TabInfo = { id: `tab-${++tabCounter}`, dashboardId, title };
    tabs.push(tab);
    return tab;
  }

  function syncTabsToHeader(): void {
    header?.setTabs(tabs.map(t => ({
      id: t.id,
      title: t.title,
      active: t.id === activeTabId,
    })));
  }

  function findDashboardConfig(ref: string): DashboardConfig | undefined {
    const configs = editor?.getConfigs() ?? [];
    for (const config of configs) {
      if (String(config.serverId ?? config.id) === ref || config.name === ref) return config;
      for (const child of config.children ?? []) {
        if (String(child.serverId ?? child.id) === ref || child.name === ref) return child;
      }
    }
    return undefined;
  }

  function resolveDashboardRef(ref: string): string {
    if (/^\d+$/.test(ref)) return ref;
    const config = findDashboardConfig(ref);
    return config?.serverId !== undefined ? String(config.serverId) : ref;
  }

  function resolveDashboardTitle(ref: string, fallback = ref): string {
    if (ref === 'dashboard-config-editor') return 'Dashboards';
    if (ref.startsWith('__blank__')) return 'New Tab';
    return findDashboardConfig(ref)?.name ?? fallback;
  }

  function setDeviceContextFromPath(devicePath?: string): void {
    if (!devicePath) return;
    const relativePath = getMirrorStore().toRelative(devicePath);
    const parts = relativePath.split('.').filter(Boolean);
    const deviceName = parts.at(-1) ?? '';
    const deviceType = parts.length >= 2 ? parts.at(-2) ?? '' : '';
    if (deviceName) getUiStore().set('deviceName', deviceName);
    if (deviceType) getUiStore().set('deviceType', deviceType);
  }

  function resolveDashboardSelection(ref: string, devicePath?: string): string {
    const dashboardRef = resolveDashboardRef(ref);
    return resolveDashboardForDeviceSubtype(editor?.getConfigs() ?? [], dashboardRef, {
      store: getMirrorStore(),
      ui: getUiStore(),
      devicePath,
      log: console,
    });
  }

  type DashboardHistoryMode = 'push' | 'replace' | 'none';

  interface DashboardHistoryState {
    xactRoute: 'dashboard';
    dashboardId: string;
    tabId: string;
    ui: {
      deviceName: string;
      deviceType: string;
      timeStart: number | null;
      timeEnd: number | null;
    };
  }

  function isBlankDashboardRef(dashboardId: string): boolean {
    return dashboardId.startsWith('__blank__');
  }

  function hasDashboardHash(dashboardId: string): boolean {
    return dashboardId !== 'dashboard-config-editor' && !isBlankDashboardRef(dashboardId);
  }

  function currentDashboardId(): string {
    return tabs.find(t => t.id === activeTabId)?.dashboardId ?? 'dashboard-config-editor';
  }

  function captureDashboardHistoryState(dashboardId: string): DashboardHistoryState {
    const ui = getUiStore();
    return {
      xactRoute: 'dashboard',
      dashboardId,
      tabId: activeTabId,
      ui: {
        deviceName: ui.get('deviceName') || '',
        deviceType: ui.get('deviceType') || '',
        timeStart: ui.get('timeStart'),
        timeEnd: ui.get('timeEnd'),
      },
    };
  }

  function readDashboardHistoryState(state: unknown): DashboardHistoryState | null {
    if (!state || typeof state !== 'object') return null;
    const candidate = state as Partial<DashboardHistoryState>;
    if (candidate.xactRoute !== 'dashboard' || typeof candidate.dashboardId !== 'string') return null;
    return {
      xactRoute: 'dashboard',
      dashboardId: candidate.dashboardId,
      tabId: typeof candidate.tabId === 'string' ? candidate.tabId : activeTabId,
      ui: {
        deviceName: typeof candidate.ui?.deviceName === 'string' ? candidate.ui.deviceName : '',
        deviceType: typeof candidate.ui?.deviceType === 'string' ? candidate.ui.deviceType : '',
        timeStart: typeof candidate.ui?.timeStart === 'number' ? candidate.ui.timeStart : null,
        timeEnd: typeof candidate.ui?.timeEnd === 'number' ? candidate.ui.timeEnd : null,
      },
    };
  }

  function sameHistoryState(a: DashboardHistoryState | null, b: DashboardHistoryState): boolean {
    return !!a
      && a.dashboardId === b.dashboardId
      && a.tabId === b.tabId
      && a.ui.deviceName === b.ui.deviceName
      && a.ui.deviceType === b.ui.deviceType
      && a.ui.timeStart === b.ui.timeStart
      && a.ui.timeEnd === b.ui.timeEnd;
  }

  function dashboardHistoryUrl(dashboardId: string): string {
    const hash = hasDashboardHash(dashboardId) ? `#${encodeURIComponent(dashboardId)}` : '';
    return `${window.location.pathname}${window.location.search}${hash}`;
  }

  function dashboardFromLocation(): string {
    const hash = window.location.hash.slice(1);
    if (!hash) return 'dashboard-config-editor';
    try {
      return decodeURIComponent(hash);
    } catch {
      return hash;
    }
  }

  function writeDashboardHistory(dashboardId: string, mode: DashboardHistoryMode): void {
    if (mode === 'none' || isBlankDashboardRef(dashboardId)) return;
    const state = captureDashboardHistoryState(dashboardId);
    const currentState = readDashboardHistoryState(history.state);
    if (mode === 'push' && sameHistoryState(currentState, state)) return;
    const url = dashboardHistoryUrl(dashboardId);
    if (mode === 'replace') {
      history.replaceState(state, '', url);
    } else {
      history.pushState(state, '', url);
    }
  }

  function applyDashboardHistoryState(state: DashboardHistoryState | null): void {
    if (!state) return;
    const ui = getUiStore();
    ui.set('deviceName', state.ui.deviceName);
    ui.set('deviceType', state.ui.deviceType);
    ui.set('timeStart', state.ui.timeStart);
    ui.set('timeEnd', state.ui.timeEnd);
  }

  function syncActiveDashboardState(dashboardId: string, title = dashboardId): void {
    const activeTab = tabs.find(t => t.id === activeTabId);
    if (activeTab) {
      activeTab.dashboardId = dashboardId;
      activeTab.title = resolveDashboardTitle(dashboardId, title);
    }
    sidebar?.setActiveItem(dashboardId);
    syncTabsToHeader();
    const isOnDashboard = hasDashboardHash(dashboardId);
    header?.setIsOnDashboard(isOnDashboard);
    if (isOnDashboard) {
      header?.setDashboardCapabilities({ canEdit: false, canInspect: false });
      header?.setDashboardMode('view');
    }
  }

  async function navigateToDashboard(dashboardId: string, mode: DashboardHistoryMode = 'push'): Promise<boolean> {
    if (!content) return false;
    const switched = await content.switchToDashboard(dashboardId);
    if (!switched) return false;
    syncActiveDashboardState(dashboardId);
    writeDashboardHistory(dashboardId, mode);
    return true;
  }

  async function activateTab(tabId: string, mode: DashboardHistoryMode = 'push'): Promise<boolean> {
    const tab = tabs.find(t => t.id === tabId);
    if (!tab) return false;
    const previousTabId = activeTabId;
    activeTabId = tabId;
    const dashboardId = resolveDashboardSelection(tab.dashboardId);
    const switched = await navigateToDashboard(dashboardId, mode);
    if (!switched) {
      activeTabId = previousTabId;
      syncTabsToHeader();
      return false;
    }
    return true;
  }

  // Create initial tab (Dashboards)
  const initialTab = createTab('dashboard-config-editor', 'Dashboards');
  activeTabId = initialTab.id;
  syncTabsToHeader();
  header?.setIsOnDashboard(false);

  // Handle dashboard changes from sidebar - navigate active tab
  sidebar?.addEventListener('dashboard-change', ((e: CustomEvent) => {
    const { dashboard } = e.detail;
    const dashboardRef = resolveDashboardSelection(dashboard);
    void navigateToDashboard(dashboardRef);
  }) as EventListener);

  content?.addEventListener('organisations-changed', (() => {
    sidebar?.refreshAuthState();
  }) as EventListener);

  // Handle dashboard shown events - update active tab title & header state
  content?.addEventListener('dashboard-shown', ((e: CustomEvent) => {
    const { title, dashboardId } = e.detail;
    syncActiveDashboardState(dashboardId, title);
  }) as EventListener);

  // Handle dashboard actions from the hamburger menu
  header?.addEventListener('dashboard-action', ((e: CustomEvent) => {
    const { action } = e.detail;
    if (action === 'toggle-edit') {
      content?.toggleEditMode();
    } else if (action === 'toggle-inspect') {
      content?.toggleInspectMode();
    }
  }) as EventListener);

  // ── Tab events from header ──
  header?.addEventListener('tab-select', ((e: CustomEvent) => {
    const { tabId } = e.detail;
    if (tabId !== activeTabId) {
      void activateTab(tabId);
    }
  }) as EventListener);

  header?.addEventListener('tab-close', ((e: CustomEvent) => {
    const { tabId } = e.detail;
    if (tabs.length <= 1) return; // keep at least one tab
    const idx = tabs.findIndex(t => t.id === tabId);
    if (idx === -1) return;
    tabs.splice(idx, 1);
    if (tabId === activeTabId) {
      // Activate adjacent tab (prefer right, fallback left)
      const nextTab = tabs[idx] || tabs[idx - 1];
      void activateTab(nextTab.id);
    } else {
      syncTabsToHeader();
    }
  }) as EventListener);

  let blankCounter = 0;
  header?.addEventListener('tab-add', (() => {
    const blankId = `__blank__${++blankCounter}`;
    const tab = createTab(blankId, 'New Tab');
    activeTabId = tab.id;
    content?.switchToDashboard(blankId);
    syncTabsToHeader();
    header?.setIsOnDashboard(false);
  }) as EventListener);

  // Reflect edit-mode changes from the active dashboard back to the header
  content?.addEventListener('dashboard-capabilities-changed', ((e: CustomEvent) => {
    header?.setDashboardCapabilities(e.detail);
  }) as EventListener);

  content?.addEventListener('dashboard-mode-changed', ((e: CustomEvent) => {
    header?.setDashboardMode(e.detail.mode);
  }) as EventListener);

  content?.addEventListener('edit-mode-changed', ((e: CustomEvent) => {
    if (e.detail.editing) header?.setDashboardMode('edit');
  }) as EventListener);

  // Load dashboard configuration from server
  editor = content?.querySelector('dashboard-config-editor') as import('./dashboards/dashboard-config-editor').DashboardConfigEditor | null;
  try {
    await loadDashboardMenuFromServer(editor, sidebar);
  } catch (err) {
    if (isAuthApiError(err)) {
      console.warn('XACT: Stored session was rejected by the server; login required.');
      await recoverFromStaleSession(app, header);
      await loadDashboardMenuFromServer(editor, sidebar);
    } else {
      console.error('XACT: Failed to load dashboards from server, using defaults:', err);
      if (editor) {
        sidebar?.setMenuItems(configsToMenuItems(editor.getConfigs()));
      }
    }
  }

  // URL hash router: restore the last active dashboard after dashboards are loaded
  const hashDashboard = window.location.hash.slice(1);
  if (hashDashboard) {
    const dashboardId = resolveDashboardSelection(decodeURIComponent(hashDashboard));
    // Navigate the initial tab to the restored dashboard
    await navigateToDashboard(dashboardId, 'replace');
  } else {
    syncActiveDashboardState(currentDashboardId());
    writeDashboardHistory(currentDashboardId(), 'replace');
  }

  window.addEventListener('popstate', (event: PopStateEvent) => {
    void (async () => {
      const beforeDashboardId = currentDashboardId();
      const beforeTabId = activeTabId;
      const state = readDashboardHistoryState(event.state);
      applyDashboardHistoryState(state);
      const stateDashboardId = state?.dashboardId ?? dashboardFromLocation();
      const restoredTabId = state?.tabId;
      if (restoredTabId && tabs.some(t => t.id === restoredTabId)) {
        activeTabId = restoredTabId;
      }
      const dashboardId = resolveDashboardSelection(stateDashboardId);
      const switched = await navigateToDashboard(dashboardId, 'none');
      if (!switched) {
        activeTabId = beforeTabId;
        syncActiveDashboardState(beforeDashboardId);
        writeDashboardHistory(beforeDashboardId, 'replace');
      }
    })();
  });

  // Sync sidebar menu with dashboard configuration and persist changes
  let saveTimeout: ReturnType<typeof setTimeout> | null = null;
  let saveQueue = Promise.resolve();
  document.addEventListener('config-change', ((e: CustomEvent) => {
    const { configs } = e.detail as { configs: DashboardConfig[] };
    const configsSnapshot = structuredClone(configs) as DashboardConfig[];
    sidebar?.setMenuItems(configsToMenuItems(configs));
    for (const tab of tabs) {
      tab.title = resolveDashboardTitle(tab.dashboardId, tab.title);
    }
    syncTabsToHeader();

    // Debounce save to server
    if (saveTimeout) clearTimeout(saveTimeout);
    saveTimeout = setTimeout(() => {
      saveQueue = saveQueue.then(() => saveDashboardConfigs(configsSnapshot, configs));
    }, 300);
  }) as EventListener);

  // Handle dashboard row click from config editor
  document.addEventListener('dashboard-open', ((e: CustomEvent) => {
    const { dashboard, devicePath } = e.detail as { dashboard: string; devicePath?: string };
    const requestedDashboard = dashboard || tabs.find(t => t.id === activeTabId)?.dashboardId || '';
    if (requestedDashboard && content) {
      setDeviceContextFromPath(devicePath);
      void navigateToDashboard(resolveDashboardSelection(requestedDashboard, devicePath));
      // sidebar update happens via dashboard-shown (after switch is confirmed)
    }
  }) as EventListener);

  // Handle sidebar toggle from header
  header?.addEventListener('toggle-sidebar', () => {
    app.classList.toggle('sidebar-collapsed');
  });

  // Update header with real user info
  const currentUser = getCurrentUser();
  if (currentUser && header) {
    header.setUser(currentUser.username);
  }

  // Handle user actions from header dropdown
  const prefsDialog = document.querySelector('preferences-dialog') as import('./components/preferences-dialog').PreferencesDialog;
  const profileDialog = document.querySelector('profile-dialog') as import('./components/profile-dialog').ProfileDialog;
  header?.addEventListener('user-action', ((e: CustomEvent) => {
    const { action } = e.detail;
    if (action === 'profile') {
      profileDialog?.open();
    } else if (action === 'preferences') {
      prefsDialog?.open();
    } else if (action === 'logout') {
      logout();
      header?.clearUser();
      window.location.reload();
    }
  }) as EventListener);

  // Update header username if profile is changed
  document.addEventListener('profile-updated', ((e: CustomEvent) => {
    const { user } = e.detail;
    if (user && header) {
      header.setUser(user.loginName);
    }
  }) as EventListener);

  // Handle responsive sidebar
  const mediaQuery = window.matchMedia('(max-width: 768px)');
  const handleResize = () => {
    app.classList.remove('sidebar-collapsed');
  };

  mediaQuery.addEventListener('change', handleResize);
  handleResize(); // Initial check
});
