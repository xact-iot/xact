import { describe, expect, it, vi } from 'vitest';
import { applyDiagramStyleTarget, matchesRule, normalizeSvgDiagramConfig, parseSvgTemplate, resolveDiagramBinding } from '../src/dashboards/widgets/svg-diagram-widget';

const TEST_OVERLAY_WIDGET = 'sdw-test-overlay-widget';
const TEST_SCHEMA_WIDGET = 'sdw-schema-overlay-widget';

if (!customElements.get(TEST_OVERLAY_WIDGET)) {
  customElements.define(TEST_OVERLAY_WIDGET, class extends HTMLElement {
    setConfig(config: Record<string, any>): void {
      this.textContent = config.value || '';
      const card = this.closest('widget-card') as any;
      card?.setTitle?.(config.headerText || 'Test Overlay');
    }
  });
}

if (!customElements.get(TEST_SCHEMA_WIDGET)) {
  customElements.define(TEST_SCHEMA_WIDGET, class extends HTMLElement {
    setConfig(config: Record<string, any>): void {
      this.textContent = config.value || '';
      const card = this.closest('widget-card') as any;
      card?.setTitle?.(config.headerText || 'Schema Overlay');
    }

    getPropertySchema(): Array<Record<string, any>> {
      return [
        { name: 'headerText', type: 'string', label: 'Header', default: 'Schema Overlay' },
        { name: 'value', type: 'string', label: 'Value', default: '' },
      ];
    }
  });
}

