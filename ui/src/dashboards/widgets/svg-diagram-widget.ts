import { BaseComponent } from '../../components/base-component';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import { getMirrorStore } from '../../store/store';
import { ensureWidgetTypeLoaded, getAvailableWidgets, getWidgetMeta, registerWidgetType } from './widget-registry';
import { getIconSVG, isIconSetLoaded, loadIconSet } from '../../utils/icons';
import '../../components/icon-picker';
import './widget-properties-dialog';
import './widget-card';
import type { WidgetPropertiesDialog, PropertyField } from './widget-properties-dialog';
import type { WidgetCard } from './widget-card';

registerWidgetType({
  type: 'svg-diagram-widget',
  name: 'SVG Diagram',
  icon: 'SVG',
  category: 'Custom',
  defaultW: 16,
  defaultH: 18,
  minW: 8,
  minH: 8,
});

export type DiagramElementType = 'line' | 'shape' | 'rect' | 'circle' | 'text' | 'icon';
export type DiagramBindTarget = 'fill' | 'stroke' | 'text' | 'opacity';
export type DiagramRuleCond = 'eq' | 'ne' | 'gt' | 'gte' | 'lt' | 'lte';

export interface DiagramStyleRule {
  cond: DiagramRuleCond;
  value: string;
  result: string;
}

export interface DiagramBinding {
  tagPath: string;
  target: DiagramBindTarget;
  defaultValue: string;
  rules: DiagramStyleRule[];
}

export interface DiagramPoint {
  x: number;
  y: number;
}

export interface DiagramElement {
  id: string;
  type: DiagramElementType;
  x: number;
  y: number;
  w: number;
  h: number;
  x2?: number;
  y2?: number;
  points?: DiagramPoint[];
  text?: string;
  icon?: string;
  fill: string;
  stroke: string;
  strokeWidth: number;
  fontSize: number;
  opacity: number;
  bindings: DiagramBinding[];
}

export interface DiagramOverlayWidget {
  id: string;
  type: string;
  x: number;
  y: number;
  w: number;
  h: number;
  config: Record<string, any>;
}

export interface DiagramTemplateSvg {
  viewBox: string;
  content: string;
}

export interface SvgDiagramConfig {
  title: string;
  width: number;
  height: number;
  background: string;
  templateSvg?: DiagramTemplateSvg;
  elements: DiagramElement[];
  widgets: DiagramOverlayWidget[];
}

const DEFAULT_CONFIG: SvgDiagramConfig = {
  title: 'SVG Diagram',
  width: 1200,
  height: 700,
  background: '#0f172a',
  templateSvg: undefined,
  elements: [
    {
      id: makeId(),
      type: 'rect',
      x: 120,
      y: 160,
      w: 180,
      h: 90,
      fill: 'rgba(34, 211, 238, 0.14)',
      stroke: '#22d3ee',
      strokeWidth: 4,
      fontSize: 28,
      opacity: 1,
      bindings: [],
    },
    {
      id: makeId(),
      type: 'line',
      x: 300,
      y: 205,
      w: 330,
      h: 0,
      x2: 630,
      y2: 205,
      fill: 'transparent',
      stroke: '#94a3b8',
      strokeWidth: 8,
      fontSize: 24,
      opacity: 1,
      bindings: [],
    },
    {
      id: makeId(),
      type: 'icon',
      x: 630,
      y: 163,
      w: 84,
      h: 84,
      icon: 'mdi:water-pump',
      fill: '#f59e0b',
      stroke: '#f59e0b',
      strokeWidth: 2,
      fontSize: 24,
      opacity: 1,
      bindings: [],
    },
  ],
  widgets: [],
};

const COND_OPTIONS: DiagramRuleCond[] = ['eq', 'ne', 'gt', 'gte', 'lt', 'lte'];
const TARGET_OPTIONS: DiagramBindTarget[] = ['fill', 'stroke', 'text', 'opacity'];
const BASIC_WIDGET_TYPES = ['big-number-widget', 'timeseries-chart-widget', 'gauge-widget', 'status-table-widget', 'binary-status-line-widget'];
const SELECTION_ADORNMENT_ATTR = 'data-selection-adornment';
const SELECTION_STROKE = '#0284c7';
type SelectionKind = 'element' | 'widget';

export function normalizeSvgDiagramConfig(input: Partial<SvgDiagramConfig> & Record<string, any> = {}): SvgDiagramConfig {
  const cfg = {
    ...clone(DEFAULT_CONFIG),
    ...input,
  } as SvgDiagramConfig;
  cfg.title = input.title === undefined ? 'SVG Diagram' : String(input.title);
  cfg.width = positiveNumber(cfg.width, 1200);
  cfg.height = positiveNumber(cfg.height, 700);
  cfg.background = String(cfg.background || '#0f172a');
  cfg.templateSvg = normalizeTemplateSvg(input.templateSvg);
  cfg.elements = Array.isArray(input.elements) ? input.elements.map(normalizeElement) : clone(DEFAULT_CONFIG.elements);
  cfg.widgets = Array.isArray(input.widgets) ? input.widgets.map(normalizeOverlayWidget) : [];
  return cfg;
}

export function resolveDiagramBinding(value: any, binding: DiagramBinding): string {
  for (const rule of binding.rules || []) {
    if (matchesRule(value, rule.cond, rule.value)) return String(rule.result ?? '');
  }
  return String(binding.defaultValue ?? '');
}

export function matchesRule(value: any, cond: DiagramRuleCond, compareTo: string): boolean {
  const leftNum = Number(value);
  const rightNum = Number(compareTo);
  const numeric = Number.isFinite(leftNum) && Number.isFinite(rightNum) && compareTo.trim() !== '';
  switch (cond) {
    case 'eq': return String(value) === compareTo || (numeric && leftNum === rightNum);
    case 'ne': return String(value) !== compareTo && (!numeric || leftNum !== rightNum);
    case 'gt': return numeric && leftNum > rightNum;
    case 'gte': return numeric && leftNum >= rightNum;
    case 'lt': return numeric && leftNum < rightNum;
    case 'lte': return numeric && leftNum <= rightNum;
    default: return false;
  }
}

export function applyDiagramStyleTarget(node: SVGElement, target: DiagramBindTarget, result: string): void {
  if (target === 'text') {
    node.textContent = result;
    return;
  }
  if (target === 'opacity') {
    node.setAttribute('opacity', String(clamp(Number(result), 0, 1)));
    return;
  }

  node.setAttribute(target, result);
  const descendantTargets = target === 'stroke' ? ['fill', 'stroke'] : [target];
  for (const descendantTarget of descendantTargets) {
    node.querySelectorAll<SVGElement>(`[${descendantTarget}]`).forEach(child => {
      if (child.closest(`[${SELECTION_ADORNMENT_ATTR}]`)) return;
      child.setAttribute(descendantTarget, result);
    });
  }
}

export function parseSvgTemplate(text: string): { template: DiagramTemplateSvg; width: number; height: number } {
  const parser = new DOMParser();
  const doc = parser.parseFromString(text, 'image/svg+xml');
  if (doc.querySelector('parsererror')) throw new Error('The selected file is not valid SVG.');
  const svg = doc.documentElement;
  if (!svg || svg.localName.toLowerCase() !== 'svg') throw new Error('The selected file does not contain an SVG root.');

  sanitizeSvgTree(svg);

  const dims = svgDimensions(svg);
  const viewBox = svg.getAttribute('viewBox') || `0 0 ${dims.width} ${dims.height}`;
  const serializer = new XMLSerializer();
  const content = Array.from(svg.childNodes).map(node => serializer.serializeToString(node)).join('\n');
  if (!content.trim()) throw new Error('The SVG did not contain importable content.');

  return {
    template: { viewBox, content },
    width: dims.width,
    height: dims.height,
  };
}

class SvgDiagramWidget extends BaseComponent {
  private config: SvgDiagramConfig = clone(DEFAULT_CONFIG);
  private overlayEl: HTMLElement | null = null;
  private selectedKind: SelectionKind | null = null;
  private selectedId = '';
  private selectedIds = new Set<string>();
  private editorClipboard: { kind: SelectionKind; items: Array<DiagramElement | DiagramOverlayWidget> } | null = null;
  private editorTool: 'select' | DiagramElementType | 'widget' = 'select';
  private dragState: null | {
    mode: 'move' | 'resize' | 'line-start' | 'line-end' | 'shape-point';
    id: string;
    kind: SelectionKind;
    ids: string[];
    pointIndex?: number;
    startX: number;
    startY: number;
    original: any;
    originals: Map<string, any>;
  } = null;
  private marqueeState: null | {
    startX: number;
    startY: number;
    currentX: number;
    currentY: number;
    additive: boolean;
  } = null;
  private storeUnsubs: Array<() => void> = [];
  private editorStoreUnsubs: Array<() => void> = [];
  private runtimeResizeObserver: ResizeObserver | null = null;
  private editorResizeObserver: ResizeObserver | null = null;
  private requestedIconSets = new Set<string>();
  private childConfigHost: HTMLElement | null = null;
  private childConfigOverlays = new Set<HTMLElement>();
  private handleEditorResize = (): void => {
    this.positionEditorOverlay();
    requestAnimationFrame(() => this.fitEditorStage());
  };
  private handleEditorKeyDown = (e: KeyboardEvent): void => {
    if (!this.overlayEl) return;
    const key = e.key.toLowerCase();
    const command = e.ctrlKey || e.metaKey;
    if (e.key === 'Escape') {
      e.preventDefault();
      this.closeEditor(false);
    } else if (command && key === 's') {
      e.preventDefault();
      this.collectGlobalEditorConfig();
      this.closeEditor(true);
    } else if (command && key === 'c' && !isEditableTarget(e.target)) {
      e.preventDefault();
      this.copySelected();
    } else if (command && key === 'v' && !isEditableTarget(e.target)) {
      e.preventDefault();
      this.pasteClipboard();
    }
  };

  setConfig(c: Partial<SvgDiagramConfig> & Record<string, any>): void {
    this.config = normalizeSvgDiagramConfig(c);
    this.rerender();
  }

  getConfig(): SvgDiagramConfig {
    return clone(this.config);
  }

  getPropertySchema(): PropertyField[] {
    return [
      { name: 'title', type: 'string', label: 'Title', default: 'SVG Diagram' },
      { name: 'width', type: 'number', label: 'Canvas width', default: 1200, context: { min: 200, step: 10 } },
      { name: 'height', type: 'number', label: 'Canvas height', default: 700, context: { min: 200, step: 10 } },
      { name: 'background', type: 'color', label: 'Background', default: '#0f172a' },
    ];
  }

  openConfig(): void {
    this.openEditor();
  }

  protected render(): void {
    this.ensureIconSetsLoaded(false);
    this.innerHTML = `
      <div class="sdw-runtime" style="position:relative;width:100%;height:100%;overflow:hidden;background:${escAttr(this.config.background)};">
        ${this.renderSvg(false)}
        <div class="sdw-widget-layer" style="position:absolute;inset:0;pointer-events:auto;"></div>
      </div>
    `;
    this.mountRuntimeWidgets();
    this.updateCardTitle();
  }

  protected attachEventListeners(): void {
    this.subscribeRuntimeBindings();
    this.observeRuntimeViewport();
  }

  protected detachEventListeners(): void {
    this.clearRuntimeSubscriptions();
    this.clearRuntimeViewportObserver();
  }

  disconnectedCallback(): void {
    this.closeEditor(false);
    super.disconnectedCallback();
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (card && typeof card.setTitle === 'function') card.setTitle(this.config.title ?? 'SVG Diagram');
    const body = card?.querySelector?.('.widget-body') as HTMLElement | null;
    if (body) {
      body.style.padding = '0';
      body.style.overflow = 'hidden';
    }
  }

  private renderSvg(editor: boolean): string {
    const classes = editor ? 'sdw-svg sdw-svg-editor' : 'sdw-svg';
    const template = this.renderTemplateSvg();
    const elements = [...this.config.elements].map(el => this.renderElement(el, editor)).join('');
    return `
      <svg class="${classes}" viewBox="0 0 ${this.config.width} ${this.config.height}" preserveAspectRatio="xMidYMid meet"
           style="position:absolute;inset:0;width:100%;height:100%;display:block;background:${escAttr(this.config.background)};"
           xmlns="http://www.w3.org/2000/svg">
        ${template}
        ${elements}
      </svg>
    `;
  }

  private renderTemplateSvg(): string {
    const tpl = this.config.templateSvg;
    if (!tpl?.content || !tpl.viewBox) return '';
    return `
      <svg data-template-layer="true" x="0" y="0" width="${this.config.width}" height="${this.config.height}"
           viewBox="${escAttr(tpl.viewBox)}" preserveAspectRatio="xMidYMid meet"
           style="pointer-events:none;">
        ${tpl.content}
      </svg>
    `;
  }

