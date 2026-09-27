import { beforeEach, expect, it, vi } from 'vitest';

const state = vi.hoisted(() => ({
  load: vi.fn(async () => {}),
  snapshot: vi.fn(() => false),
  uiSet: vi.fn(),
}));

vi.mock('../src/dashboards/widgets/builtin-widgets', () => ({}));
vi.mock('../src/dashboards/dashboard-container', () => {
  if (!customElements.get('dashboard-container')) {
    customElements.define('dashboard-container', class extends HTMLElement {
      loadPublicDashboard = state.load;
    });
  }
  return {};
});
vi.mock('../src/store/store', () => ({ getMirrorStore: () => ({ applyPublicSnapshot: state.snapshot }) }));
vi.mock('../src/store/ui-store', () => ({ getUiStore: () => ({ set: state.uiSet }) }));

beforeEach(() => {
  vi.clearAllMocks();
  vi.spyOn(window, 'setInterval').mockImplementation(() => 1);
  history.replaceState(null, '', '/xact/public/42');
  document.body.innerHTML = '<div id="loading"></div><div id="app"></div>';
});

it('opens a public dashboard without an org using default and skips the login UI', async () => {
  const requests: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    requests.push(url);
    let body: unknown;
    if (url.endsWith('/dashboards')) {
      body = [{ id: 42, name: 'Public status', isPublic: true }];
    } else if (url.endsWith('/data')) {
      body = { 'default.device.value': { value: 23 } };
    } else {
      body = { id: 42, name: 'Public status', widgets: [], isPublic: true };
    }
    return { ok: true, json: async () => body };
  }));

  await import('../src/public-page');
  await new Promise(resolve => setTimeout(resolve, 0));
  expect(requests).toEqual([
    '/xact/api/v1/public/dashboards',
    '/xact/api/v1/public/default/dashboards/42',
    '/xact/api/v1/public/default/dashboards/42/data',
  ]);
  expect(state.snapshot).toHaveBeenCalledWith('default', { 'default.device.value': { value: 23 } });
  expect(state.load).toHaveBeenCalledWith(expect.objectContaining({ id: 42, name: 'Public status' }));
  expect(document.querySelector('login-page')).toBeNull();
  expect(document.querySelector('app-header')).toBeNull();
  expect(document.querySelector('nav a')?.getAttribute('href')).toBe('/xact/public/default/42');
});
