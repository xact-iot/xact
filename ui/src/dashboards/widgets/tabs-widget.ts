/**
 * tabs-widget - Layout widget providing a tabbed container for other widgets.
 *
 * Each tab holds one child widget. Tabs run across the top of the widget.
 * No header text is shown; the widget-card's gear/delete float over the body
 * (via setHeaderVisible(false)).
 *
 * In edit mode:
 *   - The active tab shows ⚙ only when the child widget is configurable, and × (remove tab).
 *   - Tabs can be reordered by dragging.
 *   - A + button adds new tabs.
 *   - The widget-card gear opens the full tab-list manager overlay.
 *
 * Child widget-config-save events are intercepted, the per-tab config is
 * updated, then the full tabs config is re-emitted so dashboard-container persists
 * the correct state.
 */

import { BaseComponent } from '../../components/base-component';
import { ensureWidgetTypeLoaded, registerWidgetType, getAvailableWidgets, getWidgetMeta } from './widget-registry';
import type { WidgetPropertiesDialog } from './widget-properties-dialog';

// ── Config ────────────────────────────────────────────────────────────────────

interface TabEntry {
  id: string;
  label: string;
  widgetType: string;
  widgetConfig: Record<string, any>;
}

interface Config {
  tabs: TabEntry[];
  activeTabId: string;
}

const DEFAULT_CONFIG: Config = {
  tabs: [],
  activeTabId: '',
};

function generateTabId(): string {
  return 't' + Math.random().toString(36).substring(2, 8);
}

function cloneWidgetConfig(config: Record<string, any> | undefined): Record<string, any> {
  if (!config) return {};
  try {
    return JSON.parse(JSON.stringify(config));
  } catch {
    return { ...config };
  }
}

function cloneTab(tab: TabEntry): TabEntry {
  return {
    ...tab,
    widgetConfig: cloneWidgetConfig(tab.widgetConfig),
  };
}

// ── Styles (injected once) ────────────────────────────────────────────────────

function ensureStyles(): void {
  if (document.getElementById('tabs-widget-styles')) return;
  const s = document.createElement('style');
  s.id = 'tabs-widget-styles';
  s.textContent = `
    /* Tab manager overlay */
    .tw-mgr-overlay {
      position: fixed;
      inset: 0;
      background: rgba(0,0,0,0.55);
      z-index: 2000;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .tw-mgr-panel {
      background: var(--modal-bg);
      border: 1px solid var(--widget-border);
      border-radius: 8px;
      padding: 20px;
      width: 540px;
      max-height: 75vh;
      display: flex;
      flex-direction: column;
      box-shadow: 0 12px 48px rgba(0,0,0,0.5);
      font-family: var(--widget-font-family);
    }
    .tw-mgr-title {
      font-size: 0.875rem;
      font-weight: 600;
      color: var(--modal-text);
      margin-bottom: 14px;
      flex-shrink: 0;
    }
    .tw-mgr-list {
      flex: 1;
      overflow-y: auto;
      margin-bottom: 8px;
      min-height: 40px;
    }
    .tw-mgr-row {
      display: flex;
      align-items: center;
      gap: 6px;
      padding: 7px 8px;
      margin-bottom: 4px;
      border: 1px solid var(--widget-border);
      border-radius: 6px;
      background: color-mix(in srgb, var(--accent-color) 3%, var(--widget-bg));
      cursor: default;
    }
    .tw-mgr-row.tw-drag-over {
      border-color: var(--accent-color);
      background: color-mix(in srgb, var(--accent-color) 8%, var(--widget-bg));
    }
    .tw-mgr-drag {
      color: var(--widget-header-text);
      opacity: 0.4;
      font-size: 14px;
      cursor: grab;
      padding: 0 2px;
      flex-shrink: 0;
      user-select: none;
    }
    .tw-mgr-input {
      flex: 1;
      min-width: 70px;
      background: var(--widget-bg);
      border: 1px solid var(--widget-border);
      border-radius: 4px;
      padding: 4px 8px;
      font-size: var(--widget-label-font-size);
      color: var(--modal-text);
      font-family: var(--widget-font-family);
      outline: none;
    }
    .tw-mgr-input:focus { border-color: var(--accent-color); }
    .tw-mgr-select {
      flex: 1.6;
      min-width: 110px;
      background: var(--widget-bg);
      border: 1px solid var(--widget-border);
      border-radius: 4px;
      padding: 4px 6px;
      font-size: var(--widget-label-font-size);
      color: var(--modal-text);
      font-family: var(--widget-font-family);
      outline: none;
    }
    .tw-mgr-select:focus { border-color: var(--accent-color); }
    .tw-mgr-rm {
      width: 22px;
      height: 22px;
      flex-shrink: 0;
      border: 1px solid var(--widget-border);
      border-radius: 4px;
      background: transparent;
      color: var(--modal-text);
      opacity: 0.5;
      cursor: pointer;
      font-size: 11px;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: opacity 0.15s, border-color 0.15s, color 0.15s;
      padding: 0;
    }
    .tw-mgr-rm:hover {
      opacity: 1;
      border-color: var(--danger-color);
      color: var(--danger-color);
    }
    .tw-mgr-add {
      width: 100%;
      padding: 7px;
      border: 1px dashed var(--accent-color);
      border-radius: 6px;
      background: transparent;
      color: var(--accent-color);
      cursor: pointer;
      font-size: var(--widget-label-font-size);
      font-family: var(--widget-font-family);
      text-align: center;
      margin-bottom: 14px;
      flex-shrink: 0;
      transition: background 0.15s;
    }
    .tw-mgr-add:hover {
      background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    }
    .tw-mgr-footer {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
      flex-shrink: 0;
    }
    .tw-mgr-btn {
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
    .tw-mgr-btn:hover {
      background: color-mix(in srgb, var(--accent-color) 10%, transparent);
    }
    .tw-mgr-btn.tw-primary {
      background: var(--accent-color);
      border-color: var(--accent-color);
      color: var(--accent-text);
    }
    .tw-mgr-btn.tw-primary:hover { filter: brightness(1.1); }
  `;
  document.head.appendChild(s);
}

