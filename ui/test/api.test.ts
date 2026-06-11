import { afterEach, describe, expect, it, vi } from 'vitest';
import * as api from '../src/api';
import { queryEvents, setAuthHeadersProvider } from '../src/api';

function mockResponse(body: any = {}, init: { ok?: boolean; status?: number; text?: string; blob?: Blob } = {}): Response {
  return {
    ok: init.ok ?? true,
    status: init.status ?? 200,
    json: vi.fn(async () => body),
    text: vi.fn(async () => init.text ?? (typeof body === 'string' ? body : JSON.stringify(body))),
    blob: vi.fn(async () => init.blob ?? new Blob(['pdf'], { type: 'application/pdf' })),
  } as unknown as Response;
}

function stubFetch(response: Response = mockResponse({ ok: true })) {
  const fetchMock = vi.fn(async () => response);
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function lastFetch(fetchMock: ReturnType<typeof vi.fn>) {
  return fetchMock.mock.calls.at(-1) as [string, RequestInit | undefined];
}

const eventRows = [{
  id: 1,
  timestamp: '2026-05-26T12:00:00Z',
  severity: 'INFO',
  device: 'system',
  message: 'ready',
}];

describe('queryEvents', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    setAuthHeadersProvider(() => ({}));
    localStorage.clear();
  });

  it('shares identical concurrent logs requests', async () => {
    let resolveFetch!: () => void;
    const fetchPromise = new Promise<Response>(resolve => {
      resolveFetch = () => resolve({
        ok: true,
        json: async () => eventRows,
      } as Response);
    });
    const fetchMock = vi.fn(() => fetchPromise);
    vi.stubGlobal('fetch', fetchMock);

    const first = queryEvents({ limit: 1000 });
    const second = queryEvents({ limit: 1000 });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith('/xact/api/v1/logs?limit=1000', { headers: {} });

    resolveFetch();
    await expect(first).resolves.toEqual(eventRows);
    await expect(second).resolves.toEqual(eventRows);
  });

  it('starts a new request after the previous identical request settles', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => eventRows,
    } as Response));
    vi.stubGlobal('fetch', fetchMock);

    await queryEvents({ limit: 1000 });
    await queryEvents({ limit: 1000 });

    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});

