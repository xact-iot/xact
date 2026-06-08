import { expect, test, type Page, type Route } from '@playwright/test';

type DashboardRecord = {
  id: number;
  name: string;
  description: string;
  icon: string;
  variation: string;
  deviceType: string;
  permission: string;
  isCategory: boolean;
  parentId?: number | null;
  sortOrder: number;
  widgets: any[];
};

test('dashboard manager preserves widgets while creating, rearranging, moving, and renaming dashboards', async ({ page }) => {
  const api = new MockDashboardAPI();
  const calls: string[] = [];

  await installAppMocks(page, api, calls);
  await page.goto('./');
  await expect(page.locator('dashboard-config-editor')).toBeVisible();

  await addCategory(page, 'Operations');
  await addCategory(page, 'Archive');
  await addDashboard(page, 'ISS');
  await addDashboard(page, 'Telemetry');

  await expect.poll(() => api.hasDashboard('ISS')).toBe(true);
  await expect.poll(() => api.hasDashboard('Telemetry')).toBe(true);

  const issWidgets = [
    { id: 'iss-title', type: 'html-widget', x: 0, y: 0, w: 24, h: 3, config: { html: 'ISS' } },
    { id: 'iss-altitude', type: 'big-number-widget', x: 0, y: 3, w: 6, h: 5, config: { tagPath: 'NASA.ISS.orbit.altitude' } },
  ];
  const tagViewWidgets = [
    { id: 'tag-browser', type: 'tags-manager-widget', x: 0, y: 0, w: 24, h: 20, config: {} },
  ];
  api.setWidgets('ISS', issWidgets);
  api.setWidgets('Telemetry', tagViewWidgets);

  // Re-arrange top-level categories.
  await clickMove(page, 'Archive', 'up');

  // Move dashboards into the category using the manager modal.
  await editParent(page, 'ISS', 'Archive');
  await editParent(page, 'Telemetry', 'Archive');

  // Rename the category after it has children.
  await editName(page, 'Archive', 'Mission');

  await expect.poll(() => api.hasDashboard('Mission')).toBe(true);
  await expect.poll(() => api.dashboard('ISS')?.parentId).toBe(api.dashboard('Mission')?.id);
  await expect.poll(() => api.dashboard('Telemetry')?.parentId).toBe(api.dashboard('Mission')?.id);

  expect(api.dashboard('ISS')?.widgets).toEqual(issWidgets);
  expect(api.dashboard('Telemetry')?.widgets).toEqual(tagViewWidgets);
  expect(calls).not.toContain('DELETE ISS');
  expect(calls).not.toContain('DELETE Telemetry');
});

test('dashboard manager preserves widgets when renamed dashboard moves into category and back to top level', async ({ page }) => {
  const api = new MockDashboardAPI();
  const calls: string[] = [];
  const mapWidgets = [
    {
      id: 'map1',
      type: 'area-map-widget',
      x: 0,
      y: 0,
      w: 23,
      h: 45,
      config: {
        heading: 'Singapore Traffic',
        showTraffic: true,
        layers: [{ id: 'cameras', name: 'Cameras', pathPattern: 'Singapore.TrafficCamera.*' }],
      },
    },
  ];
  api.seedDashboard({ name: 'Map', icon: 'mdi:map', widgets: mapWidgets });

  await installAppMocks(page, api, calls);
  await page.goto('./');
  await expect(page.locator('dashboard-config-editor')).toBeVisible();
  await expect(page.locator('dashboard-config-editor tr', { hasText: 'Map' })).toBeVisible();

  await addCategory(page, 'Transport');
  await editName(page, 'Map', 'Singapore Traffic');
  await editParent(page, 'Singapore Traffic', 'Transport');
  await editParent(page, 'Singapore Traffic', '');

  await expect.poll(() => api.hasDashboard('Singapore Traffic')).toBe(true);
  await expect.poll(() => api.dashboard('Singapore Traffic')?.parentId ?? null).toBeNull();
  expect(api.dashboard('Singapore Traffic')?.widgets).toEqual(mapWidgets);
  expect(calls).not.toContain('DELETE Map');
  expect(calls).not.toContain('DELETE Singapore Traffic');
});

