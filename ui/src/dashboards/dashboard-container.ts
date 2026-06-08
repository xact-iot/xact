import { BaseComponent } from '../components/base-component';
import { getDashboard, createDashboard, updateDashboard } from '../api';
import type { Dashboard } from '../api';
import { GridStack } from 'gridstack';
import 'gridstack/dist/gridstack.min.css';
import { ensureWidgetTypeLoaded, ensureWidgetTypesLoaded, getWidgetsByCategory, getWidgetMeta, WIDGET_CATEGORIES } from './widgets/widget-registry';
import './widgets/widget-card';
import './widgets/widget-properties-dialog';
import { can } from '../permissions/permissions';
import { registerPermissions } from '../permissions/registry';
import { showAlert, showChoice } from '../components/app-dialog';

registerPermissions('dashboard-container', 'Widget Layout Editing', [
  { name: 'inspect', description: 'Inspect dashboard widgets and properties without changing layout' },
  { name: 'edit', description: 'Edit dashboard widgets and layout' },
], 'Controls whether a role can modify dashboard layouts - drag, resize, add, or remove widgets on any dashboard.');
import type { WidgetCard } from './widgets/widget-card';
import type { WidgetPropertiesDialog } from './widgets/widget-properties-dialog';

const GRID_COLUMNS = 24;
export type DashboardMode = 'view' | 'inspect' | 'edit';

export interface WidgetData {
  id: string;
  type: string;
  x: number;
  y: number;
  w: number;
  h: number;
  config: Record<string, any>;
}

function inLayoutOrder(widgets: WidgetData[]): WidgetData[] {
  return widgets
    .map((widget, index) => ({ widget, index }))
    .sort((a, b) =>
      (a.widget.y - b.widget.y)
      || (a.widget.x - b.widget.x)
      || a.index - b.index
    )
    .map(entry => entry.widget);
}

function collectReferencedWidgetTypes(widgets: WidgetData[]): Set<string> {
  const types = new Set<string>();
  for (const widget of widgets) {
    if (widget.type) types.add(widget.type);
    collectNestedWidgetTypes(widget.config, types);
  }
  return types;
}

function collectNestedWidgetTypes(config: any, types: Set<string>): void {
  if (!config || typeof config !== 'object') return;

  if (typeof config.widgetType === 'string') {
    types.add(config.widgetType);
    collectNestedWidgetTypes(config.widgetConfig, types);
  }

  if (Array.isArray(config.tabs)) {
    for (const tab of config.tabs) {
      if (typeof tab?.widgetType === 'string') types.add(tab.widgetType);
      collectNestedWidgetTypes(tab?.widgetConfig, types);
    }
  }

  if (Array.isArray(config.widgets)) {
    for (const child of config.widgets) {
      if (typeof child?.type === 'string') types.add(child.type);
      collectNestedWidgetTypes(child?.config, types);
    }
  }

  if (Array.isArray(config.layers)) {
    for (const layer of config.layers) {
      if (typeof layer?.zoomWidgetType === 'string') types.add(layer.zoomWidgetType);
      if (!layer?.zoomWidgetType && layer?.divWidgetConfig) types.add('status-table-widget');
      if (typeof layer?.sidePanelWidgetType === 'string') types.add(layer.sidePanelWidgetType);
      collectNestedWidgetTypes(layer?.zoomWidgetConfig ?? layer?.divWidgetConfig, types);
      collectNestedWidgetTypes(layer?.sidePanelWidgetConfig, types);
    }
  }
}

export class DashboardContainer extends BaseComponent {
  private dashboardName = '';
  private dashboardData: Dashboard | null = null;
  private mode: DashboardMode = 'view';
  private canEditDashboard = false;
  private canInspectDashboard = false;
  private dirty = false;
  private grid: GridStack | null = null;
  private widgets: WidgetData[] = [];
  private savedWidgets: WidgetData[] = [];
  private beforeUnloadHandler: ((e: BeforeUnloadEvent) => void) | null = null;
  private dropdownHideTimer: ReturnType<typeof setTimeout> | null = null;
  private saving = false;

  private isInteractiveMode(): boolean {
    return this.mode !== 'view';
  }