  private renderElement(el: DiagramElement, editor: boolean): string {
    const selected = editor && this.isSelected('element', el.id);
    const common = `data-id="${escAttr(el.id)}" data-kind="element" opacity="${clamp(el.opacity, 0, 1)}" style="cursor:${editor ? 'move' : 'default'};"`;
    if (el.type === 'line') {
      const x2 = el.x2 ?? el.x + el.w;
      const y2 = el.y2 ?? el.y + el.h;
      const line = `<line x1="${el.x}" y1="${el.y}" x2="${x2}" y2="${y2}" stroke="${escAttr(el.stroke)}" stroke-width="${el.strokeWidth}" stroke-linecap="round"/>`;
      const selection = selected ? `<line ${SELECTION_ADORNMENT_ATTR}="true" x1="${el.x}" y1="${el.y}" x2="${x2}" y2="${y2}" stroke="${SELECTION_STROKE}" stroke-width="${Math.max(3, el.strokeWidth + 4)}" stroke-linecap="round" stroke-dasharray="8 6" fill="none" pointer-events="none" opacity="0.95"/>` : '';
      return `<g ${common}>${line}${selection}</g>`;
    }
    if (el.type === 'shape') {
      const points = shapePoints(el).map(pt => `${pt.x},${pt.y}`).join(' ');
      const polyline = `<polyline points="${escAttr(points)}" fill="${escAttr(el.fill)}" stroke="${escAttr(el.stroke)}" stroke-width="${el.strokeWidth}" stroke-linecap="round" stroke-linejoin="round"/>`;
      const selection = selected ? `<polyline ${SELECTION_ADORNMENT_ATTR}="true" points="${escAttr(points)}" fill="none" stroke="${SELECTION_STROKE}" stroke-width="${Math.max(3, el.strokeWidth + 3)}" stroke-linecap="round" stroke-linejoin="round" stroke-dasharray="8 6" pointer-events="none" opacity="0.95"/>` : '';
      return `<g ${common}>${polyline}${selection}</g>`;
    }
    if (el.type === 'rect') {
      const rect = `<rect x="${el.x}" y="${el.y}" width="${el.w}" height="${el.h}" rx="4" fill="${escAttr(el.fill)}" stroke="${escAttr(el.stroke)}" stroke-width="${el.strokeWidth}"/>`;
      const selection = selected ? `<rect ${SELECTION_ADORNMENT_ATTR}="true" x="${el.x}" y="${el.y}" width="${el.w}" height="${el.h}" rx="4" fill="none" stroke="${SELECTION_STROKE}" stroke-width="${Math.max(3, el.strokeWidth + 3)}" stroke-dasharray="8 6" pointer-events="none" opacity="0.95"/>` : '';
      return `<g ${common}>${rect}${selection}</g>`;
    }
    if (el.type === 'circle') {
      const cx = el.x + el.w / 2;
      const cy = el.y + el.h / 2;
      const rx = Math.max(1, el.w / 2);
      const ry = Math.max(1, el.h / 2);
      const ellipse = `<ellipse cx="${cx}" cy="${cy}" rx="${rx}" ry="${ry}" fill="${escAttr(el.fill)}" stroke="${escAttr(el.stroke)}" stroke-width="${el.strokeWidth}"/>`;
      const selection = selected ? `<ellipse ${SELECTION_ADORNMENT_ATTR}="true" cx="${cx}" cy="${cy}" rx="${rx}" ry="${ry}" fill="none" stroke="${SELECTION_STROKE}" stroke-width="${Math.max(3, el.strokeWidth + 3)}" stroke-dasharray="8 6" pointer-events="none" opacity="0.95"/>` : '';
      return `<g ${common}>${ellipse}${selection}</g>`;
    }
    if (el.type === 'text') {
      const bounds = elementBounds(el);
      const text = `<text ${common} x="${el.x}" y="${el.y + el.fontSize}" fill="${escAttr(el.fill)}" stroke="${escAttr(el.stroke)}" stroke-width="${el.strokeWidth}" font-size="${el.fontSize}" font-family="ui-sans-serif,system-ui" dominant-baseline="alphabetic">${esc(el.text || 'Text')}</text>`;
      const selection = selected ? `<rect ${SELECTION_ADORNMENT_ATTR}="true" x="${bounds.x}" y="${bounds.y}" width="${bounds.w}" height="${bounds.h}" rx="3" fill="none" stroke="${SELECTION_STROKE}" stroke-width="2" stroke-dasharray="6 5" pointer-events="none" opacity="0.95"/>` : '';
      return `<g>${text}${selection}</g>`;
    }
    const iconName = el.icon || 'mdi:factory';
    const icon = getIconSVG(iconName, el.fill, Math.max(el.w, el.h));
    const body = icon ? icon.replace(/^<svg\b/, `<svg data-id="${escAttr(el.id)}" data-kind="element"`).replace(/width="[^"]+"/, `width="${el.w}"`).replace(/height="[^"]+"/, `height="${el.h}"`) : '';
    const fallback = `
      <g>
        <rect width="${el.w}" height="${el.h}" rx="6" fill="transparent" stroke="${escAttr(el.fill)}" stroke-width="2" stroke-dasharray="5 4" opacity="0.65"/>
        <text x="${el.w / 2}" y="${el.h / 2}" fill="${escAttr(el.fill)}" font-size="${Math.max(10, Math.min(el.w, el.h) * 0.18)}" text-anchor="middle" dominant-baseline="middle" font-family="ui-sans-serif,system-ui">${esc(iconName)}</text>
      </g>`;
    return `<g ${common} transform="translate(${el.x} ${el.y})">${body || fallback}${selected ? `<rect ${SELECTION_ADORNMENT_ATTR}="true" x="0" y="0" width="${el.w}" height="${el.h}" fill="none" stroke="#f8fafc" stroke-width="2" stroke-dasharray="6 5" pointer-events="none"/>` : ''}</g>`;
  }

  private mountRuntimeWidgets(): void {
    const layer = this.querySelector<HTMLElement>('.sdw-widget-layer');
    if (!layer) return;
    layer.innerHTML = '';
    for (const w of this.config.widgets) {
      const wrap = document.createElement('div');
      wrap.className = 'sdw-overlay-widget';
      wrap.dataset.id = w.id;
      wrap.dataset.kind = 'widget';
      wrap.style.cssText = this.overlayStyle(w, false);
      layer.appendChild(wrap);
      void this.mountOverlayWidgetCard(wrap, w, false);
    }
    this.updateRuntimeWidgetLayerViewport();
  }

  private observeRuntimeViewport(): void {
    this.clearRuntimeViewportObserver();
    const runtime = this.querySelector<HTMLElement>('.sdw-runtime');
    if (!runtime) return;
    this.runtimeResizeObserver = new ResizeObserver(() => this.updateRuntimeWidgetLayerViewport());
    this.runtimeResizeObserver.observe(runtime);
    requestAnimationFrame(() => this.updateRuntimeWidgetLayerViewport());
  }

  private clearRuntimeViewportObserver(): void {
    this.runtimeResizeObserver?.disconnect();
    this.runtimeResizeObserver = null;
  }

  private updateRuntimeWidgetLayerViewport(): void {
    const runtime = this.querySelector<HTMLElement>('.sdw-runtime');
    const layer = this.querySelector<HTMLElement>('.sdw-widget-layer');
    if (!runtime || !layer) return;
    const w = runtime.clientWidth;
    const h = runtime.clientHeight;
    if (w <= 0 || h <= 0) return;

    const canvasAspect = this.config.width / this.config.height;
    const runtimeAspect = w / h;
    let left = 0;
    let top = 0;
    let width = w;
    let height = h;

    if (runtimeAspect > canvasAspect) {
      height = h;
      width = height * canvasAspect;
      left = (w - width) / 2;
    } else {
      width = w;
      height = width / canvasAspect;
      top = (h - height) / 2;
    }

    const scale = width / this.config.width;
    layer.style.left = `${left}px`;
    layer.style.top = `${top}px`;
    layer.style.width = `${this.config.width}px`;
    layer.style.height = `${this.config.height}px`;
    layer.style.right = 'auto';
    layer.style.bottom = 'auto';
    layer.style.transformOrigin = '0 0';
    layer.style.transform = `scale(${scale})`;
  }

  private overlayStyle(w: DiagramOverlayWidget, editor: boolean): string {
    const selected = editor && this.isSelected('widget', w.id);
    return [
      'position:absolute',
      `left:${(w.x / this.config.width) * 100}%`,
      `top:${(w.y / this.config.height) * 100}%`,
      `width:${(w.w / this.config.width) * 100}%`,
      `height:${(w.h / this.config.height) * 100}%`,
      'box-sizing:border-box',
      'overflow:hidden',
      selected ? 'outline:2px dashed #f8fafc' : 'outline:0',
      editor ? 'cursor:move' : '',
    ].join(';');
  }

  private subscribeRuntimeBindings(): void {
    this.clearRuntimeSubscriptions();
    const store = getMirrorStore();
    const paths = new Set<string>();
    for (const el of this.config.elements) {
      for (const binding of el.bindings || []) {
        if (binding.tagPath) paths.add(store.toAbsolute(binding.tagPath));
      }
    }
    for (const path of paths) {
      this.storeUnsubs.push(store.subscribeTagReference(path, () => this.applyRuntimeBindings()));
    }
    this.applyRuntimeBindings();
  }

  private clearRuntimeSubscriptions(): void {
    for (const unsub of this.storeUnsubs) unsub();
    this.storeUnsubs = [];
  }

  private clearEditorSubscriptions(): void {
    for (const unsub of this.editorStoreUnsubs) unsub();
    this.editorStoreUnsubs = [];
  }

  private applyRuntimeBindings(): void {
    const svg = this.querySelector<SVGSVGElement>('.sdw-svg');
    if (!svg) return;
    this.applyBindingsToSvg(svg);
  }

  private applyBindingsToSvg(svg: SVGSVGElement): void {
    const store = getMirrorStore();
    for (const el of this.config.elements) {
      const node = svg.querySelector<SVGElement>(`[data-id="${cssEscape(el.id)}"]`);
      if (!node) continue;
      for (const binding of el.bindings || []) {
        if (!binding.tagPath) continue;
        const value = store.resolveTagReference(store.toAbsolute(binding.tagPath));
        const result = resolveDiagramBinding(value, binding);
        applyDiagramStyleTarget(node, binding.target, result);
      }
    }
  }

  private openEditor(): void {
    if (this.overlayEl) return;
    this.clearSelection(false);
    const overlay = document.createElement('div');
    overlay.className = 'sdw-editor';
    overlay.style.cssText = 'position:fixed;z-index:9000;background:var(--content-bg);color:var(--content-text);display:flex;flex-direction:column;overflow:hidden;box-shadow:0 0 0 1px var(--border-color);';
    document.body.appendChild(overlay);
    this.overlayEl = overlay;
    this.positionEditorOverlay();
    window.addEventListener('resize', this.handleEditorResize);
    document.addEventListener('keydown', this.handleEditorKeyDown);
    this.renderEditor();
  }

  private closeEditor(save: boolean): void {
    if (!this.overlayEl) return;
    this.cleanupChildWidgetConfig();
    this.clearEditorSubscriptions();
    this.clearEditorViewportObserver();
    window.removeEventListener('resize', this.handleEditorResize);
    document.removeEventListener('keydown', this.handleEditorKeyDown);
    this.overlayEl.remove();
    this.overlayEl = null;
    this.dragState = null;
    if (save) {
      this.rerender();
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
    }
  }

  private positionEditorOverlay(): void {
    if (!this.overlayEl) return;
    const content = document.querySelector<HTMLElement>('app-content');
    const rect = content?.getBoundingClientRect();
    if (rect && rect.width > 0 && rect.height > 0) {
      this.overlayEl.style.top = `${Math.max(0, rect.top)}px`;
      this.overlayEl.style.left = `${Math.max(0, rect.left)}px`;
      this.overlayEl.style.right = `${Math.max(0, window.innerWidth - rect.right)}px`;
      this.overlayEl.style.bottom = `${Math.max(0, window.innerHeight - rect.bottom)}px`;
      return;
    }
    this.overlayEl.style.top = '60px';
    this.overlayEl.style.left = '0';
    this.overlayEl.style.right = '0';
    this.overlayEl.style.bottom = '40px';
  }

  private observeEditorViewport(): void {
    this.clearEditorViewportObserver();
    const main = this.overlayEl?.querySelector<HTMLElement>('.sdw-editor-main');
    if (!main) return;
    this.editorResizeObserver = new ResizeObserver(() => this.fitEditorStage());
    this.editorResizeObserver.observe(main);
    requestAnimationFrame(() => this.fitEditorStage());
  }