test('browser back button returns to the previous dashboard', async ({ page }) => {
  const api = new MockDashboardAPI();
  const calls: string[] = [];
  const widgets = [
    { id: 'title', type: 'text-widget', x: 0, y: 0, w: 6, h: 3, config: { text: 'Dashboard' } },
  ];
  api.seedDashboard({ name: 'Overview', icon: 'mdi:view-dashboard', widgets });
  api.seedDashboard({ name: 'Pumps', icon: 'mdi:water-pump', widgets });

  await installAppMocks(page, api, calls);
  await page.goto('./');
  await expect(page.locator('dashboard-config-editor')).toBeVisible();

  const overview = api.dashboard('Overview')!;
  const pumps = api.dashboard('Pumps')!;
  await page.locator(`app-sidebar [data-dashboard="${overview.id}"]`).click();
  await expect(page.locator('.xact-tab.active .xact-tab-title')).toHaveText('Overview');
  await expect(page).toHaveURL(new RegExp(`#${overview.id}$`));

  await page.locator(`app-sidebar [data-dashboard="${pumps.id}"]`).click();
  await expect(page.locator('.xact-tab.active .xact-tab-title')).toHaveText('Pumps');
  await expect(page).toHaveURL(new RegExp(`#${pumps.id}$`));

  await page.goBack();
  await expect(page.locator('.xact-tab.active .xact-tab-title')).toHaveText('Overview');
  await expect(page.locator('dashboard-container')).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`#${overview.id}$`));
});

test('dashboard editor stop editing saves dirty layout before leaving edit mode', async ({ page }) => {
  const api = new MockDashboardAPI();
  const calls: string[] = [];
  const widgets = [
    { id: 'w1', type: 'text-widget', x: 0, y: 0, w: 4, h: 3, config: { text: 'Original' } },
  ];
  api.seedDashboard({ name: 'ISS', description: 'ISS telemetry', icon: 'mdi:space-station', widgets });

  await installAppMocks(page, api, calls);
  await page.goto(`./#${api.dashboard('ISS')!.id}`);
  await expect(page.locator('dashboard-container')).toBeVisible();
  await expect(page.locator('.xact-tab.active .xact-tab-title')).toHaveText('ISS');

  await page.evaluate(async () => {
    const dashboard = document.querySelector('dashboard-container') as any;
    await dashboard.setDashboardMode('edit');
    dashboard.dirty = true;
    dashboard.updateSaveButton();
  });
  await expect(page.locator('#pc-save')).toBeVisible();

  await page.locator('#menu-btn').click();
  await page.getByRole('button', { name: /Stop Editing/ }).click();
  await page.getByRole('button', { name: 'Save changes' }).click();

  await expect(page.locator('#pc-save')).toHaveCount(0);
  await expect.poll(() => api.dashboard('ISS')?.widgets).toEqual(widgets);
  expect(calls.some(call => call.startsWith('PUT ISS'))).toBe(true);

  await page.locator('app-sidebar [data-dashboard="dashboard-config-editor"]').click();
  await expect(page.getByText('Unsaved changes')).toHaveCount(0);
});