  async loadDashboard(dashboardRef: string): Promise<void> {
    this.dashboardName = dashboardRef;
    try {
      const id = Number(dashboardRef);
      if (!Number.isInteger(id) || id <= 0) {
        throw new Error('404');
      }
      this.dashboardData = await getDashboard(id);
      this.dashboardName = this.dashboardData.name;
    } catch (err: any) {
      // 404 means the dashboard hasn't been saved to the server yet - treat it
      // as an empty dashboard and drop into edit mode so the user can add widgets.
      if (err?.message?.includes('404')) {
        this.dashboardData = { id: 0, name: dashboardRef, description: '', icon: '', variation: '', deviceType: '', permission: '', isCategory: false, sortOrder: 0, widgets: [] as any };
      } else {
        console.error('Failed to load dashboard:', err);
        this.innerHTML = `<div class="p-4 text-sm opacity-60">Failed to load dashboard "${dashboardRef}".</div>`;
        return;
      }
    }
    this.widgets = Array.isArray(this.dashboardData!.widgets) ? cloneWidgets(this.dashboardData!.widgets as WidgetData[]) : [];
    this.savedWidgets = cloneWidgets(this.widgets);
    const [canEditDashboard, canInspectDashboard, canConfigureWidgets] = await Promise.all([
      can('dashboard-container.edit'),
      can('dashboard-container.inspect'),
      can('widget-default.configure'),
    ]);
    this.canEditDashboard = canEditDashboard;
    this.canInspectDashboard = canEditDashboard || canInspectDashboard || canConfigureWidgets;
    // Auto-enter edit mode for an empty editable dashboard.
    this.mode = this.widgets.length === 0 && this.canEditDashboard ? 'edit' : 'view';
    this.dirty = false;
    this.rerender();
    await this.initGrid();
    this.emitCapabilities();
    this.emitModeChanged();
  }

  hasUnsavedChanges(): boolean {
    return this.dirty;
  }

  protected render(): void {
    // Ensure the custom element itself is a sized flex container
    this.style.display = 'flex';
    this.style.flexDirection = 'column';
    this.style.flex = '1';
    this.style.overflow = 'hidden';

    this.innerHTML = `
      ${this.isInteractiveMode() ? `
      <!-- Edit/inspect toolbar. Inspect can experiment locally but cannot save. -->
      <div id="pc-toolbar" class="flex items-center gap-2 px-4 py-1.5 flex-shrink-0" style="position:relative;z-index:3000;overflow:visible;border-bottom:1px solid var(--border-color);background:color-mix(in srgb, var(--accent-color) 5%, transparent);">
        ${this.renderToolbarItems()}
      </div>
      ` : ''}

      <!-- Grid area -->
      <div id="pc-grid-scroll" class="flex-1 overflow-auto p-4 flex flex-col" style="min-height: 0;">
        <div id="pc-grid" class="grid-stack flex-1"></div>
      </div>

      <!-- Properties Dialog -->
      <widget-properties-dialog id="pc-properties-dialog"></widget-properties-dialog>
    `;
  }

  private renderToolbarItems(): string {
    const byCategory = getWidgetsByCategory();
    const categories = WIDGET_CATEGORIES.map(cat => {
      const widgets = byCategory.get(cat) || [];
      const hasWidgets = widgets.length > 0;
      return `
        <div class="widget-category-menu relative">
          <button class="widget-category-btn px-2.5 py-1 text-xs rounded transition-colors hover:opacity-80 ${hasWidgets ? '' : 'opacity-40'}"
                  style="border: 1px solid var(--border-color);"
                  data-category="${cat}" ${hasWidgets ? '' : 'disabled'}>
            ${cat}
          </button>
          ${hasWidgets ? `
          <div class="widget-category-dropdown hidden absolute top-full left-0 mt-1 min-w-[180px] rounded-lg border shadow-lg py-1"
               style="z-index:3001;background-color:var(--content-bg);border-color:var(--border-color);"
               data-dropdown="${cat}">
            ${widgets.map(w => `
              <div class="widget-toolbar-item grid-stack-item flex items-center gap-2 px-3 py-1.5 text-xs border-0 rounded-none"
                   data-widget-type="${w.type}"
                   data-gs-w="${w.defaultW}" data-gs-h="${w.defaultH}"
                   ${w.minW ? `data-gs-min-w="${w.minW}"` : ''} ${w.minH ? `data-gs-min-h="${w.minH}"` : ''}>
                <div class="grid-stack-item-content flex items-center gap-2">
                  <span class="text-base">${w.icon}</span>
                  <span>${w.name}</span>
                </div>
              </div>
            `).join('')}
          </div>
          ` : ''}
        </div>
      `;
    }).join('');

    const btnStyle = 'border: 1px solid var(--border-color);';
    const btnClass = 'px-2.5 py-1 text-xs rounded transition-colors hover:opacity-80';
    return `${categories}
      <div id="pc-toolbar-actions" class="flex items-center gap-1.5 ml-auto">
        <button id="pc-export" class="${btnClass}" style="${btnStyle}">&#8659; Export</button>
        <button id="pc-import" class="${btnClass}" style="${btnStyle}">&#8657; Import</button>
      </div>`;
  }

