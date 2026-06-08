/**
 * array-layout-widget - Layout widget that repeats a child widget for each
 * element in an RTDB array node.
 *
 * The user configures:
 *   - arrayPath   - dot-separated path to an RTDB array node (wildcard * supported)
 *   - widgetType  - which widget to instantiate for each element
 *   - widgetConfig - shared config passed to every child widget instance
 *
 * Children are laid out left-to-right in a wrapping flex container.
 * Each child's config receives an `arrayElementPath` override so it knows
 * which RTDB sub-tree it represents.
 *
 * In edit mode only the first instance shows the gear icon (per spec).
 */

import { BaseComponent } from '../../components/base-component';
import { ensureWidgetTypeLoaded, registerWidgetType, getAvailableWidgets, getWidgetMeta } from './widget-registry';
import { getMirrorStore } from '../../store/store';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import type { WidgetPropertiesDialog } from './widget-properties-dialog';
import type { PropertyField } from './widget-properties-dialog';

// ── Config ───────────────────────────────────────────────────────────────────

interface Config {
  arrayPath: string;
  widgetType: string;
  widgetConfig: Record<string, any>;
  /** Width of each repeated tile in pixels. 0 = auto (fit content). */
  tileWidth: number;
  /** Height of each repeated tile in pixels. 0 = match widget height. */
  tileHeight: number;
  /** Gap between tiles in pixels */
  tileGap: number;
}

const DEFAULT_CONFIG: Config = {
  arrayPath: '',
  widgetType: '',
  widgetConfig: {},
  tileWidth: 320,
  tileHeight: 240,
  tileGap: 8,
};

// ── Styles (injected once) ───────────────────────────────────────────────────

function ensureStyles(): void {
  if (document.getElementById('array-layout-widget-styles')) return;
  const s = document.createElement('style');
  s.id = 'array-layout-widget-styles';
  s.textContent = `
    .alw-container {
      display: flex;
      flex-wrap: wrap;
      align-content: flex-start;
      height: 100%;
      overflow-y: auto;
      overflow-x: hidden;
      padding: 6px;
      scrollbar-width: thin;
      scrollbar-color: var(--border-color) transparent;
    }

    .alw-tile {
      position: relative;
      border: 1px solid var(--widget-border);
      border-radius: 6px;
      background: var(--widget-bg);
      overflow: hidden;
      display: flex;
      flex-direction: column;
    }

    .alw-tile-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 4px 8px;
      background: var(--widget-header-bg);
      border-bottom: 1px solid var(--widget-border);
      font-family: var(--widget-font-family);
      font-size: var(--widget-label-font-size);
      font-weight: 600;
      color: var(--widget-header-text);
      flex-shrink: 0;
      min-height: 1.6rem;
    }

    .alw-tile-label {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      flex: 1;
      min-width: 0;
    }

    .alw-tile-body {
      flex: 1;
      min-height: 0;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    .alw-tile-gear {
      background: none;
      border: none;
      cursor: pointer;
      padding: 1px 4px;
      font-size: 0.85rem;
      opacity: 0.5;
      color: var(--widget-header-text);
      transition: opacity 0.15s;
      flex-shrink: 0;
      line-height: 1;
    }
    .alw-tile-gear:hover { opacity: 1; }

    .alw-empty {
      display: flex;
      align-items: center;
      justify-content: center;
      height: 100%;
      opacity: 0.35;
      font-size: 0.8rem;
      color: var(--content-text);
      font-family: var(--widget-font-family);
    }

    /* Config overlay */
    .alw-cfg-overlay {
      position: fixed;
      inset: 0;
      background: rgba(0,0,0,0.55);
      z-index: 2000;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .alw-cfg-panel {
      background: var(--modal-bg);
      border: 1px solid var(--widget-border);
      border-radius: 8px;
      padding: 20px;
      width: 480px;
      max-height: 75vh;
      display: flex;
      flex-direction: column;
      box-shadow: 0 12px 48px rgba(0,0,0,0.5);
      font-family: var(--widget-font-family);
    }
    .alw-cfg-title {
      font-size: 0.875rem;
      font-weight: 600;
      color: var(--modal-text);
      margin-bottom: 14px;
      flex-shrink: 0;
    }
    .alw-cfg-field {
      display: flex;
      flex-direction: column;
      gap: 4px;
      margin-bottom: 12px;
    }
    .alw-cfg-label {
      font-size: var(--widget-label-font-size);
      color: var(--modal-text);
      opacity: 0.7;
    }
    .alw-cfg-input {
      background: var(--widget-bg);
      border: 1px solid var(--widget-border);
      border-radius: 4px;
      padding: 6px 8px;
      font-size: var(--widget-label-font-size);
      color: var(--modal-text);
      font-family: var(--widget-font-family);
      outline: none;
    }
    .alw-cfg-input:focus { border-color: var(--accent-color); }
    .alw-cfg-select {
      background: var(--widget-bg);
      border: 1px solid var(--widget-border);
      border-radius: 4px;
      padding: 6px 8px;
      font-size: var(--widget-label-font-size);
      color: var(--modal-text);
      font-family: var(--widget-font-family);
      outline: none;
    }
    .alw-cfg-select:focus { border-color: var(--accent-color); }
    .alw-cfg-footer {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
      margin-top: 8px;
      flex-shrink: 0;
    }
    .alw-cfg-btn {
      padding: 6px 16px;
      border-radius: 6px;
      font-size: var(--widget-label-font-size);
      font-family: var(--widget-font-family);
      cursor: pointer;
      border: 1px solid var(--widget-border);
      background: transparent;
      color: var(--modal-text);
      transition: background 0.15s;
    }
    .alw-cfg-btn:hover {
      background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    }
    .alw-cfg-btn.alw-primary {
      background: var(--accent-color);
      border-color: var(--accent-color);
      color: var(--accent-text);
    }
    .alw-cfg-btn.alw-primary:hover { filter: brightness(1.1); }
    .alw-cfg-hint {
      font-size: 0.65rem;
      opacity: 0.5;
      color: var(--modal-text);
      margin-top: 2px;
    }
  `;
  document.head.appendChild(s);
}