test('dashboard editor retries save as update when create collides with existing dashboard', async ({ page }) => {
  const api = new MockDashboardAPI();
  const calls: string[] = [];
  const name = 'Orgs, Users, Permissions';
  const widgets = [
    {
      id: 'ufe27ej1',
      type: 'tabs-widget',
      x: 1,
      y: 0,
      w: 23,
      h: 46,
      config: {
        tabs: [
          { id: 'tpxdyik', label: 'Organisations', widgetType: 'organisations-widget', widgetConfig: {} },
          { id: 'tdbfspi', label: 'Users', widgetType: 'users-widget', widgetConfig: {} },
          { id: 'tphcmax', label: 'Permissions', widgetType: 'permissions-widget', widgetConfig: {} },
        ],
        activeTabId: 'tpxdyik',
      },
    },
  ];
  api.seedDashboard({ name, widgets: [] });

  await installAppMocks(page, api, calls);
  await page.goto(`./#${api.dashboard(name)!.id}`);
  await expect(page.locator('dashboard-container')).toBeVisible();
  await expect(page.locator('.xact-tab.active .xact-tab-title')).toHaveText(name);

  await page.evaluate(async ({ dashboardName, dashboardWidgets, dashboardId }) => {
    const dashboard = document.querySelector('dashboard-container') as any;
    dashboard.dashboardName = dashboardName;
    dashboard.dashboardData = {
      id: dashboardId,
      name: dashboardName,
      description: '',
      icon: '',
      variation: '',
      deviceType: '',
      permission: '',
      isCategory: false,
      sortOrder: 0,
      widgets: dashboardWidgets,
    };
    dashboard.widgets = dashboardWidgets;
    await dashboard.setDashboardMode('edit');
    dashboard.rerender();
    dashboard.initGrid();
    dashboard.dirty = true;
    dashboard.updateSaveButton();
  }, { dashboardName: name, dashboardWidgets: widgets, dashboardId: api.dashboard(name)!.id });

  await page.locator('#pc-save').click();

  await expect(page.locator('#pc-save')).toHaveCount(0);
  await expect.poll(() => api.dashboard(name)?.widgets).toEqual(widgets);
  expect(calls.some(call => call.startsWith(`PUT ${name}`))).toBe(true);
});

test('tabs widget hides child configure gear for widgets without properties', async ({ page }) => {
  const api = new MockDashboardAPI();
  const calls: string[] = [];
  const name = 'Tabbed Admin';
  api.seedDashboard({
    name,
    widgets: [
      {
        id: 'tabs1',
        type: 'tabs-widget',
        x: 0,
        y: 0,
        w: 12,
        h: 8,
        config: {
          tabs: [
            { id: 'orgs', label: 'Organisations', widgetType: 'organisations-widget', widgetConfig: {} },
            { id: 'html', label: 'HTML', widgetType: 'html-widget', widgetConfig: { html: '<b>Editable</b>' } },
          ],
          activeTabId: 'orgs',
        },
      },
    ],
  });

  await installAppMocks(page, api, calls);
  await page.goto(`./#${api.dashboard(name)!.id}`);
  await expect(page.locator('dashboard-container')).toBeVisible();

  await page.evaluate(async () => {
    const dashboard = document.querySelector('dashboard-container') as any;
    await dashboard.setDashboardMode('edit');
  });

  await expect(page.locator('.tw-tab-active')).toContainText('Organisations');
  await expect(page.locator('.tw-tab-active .tw-child-cfg')).toHaveCount(0);

  await page.locator('.tw-tab', { hasText: 'HTML' }).click();
  await expect(page.locator('.tw-tab-active')).toContainText('HTML');
  await expect(page.locator('.tw-tab-active .tw-child-cfg')).toHaveCount(1);
});

