import type { MenuItem } from '../components/app-sidebar';
import type { DashboardConfig } from './dashboard-config-editor';

export function configsToMenuItems(configs: DashboardConfig[]): MenuItem[] {
  const items: MenuItem[] = [];
  const dashboardItemsByName = new Map<string, MenuItem>();

  for (const config of configs) {
    if (config.children) {
      items.push({
        id: config.id,
        label: config.name,
        icon: config.icon,
        children: configsToMenuItems(config.children),
      });
      continue;
    }

    addDashboardItem(items, dashboardItemsByName, config);
  }

  return items;
}

function addDashboardItem(items: MenuItem[], dashboardItemsByName: Map<string, MenuItem>, config: DashboardConfig): void {
  const key = config.name.trim();
  const aliases = dashboardRefs(config);
  const existing = dashboardItemsByName.get(key);

  if (existing) {
    existing.dashboardAliases = unique([...(existing.dashboardAliases ?? []), ...aliases]);
    return;
  }

  const item: MenuItem = {
    id: dashboardItemId(config),
    label: config.name,
    icon: config.icon,
    dashboard: dashboardTarget(config),
    dashboardAliases: aliases,
    permission: config.permission || undefined,
  };
  dashboardItemsByName.set(key, item);
  items.push(item);
}

function dashboardItemId(config: DashboardConfig): string {
  return String(config.serverId ?? config.id);
}

function dashboardTarget(config: DashboardConfig): string {
  return String(config.serverId ?? config.name);
}

function dashboardRefs(config: DashboardConfig): string[] {
  return unique([
    dashboardItemId(config),
    dashboardTarget(config),
    config.id,
    config.name,
  ]);
}

function unique(values: string[]): string[] {
  return [...new Set(values.filter(Boolean))];
}
