import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { registerPermissions, getRegistry } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import { fetchAllRolePermissions, updateRolePermissions } from '../../api';
import type { RolePermissions } from '../../api';

registerPermissions('permissions', 'Role Permissions Manager', [
  { name: 'view', description: 'View role permissions' },
  { name: 'manage', description: 'Manage role permissions' },
], 'Controls access to this Permissions Manager widget - roles with view can inspect permissions; roles with manage can edit permissions for all other roles.');

registerWidgetType({
  type: 'permissions-widget',
  name: 'Permissions Manager',
  icon: '\uD83D\uDD10',
  category: 'System',
  defaultW: 12,
  defaultH: 20,
  minW: 8,
  minH: 10,
});

interface EditState {
  roles: RolePermissions[];
  dirty: Set<string>;
}

export class PermissionsWidget extends BaseComponent {
  private state: EditState = { roles: [], dirty: new Set() };
  private loading = true;
  private saving = false;
  private error = '';
  private canManage = false;

  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  private async initWithPermissions(): Promise<void> {
    const [canView, canManage] = await Promise.all([
      can('permissions.view'),
      can('permissions.manage'),
    ]);
    this.canManage = canManage;
    if (!canView && !canManage) {
      this.innerHTML = `<div class="p-4 text-sm opacity-60">You do not have permission to view roles.</div>`;
      return;
    }
    await this.loadData();
  }

  private async loadData(): Promise<void> {
    try {
      const roles = await fetchAllRolePermissions();
      this.state = { roles, dirty: new Set() };
      this.loading = false;
      if (this.canManage) await this.ensureSystemAdminFullAccess();
      this.rerender();
    } catch (err) {
      this.error = 'Failed to load permissions';
      this.loading = false;
      this.rerender();
    }
  }

  // Automatically grant SystemAdmin all permissions from the registry.
  // Runs silently after load - the column is never shown in the UI.
  private async ensureSystemAdminFullAccess(): Promise<void> {
    const registry = getRegistry();
    const sysAdmin = this.state.roles.find(r => r.role === 'SystemAdmin');
    if (!sysAdmin) return;

    let changed = false;
    for (const entry of registry) {
      if (!sysAdmin.ui[entry.resource]) {
        sysAdmin.ui[entry.resource] = {};
        changed = true;
      }
      for (const perm of entry.permissions) {
        if (sysAdmin.ui[entry.resource][perm.name] !== true) {
          sysAdmin.ui[entry.resource][perm.name] = true;
          changed = true;
        }
      }
    }

    if (changed) {
      try {
        await updateRolePermissions('SystemAdmin', { ui: sysAdmin.ui, server: sysAdmin.server });
      } catch (err) {
        console.error('Failed to auto-grant SystemAdmin permissions:', err);
      }
    }
  }