async function installAppMocks(page: Page, api: MockDashboardAPI, calls: string[]) {
  await page.addInitScript(() => {
    const payload = btoa(JSON.stringify({
      exp: Math.floor(Date.now() / 1000) + 3600,
      tenant_id: 'default',
    }));
    localStorage.setItem('xact_auth_token', `header.${payload}.sig`);
    localStorage.setItem('xact_auth_user', JSON.stringify({
      id: '1',
      username: 'admin',
      tenant_id: 'default',
      roles: ['SystemAdmin'],
      allowed_orgs: ['default'],
    }));
  });

  await page.route('**/xact/health', route => fulfillJSON(route, { status: 'ok', timezone: 'UTC' }));
  await page.route('**/xact/api/v1/system/nats-config', route => fulfillJSON(route, {
    username: 'test',
    password: 'test',
    natsWsPath: '/xact/ws',
  }));
  await page.route('**/xact/api/v1/nodes**', route => fulfillJSON(route, { name: 'default', type: 'Organisation', children: {} }));
  await page.route('**/xact/api/v1/auth/my-orgs', route => fulfillJSON(route, [
    { name: 'default', displayName: 'Default', role: 'SystemAdmin' },
  ]));
  await page.route('**/xact/api/v1/plugins/widgets', route => fulfillJSON(route, []));
  await page.route('**/xact/api/v1/plugins/themes', route => fulfillJSON(route, []));
  await page.route('**/xact/api/v1/dashboards**', route => api.handle(route, calls));
}

async function addCategory(page: Page, name: string) {
  await page.locator('#add-category-btn').click();
  await saveName(page, name);
  await expect(page.locator('dashboard-config-editor')).toContainText(name);
  await waitForDashboardSave();
}

async function addDashboard(page: Page, name: string) {
  await page.locator('#add-dashboard-btn').click();
  await saveName(page, name);
  await expect(page.locator('dashboard-config-editor')).toContainText(name);
  await waitForDashboardSave();
}

async function editName(page: Page, currentName: string, nextName: string) {
  await editRow(page, currentName);
  await saveName(page, nextName);
  await expect(page.locator('dashboard-config-editor')).toContainText(nextName);
  await waitForDashboardSave();
}

async function editParent(page: Page, name: string, parentName: string) {
  await editRow(page, name);
  if (parentName) {
    await page.locator('#edit-parent').selectOption({ label: parentName });
  } else {
    await page.locator('#edit-parent').selectOption({ value: '' });
  }
  await page.locator('#edit-save').click();
  await waitForDashboardSave();
}

async function saveName(page: Page, name: string) {
  await page.locator('#edit-name').fill(name);
  await page.locator('#edit-save').click();
}

async function editRow(page: Page, name: string) {
  const row = page.locator('dashboard-config-editor tr', { hasText: name }).first();
  await row.locator('.edit-btn').click();
  await expect(page.locator('#edit-name')).toBeVisible();
}

async function clickMove(page: Page, id: string, dir: 'up' | 'down') {
  const row = page.locator('dashboard-config-editor tr', { hasText: id }).first();
  await row.locator(`.move-btn[data-dir="${dir}"]`).click();
  await waitForDashboardSave();
}

async function waitForDashboardSave() {
  // main.ts debounces dashboard-manager saves by 300 ms.
  await new Promise(resolve => setTimeout(resolve, 1000));
}

class MockDashboardAPI {
  private dashboards: DashboardRecord[] = [];
  private nextID = 1;

  hasDashboard(name: string) {
    return Boolean(this.dashboard(name));
  }

  dashboard(name: string) {
    return this.dashboards.find(p => p.name === name);
  }

  snapshot() {
    return structuredClone(this.dashboards);
  }

  setWidgets(name: string, widgets: any[]) {
    const dashboard = this.dashboard(name);
    if (!dashboard) throw new Error(`dashboard ${name} not found`);
    dashboard.widgets = structuredClone(widgets);
  }

  seedDashboard(dashboard: Partial<DashboardRecord> & { name: string; widgets?: any[] }) {
    this.dashboards.push({
      id: this.nextID++,
      name: dashboard.name,
      description: dashboard.description ?? '',
      icon: dashboard.icon ?? '',
      variation: dashboard.variation ?? '',
      deviceType: dashboard.deviceType ?? '',
      permission: dashboard.permission ?? '',
      isCategory: dashboard.isCategory ?? false,
      parentId: dashboard.parentId ?? null,
      sortOrder: dashboard.sortOrder ?? this.dashboards.length,
      widgets: structuredClone(dashboard.widgets ?? []),
    });
  }