  private async initGrid(): Promise<void> {
    const gridEl = this.querySelector('#pc-grid') as HTMLElement;
    if (!gridEl) return;
    this.grid = GridStack.init({
      column: GRID_COLUMNS,
      cellHeight: 20,
      margin: 8,
      animate: true,
      float: false,
      handle: '.widget-header',
      acceptWidgets: this.isInteractiveMode(),
      disableResize: !this.isInteractiveMode(),
      disableDrag: !this.isInteractiveMode(),
      removable: false,
      copyDragIn: true,
    } as any, gridEl);

    // Add existing widgets to the grid before listening for 'change' / 'added',
    // so that loading saved widgets doesn't mark the dashboard as dirty.
    await ensureWidgetTypesLoaded(collectReferencedWidgetTypes(this.widgets));
    for (const w of inLayoutOrder(this.widgets)) {
      await this.addWidgetToGrid(w);
    }

    // Listen for grid changes - registered AFTER initial load to avoid
    // false dirty flags from GridStack position adjustments during load.
    this.grid.on('change', () => {
      if (!this.isInteractiveMode()) return;
      if (this.mode === 'edit') {
        this.dirty = true;
        this.updateSaveButton();
      }
    });

    this.grid.on('added', () => {
      if (!this.isInteractiveMode()) return;
      if (this.mode === 'edit') {
        this.dirty = true;
        this.updateSaveButton();
      }
    });

    // Always register dropped handler - fires only when acceptWidgets is true
    this.grid.on('dropped', (_event: Event, _prevNode: any, newNode: any) => {
      if (!newNode) return;

      // Cancel the deferred hide and close immediately on drop.
      if (this.dropdownHideTimer) {
        clearTimeout(this.dropdownHideTimer);
        this.dropdownHideTimer = null;
      }
      this.closeAllDropdowns();

      // The dropped element from toolbar becomes a grid item.
      // We need to replace it with a proper widget.
      const gsEl = newNode.el as HTMLElement;
      const widgetType = gsEl.querySelector('.grid-stack-item')?.getAttribute('data-widget-type')
        || gsEl.getAttribute('data-widget-type') || 'text-widget';

      const meta = getWidgetMeta(widgetType);
      const widgetData: WidgetData = {
        id: generateId(),
        type: widgetType,
        x: newNode.x ?? 0,
        y: newNode.y ?? 0,
        w: newNode.w ?? meta?.defaultW ?? 4,
        h: newNode.h ?? meta?.defaultH ?? 2,
        config: {},
      };

      // Remove the temporary dropped element and add a proper widget
      this.grid!.removeWidget(gsEl);
      this.widgets.push(widgetData);
      void this.addWidgetToGrid(widgetData);
      if (this.mode === 'edit') {
        this.dirty = true;
        this.updateSaveButton();
      }
    });

    // Set up external drag from toolbar
    if (this.isInteractiveMode()) {
      this.setupToolbarDrag();
    }
  }

