import { BaseComponent } from '../../components/base-component';
import { getTreeBrowserDialog } from '../../components/tree-browser-dialog';
import { getCurrentUser, switchOrgSession } from '../../auth';
import { can } from '../../permissions/permissions';
import {
  fetchMobileAppConfig, saveMobileAppConfig, listMyOrganisations, listDashboards,
} from '../../api';
import type { MobileAppConfig, OrganisationSummary } from '../../api';
import { registerPermissions } from '../../permissions/registry';
import { registerWidgetType } from './widget-registry';

registerPermissions('mobile-app', 'Mobile App Configuration', [
  { name: 'read', description: 'View mobile app configuration' },
  { name: 'write', description: 'Change mobile app configuration' },
], 'Controls organisation-specific mobile device tabs and the default dashboard.');

registerWidgetType({
  type: 'mobile-app-config-widget', name: 'Mobile App Configuration', icon: '📱',
  category: 'System', defaultW: 12, defaultH: 14, minW: 8, minH: 8,
});

export class MobileAppConfigWidget extends BaseComponent {
  private value: MobileAppConfig = { deviceParentNodes: [], defaultDashboardName: '' };
  private organisations: OrganisationSummary[] = [];
  private dashboardNames: string[] = [];
  private loading = true;
  private saving = false;
  private error = '';
  private saved = false;
  private canWrite = false;
  private isSystemAdmin = false;

  connectedCallback(): void {
    super.connectedCallback();
    void this.load();
  }

  private async load(): Promise<void> {
    const user = getCurrentUser();
    const currentOrg = user?.tenant_id ?? '';
    this.isSystemAdmin = user?.roles?.includes('SystemAdmin') ?? false;
    const [canRead, canWrite] = await Promise.all([can('mobile-app.read'), can('mobile-app.write')]);
    this.canWrite = canWrite;
    if (!canRead && !canWrite) {
      this.innerHTML = '<div class="p-8 text-center opacity-40 text-sm">Insufficient permissions</div>';
      return;
    }
    try {
      const [value, dashboards, organisations] = await Promise.all([
        fetchMobileAppConfig(),
        listDashboards(),
        this.isSystemAdmin
          ? listMyOrganisations().catch(() => currentOrg ? [{ name: currentOrg, displayName: currentOrg }] : [])
          : Promise.resolve([]),
      ]);
      this.value = value;
      this.dashboardNames = [...new Set(dashboards.filter(d => !d.isCategory).map(d => d.name))]
        .sort((a, b) => a.localeCompare(b));
      this.organisations = organisations.some(org => org.name === currentOrg) || !currentOrg
        ? organisations
        : [{ name: currentOrg, displayName: currentOrg }, ...organisations];
    } catch (error) {
      this.error = error instanceof Error ? error.message : 'Failed to load configuration';
    }
    this.loading = false;
    this.render();
  }

