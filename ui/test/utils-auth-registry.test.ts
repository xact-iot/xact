import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  getAuthHeaders,
  getAuthToken,
  getBootstrapAdminStatus,
  getCurrentUser,
  initializeAuth,
  isAuthenticated,
  login,
  logout,
  setBootstrapAdminPassword,
} from '../src/auth';
import { getUiStore } from '../src/store/ui-store';
import { formatUnixMillis } from '../src/utils/time';
import { sanitizeHtml } from '../src/utils/html-sanitize';
import { getIconSVG, isIconSetLoaded, loadIconSet, preloadIconSet, searchIcons } from '../src/utils/icons';
import {
  ensureWidgetTypeLoaded,
  ensureWidgetTypesLoaded,
  getAvailableWidgets,
  getWidgetMeta,
  getWidgetsByCategory,
  registerWidgetType,
  registerWidgetTypes,
} from '../src/dashboards/widgets/widget-registry';

function response(body: any, ok = true, status = 200): Response {
  return {
    ok,
    status,
    json: vi.fn(async () => body),
  } as unknown as Response;
}

function jwt(payload: Record<string, any>): string {
  return ['header', btoa(JSON.stringify(payload)), 'sig'].join('.');
}

describe('auth helpers', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    logout();
  });

  it('logs in, stores auth state, builds headers, and logs out', async () => {
    const token = jwt({ exp: Math.floor(Date.now() / 1000) + 3600, tenant_id: 'default' });
    const auth = {
      token,
      token_type: 'Bearer',
      expires_in: 3600,
      user: { id: '1', username: 'admin', tenant_id: 'default', roles: ['Admin'], allowed_orgs: ['default'] },
    };
    const fetchMock = vi.fn(async () => response(auth));
    vi.stubGlobal('fetch', fetchMock);

    await expect(login('admin', 'pw')).resolves.toEqual(auth);
    expect(fetchMock).toHaveBeenCalledWith('/xact/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: 'admin', password: 'pw' }),
    });
    expect(getAuthToken()).toBe(token);
    expect(getAuthHeaders()).toEqual({ 'Content-Type': 'application/json', Authorization: `Bearer ${token}` });
    expect(isAuthenticated()).toBe(true);
    expect(getCurrentUser()?.tenant_id).toBe('default');

    logout();
    expect(getAuthToken()).toBeNull();
    expect(getAuthHeaders()).toEqual({ 'Content-Type': 'application/json' });
  });

  it('handles bootstrap admin endpoints and error bodies', async () => {
    const fetchMock = vi.fn(async () => response({ setupRequired: true, passwordSet: false }));
    vi.stubGlobal('fetch', fetchMock);
    await expect(getBootstrapAdminStatus()).resolves.toEqual({ setupRequired: true, passwordSet: false });

    const auth = {
      token: jwt({ exp: Math.floor(Date.now() / 1000) + 3600, tenant_id: 'default' }),
      token_type: 'Bearer',
      expires_in: 3600,
      user: { id: '1', username: 'admin', tenant_id: 'default', roles: ['SystemAdmin'], allowed_orgs: ['default'] },
    };
    fetchMock.mockResolvedValueOnce(response(auth));
    await expect(setBootstrapAdminPassword('new-password')).resolves.toEqual(auth);
    expect(fetchMock).toHaveBeenLastCalledWith('/xact/api/v1/bootstrap/admin/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: 'new-password' }),
    });

    fetchMock.mockResolvedValueOnce(response({ error: 'nope' }, false, 401));
    await expect(login('admin', 'bad')).rejects.toThrow('nope');
  });

  it('initializes only valid tokens and clears expired or malformed state', () => {
    localStorage.setItem('xact_auth_token', jwt({ exp: Math.floor(Date.now() / 1000) + 3600, tenant_id: 'default' }));
    expect(initializeAuth()).toBe(true);

    localStorage.setItem('xact_auth_token', jwt({ exp: Math.floor(Date.now() / 1000) - 1, tenant_id: 'default' }));
    expect(initializeAuth()).toBe(false);
    expect(localStorage.getItem('xact_auth_token')).toBeNull();

    localStorage.setItem('xact_auth_token', 'not-a-jwt');
    expect(isAuthenticated()).toBe(false);
  });
});

