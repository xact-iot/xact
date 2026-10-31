import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

let bearer: string | null = 'session-token';
vi.mock('../src/auth', () => ({
  getAuthToken: () => bearer,
  getAuthHeaders: () => (bearer ? { 'Content-Type': 'application/json', Authorization: `Bearer ${bearer}` } : {}),
  getCurrentUser: () => ({ tenant_id: 'default' }),
}));
vi.mock('../src/store/store', () => ({ getMirrorStore: () => ({}) }));
vi.mock('../src/permissions/registry', () => ({ registerPermissions: () => {} }));

import { applications, loadApplications } from '../src/plugins/applications';

const manifests = [
  {
    id: 'traffic', name: 'Traffic', widgets: [], operations: ['get_status'],
    resource: { id: 'traffic-studio', description: '', permissions: [] },
    http: { prefix: 'traffic', max_body_mb: 200, websocket_paths: ['/ws'] },
  },
  {
    id: 'public_bus', name: 'Bus', widgets: [], operations: ['get_status'],
    resource: { id: 'public-bus-config', description: '', permissions: [] },
  },
];

describe('application proxy helpers', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    bearer = 'session-token';
    fetchMock = vi.fn(async () => ({ ok: true, status: 200, json: async () => manifests }) as unknown as Response);
    vi.stubGlobal('fetch', fetchMock);
    await loadApplications();
    fetchMock.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('builds absolute http and ws URLs below the application prefix', () => {
    const origin = window.location.origin;
    expect(applications.proxyUrl('traffic', '/api/v1/networks?bbox=1,2')).toBe(`${origin}/xact/traffic/api/v1/networks?bbox=1,2`);
    expect(applications.proxyUrl('traffic', '/ws?ticket=abc')).toBe(`${origin.replace(/^http/, 'ws')}/xact/traffic/ws?ticket=abc`);
    expect(applications.proxyUrl('traffic', '/ws/other')).toMatch(/^http/);
  });

  it('rejects paths outside the prefix and applications without a proxy', () => {
    expect(() => applications.proxyUrl('traffic', 'api/v1/networks')).toThrow('must start with /');
    expect(() => applications.proxyUrl('traffic', '/../api/v1/users')).toThrow('leaves the application prefix');
    expect(() => applications.proxyUrl('traffic', '/%2e%2e/api/v1/users')).toThrow('leaves the application prefix');
    expect(() => applications.proxyUrl('public_bus', '/x')).toThrow('no HTTP proxy');
    expect(() => applications.proxyUrl('unknown', '/x')).toThrow('no HTTP proxy');
  });

  it('fetches through the proxy with the current Bearer token', async () => {
    fetchMock.mockResolvedValueOnce({ ok: false, status: 502 } as Response);
    const body = new Blob(['osm']);
    const res = await applications.fetch('traffic', '/api/v1/uploads', {
      method: 'POST', body, headers: { 'Content-Type': 'application/xml', Authorization: 'Bearer stale' },
    });
    expect(res.status).toBe(502);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(`${window.location.origin}/xact/traffic/api/v1/uploads`);
    const headers = init.headers as Headers;
    expect(headers.get('Authorization')).toBe('Bearer session-token');
    expect(headers.get('Content-Type')).toBe('application/xml');
    expect(init).toMatchObject({ method: 'POST', body, credentials: 'omit', cache: 'no-store' });
  });

  it('refuses to fetch without a session', async () => {
    bearer = null;
    await expect(applications.fetch('traffic', '/api/v1/networks')).rejects.toThrow('Session expired');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