describe('svg diagram widget helpers', () => {
  it('normalizes missing and partial config', () => {
    const cfg = normalizeSvgDiagramConfig({
      title: 'Pump Mimic',
      width: 900,
      elements: [
        { id: 'pipe-1', type: 'line', x: 10, y: 20, x2: 100, y2: 20, stroke: '#22d3ee' },
      ],
      widgets: [
        { id: 'metric-1', type: 'big-number-widget', x: 50, y: 60, w: 180, h: 90, config: { tagPath: 'A.B' } },
      ],
    });

    expect(cfg.title).toBe('Pump Mimic');
    expect(cfg.width).toBe(900);
    expect(cfg.height).toBe(700);
    expect(cfg.elements).toHaveLength(1);
    expect(cfg.elements[0].type).toBe('line');
    expect(cfg.elements[0].stroke).toBe('#22d3ee');
    expect(cfg.widgets[0].config.tagPath).toBe('A.B');
  });

  it('preserves a deliberately blank title', () => {
    const cfg = normalizeSvgDiagramConfig({ title: '' });

    expect(cfg.title).toBe('');
  });

  it('normalizes element and overlay geometry to integers', () => {
    const cfg = normalizeSvgDiagramConfig({
      elements: [
        { id: 'rect-1', type: 'rect', x: 10.4, y: 20.5, w: 99.6, h: 80.2, fill: '#fff' },
        { id: 'line-1', type: 'line', x: 1.2, y: 2.8, x2: 100.4, y2: 200.6 },
        { id: 'shape-1', type: 'shape', points: [{ x: 10.2, y: 20.8 }, { x: 80.6, y: 40.1 }, { x: 30.5, y: 90.4 }] },
      ],
      widgets: [
        { id: 'metric-1', type: 'big-number-widget', x: 50.5, y: 60.4, w: 180.8, h: 90.1, config: {} },
      ],
    });

    expect(cfg.elements[0]).toMatchObject({ x: 10, y: 21, w: 100, h: 80 });
    expect(cfg.elements[1]).toMatchObject({ x: 1, y: 3, x2: 100, y2: 201 });
    expect(cfg.elements[2]).toMatchObject({ type: 'shape', x: 10, y: 21, w: 71, h: 69 });
    expect(cfg.elements[2].points).toEqual([{ x: 10, y: 21 }, { x: 81, y: 40 }, { x: 31, y: 90 }]);
    expect(cfg.widgets[0]).toMatchObject({ x: 51, y: 60, w: 181, h: 90 });
  });

  it('imports an SVG template layer from sanitized SVG markup', () => {
    const imported = parseSvgTemplate(`
      <svg width="1320" height="900" viewBox="0 0 1320 900" onload="bad()">
        <defs><style>.label{fill:#111}</style></defs>
        <script>alert(1)</script>
        <rect x="0" y="0" width="1320" height="900" fill="#fff"/>
        <a href="javascript:alert(1)"><text x="10" y="20">Label</text></a>
      </svg>
    `);

    expect(imported.width).toBe(1320);
    expect(imported.height).toBe(900);
    expect(imported.template.viewBox).toBe('0 0 1320 900');
    expect(imported.template.content).toContain('<rect');
    expect(imported.template.content).not.toContain('<script');
    expect(imported.template.content).not.toContain('javascript:');
    expect(imported.template.content).not.toContain('onload');
  });

  it('matches numeric and string rules', () => {
    expect(matchesRule(12, 'gt', '10')).toBe(true);
    expect(matchesRule(12, 'lte', '10')).toBe(false);
    expect(matchesRule('RUNNING', 'eq', 'RUNNING')).toBe(true);
    expect(matchesRule('STOPPED', 'ne', 'RUNNING')).toBe(true);
  });

  it('resolves binding rule result or default', () => {
    const binding = {
      tagPath: 'WaterWorks.PUMP.PUMP_01.sensors.status',
      target: 'fill' as const,
      defaultValue: '#64748b',
      rules: [
        { cond: 'eq' as const, value: '1', result: '#22c55e' },
        { cond: 'eq' as const, value: '0', result: '#ef4444' },
      ],
    };

    expect(resolveDiagramBinding(1, binding)).toBe('#22c55e');
    expect(resolveDiagramBinding(0, binding)).toBe('#ef4444');
    expect(resolveDiagramBinding(99, binding)).toBe('#64748b');
  });

  it('does not apply style bindings to editor selection adornments', () => {
    const ns = 'http://www.w3.org/2000/svg';
    const group = document.createElementNS(ns, 'g');
    const iconPath = document.createElementNS(ns, 'path');
    iconPath.setAttribute('fill', 'currentColor');
    group.appendChild(iconPath);

    const selection = document.createElementNS(ns, 'rect');
    selection.setAttribute('data-selection-adornment', 'true');
    selection.setAttribute('fill', 'none');
    selection.setAttribute('stroke', '#f8fafc');
    group.appendChild(selection);

    applyDiagramStyleTarget(group, 'fill', '#22c55e');
    expect(iconPath.getAttribute('fill')).toBe('#22c55e');
    expect(selection.getAttribute('fill')).toBe('none');

    applyDiagramStyleTarget(group, 'stroke', '#ef4444');
    expect(iconPath.getAttribute('fill')).toBe('#ef4444');
    expect(selection.getAttribute('stroke')).toBe('#f8fafc');
  });

  it('marks selected icon outlines as editor-only adornments', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    const [icon] = normalizeSvgDiagramConfig({
      elements: [{ id: 'pump-1', type: 'icon', x: 10, y: 20, w: 64, h: 64, icon: 'mdi:water-pump' }],
    }).elements;

    widget.selectedKind = 'element';
    widget.selectedId = icon.id;

    expect(widget.renderElement(icon, true)).toContain('data-selection-adornment="true"');
  });

  it('keeps selected line stroke visible and draws a separate selection adornment', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    const [line] = normalizeSvgDiagramConfig({
      elements: [{ id: 'line-1', type: 'line', x: 10, y: 20, x2: 120, y2: 20, stroke: '#111827', strokeWidth: 4 }],
    }).elements;

    widget.selectedKind = 'element';
    widget.selectedId = line.id;

    const markup = widget.renderElement(line, true);
    expect(markup).toContain('stroke="#111827"');
    expect(markup).toContain('data-selection-adornment="true"');
    expect(markup).toContain('stroke="#0284c7"');
    expect(markup).not.toContain('stroke="#f8fafc"');
  });

  it('draws a visible selection box around selected text close to the glyphs', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    const [text] = normalizeSvgDiagramConfig({
      elements: [{ id: 'label-b2', type: 'text', x: 100, y: 100, w: 240, h: 120, text: 'B2', fontSize: 28 }],
    }).elements;
    widget.selectedKind = 'element';
    widget.selectedId = text.id;
    widget.selectedIds = new Set([text.id]);

    const markup = widget.renderElement(text, true);

    expect(markup).toContain('data-selection-adornment="true"');
    expect(markup).toContain('x="98"');
    expect(markup).toContain('y="98"');
    expect(markup).toContain('width="39"');
    expect(markup).not.toContain('width="240"');
  });

  it('renders the editor stage without a fixed horizontal minimum', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      width: 1600,
      height: 900,
      templateSvg: { viewBox: '0 0 1600 900', content: '<rect width="1600" height="900"/>' },
      elements: [],
      widgets: [],
    });

    const html = widget.editorHtml();

    expect(html).toContain('class="sdw-stage"');
    expect(html).toContain('width:100%');
    expect(html).toContain('min-width:0');
    expect(html).not.toContain('min-width:720px');
  });

  it('fits the editor stage inside the reduced edit area without changing aspect ratio', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      width: 1600,
      height: 900,
      elements: [],
      widgets: [],
    });
    const overlay = document.createElement('div');
    overlay.innerHTML = `
      <main class="sdw-editor-main" style="padding:18px;">
        <div id="sdw-stage"></div>
      </main>
    `;
    document.body.appendChild(overlay);
    widget.overlayEl = overlay;
    const main = overlay.querySelector('.sdw-editor-main') as HTMLElement;
    const stage = overlay.querySelector('#sdw-stage') as HTMLElement;
    Object.defineProperty(main, 'clientWidth', { configurable: true, value: 800 });
    Object.defineProperty(main, 'clientHeight', { configurable: true, value: 500 });

    widget.fitEditorStage();

    expect(stage.style.width).toBe('764px');
    expect(parseFloat(stage.style.height)).toBeCloseTo(429.75, 2);

    Object.defineProperty(main, 'clientHeight', { configurable: true, value: 300 });
    widget.fitEditorStage();

    expect(parseFloat(stage.style.width)).toBeCloseTo(469.33, 2);
    expect(stage.style.height).toBe('264px');

    overlay.remove();
  });

  it('renders runtime and editor SVGs centered without stretching', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      width: 1600,
      height: 900,
      templateSvg: { viewBox: '0 0 1600 900', content: '<rect width="1600" height="900"/>' },
      elements: [],
      widgets: [],
    });

    expect(widget.renderSvg(false)).toContain('preserveAspectRatio="xMidYMid meet"');
    expect(widget.renderSvg(true)).toContain('preserveAspectRatio="xMidYMid meet"');
    expect(widget.renderTemplateSvg()).toContain('preserveAspectRatio="xMidYMid meet"');
  });

  it('renders a Fill checkbox instead of a No fill button for fillable elements', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    const [rect] = normalizeSvgDiagramConfig({
      elements: [{ id: 'rect-1', type: 'rect', x: 10, y: 20, w: 60, h: 40, fill: 'none' }],
    }).elements;
    widget.selectedKind = 'element';
    widget.selectedId = rect.id;
    widget.selectedIds = new Set([rect.id]);
    widget.config = { elements: [rect], widgets: [] };

    const html = widget.inspectorHtml();

    expect(html).toContain('class="sdw-fill-enabled"');
    expect(html).toContain('<span>Fill</span>');
    expect(html).not.toContain('No fill');
    expect(html).not.toContain('checked');
  });

  it('duplicates selected element groups while preserving relative geometry', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      elements: [
        { id: 'rect-1', type: 'rect', x: 10, y: 20, w: 50, h: 40 },
        { id: 'line-1', type: 'line', x: 80, y: 90, x2: 130, y2: 140 },
      ],
    });
    widget.selectedKind = 'element';
    widget.selectedId = 'line-1';
    widget.selectedIds = new Set(['rect-1', 'line-1']);

    widget.duplicateSelected();

    expect(widget.config.elements).toHaveLength(4);
    expect(widget.config.elements[2]).toMatchObject({ type: 'rect', x: 40, y: 50, w: 50, h: 40 });
    expect(widget.config.elements[3]).toMatchObject({ type: 'line', x: 110, y: 120, x2: 160, y2: 170 });
    expect([...widget.selectedIds]).toEqual([widget.config.elements[2].id, widget.config.elements[3].id]);
  });

  it('copies and pastes selected element groups as a new selected set', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      elements: [
        { id: 'rect-1', type: 'rect', x: 15, y: 25, w: 50, h: 40 },
        { id: 'shape-1', type: 'shape', points: [{ x: 80, y: 90 }, { x: 120, y: 90 }, { x: 120, y: 130 }] },
      ],
    });
    widget.selectedKind = 'element';
    widget.selectedId = 'shape-1';
    widget.selectedIds = new Set(['rect-1', 'shape-1']);

    widget.copySelected();
    widget.pasteClipboard();

    expect(widget.config.elements).toHaveLength(4);
    expect(widget.config.elements[2]).toMatchObject({ type: 'rect', x: 45, y: 55 });
    expect(widget.config.elements[3]).toMatchObject({ type: 'shape', x: 110, y: 120, w: 40, h: 40 });
    expect(widget.config.elements[3].points).toEqual([{ x: 110, y: 120 }, { x: 150, y: 120 }, { x: 150, y: 160 }]);
    expect([...widget.selectedIds]).toEqual([widget.config.elements[2].id, widget.config.elements[3].id]);
  });

  it('brings a selected element above the full overlapping stack', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      elements: [
        { id: 'indicator', type: 'circle', x: 90, y: 40, w: 28, h: 28 },
        { id: 'tank-top', type: 'circle', x: 80, y: 20, w: 100, h: 30 },
        { id: 'tank-body', type: 'rect', x: 80, y: 35, w: 100, h: 100 },
        { id: 'water', type: 'rect', x: 84, y: 60, w: 92, h: 60 },
        { id: 'tank-bottom', type: 'circle', x: 80, y: 120, w: 100, h: 30 },
        { id: 'label', type: 'text', x: 250, y: 40, text: 'Reservoir' },
      ],
    });
    widget.selectedKind = 'element';
    widget.selectedId = 'indicator';
    widget.selectedIds = new Set(['indicator']);

    widget.moveSelectedElement(1);

    expect(widget.config.elements.map((el: any) => el.id)).toEqual([
      'tank-top',
      'tank-body',
      'water',
      'indicator',
      'tank-bottom',
      'label',
    ]);
  });

  it('sends a selected element below the full overlapping stack', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      elements: [
        { id: 'tank-top', type: 'circle', x: 80, y: 20, w: 100, h: 30 },
        { id: 'tank-body', type: 'rect', x: 80, y: 35, w: 100, h: 100 },
        { id: 'water', type: 'rect', x: 84, y: 60, w: 92, h: 60 },
        { id: 'tank-bottom', type: 'circle', x: 80, y: 120, w: 100, h: 30 },
        { id: 'indicator', type: 'circle', x: 90, y: 40, w: 28, h: 28 },
        { id: 'label', type: 'text', x: 250, y: 40, text: 'Reservoir' },
      ],
    });
    widget.selectedKind = 'element';
    widget.selectedId = 'indicator';
    widget.selectedIds = new Set(['indicator']);

    widget.moveSelectedElement(-1);

    expect(widget.config.elements.map((el: any) => el.id)).toEqual([
      'indicator',
      'tank-top',
      'tank-body',
      'water',
      'tank-bottom',
      'label',
    ]);
  });

  it('marquee-selects only elements fully contained by the rectangle', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      elements: [
        { id: 'inside-rect', type: 'rect', x: 20, y: 20, w: 40, h: 30 },
        { id: 'inside-line', type: 'line', x: 80, y: 80, x2: 120, y2: 80 },
        { id: 'partial', type: 'rect', x: 140, y: 140, w: 80, h: 80 },
        { id: 'outside', type: 'circle', x: 260, y: 260, w: 30, h: 30 },
      ],
    });

    widget.marqueeState = { startX: 10, startY: 10, currentX: 180, currentY: 180, additive: false };
    widget.finishMarqueeSelect();

    expect(widget.selectedKind).toBe('element');
    expect([...widget.selectedIds]).toEqual(['inside-rect', 'inside-line']);
  });

  it('marquee-selects short text labels by visible text bounds instead of invisible stored size', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      elements: [
        { id: 'label-b1', type: 'text', x: 100, y: 100, w: 240, h: 120, text: 'B1', fontSize: 28 },
        { id: 'label-b2', type: 'text', x: 150, y: 100, w: 240, h: 120, text: 'B2', fontSize: 28 },
      ],
    });

    widget.marqueeState = { startX: 95, startY: 95, currentX: 190, currentY: 140, additive: false };
    widget.finishMarqueeSelect();

    expect([...widget.selectedIds]).toEqual(['label-b1', 'label-b2']);
  });

  it('adds marquee-selected elements to an existing element selection when modifier is held', () => {
    const widget = document.createElement('svg-diagram-widget') as any;
    widget.config = normalizeSvgDiagramConfig({
      elements: [
        { id: 'already-selected', type: 'rect', x: 240, y: 20, w: 30, h: 30 },
        { id: 'inside', type: 'rect', x: 20, y: 20, w: 40, h: 30 },
      ],
    });
    widget.selectedKind = 'element';
    widget.selectedId = 'already-selected';
    widget.selectedIds = new Set(['already-selected']);

    widget.marqueeState = { startX: 10, startY: 10, currentX: 100, currentY: 100, additive: true };
    widget.finishMarqueeSelect();

    expect([...widget.selectedIds]).toEqual(['already-selected', 'inside']);
  });

  it('mounts overlay widgets inside an inner widget card before applying child config', () => {
    mockIconSetFetch();
    const widget = document.createElement('svg-diagram-widget') as any;
    document.body.appendChild(widget);

    widget.setConfig({
      title: 'Diagram',
      background: '#ffffff',
      elements: [],
      widgets: [
        { id: 'metric-1', type: TEST_OVERLAY_WIDGET, x: 20, y: 30, w: 180, h: 90, config: { headerText: 'Flow Rate', value: '42' } },
      ],
    });

    const overlay = widget.querySelector('.sdw-overlay-widget') as HTMLElement | null;
    const innerCard = overlay?.querySelector('widget-card') as HTMLElement | null;
    const child = innerCard?.querySelector(TEST_OVERLAY_WIDGET) as HTMLElement | null;

    expect(innerCard).toBeTruthy();
    expect(child).toBeTruthy();
    expect(child?.closest('widget-card')).toBe(innerCard);
    expect(innerCard?.classList.contains('sdw-overlay-card')).toBe(true);
    expect(innerCard?.querySelector('.wc-title')?.textContent).toBe('Flow Rate');
    expect((innerCard?.querySelector('.wc-actions') as HTMLElement | null)?.style.display).toBe('none');
    expect(child?.textContent).toBe('42');

    widget.remove();
  });

  it('stores child widget config saves under the overlay widget and re-emits full diagram config', () => {
    mockIconSetFetch();
    const widget = document.createElement('svg-diagram-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      title: 'Diagram',
      elements: [],
      widgets: [
        { id: 'metric-1', type: TEST_OVERLAY_WIDGET, x: 20, y: 30, w: 180, h: 90, config: { headerText: 'Flow Rate' } },
      ],
    });

    const received: CustomEvent[] = [];
    widget.addEventListener('widget-config-save', ((e: CustomEvent) => received.push(e)) as EventListener);

    const child = widget.querySelector(TEST_OVERLAY_WIDGET) as HTMLElement;
    child.dispatchEvent(new CustomEvent('widget-config-save', {
      bubbles: true,
      composed: true,
      detail: { config: { headerText: 'Pressure', value: '88' }, forceDirty: true },
    }));

    expect(received).toHaveLength(1);
    expect(received[0].target).toBe(widget);
    expect(received[0].detail.forceDirty).toBe(true);
    expect(received[0].detail.config.title).toBe('Diagram');
    expect(received[0].detail.config.widgets[0].config).toEqual({ headerText: 'Pressure', value: '88' });
    expect(widget.getConfig().widgets[0].config).toEqual({ headerText: 'Pressure', value: '88' });

    widget.remove();
  });

  it('keeps editor overlay widget preview bodies mounted after connecting the preview wrapper', () => {
    mockIconSetFetch();
    const widget = document.createElement('svg-diagram-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      title: 'Diagram',
      elements: [],
      widgets: [
        { id: 'metric-1', type: TEST_OVERLAY_WIDGET, x: 20, y: 30, w: 180, h: 90, config: { headerText: 'Flow Rate', value: '42' } },
      ],
    });

    widget.openEditor();

    const preview = document.querySelector('.sdw-editor-overlay-widget') as HTMLElement | null;
    const innerCard = preview?.querySelector('widget-card') as HTMLElement | null;
    const body = innerCard?.querySelector('.widget-body') as HTMLElement | null;
    const child = body?.querySelector(TEST_OVERLAY_WIDGET) as HTMLElement | null;

    expect(preview).toBeTruthy();
    expect(innerCard?.querySelector('.wc-title')?.textContent).toBe('Flow Rate');
    expect(child).toBeTruthy();
    expect(child?.textContent).toBe('42');

    widget.closeEditor(false);
    widget.remove();
  });

  it('keeps the SVG editor open when applying a schema-based overlay widget config dialog', async () => {
    mockIconSetFetch();
    const widget = document.createElement('svg-diagram-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      title: 'Diagram',
      elements: [],
      widgets: [
        { id: 'chart-1', type: TEST_SCHEMA_WIDGET, x: 20, y: 30, w: 180, h: 90, config: { headerText: 'Trend', value: '42' } },
      ],
    });

    widget.openEditor();
    const editor = document.querySelector('.sdw-editor') as HTMLElement | null;
    widget.selectedId = 'chart-1';
    widget.selectedKind = 'widget';
    widget.selectedIds = new Set(['chart-1']);

    widget.openChildWidgetConfig();
    await nextFrame();

    const dialog = document.querySelector('widget-properties-dialog') as HTMLElement | null;
    expect(editor).toBeTruthy();
    expect(dialog).toBeTruthy();

    dialog?.dispatchEvent(new CustomEvent('properties-updated', {
      bubbles: true,
      composed: true,
      detail: { config: { headerText: 'Trend Updated', value: '88' } },
    }));

    expect(editor?.isConnected).toBe(true);
    expect(document.querySelector('.sdw-editor')).toBe(editor);
    expect(document.querySelector('.sdw-side')).toBeTruthy();
    expect(widget.getConfig().widgets[0].config).toEqual({ headerText: 'Trend Updated', value: '88' });

    widget.closeEditor(false);
    widget.remove();
  });

  it('scales the runtime overlay layer with the visible SVG viewport', () => {
    mockIconSetFetch();
    const widget = document.createElement('svg-diagram-widget') as any;
    document.body.appendChild(widget);
    widget.setConfig({
      width: 1200,
      height: 600,
      elements: [],
      widgets: [
        { id: 'metric-1', type: TEST_OVERLAY_WIDGET, x: 120, y: 60, w: 240, h: 120, config: {} },
      ],
    });

    const runtime = widget.querySelector('.sdw-runtime') as HTMLElement;
    const layer = widget.querySelector('.sdw-widget-layer') as HTMLElement;
    Object.defineProperty(runtime, 'clientWidth', { configurable: true, value: 600 });
    Object.defineProperty(runtime, 'clientHeight', { configurable: true, value: 600 });

    widget.updateRuntimeWidgetLayerViewport();

    expect(layer.style.left).toBe('0px');
    expect(layer.style.top).toBe('150px');
    expect(layer.style.width).toBe('1200px');
    expect(layer.style.height).toBe('600px');
    expect(layer.style.right).toBe('auto');
    expect(layer.style.bottom).toBe('auto');
    expect(layer.style.transformOrigin).toBe('0 0');
    expect(layer.style.transform).toBe('scale(0.5)');

    widget.remove();
  });
});

function nextFrame(): Promise<void> {
  return new Promise(resolve => requestAnimationFrame(() => resolve()));
}

function mockIconSetFetch(): void {
  vi.stubGlobal('ResizeObserver', class {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  });
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({
    prefix: 'mdi',
    icons: {},
    width: 24,
    height: 24,
  }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  }));
}
