export interface DashboardSelectionNode {
  id?: string;
  serverId?: number;
  name: string;
  variation?: string;
  children?: DashboardSelectionNode[];
}

interface UiStoreLike {
  get(key: string): unknown;
}

interface MirrorStoreLike {
  getOrg?(): string;
  toAbsolute?(path: string): string;
  nodeExists?(path: string): boolean;
  listChildrenNames(path: string): string[];
  getNodeValue(path: string): unknown;
  resolveTagReference?(path: string): unknown;
}

interface LogLike {
  log(message?: unknown, ...optionalParams: unknown[]): void;
}

interface ResolveOptions {
  store?: MirrorStoreLike;
  ui?: UiStoreLike;
  devicePath?: string;
  log?: LogLike;
}

const DEVICE_SUBTYPE_TAGS = ['deviceSubtype', 'subtype', 'variation'];
const DEVICE_SEARCH_LIMIT = 50_000;

export function resolveDashboardForDeviceSubtype(
  configs: DashboardSelectionNode[],
  requestedRef: string,
  options: ResolveOptions = {},
): string {
  const dashboards = flattenDashboards(configs);
  const requested = dashboards.find(dashboard => matchesDashboardRef(dashboard, requestedRef));
  if (!requested) return requestedRef;

  const sameName = dashboards.filter(dashboard => dashboard.name === requested.name);
  if (sameName.length <= 1) return dashboardRef(requested);

  const subtype = getCurrentDeviceSubtype(options);
  const normalisedSubtype = normaliseSubtype(subtype);
  const match = normalisedSubtype
    ? sameName.find(dashboard => normaliseSubtype(dashboard.variation) === normalisedSubtype)
    : undefined;

  if (match) return dashboardRef(match);

  const fallback = sameName[0];
  options.log?.log(
    `XACT: dashboard "${requested.name}" has variations, but no dashboard matches ` +
    `device "${currentDeviceName(options.ui) || '(unset)'}" subtype "${subtype || '(unset)'}"; ` +
    `displaying first variation "${fallback.variation || '(none)'}".`,
  );
  return dashboardRef(fallback);
}

function flattenDashboards(configs: DashboardSelectionNode[]): DashboardSelectionNode[] {
  const dashboards: DashboardSelectionNode[] = [];
  for (const config of configs) {
    if (config.children) {
      dashboards.push(...flattenDashboards(config.children));
    } else {
      dashboards.push(config);
    }
  }
  return dashboards;
}

function dashboardRef(dashboard: DashboardSelectionNode): string {
  if (dashboard.serverId !== undefined) return String(dashboard.serverId);
  return String(dashboard.id || dashboard.name);
}

function matchesDashboardRef(dashboard: DashboardSelectionNode, ref: string): boolean {
  const requested = String(ref);
  return dashboardRef(dashboard) === requested
    || String(dashboard.serverId ?? '') === requested
    || String(dashboard.id ?? '') === requested
    || dashboard.name === requested;
}

function getCurrentDeviceSubtype(options: ResolveOptions): string {
  const store = options.store;
  if (!store) return '';

  const devicePath = resolveDevicePath(options);
  if (!devicePath) return '';

  for (const tag of DEVICE_SUBTYPE_TAGS) {
    const path = `${devicePath}.meta.${tag}`;
    const value = store.resolveTagReference ? store.resolveTagReference(path) : store.getNodeValue(path);
    const text = value == null ? '' : String(value).trim();
    if (text) return text;
  }

  return '';
}

function resolveDevicePath(options: ResolveOptions): string {
  const store = options.store;
  if (!store) return '';

  if (options.devicePath) {
    const absolute = toAbsolute(store, options.devicePath);
    if (isDevicePath(store, absolute)) return absolute;
  }

  const deviceName = currentDeviceName(options.ui);
  if (!deviceName) return '';

  const deviceType = String(options.ui?.get('deviceType') ?? '').trim();
  const candidates = [
    deviceType ? `${deviceType}.${deviceName}` : '',
    deviceName,
  ].filter(Boolean);

  for (const candidate of candidates) {
    const absolute = toAbsolute(store, candidate);
    if (isDevicePath(store, absolute)) return absolute;
  }

  return findDevicePathByName(store, deviceName, String(options.ui?.get('orgName') ?? '').trim());
}

function findDevicePathByName(store: MirrorStoreLike, deviceName: string, orgName: string): string {
  const org = store.getOrg?.() || orgName;
  const roots = org ? [org] : store.listChildrenNames('');
  const queue = [...roots];
  let visited = 0;

  while (queue.length > 0 && visited < DEVICE_SEARCH_LIMIT) {
    const path = queue.shift()!;
    visited++;

    const children = store.listChildrenNames(path);
    if (path.split('.').pop() === deviceName && children.includes('meta')) return path;

    for (const child of children) {
      if (child === 'meta') continue;
      queue.push(path ? `${path}.${child}` : child);
    }
  }

  return '';
}

function isDevicePath(store: MirrorStoreLike, path: string): boolean {
  if (!path) return false;
  if (store.nodeExists && !store.nodeExists(path)) return false;
  return store.listChildrenNames(path).includes('meta');
}

function toAbsolute(store: MirrorStoreLike, path: string): string {
  const trimmed = path.trim();
  return store.toAbsolute ? store.toAbsolute(trimmed) : trimmed;
}

function currentDeviceName(ui?: UiStoreLike): string {
  return String(ui?.get('deviceName') ?? '').trim();
}

function normaliseSubtype(value: unknown): string {
  return String(value ?? '').trim().toLowerCase().replace(/[\s_]+/g, '-');
}
