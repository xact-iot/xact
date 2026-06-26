import { BaseComponent } from './base-component';
import { can, getPermissions } from '../permissions/permissions';
import { getCurrentUser, getAuthHeaders, isAuthenticated, switchOrg } from '../auth';
import { getIconSVG, loadIconSet } from '../utils/icons';

export interface MenuItem {
  id: string;
  label: string;
  icon?: string;
  dashboard?: string;
  dashboardAliases?: string[];
  permission?: string;   // required permission key, e.g. 'site-a' → checks 'site-a.view'
  children?: MenuItem[];
}

interface OrgSummary {
  name: string;
  displayName?: string;
  logo?: string;
  favicon?: string;
}

export class AppSidebar extends BaseComponent {
  private menuItems: MenuItem[] = [];
  private filteredItems: MenuItem[] = [];

  private activeItem: string = 'dashboard-config';
  private expandedCategories: Set<string> = new Set(['engineer']);

  private currentOrg: string = '';
  private allowedOrgs: string[] = [];
  private orgDetails: Map<string, OrgSummary> = new Map();
  private orgDropdownOpen: boolean = false;
  private canViewDashboards: boolean = false;

  connectedCallback(): void {
    // Read user data before the first render so the org badge is correct
    // for sessions where the token already exists in localStorage.
    const user = getCurrentUser();
    if (user) {
      this.currentOrg = user.tenant_id ?? '';
      this.allowedOrgs = user.allowed_orgs ?? [];
      this.orgDetails = new Map(this.allowedOrgs.map(name => [name, { name }]));
    }
    super.connectedCallback();
    this.refreshAuthState();
  }

  async refreshAuthState(): Promise<void> {
    if (!isAuthenticated()) {
      this.currentOrg = '';
      this.allowedOrgs = [];
      this.orgDetails = new Map();
      this.filteredItems = [];
      this.canViewDashboards = false;
      this.rerender();
      return;
    }

    this.filteredItems = await this.filterByPermission(this.menuItems);
    // Show the button unless dashboards-setup has been explicitly configured
    // AND the user's role doesn't have read access. If the permission hasn't
    // been configured yet (new install), default to showing it.
    const hasDashboardPerm = await can('dashboards-setup.read');
    const dashboardConfigured = 'dashboards-setup' in getPermissions();
    this.canViewDashboards = hasDashboardPerm || !dashboardConfigured;
    await this.loadAllowedOrgs();
    // Re-read the current user: if the component connected before login completed
    // (the login-page flow), currentOrg would still be empty at this point.
    const user = getCurrentUser();
    if (user) {
      this.currentOrg = user.tenant_id ?? '';
    }
    this.applyCurrentBranding();
    this.rerender();
  }