  private clearEditorViewportObserver(): void {
    this.editorResizeObserver?.disconnect();
    this.editorResizeObserver = null;
  }

  private fitEditorStage(): void {
    const main = this.overlayEl?.querySelector<HTMLElement>('.sdw-editor-main');
    const stage = this.overlayEl?.querySelector<HTMLElement>('#sdw-stage');
    if (!main || !stage) return;

    const mainWidth = main.clientWidth;
    const mainHeight = main.clientHeight;
    if (mainWidth <= 0 || mainHeight <= 0) return;

    const styles = window.getComputedStyle(main);
    const paddingX = cssNumber(styles.paddingLeft) + cssNumber(styles.paddingRight);
    const paddingY = cssNumber(styles.paddingTop) + cssNumber(styles.paddingBottom);
    const availableWidth = Math.max(1, mainWidth - paddingX);
    const availableHeight = Math.max(1, mainHeight - paddingY);
    const aspect = positiveNumber(this.config.width / this.config.height, 1);

    let width = availableWidth;
    let height = width / aspect;
    if (height > availableHeight) {
      height = availableHeight;
      width = height * aspect;
    }

    stage.style.width = `${width}px`;
    stage.style.height = `${height}px`;
  }

  private renderEditor(): void {
    const overlay = this.overlayEl;
    if (!overlay) return;
    this.ensureIconSetsLoaded(true);
    overlay.innerHTML = this.editorHtml();
    this.attachEditorListeners(overlay);
    this.mountEditorWidgets();
    this.subscribeEditorBindings();
    this.syncSelectionHandles();
    this.observeEditorViewport();
  }

  private editorHtml(): string {
    return `
      <style>
        .sdw-editor{font-size:0.875rem;line-height:1.4}
        .sdw-editor button,.sdw-editor input,.sdw-editor select,.sdw-editor textarea{font:inherit}
        .sdw-editor-header{height:46px;display:flex;align-items:center;gap:8px;padding:0 14px;border-bottom:1px solid var(--border-color);flex-shrink:0}
        .sdw-editor-title{font-size:0.875rem;font-weight:600;color:var(--accent-color);white-space:nowrap}
        .sdw-btn{border:1px solid var(--border-color);background:color-mix(in srgb,var(--border-color) 24%,transparent);color:var(--content-text);border-radius:4px;padding:5px 10px;cursor:pointer;font-size:0.75rem;line-height:1.25}
        .sdw-btn.active{border-color:var(--accent-color);color:var(--accent-color);background:color-mix(in srgb,var(--accent-color) 12%,transparent)}
        .sdw-btn:disabled{opacity:.45;cursor:not-allowed}
        .sdw-field{width:100%;border:1px solid var(--border-color);border-radius:4px;background:var(--content-bg);color:var(--content-text);padding:5px 7px;font-size:0.8125rem;line-height:1.35;box-sizing:border-box}
        .sdw-widget-select{width:100%;background:var(--widget-bg,var(--content-bg));border:1px solid var(--widget-border,var(--border-color));border-radius:4px;padding:4px 6px;font-size:var(--widget-label-font-size,0.8125rem);color:var(--modal-text,var(--content-text));font-family:var(--widget-font-family,inherit);outline:none;box-sizing:border-box}
        .sdw-widget-select:focus{border-color:var(--accent-color)}
        .sdw-label{font-size:0.72rem;opacity:.68;margin-bottom:4px;display:block}
        .sdw-side{padding:12px;overflow:auto;font-size:0.8125rem}
        .sdw-section-label{font-size:0.75rem;font-weight:600;color:var(--accent-color);letter-spacing:0.05em;text-transform:uppercase;margin-bottom:8px;padding-bottom:6px;border-bottom:1px solid color-mix(in srgb,var(--accent-color) 18%,var(--border-color))}
        .sdw-panel-title{font-size:0.75rem;font-weight:600;color:var(--accent-color);letter-spacing:0.05em;text-transform:uppercase;margin-bottom:10px}
        .sdw-subtle-title{font-size:0.72rem;font-weight:600;opacity:.68;margin:8px 0 4px}
        .sdw-muted{font-size:0.8125rem;line-height:1.5;opacity:.58}
        .sdw-editor-main{position:relative;overflow:hidden;background:color-mix(in srgb,var(--content-bg) 76%,#000);display:flex;align-items:center;justify-content:center;padding:18px;box-sizing:border-box}
        .sdw-stage{position:relative;flex:0 0 auto;width:100%;height:auto;min-width:0;max-width:100%;max-height:100%;aspect-ratio:var(--sdw-aspect);background:var(--sdw-stage-bg);box-shadow:0 0 0 1px var(--border-color),0 16px 40px rgba(0,0,0,.25)}
      </style>
      <div class="sdw-editor-header">
        <strong class="sdw-editor-title">SVG Diagram Editor</strong>
        <input id="sdw-title" class="sdw-field" value="${escAttr(this.config.title)}" style="width:220px;">
        <button id="sdw-close" class="sdw-btn" style="margin-left:auto;">Exit editor</button>
        <button id="sdw-save" class="sdw-btn active">Save and exit</button>
      </div>
      <div style="flex:1;min-height:0;display:grid;grid-template-columns:176px minmax(0,1fr) 300px;">
        <aside class="sdw-side" style="border-right:1px solid var(--border-color);">
          <div class="sdw-section-label">Tools</div>
          <div style="display:grid;grid-template-columns:1fr;gap:6px;">
            ${this.toolButton('select', 'Select / move')}
            ${this.toolButton('line', 'Line')}
            ${this.toolButton('shape', 'Shape')}
            ${this.toolButton('rect', 'Rectangle')}
            ${this.toolButton('circle', 'Circle')}
            ${this.toolButton('text', 'Text')}
            ${this.toolButton('icon', 'Icon')}
            ${this.toolButton('widget', 'Widget overlay')}
          </div>
          <div style="height:1px;background:var(--border-color);margin:12px 0;"></div>
          <label class="sdw-label">Canvas width</label>
          <input id="sdw-width" class="sdw-field" type="number" min="200" value="${this.config.width}">
          <label class="sdw-label" style="margin-top:8px;">Canvas height</label>
          <input id="sdw-height" class="sdw-field" type="number" min="200" value="${this.config.height}">
          <label class="sdw-label" style="margin-top:8px;">Background</label>
          <input id="sdw-bg" class="sdw-field" type="color" value="${toHex(this.config.background)}">
          <div style="height:1px;background:var(--border-color);margin:12px 0;"></div>
          <div class="sdw-section-label">Template</div>
          <input id="sdw-import-svg-file" type="file" accept=".svg,image/svg+xml" style="display:none;">
          <button id="sdw-import-svg" class="sdw-btn" style="width:100%;margin-bottom:6px;">Import SVG template</button>
          <button id="sdw-remove-template" class="sdw-btn" style="width:100%;margin-bottom:6px;color:#f87171;" ${this.config.templateSvg ? '' : 'disabled'}>Remove template</button>
          ${this.config.templateSvg ? `<div class="sdw-muted" style="font-size:0.72rem;margin-bottom:10px;">Template layer imported. Draw over it, then remove it when finished.</div>` : ''}
          <div style="height:1px;background:var(--border-color);margin:12px 0;"></div>
          <button id="sdw-duplicate" class="sdw-btn" style="width:100%;margin-bottom:6px;" ${this.selectedKind ? '' : 'disabled'}>Duplicate</button>
          <button id="sdw-forward" class="sdw-btn" style="width:100%;margin-bottom:6px;" ${this.selectedKind === 'element' ? '' : 'disabled'}>Bring forward</button>
          <button id="sdw-back" class="sdw-btn" style="width:100%;margin-bottom:6px;" ${this.selectedKind === 'element' ? '' : 'disabled'}>Send backward</button>
          <button id="sdw-delete" class="sdw-btn" style="width:100%;color:#f87171;" ${this.selectedKind ? '' : 'disabled'}>Delete</button>
        </aside>
        <main class="sdw-editor-main">
          <div id="sdw-stage" class="sdw-stage" style="--sdw-aspect:${this.config.width / this.config.height};--sdw-stage-bg:${escAttr(this.config.background)};">
            ${this.renderSvg(true)}
            <div id="sdw-editor-widget-layer" style="position:absolute;inset:0;pointer-events:none;"></div>
            <div id="sdw-selection-layer" style="position:absolute;inset:0;pointer-events:none;"></div>
          </div>
        </main>
        <aside id="sdw-inspector" class="sdw-side" style="border-left:1px solid var(--border-color);">
          ${this.inspectorHtml()}
        </aside>
      </div>
    `;
  }

  private toolButton(tool: string, label: string): string {
    return `<button class="sdw-btn ${this.editorTool === tool ? 'active' : ''}" data-tool="${tool}">${label}</button>`;
  }

  private inspectorHtml(): string {
    if (!this.selectedKind) {
      return '<div class="sdw-muted">Select an element, or choose a tool and click the canvas to add one.</div>';
    }
    if (this.selectedIds.size > 1) return this.multiSelectionInspectorHtml();
    if (this.selectedKind === 'widget') return this.widgetInspectorHtml();
    const el = this.selectedElement();
    if (!el) return '';
    if (el.type === 'shape') return this.shapeInspectorHtml(el);
    const isLine = el.type === 'line';
    const canHaveNoFill = el.type === 'rect' || el.type === 'circle' || el.type === 'text';
    const fillLabel = el.type === 'icon' ? 'Icon color' : 'Fill';
    const fillValue = noFill(el.fill) ? '#000000' : toHex(el.fill);
    return `
      <div class="sdw-panel-title">${el.type} properties</div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;">
        ${field('X', 'x', el.x)} ${field('Y', 'y', el.y)}
        ${isLine ? `${field('X2', 'x2', el.x2 ?? el.x + el.w)} ${field('Y2', 'y2', el.y2 ?? el.y + el.h)}` : `${field('W', 'w', el.w)} ${field('H', 'h', el.h)}`}
      </div>
      ${el.type === 'text' ? `<label class="sdw-label" style="margin-top:8px;">Text</label><input class="sdw-field sdw-prop" data-prop="text" value="${escAttr(el.text || '')}">` : ''}
      ${el.type === 'icon' ? `<label class="sdw-label" style="margin-top:8px;">Icon</label><div style="display:flex;gap:6px;align-items:center;"><icon-picker id="sdw-icon" value="${escAttr(el.icon || 'mdi:factory')}"></icon-picker><input class="sdw-field sdw-prop" data-prop="icon" value="${escAttr(el.icon || 'mdi:factory')}"></div>` : ''}
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:8px;">
        <div>
          <label class="sdw-label">${fillLabel}</label>
          <div style="display:flex;gap:6px;align-items:center;">
            <input class="sdw-field sdw-prop" data-prop="fill" type="color" value="${fillValue}" style="flex:1;min-width:0;" ${canHaveNoFill && noFill(el.fill) ? 'disabled' : ''}>
            ${canHaveNoFill ? fillCheckboxHtml(!noFill(el.fill)) : ''}
          </div>
        </div>
        ${el.type === 'icon' ? '' : `<div><label class="sdw-label">Stroke</label><input class="sdw-field sdw-prop" data-prop="stroke" type="color" value="${toHex(el.stroke)}"></div>`}
        ${el.type === 'icon' ? '' : field('Line thickness', 'strokeWidth', el.strokeWidth)}
        ${el.type === 'icon' ? '' : field('Font size', 'fontSize', el.fontSize)}
        ${field('Opacity', 'opacity', el.opacity, 'number', '0.05')}
      </div>
      <div style="height:1px;background:var(--border-color);margin:14px 0;"></div>
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">
        <span class="sdw-panel-title" style="margin-bottom:0;">Tag-driven styles</span>
        <button id="sdw-add-binding" class="sdw-btn">+ Binding</button>
      </div>
      ${this.bindingsHtml(el.bindings)}
    `;
  }

