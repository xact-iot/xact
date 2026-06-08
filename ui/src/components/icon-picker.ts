/**
 * <icon-picker> - self-contained icon picker custom element.
 *
 * Renders inline: a 24px SVG preview + "Choose…" button.
 * Opens a portal modal on document.body with:
 *   - Search input (debounced 200ms)
 *   - Set-selector tab buttons
 *   - Scrollable icon grid (50 per page, "Load more")
 *   - Lazy-loads each set on first tab click
 *
 * On icon select: sets .value, dispatches `change` CustomEvent, closes modal.
 * Backdrop click closes modal.
 *
 * Uses industrial amber aesthetic matching the rest of the XACT UI.
 */

import {
  ICON_SETS,
  loadIconSet,
  isIconSetLoaded,
  preloadIconSet,
  getIconSVG,
  searchIcons,
  type IconResult,
} from '../utils/icons';

const PAGE_SIZE = 50;

export class IconPicker extends HTMLElement {
  private _value = '';
  private _modal: HTMLDivElement | null = null;
  private _activePrefix = 'mdi';
  private _query = '';
  private _page = 1;
  private _debounceTimer: ReturnType<typeof setTimeout> | null = null;

  // ── Observed attributes ────────────────────────────────────────────────────

  static get observedAttributes() { return ['value']; }

  attributeChangedCallback(name: string, _old: string, value: string) {
    if (name === 'value') {
      this._value = value || '';
      this._renderInline();
    }
  }

  get value(): string { return this._value; }
  set value(v: string) {
    this._value = v || '';
    this._renderInline();
  }

  // ── Lifecycle ──────────────────────────────────────────────────────────────

  connectedCallback() {
    this.style.display = 'inline-flex';
    this.style.alignItems = 'center';
    this.style.gap = '6px';
    this._renderInline();
    preloadIconSet('mdi');
  }

  disconnectedCallback() {
    this._closeModal();
  }

  // ── Inline preview ─────────────────────────────────────────────────────────

  private _renderInline() {
    const svg = this._value ? getIconSVG(this._value, 'var(--accent-color)', 22) : '';
    const preview = svg
      ? svg
      : `<span style="display:inline-flex;align-items:center;justify-content:center;width:22px;height:22px;font-size:16px;opacity:0.4;">◻</span>`;

    this.innerHTML = `
      <span class="ip-preview" style="display:inline-flex;align-items:center;justify-content:center;width:28px;height:28px;border:1px solid var(--border-color);border-radius:4px;background:color-mix(in srgb,var(--border-color) 20%,transparent);">
        ${preview}
      </span>
      <button class="ip-choose-btn" type="button"
              style="font-size:14px;font-family:'IBM Plex Mono',monospace;padding:3px 6px;border:1px solid var(--border-color);border-radius:3px;background:color-mix(in srgb,var(--border-color) 25%,transparent);color:var(--content-text);cursor:pointer;white-space:nowrap;line-height:1;"
              title="Choose icon">✏️</button>
    `;
    this.querySelector('.ip-choose-btn')?.addEventListener('click', (e) => {
      e.stopPropagation();
      this._openModal();
    });
  }

  // ── Modal ──────────────────────────────────────────────────────────────────

  private _openModal() {
    if (this._modal) return;

    // Determine active prefix from current value
    if (this._value?.includes(':')) {
      this._activePrefix = this._value.split(':')[0];
    }

    this._modal = document.createElement('div');
    document.body.appendChild(this._modal);
    this._renderModal();

    // Lazy-load active set
    if (!isIconSetLoaded(this._activePrefix)) {
      loadIconSet(this._activePrefix).then(() => this._renderGrid());
    }
  }

  private _closeModal() {
    if (!this._modal) return;
    this._modal.remove();
    this._modal = null;
    if (this._debounceTimer) clearTimeout(this._debounceTimer);
  }