// ── Widget ───────────────────────────────────────────────────────────────────

export class ArrayLayoutWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG };
  private editMode = false;
  private _cfgOverlay: HTMLElement | null = null;
  private _propsDialog: WidgetPropertiesDialog | null = null;
  private _unsubTreeChange: (() => void) | null = null;
  /** Currently rendered element names */
  private _renderedElements: string[] = [];

  // ── Lifecycle ────────────────────────────────────────────────────────────────

  connectedCallback(): void {
    ensureStyles();
    super.connectedCallback();
  }

  disconnectedCallback(): void {
    this._closeCfg();
    this._unsubTree();
    if (this._propsDialog) {
      this._propsDialog.remove();
      this._propsDialog = null;
    }
    super.disconnectedCallback();
  }

  // ── Public API ─────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = {
      ...this.config,
      ...c,
      widgetConfig: { ...(c.widgetConfig ?? this.config.widgetConfig) },
    };
    this.rerender();
  }

  getConfig(): Config {
    this._syncFirstChildConfig();
    return {
      ...this.config,
      widgetConfig: { ...this.config.widgetConfig },
    };
  }

  setEditMode(editing: boolean): void {
    this.editMode = editing;
    this.rerender();
  }

  /** Called by widget-card gear - opens the array config overlay. */
  openConfig(): void {
    if (this._cfgOverlay) return;
    this._syncFirstChildConfig();
    this._cfgOverlay = document.createElement('div');
    this._cfgOverlay.className = 'alw-cfg-overlay';
    document.body.appendChild(this._cfgOverlay);
    this._renderCfg();
  }

  // ── Render ─────────────────────────────────────────────────────────────────

  protected render(): void {
    this.style.display = 'flex';
    this.style.flexDirection = 'column';
    this.style.height = '100%';
    this.style.overflow = 'hidden';

    this._unsubTree();

    if (!this.config.arrayPath) {
      this.innerHTML = `<div class="alw-empty">${this.editMode ? 'Click ⚙ to configure the array path' : 'No array path configured'}</div>`;
      return;
    }

    if (!this.config.widgetType) {
      this.innerHTML = `<div class="alw-empty">${this.editMode ? 'Click ⚙ to select a widget type' : 'No widget type configured'}</div>`;
      return;
    }

    const elements = this._resolveElements();
    this._renderedElements = elements;

    if (elements.length === 0) {
      this.innerHTML = `<div class="alw-empty">No array elements found at: ${this._esc(this.config.arrayPath)}</div>`;
      this._subscribeToTree();
      return;
    }

    const gap = this.config.tileGap || 8;
    const tileW = this.config.tileWidth || 320;
    const tileH = this.config.tileHeight || 240;

    this.innerHTML = `<div class="alw-container" style="gap:${gap}px;"></div>`;
    const container = this.querySelector('.alw-container')!;

    for (let i = 0; i < elements.length; i++) {
      const elemPath = elements[i];
      const elemName = elemPath.split('.').pop() || elemPath;
      // Display 1-based index for numeric element names (e.g. "0" → "1")
      const displayName = /^\d+$/.test(elemName) ? String(Number(elemName) + 1) : elemName;

      const tile = document.createElement('div');
      tile.className = 'alw-tile';
      tile.dataset.elemPath = elemPath;
      tile.style.width = `${tileW}px`;
      tile.style.height = `${tileH}px`;
      tile.style.flexShrink = '0';

      // Tile header with element name
      const header = document.createElement('div');
      header.className = 'alw-tile-header';

      const label = document.createElement('span');
      label.className = 'alw-tile-label';
      label.textContent = displayName;
      header.appendChild(label);

      // Only first tile gets gear icon in edit mode (per spec)
      if (this.editMode && i === 0) {
        const gear = document.createElement('button');
        gear.className = 'alw-tile-gear';
        gear.innerHTML = '&#9881;';
        gear.title = 'Configure child widget';
        gear.addEventListener('click', this._onFirstChildGear);
        header.appendChild(gear);
      }

      tile.appendChild(header);

      // Tile body - mount child widget
      const body = document.createElement('div');
      body.className = 'alw-tile-body';
      tile.appendChild(body);

      void this._mountChildWidget(body, elemPath, displayName);

      container.appendChild(tile);
    }

    // Subscribe to tree changes so we re-render when elements are added/removed
    this._subscribeToTree();
  }

  private async _mountChildWidget(body: Element, elemPath: string, displayName: string): Promise<void> {
    const widgetType = this.config.widgetType;
    body.innerHTML = `<div class="alw-empty">Loading ${this._esc(widgetType)}...</div>`;

    try {
      await ensureWidgetTypeLoaded(widgetType);
      if (!this.isConnected || !body.isConnected || this.config.widgetType !== widgetType) return;
      if (!customElements.get(widgetType)) throw new Error(`Widget type is not registered: ${widgetType}`);

      const widgetEl = document.createElement(widgetType) as any;
      widgetEl.style.flex = '1';
      widgetEl.style.minHeight = '0';
      body.replaceChildren(widgetEl);

      // Build per-element config: shared config + element-specific path
      // Store org-relative paths so child widgets normalise them via toAbsolute()
      const relElemPath = getMirrorStore().toRelative(elemPath);
      const elemConfig = {
        ...this.config.widgetConfig,
        arrayElementPath: relElemPath,
        arrayElementName: displayName,
        tagPrefix: relElemPath,
      };
      if (typeof widgetEl.setConfig === 'function') widgetEl.setConfig(elemConfig);
      if (typeof widgetEl.setEditMode === 'function') widgetEl.setEditMode(false);
    } catch (err) {
      console.error('ArrayLayoutWidget: failed to create child', widgetType, err);
      if (body.isConnected) body.innerHTML = `<div class="alw-empty">Error: ${this._esc(widgetType)}</div>`;
    }
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  // ── Event listeners ────────────────────────────────────────────────────────

  protected attachEventListeners(): void {
    this.addEventListener('widget-config-save', this._onChildConfigSave as EventListener);
  }

  protected detachEventListeners(): void {
    this.removeEventListener('widget-config-save', this._onChildConfigSave as EventListener);
  }

  // ── Child config interception ──────────────────────────────────────────────

  private _onChildConfigSave = (e: CustomEvent): void => {
    if (e.target === this) return;
    e.stopPropagation();

    // Update shared config from child
    const childConfig = { ...e.detail.config };
    delete childConfig.arrayElementPath;
    delete childConfig.arrayElementName;
    delete childConfig.tagPrefix;
    this.config.widgetConfig = childConfig;

    // Re-emit so dashboard-container persists
    this._emitConfig();

    // Apply to all children
    this._applyConfigToAll();
  };

  private _onFirstChildGear = (e: Event): void => {
    e.stopPropagation();
    const firstTile = this.querySelector('.alw-tile');
    if (!firstTile) return;
    const child = firstTile.querySelector('.alw-tile-body > *') as any;
    if (!child) return;

    if (typeof child.openConfig === 'function') {
      child.openConfig();
      return;
    }

    if (typeof child.getPropertySchema === 'function') {
      const schema = child.getPropertySchema() as PropertyField[];
      if (!schema?.length) return;
      const elemPath = (firstTile as HTMLElement).dataset.elemPath || '';
      const relElemPath = getMirrorStore().toRelative(elemPath);
      const elemName = elemPath.split('.').pop() || '';
      const displayName = /^\d+$/.test(elemName) ? String(Number(elemName) + 1) : elemName;
      const currentValues = {
        ...(typeof child.getConfig === 'function' ? child.getConfig() : (this.config.widgetConfig ?? {})),
        arrayElementPath: relElemPath,
        arrayElementName: displayName,
        tagPrefix: relElemPath,
      };

      if (!this._propsDialog) {
        this._propsDialog = document.createElement('widget-properties-dialog') as WidgetPropertiesDialog;
        document.body.appendChild(this._propsDialog);
      }
      this._propsDialog.addEventListener('properties-updated', this._onChildPropsUpdated as EventListener, { once: true });
      this._propsDialog.open('array-child', schema, currentValues, false, getWidgetMeta(this.config.widgetType)?.name ?? 'Widget Properties');
    }
  };

  private _onChildPropsUpdated = (e: CustomEvent): void => {
    const { config } = e.detail;
    const childConfig = { ...config };
    delete childConfig.arrayElementPath;
    delete childConfig.arrayElementName;
    delete childConfig.tagPrefix;
    this.config.widgetConfig = { ...this.config.widgetConfig, ...childConfig };
    this._emitConfig();
    this._applyConfigToAll();
  };

  private _applyConfigToAll(): void {
    const store = getMirrorStore();
    const tiles = this.querySelectorAll('.alw-tile');
    tiles.forEach(tile => {
      const elemPath = (tile as HTMLElement).dataset.elemPath || '';
      const relElemPath = store.toRelative(elemPath);
      const elemName = elemPath.split('.').pop() || '';
      const displayN = /^\d+$/.test(elemName) ? String(Number(elemName) + 1) : elemName;
      const child = tile.querySelector('.alw-tile-body > *') as any;
      if (child && typeof child.setConfig === 'function') {
        child.setConfig({
          ...this.config.widgetConfig,
          arrayElementPath: relElemPath,
          arrayElementName: displayN,
          tagPrefix: relElemPath,
        });
      }
    });
  }

  // ── RTDB array resolution ──────────────────────────────────────────────────

  private _resolveElements(): string[] {
    const store = getMirrorStore();
    const path = this.config.arrayPath;
    if (!path) return [];

    // Convert to absolute for store operations
    const absPath = store.toAbsolute(path);

    let results: string[];
    if (absPath.includes('*')) {
      results = this._resolvePattern(absPath);
    } else {
      // Direct path: list children of the array node
      results = store.listChildrenNames(absPath).map(name => `${absPath}.${name}`);
    }

    // Natural sort: numeric segments compared as numbers, rest lexicographic
    results.sort((a, b) => {
      const nameA = a.split('.').pop() || '';
      const nameB = b.split('.').pop() || '';
      const numA = Number(nameA);
      const numB = Number(nameB);
      if (!isNaN(numA) && !isNaN(numB)) return numA - numB;
      return nameA.localeCompare(nameB, undefined, { numeric: true });
    });

    return results;
  }

  private _resolvePattern(pattern: string): string[] {
    const store = getMirrorStore();
    const parts = pattern.split('.');
    const starIdx = parts.indexOf('*');
    if (starIdx === -1) return [pattern];

    const prefix = parts.slice(0, starIdx).join('.');
    const suffix = parts.slice(starIdx + 1);

    const children = store.listChildrenNames(prefix);
    const results: string[] = [];
    for (const child of children) {
      const childPath = prefix ? `${prefix}.${child}` : child;
      if (suffix.length === 0) {
        results.push(childPath);
      } else if (suffix[0] === '*') {
        results.push(...this._resolvePattern(childPath + '.' + suffix.join('.')));
      } else {
        results.push(childPath + '.' + suffix.join('.'));
      }
    }
    return results;
  }

  private _subscribeToTree(): void {
    if (!this.config.arrayPath) return;
    const store = getMirrorStore();
    // Subscribe to the array node's parent path for structural changes
    const rawPath = store.toAbsolute(this.config.arrayPath);
    const path = rawPath.includes('*')
      ? rawPath.split('*')[0].replace(/\.$/, '')
      : rawPath;

    if (!path) return;
    this._unsubTreeChange = store.subscribeToTreeChanges(path, (_p, _data) => {
      const newElements = this._resolveElements();
      const changed = newElements.length !== this._renderedElements.length
        || newElements.some((e, i) => e !== this._renderedElements[i]);
      if (changed) {
        this.rerender();
      }
    });
  }

  private _unsubTree(): void {
    if (this._unsubTreeChange) {
      this._unsubTreeChange();
      this._unsubTreeChange = null;
    }
  }

  // ── Config overlay ─────────────────────────────────────────────────────────

  private _renderCfg(): void {
    if (!this._cfgOverlay) return;

    const available = getAvailableWidgets().filter(w =>
      w.type !== 'array-layout-widget' && w.type !== 'tabs-widget'
    );
    const widgetOpts = [
      '<option value="">(select widget)</option>',
      ...available.map(w =>
        `<option value="${w.type}"${this.config.widgetType === w.type ? ' selected' : ''}>${w.icon} ${this._esc(w.name)}</option>`
      ),
    ].join('');

    this._cfgOverlay.innerHTML = `
      <div class="alw-cfg-panel">
        <div class="alw-cfg-title">Array Layout Configuration</div>

        <div class="alw-cfg-field">
          <label class="alw-cfg-label">Array Path</label>
          <div style="display:flex;gap:0.25rem;">
            <input class="alw-cfg-input" id="alw-array-path" type="text"
                   placeholder="org.devices.pump_array"
                   value="${this._esc(this.config.arrayPath)}"
                   style="flex:1;min-width:0;">
            <button id="alw-browse-path" style="padding:0.25rem 0.5rem;font-size:0.875rem;background:color-mix(in srgb,var(--border-color) 40%,transparent);border:1px solid var(--widget-border);border-radius:0.25rem;cursor:pointer;color:var(--modal-text);" title="Browse RTDB tree">…</button>
          </div>
          <div class="alw-cfg-hint">Dot-separated path to an RTDB array node. Wildcards (*) supported.</div>
        </div>

        <div class="alw-cfg-field">
          <label class="alw-cfg-label">Widget Type</label>
          <select class="alw-cfg-select" id="alw-widget-type">${widgetOpts}</select>
        </div>

        <div class="alw-cfg-field">
          <label class="alw-cfg-label">Tile Width (px)</label>
          <input class="alw-cfg-input" id="alw-tile-w" type="number" min="100" max="2000"
                 value="${this.config.tileWidth || 320}">
        </div>

        <div class="alw-cfg-field">
          <label class="alw-cfg-label">Tile Height (px)</label>
          <input class="alw-cfg-input" id="alw-tile-h" type="number" min="80" max="2000"
                 value="${this.config.tileHeight || 240}">
        </div>

        <div class="alw-cfg-field">
          <label class="alw-cfg-label">Gap (px)</label>
          <input class="alw-cfg-input" id="alw-tile-gap" type="number" min="0" max="48"
                 value="${this.config.tileGap ?? 8}">
        </div>

        <div class="alw-cfg-footer">
          <button class="alw-cfg-btn" id="alw-cancel">Cancel</button>
          <button class="alw-cfg-btn alw-primary" id="alw-save">Save</button>
        </div>
      </div>
    `;

    this._cfgOverlay.querySelector('#alw-browse-path')?.addEventListener('click', () => {
      const currentPath = (this._cfgOverlay?.querySelector('#alw-array-path') as HTMLInputElement)?.value.trim() ?? '';
      const expandTo = currentPath.replace(/\.\*.*$/, '');
      getTreeBrowserDialog().open('', 'Select Array Node', (path) => {
        const input = this._cfgOverlay?.querySelector('#alw-array-path') as HTMLInputElement | null;
        if (input) input.value = path;
      }, false, expandTo);
    });

    this._cfgOverlay.querySelector('#alw-cancel')?.addEventListener('click', () => this._closeCfg());
    this._cfgOverlay.querySelector('#alw-save')?.addEventListener('click', () => {
      const arrayPath = (this._cfgOverlay!.querySelector('#alw-array-path') as HTMLInputElement).value.trim();
      const widgetType = (this._cfgOverlay!.querySelector('#alw-widget-type') as HTMLSelectElement).value;
      const tileWidth = parseInt((this._cfgOverlay!.querySelector('#alw-tile-w') as HTMLInputElement).value) || 320;
      const tileHeight = parseInt((this._cfgOverlay!.querySelector('#alw-tile-h') as HTMLInputElement).value) || 240;
      const tileGap = parseInt((this._cfgOverlay!.querySelector('#alw-tile-gap') as HTMLInputElement).value) ?? 8;

      // If widget type changed, clear child config
      if (widgetType !== this.config.widgetType) {
        this.config.widgetConfig = {};
      }

      this.config.arrayPath = arrayPath;
      this.config.widgetType = widgetType;
      this.config.tileWidth = tileWidth;
      this.config.tileHeight = tileHeight;
      this.config.tileGap = tileGap;

      this._emitConfig();
      this.rerender();
      this._closeCfg();
    });

    this._cfgOverlay.addEventListener('click', (e) => {
      if (e.target === this._cfgOverlay) this._closeCfg();
    });
  }

  private _closeCfg(): void {
    this._cfgOverlay?.remove();
    this._cfgOverlay = null;
  }

  // ── Helpers ────────────────────────────────────────────────────────────────

  private _emitConfig(): void {
    this.emit('widget-config-save', {
      config: this.getConfig(),
    });
  }

  private _syncFirstChildConfig(): void {
    const firstChild = this.querySelector('.alw-tile-body > *') as any;
    if (firstChild && typeof firstChild.getConfig === 'function') {
      const childConfig = { ...firstChild.getConfig() };
      delete childConfig.arrayElementPath;
      delete childConfig.arrayElementName;
      delete childConfig.tagPrefix;
      this.config.widgetConfig = childConfig;
    }
  }

  private _esc(s: string): string {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
}

// ── Registration ─────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'array-layout-widget',
  name: 'Array Layout',
  icon: '🔲',
  category: 'Layout',
  defaultW: 12,
  defaultH: 8,
  minW: 4,
  minH: 3,
});

customElements.define('array-layout-widget', ArrayLayoutWidget);
