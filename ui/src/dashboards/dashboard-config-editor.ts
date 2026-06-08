import { BaseComponent } from '../components/base-component';
import { registerPermissions } from '../permissions/registry';
import { can } from '../permissions/permissions';
import { getIconSVG, preloadIconSet, loadIconSet } from '../utils/icons';
import { showConfirm } from '../components/app-dialog';
import '../components/icon-picker';

registerPermissions('dashboards-setup', 'Dashboards Config Editor', [
  { name: 'read', description: 'View dashboard configuration' },
  { name: 'edit', description: 'Edit dashboard layout and structure' },
], 'Controls access to the Dashboards configuration editor in the sidebar - where dashboards are created, renamed, and arranged.');

export interface DashboardConfig {
  id: string;
  serverId?: number;      // numeric DB id assigned by the server; undefined for unsaved dashboards
  name: string;
  description: string;
  icon: string;
  dashboardTag: string;       // custom element tag name (empty for categories)
  variation: string;
  deviceType: string;
  permission: string;     // required permission key to view this dashboard (e.g. 'site-a')
  widgets?: any[];
  children?: DashboardConfig[];  // if present, this is a category
}

export const STARTER_HELP_WIDGETS = [
  { id: 'help-manual', type: 'manual-widget', x: 0, y: 0, w: 24, h: 28, config: {} },
];

export const STARTER_TAG_VIEW_WIDGETS = [
  { id: 'tag-browser', type: 'tags-manager-widget', x: 0, y: 0, w: 24, h: 28, config: {} },
];

function cloneStarterWidgets(widgets: any[]): any[] {
  return widgets.map(widget => ({
    ...widget,
    config: { ...(widget.config ?? {}) },
  }));
}

export function createStarterHelpWidgets(): any[] {
  return cloneStarterWidgets(STARTER_HELP_WIDGETS);
}

export function createStarterTagViewWidgets(): any[] {
  return cloneStarterWidgets(STARTER_TAG_VIEW_WIDGETS);
}

// Flatten the tree into ordered rows for display
interface FlatRow {
  config: DashboardConfig;
  parentId: string | null;
  depth: number;
}

// Register a dashboard's permission in the registry so it appears in permissions-widget.
function registerDashboardPermission(permissionKey: string): void {
  if (!permissionKey) return;
  registerPermissions(permissionKey, `Sidebar Dashboard: ${permissionKey}`, [
    { name: 'view', description: 'View dashboard in sidebar' },
  ], `Controls whether this role can see and navigate to the '${permissionKey}' dashboard in the sidebar.`);
}

export class DashboardConfigEditor extends BaseComponent {
  private configs: DashboardConfig[] = [
    {
      id: 'dashboard',
      name: 'DASHBOARD',
      description: 'XACT help manual',
      icon: 'mdi:view-dashboard',
      dashboardTag: '',
      variation: '',
      deviceType: '',
      permission: '',
      widgets: createStarterHelpWidgets(),
    },
    {
      id: 'monitoring',
      name: 'MONTORING',
      description: 'Monitoring tools',
      icon: 'mdi:monitor-dashboard',
      dashboardTag: '',
      variation: '',
      deviceType: '',
      permission: '',
      children: [
        {
          id: 'tag-view',
          name: 'Tag View',
          description: 'Browse and monitor tags',
          icon: 'mdi:tag-multiple',
          dashboardTag: 'tag-view-dashboard',
          variation: '',
          deviceType: '',
          permission: '',
          widgets: createStarterTagViewWidgets(),
        },
      ],
    },
  ];

  private editingId: string | null = null;
  private editForm: Partial<DashboardConfig> = {};
  private permissionsChecked = false;
  private hasPermission = false;
  private canEdit = false;
  private visibleIds: Set<string> = new Set();

  // Modal lives in document.body to escape GridStack's CSS transforms
  private modalEl: HTMLElement | null = null;

  connectedCallback(): void {
    super.connectedCallback();
    preloadIconSet('mdi');
    this.initPermissions();
  }

  disconnectedCallback(): void {
    this.destroyModal();
  }