describe('small utilities', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('formats unix milliseconds with invalid and timezone fallbacks', () => {
    expect(formatUnixMillis(null)).toBeNull();
    expect(formatUnixMillis('')).toBeNull();
    expect(formatUnixMillis('nope')).toBeNull();
    expect(formatUnixMillis(BigInt(0), 'UTC')).toContain('1970-01-01');
    expect(formatUnixMillis(0, 'Not/AZone')).toMatch(/\d{4}-\d{2}-\d{2}/);
  });

  it('sanitizes dangerous markup while keeping safe content', () => {
    const html = sanitizeHtml(`
      <p onclick="alert(1)" style="color:red">Hello <a href="javascript:alert(1)">bad</a><a href="/safe">safe</a></p>
      <img srcdoc="<script></script>" src="data:text/html,bad">
      <script>alert(1)</script>
    `);
    expect(html).toContain('Hello');
    expect(html).toContain('href="/safe"');
    expect(html).not.toContain('onclick');
    expect(html).not.toContain('javascript:');
    expect(html).not.toContain('script');

    const stripped = sanitizeHtml('<div><b>Keep</b><span>Text</span></div>', { allowedTags: new Set(['b']) });
    expect(stripped).toBe('<b>Keep</b>Text');
  });

  it('loads, searches, and renders cached icons', async () => {
    const fetchMock = vi.fn(async () => response({
      prefix: 'test-icons',
      width: 16,
      height: 16,
      icons: { pump: { body: '<path d="M0 0h1v1z"/>' } },
      aliases: { water: { parent: 'pump' } },
    }));
    vi.stubGlobal('fetch', fetchMock);

    const first = loadIconSet('test-icons');
    const second = loadIconSet('test-icons');
    await Promise.all([first, second]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(isIconSetLoaded('test-icons')).toBe(true);
    expect(searchIcons('wa', 'test-icons')).toEqual([{ name: 'test-icons:water', prefix: 'test-icons', iconName: 'water' }]);
    expect(getIconSVG('test-icons:water', '"red"&', 20)).toContain('&quot;red&quot;&amp;');
    expect(getIconSVG('missing:pump')).toBe('');

    preloadIconSet('test-icons');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe('ui store and widget registry', () => {
  it('notifies subscribers immediately for meaningful values and supports unsubscribe', () => {
    const store = getUiStore();
    store.set('deviceName', '');
    const seen: string[] = [];
    const unsubscribe = store.subscribe('deviceName', value => seen.push(value));
    expect(seen).toEqual([]);
    store.set('deviceName', 'Pump1');
    unsubscribe();
    store.set('deviceName', 'Pump2');
    expect(seen).toEqual(['Pump1']);

    const immediate: string[] = [];
    store.subscribe('deviceName', value => immediate.push(value))();
    expect(immediate).toEqual(['Pump2']);
  });

  it('registers widgets, preserves existing loaders, groups categories, and deduplicates lazy loads', async () => {
    const load = vi.fn(async () => undefined);
    registerWidgetType({ type: 'x-test-widget', name: 'Test', icon: 'mdi:test', category: 'Custom', defaultW: 2, defaultH: 3, load });
    registerWidgetType({ type: 'x-test-widget', name: 'Renamed', icon: 'mdi:new', category: 'Custom', defaultW: 4, defaultH: 5 });
    registerWidgetTypes([{ type: 'x-general-widget', name: 'General', icon: 'mdi:view', category: 'General', defaultW: 1, defaultH: 1 }]);

    expect(getWidgetMeta('x-test-widget')?.name).toBe('Renamed');
    expect(getAvailableWidgets().some(widget => widget.type === 'x-general-widget')).toBe(true);
    expect(getWidgetsByCategory().get('Custom')?.some(widget => widget.type === 'x-test-widget')).toBe(true);

    await Promise.all([ensureWidgetTypeLoaded('x-test-widget'), ensureWidgetTypeLoaded('x-test-widget')]);
    expect(load).toHaveBeenCalledTimes(1);
    await ensureWidgetTypesLoaded(['', 'x-test-widget', 'x-general-widget', 'x-general-widget']);
    expect(load).toHaveBeenCalledTimes(2);
  });
});