  private shapeInspectorHtml(el: DiagramElement): string {
    const fillValue = noFill(el.fill) ? '#000000' : toHex(el.fill);
    return `
      <div class="sdw-panel-title">shape properties</div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;">
        ${field('X', 'x', el.x)} ${field('Y', 'y', el.y)}
        ${field('W', 'w', el.w)} ${field('H', 'h', el.h)}
      </div>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:8px;">
        <div>
          <label class="sdw-label">Fill</label>
          <div style="display:flex;gap:6px;align-items:center;">
            <input class="sdw-field sdw-prop" data-prop="fill" type="color" value="${fillValue}" style="flex:1;min-width:0;" ${noFill(el.fill) ? 'disabled' : ''}>
            ${fillCheckboxHtml(!noFill(el.fill))}
          </div>
        </div>
        <div><label class="sdw-label">Stroke</label><input class="sdw-field sdw-prop" data-prop="stroke" type="color" value="${toHex(el.stroke)}"></div>
        ${field('Line thickness', 'strokeWidth', el.strokeWidth)}
        ${field('Opacity', 'opacity', el.opacity, 'number', '0.05')}
      </div>
      <div style="height:1px;background:var(--border-color);margin:14px 0;"></div>
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">
        <span class="sdw-panel-title" style="margin-bottom:0;">Points</span>
        <button id="sdw-add-point" class="sdw-btn">+ Point</button>
      </div>
      ${shapePoints(el).map((pt, idx) => `
        <div class="sdw-shape-point" data-point-idx="${idx}" style="display:grid;grid-template-columns:1fr 1fr auto;gap:6px;align-items:end;margin-bottom:6px;">
          ${pointField(`P${idx + 1} X`, 'x', pt.x)}
          ${pointField(`P${idx + 1} Y`, 'y', pt.y)}
          <button class="sdw-btn sdw-del-point" style="color:#f87171;" ${shapePoints(el).length <= 2 ? 'disabled' : ''}>Del</button>
        </div>
      `).join('')}
      <div style="height:1px;background:var(--border-color);margin:14px 0;"></div>
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:8px;">
        <span class="sdw-panel-title" style="margin-bottom:0;">Tag-driven styles</span>
        <button id="sdw-add-binding" class="sdw-btn">+ Binding</button>
      </div>
      ${this.bindingsHtml(el.bindings)}
    `;
  }

  private widgetInspectorHtml(): string {
    const w = this.selectedWidget();
    if (!w) return '';
    const widgetOptions = getEmbeddableWidgetTypes().map(type => {
      const meta = getWidgetMeta(type);
      const label = meta ? `${meta.icon} ${meta.name}` : type;
      return `<option value="${escAttr(type)}"${w.type === type ? ' selected' : ''}>${esc(label)}</option>`;
    }).join('');
    return `
      <div class="sdw-panel-title">Widget overlay</div>
      <label class="sdw-label">Widget type</label>
      <select id="sdw-widget-type" class="sdw-widget-select">${widgetOptions}</select>
      <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-top:8px;">
        ${field('X', 'x', w.x)} ${field('Y', 'y', w.y)}
        ${field('W', 'w', w.w)} ${field('H', 'h', w.h)}
      </div>
      <div style="display:flex;gap:6px;margin-top:10px;">
        <button id="sdw-config-child" class="sdw-btn" style="flex:1;">Configure widget</button>
      </div>
    `;
  }

  private multiSelectionInspectorHtml(): string {
    const label = this.selectedKind === 'widget' ? 'widget overlays' : 'elements';
    return `
      <div class="sdw-panel-title">${this.selectedIds.size} ${label} selected</div>
    `;
  }

  private bindingsHtml(bindings: DiagramBinding[]): string {
    if (!bindings.length) return '<div class="sdw-muted">No tag bindings configured.</div>';
    return bindings.map((binding, idx) => `
      <div class="sdw-binding" data-idx="${idx}" style="border:1px solid var(--border-color);border-radius:7px;padding:8px;margin-bottom:8px;">
        <div style="display:grid;grid-template-columns:1fr 84px auto;gap:6px;align-items:end;">
          <div><label class="sdw-label">Tag path</label><input class="sdw-field sdw-bind-path" value="${escAttr(binding.tagPath)}"></div>
          <button class="sdw-btn sdw-bind-browse" title="Browse tags">Browse</button>
          <button class="sdw-btn sdw-bind-del" style="color:#f87171;">Del</button>
        </div>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:6px;margin-top:6px;">
          <div><label class="sdw-label">Target</label><select class="sdw-field sdw-bind-target">${TARGET_OPTIONS.map(t => `<option value="${t}"${binding.target === t ? ' selected' : ''}>${t}</option>`).join('')}</select></div>
          <div><label class="sdw-label">Default</label>${bindingStyleInput('sdw-bind-default', binding.target, binding.defaultValue, 'default')}</div>
        </div>
        <div class="sdw-subtle-title">Rules</div>
        ${(binding.rules || []).map((rule, rIdx) => `
          <div class="sdw-rule" data-ridx="${rIdx}" style="display:grid;grid-template-columns:64px 1fr 1fr auto;gap:5px;margin-bottom:5px;">
            <select class="sdw-field sdw-rule-cond">${COND_OPTIONS.map(c => `<option value="${c}"${rule.cond === c ? ' selected' : ''}>${c}</option>`).join('')}</select>
            <input class="sdw-field sdw-rule-value" value="${escAttr(rule.value)}" placeholder="value">
            ${bindingStyleInput('sdw-rule-result', binding.target, rule.result, 'style')}
            <button class="sdw-btn sdw-rule-del" style="color:#f87171;">x</button>
          </div>`).join('')}
        <button class="sdw-btn sdw-add-rule" style="width:100%;">+ Rule</button>
      </div>
    `).join('');
  }

  private mountEditorWidgets(): void {
    const layer = this.overlayEl?.querySelector<HTMLElement>('#sdw-editor-widget-layer');
    if (!layer) return;
    layer.innerHTML = '';
    for (const w of this.config.widgets) {
      const wrap = document.createElement('div');
      wrap.className = 'sdw-editor-overlay-widget';
      wrap.dataset.id = w.id;
      wrap.dataset.kind = 'widget';
      wrap.style.cssText = this.overlayStyle(w, true);
      wrap.style.pointerEvents = 'auto';
      layer.appendChild(wrap);
      this.mountEditorWidgetPreview(wrap, w);
    }
  }

  private mountEditorWidgetPreview(wrap: HTMLElement, w: DiagramOverlayWidget): void {
    const frame = document.createElement('div');
    frame.style.cssText = [
      'width:100%',
      'height:100%',
      'box-sizing:border-box',
      'border:1px dashed color-mix(in srgb,var(--accent-color) 45%,var(--border-color))',
      'background:color-mix(in srgb,var(--content-bg) 18%,transparent)',
      'pointer-events:none',
      'overflow:hidden',
    ].join(';');
    wrap.appendChild(frame);

    void this.mountOverlayWidgetCard(frame, w, true);
  }

  private async mountOverlayWidgetCard(host: HTMLElement, w: DiagramOverlayWidget, preview: boolean): Promise<void> {
    host.addEventListener('widget-config-save', ((e: CustomEvent) => this.handleOverlayWidgetConfigSave(e, w.id)) as EventListener);
    host.innerHTML = this.editorWidgetFallback(w);

    try {
      if (!customElements.get(w.type)) await ensureWidgetTypeLoaded(w.type);
      if (!this.isConnected || !host.isConnected) return;
      if (!customElements.get(w.type)) throw new Error(`Widget type is not registered: ${w.type}`);
      host.innerHTML = '';

      const card = document.createElement('widget-card') as WidgetCard;
      card.classList.add('sdw-overlay-card');
      card.setWidgetId(w.id);
      card.setMode('view');
      card.setTitle(getWidgetMeta(w.type)?.name || w.type);
      card.setHasProperties(false);
      card.style.display = 'block';
      card.style.width = '100%';
      card.style.height = '100%';
      if (preview) card.style.pointerEvents = 'none';
      host.appendChild(card);

      if (!card.querySelector('.widget-body') && typeof (card as any).render === 'function') {
        (card as any).render();
      }
      const body = card.querySelector<HTMLElement>('.widget-body');
      if (!body) throw new Error('widget-card did not render a body');

      const child = document.createElement(w.type) as any;
      child.style.display = 'block';
      child.style.width = '100%';
      child.style.height = '100%';
      if (preview) child.style.pointerEvents = 'none';
      else {
        child.addEventListener('mousedown', (e: MouseEvent) => e.stopPropagation());
        child.addEventListener('touchstart', (e: TouchEvent) => e.stopPropagation(), { passive: true });
      }
      body.appendChild(child);

      if (typeof child.setConfig === 'function') child.setConfig(w.config || {});
      if (typeof child.setDashboardMode === 'function') child.setDashboardMode('view');
      else if (typeof child.setEditMode === 'function') child.setEditMode(false);
    } catch (err) {
      console.warn('SVG Diagram: failed to mount overlay widget', w.type, err);
      host.innerHTML = this.editorWidgetFallback(w);
    }
  }

  private handleOverlayWidgetConfigSave(e: CustomEvent, widgetId: string): void {
    const w = this.config.widgets.find(item => item.id === widgetId);
    if (!w) return;
    e.stopPropagation();
    const nextConfig = e.detail?.config;
    if (nextConfig && typeof nextConfig === 'object') w.config = clone(nextConfig);
    this.emit('widget-config-save', {
      config: this.getConfig(),
      silent: e.detail?.silent,
      forceDirty: e.detail?.forceDirty,
    });
  }

  private editorWidgetFallback(w: DiagramOverlayWidget): string {
    const meta = getWidgetMeta(w.type);
    const label = meta?.name || w.type;
    const icon = meta?.icon || '□';
    return `
      <div style="width:100%;height:100%;display:flex;align-items:center;justify-content:center;gap:8px;box-sizing:border-box;padding:8px;color:var(--content-text);font-size:12px;text-align:center;overflow:hidden;">
        <span style="font-size:16px;line-height:1;">${esc(icon)}</span>
        <span style="min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${esc(label)}</span>
      </div>
    `;
  }

  private attachEditorListeners(overlay: HTMLElement): void {
    overlay.querySelector('#sdw-close')?.addEventListener('click', () => this.closeEditor(false));
    overlay.querySelector('#sdw-save')?.addEventListener('click', () => {
      this.collectGlobalEditorConfig();
      this.closeEditor(true);
    });
    overlay.querySelectorAll<HTMLElement>('[data-tool]').forEach(btn => {
      btn.addEventListener('click', () => {
        this.editorTool = btn.dataset.tool as any;
        this.renderEditor();
      });
    });
    overlay.querySelector('#sdw-stage')?.addEventListener('pointerdown', this.handleStagePointerDown as EventListener);
    overlay.addEventListener('pointermove', this.handleEditorPointerMove as EventListener);
    overlay.addEventListener('pointerup', this.handleEditorPointerUp as EventListener);
    overlay.querySelectorAll<HTMLInputElement>('#sdw-title,#sdw-width,#sdw-height,#sdw-bg').forEach(input => {
      input.addEventListener('change', () => {
        this.collectGlobalEditorConfig();
        this.renderEditor();
      });
    });
    overlay.querySelector('#sdw-delete')?.addEventListener('click', () => this.deleteSelected());
    overlay.querySelector('#sdw-duplicate')?.addEventListener('click', () => this.duplicateSelected());
    overlay.querySelector('#sdw-forward')?.addEventListener('click', () => this.moveSelectedElement(1));
    overlay.querySelector('#sdw-back')?.addEventListener('click', () => this.moveSelectedElement(-1));
    overlay.querySelector('#sdw-import-svg')?.addEventListener('click', () => {
      overlay.querySelector<HTMLInputElement>('#sdw-import-svg-file')?.click();
    });
    overlay.querySelector<HTMLInputElement>('#sdw-import-svg-file')?.addEventListener('change', e => this.importSvgTemplate(e));
    overlay.querySelector('#sdw-remove-template')?.addEventListener('click', () => {
      this.config.templateSvg = undefined;
      this.renderEditor();
    });
    overlay.querySelectorAll<HTMLInputElement>('.sdw-prop').forEach(input => {
      input.addEventListener('change', () => this.updateSelectedElementProp(input.dataset.prop || '', input.value));
    });
    overlay.querySelector<HTMLInputElement>('.sdw-fill-enabled')?.addEventListener('change', e => {
      const el = this.selectedElement();
      if (!el) return;
      this.updateSelectedElementProp('fill', (e.target as HTMLInputElement).checked ? '#38bdf8' : 'none');
    });
    const iconPicker = overlay.querySelector('icon-picker#sdw-icon');
    iconPicker?.addEventListener('change', ((e: CustomEvent) => {
      this.updateSelectedElementProp('icon', String(e.detail || (e.target as any)?.value || ''));
    }) as EventListener);
    iconPicker?.addEventListener('icon-selected', ((e: CustomEvent) => {
      this.updateSelectedElementProp('icon', e.detail?.icon || e.detail?.name || '');
    }) as EventListener);
    overlay.querySelectorAll<HTMLInputElement>('.sdw-num').forEach(input => {
      input.addEventListener('change', () => this.updateSelectedGeometry(input.dataset.prop || '', Number(input.value)));
    });
    overlay.querySelectorAll<HTMLInputElement>('.sdw-point-num').forEach(input => {
      input.addEventListener('change', () => {
        const row = input.closest<HTMLElement>('.sdw-shape-point');
        this.updateShapePoint(Number(row?.dataset.pointIdx), input.dataset.coord || 'x', Number(input.value));
      });
    });
    overlay.querySelectorAll<HTMLElement>('.sdw-del-point').forEach(btn => {
      btn.addEventListener('click', () => {
        const row = btn.closest<HTMLElement>('.sdw-shape-point');
        this.deleteShapePoint(Number(row?.dataset.pointIdx));
      });
    });
    overlay.querySelector('#sdw-add-point')?.addEventListener('click', () => this.addShapePoint());
    overlay.querySelector('#sdw-add-binding')?.addEventListener('click', () => {
      const el = this.selectedElement();
      if (!el) return;
      const strokeOnly = el.type === 'line' || el.type === 'shape';
      el.bindings.push({ tagPath: '', target: strokeOnly ? 'stroke' : 'fill', defaultValue: strokeOnly ? el.stroke : el.fill, rules: [] });
      this.renderEditor();
    });
    this.attachBindingListeners(overlay);
    this.attachWidgetInspectorListeners(overlay);
  }