  private async initPermissions(): Promise<void> {
    this.hasPermission = await can('dashboards-setup.read');
    this.canEdit = await can('dashboards-setup.edit');
    this.permissionsChecked = true;
    if (!this.hasPermission) {
      this.detachEventListeners();
      this.innerHTML = `<div class="p-4 text-sm opacity-60">You do not have permission to view dashboard configuration.</div>`;
      return;
    }
    await this.computeVisibleIds();
    this.rerender();
  }

  private async computeVisibleIds(): Promise<void> {
    this.visibleIds = new Set();
    const allRows = this.flattenConfigs();
    await Promise.all(allRows.map(async (row) => {
      const perm = row.config.permission;
      if (!perm || await can(`${perm}.view`)) {
        this.visibleIds.add(row.config.id);
      }
    }));
  }

  protected render(): void {
    if (!this.permissionsChecked) {
      this.innerHTML = `<div class="p-4 text-sm opacity-60">Loading...</div>`;
      return;
    }

    const rows = this.flattenConfigs().filter(r => this.visibleIds.has(r.config.id));

    this.innerHTML = `
      <div class="space-y-4">
        ${this.canEdit ? `
        <div class="flex items-center justify-end">
          <div class="flex gap-2">
            <button id="add-dashboard-btn" class="px-3 py-1.5 text-sm rounded-lg transition-colors hover:opacity-90"
                    style="background-color: var(--accent-color); color: var(--accent-text);">+ Dashboard</button>
            <button id="add-category-btn" class="px-3 py-1.5 text-sm rounded-lg border transition-colors hover:opacity-80"
                    style="border-color: var(--border-color);">+ Category</button>
          </div>
        </div>` : ''}

        <div class="border rounded-lg overflow-hidden" style="border-color: var(--border-color);">
          <table class="w-full text-sm">
            <thead>
              <tr style="background-color: var(--sidebar-bg);">
                <th class="px-4 py-2.5 text-left font-medium text-xs uppercase tracking-wider opacity-60 w-10"></th>
                <th class="px-4 py-2.5 text-left font-medium text-xs uppercase tracking-wider opacity-60">Name</th>
                <th class="px-4 py-2.5 text-left font-medium text-xs uppercase tracking-wider opacity-60 hidden md:table-cell">Description</th>
                <th class="px-4 py-2.5 text-left font-medium text-xs uppercase tracking-wider opacity-60 hidden sm:table-cell">Type</th>
                <th class="px-4 py-2.5 text-right font-medium text-xs uppercase tracking-wider opacity-60 w-32">Actions</th>
              </tr>
            </thead>
            <tbody>
              ${rows.length === 0 ? `
                <tr><td colspan="5" class="px-4 py-8 text-center opacity-40">No dashboards configured. Add a dashboard or category to get started.</td></tr>
              ` : rows.map((row, index) => this.renderRow(row, index, rows.length)).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  private renderRow(row: FlatRow, index: number, total: number): string {
    const c = row.config;
    const isCategory = !!c.children;
    const indent = row.depth * 24;

    return `
      <tr class="border-t transition-colors hover:opacity-90 ${isCategory ? '' : 'cursor-pointer'}" style="border-color: var(--border-color);"
          ${isCategory ? '' : `data-dashboard-name="${c.name}" data-dashboard-id="${c.serverId ?? ''}"`}>
        <td class="px-4 py-2.5">
          ${this.canEdit ? `
          <div class="flex flex-col gap-0.5">
            <button class="move-btn text-xs opacity-40 hover:opacity-100 ${index === 0 ? 'invisible' : ''}" data-id="${c.id}" data-dir="up" title="Move up">&#9650;</button>
            <button class="move-btn text-xs opacity-40 hover:opacity-100 ${index === total - 1 ? 'invisible' : ''}" data-id="${c.id}" data-dir="down" title="Move down">&#9660;</button>
          </div>
          ` : ''}
        </td>
        <td class="px-4 py-2.5">
          <div class="flex items-center gap-2" style="padding-left: ${indent}px;">
            ${(() => {
              const ico = c.icon || (isCategory ? 'mdi:folder' : 'mdi:file-document');
              const svg = getIconSVG(ico, 'var(--accent-color)', 18);
              return svg ? `<span style="display:inline-flex;align-items:center;">${svg}</span>` : '';
            })()}
            <span class="${isCategory ? 'font-semibold uppercase text-xs tracking-wider' : 'font-medium'}">${c.name}</span>
            ${c.permission ? `<span class="text-xs px-1.5 py-0.5 rounded opacity-60" style="background-color: color-mix(in srgb, var(--border-color) 60%, transparent);">🔒 ${c.permission}</span>` : ''}
          </div>
        </td>
        <td class="px-4 py-2.5 hidden md:table-cell">
          <span class="opacity-60 text-xs">${c.description || '-'}</span>
        </td>
        <td class="px-4 py-2.5 hidden sm:table-cell">
          <span class="px-2 py-0.5 text-xs rounded-full" style="background-color: color-mix(in srgb, var(--accent-color) 15%, transparent); color: var(--accent-color);">
            ${isCategory ? 'Category' : 'Dashboard'}
          </span>
        </td>
        <td class="px-4 py-2.5 text-right">
          ${this.canEdit ? `
          <div class="flex items-center justify-end gap-1">
            <button class="edit-btn p-1.5 rounded hover:opacity-70 transition-opacity" data-id="${c.id}" title="Edit">
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
            </button>
            <button class="delete-btn p-1.5 rounded hover:opacity-70 transition-opacity" style="color: #ef4444;" data-id="${c.id}" title="Delete">
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
            </button>
          </div>
          ` : ''}
        </td>
      </tr>
    `;
  }

  private renderModalContent(): string {
    const f = this.editForm;
    const isCategory = f.children !== undefined;

    return `
      <div id="modal-backdrop" style="position:fixed;inset:0;background:rgba(0,0,0,0.6);z-index:9999;display:flex;align-items:center;justify-content:center;padding:1rem;">
        <div class="border rounded-lg p-5 w-full max-w-xl overflow-y-auto" style="max-height:90vh;border-color:var(--accent-color);background-color:var(--content-bg);color:var(--content-text);box-shadow:0 25px 50px -12px rgba(0,0,0,0.5);">
          <div class="flex items-center justify-between mb-4">
            <h3 class="text-sm font-semibold">${this.editingId ? 'Edit' : 'New'} ${isCategory ? 'Category' : 'Dashboard'}: ${f.name || 'New'}</h3>
            <button id="modal-close" class="w-7 h-7 flex items-center justify-center rounded opacity-60 hover:opacity-100 text-xl leading-none" title="Close">&times;</button>
          </div>

          <div class="space-y-4">
            <div>
              <label class="block text-xs font-medium mb-1 opacity-60">Name</label>
              <input type="text" id="edit-name" value="${f.name || ''}"
                class="w-full px-3 py-2 text-sm border rounded-lg"
                style="background-color: var(--content-bg); border-color: var(--border-color); color: var(--content-text);">
            </div>

            <div>
              <label class="block text-xs font-medium mb-1 opacity-60">Icon</label>
              <icon-picker id="edit-icon-picker" value="${f.icon || 'mdi:file-document'}"></icon-picker>
            </div>

            <div>
              <label class="block text-xs font-medium mb-1 opacity-60">Description</label>
              <input type="text" id="edit-description" value="${f.description || ''}"
                class="w-full px-3 py-2 text-sm border rounded-lg"
                style="background-color: var(--content-bg); border-color: var(--border-color); color: var(--content-text);">
            </div>

            ${!isCategory ? `
              <div>
                <label class="block text-xs font-medium mb-1 opacity-60">Menu Category</label>
                <select id="edit-parent"
                  class="w-full px-3 py-2 text-sm border rounded-lg"
                  style="background-color: var(--content-bg); border-color: var(--border-color); color: var(--content-text);">
                  <option value="">Top Level</option>
                  ${this.getCategoryOptions(this.editingId!)}
                </select>
              </div>
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                  <label class="block text-xs font-medium mb-1 opacity-60">Device Type</label>
                  <input type="text" id="edit-deviceType" value="${f.deviceType || ''}"
                    class="w-full px-3 py-2 text-sm border rounded-lg"
                    style="background-color: var(--content-bg); border-color: var(--border-color); color: var(--content-text);">
                </div>
                <div>
                  <label class="block text-xs font-medium mb-1 opacity-60">Device Subtype</label>
                  <input type="text" id="edit-variation" value="${f.variation || ''}"
                    class="w-full px-3 py-2 text-sm border rounded-lg"
                    style="background-color: var(--content-bg); border-color: var(--border-color); color: var(--content-text);">
                </div>
              </div>
              <div>
                <label class="block text-xs font-medium mb-1 opacity-60">Permission</label>
                <input type="text" id="edit-permission" value="${f.permission || ''}"
                  placeholder="e.g. site-a-data (leave blank for all users)"
                  class="w-full px-3 py-2 text-sm border rounded-lg"
                  style="background-color: var(--content-bg); border-color: var(--border-color); color: var(--content-text);">
                <p class="mt-1 text-xs opacity-50">Restricts sidebar visibility. Creates a 'Sidebar Dashboard: &lt;key&gt;' entry in the Permissions manager.</p>
              </div>
            ` : ''}

            <div class="flex justify-end gap-2 pt-2">
              <button id="edit-cancel" class="px-4 py-2 text-sm rounded-lg border transition-colors hover:opacity-80"
                      style="border-color: var(--border-color);">Cancel</button>
              <button id="edit-save" class="px-4 py-2 text-sm rounded-lg transition-colors hover:opacity-90"
                      style="background-color: var(--accent-color); color: var(--accent-text);">Save</button>
            </div>
          </div>
        </div>
      </div>
    `;
  }

  // ─── Modal lifecycle (appended to document.body to escape GridStack transforms) ─

  private syncModal(): void {
    if (this.editingId) {
      if (!this.modalEl) {
        this.modalEl = document.createElement('div');
        document.body.appendChild(this.modalEl);
      }
      this.detachModalListeners();
      this.modalEl.innerHTML = this.renderModalContent();
      this.attachModalListeners();
    } else {
      this.destroyModal();
    }
  }

  private destroyModal(): void {
    if (this.modalEl) {
      this.detachModalListeners();
      this.modalEl.remove();
      this.modalEl = null;
    }
  }

  private attachModalListeners(): void {
    if (!this.modalEl) return;
    this.modalEl.querySelector('#edit-save')?.addEventListener('click', this.handleSave);
    this.modalEl.querySelector('#edit-cancel')?.addEventListener('click', this.handleCancelEdit);
    this.modalEl.querySelector('#modal-close')?.addEventListener('click', this.handleCancelEdit);
    this.modalEl.querySelector('#modal-backdrop')?.addEventListener('click', this.handleBackdropClick);
  }

  private detachModalListeners(): void {
    if (!this.modalEl) return;
    this.modalEl.querySelector('#edit-save')?.removeEventListener('click', this.handleSave);
    this.modalEl.querySelector('#edit-cancel')?.removeEventListener('click', this.handleCancelEdit);
    this.modalEl.querySelector('#modal-close')?.removeEventListener('click', this.handleCancelEdit);
    this.modalEl.querySelector('#modal-backdrop')?.removeEventListener('click', this.handleBackdropClick);
  }

  // ─────────────────────────────────────────────────────────────────────────────

  private getCategoryOptions(editingId: string): string {
    const found = this.findConfig(editingId);
    const currentParentId = found?.parent?.id || '';
    return this.configs
      .filter(c => c.children !== undefined)
      .map(c => `<option value="${c.id}" ${c.id === currentParentId ? 'selected' : ''}>${c.name}</option>`)
      .join('');
  }

  private flattenConfigs(): FlatRow[] {
    const rows: FlatRow[] = [];
    for (const config of this.configs) {
      rows.push({ config, parentId: null, depth: 0 });
      if (config.children) {
        for (const child of config.children) {
          rows.push({ config: child, parentId: config.id, depth: 1 });
        }
      }
    }
    return rows;
  }

  private findConfig(id: string): { config: DashboardConfig; parent: DashboardConfig | null; index: number } | null {
    for (let i = 0; i < this.configs.length; i++) {
      if (this.configs[i].id === id) {
        return { config: this.configs[i], parent: null, index: i };
      }
      const children = this.configs[i].children;
      if (children) {
        for (let j = 0; j < children.length; j++) {
          if (children[j].id === id) {
            return { config: children[j], parent: this.configs[i], index: j };
          }
        }
      }
    }
    return null;
  }

  private generateId(): string {
    return 'dashboard-' + Date.now().toString(36);
  }

  protected attachEventListeners(): void {
    this.querySelector('#add-dashboard-btn')?.addEventListener('click', this.handleAddDashboard);
    this.querySelector('#add-category-btn')?.addEventListener('click', this.handleAddCategory);
    this.querySelectorAll('.edit-btn').forEach(el => el.addEventListener('click', this.handleEdit));
    this.querySelectorAll('.delete-btn').forEach(el => el.addEventListener('click', this.handleDelete));
    this.querySelectorAll('.move-btn').forEach(el => el.addEventListener('click', this.handleMove));
    this.querySelectorAll('tr[data-dashboard-name]').forEach(el => el.addEventListener('click', this.handleRowClick));
  }

  protected detachEventListeners(): void {
    this.querySelector('#add-dashboard-btn')?.removeEventListener('click', this.handleAddDashboard);
    this.querySelector('#add-category-btn')?.removeEventListener('click', this.handleAddCategory);
    this.querySelectorAll('.edit-btn').forEach(el => el.removeEventListener('click', this.handleEdit));
    this.querySelectorAll('.delete-btn').forEach(el => el.removeEventListener('click', this.handleDelete));
    this.querySelectorAll('.move-btn').forEach(el => el.removeEventListener('click', this.handleMove));
    this.querySelectorAll('tr[data-dashboard-name]').forEach(el => el.removeEventListener('click', this.handleRowClick));
  }

  private handleAddDashboard = (): void => {
    const newDashboard: DashboardConfig = {
      id: this.generateId(),
      name: 'New Dashboard',
      description: '',
      icon: 'mdi:file-document',
      dashboardTag: '',
      variation: '',
      deviceType: '',
      permission: '',
    };
    this.configs.push(newDashboard);
    this.visibleIds.add(newDashboard.id);
    this.editingId = newDashboard.id;
    this.editForm = { ...newDashboard };
    this.rerender();
    this.emitConfigChange();
  };

  private handleAddCategory = (): void => {
    const newCat: DashboardConfig = {
      id: this.generateId(),
      name: 'New Category',
      description: '',
      icon: 'mdi:folder',
      dashboardTag: '',
      variation: '',
      deviceType: '',
      permission: '',
      children: [],
    };
    this.configs.push(newCat);
    this.visibleIds.add(newCat.id);
    this.editingId = newCat.id;
    this.editForm = { ...newCat };
    this.rerender();
    this.emitConfigChange();
  };

  private handleRowClick = (e: Event): void => {
    const target = e.target as HTMLElement;
    if (target.closest('button')) return;

    const row = (e.currentTarget as HTMLElement);
    const dashboardName = row.dataset.dashboardName;
    const dashboardId = row.dataset.dashboardId;
    const dashboardRef = dashboardId || dashboardName;
    if (dashboardRef) {
      this.emit('dashboard-open', { dashboard: dashboardRef, id: dashboardId });
    }
  };

  private handleEdit = (e: Event): void => {
    const id = (e.currentTarget as HTMLElement).dataset.id!;
    const found = this.findConfig(id);
    if (found) {
      this.editingId = id;
      this.editForm = { ...found.config };
      this.rerender();
    }
  };

  private handleDelete = async (e: Event): Promise<void> => {
    const id = (e.currentTarget as HTMLElement).dataset.id!;
    const found = this.findConfig(id);
    if (!found) return;

    const label = found.config.children ? 'category' : 'dashboard';
    const confirmed = await showConfirm(`Delete ${label} "${found.config.name}"?`, {
      title: `Delete ${label}`,
      confirmLabel: 'Delete',
      cancelLabel: 'Keep',
      tone: 'danger',
    });
    if (!confirmed) return;

    if (found.parent) {
      found.parent.children!.splice(found.index, 1);
    } else {
      this.configs.splice(found.index, 1);
    }
    this.visibleIds.delete(id);

    if (this.editingId === id) {
      this.editingId = null;
      this.editForm = {};
    }
    this.rerender();
    this.emitConfigChange();
  };

  private handleMove = (e: Event): void => {
    const btn = e.currentTarget as HTMLElement;
    const id = btn.dataset.id!;
    const dir = btn.dataset.dir as 'up' | 'down';

    const found = this.findConfig(id);
    if (!found) return;

    const list = found.parent ? found.parent.children! : this.configs;
    const idx = found.index;
    const targetIdx = dir === 'up' ? idx - 1 : idx + 1;

    if (targetIdx < 0 || targetIdx >= list.length) return;

    [list[idx], list[targetIdx]] = [list[targetIdx], list[idx]];
    this.rerender();
    this.emitConfigChange();
  };

  private handleSave = (): void => {
    if (!this.editingId || !this.modalEl) return;

    const name = (this.modalEl.querySelector('#edit-name') as HTMLInputElement)?.value || '';
    const icon = (this.modalEl.querySelector('#edit-icon-picker') as any)?.value || '';
    const description = (this.modalEl.querySelector('#edit-description') as HTMLInputElement)?.value || '';
    const variation = (this.modalEl.querySelector('#edit-variation') as HTMLInputElement)?.value || '';
    const deviceType = (this.modalEl.querySelector('#edit-deviceType') as HTMLInputElement)?.value || '';
    const permission = (this.modalEl.querySelector('#edit-permission') as HTMLInputElement)?.value.trim() || '';
    const newParentId = (this.modalEl.querySelector('#edit-parent') as HTMLSelectElement)?.value || '';

    const found = this.findConfig(this.editingId);
    if (found) {
      found.config.name = name;
      found.config.icon = icon;
      found.config.description = description;
      found.config.variation = variation;
      found.config.deviceType = deviceType;
      found.config.permission = permission;

      if (permission) {
        registerDashboardPermission(permission);
        this.visibleIds.add(found.config.id);
      }

      if (found.config.children === undefined) {
        const currentParentId = found.parent?.id || '';
        if (newParentId !== currentParentId) {
          if (found.parent) {
            found.parent.children!.splice(found.index, 1);
          } else {
            this.configs.splice(found.index, 1);
          }

          if (newParentId) {
            const newParent = this.configs.find(c => c.id === newParentId);
            if (newParent?.children) {
              newParent.children.push(found.config);
            }
          } else {
            this.configs.push(found.config);
          }
        }
      }
    }

    this.editingId = null;
    this.editForm = {};
    this.rerender();
    this.emitConfigChange();
  };

  private handleCancelEdit = (): void => {
    this.editingId = null;
    this.editForm = {};
    this.rerender();
  };

  private handleBackdropClick = (e: Event): void => {
    if ((e.target as HTMLElement).id === 'modal-backdrop') {
      this.handleCancelEdit();
    }
  };

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    this.syncModal();
  }

  private emitConfigChange(): void {
    this.emit('config-change', { configs: this.configs });
  }

  /** Get current dashboard configs (for sidebar to consume) */
  getConfigs(): DashboardConfig[] {
    return this.configs;
  }

  /** Set dashboard configs (e.g. loaded from server) */
  setConfigs(configs: DashboardConfig[]): void {
    this.configs = configs;
    this.editingId = null;
    this.editForm = {};
    for (const c of configs) {
      if (c.permission) registerDashboardPermission(c.permission);
      if (c.children) {
        for (const child of c.children) {
          if (child.permission) registerDashboardPermission(child.permission);
        }
      }
    }
    // Load icon sets used by dashboard configs so icons render correctly.
    this.loadConfigIconSets(configs);
    if (this.permissionsChecked) {
      this.computeVisibleIds().then(() => this.rerender());
    }
  }

  private loadConfigIconSets(configs: DashboardConfig[]): void {
    const prefixes = new Set<string>();
    for (const c of configs) {
      if (c.icon?.includes(':')) prefixes.add(c.icon.split(':')[0]);
      if (c.children) {
        for (const child of c.children) {
          if (child.icon?.includes(':')) prefixes.add(child.icon.split(':')[0]);
        }
      }
    }
    const loads = [...prefixes].map(p => loadIconSet(p));
    Promise.all(loads).then(() => this.rerender());
  }
}

customElements.define('dashboard-config-editor', DashboardConfigEditor);