  private async addWidgetToGrid(data: WidgetData): Promise<void> {
    if (!this.grid) return;

    const meta = getWidgetMeta(data.type);
    const displayName = meta?.name || data.type;
    try {
      await ensureWidgetTypeLoaded(data.type);
    } catch (err) {
      console.error('Failed to load widget type:', data.type, err);
    }

    // Use addWidget with a placeholder, then inject our DOM elements
    const gsEl = this.grid.addWidget({
      x: data.x,
      y: data.y,
      w: data.w,
      h: data.h,
      minW: meta?.minW,
      minH: meta?.minH,
      id: data.id,
      content: '',
    });

    // Replace the empty content with our widget card
    const contentEl = gsEl.querySelector('.grid-stack-item-content');
    if (!contentEl) return;
    contentEl.innerHTML = '';

    const card = document.createElement('widget-card') as WidgetCard;
    card.setWidgetId(data.id);
    card.setMode(this.mode);
    contentEl.appendChild(card);

    // Now that the card is in the DOM, set title and append widget
    card.setTitle(displayName);
    const body = card.querySelector('.widget-body');
    if (body) {
      if (!customElements.get(data.type)) {
        body.innerHTML = `<div class="p-3 text-xs opacity-60">Unable to load widget "${escapeHtml(data.type)}".</div>`;
        card.setHasProperties(false);
        return;
      }

      const widgetEl = document.createElement(data.type);
      if ('setConfig' in widgetEl && typeof (widgetEl as any).setConfig === 'function') {
        (widgetEl as any).setConfig(data.config || {});
      }

      // Check if widget has property schema with actual properties, OR an
      // openConfig() method (custom overlay). Either enables the gear button.
      let hasProperties = false;
      if ('openConfig' in widgetEl && typeof (widgetEl as any).openConfig === 'function') {
        hasProperties = true;
      } else if ('getPropertySchema' in widgetEl && typeof (widgetEl as any).getPropertySchema === 'function') {
        const schema = (widgetEl as any).getPropertySchema();
        hasProperties = !!(schema && schema.length > 0);
      } else {
        const widgetConstructor = customElements.get(data.type);
        if (widgetConstructor && 'getPropertySchema' in widgetConstructor &&
            typeof (widgetConstructor as any).getPropertySchema === 'function') {
          const schema = (widgetConstructor as any).getPropertySchema();
          hasProperties = !!(schema && schema.length > 0);
        }
      }
      card.setHasProperties(hasProperties);

      body.appendChild(widgetEl);

      // Inspect is intentionally interactive like edit; only server persistence
      // is blocked. Prefer forwarding the full dashboard mode when supported.
      if ('setDashboardMode' in widgetEl && typeof (widgetEl as any).setDashboardMode === 'function') {
        (widgetEl as any).setDashboardMode(this.mode);
      } else if ('setEditMode' in widgetEl && typeof (widgetEl as any).setEditMode === 'function') {
        (widgetEl as any).setEditMode(this.isInteractiveMode());
      }
    }

    // Listen for delete
    card.addEventListener('widget-delete', ((e: CustomEvent) => {
      this.removeWidget(e.detail.widgetId);
    }) as EventListener);

    // Listen for duplicate
    card.addEventListener('widget-duplicate', ((e: CustomEvent) => {
      void this.duplicateWidget(e.detail.widgetId);
    }) as EventListener);

    // Listen for configure
    card.addEventListener('widget-configure', ((e: CustomEvent) => {
      this.showWidgetProperties(e.detail.widgetId);
    }) as EventListener);
  }

  private async duplicateWidget(widgetId: string): Promise<void> {
    if (!this.grid || !this.isInteractiveMode()) return;

    const sourceIndex = this.widgets.findIndex(w => w.id === widgetId);
    const source = this.widgets[sourceIndex];
    if (!source) return;

    const sourceEl = this.querySelector(`[gs-id="${widgetId}"]`) as (HTMLElement & { gridstackNode?: any }) | null;
    const node = sourceEl?.gridstackNode;
    const x = node?.x ?? source.x;
    const y = node?.y ?? source.y;
    const w = node?.w ?? source.w;
    const h = node?.h ?? source.h;
    const rightX = x + w;
    const leftX = x - w;
    const placement = rightX + w <= GRID_COLUMNS
      ? { x: rightX, y }
      : leftX >= 0
        ? { x: leftX, y }
        : { x, y: y + h };

    const copy: WidgetData = {
      ...source,
      id: generateId(),
      x: placement.x,
      y: placement.y,
      w,
      h,
      config: cloneConfig(source.config || {}),
    };

    this.widgets.splice(sourceIndex + 1, 0, copy);
    await this.addWidgetToGrid(copy);
    if (this.mode === 'edit') {
      this.dirty = true;
      this.updateSaveButton();
    }
  }

  private removeWidget(widgetId: string): void {
    if (!this.grid || !this.isInteractiveMode()) return;

    const el = this.querySelector(`[gs-id="${widgetId}"]`);
    if (el) {
      this.grid.removeWidget(el as HTMLElement);
      this.widgets = this.widgets.filter(w => w.id !== widgetId);
      if (this.mode === 'edit') {
        this.dirty = true;
        this.updateSaveButton();
      }
    }
  }