  private attachBindingListeners(overlay: HTMLElement): void {
    overlay.querySelectorAll<HTMLElement>('.sdw-binding').forEach(card => {
      const idx = Number(card.dataset.idx);
      card.querySelector('.sdw-bind-del')?.addEventListener('click', () => {
        const el = this.selectedElement();
        if (!el) return;
        el.bindings.splice(idx, 1);
        this.renderEditor();
      });
      card.querySelector('.sdw-bind-browse')?.addEventListener('click', () => {
        const input = card.querySelector<HTMLInputElement>('.sdw-bind-path');
        getTreeBrowserDialog().open('', 'Select Tag', selected => {
          if (input) input.value = selected;
          this.collectBindingCard(card, idx);
        }, true, input?.value || '');
      });
      card.querySelectorAll<HTMLInputElement | HTMLSelectElement>('input,select').forEach(input => {
        input.addEventListener('change', () => {
          this.collectBindingCard(card, idx);
          if (input.classList.contains('sdw-bind-target')) this.renderEditor();
        });
      });
      card.querySelector('.sdw-add-rule')?.addEventListener('click', () => {
        const el = this.selectedElement();
        const binding = el?.bindings[idx];
        if (!binding) return;
        binding.rules.push({ cond: 'eq', value: '1', result: binding.target === 'opacity' ? '1' : '#22c55e' });
        this.renderEditor();
      });
      card.querySelectorAll<HTMLElement>('.sdw-rule-del').forEach(btn => {
        btn.addEventListener('click', () => {
          const ridx = Number((btn.closest('.sdw-rule') as HTMLElement)?.dataset.ridx);
          const el = this.selectedElement();
          const binding = el?.bindings[idx];
          if (!binding) return;
          binding.rules.splice(ridx, 1);
          this.renderEditor();
        });
      });
    });
  }

  private attachWidgetInspectorListeners(overlay: HTMLElement): void {
    overlay.querySelector<HTMLSelectElement>('#sdw-widget-type')?.addEventListener('change', e => {
      const w = this.selectedWidget();
      if (!w) return;
      w.type = (e.target as HTMLSelectElement).value;
      w.config = {};
      this.renderEditor();
    });
    overlay.querySelector('#sdw-config-child')?.addEventListener('click', () => void this.openChildWidgetConfig());
  }

  private async importSvgTemplate(e: Event): Promise<void> {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    try {
      const text = await file.text();
      const imported = parseSvgTemplate(text);
      this.config.templateSvg = imported.template;
      this.config.width = imported.width;
      this.config.height = imported.height;
      this.clearSelection(false);
      this.renderEditor();
    } catch (err) {
      console.error('Failed to import SVG template:', err);
      window.alert(`Import failed: ${(err as Error).message}`);
    }
  }

  private collectBindingCard(card: HTMLElement, idx: number): void {
    const el = this.selectedElement();
    const binding = el?.bindings[idx];
    if (!binding) return;
    binding.tagPath = card.querySelector<HTMLInputElement>('.sdw-bind-path')?.value.trim() || '';
    binding.target = (card.querySelector<HTMLSelectElement>('.sdw-bind-target')?.value || 'fill') as DiagramBindTarget;
    binding.defaultValue = card.querySelector<HTMLInputElement>('.sdw-bind-default')?.value || '';
    binding.rules = [...card.querySelectorAll<HTMLElement>('.sdw-rule')].map(row => ({
      cond: (row.querySelector<HTMLSelectElement>('.sdw-rule-cond')?.value || 'eq') as DiagramRuleCond,
      value: row.querySelector<HTMLInputElement>('.sdw-rule-value')?.value || '',
      result: row.querySelector<HTMLInputElement>('.sdw-rule-result')?.value || '',
    }));
    this.subscribeEditorBindings();
  }

  private collectGlobalEditorConfig(): void {
    const overlay = this.overlayEl;
    if (!overlay) return;
    this.config.title = overlay.querySelector<HTMLInputElement>('#sdw-title')?.value.trim() ?? 'SVG Diagram';
    this.config.width = positiveNumber(Number(overlay.querySelector<HTMLInputElement>('#sdw-width')?.value), this.config.width);
    this.config.height = positiveNumber(Number(overlay.querySelector<HTMLInputElement>('#sdw-height')?.value), this.config.height);
    this.config.background = overlay.querySelector<HTMLInputElement>('#sdw-bg')?.value || this.config.background;
  }

  private handleStagePointerDown = (e: PointerEvent): void => {
    const stage = this.overlayEl?.querySelector<HTMLElement>('#sdw-stage');
    if (!stage) return;
    const target = e.target as HTMLElement;
    const handle = target.closest<HTMLElement>('[data-handle]');
    if (handle) {
      this.startHandleDrag(e, handle);
      return;
    }
    const widgetEl = target.closest<HTMLElement>('.sdw-editor-overlay-widget');
    const svgEl = target.closest<SVGElement>('[data-kind="element"]');
    if (this.editorTool !== 'select') {
      const pt = this.eventToCanvas(e);
      if (!pt) return;
      this.addWithTool(pt.x, pt.y);
      return;
    }
    const toggle = e.ctrlKey || e.metaKey;
    if (widgetEl?.dataset.id) {
      if (toggle) {
        e.preventDefault();
        this.toggleSelection('widget', widgetEl.dataset.id);
        return;
      }
      if (!this.isSelected('widget', widgetEl.dataset.id)) this.select('widget', widgetEl.dataset.id, false);
      this.startDrag(e, 'widget', widgetEl.dataset.id);
      this.softRefreshEditorSurface();
      return;
    }
    if (svgEl?.dataset.id) {
      if (toggle) {
        e.preventDefault();
        this.toggleSelection('element', svgEl.dataset.id);
        return;
      }
      if (!this.isSelected('element', svgEl.dataset.id)) this.select('element', svgEl.dataset.id, false);
      this.startDrag(e, 'element', svgEl.dataset.id);
      this.softRefreshEditorSurface();
      return;
    }
    const pt = this.eventToCanvas(e);
    if (!pt) {
      this.select(null, '');
      return;
    }
    this.startMarqueeSelect(e, pt);
  };

  private handleEditorPointerMove = (e: PointerEvent): void => {
    if (this.marqueeState) {
      const pt = this.eventToCanvas(e);
      if (!pt) return;
      this.marqueeState.currentX = pt.x;
      this.marqueeState.currentY = pt.y;
      this.drawMarqueeSelection();
      return;
    }
    if (!this.dragState) return;
    const pt = this.eventToCanvas(e);
    if (!pt) return;
    const dx = pt.x - this.dragState.startX;
    const dy = pt.y - this.dragState.startY;
    if (this.dragState.kind === 'widget') {
      if (this.dragState.mode === 'move' && this.dragState.ids.length > 1) {
        for (const id of this.dragState.ids) {
          const w = this.config.widgets.find(item => item.id === id);
          const original = this.dragState.originals.get(id);
          if (!w || !original) continue;
          w.x = snapInt(original.x + dx);
          w.y = snapInt(original.y + dy);
        }
        this.softRefreshEditorSurface();
        return;
      }
      const w = this.config.widgets.find(item => item.id === this.dragState?.id);
      if (!w) return;
      if (this.dragState.mode === 'resize') {
        w.w = snapInt(Math.max(40, this.dragState.original.w + dx));
        w.h = snapInt(Math.max(30, this.dragState.original.h + dy));
      } else {
        w.x = snapInt(this.dragState.original.x + dx);
        w.y = snapInt(this.dragState.original.y + dy);
      }
    } else {
      if (this.dragState.mode === 'move' && this.dragState.ids.length > 1) {
        for (const id of this.dragState.ids) {
          const el = this.config.elements.find(item => item.id === id);
          const original = this.dragState.originals.get(id);
          if (!el || !original) continue;
          moveElementFromOriginal(el, original, dx, dy);
        }
        this.softRefreshEditorSurface();
        return;
      }
      const el = this.config.elements.find(item => item.id === this.dragState?.id);
      if (!el) return;
      if (this.dragState.mode === 'resize') {
        el.w = snapInt(Math.max(8, this.dragState.original.w + dx));
        el.h = snapInt(Math.max(8, this.dragState.original.h + dy));
      } else if (this.dragState.mode === 'line-start') {
        el.x = snapInt(this.dragState.original.x + dx);
        el.y = snapInt(this.dragState.original.y + dy);
      } else if (this.dragState.mode === 'line-end') {
        el.x2 = snapInt(this.dragState.original.x2 + dx);
        el.y2 = snapInt(this.dragState.original.y2 + dy);
      } else if (this.dragState.mode === 'shape-point') {
        const idx = this.dragState.pointIndex ?? -1;
        const originalPoints = shapePoints(this.dragState.original);
        const points = shapePoints(el);
        if (idx >= 0 && idx < points.length && originalPoints[idx]) {
          points[idx] = { x: snapInt(originalPoints[idx].x + dx), y: snapInt(originalPoints[idx].y + dy) };
          this.setShapePoints(el, points);
        }
      } else {
        moveElementFromOriginal(el, this.dragState.original, dx, dy);
      }
    }
    this.softRefreshEditorSurface();
  };

  private handleEditorPointerUp = (e?: PointerEvent): void => {
    if (this.marqueeState) {
      if (e) {
        const pt = this.eventToCanvas(e);
        if (pt) {
          this.marqueeState.currentX = pt.x;
          this.marqueeState.currentY = pt.y;
        }
      }
      this.finishMarqueeSelect();
      return;
    }
    if (!this.dragState) return;
    this.dragState = null;
    this.renderEditor();
  };

  private startMarqueeSelect(e: PointerEvent, pt: { x: number; y: number }): void {
    const additive = e.ctrlKey || e.metaKey;
    if (!additive) {
      this.clearSelection(false);
      const layer = this.overlayEl?.querySelector<HTMLElement>('#sdw-selection-layer');
      if (layer) layer.innerHTML = '';
    }
    this.marqueeState = { startX: pt.x, startY: pt.y, currentX: pt.x, currentY: pt.y, additive };
    this.drawMarqueeSelection();
    this.captureEditorPointer(e);
  }

  private drawMarqueeSelection(): void {
    const layer = this.overlayEl?.querySelector<HTMLElement>('#sdw-selection-layer');
    if (!layer || !this.marqueeState) return;
    layer.querySelector('[data-marquee-selection]')?.remove();
    const rect = normalizedCanvasRect(
      this.marqueeState.startX,
      this.marqueeState.startY,
      this.marqueeState.currentX,
      this.marqueeState.currentY,
    );
    const el = document.createElement('div');
    el.setAttribute('data-marquee-selection', 'true');
    el.style.cssText = [
      'position:absolute',
      `left:${(rect.x / this.config.width) * 100}%`,
      `top:${(rect.y / this.config.height) * 100}%`,
      `width:${(rect.w / this.config.width) * 100}%`,
      `height:${(rect.h / this.config.height) * 100}%`,
      'box-sizing:border-box',
      `border:1px dashed ${SELECTION_STROKE}`,
      'background:rgba(2,132,199,0.12)',
      'pointer-events:none',
    ].join(';');
    layer.appendChild(el);
  }

  private finishMarqueeSelect(): void {
    const state = this.marqueeState;
    this.marqueeState = null;
    this.overlayEl?.querySelector('[data-marquee-selection]')?.remove();
    if (!state) return;

    const rect = normalizedCanvasRect(state.startX, state.startY, state.currentX, state.currentY);
    if (rect.w < 3 && rect.h < 3) {
      if (!state.additive) this.clearSelection();
      else this.syncSelectionHandles();
      return;
    }

    const ids = this.config.elements
      .filter(el => boundsContain(rect, elementBounds(el)))
      .map(el => el.id);
    const existingIds = state.additive && this.selectedKind === 'element' ? this.activeSelectedIds() : new Set<string>();
    const nextIds = state.additive
      ? [...existingIds, ...ids.filter(id => !existingIds.has(id))]
      : ids;
    this.selectMany('element', nextIds);
  }

