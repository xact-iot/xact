import { BaseComponent } from '../../components/base-component';
import { getUiStore } from '../../store/ui-store';
import { registerWidgetType } from './widget-registry';
import type { PropertyField } from './widget-properties-dialog';
import { sanitizeHtml } from '../../utils/html-sanitize';

// ── Config ────────────────────────────────────────────────────────────────────

interface Config {
  text: string;
  fontSize: number;
  color: string;
  textAlign: 'left' | 'center' | 'right';
}

const DEFAULT_CONFIG: Config = {
  text: 'Text',
  fontSize: 14,
  color: '#e2e8f0',
  textAlign: 'left',
};

// ── Widget ────────────────────────────────────────────────────────────────────

export class TextWidget extends BaseComponent {
  private static readonly ALLOWED_INLINE_TAGS = new Set([
    'b', 'strong', 'i', 'em', 'u', 's', 'span', 'small', 'br', 'code', 'sub', 'sup',
  ]);

  private config: Config = { ...DEFAULT_CONFIG };
  private _uiUnsubs: Array<() => void> = [];

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = { ...this.config, ...c };
    this.rerender();
  }

  getPropertySchema(): PropertyField[] {
    return [
      {
        name: 'text',
        type: 'string',
        label: 'Text',
        description: 'Supports {deviceName}, {deviceType}, {orgName}',
        default: 'Text',
      },
      {
        name: 'fontSize',
        type: 'number',
        label: 'Font size (px)',
        default: 14,
      },
      {
        name: 'color',
        type: 'color',
        label: 'Color',
        default: '#e2e8f0',
      },
      {
        name: 'textAlign',
        type: 'select',
        label: 'Text align',
        default: 'left',
        context: {
          options: [
            { value: 'left',   label: 'Left' },
            { value: 'center', label: 'Center' },
            { value: 'right',  label: 'Right' },
          ],
        },
      },
    ];
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  protected render(): void {
    const { fontSize, color, textAlign } = this.config;
    const colorStyle = `color:${color || 'var(--content-text)'};`;

    this.innerHTML = `
      <div style="
        height:100%; display:flex; align-items:center;
        padding:0.25rem 0; box-sizing:border-box;
        font-size:${fontSize}px; ${colorStyle}
        width:100%; line-height:1.4;
      ">
        <div class="tw-text-content" style="
          width:100%; text-align:${textAlign};
          white-space:pre-wrap; word-break:break-word;
        ">${this.resolveHtml()}</div>
      </div>
    `;

    // This widget has no header; clear the registry title so edit/inspect mode
    // can show controls without inserting "Text" into the header.
    const card = this.closest('widget-card') as any;
    card?.setTitle?.('');
    card?.setHeaderVisible?.(false);
  }

  protected attachEventListeners(): void {
    const store = getUiStore();
    this._uiUnsubs = [
      store.subscribe('deviceName', () => this.refreshText()),
      store.subscribe('deviceType', () => this.refreshText()),
      store.subscribe('orgName',    () => this.refreshText()),
    ];
  }

  protected detachEventListeners(): void {
    this._uiUnsubs.forEach(u => u());
    this._uiUnsubs = [];
  }

  // ── Helpers ─────────────────────────────────────────────────────────────────

  private refreshText(): void {
    const el = this.querySelector<HTMLElement>('.tw-text-content');
    if (el) el.innerHTML = this.resolveHtml();
  }

  private resolveText(): string {
    const s = getUiStore();
    return this.config.text
      .replace(/\{deviceName\}/g, this.esc(s.get('deviceName') || ''))
      .replace(/\{deviceType\}/g, this.esc(s.get('deviceType') || ''))
      .replace(/\{orgName\}/g,    this.esc(s.get('orgName')    || ''));
  }

  private resolveHtml(): string {
    return sanitizeHtml(this.resolveText(), { allowedTags: TextWidget.ALLOWED_INLINE_TAGS });
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  private esc(s: string): string {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
}

// ── Registration ──────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'text-widget',
  name: 'Text',
  icon: '📝',
  category: 'General',
  defaultW: 6,
  defaultH: 1,
  minW: 2,
  minH: 1,
});

customElements.define('text-widget', TextWidget);