  private showWidgetProperties(widgetId: string): void {
    const widgetData = this.widgets.find(w => w.id === widgetId);
    if (!widgetData) return;
    if (this.mode === 'view') return;
    const readOnly = false;

    // Find the live widget element so we can call instance methods.
    const gsEl = this.querySelector(`[gs-id="${widgetId}"]`);
    const widgetEl = gsEl?.querySelector(widgetData.type) as HTMLElement | null;

    // If widget implements openConfig(), delegate entirely to it.
    if (widgetEl && 'openConfig' in widgetEl && typeof (widgetEl as any).openConfig === 'function') {
      (widgetEl as any).openConfig({ readOnly });
      return;
    }

    let schema: any[] | null = null;
    let currentValues: Record<string, any> = widgetData.config || {};

    // Prefer instance methods - they return context-aware schemas and live config.
    if (widgetEl &&
        'getPropertySchema' in widgetEl &&
        typeof (widgetEl as any).getPropertySchema === 'function') {
      schema = (widgetEl as any).getPropertySchema();
      if ('getConfig' in widgetEl && typeof (widgetEl as any).getConfig === 'function') {
        currentValues = (widgetEl as any).getConfig();
      }
    } else {
      const widgetConstructor = customElements.get(widgetData.type);
      if (!widgetConstructor || !('getPropertySchema' in widgetConstructor)) return;
      schema = (widgetConstructor as any).getPropertySchema();
    }

    if (!schema || !schema.length) return;

    const dialog = this.querySelector('#pc-properties-dialog') as WidgetPropertiesDialog;
    if (dialog) {
      dialog.open(widgetId, schema, currentValues, readOnly, getWidgetMeta(widgetData.type)?.name ?? 'Widget Properties');
    }
  }

  /** Handles widget-config-save events from widgets that manage their own config UI. */
  private handleWidgetConfigSave = (e: CustomEvent): void => {
    if (!this.isInteractiveMode()) return;
    const { config, silent, forceDirty } = e.detail;
    const widgetEl = e.target as HTMLElement;
    const gsEl = widgetEl.closest('[gs-id]');
    const id = gsEl?.getAttribute('gs-id');
    if (!id) return;
    const widgetData = this.widgets.find(w => w.id === id);
    if (widgetData) {
      // Always update the stored config so it is included when the dashboard is saved.
      // Only mark dirty for user-initiated changes - widgets may emit with
      // silent:true during their initial load to persist state without triggering
      // the unsaved-changes warning.
      const changed = JSON.stringify(widgetData.config) !== JSON.stringify(config);
      widgetData.config = config;
      if (this.mode === 'edit' && (changed || forceDirty) && !silent) {
        this.dirty = true;
        this.updateSaveButton();
      }
    }
  };

  private handlePropertiesUpdated = (e: CustomEvent): void => {
    if (!this.isInteractiveMode()) return;
    const { widgetId, config } = e.detail;
    const widgetData = this.widgets.find(w => w.id === widgetId);
    if (!widgetData) return;

    // Merge into existing config so that non-schema fields (e.g. layers) are preserved
    widgetData.config = { ...widgetData.config, ...config };

    // Update the actual widget element
    const gsEl = this.querySelector(`[gs-id="${widgetId}"]`);
    if (gsEl) {
      const widgetEl = gsEl.querySelector(widgetData.type);
      if (widgetEl && 'setConfig' in widgetEl && typeof (widgetEl as any).setConfig === 'function') {
        (widgetEl as any).setConfig(config);
      }

      // Trigger re-render if widget has rerender method
      if (widgetEl && 'rerender' in widgetEl && typeof (widgetEl as any).rerender === 'function') {
        (widgetEl as any).rerender();
      } else if (widgetEl && 'render' in widgetEl && typeof (widgetEl as any).render === 'function') {
        (widgetEl as any).render();
      }
    }

    if (this.mode === 'edit') {
      this.dirty = true;
      this.updateSaveButton();
    }
  };

  private closeAllDropdowns(): void {
    this.querySelectorAll('.widget-category-dropdown').forEach(el =>
      el.classList.add('hidden')
    );
  }

  private setupToolbarDrag(): void {
    if (!this.grid) return;
    GridStack.setupDragIn('.widget-toolbar-item.grid-stack-item');

    // Hide the dropdown 2 s after the user presses down on a toolbar item.
    // GridStack uses its own mouse-event drag system, not the native HTML5
    // drag API, so 'dragstart' never fires - 'mousedown' is the reliable hook.
    this.querySelectorAll('.widget-toolbar-item').forEach(el => {
      el.addEventListener('mousedown', () => {
        if (this.dropdownHideTimer) clearTimeout(this.dropdownHideTimer);
        this.dropdownHideTimer = setTimeout(() => {
          this.closeAllDropdowns();
          this.dropdownHideTimer = null;
        }, 2000);
      });
    });
  }