  private startDrag(e: PointerEvent, kind: SelectionKind, id: string): void {
    const pt = this.eventToCanvas(e);
    if (!pt) return;
    const ids = this.selectedKind === kind && this.selectedIds.has(id) ? [...this.selectedIds] : [id];
    const original = kind === 'element' ? clone(this.config.elements.find(item => item.id === id)) : clone(this.config.widgets.find(item => item.id === id));
    const originals = new Map<string, any>();
    const source = kind === 'element' ? this.config.elements : this.config.widgets;
    for (const selectedId of ids) {
      const item = source.find(item => item.id === selectedId);
      if (item) originals.set(selectedId, clone(item));
    }
    this.dragState = { mode: 'move', kind, id, ids, startX: pt.x, startY: pt.y, original, originals };
    this.captureEditorPointer(e);
  }

  private startHandleDrag(e: PointerEvent, handle: HTMLElement): void {
    const pt = this.eventToCanvas(e);
    if (!pt || !this.selectedKind) return;
    const mode = handle.dataset.handle as any;
    const id = this.selectedId;
    const original = this.selectedKind === 'element' ? clone(this.selectedElement()) : clone(this.selectedWidget());
    this.dragState = {
      mode,
      kind: this.selectedKind,
      id,
      ids: [id],
      pointIndex: number(handle.dataset.pointIndex, -1),
      startX: pt.x,
      startY: pt.y,
      original,
      originals: new Map([[id, original]]),
    };
    this.captureEditorPointer(e);
    e.stopPropagation();
  }

  private addWithTool(x: number, y: number): void {
    if (this.editorTool === 'widget') {
      const type = getEmbeddableWidgetTypes()[0] || 'big-number-widget';
      const w: DiagramOverlayWidget = { id: makeId(), type, x: snapInt(x), y: snapInt(y), w: 260, h: 140, config: {} };
      this.config.widgets.push(w);
      this.editorTool = 'select';
      this.select('widget', w.id);
      return;
    }
    const type = this.editorTool as DiagramElementType;
    const shape = type === 'shape';
    const base: DiagramElement = {
      id: makeId(),
      type,
      x: snapInt(x),
      y: snapInt(y),
      w: type === 'line' ? 180 : 120,
      h: type === 'line' ? 0 : 80,
      x2: type === 'line' ? snapInt(x + 180) : undefined,
      y2: type === 'line' ? snapInt(y) : undefined,
      points: shape ? [
        { x: snapInt(x), y: snapInt(y) },
        { x: snapInt(x + 120), y: snapInt(y) },
        { x: snapInt(x + 120), y: snapInt(y + 80) },
        { x: snapInt(x + 40), y: snapInt(y + 110) },
      ] : undefined,
      text: type === 'text' ? 'Text' : undefined,
      icon: type === 'icon' ? 'mdi:factory' : undefined,
      fill: type === 'line' || type === 'shape' ? 'transparent' : '#38bdf8',
      stroke: '#94a3b8',
      strokeWidth: type === 'line' || type === 'shape' ? 6 : 2,
      fontSize: 28,
      opacity: 1,
      bindings: [],
    };
    if (shape) updateShapeBounds(base);
    this.config.elements.push(base);
    this.editorTool = 'select';
    this.select('element', base.id);
  }

  private select(kind: SelectionKind | null, id: string, render = true): void {
    this.selectedKind = kind;
    this.selectedId = id;
    this.selectedIds = kind && id ? new Set([id]) : new Set();
    if (render) this.renderEditor();
  }

  private clearSelection(render = true): void {
    this.select(null, '', render);
  }

  private toggleSelection(kind: SelectionKind, id: string): void {
    if (this.selectedKind !== kind) {
      this.select(kind, id);
      return;
    }
    if (this.selectedIds.has(id)) this.selectedIds.delete(id);
    else this.selectedIds.add(id);
    this.selectedKind = this.selectedIds.size ? kind : null;
    this.selectedId = this.selectedIds.has(id) ? id : [...this.selectedIds][0] || '';
    this.renderEditor();
  }

  private selectMany(kind: SelectionKind, ids: string[], render = true): void {
    this.selectedIds = new Set(ids.filter(Boolean));
    this.selectedKind = this.selectedIds.size ? kind : null;
    this.selectedId = ids[ids.length - 1] || '';
    if (render) this.renderEditor();
  }

  private isSelected(kind: SelectionKind, id: string): boolean {
    return this.selectedKind === kind && (this.selectedIds.has(id) || (this.selectedIds.size === 0 && this.selectedId === id));
  }

  private activeSelectedIds(): Set<string> {
    return this.selectedIds.size ? new Set(this.selectedIds) : new Set(this.selectedId ? [this.selectedId] : []);
  }

  private captureEditorPointer(e: PointerEvent): void {
    const stage = this.overlayEl?.querySelector<HTMLElement>('#sdw-stage');
    try {
      stage?.setPointerCapture?.(e.pointerId);
    } catch {
      // Some pointer targets are SVG descendants or get redrawn immediately;
      // dragging still works through overlay-level pointer listeners.
    }
  }

  private selectedElement(): DiagramElement | undefined {
    return this.config.elements.find(el => el.id === this.selectedId);
  }

  private selectedWidget(): DiagramOverlayWidget | undefined {
    return this.config.widgets.find(w => w.id === this.selectedId);
  }

  private updateSelectedElementProp(prop: string, value: string): void {
    const el = this.selectedElement();
    if (!el) return;
    if (['strokeWidth', 'fontSize', 'opacity'].includes(prop)) (el as any)[prop] = Number(value);
    else (el as any)[prop] = value;
    this.renderEditor();
  }

  private updateSelectedGeometry(prop: string, value: number): void {
    const item = this.selectedKind === 'element' ? this.selectedElement() : this.selectedWidget();
    if (!item || !Number.isFinite(value)) return;
    if (this.selectedKind === 'element' && (item as DiagramElement).type === 'shape' && ['x', 'y', 'w', 'h'].includes(prop)) {
      this.updateShapeGeometry(item as DiagramElement, prop, snapInt(value));
      this.renderEditor();
      return;
    }
    (item as any)[prop] = isGeometryProp(prop) ? snapInt(value) : value;
    this.renderEditor();
  }

  private updateShapeGeometry(el: DiagramElement, prop: string, value: number): void {
    const old = shapeBounds(shapePoints(el));
    const points = shapePoints(el);
    if (prop === 'x' || prop === 'y') {
      const dx = prop === 'x' ? value - old.x : 0;
      const dy = prop === 'y' ? value - old.y : 0;
      this.setShapePoints(el, points.map(pt => ({ x: snapInt(pt.x + dx), y: snapInt(pt.y + dy) })));
      return;
    }
    const nextW = prop === 'w' ? Math.max(1, value) : old.w;
    const nextH = prop === 'h' ? Math.max(1, value) : old.h;
    const scaleX = old.w === 0 ? 1 : nextW / old.w;
    const scaleY = old.h === 0 ? 1 : nextH / old.h;
    this.setShapePoints(el, points.map(pt => ({
      x: snapInt(old.x + (pt.x - old.x) * scaleX),
      y: snapInt(old.y + (pt.y - old.y) * scaleY),
    })));
  }

  private updateShapePoint(index: number, coord: string, value: number): void {
    const el = this.selectedElement();
    if (!el || el.type !== 'shape' || !Number.isFinite(index) || !Number.isFinite(value)) return;
    const points = shapePoints(el);
    if (index < 0 || index >= points.length) return;
    if (coord === 'y') points[index].y = snapInt(value);
    else points[index].x = snapInt(value);
    this.setShapePoints(el, points);
    this.renderEditor();
  }

  private addShapePoint(): void {
    const el = this.selectedElement();
    if (!el || el.type !== 'shape') return;
    const points = shapePoints(el);
    const last = points[points.length - 1] || { x: el.x, y: el.y };
    points.push({ x: snapInt(last.x + 50), y: snapInt(last.y) });
    this.setShapePoints(el, points);
    this.renderEditor();
  }

  private deleteShapePoint(index: number): void {
    const el = this.selectedElement();
    if (!el || el.type !== 'shape' || !Number.isFinite(index)) return;
    const points = shapePoints(el);
    if (points.length <= 2 || index < 0 || index >= points.length) return;
    points.splice(index, 1);
    this.setShapePoints(el, points);
    this.renderEditor();
  }

  private setShapePoints(el: DiagramElement, points: DiagramPoint[]): void {
    el.points = points.map(pt => ({ x: snapInt(pt.x), y: snapInt(pt.y) }));
    updateShapeBounds(el);
  }

  private deleteSelected(): void {
    const ids = this.activeSelectedIds();
    if (this.selectedKind === 'element') this.config.elements = this.config.elements.filter(el => !ids.has(el.id));
    if (this.selectedKind === 'widget') this.config.widgets = this.config.widgets.filter(w => !ids.has(w.id));
    this.clearSelection(false);
    this.renderEditor();
  }

  private duplicateSelected(): void {
    if (this.selectedKind === 'element') {
      const copies = this.cloneSelectedElements(30, 30);
      if (!copies.length) return;
      this.config.elements.push(...copies);
      this.selectMany('element', copies.map(copy => copy.id), false);
    } else if (this.selectedKind === 'widget') {
      const copies = this.cloneSelectedWidgets(30, 30);
      if (!copies.length) return;
      this.config.widgets.push(...copies);
      this.selectMany('widget', copies.map(copy => copy.id), false);
    }
    this.renderEditor();
  }

  private moveSelectedElement(direction: 1 | -1): void {
    if (this.selectedKind !== 'element') return;
    this.moveSelectedElements(direction);
  }

  private moveSelectedElements(direction: 1 | -1): void {
    const ids = this.activeSelectedIds();
    const selectedBounds = this.selectionBounds();
    if (!selectedBounds) return;
    const selectedItems = this.config.elements.filter(el => ids.has(el.id));
    if (!selectedItems.length) return;
    const selectedIndexes = this.config.elements
      .map((el, idx) => ids.has(el.id) ? idx : -1)
      .filter(idx => idx >= 0);
    const minSelectedIndex = Math.min(...selectedIndexes);
    const maxSelectedIndex = Math.max(...selectedIndexes);

    if (direction > 0) {
      let insertAfterId = '';
      this.config.elements.forEach((el, idx) => {
        if (ids.has(el.id)) return;
        if (idx <= maxSelectedIndex) return;
        if (boundsOverlap(selectedBounds, elementBounds(el))) insertAfterId = el.id;
      });
      if (insertAfterId) {
        this.config.elements = this.insertSelectedElementsAfter(ids, insertAfterId);
        this.renderEditor();
        return;
      }
    } else {
      let insertBeforeId = '';
      this.config.elements.forEach((el, idx) => {
        if (ids.has(el.id)) return;
        if (idx >= minSelectedIndex) return;
        if (boundsOverlap(selectedBounds, elementBounds(el))) {
          if (!insertBeforeId) insertBeforeId = el.id;
        }
      });
      if (insertBeforeId) {
        this.config.elements = this.insertSelectedElementsBefore(ids, insertBeforeId);
        this.renderEditor();
        return;
      }
    }

    if (direction > 0) {
      for (let i = this.config.elements.length - 2; i >= 0; i--) {
        if (!ids.has(this.config.elements[i].id) || ids.has(this.config.elements[i + 1].id)) continue;
        [this.config.elements[i], this.config.elements[i + 1]] = [this.config.elements[i + 1], this.config.elements[i]];
      }
    } else {
      for (let i = 1; i < this.config.elements.length; i++) {
        if (!ids.has(this.config.elements[i].id) || ids.has(this.config.elements[i - 1].id)) continue;
        [this.config.elements[i], this.config.elements[i - 1]] = [this.config.elements[i - 1], this.config.elements[i]];
      }
    }
    this.renderEditor();
  }

  private insertSelectedElementsAfter(ids: Set<string>, afterId: string): DiagramElement[] {
    const selected = this.config.elements.filter(el => ids.has(el.id));
    const remaining = this.config.elements.filter(el => !ids.has(el.id));
    const idx = remaining.findIndex(el => el.id === afterId);
    if (idx < 0) return this.config.elements;
    remaining.splice(idx + 1, 0, ...selected);
    return remaining;
  }

  private insertSelectedElementsBefore(ids: Set<string>, beforeId: string): DiagramElement[] {
    const selected = this.config.elements.filter(el => ids.has(el.id));
    const remaining = this.config.elements.filter(el => !ids.has(el.id));
    const idx = remaining.findIndex(el => el.id === beforeId);
    if (idx < 0) return this.config.elements;
    remaining.splice(idx, 0, ...selected);
    return remaining;
  }