// ── Widget ────────────────────────────────────────────────────────────────────

export class TabsWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG, tabs: [] };
  private editMode = false;
  private dragTabId: string | null = null;
  private _mgrOverlay: HTMLElement | null = null;
  private _propsDialog: WidgetPropertiesDialog | null = null;

  // ── Lifecycle ────────────────────────────────────────────────────────────────

  connectedCallback(): void {
    ensureStyles();
    super.connectedCallback();
  }

  disconnectedCallback(): void {
    this._closeMgr();
    if (this._propsDialog) {
      this._propsDialog.remove();
      this._propsDialog = null;
    }
    super.disconnectedCallback();
  }

  // ── Public API ───────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    this.config = { ...this.config, ...c };
    if (c.tabs) this.config.tabs = c.tabs.map(cloneTab);
    if (!Array.isArray(this.config.tabs)) this.config.tabs = [];
    if (!this.config.tabs.find(t => t.id === this.config.activeTabId)) {
      this.config.activeTabId = this.config.tabs[0]?.id ?? '';
    }
    this.rerender();
  }

  getConfig(): Config {
    this._syncChildConfig();
    return { ...this.config, tabs: this.config.tabs.map(cloneTab) };
  }

  setEditMode(editing: boolean): void {
    this.editMode = editing;
    this.rerender();
  }

  /** Called by widget-card gear button - opens the tab manager overlay. */
  openConfig(): void {
    if (this._mgrOverlay) return;
    this._syncChildConfig();
    this._mgrOverlay = document.createElement('div');
    this._mgrOverlay.className = 'tw-mgr-overlay';
    document.body.appendChild(this._mgrOverlay);
    this._renderMgr();
  }

  // ── Render ───────────────────────────────────────────────────────────────────

  protected render(): void {
    this.style.display = 'flex';
    this.style.flexDirection = 'column';
    this.style.height = '100%';
    this.style.overflow = 'hidden';

    // Hide the widget-card header - gear/delete float over content
    const card = this.closest('widget-card') as any;
    card?.setHeaderVisible?.(false);

    const activeTab = this.config.tabs.find(t => t.id === this.config.activeTabId);

    this.innerHTML = `
      <div style="
        display:flex; flex-direction:column; height:100%;
        font-family:var(--widget-font-family);
      ">
        <!-- Tab bar -->
        <div class="tw-tab-bar" style="
          display:flex; align-items:stretch;
          background:var(--widget-header-bg);
          border-bottom:1px solid var(--widget-border);
          flex-shrink:0;
          overflow-x:auto; overflow-y:hidden;
          scrollbar-width:none;
        ">
          <div style="display:flex; align-items:stretch; flex:1; min-width:0;">
            ${this.config.tabs.map(tab => this._renderTab(tab)).join('')}
            ${this.editMode ? `
              <button class="tw-add-tab" title="Add tab" style="
                display:flex; align-items:center; justify-content:center;
                padding:0 0.6rem;
                color:var(--accent-color); opacity:0.6;
                background:transparent; border:none; cursor:pointer;
                font-size:1.1rem;
                transition:opacity 0.15s;
                flex-shrink:0;
              ">+</button>
            ` : ''}
          </div>
        </div>

        <!-- Dashboard content -->
        <div class="tw-panel-content" style="
          flex:1; min-height:0;
          display:flex; flex-direction:column;
          overflow:hidden;
        ">
          ${activeTab ? '' : this._renderEmpty()}
        </div>
      </div>
    `;

    if (activeTab) this._mountWidget(activeTab);
  }

  private _renderTab(tab: TabEntry): string {
    const isActive = tab.id === this.config.activeTabId;
    const showChildConfigure = this.editMode && isActive && this._tabChildIsConfigurable(tab);
    return `
      <div class="tw-tab ${isActive ? 'tw-tab-active' : ''}"
           data-tab-id="${tab.id}"
           draggable="${this.editMode ? 'true' : 'false'}"
           style="
             display:flex; align-items:center; gap:0.2rem;
             padding:0 ${this.editMode && isActive ? '0.45rem' : '0.75rem'} 0 0.75rem;
             cursor:pointer; user-select:none; position:relative;
             font-size:0.78rem; font-weight:${isActive ? '600' : '400'};
             color:${isActive ? 'var(--accent-color)' : 'var(--widget-header-text)'};
             opacity:${isActive ? '1' : '0.6'};
             background:${isActive ? 'var(--widget-bg)' : 'transparent'};
             border-right:1px solid var(--widget-border);
             transition: opacity 0.15s, background 0.15s, color 0.15s;
             flex-shrink:0; min-height:1.85rem;
           ">
        <span style="white-space:nowrap; overflow:hidden; text-overflow:ellipsis; max-width:9rem;">
          ${this._esc(tab.label || 'Tab')}
        </span>
        ${showChildConfigure ? `
          <button class="tw-child-cfg" data-tab-id="${tab.id}"
                  title="Configure widget" style="
                    background:none; border:none; cursor:pointer; padding:1px 2px;
                    font-size:0.9rem; opacity:0.55; color:var(--widget-header-text);
                    transition:opacity 0.15s; flex-shrink:0; line-height:1;
                  ">&#9881;</button>
        ` : ''}
        ${this.editMode ? `
          <button class="tw-tab-remove" data-tab-id="${tab.id}"
                  title="Remove tab" style="
                    background:none; border:none; cursor:pointer; padding:1px 2px;
                    font-size:1rem; opacity:0.45; color:var(--widget-header-text);
                    transition:opacity 0.15s; flex-shrink:0; line-height:1;
                  ">&times;</button>
        ` : ''}
        ${isActive ? `<span style="
          position:absolute; bottom:0; left:0; right:0; height:2px;
          background:var(--accent-color);
        "></span>` : ''}
      </div>
    `;
  }

  private _tabChildIsConfigurable(tab: TabEntry): boolean {
    if (!tab.widgetType) return true;

    const ctor = customElements.get(tab.widgetType) as any;
    if (!ctor) {
      void ensureWidgetTypeLoaded(tab.widgetType).then(() => {
        if (this.isConnected) this.rerender();
      }).catch(err => console.warn('TabsWidget: cannot load child widget', tab.widgetType, err));
      return false;
    }

    if (typeof ctor.getPropertySchema === 'function') {
      try {
        const schema = ctor.getPropertySchema();
        if (schema?.length) return true;
      } catch (err) {
        console.warn('TabsWidget: cannot read static property schema for', tab.widgetType, err);
      }
    }

    const proto = ctor.prototype as any;
    if (typeof proto.openConfig === 'function') return true;

    if (typeof proto.getPropertySchema !== 'function') return false;

    try {
      const child = document.createElement(tab.widgetType) as any;
      if (typeof child.setConfig === 'function') {
        child.setConfig(tab.widgetConfig ?? {});
      }
      const schema = child.getPropertySchema();
      return !!schema?.length;
    } catch (err) {
      console.warn('TabsWidget: cannot read property schema for', tab.widgetType, err);
      return false;
    }
  }

  private _renderEmpty(): string {
    return `
      <div style="
        display:flex; align-items:center; justify-content:center;
        flex:1; opacity:0.35; font-size:0.8rem;
        color:var(--content-text);
      ">${this.editMode ? 'Click + to add a tab' : 'No tabs configured'}</div>
    `;
  }

  private _mountWidget(tab: TabEntry): void {
    const container = this.querySelector('.tw-panel-content');
    if (!container) return;

    if (!tab.widgetType) {
      container.innerHTML = `<div style="
        display:flex; align-items:center; justify-content:center;
        flex:1; opacity:0.35; font-size:0.8rem; color:var(--content-text);
      ">${this.editMode ? 'Click ⚙ on the tab to select a widget' : ''}</div>`;
      return;
    }

    container.innerHTML = `<div style="
      display:flex; align-items:center; justify-content:center;
      flex:1; opacity:0.35; font-size:0.8rem; color:var(--content-text);
    ">Loading ${this._esc(tab.widgetType)}...</div>`;
    void this._mountWidgetWhenLoaded(container, tab);
  }

  private async _mountWidgetWhenLoaded(container: Element, tab: TabEntry): Promise<void> {
    const widgetType = tab.widgetType;
    if (!widgetType) return;

    try {
      await ensureWidgetTypeLoaded(widgetType);
      if (!this.isConnected || !container.isConnected || this.config.activeTabId !== tab.id) return;
      if (!customElements.get(widgetType)) throw new Error(`Widget type is not registered: ${widgetType}`);

      const widgetEl = document.createElement(widgetType) as any;
      widgetEl.style.flex = '1';
      widgetEl.style.minHeight = '0';
      container.replaceChildren(widgetEl);
      if (typeof widgetEl.setConfig === 'function') widgetEl.setConfig(tab.widgetConfig ?? {});
      if (typeof widgetEl.setEditMode === 'function') widgetEl.setEditMode(this.editMode);
    } catch (err) {
      console.error('TabsWidget: cannot create child widget', widgetType, err);
      if (container.isConnected) {
        container.innerHTML = `<div style="
          display:flex; align-items:center; justify-content:center;
          flex:1; opacity:0.35; font-size:0.8rem; color:var(--content-text);
        ">Unknown widget: ${this._esc(widgetType)}</div>`;
      }
    }
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  // ── Event listeners ──────────────────────────────────────────────────────────

  protected attachEventListeners(): void {
    this.querySelectorAll('.tw-tab').forEach(el => {
      el.addEventListener('click', this._onTabClick);
      el.addEventListener('dragstart', this._onDragStart as EventListener);
      el.addEventListener('dragover', this._onDragOver as EventListener);
      el.addEventListener('dragleave', this._onDragLeave as EventListener);
      el.addEventListener('drop', this._onDrop as EventListener);
      el.addEventListener('dragend', this._onDragEnd as EventListener);
    });
    this.querySelectorAll('.tw-tab-remove').forEach(el =>
      el.addEventListener('click', this._onTabRemove)
    );
    this.querySelectorAll('.tw-child-cfg').forEach(el =>
      el.addEventListener('click', this._onChildCfg)
    );
    this.querySelector('.tw-add-tab')?.addEventListener('click', this._onAddTab);

    // Intercept child widget-config-save before it reaches dashboard-container
    this.addEventListener('widget-config-save', this._onChildConfigSave as EventListener);
  }

  protected detachEventListeners(): void {
    this.querySelectorAll('.tw-tab').forEach(el => {
      el.removeEventListener('click', this._onTabClick);
      el.removeEventListener('dragstart', this._onDragStart as EventListener);
      el.removeEventListener('dragover', this._onDragOver as EventListener);
      el.removeEventListener('dragleave', this._onDragLeave as EventListener);
      el.removeEventListener('drop', this._onDrop as EventListener);
      el.removeEventListener('dragend', this._onDragEnd as EventListener);
    });
    this.querySelectorAll('.tw-tab-remove').forEach(el =>
      el.removeEventListener('click', this._onTabRemove)
    );
    this.querySelectorAll('.tw-child-cfg').forEach(el =>
      el.removeEventListener('click', this._onChildCfg)
    );
    this.querySelector('.tw-add-tab')?.removeEventListener('click', this._onAddTab);
    this.removeEventListener('widget-config-save', this._onChildConfigSave as EventListener);
  }

  // ── Tab bar handlers ─────────────────────────────────────────────────────────

  private _onTabClick = (e: Event): void => {
    const target = e.currentTarget as HTMLElement;
    if ((e.target as HTMLElement).closest('.tw-tab-remove, .tw-child-cfg')) return;
    const tabId = target.dataset.tabId;
    if (!tabId || tabId === this.config.activeTabId) return;
    this._syncChildConfig();
    this.config.activeTabId = tabId;
    this._saveAndRerender();
  };

  private _onTabRemove = (e: Event): void => {
    e.stopPropagation();
    const tabId = (e.currentTarget as HTMLElement).dataset.tabId;
    if (!tabId) return;
    this.config.tabs = this.config.tabs.filter(t => t.id !== tabId);
    if (this.config.activeTabId === tabId) {
      this.config.activeTabId = this.config.tabs[0]?.id ?? '';
    }
    this._saveAndRerender();
  };

  private _onAddTab = (e: Event): void => {
    e.stopPropagation();
    const newTab: TabEntry = {
      id: generateTabId(),
      label: `Tab ${this.config.tabs.length + 1}`,
      widgetType: '',
      widgetConfig: {},
    };
    this._syncChildConfig();
    this.config.tabs.push(newTab);
    this.config.activeTabId = newTab.id;
    this._saveAndRerender();
  };

  // ── Child widget configure ───────────────────────────────────────────────────

  private _onChildCfg = (e: Event): void => {
    e.stopPropagation();
    this._syncChildConfig();

    const tabId = (e.currentTarget as HTMLElement).dataset.tabId;
    const activeTab = this.config.tabs.find(t => t.id === tabId);
    const child = this.querySelector('.tw-panel-content > *') as any;

    if (!activeTab) return;

    // If no widget type configured yet, open the full manager
    if (!activeTab.widgetType) {
      this.openConfig();
      return;
    }

    if (!child) return;

    // Delegate to child's openConfig if available
    if (typeof child.openConfig === 'function') {
      child.openConfig();
      return;
    }

    // Fall back to widget-properties-dialog
    if (typeof child.getPropertySchema === 'function') {
      const schema = child.getPropertySchema();
      if (!schema?.length) return;
      const currentValues = typeof child.getConfig === 'function'
        ? child.getConfig()
        : (activeTab.widgetConfig ?? {});

      if (!this._propsDialog) {
        this._propsDialog = document.createElement('widget-properties-dialog') as WidgetPropertiesDialog;
        document.body.appendChild(this._propsDialog);
      }
      this._propsDialog.addEventListener('properties-updated', this._onChildPropsUpdated as EventListener, { once: true });
      this._propsDialog.open('tabs-child', schema, currentValues, false, getWidgetMeta(activeTab.widgetType)?.name ?? 'Widget Properties');
    }
  };

  private _onChildPropsUpdated = (e: CustomEvent): void => {
    const { config } = e.detail;
    const activeTab = this.config.tabs.find(t => t.id === this.config.activeTabId);
    if (!activeTab) return;
    activeTab.widgetConfig = { ...activeTab.widgetConfig, ...config };
    const child = this.querySelector('.tw-panel-content > *') as any;
    if (child && typeof child.setConfig === 'function') child.setConfig(config);
    this._emitConfig();
  };

  // ── Drag-to-reorder ──────────────────────────────────────────────────────────

  private _onDragStart = (e: DragEvent): void => {
    if (!this.editMode) return;
    const tabEl = e.currentTarget as HTMLElement;
    this.dragTabId = tabEl.dataset.tabId ?? null;
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', this.dragTabId ?? '');
    }
    tabEl.style.opacity = '0.4';
  };

  private _onDragOver = (e: DragEvent): void => {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    const tabEl = (e.currentTarget as HTMLElement).closest('.tw-tab') as HTMLElement;
    if (tabEl?.dataset.tabId !== this.dragTabId) {
      this.querySelectorAll('.tw-tab').forEach(el => (el as HTMLElement).style.boxShadow = '');
      tabEl.style.boxShadow = 'inset 2px 0 0 var(--accent-color)';
    }
  };

  private _onDragLeave = (e: DragEvent): void => {
    (e.currentTarget as HTMLElement).style.boxShadow = '';
  };

  private _onDrop = (e: DragEvent): void => {
    e.preventDefault();
    const targetEl = (e.currentTarget as HTMLElement).closest('.tw-tab') as HTMLElement;
    const targetId = targetEl?.dataset.tabId;
    if (!this.dragTabId || !targetId || this.dragTabId === targetId) return;
    this._syncChildConfig();
    const tabs = this.config.tabs;
    const fromIdx = tabs.findIndex(t => t.id === this.dragTabId);
    const toIdx = tabs.findIndex(t => t.id === targetId);
    if (fromIdx !== -1 && toIdx !== -1) {
      const [moved] = tabs.splice(fromIdx, 1);
      tabs.splice(toIdx, 0, moved);
    }
    this.dragTabId = null;
    this._saveAndRerender();
  };

  private _onDragEnd = (e: DragEvent): void => {
    (e.currentTarget as HTMLElement).style.opacity = '';
    this.querySelectorAll('.tw-tab').forEach(el => (el as HTMLElement).style.boxShadow = '');
    this.dragTabId = null;
  };

  // ── Child config-save interception ───────────────────────────────────────────

  private _onChildConfigSave = (e: CustomEvent): void => {
    // Let our own re-emitted events pass through
    if (e.target === this) return;

    const childEl = this.querySelector('.tw-panel-content > *');
    if (!childEl?.contains(e.target as Node)) return;

    e.stopPropagation();
    const activeTab = this.config.tabs.find(t => t.id === this.config.activeTabId);
    if (activeTab) activeTab.widgetConfig = cloneWidgetConfig(e.detail.config);
    this._emitConfig(e.detail?.forceDirty === true);
  };

  // ── Tab manager overlay ──────────────────────────────────────────────────────

  private _renderMgr(): void {
    if (!this._mgrOverlay) return;

    const available = getAvailableWidgets().filter(w => w.type !== 'tabs-widget');
    const baseOpts = ['<option value="">(none)</option>',
      ...available.map(w => `<option value="${w.type}">${w.icon} ${this._esc(w.name)}</option>`)
    ].join('');

    this._mgrOverlay.innerHTML = `
      <div class="tw-mgr-panel">
        <div class="tw-mgr-title">Manage Tabs</div>
        <div class="tw-mgr-list" id="tw-mgr-list">
          ${this.config.tabs.map(t => this._mgrRowHtml(t, available)).join('')}
        </div>
        <button class="tw-mgr-add" id="tw-mgr-add">+ Add Tab</button>
        <div class="tw-mgr-footer">
          <button class="tw-mgr-btn" id="tw-mgr-cancel">Cancel</button>
          <button class="tw-mgr-btn tw-primary" id="tw-mgr-save">Save</button>
        </div>
      </div>
    `;

    const list = this._mgrOverlay.querySelector('#tw-mgr-list') as HTMLElement;

    this._attachMgrDrag(list);

    list.querySelectorAll('.tw-mgr-rm').forEach(btn =>
      btn.addEventListener('click', (e) => {
        const id = (e.currentTarget as HTMLElement).dataset.rm;
        list.querySelector(`[data-mgr-tab="${id}"]`)?.remove();
      })
    );

    this._mgrOverlay.querySelector('#tw-mgr-add')?.addEventListener('click', () => {
      const newId = generateTabId();
      const row = document.createElement('div');
      row.className = 'tw-mgr-row';
      row.setAttribute('data-mgr-tab', newId);
      row.setAttribute('draggable', 'true');
      row.innerHTML = `
        <span class="tw-mgr-drag">⠿</span>
        <input class="tw-mgr-input" type="text" placeholder="Tab label" value="New Tab" data-field="label">
        <select class="tw-mgr-select" data-field="widgetType">${baseOpts}</select>
        <button class="tw-mgr-rm" data-rm="${newId}">✕</button>
      `;
      list.appendChild(row);
      row.querySelector('.tw-mgr-rm')?.addEventListener('click', () => row.remove());
    });

    this._mgrOverlay.querySelector('#tw-mgr-cancel')?.addEventListener('click', () => this._closeMgr());

    this._mgrOverlay.querySelector('#tw-mgr-save')?.addEventListener('click', () => {
      const rows = list.querySelectorAll('[data-mgr-tab]');
      const newTabs: TabEntry[] = [];
      rows.forEach(row => {
        const id = row.getAttribute('data-mgr-tab')!;
        const label = (row.querySelector('[data-field="label"]') as HTMLInputElement)?.value.trim() || 'Tab';
        const widgetType = (row.querySelector('[data-field="widgetType"]') as HTMLSelectElement)?.value ?? '';
        const existing = this.config.tabs.find(t => t.id === id);
        const widgetConfig = (existing && existing.widgetType === widgetType) ? cloneWidgetConfig(existing.widgetConfig) : {};
        newTabs.push({ id, label, widgetType, widgetConfig });
      });
      const newActiveId = newTabs.find(t => t.id === this.config.activeTabId)?.id
        ?? newTabs[0]?.id ?? '';
      this.config = { tabs: newTabs, activeTabId: newActiveId };
      this.rerender();
      this._emitConfig();
      this._closeMgr();
    });

    this._mgrOverlay.addEventListener('click', (e) => {
      if (e.target === this._mgrOverlay) this._closeMgr();
    });
  }

  private _mgrRowHtml(tab: TabEntry, available: ReturnType<typeof getAvailableWidgets>): string {
    const opts = ['<option value="">(none)</option>',
      ...available.map(w =>
        `<option value="${w.type}"${tab.widgetType === w.type ? ' selected' : ''}>${w.icon} ${this._esc(w.name)}</option>`
      )
    ].join('');
    return `
      <div class="tw-mgr-row" data-mgr-tab="${tab.id}" draggable="true">
        <span class="tw-mgr-drag">⠿</span>
        <input class="tw-mgr-input" type="text" placeholder="Tab label"
               value="${this._esc(tab.label)}" data-field="label">
        <select class="tw-mgr-select" data-field="widgetType">${opts}</select>
        <button class="tw-mgr-rm" data-rm="${tab.id}">✕</button>
      </div>
    `;
  }

  private _attachMgrDrag(list: HTMLElement): void {
    let src: HTMLElement | null = null;

    list.addEventListener('dragstart', (e) => {
      const row = (e.target as HTMLElement).closest('[data-mgr-tab]') as HTMLElement | null;
      if (row) { src = row; row.style.opacity = '0.45'; e.dataTransfer!.effectAllowed = 'move'; }
    });
    list.addEventListener('dragend', () => {
      if (src) { src.style.opacity = ''; src = null; }
      list.querySelectorAll('[data-mgr-tab]').forEach(r => r.classList.remove('tw-drag-over'));
    });
    list.addEventListener('dragover', (e) => {
      e.preventDefault();
      const row = (e.target as HTMLElement).closest('[data-mgr-tab]') as HTMLElement | null;
      list.querySelectorAll('[data-mgr-tab]').forEach(r => r.classList.remove('tw-drag-over'));
      if (row && row !== src) row.classList.add('tw-drag-over');
    });
    list.addEventListener('drop', (e) => {
      e.preventDefault();
      const row = (e.target as HTMLElement).closest('[data-mgr-tab]') as HTMLElement | null;
      if (row && src && row !== src) {
        const all = Array.from(list.querySelectorAll('[data-mgr-tab]'));
        const si = all.indexOf(src), di = all.indexOf(row);
        if (si !== -1 && di !== -1) { if (si < di) row.after(src); else row.before(src); }
      }
      list.querySelectorAll('[data-mgr-tab]').forEach(r => r.classList.remove('tw-drag-over'));
    });
  }

  private _closeMgr(): void {
    this._mgrOverlay?.remove();
    this._mgrOverlay = null;
  }

  // ── Helpers ──────────────────────────────────────────────────────────────────

  private _saveAndRerender(): void {
    this._emitConfig();
    this.rerender();
  }

  private _emitConfig(forceDirty = false): void {
    this.emit('widget-config-save', {
      config: { ...this.config, tabs: this.config.tabs.map(cloneTab) },
      forceDirty,
    });
  }

  /** Read back the current active child widget's config before navigation. */
  private _syncChildConfig(): void {
    const activeTab = this.config.tabs.find(t => t.id === this.config.activeTabId);
    if (!activeTab) return;
    const child = this.querySelector('.tw-panel-content > *') as any;
    if (child && typeof child.getConfig === 'function') {
      activeTab.widgetConfig = cloneWidgetConfig(child.getConfig());
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

// ── Registration ──────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'tabs-widget',
  name: 'Tabs',
  icon: '📑',
  category: 'Layout',
  defaultW: 12,
  defaultH: 8,
  minW: 4,
  minH: 3,
});

customElements.define('tabs-widget', TabsWidget);