  /** Add or remove the edit toolbar without touching the grid or its widgets. */
  private syncToolbar(): void {
    const existing = this.querySelector<HTMLElement>('#pc-toolbar');

    if (!this.isInteractiveMode()) {
      if (existing) {
        existing.querySelectorAll('.widget-category-btn').forEach(el =>
          el.removeEventListener('click', this.handleCategoryClick)
        );
        existing.remove();
      }
      return;
    }

    // Create toolbar if it doesn't exist yet
    let toolbar = existing;
    if (!toolbar) {
      toolbar = document.createElement('div');
      toolbar.id = 'pc-toolbar';
      toolbar.className = 'flex items-center gap-2 px-4 py-1.5 flex-shrink-0';
      toolbar.style.cssText = 'position:relative;z-index:3000;overflow:visible;border-bottom:1px solid var(--border-color);background:color-mix(in srgb, var(--accent-color) 5%, transparent);';
      const gridScroll = this.querySelector('#pc-grid-scroll');
      if (gridScroll) {
        this.insertBefore(toolbar, gridScroll);
      } else {
        this.prepend(toolbar);
      }
    }

    // Detach stale listeners, re-render content, reattach
    toolbar.querySelector('#pc-export')?.removeEventListener('click', this.handleExport);
    toolbar.querySelector('#pc-import')?.removeEventListener('click', this.handleImport);
    toolbar.querySelectorAll('.widget-category-btn').forEach(el =>
      el.removeEventListener('click', this.handleCategoryClick)
    );
    toolbar.innerHTML = this.renderToolbarItems();
    toolbar.querySelectorAll('.widget-category-btn').forEach(el =>
      el.addEventListener('click', this.handleCategoryClick)
    );
    toolbar.querySelector('#pc-export')?.addEventListener('click', this.handleExport);
    toolbar.querySelector('#pc-import')?.addEventListener('click', this.handleImport);

    this.updateSaveButton();
    this.setupToolbarDrag();
  }

  private updateSaveButton(): void {
    const existing = this.querySelector<HTMLButtonElement>('#pc-save');
    if (this.dirty && this.mode === 'edit' && !existing) {
      const actions = this.querySelector('#pc-toolbar-actions');
      if (actions) {
        const btn = document.createElement('button');
        btn.id = 'pc-save';
        btn.className = 'px-3 py-1 text-xs rounded-lg text-white transition-colors hover:opacity-90';
        btn.style.backgroundColor = 'var(--danger-color)';
        btn.textContent = this.saving ? 'Saving...' : 'Save';
        btn.disabled = this.saving;
        btn.addEventListener('click', this.handleSave);
        actions.appendChild(btn);
      }
    } else if ((!this.dirty || this.mode !== 'edit') && existing) {
      existing.remove();
    } else if (existing) {
      existing.textContent = this.saving ? 'Saving...' : 'Save';
      existing.disabled = this.saving;
    }
  }

  private collectWidgetsFromGrid(): WidgetData[] {
    if (!this.grid) return [];
    const items = this.grid.getGridItems();
    const savedWidgets: WidgetData[] = [];
    for (const item of items) {
      const node = item.gridstackNode;
      if (!node) continue;

      const id = String(node.id || '');
      const existing = this.widgets.find(w => w.id === id);

      savedWidgets.push({
        id,
        type: existing?.type || 'sample-widget',
        x: node.x ?? 0,
        y: node.y ?? 0,
        w: node.w ?? 4,
        h: node.h ?? 2,
        config: existing?.config || {},
      });
    }
    return inLayoutOrder(savedWidgets);
  }

  private handleSave = async (): Promise<boolean> => {
    if (this.saving) return false;
    if (!this.grid || !this.dashboardName) {
      void showAlert('This dashboard is not ready to save yet. Try reloading the dashboard and saving again.', {
        title: 'Save unavailable',
        tone: 'danger',
      });
      return false;
    }

    const savedWidgets = this.collectWidgetsFromGrid();

    this.saving = true;
    this.updateSaveButton();
    try {
      if (this.dashboardData?.id === 0) {
        // Dashboard doesn't exist on the server yet - create it, then mark as persisted.
        try {
          const created = await createDashboard({
            name: this.dashboardName,
            description: this.dashboardData.description ?? '',
            icon: this.dashboardData.icon ?? '',
            variation: this.dashboardData.variation ?? '',
            deviceType: this.dashboardData.deviceType ?? '',
            permission: this.dashboardData.permission ?? '',
            isCategory: false,
            sortOrder: this.dashboardData.sortOrder ?? 0,
            widgets: savedWidgets as any,
          });
          this.dashboardData.id = created.id;
        } catch (createErr) {
          // If the row already exists but the initial GET missed it, the POST
          // will fail on the unique name constraint. Preserve the user's layout
          // by updating the existing row instead of surfacing a dead-end error.
          console.warn('Dashboard create failed; retrying save as update:', createErr);
          if (!this.dashboardData?.id) throw createErr;
          await updateDashboard(this.dashboardData.id, { widgets: savedWidgets });
        }
      } else {
        await updateDashboard(this.dashboardData!.id, { widgets: savedWidgets });
      }
      this.widgets = savedWidgets;
      this.savedWidgets = cloneWidgets(savedWidgets);
      if (this.dashboardData) this.dashboardData.widgets = cloneWidgets(savedWidgets) as any;
      this.dirty = false;
      this.saving = false;
      this.updateSaveButton();
      return true;
    } catch (err) {
      console.error('Failed to save dashboard:', err);
      this.saving = false;
      this.updateSaveButton();
      void showAlert(`Failed to save dashboard "${this.dashboardName}": ${(err as Error).message}`, {
        title: 'Save failed',
        tone: 'danger',
      });
      return false;
    }
  };