  private copySelected(): void {
    if (this.selectedKind === 'element') {
      const ids = this.activeSelectedIds();
      const items = this.config.elements.filter(el => ids.has(el.id)).map(el => clone(el));
      if (items.length) this.editorClipboard = { kind: 'element', items };
    } else if (this.selectedKind === 'widget') {
      const ids = this.activeSelectedIds();
      const items = this.config.widgets.filter(w => ids.has(w.id)).map(w => clone(w));
      if (items.length) this.editorClipboard = { kind: 'widget', items };
    }
  }

  private pasteClipboard(): void {
    if (!this.editorClipboard?.items.length) return;
    if (this.editorClipboard.kind === 'element') {
      const copies = (this.editorClipboard.items as DiagramElement[]).map(el => cloneElementWithOffset(el, 30, 30));
      this.config.elements.push(...copies);
      this.selectMany('element', copies.map(copy => copy.id), false);
    } else {
      const copies = (this.editorClipboard.items as DiagramOverlayWidget[]).map(w => ({ ...clone(w), id: makeId(), x: w.x + 30, y: w.y + 30 }));
      this.config.widgets.push(...copies);
      this.selectMany('widget', copies.map(copy => copy.id), false);
    }
    this.renderEditor();
  }

  private cloneSelectedElements(dx: number, dy: number): DiagramElement[] {
    const ids = this.activeSelectedIds();
    return this.config.elements
      .filter(el => ids.has(el.id))
      .map(el => cloneElementWithOffset(el, dx, dy));
  }

  private cloneSelectedWidgets(dx: number, dy: number): DiagramOverlayWidget[] {
    const ids = this.activeSelectedIds();
    return this.config.widgets
      .filter(w => ids.has(w.id))
      .map(w => ({ ...clone(w), id: makeId(), x: w.x + dx, y: w.y + dy }));
  }

  private async openChildWidgetConfig(): Promise<void> {
    const w = this.selectedWidget();
    if (!w) return;
    this.cleanupChildWidgetConfig();
    if (!customElements.get(w.type)) await ensureWidgetTypeLoaded(w.type);
    const ctor = customElements.get(w.type) as any;
    if (!ctor) {
      window.alert(`The selected widget type (${w.type}) is not available.`);
      return;
    }
    const temp = document.createElement(w.type) as any;
    if (typeof temp.setConfig === 'function') temp.setConfig(w.config || {});

    if (typeof temp.openConfig === 'function') {
      this.openCustomChildWidgetConfig(w, temp);
      return;
    }

    const schema = typeof temp.getPropertySchema === 'function'
      ? temp.getPropertySchema()
      : typeof ctor?.getPropertySchema === 'function'
        ? ctor.getPropertySchema()
        : null;
    if (!schema?.length) {
      window.alert(`The selected widget type (${w.type}) does not expose configurable properties.`);
      return;
    }
    const existingBodyChildren = new Set<HTMLElement>(Array.from(document.body.children) as HTMLElement[]);
    const dialog = document.createElement('widget-properties-dialog') as WidgetPropertiesDialog;
    document.body.appendChild(dialog);
    this.childConfigHost = dialog;
    dialog.addEventListener('properties-updated', ((e: CustomEvent) => {
      e.stopPropagation();
      w.config = { ...(w.config || {}), ...(e.detail?.config || {}) };
      this.cleanupChildWidgetConfig();
      this.renderEditor();
    }) as EventListener, { once: true });
    dialog.open(w.id, schema, w.config || {}, false, getWidgetMeta(w.type)?.name ?? 'Widget Properties');
    requestAnimationFrame(() => this.promoteChildConfigOverlays(existingBodyChildren));
  }

  private openCustomChildWidgetConfig(w: DiagramOverlayWidget, temp: any): void {
    const existingBodyChildren = new Set<HTMLElement>(Array.from(document.body.children) as HTMLElement[]);
    temp.style.display = 'none';
    document.body.appendChild(temp);
    this.childConfigHost = temp;

    temp.addEventListener('widget-config-save', ((e: CustomEvent) => {
      e.stopPropagation();
      const nextConfig = e.detail?.config;
      if (nextConfig && typeof nextConfig === 'object') {
        w.config = clone(nextConfig);
      } else if (typeof temp.getConfig === 'function') {
        w.config = clone(temp.getConfig());
      }
      this.cleanupChildWidgetConfig();
      this.renderEditor();
    }) as EventListener, { once: true });

    temp.addEventListener('widget-config-close', ((e: CustomEvent) => {
      e.stopPropagation();
      this.cleanupChildWidgetConfig();
    }) as EventListener, { once: true });

    temp.openConfig();
    requestAnimationFrame(() => this.promoteChildConfigOverlays(existingBodyChildren));
  }

  private promoteChildConfigOverlays(existingBodyChildren: Set<HTMLElement>): void {
    const created = (Array.from(document.body.children) as HTMLElement[])
      .filter(el => !existingBodyChildren.has(el) && el !== this.childConfigHost);
    for (const el of created) {
      const style = window.getComputedStyle(el);
      const looksLikeOverlay = /overlay|backdrop|dialog|modal|mgr/i.test(`${el.id} ${el.className}`);
      if (looksLikeOverlay || style.position === 'fixed' || style.position === 'absolute') {
        el.style.zIndex = String(Math.max(Number.parseInt(style.zIndex || '0', 10) || 0, 22000));
        this.childConfigOverlays.add(el);
      }
    }
  }

  private cleanupChildWidgetConfig(): void {
    for (const el of this.childConfigOverlays) el.remove();
    this.childConfigOverlays.clear();
    this.childConfigHost?.remove();
    this.childConfigHost = null;
  }

  private eventToCanvas(e: PointerEvent): { x: number; y: number } | null {
    const stage = this.overlayEl?.querySelector<HTMLElement>('#sdw-stage');
    if (!stage) return null;
    const rect = stage.getBoundingClientRect();
    return {
      x: snapInt(clamp(((e.clientX - rect.left) / rect.width) * this.config.width, 0, this.config.width)),
      y: snapInt(clamp(((e.clientY - rect.top) / rect.height) * this.config.height, 0, this.config.height)),
    };
  }

  private softRefreshEditorSurface(): void {
    const stage = this.overlayEl?.querySelector<HTMLElement>('#sdw-stage');
    const oldSvg = stage?.querySelector('.sdw-svg');
    if (oldSvg) oldSvg.outerHTML = this.renderSvg(true);
    this.mountEditorWidgets();
    this.syncSelectionHandles();
    const svg = stage?.querySelector<SVGSVGElement>('.sdw-svg');
    if (svg) this.applyBindingsToSvg(svg);
  }

  private syncSelectionHandles(): void {
    const layer = this.overlayEl?.querySelector<HTMLElement>('#sdw-selection-layer');
    if (!layer) return;
    layer.innerHTML = '';
    if (!this.selectedKind) return;
    if (this.selectedIds.size > 1) {
      const bounds = this.selectionBounds();
      if (!bounds) return;
      const outline = document.createElement('div');
      outline.style.cssText = [
        'position:absolute',
        `left:${(bounds.x / this.config.width) * 100}%`,
        `top:${(bounds.y / this.config.height) * 100}%`,
        `width:${(bounds.w / this.config.width) * 100}%`,
        `height:${(bounds.h / this.config.height) * 100}%`,
        'box-sizing:border-box',
        `border:2px dashed ${SELECTION_STROKE}`,
        'pointer-events:none',
      ].join(';');
      layer.appendChild(outline);
      return;
    }
    const item = this.selectedKind === 'element' ? this.selectedElement() : this.selectedWidget();
    if (!item) return;
    const addHandle = (x: number, y: number, mode: string) => {
      const h = document.createElement('div');
      h.dataset.handle = mode;
      h.style.cssText = `position:absolute;left:${(x / this.config.width) * 100}%;top:${(y / this.config.height) * 100}%;width:12px;height:12px;margin:-6px 0 0 -6px;background:#f8fafc;border:2px solid #0284c7;border-radius:999px;pointer-events:auto;cursor:nwse-resize;`;
      layer.appendChild(h);
    };
    if (this.selectedKind === 'element' && (item as DiagramElement).type === 'line') {
      const el = item as DiagramElement;
      addHandle(el.x, el.y, 'line-start');
      addHandle(el.x2 ?? el.x + el.w, el.y2 ?? el.y + el.h, 'line-end');
    } else if (this.selectedKind === 'element' && (item as DiagramElement).type === 'shape') {
      const el = item as DiagramElement;
      shapePoints(el).forEach((pt, idx) => {
        const h = document.createElement('div');
        h.dataset.handle = 'shape-point';
        h.dataset.pointIndex = String(idx);
        h.title = `Point ${idx + 1}`;
        h.style.cssText = `position:absolute;left:${(pt.x / this.config.width) * 100}%;top:${(pt.y / this.config.height) * 100}%;width:12px;height:12px;margin:-6px 0 0 -6px;background:#f8fafc;border:2px solid #0284c7;border-radius:3px;pointer-events:auto;cursor:move;`;
        layer.appendChild(h);
      });
    } else {
      const bounds = this.selectedKind === 'element'
        ? elementBounds(item as DiagramElement)
        : { x: item.x, y: item.y, w: item.w, h: item.h };
      addHandle(bounds.x + bounds.w, bounds.y + bounds.h, 'resize');
    }
  }

  private selectionBounds(): { x: number; y: number; w: number; h: number } | null {
    const items = this.selectedKind === 'element'
      ? this.config.elements.filter(el => this.selectedIds.has(el.id)).map(elementBounds)
      : this.config.widgets.filter(w => this.selectedIds.has(w.id)).map(w => ({ x: w.x, y: w.y, w: w.w, h: w.h }));
    if (!items.length) return null;
    const left = Math.min(...items.map(item => item.x));
    const top = Math.min(...items.map(item => item.y));
    const right = Math.max(...items.map(item => item.x + item.w));
    const bottom = Math.max(...items.map(item => item.y + item.h));
    return { x: left, y: top, w: Math.max(1, right - left), h: Math.max(1, bottom - top) };
  }

  private subscribeEditorBindings(): void {
    this.clearEditorSubscriptions();
    const svg = this.overlayEl?.querySelector<SVGSVGElement>('.sdw-svg');
    if (!svg) return;
    const store = getMirrorStore();
    const paths = new Set<string>();
    for (const el of this.config.elements) {
      for (const binding of el.bindings || []) {
        if (binding.tagPath) paths.add(store.toAbsolute(binding.tagPath));
      }
    }
    for (const path of paths) {
      this.editorStoreUnsubs.push(store.subscribeTagReference(path, () => {
        const current = this.overlayEl?.querySelector<SVGSVGElement>('.sdw-svg');
        if (current) this.applyBindingsToSvg(current);
      }));
    }
    this.applyBindingsToSvg(svg);
  }

  private ensureIconSetsLoaded(editor: boolean): void {
    const prefixes = new Set(['mdi']);
    for (const el of this.config.elements) {
      const icon = el.icon || '';
      if (icon.includes(':')) prefixes.add(icon.split(':')[0]);
    }
    for (const prefix of prefixes) {
      if (isIconSetLoaded(prefix) || this.requestedIconSets.has(prefix)) continue;
      this.requestedIconSets.add(prefix);
      loadIconSet(prefix).then(() => {
        this.requestedIconSets.delete(prefix);
        if (this.overlayEl) this.renderEditor();
        else if (!editor && this.isConnected) this.rerender();
      });
    }
  }
}

function normalizeElement(input: Partial<DiagramElement> & Record<string, any>): DiagramElement {
  const rawType = String(input.type || '');
  const type = (['line', 'shape', 'rect', 'circle', 'text', 'icon'] as string[]).includes(rawType) ? rawType as DiagramElementType : 'rect';
  const element: DiagramElement = {
    id: String(input.id || makeId()),
    type,
    x: intNumber(input.x, 40),
    y: intNumber(input.y, 40),
    w: positiveInt(input.w, type === 'line' ? 160 : 100),
    h: intNumber(input.h, type === 'line' ? 0 : 80),
    x2: input.x2 === undefined ? undefined : intNumber(input.x2, intNumber(input.x, 40) + 160),
    y2: input.y2 === undefined ? undefined : intNumber(input.y2, intNumber(input.y, 40)),
    points: type === 'shape' ? normalizePoints(input.points, intNumber(input.x, 40), intNumber(input.y, 40)) : undefined,
    text: String(input.text ?? 'Text'),
    icon: String(input.icon || 'mdi:factory'),
    fill: String(input.fill || (type === 'line' || type === 'shape' ? 'transparent' : '#38bdf8')),
    stroke: String(input.stroke || '#94a3b8'),
    strokeWidth: positiveNumber(input.strokeWidth, 2),
    fontSize: positiveNumber(input.fontSize, 28),
    opacity: clamp(number(input.opacity, 1), 0, 1),
    bindings: Array.isArray(input.bindings) ? input.bindings.map(normalizeBinding) : [],
  };
  if (element.type === 'shape') updateShapeBounds(element);
  return element;
}