  private async loadAllowedOrgs(): Promise<void> {
    try {
      const response = await fetch('/xact/api/v1/auth/my-orgs', {
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        const data = await response.json();
        const orgs = data.orgs ?? [];
        this.orgDetails = new Map();
        this.allowedOrgs = orgs.map((org: string | OrgSummary) => {
          if (typeof org === 'string') {
            this.orgDetails.set(org, { name: org });
            return org;
          }
          this.orgDetails.set(org.name, org);
          return org.name;
        });
      }
    } catch {
      // keep whatever was loaded from localStorage
    }
  }

  private async filterByPermission(items: MenuItem[]): Promise<MenuItem[]> {
    const results: MenuItem[] = [];
    for (const item of items) {
      if (item.children) {
        // For categories, filter their children
        const visibleChildren = await this.filterByPermission(item.children);
        if (visibleChildren.length > 0) {
          results.push({ ...item, children: visibleChildren });
        }
      } else {
        // For leaf items, check permission
        if (!item.permission || await can(`${item.permission}.view`)) {
          results.push(item);
        }
      }
    }
    return results;
  }

  protected render(): void {
    this.style.cssText = 'grid-area: sidebar;';
    this.style.backgroundColor = 'var(--sidebar-bg)';
    this.style.color = 'var(--sidebar-text)';
    this.style.borderColor = 'var(--border-color)';
    this.className = 'flex flex-col border-r overflow-hidden';

    const multiOrg = this.allowedOrgs.length > 1;
    const currentDetails = this.orgDetails.get(this.currentOrg);
    const orgLabel = currentDetails?.displayName || this.currentOrg || 'XACT';
    const logoSrc = currentDetails?.logo || '/xact/logo.svg';

    this.innerHTML = `
      <div class="flex flex-col border-b shrink-0" style="border-color: var(--border-color);">
        <div class="h-[88px] flex items-center gap-3 px-4 shrink-0">
          <img src="${this.escapeHTML(logoSrc)}" alt="${this.escapeHTML(orgLabel)}" class="w-16 h-16 flex-shrink-0 object-contain">
          <h1 class="text-xl font-bold truncate sidebar-label" style="color: var(--accent-color);">${this.escapeHTML(orgLabel)}</h1>
        </div>
        ${this.currentOrg ? `
        <div class="relative px-3 pb-3 sidebar-label">
          <button id="org-btn"
                  class="w-full flex items-center justify-between gap-2 px-3 py-1.5 rounded-md text-xs font-mono transition-colors"
                  style="background-color: color-mix(in srgb, var(--accent-color) 10%, transparent); color: var(--accent-color); border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);"
                  ${multiOrg ? '' : 'disabled'}>
            <span class="truncate uppercase tracking-widest">&nbsp;</span>
            ${multiOrg ? `
            <svg class="w-3 h-3 flex-shrink-0 transition-transform ${this.orgDropdownOpen ? 'rotate-180' : ''}"
                 fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
            </svg>` : ''}
          </button>
          ${multiOrg ? `
          <div id="org-dropdown" class="${this.orgDropdownOpen ? '' : 'hidden'} absolute left-3 right-3 top-full z-50 rounded-md py-1 mt-1"
               style="background: color-mix(in srgb, var(--sidebar-bg) 76%, var(--content-bg)); border: 2px solid color-mix(in srgb, var(--accent-color) 55%, var(--border-color)); box-shadow: 0 14px 28px rgba(0,0,0,0.45), 0 0 0 1px color-mix(in srgb, var(--accent-color) 18%, transparent) inset;">
            <div class="px-3 py-1.5 text-xs font-mono uppercase tracking-widest opacity-40"
                 style="border-bottom: 1px solid color-mix(in srgb, var(--accent-color) 20%, var(--border-color));">&nbsp;</div>
            ${this.allowedOrgs.map(org => `
            <button class="org-option w-full text-left px-3 py-1.5 text-xs font-mono uppercase tracking-widest transition-colors hover:opacity-80 ${org === this.currentOrg ? 'font-bold' : ''}"
                    style="${org === this.currentOrg ? 'color: var(--accent-color); background: color-mix(in srgb, var(--accent-color) 12%, transparent);' : ''}"
                    data-org="${org}">
              ${org === this.currentOrg ? '&#x25CF; ' : '&#x25CB; '}${this.escapeHTML(this.orgDetails.get(org)?.displayName || org)}
            </button>`).join('')}
          </div>` : ''}
        </div>` : ''}
      </div>

      <nav class="flex-1 overflow-y-auto py-3">
        ${this.renderItems(this.filteredItems)}
      </nav>

      ${this.canViewDashboards ? `
      <div class="shrink-0 border-t" style="border-color: var(--border-color);">
        <button class="menu-item w-full flex items-center gap-3 px-4 py-2 text-left text-xs font-bold uppercase tracking-wider transition-colors duration-150 hover:opacity-90 ${this.activeItem === 'dashboard-config' ? 'font-medium' : ''}"
                style="${this.activeItem === 'dashboard-config'
                  ? this.topLevelItemStyle(true)
                  : this.topLevelItemStyle()}"
                data-dashboard="dashboard-config-editor"
                data-id="dashboard-config">
          <span class="text-base">&#9881;</span>
          <span class="truncate sidebar-label">Dashboards</span>
        </button>
      </div>` : ''}
    `;
  }