  protected render(): void {
    if (this.loading) {
      this.innerHTML = `<div class="p-4 text-sm opacity-60">Loading permissions...</div>`;
      return;
    }

    if (this.error) {
      this.innerHTML = `<div class="p-4 text-sm" style="color: #ef4444;">${this.error}</div>`;
      return;
    }

    const registry = getRegistry();
    // SystemAdmin is managed automatically - never shown in the UI
    const roles = this.state.roles.filter(r => r.role !== 'SystemAdmin');

    // Collect all resource+action pairs from both registry and server data
    // Also build description lookups for resources and actions
    const actionDescriptions = new Map<string, string>();
    const resourceDescriptions = new Map<string, string>();
    const resourceTooltips = new Map<string, string>();
    const resourceActions = new Map<string, Set<string>>();
    for (const entry of registry) {
      const actions = new Set<string>();
      if (entry.description) {
        resourceDescriptions.set(entry.resource, entry.description);
      }
      if (entry.tooltip) {
        resourceTooltips.set(entry.resource, entry.tooltip);
      }
      for (const p of entry.permissions) {
        actions.add(p.name);
        if (p.description) {
          actionDescriptions.set(`${entry.resource}.${p.name}`, p.description);
        }
      }
      resourceActions.set(entry.resource, actions);
    }
    for (const role of roles) {
      for (const [resource, actions] of Object.entries(role.ui)) {
        if (!resourceActions.has(resource)) {
          resourceActions.set(resource, new Set());
        }
        const existing = resourceActions.get(resource)!;
        for (const action of Object.keys(actions)) {
          existing.add(action);
        }
      }
    }

    const resources = Array.from(resourceActions.entries()).sort((a, b) => a[0].localeCompare(b[0]));

    this.innerHTML = `
      <div class="flex flex-col h-full overflow-hidden text-sm">
        <div class="flex items-center justify-between px-3 py-2 border-b" style="border-color: var(--border-color);">
          <span class="font-medium">Role Permissions</span>
          ${this.canManage ? `<button id="perm-save" class="px-3 py-1 text-xs rounded-lg transition-colors hover:opacity-90 ${this.state.dirty.size === 0 ? 'opacity-40 cursor-not-allowed' : ''}"
                  style="background-color: var(--accent-color); color: var(--accent-text);" ${this.state.dirty.size === 0 ? 'disabled' : ''}>
            ${this.saving ? 'Saving...' : 'Save'}
          </button>` : `<span class="text-xs opacity-50">Read only</span>`}
        </div>

        <div class="flex-1 overflow-auto">
          <table class="w-full text-xs">
            <thead class="sticky top-0" style="background-color: var(--sidebar-bg);">
              <tr>
                <th class="px-3 py-2 text-left font-medium uppercase tracking-wider opacity-60">Resource</th>
                <th class="px-3 py-2 text-left font-medium uppercase tracking-wider opacity-60">Action</th>
                ${roles.map(r => `<th class="px-2 py-2 text-center font-medium uppercase tracking-wider opacity-60">${r.role}</th>`).join('')}
              </tr>
            </thead>
            <tbody>
              ${resources.map(([resource, actions]) => {
                const sortedActions = Array.from(actions).sort();
                return sortedActions.map((action, i) => {
                  const borderStyle = i === 0
                    ? 'border-top: 3px solid color-mix(in srgb, var(--border-color) 88%, var(--content-text));'
                    : 'border-top: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);';
                  const groupEndStyle = i === sortedActions.length - 1
                    ? 'border-bottom: 2px solid color-mix(in srgb, var(--border-color) 92%, var(--content-text));'
                    : '';
                  const cellBorderStyle = `${borderStyle}${groupEndStyle}`;
                  return `
                  <tr>
                    ${i === 0 ? (() => {
                      const label = resourceDescriptions.get(resource) || resource;
                      const tip = resourceTooltips.get(resource) || resource;
                      return `<td class="px-3 py-1.5 font-medium" rowspan="${sortedActions.length}" style="${cellBorderStyle}">
                        <span title="${tip}" style="cursor:help;border-bottom:1px dashed currentColor;opacity:0.9">${label}</span>
                      </td>`;
                    })() : ''}
                    <td class="px-3 py-1.5 opacity-70 cursor-default" style="${cellBorderStyle}" ${actionDescriptions.has(`${resource}.${action}`) ? `title="${actionDescriptions.get(`${resource}.${action}`)}"` : ''}>${action}</td>
                    ${roles.map(r => {
                      const checked = r.ui[resource]?.[action] === true;
                      return `<td class="px-2 py-1.5 text-center" style="${cellBorderStyle}">
                        <input type="checkbox" class="perm-check" data-role="${r.role}" data-resource="${resource}" data-action="${action}"
                               ${checked ? 'checked' : ''} ${this.canManage ? '' : 'disabled'}>
                      </td>`;
                    }).join('')}
                  </tr>
                `}).join('');
              }).join('')}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    this.querySelectorAll('.perm-check').forEach(el =>
      el.addEventListener('change', this.handleCheckChange)
    );
    if (this.canManage) this.querySelector('#perm-save')?.addEventListener('click', this.handleSave);
  }

  protected detachEventListeners(): void {
    this.querySelectorAll('.perm-check').forEach(el =>
      el.removeEventListener('change', this.handleCheckChange)
    );
    if (this.canManage) this.querySelector('#perm-save')?.removeEventListener('click', this.handleSave);
  }

  private handleCheckChange = (e: Event): void => {
    if (!this.canManage) return;
    const input = e.target as HTMLInputElement;
    const role = input.dataset.role!;
    const resource = input.dataset.resource!;
    const action = input.dataset.action!;

    const roleData = this.state.roles.find(r => r.role === role);
    if (!roleData) return;

    if (!roleData.ui[resource]) {
      roleData.ui[resource] = {};
    }
    roleData.ui[resource][action] = input.checked;
    this.state.dirty.add(role);

    // Update save button state
    const saveBtn = this.querySelector('#perm-save') as HTMLButtonElement;
    if (saveBtn) {
      saveBtn.disabled = false;
      saveBtn.classList.remove('opacity-40', 'cursor-not-allowed');
    }
  };

  private handleSave = async (): Promise<void> => {
    if (!this.canManage) return;
    if (this.state.dirty.size === 0 || this.saving) return;

    this.saving = true;
    this.rerender();

    try {
      for (const roleName of this.state.dirty) {
        const roleData = this.state.roles.find(r => r.role === roleName);
        if (roleData) {
          await updateRolePermissions(roleName, { ui: roleData.ui, server: roleData.server });
        }
      }
      this.state.dirty.clear();
    } catch (err) {
      console.error('Failed to save permissions:', err);
      this.error = 'Failed to save. Please try again.';
    }

    this.saving = false;
    this.rerender();
  };

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  setConfig(_config: Record<string, any>): void {
    // No configuration needed
  }
}

customElements.define('permissions-widget', PermissionsWidget);
