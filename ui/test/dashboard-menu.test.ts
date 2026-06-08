import { describe, expect, it } from 'vitest';
import { configsToMenuItems } from '../src/dashboards/dashboard-menu';
import type { DashboardConfig } from '../src/dashboards/dashboard-config-editor';

function dashboard(id: string, serverId: number, name: string, variation = ''): DashboardConfig {
  return {
    id,
    serverId,
    name,
    description: '',
    icon: '',
    dashboardTag: '',
    variation,
    deviceType: '',
    permission: '',
  };
}

function category(id: string, name: string, children: DashboardConfig[]): DashboardConfig {
  return {
    id,
    name,
    description: '',
    icon: '',
    dashboardTag: '',
    variation: '',
    deviceType: '',
    permission: '',
    children,
  };
}

describe('dashboard sidebar menu', () => {
  it('collapses same-name top-level dashboards into one sidebar item', () => {
    const items = configsToMenuItems([
      dashboard('server:27', 27, 'Air Quality Device', 'standard'),
      dashboard('server:49', 49, 'Air Quality Device', 'battery-backup'),
      dashboard('server:7', 7, 'Overview'),
    ]);

    expect(items.map(item => item.label)).toEqual(['Air Quality Device', 'Overview']);
    expect(items[0]).toMatchObject({
      id: '27',
      dashboard: '27',
      dashboardAliases: expect.arrayContaining(['27', '49', 'Air Quality Device']),
    });
  });

  it('collapses same-name dashboards only within their own category', () => {
    const items = configsToMenuItems([
      category('air', 'Air Quality', [
        dashboard('server:27', 27, 'Air Quality Device', 'standard'),
        dashboard('server:49', 49, 'Air Quality Device', 'battery-backup'),
      ]),
      category('water', 'Water', [
        dashboard('server:52', 52, 'Air Quality Device', 'standard'),
      ]),
    ]);

    expect(items).toHaveLength(2);
    expect(items[0].children?.map(item => item.label)).toEqual(['Air Quality Device']);
    expect(items[0].children?.[0].dashboardAliases).toEqual(expect.arrayContaining(['27', '49']));
    expect(items[1].children?.map(item => item.label)).toEqual(['Air Quality Device']);
    expect(items[1].children?.[0].dashboardAliases).toEqual(expect.arrayContaining(['52']));
  });
});
