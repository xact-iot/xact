import { BaseComponent } from '../../components/base-component';
import { getUiStore } from '../../store/ui-store';
import { registerWidgetType } from './widget-registry';
import type { PropertyField } from './widget-properties-dialog';

// ── Config ────────────────────────────────────────────────────────────────────

interface Config {
  headerText: string;
}

const DEFAULT_CONFIG: Config = {
  headerText: 'Time Range',
};

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Convert a Unix-ms timestamp to the value string expected by datetime-local inputs. */
function msToInputValue(ms: number | null): string {
  if (ms === null) return '';
  // datetime-local requires "YYYY-MM-DDTHH:mm" in local time
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Parse a datetime-local input value to a Unix-ms timestamp, or null if empty/invalid. */
function inputValueToMs(v: string): number | null {
  if (!v) return null;
  const ms = new Date(v).getTime();
  return isNaN(ms) ? null : ms;
}

// ── Widget ────────────────────────────────────────────────────────────────────

export class TimeRangeWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG };
  private _uiUnsubs: Array<() => void> = [];
  /** Guard to prevent store → DOM → store loops */
  private _updating = false;

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = { ...this.config, ...c };
    this.rerender();
  }

  getPropertySchema(): PropertyField[] {
    return [
      {
        name: 'headerText',
        type: 'string',
        label: 'Header text',
        default: 'Time Range',
      },
    ];
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  protected render(): void {
    const card = this.closest('widget-card') as any;
    card?.setTitle?.(this.config.headerText);
    card?.setHeaderVisible?.(true);

    const store = getUiStore();
    const startVal = msToInputValue(store.get('timeStart'));
    const endVal   = msToInputValue(store.get('timeEnd'));

    const inputStyle = `
      background: var(--widget-bg);
      border: 1px solid var(--widget-border);
      color: var(--content-text);
      border-radius: 0.25rem;
      padding: 0.15rem 0.4rem;
      font-size: 0.75rem;
      font-family: inherit;
      cursor: pointer;
      min-width: 0;
    `;

    const labelStyle = `
      font-size: 0.7rem;
      color: var(--content-text);
      opacity: 0.6;
      white-space: nowrap;
    `;

    this.innerHTML = `
      <style>
        :host input[type="datetime-local"] {
          color-scheme: dark;
        }
        :host input[type="datetime-local"]::-webkit-calendar-picker-indicator {
          opacity: 0;
          cursor: pointer;
        }
        .trw-input-wrap {
          position: relative;
          display: inline-flex;
          align-items: center;
          min-width: 0;
        }
        .trw-input-wrap input {
          padding-right: 1.9rem !important;
        }
        .trw-picker-btn {
          position: absolute;
          right: 0.35rem;
          width: 1rem;
          height: 1rem;
          padding: 0;
          border: 0;
          background: transparent;
          color: var(--accent-color);
          opacity: 0.95;
          display: inline-flex;
          align-items: center;
          justify-content: center;
          cursor: pointer;
        }
        .trw-picker-btn:hover,
        .trw-picker-btn:focus-visible {
          opacity: 1;
          outline: none;
        }
      </style>
      <div style="
        height: 100%; display: flex; align-items: center;
        gap: 0.5rem; padding: 0.25rem 0; box-sizing: border-box;
        flex-wrap: wrap;
      ">
        <span style="${labelStyle}">From</span>
        <span class="trw-input-wrap">
          <input id="trw-start" type="datetime-local" value="${startVal}" style="${inputStyle}">
          <button type="button" class="trw-picker-btn" data-target="trw-start" aria-label="Open start date picker" title="Open date picker">
            ${this.calendarIcon()}
          </button>
        </span>
        <span style="${labelStyle}">To</span>
        <span class="trw-input-wrap">
          <input id="trw-end" type="datetime-local" value="${endVal}" style="${inputStyle}">
          <button type="button" class="trw-picker-btn" data-target="trw-end" aria-label="Open end date picker" title="Open date picker">
            ${this.calendarIcon()}
          </button>
        </span>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    const start = this.querySelector<HTMLInputElement>('#trw-start');
    const end   = this.querySelector<HTMLInputElement>('#trw-end');

    start?.addEventListener('change', this._onStartChange);
    end?.addEventListener('change', this._onEndChange);
    this.querySelectorAll<HTMLButtonElement>('.trw-picker-btn').forEach(btn => {
      btn.addEventListener('click', this._onPickerClick);
    });

    // Keep inputs in sync when another widget (e.g. Previous Period) changes the store
    const store = getUiStore();
    this._uiUnsubs = [
      store.subscribe('timeStart', (v) => {
        if (this._updating) return;
        if (start) start.value = msToInputValue(v);
      }),
      store.subscribe('timeEnd', (v) => {
        if (this._updating) return;
        if (end) end.value = msToInputValue(v);
      }),
    ];
  }

  protected detachEventListeners(): void {
    const start = this.querySelector<HTMLInputElement>('#trw-start');
    const end   = this.querySelector<HTMLInputElement>('#trw-end');
    start?.removeEventListener('change', this._onStartChange);
    end?.removeEventListener('change', this._onEndChange);
    this.querySelectorAll<HTMLButtonElement>('.trw-picker-btn').forEach(btn => {
      btn.removeEventListener('click', this._onPickerClick);
    });
    this._uiUnsubs.forEach(u => u());
    this._uiUnsubs = [];
  }

  // ── Handlers ────────────────────────────────────────────────────────────────

  private _onStartChange = (e: Event): void => {
    const v = (e.target as HTMLInputElement).value;
    this._updating = true;
    getUiStore().set('timeStart', inputValueToMs(v));
    this._updating = false;
  };

  private _onEndChange = (e: Event): void => {
    const v = (e.target as HTMLInputElement).value;
    this._updating = true;
    getUiStore().set('timeEnd', inputValueToMs(v));
    this._updating = false;
  };

  private _onPickerClick = (e: Event): void => {
    const targetId = (e.currentTarget as HTMLElement).dataset.target;
    const input = targetId ? this.querySelector<HTMLInputElement>(`#${targetId}`) : null;
    if (!input) return;
    input.focus();
    const pickerInput = input as HTMLInputElement & { showPicker?: () => void };
    if (typeof pickerInput.showPicker === 'function') {
      try { pickerInput.showPicker(); } catch { /* focus fallback is enough */ }
    }
  };

  // ── Helpers ─────────────────────────────────────────────────────────────────

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  private calendarIcon(): string {
    return `
      <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" fill="none"
           stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M8 2v4"></path>
        <path d="M16 2v4"></path>
        <rect x="3" y="4" width="18" height="18" rx="2"></rect>
        <path d="M3 10h18"></path>
      </svg>`;
  }
}

// ── Registration ──────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'time-range-widget',
  name: 'Time Range',
  icon: '📅',
  category: 'General',
  defaultW: 6,
  defaultH: 1,
  minW: 4,
  minH: 1,
});

customElements.define('time-range-widget', TimeRangeWidget);