  private _renderModal() {
    if (!this._modal) return;

    const tabsHtml = ICON_SETS.map(s => `
      <button class="ip-tab" data-prefix="${s.prefix}"
              style="font-size:12px;padding:5px 10px;border:1px solid ${s.prefix === this._activePrefix ? 'var(--accent-color)' : 'var(--border-color)'};border-radius:3px;cursor:pointer;background:${s.prefix === this._activePrefix ? 'color-mix(in srgb,var(--accent-color) 15%,transparent)' : 'color-mix(in srgb,var(--border-color) 20%,transparent)'};color:${s.prefix === this._activePrefix ? 'var(--accent-color)' : 'var(--content-text)'};white-space:nowrap;">
        ${s.label}
      </button>
    `).join('');

    this._modal.innerHTML = `
      <div class="ip-backdrop" style="position:fixed;inset:0;background:rgba(0,0,0,0.65);z-index:30000;display:flex;align-items:center;justify-content:center;padding:1rem;">
        <div class="ip-dialog" style="display:flex;flex-direction:column;width:min(680px,95vw);max-height:80vh;border:1px solid var(--accent-color);border-radius:6px;background:var(--content-bg);color:var(--content-text);font-size:13px;box-shadow:0 24px 60px rgba(0,0,0,0.6);overflow:hidden;">

          <!-- Header -->
          <div style="display:flex;align-items:center;justify-content:space-between;padding:12px 16px;border-bottom:1px solid color-mix(in srgb,var(--accent-color) 25%,var(--border-color));">
            <span style="font-size:13px;font-weight:600;color:var(--accent-color);">Select Icon</span>
            <button class="ip-close" type="button" style="background:none;border:none;color:var(--content-text);font-size:20px;cursor:pointer;line-height:1;opacity:0.6;">&times;</button>
          </div>

          <!-- Search -->
          <div style="padding:10px 16px;border-bottom:1px solid var(--border-color);">
            <input class="ip-search" type="text" placeholder="Search icons…" value="${this._query}"
                   style="width:100%;box-sizing:border-box;padding:6px 10px;font-size:13px;border:1px solid var(--border-color);border-radius:4px;background:color-mix(in srgb,var(--border-color) 15%,transparent);color:var(--content-text);outline:none;">
          </div>

          <!-- Tabs -->
          <div style="display:flex;gap:6px;padding:10px 16px;border-bottom:1px solid var(--border-color);flex-wrap:wrap;">
            ${tabsHtml}
          </div>

          <!-- Grid (scrollable) -->
          <div class="ip-grid-wrap" style="flex:1;overflow-y:auto;padding:12px 16px;">
            <div class="ip-grid" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(48px,1fr));gap:6px;">
              <div class="ip-loading" style="grid-column:1/-1;text-align:center;padding:24px;font-size:13px;opacity:0.5;">Loading…</div>
            </div>
            <div class="ip-load-more-wrap" style="text-align:center;padding:10px 0;display:none;">
              <button class="ip-load-more" type="button"
                      style="font-size:12px;padding:5px 16px;border:1px solid var(--border-color);border-radius:3px;background:color-mix(in srgb,var(--border-color) 20%,transparent);color:var(--accent-color);cursor:pointer;">
                Load more
              </button>
            </div>
          </div>

        </div>
      </div>
    `;

    // Attach listeners
    this._modal.querySelector('.ip-backdrop')?.addEventListener('click', (e) => {
      if ((e.target as Element).classList.contains('ip-backdrop')) this._closeModal();
    });
    this._modal.querySelector('.ip-close')?.addEventListener('click', () => this._closeModal());
    this._modal.querySelectorAll('.ip-tab').forEach(tab => {
      tab.addEventListener('click', () => {
        // Preserve whatever the user has typed in the search box
        const currentQuery = this._modal?.querySelector<HTMLInputElement>('.ip-search')?.value ?? this._query;
        this._query = currentQuery;
        const prefix = (tab as HTMLElement).dataset.prefix!;
        this._activePrefix = prefix;
        this._page = 1;
        this._renderModal(); // re-render tabs (search value is restored from this._query)
        if (!isIconSetLoaded(prefix)) {
          loadIconSet(prefix).then(() => this._renderGrid());
        } else {
          this._renderGrid();
        }
      });
    });
    const searchInput = this._modal.querySelector<HTMLInputElement>('.ip-search')!;
    searchInput?.addEventListener('input', () => {
      if (this._debounceTimer) clearTimeout(this._debounceTimer);
      this._debounceTimer = setTimeout(() => {
        this._query = searchInput.value;
        this._page = 1;
        this._renderGrid();
      }, 200);
    });
    this._modal.querySelector('.ip-load-more')?.addEventListener('click', () => {
      this._page++;
      this._renderGrid(true);
    });

    // Render grid if already loaded
    if (isIconSetLoaded(this._activePrefix)) {
      this._renderGrid();
    }

    // Focus search input
    setTimeout(() => searchInput?.focus(), 50);
  }

