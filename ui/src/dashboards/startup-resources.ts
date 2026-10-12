import { getWidgetMeta } from './widgets/widget-registry';

let dataReady: Promise<unknown> = Promise.resolve();
let pluginsReady: Promise<unknown> = Promise.resolve();

// Public dashboards and isolated widget use have no application startup barrier.
export function setDashboardStartupResources(data: Promise<unknown>, plugins: Promise<unknown>): void {
  dataReady = data;
  pluginsReady = plugins;
}

const treeWidgets = new Set([
  'area-map-widget', 'device-list-widget', 'status-table-widget',
  'dashboard-nav-widget', 'array-layout-widget', 'tags-manager-widget',
  'svg-diagram-widget',
]);

export async function waitForWidgetStartup(types: Iterable<string>): Promise<void> {
  let needsData = false;
  let needsPlugins = false;
  for (const type of types) {
    const meta = getWidgetMeta(type);
    const custom = !meta || meta.category === 'Custom';
    needsData ||= treeWidgets.has(type) || custom;
    needsPlugins ||= custom || type === 'area-map-widget';
  }
  await Promise.all([
    needsData ? dataReady : undefined,
    needsPlugins ? pluginsReady : undefined,
  ]);
}
