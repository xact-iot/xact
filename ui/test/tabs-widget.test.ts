import { afterEach, describe, expect, it } from 'vitest';
import '../src/dashboards/widgets/tabs-widget';

const TEST_CHILD_WIDGET = 'tabs-test-child-widget';

if (!customElements.get(TEST_CHILD_WIDGET)) {
  customElements.define(TEST_CHILD_WIDGET, class extends HTMLElement {
    private config: Record<string, any> = {};

    setConfig(config: Record<string, any>): void {
      this.config = { ...config };
    }

    getConfig(): Record<string, any> {
      return { ...this.config };
    }
  });
}

describe('tabs-widget child config saves', () => {
  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('re-emits child config saves with forceDirty and without mutating the original config', async () => {
    const originalConfig = {
      tabs: [{
        id: 'tab-1',
        label: 'Status',
        widgetType: TEST_CHILD_WIDGET,
        widgetConfig: { rows: [{ label: 'Original' }] },
      }],
      activeTabId: 'tab-1',
    };
    const host = document.createElement('div');
    const widget = document.createElement('tabs-widget') as any;
    const saves: CustomEvent[] = [];
    host.addEventListener('widget-config-save', ((event: CustomEvent) => saves.push(event)) as EventListener);
    host.appendChild(widget);
    document.body.appendChild(host);

    widget.setConfig(originalConfig);
    await Promise.resolve();
    await Promise.resolve();

    const child = widget.querySelector(TEST_CHILD_WIDGET) as HTMLElement;
    child.dispatchEvent(new CustomEvent('widget-config-save', {
      bubbles: true,
      composed: true,
      detail: {
        config: { rows: [{ label: 'Changed' }] },
        forceDirty: true,
      },
    }));

    expect(saves).toHaveLength(1);
    expect(saves[0].detail.forceDirty).toBe(true);
    expect(saves[0].detail.config.tabs[0].widgetConfig.rows[0].label).toBe('Changed');
    expect(originalConfig.tabs[0].widgetConfig.rows[0].label).toBe('Original');
  });
});
