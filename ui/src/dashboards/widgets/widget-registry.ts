export type WidgetCategory = 'General' | 'Metrics' | 'Layout' | 'System' | 'Custom';

export const WIDGET_CATEGORIES: WidgetCategory[] = ['General', 'Metrics', 'Layout', 'System', 'Custom'];

export interface WidgetTypeMeta {
  type: string;
  name: string;
  icon: string;
  category: WidgetCategory;
  defaultW: number;
  defaultH: number;
  minW?: number;
  minH?: number;
  load?: () => Promise<unknown>;
}

const registry: Map<string, WidgetTypeMeta> = new Map();
const pendingLoads: Map<string, Promise<void>> = new Map();
const widgetNameCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

export function registerWidgetType(meta: WidgetTypeMeta): void {
  const existing = registry.get(meta.type);
  registry.set(meta.type, {
    ...meta,
    load: meta.load ?? existing?.load,
  });
}

export function registerWidgetTypes(metas: WidgetTypeMeta[]): void {
  metas.forEach(registerWidgetType);
}

export function getWidgetMeta(type: string): WidgetTypeMeta | undefined {
  return registry.get(type);
}

export function getAvailableWidgets(): WidgetTypeMeta[] {
  return Array.from(registry.values());
}

export function getWidgetsByCategory(): Map<WidgetCategory, WidgetTypeMeta[]> {
  const byCategory = new Map<WidgetCategory, WidgetTypeMeta[]>();
  for (const cat of WIDGET_CATEGORIES) {
    byCategory.set(cat, []);
  }
  for (const meta of registry.values()) {
    const list = byCategory.get(meta.category);
    if (list) list.push(meta);
  }
  for (const widgets of byCategory.values()) {
    widgets.sort(compareWidgetNames);
  }
  return byCategory;
}

function compareWidgetNames(a: WidgetTypeMeta, b: WidgetTypeMeta): number {
  return widgetNameCollator.compare(a.name, b.name) || a.type.localeCompare(b.type);
}

export async function ensureWidgetTypeLoaded(type: string): Promise<void> {
  if (!type || customElements.get(type)) return;

  const meta = registry.get(type);
  if (!meta?.load) return;

  let pending = pendingLoads.get(type);
  if (!pending) {
    pending = Promise.resolve(meta.load())
      .then(() => undefined)
      .finally(() => pendingLoads.delete(type));
    pendingLoads.set(type, pending);
  }

  await pending;
}

export async function ensureWidgetTypesLoaded(types: Iterable<string>): Promise<void> {
  await Promise.all([...new Set([...types].filter(Boolean))].map(ensureWidgetTypeLoaded));
}