describe('REST API wrappers', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    setAuthHeadersProvider(() => ({}));
    localStorage.clear();
  });

  it('fetches health and NATS config with expected URLs and headers', async () => {
    const fetchMock = stubFetch(mockResponse({ status: 'healthy' }));
    await expect(api.fetchHealth()).resolves.toEqual({ status: 'healthy' });
    expect(fetchMock).toHaveBeenLastCalledWith('/xact/health');

    setAuthHeadersProvider(() => ({ Authorization: 'Bearer token' }));
    fetchMock.mockResolvedValueOnce(mockResponse({ username: 'nats' }));
    await expect(api.fetchNATSConfig()).resolves.toEqual({ username: 'nats' });
    expect(fetchMock).toHaveBeenLastCalledWith('/xact/api/v1/system/nats-config', {
      headers: { Authorization: 'Bearer token' },
    });
  });

  it('builds node and tag paths relative to the current organisation', async () => {
    localStorage.setItem('xact_auth_user', JSON.stringify({ tenant_id: 'default' }));
    const fetchMock = stubFetch(mockResponse({ name: 'node' }));

    await api.loadNode('default.Building.Floor', 2);
    expect(fetchMock).toHaveBeenLastCalledWith('/xact/api/v1/nodes/Building/Floor?depth=2', { headers: {} });

    await api.loadTag('/default/Device/Tag');
    expect(fetchMock).toHaveBeenLastCalledWith('/xact/api/v1/tags/Device/Tag', { headers: {} });

    await api.createNode('default.Devices', 'Device', true);
    let [url, init] = lastFetch(fetchMock);
    expect(url).toBe('/xact/api/v1/nodes/');
    expect(init?.method).toBe('POST');
    expect(JSON.parse(String(init?.body))).toEqual({ path: '/Devices', nodeType: 'Device', isArray: true });

    await api.createTag('default.Devices.Pump.Mode', 'enum', {
      description: 'Mode',
      units: 'state',
      deadband: 1,
      enumValues: { 0: 'Off', 1: 'On' },
    });
    [, init] = lastFetch(fetchMock);
    const tagBody = JSON.parse(String(init?.body));
    expect(tagBody.path).toBe('/Devices/Pump/Mode');
    expect(tagBody.type).toBe(api.ScalarType.Enum);
    expect(tagBody.shared).toEqual({ description: 'Mode', units: 'state', deadband: 1, enumValues: { 0: 'Off', 1: 'On' } });

    await api.updateTagValue('default.Devices.Pump.Mode', 1);
    [, init] = lastFetch(fetchMock);
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(String(init?.body))).toEqual({ value: 1 });
  });

  it('covers tag update, debug, node update, and delete error paths', async () => {
    const fetchMock = stubFetch(mockResponse({ updated: true }));
    await expect(api.updateTag('Pump.Temp', { description: 'Temperature', pipeline: [] })).resolves.toEqual({ updated: true });
    expect(lastFetch(fetchMock)[1]?.method).toBe('PUT');

    fetchMock.mockResolvedValueOnce(mockResponse({ steps: [], finalOutput: 10, blockCount: 0 }));
    await expect(api.debugTagPipeline('Pump.Temp', 10)).resolves.toEqual({ steps: [], finalOutput: 10, blockCount: 0 });
    expect(lastFetch(fetchMock)[0]).toBe('/xact/api/v1/debug/tags/Pump/Temp');

    await expect(api.updateNode('Pump', { description: 'Pump node', isDevice: true })).resolves.toEqual({ updated: true });

    fetchMock.mockResolvedValueOnce(mockResponse({ error: 'cannot delete' }, { ok: false, status: 409 }));
    await expect(api.deleteNode('Pump')).rejects.toThrow('cannot delete');

    fetchMock.mockResolvedValueOnce(mockResponse({}, { ok: false, status: 404 }));
    await expect(api.deleteTag('Pump.Temp')).rejects.toThrow('HTTP 404');
  });

  it('covers dashboard and permission wrappers', async () => {
    const fetchMock = stubFetch(mockResponse([{ id: 1, name: 'Main' }]));
    await expect(api.listDashboards()).resolves.toEqual([{ id: 1, name: 'Main' }]);

    fetchMock.mockResolvedValueOnce(mockResponse({ error: 'unauthorized' }, { ok: false, status: 401 }));
    await expect(api.listDashboards()).rejects.toMatchObject({ name: 'ApiError', status: 401 });

    fetchMock.mockResolvedValueOnce(mockResponse([{ id: 1, name: 'Main' }]));
    await expect(api.getDashboard(1)).resolves.toEqual([{ id: 1, name: 'Main' }]);

    fetchMock.mockResolvedValueOnce(mockResponse({ id: 2, name: 'New' }));
    await expect(api.createDashboard({ name: 'New' } as any)).resolves.toEqual({ id: 2, name: 'New' });
    expect(lastFetch(fetchMock)[1]?.method).toBe('POST');

    await api.updateDashboard(2, { description: 'changed' });
    expect(lastFetch(fetchMock)[0]).toBe('/xact/api/v1/dashboards/2');
    expect(lastFetch(fetchMock)[1]?.method).toBe('PUT');

    await api.deleteDashboard(2);
    expect(lastFetch(fetchMock)[1]?.method).toBe('DELETE');

    await expect(api.fetchMyPermissions()).resolves.toEqual([{ id: 1, name: 'Main' }]);
    await expect(api.fetchAllRolePermissions()).resolves.toEqual([{ id: 1, name: 'Main' }]);
    await api.updateRolePermissions('Admin/User', { ui: {}, server: {} });
    expect(lastFetch(fetchMock)[0]).toBe('/xact/api/v1/permissions/roles/Admin%2FUser');
  });

  it('covers user and profile wrappers including JSON error bodies', async () => {
    const fetchMock = stubFetch(mockResponse([{ id: 1, loginName: 'admin' }]));
    await expect(api.listUsers()).resolves.toEqual([{ id: 1, loginName: 'admin' }]);
    await expect(api.getUser(1)).resolves.toEqual([{ id: 1, loginName: 'admin' }]);

    fetchMock.mockResolvedValueOnce(mockResponse({ id: 2, loginName: 'op' }));
    await expect(api.createUser({
      firstName: 'Op', lastName: 'One', loginName: 'op', email: 'op@example.test', password: 'pw', roles: ['Operator'],
    })).resolves.toEqual({ id: 2, loginName: 'op' });

    fetchMock.mockResolvedValueOnce(mockResponse({ id: 2, active: false }));
    await expect(api.updateUser(2, { active: false })).resolves.toEqual({ id: 2, active: false });
    await expect(api.resetUserPassword(2)).resolves.toEqual([{ id: 1, loginName: 'admin' }]);
    await expect(api.listRoles()).resolves.toEqual([{ id: 1, loginName: 'admin' }]);
    await expect(api.getMyProfile()).resolves.toEqual([{ id: 1, loginName: 'admin' }]);

    fetchMock.mockResolvedValueOnce(mockResponse({ error: 'email exists' }, { ok: false, status: 409 }));
    await expect(api.updateMyProfile({ email: 'taken@example.test' })).rejects.toThrow('email exists');

    fetchMock.mockResolvedValueOnce(mockResponse({ error: 'bad password' }, { ok: false, status: 400 }));
    await expect(api.changeMyPassword('old', 'new')).rejects.toThrow('bad password');
  });

  it('covers organisation and API key wrappers', async () => {
    const fetchMock = stubFetch(mockResponse([{ name: 'default' }]));
    await expect(api.listOrganisations()).resolves.toEqual([{ name: 'default' }]);
    await expect(api.getOrganisation('Plant A')).resolves.toEqual([{ name: 'default' }]);
    expect(lastFetch(fetchMock)[0]).toBe('/xact/api/v1/organisations/Plant%20A');

    fetchMock.mockResolvedValueOnce(mockResponse({ name: 'plant' }));
    await expect(api.createOrganisation({ name: 'plant', active: true })).resolves.toEqual({ name: 'plant' });

    fetchMock.mockResolvedValueOnce(mockResponse({ name: 'plant', active: false }));
    await expect(api.updateOrganisation('plant', { active: false })).resolves.toEqual({ name: 'plant', active: false });

    fetchMock.mockResolvedValueOnce(mockResponse({}, { ok: false, status: 400, text: 'cannot delete default' }));
    await expect(api.deleteOrganisation('default')).rejects.toThrow('cannot delete default');

    fetchMock.mockResolvedValueOnce(mockResponse([{ id: 1, key: 'masked' }]));
    await expect(api.listAPIKeys()).resolves.toEqual([{ id: 1, key: 'masked' }]);
    fetchMock.mockResolvedValueOnce(mockResponse({ id: 2, key: 'raw' }));
    await expect(api.createAPIKey('ingest')).resolves.toEqual({ id: 2, key: 'raw' });
    await api.deleteAPIKey(2);
    expect(lastFetch(fetchMock)[0]).toBe('/xact/api/v1/api-keys/2');
  });

  it('covers reports, events, notifications, tag calcs, and scheduler wrappers', async () => {
    const pdf = new Blob(['pdf'], { type: 'application/pdf' });
    const fetchMock = stubFetch(mockResponse({ id: 'tpl' }, { blob: pdf }));

    await expect(api.loadBlockSchemas()).resolves.toEqual({ id: 'tpl' });
    await expect(api.listPDFTemplates()).resolves.toEqual({ id: 'tpl' });
    await expect(api.getPDFTemplate('tpl')).resolves.toEqual({ id: 'tpl' });
    await expect(api.createPDFTemplate({ name: 'Daily' })).resolves.toEqual({ id: 'tpl' });
    await expect(api.updatePDFTemplate('tpl', { name: 'Weekly' })).resolves.toEqual({ id: 'tpl' });
    await api.deletePDFTemplate('tpl');
    await expect(api.previewPDFTemplate('tpl', { site: 'A' })).resolves.toBe(pdf);
    await expect(api.generatePDF('tpl', { site: 'A' })).resolves.toBe(pdf);

    await api.createEventLogEntry({ message: 'ready', severity: 'INFO' });
    expect(lastFetch(fetchMock)[0]).toBe('/xact/api/v1/logs');

    await expect(api.listNotificationProfiles()).resolves.toEqual({ id: 'tpl' });
    await expect(api.getNotificationProfile(1)).resolves.toEqual({ id: 'tpl' });
    await expect(api.createNotificationProfile({ name: 'Ops' })).resolves.toEqual({ id: 'tpl' });
    await expect(api.updateNotificationProfile(1, { name: 'Ops' })).resolves.toEqual({ id: 'tpl' });
    await api.deleteNotificationProfile(1);
    await expect(api.getChannelConfig()).resolves.toEqual({ id: 'tpl' });
    await expect(api.saveChannelConfig({ email: {} as any, telegram: {} as any })).resolves.toEqual({ id: 'tpl' });

    await expect(api.listTagCalcs()).resolves.toEqual({ id: 'tpl' });
    await expect(api.createTagCalc({ name: 'Calc' })).resolves.toEqual({ id: 'tpl' });
    await expect(api.updateTagCalc(1, { name: 'Calc' })).resolves.toEqual({ id: 'tpl' });
    await api.deleteTagCalc(1);
    await expect(api.testTagCalc('1+1')).resolves.toEqual({ id: 'tpl' });

    await expect(api.listScheduledTasks()).resolves.toEqual({ id: 'tpl' });
    await expect(api.getScheduledTask('task')).resolves.toEqual({ id: 'tpl' });
    await expect(api.createScheduledTask({ name: 'Task' })).resolves.toEqual({ id: 'tpl' });
    await expect(api.updateScheduledTask('task', { name: 'Task' })).resolves.toEqual({ id: 'tpl' });
    await api.deleteScheduledTask('task');
    await expect(api.runScheduledTaskNow('task')).resolves.toEqual({ id: 'tpl' });
    await expect(api.getScheduleRunLog('task')).resolves.toEqual({ id: 'tpl' });
  });

  it('handles run-now empty and error responses', async () => {
    stubFetch(mockResponse('', { text: '' }));
    await expect(api.runScheduledTaskNow('task')).resolves.toEqual({});

    stubFetch(mockResponse({ error: 'backup failed' }));
    await expect(api.runScheduledTaskNow('task')).rejects.toThrow('backup failed');

    stubFetch(mockResponse('gateway timeout', { ok: false, status: 504, text: 'gateway timeout' }));
    await expect(api.runScheduledTaskNow('task')).rejects.toThrow('gateway timeout');
  });

  it('throws useful fallback errors for failed simple wrappers', async () => {
    const fetchMock = stubFetch(mockResponse({}, { ok: false, status: 503 }));
    await expect(api.fetchHealth()).rejects.toThrow('Health check failed: 503');
    await expect(api.fetchNATSConfig()).rejects.toThrow('NATS config fetch failed: 503');
    await expect(api.loadNode('x')).rejects.toThrow('Failed to load node: 503');
    await expect(api.loadTag('x')).rejects.toThrow('Failed to load tag: 503');
    await expect(api.createNode('x')).rejects.toThrow('Failed to create node: 503');
    await expect(api.createTag('x', 'string')).rejects.toThrow('Failed to create tag: 503');
    await expect(api.updateTagValue('x', 1)).rejects.toThrow('Failed to update tag: 503');
    await expect(api.updateTag('x', {})).rejects.toThrow('Failed to update tag: 503');
    await expect(api.debugTagPipeline('x', 1)).rejects.toThrow('Failed to debug pipeline: 503');
    await expect(api.updateNode('x', {})).rejects.toThrow('Failed to update node: 503');
    expect(fetchMock).toHaveBeenCalled();
  });
});