  private renderItems(items: MenuItem[]): string {
    return items.map(item => {
      if (item.children) {
        return this.renderCategory(item);
      }
      return this.renderLink(item, false);
    }).join('');
  }

  private renderCategory(item: MenuItem): string {
    const expanded = this.expandedCategories.has(item.id);
    return `
      <div class="mt-2">
        <button class="category-toggle w-full flex items-center justify-between px-4 py-2 text-left text-xs font-bold uppercase tracking-wider transition-colors hover:opacity-90"
                style="${this.topLevelItemStyle()}"
                data-category="${item.id}">
          <span class="flex items-center gap-2">
            ${item.icon ? (() => { const svg = getIconSVG(item.icon, 'currentColor', 16); return svg ? `<span style="display:inline-flex;align-items:center;">${svg}</span>` : ''; })() : ''}
            <span class="sidebar-label">${item.label}</span>
          </span>
          <span class="sidebar-label text-[10px] transform transition-transform ${expanded ? 'rotate-90' : ''}">&#9654;</span>
        </button>
        <div class="${expanded ? '' : 'hidden'}">
          ${item.children!.map(child => this.renderLink(child, true)).join('')}
        </div>
      </div>
    `;
  }

  private renderLink(item: MenuItem, indented: boolean): string {
    const active = this.isItemActive(item);
    const buttonClass = indented
      ? `w-full flex items-center gap-3 pl-8 pr-4 py-2.5 text-left text-sm transition-colors duration-150 ${active ? 'font-medium' : ''}`
      : `w-full flex items-center gap-3 px-4 py-2 text-left text-xs font-bold uppercase tracking-wider transition-colors duration-150 hover:opacity-90 ${active ? 'font-medium' : ''}`;
    const buttonStyle = active
      ? (indented
        ? 'background-color: color-mix(in srgb, var(--accent-color) 15%, transparent); color: var(--accent-color);'
        : this.topLevelItemStyle(true))
      : (indented ? 'color: color-mix(in srgb, var(--sidebar-text) 76%, transparent);' : this.topLevelItemStyle());
    return `
      <button class="menu-item ${buttonClass}"
              style="${buttonStyle}"
              data-dashboard="${item.dashboard || ''}"
              data-id="${item.id}">
        ${item.icon ? (() => { const svg = getIconSVG(item.icon, 'currentColor', 18); return svg ? `<span style="display:inline-flex;align-items:center;">${svg}</span>` : ''; })() : ''}
        <span class="truncate sidebar-label">${item.label}</span>
      </button>
    `;
  }

  private topLevelItemStyle(active = false): string {
    const color = active ? 'var(--accent-color)' : 'var(--sidebar-text)';
    const background = active
      ? 'color-mix(in srgb, var(--accent-color) 15%, transparent)'
      : 'color-mix(in srgb, var(--sidebar-hover) 35%, transparent)';
    return `color: ${color}; background: ${background}; border-top: 1px solid color-mix(in srgb, var(--border-color) 75%, transparent); border-bottom: 1px solid color-mix(in srgb, var(--border-color) 50%, transparent);`;
  }

  protected attachEventListeners(): void {
    this.querySelectorAll('.menu-item').forEach(el =>
      el.addEventListener('click', this.handleMenuClick)
    );
    this.querySelectorAll('.category-toggle').forEach(el =>
      el.addEventListener('click', this.handleCategoryToggle)
    );
    this.querySelector('#org-btn')?.addEventListener('click', this.handleOrgBtnClick);
    this.querySelectorAll('.org-option').forEach(el =>
      el.addEventListener('click', this.handleOrgSelect)
    );
    document.addEventListener('click', this.handleOutsideOrgClick);
  }

