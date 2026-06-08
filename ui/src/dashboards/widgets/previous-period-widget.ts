import { BaseComponent } from '../../components/base-component';
import { getUiStore } from '../../store/ui-store';
import { registerWidgetType } from './widget-registry';
import type { PropertyField } from './widget-properties-dialog';

// ── Config ────────────────────────────────────────────────────────────────────

interface Config {
  headerLabel: string;
}

const DEFAULT_CONFIG: Config = {
  headerLabel: 'Time Period',
};

// ── Periods ───────────────────────────────────────────────────────────────────

const PERIODS = [
  { label: '1h',  ms: 1 * 3_600_000 },
  { label: '3h',  ms: 3 * 3_600_000 },
  { label: '6h',  ms: 6 * 3_600_000 },
  { label: '12h', ms: 12 * 3_600_000 },
  { label: '24h', ms: 24 * 3_600_000 },
  { label: '48h', ms: 48 * 3_600_000 },
  { label: '7d',  ms: 7 * 86_400_000 },
];

// ── Widget ────────────────────────────────────────────────────────────────────

export class PreviousPeriodWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG };
  private _activeMs: number | null = null;

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = { ...this.config, ...c };
    this.rerender();
  }

  getPropertySchema(): PropertyField[] {
    return [
      {
        name: 'headerLabel',
        type: 'string',
        label: 'Header label',
        default: 'Time Period',
      },
    ];
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  protected render(): void {
    const card = this.closest('widget-card') as any;
    card?.setTitle?.(this.config.headerLabel);
    card?.setHeaderVisible?.(true);

    const buttons = PERIODS.map(p => {
      const active = this._activeMs === p.ms;
      return `<button
        data-ms="${p.ms}"
        style="
          padding: 0.2rem 0.55rem;
          font-size: 0.75rem;
          font-weight: 600;
          border-radius: 0.25rem;
          cursor: pointer;
          border: 1px solid ${active ? 'var(--accent-color)' : 'var(--widget-border)'};
          background: ${active ? 'var(--accent-color)' : 'transparent'};
          color: ${active ? 'var(--widget-header-bg)' : 'var(--content-text)'};
          transition: background 0.15s, color 0.15s, border-color 0.15s;
          white-space: nowrap;
        "
      >${p.label}</button>`;
    }).join('');

    this.innerHTML = `
      <div style="
        height: 100%; display: flex; align-items: center;
        gap: 0.35rem; padding: 0.25rem 0; box-sizing: border-box;
        flex-wrap: wrap;
      ">${buttons}</div>
    `;
  }

  protected attachEventListeners(): void {
    this.querySelectorAll<HTMLButtonElement>('button[data-ms]').forEach(btn => {
      btn.addEventListener('click', this._onButtonClick);
      btn.addEventListener('mouseover', this._onHover);
      btn.addEventListener('mouseout', this._onHoverOut);
    });
  }

  protected detachEventListeners(): void {
    this.querySelectorAll<HTMLButtonElement>('button[data-ms]').forEach(btn => {
      btn.removeEventListener('click', this._onButtonClick);
      btn.removeEventListener('mouseover', this._onHover);
      btn.removeEventListener('mouseout', this._onHoverOut);
    });
  }

  // ── Handlers ────────────────────────────────────────────────────────────────

  private _onButtonClick = (e: Event): void => {
    const btn = e.currentTarget as HTMLButtonElement;
    const ms = parseInt(btn.dataset.ms ?? '0', 10);
    if (!ms) return;

    this._activeMs = ms;
    const now = Date.now();
    const store = getUiStore();
    store.set('timeStart', now - ms);
    store.set('timeEnd', now);

    // Update active styling without full rerender
    this.querySelectorAll<HTMLButtonElement>('button[data-ms]').forEach(b => {
      const active = parseInt(b.dataset.ms ?? '0', 10) === ms;
      b.style.background = active ? 'var(--accent-color)' : 'transparent';
      b.style.color      = active ? 'var(--widget-header-bg)' : 'var(--content-text)';
      b.style.borderColor = active ? 'var(--accent-color)' : 'var(--widget-border)';
    });
  };

  private _onHover = (e: Event): void => {
    const btn = e.currentTarget as HTMLButtonElement;
    const ms = parseInt(btn.dataset.ms ?? '0', 10);
    if (ms !== this._activeMs) {
      btn.style.borderColor = 'var(--accent-color)';
      btn.style.color = 'var(--accent-color)';
    }
  };

  private _onHoverOut = (e: Event): void => {
    const btn = e.currentTarget as HTMLButtonElement;
    const ms = parseInt(btn.dataset.ms ?? '0', 10);
    const active = ms === this._activeMs;
    btn.style.background   = active ? 'var(--accent-color)' : 'transparent';
    btn.style.color        = active ? 'var(--widget-header-bg)' : 'var(--content-text)';
    btn.style.borderColor  = active ? 'var(--accent-color)' : 'var(--widget-border)';
  };

  // ── Helpers ─────────────────────────────────────────────────────────────────

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }
}

// ── Registration ──────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'previous-period-widget',
  name: 'Previous Period',
  icon: '⏱',
  category: 'General',
  defaultW: 6,
  defaultH: 1,
  minW: 3,
  minH: 1,
});

customElements.define('previous-period-widget', PreviousPeriodWidget);
