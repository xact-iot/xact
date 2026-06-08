import { beforeEach, describe, expect, it, vi } from 'vitest';

const storeMock = vi.hoisted(() => ({
  toAbsolute: vi.fn((path: string) => path.startsWith('default.') ? path : `default.${path}`),
  listChildrenNames: vi.fn((path: string) => path === 'default.LA_LongBeach.AirQuality' ? ['AQ-S-0140'] : []),
}));

const uiStoreMock = vi.hoisted(() => ({
  get: vi.fn(() => ''),
}));

vi.mock('../src/store/store', () => ({
  getMirrorStore: () => storeMock,
}));

vi.mock('../src/store/ui-store', () => ({
  getUiStore: () => uiStoreMock,
}));

import { resolveMetricTagPath } from '../src/dashboards/widgets/tag-path-resolver';

describe('tag-path-resolver', () => {
  beforeEach(() => {
    storeMock.toAbsolute.mockClear();
    storeMock.listChildrenNames.mockClear();
    uiStoreMock.get.mockReset();
    uiStoreMock.get.mockReturnValue('');
  });

  it('uses dashboard deviceName for wildcard prefixes', () => {
    uiStoreMock.get.mockReturnValue('AQ-B-0149');

    expect(resolveMetricTagPath('LA_LongBeach.AirQuality.*', 'particulate.pm1')).toBe(
      'default.LA_LongBeach.AirQuality.AQ-B-0149.particulate.pm1',
    );
  });

  it('falls back to a concrete child when wildcard prefix has no dashboard deviceName', () => {
    expect(resolveMetricTagPath('LA_LongBeach.AirQuality.*', 'particulate.pm1')).toBe(
      'default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm1',
    );
  });

  it('does not create blank path segments when wildcard prefix has no child fallback', () => {
    expect(resolveMetricTagPath('LA_LongBeach.Unknown.*', 'particulate.pm1')).toBe(
      'default.LA_LongBeach.Unknown.particulate.pm1',
    );
  });

  it('does not double-prefix a previously stored full relative path', () => {
    expect(resolveMetricTagPath('LA_LongBeach.AirQuality.AQ-S-0140', 'LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm1')).toBe(
      'default.LA_LongBeach.AirQuality.AQ-S-0140.particulate.pm1',
    );
  });
});