  private handleExport = (): void => {
    if (!this.dashboardName) return;
    const items = this.grid ? this.grid.getGridItems() : [];
    const exportWidgets: WidgetData[] = [];
    for (const item of items) {
      const node = item.gridstackNode;
      if (!node) continue;
      const id = String(node.id || '');
      const existing = this.widgets.find(w => w.id === id);
      exportWidgets.push({
        id,
        type: existing?.type || 'sample-widget',
        x: node.x ?? 0,
        y: node.y ?? 0,
        w: node.w ?? 4,
        h: node.h ?? 2,
        config: existing?.config || {},
      });
    }
    const json = JSON.stringify({ dashboardName: this.dashboardName, widgets: inLayoutOrder(exportWidgets) }, null, 2);
    const blob = new Blob([json], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${this.dashboardName}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  private handleImport = (): void => {
    if (!this.isInteractiveMode()) return;
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json,application/json';
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      try {
        const text = await file.text();
        const data = JSON.parse(text);
        const widgets: WidgetData[] = Array.isArray(data.widgets) ? data.widgets : (Array.isArray(data) ? data : null);
        if (!widgets) throw new Error('Invalid format: expected { widgets: [...] }');
        this.widgets = widgets;
        if (this.mode === 'edit') this.dirty = true;
        this.rerender();
        await this.initGrid();
      } catch (err) {
        console.error('Failed to import dashboard:', err);
        void showAlert(`Import failed: ${(err as Error).message}`, {
          title: 'Import failed',
          tone: 'danger',
        });
      }
    };
    input.click();
  };

  toggleEditMode(): void {
    void this.setDashboardMode(this.mode === 'edit' ? 'view' : 'edit');
  }

  toggleInspectMode(): void {
    void this.setDashboardMode(this.mode === 'inspect' ? 'view' : 'inspect');
  }

  async setDashboardMode(mode: DashboardMode): Promise<void> {
    if (mode === 'edit' && !this.canEditDashboard) return;
    if (mode === 'inspect' && !this.canInspectDashboard) return;
    if (this.mode === mode) return;

    if (this.mode === 'edit' && mode !== 'edit' && this.dirty) {
      const choice = await showChoice('Save changes before leaving edit mode?', {
        title: 'Unsaved changes',
        choices: [
          { value: 'keep', label: 'Keep editing', role: 'secondary' },
          { value: 'discard', label: 'Discard changes', role: 'danger' },
          { value: 'save', label: 'Save changes', role: 'primary' },
        ],
      });
      if (choice === 'keep') return;
      if (choice === 'save') {
        const saved = await this.handleSave();
        if (!saved) return;
      } else if (choice === 'discard') {
        this.widgets = cloneWidgets(this.savedWidgets);
        if (this.dashboardData) this.dashboardData.widgets = cloneWidgets(this.savedWidgets) as any;
        this.dirty = false;
        this.rerender();
        await this.initGrid();
      }
    }

    this.mode = mode;

    if (this.grid) {
      this.grid.enableResize(this.isInteractiveMode());
      this.grid.enableMove(this.isInteractiveMode());
    }

    // Update all widget cards and forward dashboard mode to widgets that support it.
    this.querySelectorAll('.grid-stack-item-content > widget-card').forEach(card => {
      (card as WidgetCard).setMode(this.mode);
      const body = card.querySelector('.widget-body');
      if (body) {
        const widgetEl = body.firstElementChild as any;
        if (widgetEl && typeof widgetEl.setDashboardMode === 'function') {
          widgetEl.setDashboardMode(this.mode);
        } else if (widgetEl && typeof widgetEl.setEditMode === 'function') {
          widgetEl.setEditMode(this.isInteractiveMode());
        }
      }
    });

    // Toggle whether the grid accepts external drops from the toolbar
    if (this.grid) this.grid.updateOptions({ acceptWidgets: this.isInteractiveMode() });

    // Add/remove the toolbar without destroying the grid or recreating widgets
    this.syncToolbar();

    // Notify parent so the header can update its menu label
    this.emitModeChanged();
  }

  private emitCapabilities(): void {
    this.emit('dashboard-capabilities-changed', {
      canEdit: this.canEditDashboard,
      canInspect: this.canInspectDashboard,
    });
  }

  private emitModeChanged(): void {
    this.emit('dashboard-mode-changed', { mode: this.mode });
    this.emit('edit-mode-changed', { editing: this.mode === 'edit' });
  }

  private handleCategoryClick = (e: Event): void => {
    e.stopPropagation();
    const btn = e.currentTarget as HTMLElement;
    const cat = btn.dataset.category;
    if (!cat) return;

    const dropdown = this.querySelector(`[data-dropdown="${cat}"]`) as HTMLElement | null;
    if (!dropdown) return;

    // Close all other dropdowns
    this.querySelectorAll('.widget-category-dropdown').forEach(el => {
      if (el !== dropdown) el.classList.add('hidden');
    });

    dropdown.classList.toggle('hidden');
  };

  private handleOutsideClick = (e: Event): void => {
    const target = e.target as HTMLElement;
    if (!target.closest('.widget-category-menu')) {
      this.querySelectorAll('.widget-category-dropdown').forEach(el =>
        el.classList.add('hidden')
      );
    }
  };

  private rerender(): void {
    if (this.grid) {
      this.grid.destroy(false);
      this.grid = null;
    }
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  protected attachEventListeners(): void {
    this.querySelector('#pc-export')?.addEventListener('click', this.handleExport);
    this.querySelector('#pc-import')?.addEventListener('click', this.handleImport);
    this.querySelectorAll('.widget-category-btn').forEach(el =>
      el.addEventListener('click', this.handleCategoryClick)
    );
    if (this.isInteractiveMode()) this.updateSaveButton();
    document.addEventListener('click', this.handleOutsideClick);

    const dialog = this.querySelector('#pc-properties-dialog');
    if (dialog) {
      dialog.addEventListener('properties-updated', this.handlePropertiesUpdated as EventListener);
    }

    this.addEventListener('widget-config-save', this.handleWidgetConfigSave as EventListener);

    // Warn on browser navigation with unsaved changes
    this.beforeUnloadHandler = (e: BeforeUnloadEvent) => {
      if (this.dirty) {
        e.preventDefault();
      }
    };
    window.addEventListener('beforeunload', this.beforeUnloadHandler);
  }

  protected detachEventListeners(): void {
    this.querySelector('#pc-save')?.removeEventListener('click', this.handleSave);
    this.querySelector('#pc-export')?.removeEventListener('click', this.handleExport);
    this.querySelector('#pc-import')?.removeEventListener('click', this.handleImport);
    this.querySelectorAll('.widget-category-btn').forEach(el =>
      el.removeEventListener('click', this.handleCategoryClick)
    );
    document.removeEventListener('click', this.handleOutsideClick);

    const dialog = this.querySelector('#pc-properties-dialog');
    if (dialog) {
      dialog.removeEventListener('properties-updated', this.handlePropertiesUpdated as EventListener);
    }

    this.removeEventListener('widget-config-save', this.handleWidgetConfigSave as EventListener);

    if (this.beforeUnloadHandler) {
      window.removeEventListener('beforeunload', this.beforeUnloadHandler);
      this.beforeUnloadHandler = null;
    }
  }

  disconnectedCallback(): void {
    if (this.grid) {
      this.grid.destroy(false);
      this.grid = null;
    }
    super.disconnectedCallback();
  }
}

function generateId(): string {
  return Math.random().toString(36).substring(2, 10);
}

function escapeHtml(value: string): string {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function cloneConfig(config: Record<string, any>): Record<string, any> {
  if (typeof structuredClone === 'function') return structuredClone(config);
  return JSON.parse(JSON.stringify(config));
}

function cloneWidgets(widgets: WidgetData[]): WidgetData[] {
  if (typeof structuredClone === 'function') return structuredClone(widgets);
  return JSON.parse(JSON.stringify(widgets));
}

customElements.define('dashboard-container', DashboardContainer);