  async handle(route: Route, calls: string[]) {
    const request = route.request();
    const url = new URL(request.url());
    const match = url.pathname.match(/\/xact\/api\/v1\/dashboards\/?([^/]*)$/);
    const encodedID = match?.[1] ?? '';
    const id = encodedID ? Number(decodeURIComponent(encodedID)) : 0;

    if (request.method() === 'GET' && !id) {
      await fulfillJSON(route, this.list());
      return;
    }

    if (request.method() === 'GET') {
      const dashboard = this.dashboardByID(id);
      if (!dashboard) return route.fulfill({ status: 404, body: 'dashboard not found' });
      await fulfillJSON(route, structuredClone(dashboard));
      return;
    }

    if (request.method() === 'POST') {
      const payload = await request.postDataJSON();
      calls.push(`POST ${payload.name}`);
      const dashboard = this.create(payload);
      await fulfillJSON(route, structuredClone(dashboard), 201);
      return;
    }

    if (request.method() === 'PUT') {
      const payload = await request.postDataJSON();
      const existing = this.dashboardByID(id);
      calls.push(`PUT ${existing?.name ?? id} -> ${payload.name ?? existing?.name ?? ''}`);
      const dashboard = this.update(id, payload);
      if (!dashboard) return route.fulfill({ status: 404, body: 'dashboard not found' });
      await fulfillJSON(route, structuredClone(dashboard));
      return;
    }

    if (request.method() === 'DELETE') {
      const existing = this.dashboardByID(id);
      calls.push(`DELETE ${existing?.name ?? id}`);
      this.delete(id);
      await route.fulfill({ status: 204, body: '' });
      return;
    }

    await route.fallback();
  }

  private list() {
    return this.dashboards
      .slice()
      .sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name))
      .map(({ widgets, ...meta }) => meta);
  }

  private create(payload: any) {
    const dashboard: DashboardRecord = {
      id: this.nextID++,
      name: payload.name,
      description: payload.description ?? '',
      icon: payload.icon ?? '',
      variation: payload.variation ?? '',
      deviceType: payload.deviceType ?? '',
      permission: payload.permission ?? '',
      isCategory: payload.isCategory ?? false,
      parentId: payload.parentId ?? null,
      sortOrder: payload.sortOrder ?? 0,
      widgets: structuredClone(payload.widgets ?? []),
    };
    this.dashboards.push(dashboard);
    return dashboard;
  }

  private update(id: number, payload: any) {
    const dashboard = this.dashboardByID(id);
    if (!dashboard) return null;
    if ('name' in payload) dashboard.name = payload.name;
    if ('description' in payload) dashboard.description = payload.description;
    if ('icon' in payload) dashboard.icon = payload.icon;
    if ('variation' in payload) dashboard.variation = payload.variation;
    if ('deviceType' in payload) dashboard.deviceType = payload.deviceType;
    if ('permission' in payload) dashboard.permission = payload.permission;
    if ('isCategory' in payload) dashboard.isCategory = payload.isCategory;
    if ('sortOrder' in payload) dashboard.sortOrder = payload.sortOrder;
    if ('parentId' in payload) dashboard.parentId = payload.parentId ?? null;
    if ('widgets' in payload) dashboard.widgets = structuredClone(payload.widgets);
    return dashboard;
  }

  private delete(id: number) {
    const dashboard = this.dashboardByID(id);
    if (!dashboard) return;
    const deletedIDs = new Set([dashboard.id]);
    for (const child of this.dashboards) {
      if (child.parentId === dashboard.id) deletedIDs.add(child.id);
    }
    this.dashboards = this.dashboards.filter(p => !deletedIDs.has(p.id));
  }

  private dashboardByID(id: number) {
    return this.dashboards.find(p => p.id === id);
  }
}

async function fulfillJSON(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  });
}