  private _renderGrid(append = false) {
    if (!this._modal) return;
    const grid = this._modal.querySelector<HTMLElement>('.ip-grid')!;
    const loadMoreWrap = this._modal.querySelector<HTMLElement>('.ip-load-more-wrap')!;
    if (!grid) return;

    if (!isIconSetLoaded(this._activePrefix)) {
      grid.innerHTML = `<div style="grid-column:1/-1;text-align:center;padding:24px;font-family:'IBM Plex Mono',monospace;font-size:12px;opacity:0.5;">Loading…</div>`;
      loadMoreWrap.style.display = 'none';
      return;
    }

    const limit = this._page * PAGE_SIZE;
    const results = searchIcons(this._query, this._activePrefix, limit + 1);
    const hasMore = results.length > limit;
    const shown = results.slice(0, limit);

    if (!append) {
      grid.innerHTML = shown.length === 0
        ? `<div style="grid-column:1/-1;text-align:center;padding:24px;font-family:'IBM Plex Mono',monospace;font-size:12px;opacity:0.5;">No icons found</div>`
        : '';
    }

    if (shown.length > 0) {
      const start = append ? (this._page - 1) * PAGE_SIZE : 0;
      const slice = shown.slice(start);
      const fragment = document.createDocumentFragment();
      for (const icon of slice) {
        fragment.appendChild(this._makeIconButton(icon));
      }
      grid.appendChild(fragment);
    }

    loadMoreWrap.style.display = hasMore ? 'block' : 'none';
  }

  private _makeIconButton(icon: IconResult): HTMLElement {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.title = icon.iconName;
    btn.dataset.name = icon.name;

    const isSelected = icon.name === this._value;
    btn.style.cssText = `
      display:flex;align-items:center;justify-content:center;
      width:48px;height:48px;border-radius:4px;cursor:pointer;
      border:1px solid ${isSelected ? 'var(--accent-color)' : 'transparent'};
      background:${isSelected ? 'color-mix(in srgb,var(--accent-color) 15%,transparent)' : 'color-mix(in srgb,var(--border-color) 15%,transparent)'};
      transition:background 0.1s,border-color 0.1s;
      padding:0;
    `;

    const svg = getIconSVG(icon.name, 'var(--content-text)', 30);
    btn.innerHTML = svg || `<span style="font-size:10px;font-family:'IBM Plex Mono',monospace;opacity:0.4;overflow:hidden;max-width:44px;text-overflow:ellipsis;">${icon.iconName.slice(0, 4)}</span>`;

    btn.addEventListener('mouseenter', () => {
      if (icon.name !== this._value) {
        btn.style.background = 'color-mix(in srgb,var(--accent-color) 10%,transparent)';
        btn.style.borderColor = 'color-mix(in srgb,var(--accent-color) 40%,transparent)';
      }
    });
    btn.addEventListener('mouseleave', () => {
      if (icon.name !== this._value) {
        btn.style.background = 'color-mix(in srgb,var(--border-color) 15%,transparent)';
        btn.style.borderColor = 'transparent';
      }
    });
    btn.addEventListener('click', () => {
      this._value = icon.name;
      this._renderInline();
      this.dispatchEvent(new CustomEvent('change', { detail: icon.name, bubbles: true }));
      this._closeModal();
    });

    return btn;
  }
}

customElements.define('icon-picker', IconPicker);
