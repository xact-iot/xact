import { describe, expect, it, vi } from 'vitest';
import { resolveDashboardForDeviceSubtype, type DashboardSelectionNode } from '../src/dashboards/dashboard-selection';

class FakeStore {
  private readonly children = new Map<string, Set<string>>();
  private readonly values = new Map<string, unknown>();

  constructor(private readonly org = 'default') {
    this.ensureNode('');
    this.ensureNode(org);
  }

  getOrg(): string {
    return this.org;
  }

  toAbsolute(path: string): string {
    if (!path) return '';
    return path === this.org || path.startsWith(`${this.org}.`) ? path : `${this.org}.${path}`;
  }

  nodeExists(path: string): boolean {
    return this.children.has(path) || this.values.has(path);
  }

  listChildrenNames(path: string): string[] {
    return [...(this.children.get(path) ?? [])];
  }

  getNodeValue(path: string): unknown {
    return this.values.get(path);
  }

  addLeaf(path: string, value: unknown): void {
    this.ensureNode(path);
    this.values.set(path, value);
  }

  private ensureNode(path: string): void {
    if (!this.children.has(path)) this.children.set(path, new Set());
    if (!path) return;

    const parts = path.split('.');
    for (let i = 1; i <= parts.length; i++) {
      const current = parts.slice(0, i).join('.');
      const parent = parts.slice(0, i - 1).join('.');
      const name = parts[i - 1];
      if (!this.children.has(current)) this.children.set(current, new Set());
      if (!this.children.has(parent)) this.children.set(parent, new Set());
      this.children.get(parent)!.add(name);
    }
  }
}

const dashboards: DashboardSelectionNode[] = [
  { id: 'server:27', serverId: 27, name: 'Air Quality Device', variation: 'standard' },
  { id: 'server:49', serverId: 49, name: 'Air Quality Device', variation: 'battery-backup' },
  { id: 'server:7', serverId: 7, name: 'Overview', variation: '' },
];

function ui(deviceName: string): { get(key: string): unknown } {
  return {
    get(key: string) {
      if (key === 'deviceName') return deviceName;
      if (key === 'orgName') return 'default';
      return '';
    },
  };
}

describe('dashboard variation selection', () => {
  it('selects the dashboard whose variation matches the current device subtype', () => {
    const store = new FakeStore();
    store.addLeaf('default.LA_LongBeach.AirQuality.AQ-0002.meta.deviceSubtype', 'battery-backup');

    expect(resolveDashboardForDeviceSubtype(dashboards, '27', { store, ui: ui('AQ-0002') })).toBe('49');
  });

  it('falls back to the first same-name dashboard and logs when no variation matches', () => {
    const store = new FakeStore();
    const log = { log: vi.fn() };
    store.addLeaf('default.LA_LongBeach.AirQuality.AQ-0003.meta.deviceSubtype', 'solar');

    expect(resolveDashboardForDeviceSubtype(dashboards, '49', { store, ui: ui('AQ-0003'), log })).toBe('27');
    expect(log.log).toHaveBeenCalledWith(expect.stringContaining('no dashboard matches'));
  });

  it('leaves dashboards without same-name variations unchanged', () => {
    const log = { log: vi.fn() };

    expect(resolveDashboardForDeviceSubtype(dashboards, '7', { log })).toBe('7');
    expect(log.log).not.toHaveBeenCalled();
  });
});
