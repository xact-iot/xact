import './styles.css';
import './dashboards/widgets/builtin-widgets';
import './dashboards/dashboard-container';
import { getMirrorStore } from './store/store';
import { getUiStore } from './store/ui-store';
import type { Dashboard, DashboardMeta } from './api';
import type { DashboardContainer } from './dashboards/dashboard-container';

type PublicValue = { value: unknown; units?: string; description?: string; status?: string; timestamp?: number };

const prefix = '/xact/public';
const parts = window.location.pathname.slice(prefix.length).split('/').filter(Boolean).map(decodeURIComponent);
const defaultOrgDashboard = parts.length === 1 && /^\d+$/.test(parts[0]);
const org = defaultOrgDashboard ? 'default' : parts[0] || 'default';
const requestedId = defaultOrgDashboard ? parts[0] : parts[1] || '';
const api = '/xact/api/v1/public';
const app = document.getElementById('app');
const loading = document.getElementById('loading');
document.body.dataset.publicDashboard = 'true';

function showError(message: string): void {
  if (!app) return;
  app.replaceChildren();
  const box = document.createElement('div');
  box.style.cssText = 'margin:auto;padding:24px;color:var(--content-text);font:16px system-ui,sans-serif;';
  box.textContent = message;
  app.appendChild(box);
}

async function getJSON<T>(url: string): Promise<T> {
  const response = await fetch(url, { cache: 'no-store' });
  if (!response.ok) throw new Error(String(response.status));
  return response.json() as Promise<T>;
}

function fitPublicMaps(dashboard: Dashboard, values: Record<string, PublicValue>): void {
  const points: Array<[number, number]> = [];
  for (const [path, entry] of Object.entries(values)) {
    if (!path.endsWith('.meta.lat')) continue;
    const lat = Number(entry.value);
    const lon = Number(values[path.slice(0, -3) + 'lon']?.value);
    if (Number.isFinite(lat) && Number.isFinite(lon)) points.push([lat, lon]);
  }
  if (!points.length) return;
  const lats = points.map(p => p[0]);
  const lons = points.map(p => p[1]);
  const south = Math.min(...lats), north = Math.max(...lats);
  const west = Math.min(...lons), east = Math.max(...lons);
  const latPad = Math.max((north - south) * 0.1, 0.02);
  const lonPad = Math.max((east - west) * 0.1, 0.02);
  for (const widget of dashboard.widgets) {
    if (widget.type === 'area-map-widget' && !widget.config?.savedBounds) {
      widget.config = {
        ...widget.config,
        savedBounds: { south: south - latPad, north: north + latPad, west: west - lonPad, east: east + lonPad },
      };
    }
  }
}

async function start(): Promise<void> {
  if (!app) return;
  app.style.cssText = 'display:flex;flex-direction:column;height:100vh;width:100vw;overflow:hidden;background:var(--content-bg);color:var(--content-text);';
  getUiStore().set('orgName', org);
  getUiStore().set('deviceName', '');
  getUiStore().set('deviceType', '');

  let dashboards: DashboardMeta[];
  try {
    const listUrl = org === 'default' ? api + '/dashboards' : api + '/' + encodeURIComponent(org) + '/dashboards';
    dashboards = await getJSON<DashboardMeta[]>(listUrl);
  } catch {
    showError('Public dashboards are unavailable.');
    loading?.remove();
    return;
  }

  if (dashboards.length === 0) {
    showError('No public dashboards are available for this organisation.');
    loading?.remove();
    return;
  }

  const chosen = requestedId ? dashboards.find(d => String(d.id) === requestedId) : dashboards[0];
  if (!chosen) {
    showError('This public dashboard is unavailable.');
    loading?.remove();
    return;
  }

  const base = api + '/' + encodeURIComponent(org) + '/dashboards/' + chosen.id;
  let dashboard: Dashboard;
  let values: Record<string, PublicValue>;
  try {
    [dashboard, values] = await Promise.all([
      getJSON<Dashboard>(base),
      getJSON<Record<string, PublicValue>>(base + '/data'),
    ]);
  } catch {
    showError('This public dashboard is unavailable.');
    loading?.remove();
    return;
  }

  fitPublicMaps(dashboard, values);
  getMirrorStore().applyPublicSnapshot(org, values);
  document.title = dashboard.name + ' · XACT';

  const header = document.createElement('header');
  header.style.cssText = 'flex:none;padding:12px 18px;border-bottom:1px solid var(--border-color);background:var(--widget-bg);display:flex;align-items:center;gap:18px;min-height:52px;box-sizing:border-box;';
  const title = document.createElement('strong');
  title.textContent = dashboard.name;
  title.style.cssText = 'font-size:16px;white-space:nowrap;';
  header.appendChild(title);
  const nav = document.createElement('nav');
  nav.setAttribute('aria-label', 'Public dashboards');
  nav.style.cssText = 'display:flex;gap:12px;overflow:auto;white-space:nowrap;';
  for (const item of dashboards) {
    const link = document.createElement('a');
    link.href = prefix + '/' + encodeURIComponent(org) + '/' + item.id;
    link.textContent = item.name;
    link.style.cssText = 'font-size:13px;color:var(--content-text);opacity:' + (item.id === chosen.id ? '1' : '0.65') + ';text-decoration:none;';
    if (item.id === chosen.id) link.setAttribute('aria-current', 'page');
    nav.appendChild(link);
  }
  header.appendChild(nav);
  const content = document.createElement('main');
  content.style.cssText = 'display:flex;flex:1;min-height:0;overflow:hidden;';
  const container = document.createElement('dashboard-container') as DashboardContainer;
  content.appendChild(container);
  app.replaceChildren(header, content);
  loading?.remove();
  await container.loadPublicDashboard(dashboard);

  let polling = false;
  const timer = window.setInterval(async () => {
    if (polling) return;
    polling = true;
    try {
      const next = await getJSON<Record<string, PublicValue>>(base + '/data');
      const added = getMirrorStore().applyPublicSnapshot(org, next);
      if (added) {
        for (const map of container.querySelectorAll<any>('area-map-widget')) {
          void map.refreshLayers?.();
        }
      }
    } catch (error) {
      if ((error as Error).message === '404') {
        window.clearInterval(timer);
        showError('This public dashboard is no longer available.');
      }
    } finally {
      polling = false;
    }
  }, 5000);
  window.addEventListener('beforeunload', () => window.clearInterval(timer), { once: true });
}

void start();