  protected render(): void {
    if (this.loading) {
      this.innerHTML = '<div class="p-8 text-center opacity-40 text-sm">Loading mobile app configuration…</div>';
      return;
    }
    const user = getCurrentUser();
    const disabled = this.canWrite && !this.saving ? '' : 'disabled';
    this.innerHTML = `
      <div class="flex flex-col h-full">
        <div class="px-5 py-4 border-b" style="border-color:var(--border-color)">
          <div class="text-sm font-semibold">Mobile App Configuration</div>
          <div class="text-xs opacity-50 mt-1">Settings are stored separately for each organisation.</div>
        </div>
        <div class="flex-1 overflow-auto p-5 space-y-6">
          ${this.isSystemAdmin ? `
            <label class="block text-xs font-medium">Organisation
              <select id="mac-org" class="mt-2 w-full rounded px-3 py-2 text-sm" style="background:var(--content-bg);border:1px solid var(--border-color)" ${this.saving ? 'disabled' : ''}>
                ${this.organisations.map(o => `<option value="${this.esc(o.name)}" ${o.name === user?.tenant_id ? 'selected' : ''}>${this.esc(o.displayName || o.name)}</option>`).join('')}
              </select>
            </label>` : `
            <div><div class="text-xs font-medium">Organisation</div><div class="mt-2 text-sm opacity-70">${this.esc(user?.tenant_id || '')}</div></div>`}

          <section>
            <div class="flex items-center justify-between gap-3">
              <div><div class="text-xs font-medium">Device parent nodes</div><div class="text-xs opacity-45 mt-1">Each parent becomes a device-type tab in the app.</div></div>
              ${this.canWrite ? `<button id="mac-add-parent" class="px-3 py-1.5 rounded text-xs font-medium" style="color:var(--accent-color);border:1px solid color-mix(in srgb,var(--accent-color) 35%,transparent)" ${disabled}>+ Add node</button>` : ''}
            </div>
            <div class="mt-3 flex flex-wrap gap-2">
              ${this.value.deviceParentNodes.length ? this.value.deviceParentNodes.map((path, index) => `
                <span class="inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-xs" style="background:color-mix(in srgb,var(--accent-color) 12%,transparent);color:var(--accent-color)">
                  ${this.esc(path)}
                  ${this.canWrite ? `<button class="mac-remove opacity-60 hover:opacity-100" data-index="${index}" ${disabled}>×</button>` : ''}
                </span>`).join('') : '<span class="text-xs opacity-35">No parent nodes configured. The app will show all discovered devices.</span>'}
            </div>
          </section>

          <label class="block text-xs font-medium">Default dashboard
            <select id="mac-dashboard" class="mt-2 w-full rounded px-3 py-2 text-sm" style="background:var(--content-bg);border:1px solid var(--border-color)" ${disabled}>
              <option value="">No default dashboard</option>
              ${this.value.defaultDashboardName && !this.dashboardNames.includes(this.value.defaultDashboardName)
                ? `<option value="${this.esc(this.value.defaultDashboardName)}" selected>${this.esc(this.value.defaultDashboardName)} (unavailable)</option>` : ''}
              ${this.dashboardNames.map(name => `<option value="${this.esc(name)}" ${name === this.value.defaultDashboardName ? 'selected' : ''}>${this.esc(name)}</option>`).join('')}
            </select>
          </label>
          ${this.error ? `<div class="text-xs text-red-400">${this.esc(this.error)}</div>` : ''}
          ${this.saved ? '<div class="text-xs text-green-400">Configuration saved.</div>' : ''}
        </div>
        <div class="px-5 py-4 border-t flex items-center justify-between" style="border-color:var(--border-color)">
          <span class="text-xs opacity-45">${this.canWrite ? 'Changes apply when the mobile app refreshes.' : 'Read only'}</span>
          ${this.canWrite ? `<button id="mac-save" class="px-4 py-2 rounded text-xs font-semibold" style="background:var(--accent-color);color:#081521" ${disabled}>${this.saving ? 'Saving…' : 'Save'}</button>` : ''}
        </div>
      </div>`;
    this.attachEventListeners();
  }

  protected attachEventListeners(): void {
    this.querySelector('#mac-org')?.addEventListener('change', async event => {
      const org = (event.target as HTMLSelectElement).value;
      if (!org || org === getCurrentUser()?.tenant_id) return;
      try {
        await switchOrgSession(org);
        window.location.reload();
      } catch (error) {
        this.error = error instanceof Error ? error.message : 'Failed to switch organisation';
        this.render();
      }
    });
    this.querySelector('#mac-add-parent')?.addEventListener('click', () => {
      getTreeBrowserDialog().open('', 'Add device parent node', path => {
        const org = getCurrentUser()?.tenant_id ?? '';
        const relative = path === org ? '' : path.replace(new RegExp(`^${this.escapeRegExp(org)}\\.`), '');
        if (relative && !this.value.deviceParentNodes.includes(relative)) this.value.deviceParentNodes.push(relative);
        this.render();
      });
    });
    this.querySelectorAll<HTMLButtonElement>('.mac-remove').forEach(button => button.addEventListener('click', () => {
      this.value.deviceParentNodes.splice(Number(button.dataset.index), 1);
      this.render();
    }));
    this.querySelector('#mac-save')?.addEventListener('click', () => void this.save());
  }

  protected detachEventListeners(): void {
    // Rendering replaces the subtree, which releases its event listeners.
  }

  private async save(): Promise<void> {
    this.value.defaultDashboardName = (this.querySelector('#mac-dashboard') as HTMLSelectElement)?.value.trim() ?? '';
    this.saving = true; this.error = ''; this.saved = false; this.render();
    try {
      this.value = await saveMobileAppConfig(this.value);
      this.saved = true;
    } catch (error) {
      this.error = error instanceof Error ? error.message : 'Failed to save configuration';
    }
    this.saving = false; this.render();
  }

  private esc(value: string): string {
    return value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]!));
  }

  private escapeRegExp(value: string): string { return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }
}

customElements.define('mobile-app-config-widget', MobileAppConfigWidget);
