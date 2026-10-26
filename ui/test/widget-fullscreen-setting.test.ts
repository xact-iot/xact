import { describe, expect, it } from 'vitest';
import { fullscreenAllowed } from '../src/dashboards/dashboard-container';
import { registerWidgetType } from '../src/dashboards/widgets/widget-registry';

describe('per-widget full-screen setting', () => {
  it('uses the widget setting, else the widget type default, else off', () => {
    registerWidgetType({ type: 'fs-default-on', name: 'On', icon: '', category: 'Custom', defaultW: 4, defaultH: 4, fullscreen: true });
    registerWidgetType({ type: 'fs-default-off', name: 'Off', icon: '', category: 'Custom', defaultW: 4, defaultH: 4 });
    const w = (type: string, fullscreen?: boolean) => ({ id: 'x', type, x: 0, y: 0, w: 1, h: 1, config: {}, ...(fullscreen === undefined ? {} : { fullscreen }) });
    expect(fullscreenAllowed(w('fs-default-on'))).toBe(true);
    expect(fullscreenAllowed(w('fs-default-on', false))).toBe(false);
    expect(fullscreenAllowed(w('fs-default-off'))).toBe(false);
    expect(fullscreenAllowed(w('fs-default-off', true))).toBe(true);
    expect(fullscreenAllowed(w('unknown-type'))).toBe(false);
  });
});