function normalizeBinding(input: Partial<DiagramBinding> & Record<string, any>): DiagramBinding {
  const rawTarget = String(input.target || '');
  const target: DiagramBindTarget = (TARGET_OPTIONS as string[]).includes(rawTarget) ? rawTarget as DiagramBindTarget : 'fill';
  return {
    tagPath: String(input.tagPath || ''),
    target,
    defaultValue: String(input.defaultValue ?? ''),
    rules: Array.isArray(input.rules) ? input.rules.map(rule => ({
      cond: COND_OPTIONS.includes(rule.cond) ? rule.cond : 'eq',
      value: String(rule.value ?? ''),
      result: String(rule.result ?? ''),
    })) : [],
  };
}

function normalizeOverlayWidget(input: Partial<DiagramOverlayWidget> & Record<string, any>): DiagramOverlayWidget {
  return {
    id: String(input.id || makeId()),
    type: String(input.type || 'big-number-widget'),
    x: intNumber(input.x, 80),
    y: intNumber(input.y, 80),
    w: positiveInt(input.w, 240),
    h: positiveInt(input.h, 140),
    config: isPlainObject(input.config) ? input.config : {},
  };
}

function normalizeTemplateSvg(input: any): DiagramTemplateSvg | undefined {
  if (!isPlainObject(input)) return undefined;
  const viewBox = String(input.viewBox || '').trim();
  const content = String(input.content || '').trim();
  if (!viewBox || !content) return undefined;
  return { viewBox, content };
}

function getEmbeddableWidgetTypes(): string[] {
  const available = getAvailableWidgets().map(w => w.type).filter(type => type !== 'svg-diagram-widget');
  const preferred = BASIC_WIDGET_TYPES.filter(type => available.includes(type));
  return [...preferred, ...available.filter(type => !preferred.includes(type))];
}

function field(label: string, prop: string, value: any, type = 'number', step = '1'): string {
  return `<div><label class="sdw-label">${label}</label><input class="sdw-field sdw-num" data-prop="${prop}" type="${type}" step="${step}" value="${escAttr(String(value ?? ''))}"></div>`;
}

function pointField(label: string, coord: 'x' | 'y', value: any): string {
  return `<div><label class="sdw-label">${label}</label><input class="sdw-field sdw-point-num" data-coord="${coord}" type="number" step="1" value="${escAttr(String(value ?? ''))}"></div>`;
}

function fillCheckboxHtml(checked: boolean): string {
  return `
    <label class="sdw-fill-toggle" title="Enable fill" style="display:inline-flex;align-items:center;gap:5px;white-space:nowrap;font-size:0.75rem;line-height:1.25;">
      <input class="sdw-fill-enabled" type="checkbox" ${checked ? 'checked' : ''} style="width:14px;height:14px;margin:0;">
      <span>Fill</span>
    </label>
  `;
}

function bindingStyleInput(className: string, target: DiagramBindTarget, value: string, placeholder: string): string {
  if (target === 'fill' || target === 'stroke') {
    return `<input class="sdw-field ${className}" type="color" value="${styleColorValue(value)}" title="${escAttr(value || placeholder)}">`;
  }
  return `<input class="sdw-field ${className}" value="${escAttr(value)}" placeholder="${escAttr(placeholder)}">`;
}

function styleColorValue(value: string): string {
  if (/^#[0-9a-f]{6}$/i.test(value)) return value;
  if (/^#[0-9a-f]{3}$/i.test(value)) {
    return '#' + value.slice(1).split('').map(ch => ch + ch).join('');
  }
  return '#000000';
}

function normalizePoints(input: any, fallbackX: number, fallbackY: number): DiagramPoint[] {
  const raw = Array.isArray(input) ? input : [];
  const points = raw
    .filter((pt): pt is Record<string, any> => isPlainObject(pt))
    .map(pt => ({ x: intNumber(pt.x, fallbackX), y: intNumber(pt.y, fallbackY) }));
  if (points.length >= 2) return points;
  return [
    { x: fallbackX, y: fallbackY },
    { x: fallbackX + 120, y: fallbackY },
    { x: fallbackX + 120, y: fallbackY + 80 },
  ];
}

function shapePoints(el: Partial<DiagramElement>): DiagramPoint[] {
  return normalizePoints(el.points, intNumber(el.x, 40), intNumber(el.y, 40));
}

function shapeBounds(points: DiagramPoint[]): { x: number; y: number; w: number; h: number } {
  const xs = points.map(pt => pt.x);
  const ys = points.map(pt => pt.y);
  const x = Math.min(...xs);
  const y = Math.min(...ys);
  return { x, y, w: Math.max(1, Math.max(...xs) - x), h: Math.max(1, Math.max(...ys) - y) };
}

function updateShapeBounds(el: DiagramElement): void {
  const bounds = shapeBounds(shapePoints(el));
  el.x = bounds.x;
  el.y = bounds.y;
  el.w = bounds.w;
  el.h = bounds.h;
}

function elementBounds(el: DiagramElement): { x: number; y: number; w: number; h: number } {
  if (el.type === 'line') {
    const x2 = el.x2 ?? el.x + el.w;
    const y2 = el.y2 ?? el.y + el.h;
    const left = Math.min(el.x, x2);
    const top = Math.min(el.y, y2);
    return { x: left, y: top, w: Math.max(1, Math.abs(x2 - el.x)), h: Math.max(1, Math.abs(y2 - el.y)) };
  }
  if (el.type === 'shape') return shapeBounds(shapePoints(el));
  if (el.type === 'text') return textElementBounds(el);
  return { x: el.x, y: el.y, w: Math.max(1, el.w), h: Math.max(1, el.h) };
}

function textElementBounds(el: DiagramElement): { x: number; y: number; w: number; h: number } {
  const text = el.text || 'Text';
  const fontSize = positiveNumber(el.fontSize, 28);
  const strokePad = Math.max(2, positiveNumber(el.strokeWidth, 0));
  const width = Math.max(fontSize * 0.65, text.length * fontSize * 0.62);
  return {
    x: el.x - strokePad,
    y: el.y - strokePad,
    w: Math.ceil(width + strokePad * 2),
    h: Math.ceil(fontSize * 1.18 + strokePad * 2),
  };
}

function boundsOverlap(a: { x: number; y: number; w: number; h: number }, b: { x: number; y: number; w: number; h: number }): boolean {
  return a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;
}

function boundsContain(container: { x: number; y: number; w: number; h: number }, item: { x: number; y: number; w: number; h: number }): boolean {
  return item.x >= container.x
    && item.y >= container.y
    && item.x + item.w <= container.x + container.w
    && item.y + item.h <= container.y + container.h;
}

function normalizedCanvasRect(x1: number, y1: number, x2: number, y2: number): { x: number; y: number; w: number; h: number } {
  const x = Math.min(x1, x2);
  const y = Math.min(y1, y2);
  return {
    x,
    y,
    w: Math.abs(x2 - x1),
    h: Math.abs(y2 - y1),
  };
}

function moveElementFromOriginal(el: DiagramElement, original: DiagramElement, dx: number, dy: number): void {
  el.x = snapInt(original.x + dx);
  el.y = snapInt(original.y + dy);
  if (el.type === 'line') {
    el.x2 = snapInt((original.x2 ?? original.x + original.w) + dx);
    el.y2 = snapInt((original.y2 ?? original.y + original.h) + dy);
  } else if (el.type === 'shape') {
    el.points = shapePoints(original).map(pt => ({ x: snapInt(pt.x + dx), y: snapInt(pt.y + dy) }));
    updateShapeBounds(el);
  }
}

function cloneElementWithOffset(el: DiagramElement, dx: number, dy: number): DiagramElement {
  const copy = { ...clone(el), id: makeId(), x: el.x + dx, y: el.y + dy, x2: el.x2 === undefined ? undefined : el.x2 + dx, y2: el.y2 === undefined ? undefined : el.y2 + dy };
  if (copy.type === 'shape') {
    copy.points = shapePoints(el).map(pt => ({ x: pt.x + dx, y: pt.y + dy }));
    updateShapeBounds(copy);
  }
  return copy;
}

function makeId(): string {
  return Math.random().toString(36).slice(2, 10);
}

function clone<T>(value: T): T {
  if (typeof structuredClone === 'function') return structuredClone(value);
  return JSON.parse(JSON.stringify(value));
}

function number(value: any, fallback: number): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : fallback;
}

function cssNumber(value: string): number {
  const n = Number.parseFloat(value);
  return Number.isFinite(n) ? n : 0;
}

function positiveNumber(value: any, fallback: number): number {
  return Math.max(1, number(value, fallback));
}

function intNumber(value: any, fallback: number): number {
  return snapInt(number(value, fallback));
}

function positiveInt(value: any, fallback: number): number {
  return Math.max(1, intNumber(value, fallback));
}

function snapInt(value: number): number {
  return Math.round(value);
}

function isGeometryProp(prop: string): boolean {
  return ['x', 'y', 'w', 'h', 'x2', 'y2'].includes(prop);
}

function clamp(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) return min;
  return Math.max(min, Math.min(max, value));
}

function isPlainObject(value: any): value is Record<string, any> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function isEditableTarget(target: EventTarget | null): boolean {
  const el = target instanceof HTMLElement ? target : null;
  if (!el) return false;
  return !!el.closest('input,textarea,select,[contenteditable=""],[contenteditable="true"]');
}

function sanitizeSvgTree(root: Element): void {
  const blockedTags = new Set(['script', 'foreignobject', 'iframe', 'object', 'embed', 'audio', 'video']);
  const nodes = [root, ...Array.from(root.querySelectorAll('*'))];
  for (const node of nodes) {
    if (blockedTags.has(node.localName.toLowerCase())) {
      node.remove();
      continue;
    }
    if (node.localName.toLowerCase() === 'style' && /javascript:|expression\s*\(/i.test(node.textContent || '')) {
      node.remove();
      continue;
    }
    for (const attr of Array.from(node.attributes)) {
      const name = attr.name.toLowerCase();
      const value = attr.value.trim();
      if (name.startsWith('on')) {
        node.removeAttribute(attr.name);
        continue;
      }
      if ((name === 'href' || name === 'xlink:href') && !isSafeSvgHref(value)) {
        node.removeAttribute(attr.name);
        continue;
      }
      if (name === 'style' && /javascript:|expression\s*\(/i.test(value)) {
        node.removeAttribute(attr.name);
      }
    }
  }
}

function isSafeSvgHref(value: string): boolean {
  if (!value) return true;
  if (value.startsWith('#')) return true;
  if (/^data:image\/svg\+xml/i.test(value)) return false;
  if (/^data:image\//i.test(value)) return true;
  return !/^[a-z][a-z0-9+.-]*:/i.test(value);
}

function svgDimensions(svg: Element): { width: number; height: number } {
  const viewBox = svg.getAttribute('viewBox')?.trim();
  if (viewBox) {
    const parts = viewBox.split(/[\s,]+/).map(Number);
    if (parts.length === 4 && parts.every(Number.isFinite) && parts[2] > 0 && parts[3] > 0) {
      return { width: snapInt(parts[2]), height: snapInt(parts[3]) };
    }
  }
  const width = parseSvgLength(svg.getAttribute('width'), 1200);
  const height = parseSvgLength(svg.getAttribute('height'), 700);
  return { width, height };
}

function parseSvgLength(value: string | null, fallback: number): number {
  const n = Number(String(value || '').trim().match(/^-?\d+(?:\.\d+)?/)?.[0]);
  return Number.isFinite(n) && n > 0 ? snapInt(n) : fallback;
}

function esc(value: string): string {
  return String(value).replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[ch]!));
}

function escAttr(value: string): string {
  return esc(value);
}

function toHex(value: string): string {
  if (/^#[0-9a-f]{6}$/i.test(value)) return value;
  if (/^#[0-9a-f]{3}$/i.test(value)) {
    return '#' + value.slice(1).split('').map(ch => ch + ch).join('');
  }
  return '#0f172a';
}

function noFill(value: string): boolean {
  return ['none', 'transparent'].includes(String(value || '').trim().toLowerCase());
}

function cssEscape(value: string): string {
  return String(value).replace(/["\\]/g, '\\$&');
}

customElements.define('svg-diagram-widget', SvgDiagramWidget);