  protected detachEventListeners(): void {
    this.querySelectorAll('.menu-item').forEach(el =>
      el.removeEventListener('click', this.handleMenuClick)
    );
    this.querySelectorAll('.category-toggle').forEach(el =>
      el.removeEventListener('click', this.handleCategoryToggle)
    );
    this.querySelector('#org-btn')?.removeEventListener('click', this.handleOrgBtnClick);
    this.querySelectorAll('.org-option').forEach(el =>
      el.removeEventListener('click', this.handleOrgSelect)
    );
    document.removeEventListener('click', this.handleOutsideOrgClick);
  }

  private handleOrgBtnClick = (e: Event): void => {
    e.stopPropagation();
    if (this.allowedOrgs.length <= 1) return;
    this.orgDropdownOpen = !this.orgDropdownOpen;
    const dropdown = this.querySelector('#org-dropdown');
    if (dropdown) dropdown.classList.toggle('hidden', !this.orgDropdownOpen);
    const chevron = this.querySelector('#org-btn svg');
    if (chevron) chevron.classList.toggle('rotate-180', this.orgDropdownOpen);
  };

  private handleOrgSelect = (e: Event): void => {
    const org = (e.currentTarget as HTMLElement).dataset.org;
    if (!org || org === this.currentOrg) return;
    switchOrg(org).catch(err => console.error('Failed to switch org:', err));
  };

  private handleOutsideOrgClick = (): void => {
    if (this.orgDropdownOpen) {
      this.orgDropdownOpen = false;
      this.querySelector('#org-dropdown')?.classList.add('hidden');
    }
  };

  private handleMenuClick = (e: Event): void => {
    const target = e.currentTarget as HTMLElement;
    const dashboard = target.dataset.dashboard;
    const id = target.dataset.id;
    if (dashboard && id) {
      // Do NOT update activeItem here - that happens via dashboard-shown
      // after switchToDashboard confirms the switch (not before, when a
      // "Stay here" dialog could still be pending).
      this.emit('dashboard-change', { dashboard, id });
    }
  };

  private handleCategoryToggle = (e: Event): void => {
    const target = e.currentTarget as HTMLElement;
    const categoryId = target.dataset.category;
    if (categoryId) {
      if (this.expandedCategories.has(categoryId)) {
        this.expandedCategories.delete(categoryId);
      } else {
        this.expandedCategories.add(categoryId);
      }
      this.rerender();
    }
  };

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  private applyCurrentBranding(): void {
    const details = this.orgDetails.get(this.currentOrg);
    const favicon = details?.favicon || '/xact/favicon.svg';
    const orgLabel = details?.displayName || this.currentOrg || 'XACT';
    document.title = `XACT-${orgLabel}`;
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = favicon;
  }

  private escapeHTML(str: string): string {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  setMenuItems(items: MenuItem[]): void {
    this.menuItems = items;
    // Ensure icon sets used by menu items are loaded before rendering.
    this.loadMenuIconSets(items);
    this.refreshAuthState();
  }

  /** Collect unique icon prefixes from all menu items and load any that aren't cached. */
  private loadMenuIconSets(items: MenuItem[]): void {
    const prefixes = new Set<string>();
    const collect = (list: MenuItem[]) => {
      for (const item of list) {
        if (item.icon?.includes(':')) prefixes.add(item.icon.split(':')[0]);
        if (item.children) collect(item.children);
      }
    };
    collect(items);
    // Load any icon sets not yet loaded; re-render once they arrive.
    const loads = [...prefixes].map(p => loadIconSet(p));
    Promise.all(loads).then(() => this.rerender());
  }

  setActiveItem(id: string): void {
    this.activeItem = id;
    // Expand parent category if the item is nested
    for (const item of this.filteredItems) {
      if (item.children?.some(child => this.isItemActive(child))) {
        this.expandedCategories.add(item.id);
      }
    }
    this.rerender();
  }

  private isItemActive(item: MenuItem): boolean {
    return item.id === this.activeItem
      || item.dashboard === this.activeItem
      || (item.dashboardAliases ?? []).includes(this.activeItem);
  }
}

customElements.define('app-sidebar', AppSidebar);
