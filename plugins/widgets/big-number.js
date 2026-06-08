/**
 * big-number - XACT Widget Plugin
 *
 * Subscribes to a single RTDB tag and displays its value as a large number.
 * The widget card header uses the Label property; no label is rendered inside.
 *
 * Properties (configured via Widget Properties dialog):
 *   tagPath  {path}    Dot-separated RTDB tag path  (e.g. building.floor1.temperature)
 *   label    {string}  Widget header title           (e.g. "Floor 1 Temperature")
 *   unit     {string}  Unit suffix                   (e.g. °C, %, kWh, rpm)
 *   decimals {number}  Decimal places to display     (default: 2)
 *
 * This file is discovered automatically from plugins/widgets/ on server startup.
 * It requires no build step.
 */
(function () {
  'use strict';

  // ─── Widget implementation ───────────────────────────────────────────────

  class BigNumberWidget extends HTMLElement {

    constructor() {
      super();
      this._config  = { tagPath: '', label: '', unit: '', decimals: 2 };
      this._mounted = false;
    }

    // ── Schema used by WidgetPropertiesDialog ──────────────────────────────
    // 'path' type renders a text input + Browse button that opens the tree browser.

    static getPropertySchema() {
      return [
        {
          name: 'tagPath',
          type: 'path',
          label: 'Tag Path',
          description: 'Dot-separated path to the RTDB tag  (e.g. building.floor1.temperature)',
          default: '',
        },
        {
          name: 'label',
          type: 'string',
          label: 'Label',
          description: 'Widget header title - replaces the default "Big Number" heading',
          default: '',
        },
        {
          name: 'unit',
          type: 'string',
          label: 'Unit',
          description: 'Unit suffix appended to the value  (e.g. °C, %, kWh)',
          default: '',
        },
        {
          name: 'decimals',
          type: 'number',
          label: 'Decimal places',
          description: 'Number of decimal places to display',
          default: 2,
          context: { min: 0, max: 10, step: 1 },
        },
      ];
    }

    // ── Lifecycle ──────────────────────────────────────────────────────────

    setConfig(config) {
      this._config = Object.assign({ tagPath: '', label: '', unit: '', decimals: 2 }, config);
      this._config.decimals = _normalizeDecimals(this._config.decimals);
      // setConfig is called before connectedCallback (element not yet in DOM),
      // so card title update is deferred to connectedCallback / rerender.
    }

    connectedCallback() {
      this._mounted = true;
      this._render();
      this._subscribe();
      this._updateCardTitle();
    }

    disconnectedCallback() {
      this._mounted = false;
      // NOTE: The MirrorStore has no unsubscribe API. The callback registered
      // in _subscribe() will remain in the store's subscriber list until the
      // app session ends. The _mounted guard makes it a cheap no-op, but the
      // widget element is kept alive by the closure - a known limitation.
    }

    /** Called by dashboard-container after setConfig() when properties change live. */
    rerender() {
      this._render();
      this._subscribe();
      this._updateCardTitle();
    }

    // ── Card title ─────────────────────────────────────────────────────────

    _updateCardTitle() {
      // The element lives inside: widget-card > .widget-card > .widget-body > xact-big-number
      const card = this.closest('widget-card');
      if (card && typeof card.setTitle === 'function') {
        card.setTitle(this._config.label || 'Big Number');
      }
    }

    // ── Rendering ─────────────────────────────────────────────────────────

    _render() {
      const { unit } = this._config;
      const hasTag = Boolean(this._config.tagPath);

      this.innerHTML = `
        <style>
          .bn-root {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            height: 100%;
            padding: 0.75rem;
            box-sizing: border-box;
            overflow: hidden;
          }

          .bn-no-tag {
            font-size: 0.7rem;
            opacity: 0.4;
            font-style: italic;
            font-family: 'IBM Plex Mono', monospace;
            text-align: center;
          }

          .bn-body {
            display: flex;
            align-items: baseline;
            gap: 0.2em;
          }

          .bn-value {
            font-family: 'IBM Plex Mono', 'Courier New', monospace;
            font-size: clamp(1.8rem, 4.5cqw, 5rem);
            font-weight: 700;
            line-height: 1;
            letter-spacing: -0.03em;
            color: var(--accent-color, #f59e0b);
            white-space: nowrap;
          }

          .bn-unit {
            font-family: 'IBM Plex Mono', 'Courier New', monospace;
            font-size: clamp(0.9rem, 2cqw, 2rem);
            font-weight: 400;
            opacity: 0.55;
            color: var(--content-text, currentColor);
          }

          @keyframes bn-flash {
            0%   { color: var(--success-color, #10b981); }
            100% { color: var(--accent-color, #f59e0b); }
          }

          .bn-value.bn-flashing {
            animation: bn-flash 0.4s ease-out forwards;
          }
        </style>

        <div class="bn-root">
          ${hasTag ? `
            <div class="bn-body">
              <span class="bn-value" id="bn-val">-</span>
              ${unit ? `<span class="bn-unit">${_escapeHtml(unit)}</span>` : ''}
            </div>
          ` : `
            <div class="bn-no-tag">No tag configured - click ⚙ to set a tag path</div>
          `}
        </div>
      `;
    }

    // ── Subscription ───────────────────────────────────────────────────────

    _subscribe() {
      const { tagPath } = this._config;
      if (!tagPath || !window.XACT) return;

      const store = window.XACT.getMirrorStore();

      store.subscribe(tagPath, (value) => {
        if (!this._mounted) return;       // widget removed from DOM

        const el = this.querySelector('#bn-val');
        if (!el) return;

        el.textContent = _formatValue(value, this._config.decimals);

        // Brief colour flash to indicate a live update
        el.classList.remove('bn-flashing');
        void el.offsetWidth;              // force reflow to restart animation
        el.classList.add('bn-flashing');
      });
    }
  }

  // ─── Helpers ─────────────────────────────────────────────────────────────

  function _formatValue(value, decimals) {
    if (value === null || value === undefined) return '-';
    if (typeof value === 'number') {
      return value.toFixed(decimals);
    }
    return String(value);
  }

  function _normalizeDecimals(value) {
    const n = Number(value);
    if (!Number.isFinite(n)) return 2;
    return Math.max(0, Math.min(10, Math.trunc(n)));
  }

  function _escapeHtml(str) {
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  // ─── Registration ─────────────────────────────────────────────────────────

  if (!window.XACT) {
    console.error('[big-number] window.XACT bridge not found - ensure loader.ts ran first');
    return;
  }

  window.XACT.registerWidget(
    {
      type:     'xact-big-number',
      name:     'Big Number',
      icon:     '⬛',
      defaultW: 4,
      defaultH: 3,
      minW:     2,
      minH:     2,
    },
    BigNumberWidget
  );

})();
