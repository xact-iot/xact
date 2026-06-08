import { BaseComponent } from './base-component';
import { getCurrentUser, isAuthenticated } from '../auth';

export interface TabData {
  id: string;
  title: string;
  active: boolean;
}

type DashboardMode = 'view' | 'inspect' | 'edit';

export class AppHeader extends BaseComponent {
  private clockInterval: ReturnType<typeof setInterval> | null = null;
  private dropdownOpen: boolean = false;
  private menuOpen: boolean = false;
  private dashboardMode: DashboardMode = 'view';
  private canEditDashboard: boolean = false;
  private canInspectDashboard: boolean = false;
  private isOnDashboard: boolean = false;
  private username: string = '';
  private tabData: TabData[] = [];

  protected render(): void {
    this.style.cssText = 'grid-area: header; position: relative; z-index: 20000;';
    this.style.backgroundColor = 'var(--header-bg)';
    this.style.color = 'var(--header-text)';
    this.style.borderColor = 'var(--border-color)';
    this.className = 'flex items-center justify-between px-6 border-b';
    // Use the stored username only while the auth token is still valid.
    const user = isAuthenticated() ? getCurrentUser() : null;
    if (user && !this.username) {
      this.username = user.username;
    }
    const displayName = this.username || 'Not signed in';
    const avatarLetter = displayName.charAt(0).toUpperCase();

    this.innerHTML = `
      <div class="flex items-center gap-0" style="flex: 1; min-width: 0; height: 100%;">
        <button id="sidebar-toggle" class="p-1.5 rounded-lg transition-colors md:hidden hover:opacity-80" title="Toggle sidebar" style="flex-shrink: 0;">
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"/>
          </svg>
        </button>
        <div id="tab-strip">
          ${this.renderTabsHTML()}
        </div>
      </div>

      <div class="flex items-center gap-2" style="flex-shrink: 0;">
        <div class="text-sm font-mono border rounded-full px-3 py-0.5" id="clock" style="border-color: var(--border-color);">
          ${this.formatTime()}
        </div>

        <div class="relative">
          <button id="user-btn" class="flex items-center gap-2 p-1.5 rounded-lg transition-colors hover:opacity-80">
            <div class="w-7 h-7 rounded-full flex items-center justify-center text-xs font-medium" style="background-color: var(--accent-color); color: var(--header-icon-text-color);">${avatarLetter}</div>
            <span class="text-sm hidden sm:inline">${displayName}</span>
            <svg class="w-3 h-3 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/>
            </svg>
          </button>
          <div id="user-dropdown" class="hidden absolute right-0 top-full mt-1 w-44 rounded-lg border shadow-lg py-1"
               style="background-color: var(--header-bg); border-color: var(--border-color);">
            <button class="dropdown-item w-full text-left px-4 py-2 text-sm hover:opacity-80 transition-colors" data-action="profile">Profile</button>
            <button class="dropdown-item w-full text-left px-4 py-2 text-sm hover:opacity-80 transition-colors" data-action="preferences">Preferences</button>
            <div class="border-t my-1" style="border-color: var(--border-color);"></div>
            <button class="dropdown-item w-full text-left px-4 py-2 text-sm hover:opacity-80 transition-colors" data-action="logout">Logout</button>
          </div>
        </div>

        <!-- Dashboard hamburger menu -->
        <div class="relative">
          <button id="menu-btn" class="p-1.5 rounded-lg transition-colors hover:opacity-80 relative" title="Dashboard menu"
                  style="${this.dashboardMode !== 'view' ? 'color: var(--accent-color);' : ''}">
            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"/>
            </svg>
            ${this.dashboardMode !== 'view' ? `<span class="absolute top-0.5 right-0.5 w-2 h-2 rounded-full" style="background-color: var(--accent-color);"></span>` : ''}
          </button>
          <div id="menu-dropdown" class="${this.menuOpen ? '' : 'hidden'} absolute right-0 top-full mt-1 w-48 rounded-lg border shadow-lg py-1"
               style="background-color: var(--header-bg); border-color: var(--border-color);">
            ${this.renderDashboardMenuHTML()}
          </div>
        </div>
      </div>
    `;
  }

  private renderTabsHTML(): string {
    const showClose = this.tabData.length > 1;
    const tabsHTML = this.tabData.map(tab => `
      <div class="xact-tab${tab.active ? ' active' : ''}" data-tab-id="${tab.id}">
        <span class="xact-tab-title">${this.escapeHTML(tab.title)}</span>
        ${showClose ? `<button class="xact-tab-close" data-tab-id="${tab.id}" title="Close tab">&times;</button>` : ''}
      </div>
    `).join('');

    return tabsHTML + `
      <button id="tab-add-btn" title="New tab">+</button>
    `;
  }

  private escapeHTML(str: string): string {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  private refreshTabStrip(): void {
    const strip = this.querySelector('#tab-strip');
    if (!strip) return;
    strip.innerHTML = this.renderTabsHTML();
    // Scroll active tab into view
    const activeEl = strip.querySelector('.xact-tab.active') as HTMLElement | null;
    if (activeEl) {
      activeEl.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    }
  }

  private formatTime(): string {
    const now = new Date();
    return `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
  }

  protected attachEventListeners(): void {
    this.querySelector('#sidebar-toggle')?.addEventListener('click', this.handleSidebarToggle);
    this.querySelector('#user-btn')?.addEventListener('click', this.handleUserClick);
    this.querySelector('#menu-btn')?.addEventListener('click', this.handleMenuClick);
    this.querySelectorAll('.dropdown-item').forEach(el =>
      el.addEventListener('click', this.handleDropdownAction)
    );
    this.querySelectorAll('.menu-action-item').forEach(el =>
      el.addEventListener('click', this.handleMenuAction)
    );
    // Tab strip uses event delegation - single listener for all tab interactions
    this.querySelector('#tab-strip')?.addEventListener('click', this.handleTabStripClick);
    document.addEventListener('click', this.handleOutsideClick);

    this.clockInterval = setInterval(() => {
      const clock = this.querySelector('#clock');
      if (clock) clock.textContent = this.formatTime();
    }, 10000);
  }

  protected detachEventListeners(): void {
    this.querySelector('#sidebar-toggle')?.removeEventListener('click', this.handleSidebarToggle);
    this.querySelector('#user-btn')?.removeEventListener('click', this.handleUserClick);
    this.querySelector('#menu-btn')?.removeEventListener('click', this.handleMenuClick);
    this.querySelectorAll('.dropdown-item').forEach(el =>
      el.removeEventListener('click', this.handleDropdownAction)
    );
    this.querySelectorAll('.menu-action-item').forEach(el =>
      el.removeEventListener('click', this.handleMenuAction)
    );
    this.querySelector('#tab-strip')?.removeEventListener('click', this.handleTabStripClick);
    document.removeEventListener('click', this.handleOutsideClick);

    if (this.clockInterval) {
      clearInterval(this.clockInterval);
      this.clockInterval = null;
    }
  }

  private handleSidebarToggle = (): void => {
    this.emit('toggle-sidebar');
  };

  private handleUserClick = (e: Event): void => {
    e.stopPropagation();
    this.dropdownOpen = !this.dropdownOpen;
    this.menuOpen = false;
    const dropdown = this.querySelector('#user-dropdown');
    const menuDropdown = this.querySelector('#menu-dropdown');
    if (dropdown) dropdown.classList.toggle('hidden', !this.dropdownOpen);
    if (menuDropdown) menuDropdown.classList.add('hidden');
  };

  private handleMenuClick = (e: Event): void => {
    e.stopPropagation();
    this.menuOpen = !this.menuOpen;
    this.dropdownOpen = false;
    const menuDropdown = this.querySelector('#menu-dropdown');
    const userDropdown = this.querySelector('#user-dropdown');
    if (menuDropdown) menuDropdown.classList.toggle('hidden', !this.menuOpen);
    if (userDropdown) userDropdown.classList.add('hidden');
  };

  private handleOutsideClick = (): void => {
    if (this.dropdownOpen) {
      this.dropdownOpen = false;
      this.querySelector('#user-dropdown')?.classList.add('hidden');
    }
    if (this.menuOpen) {
      this.menuOpen = false;
      this.querySelector('#menu-dropdown')?.classList.add('hidden');
    }
  };

  private handleDropdownAction = (e: Event): void => {
    const action = (e.currentTarget as HTMLElement).dataset.action;
    this.dropdownOpen = false;
    this.querySelector('#user-dropdown')?.classList.add('hidden');
    if (action) this.emit('user-action', { action });
  };

  private handleMenuAction = (e: Event): void => {
    const action = (e.currentTarget as HTMLElement).dataset.action;
    this.menuOpen = false;
    this.querySelector('#menu-dropdown')?.classList.add('hidden');
    if (action) this.emit('dashboard-action', { action });
  };

  private handleTabStripClick = (e: Event): void => {
    const target = e.target as HTMLElement;

    // Close button clicked
    const closeBtn = target.closest('.xact-tab-close') as HTMLElement | null;
    if (closeBtn) {
      e.stopPropagation();
      const tabId = closeBtn.dataset.tabId;
      if (tabId) this.emit('tab-close', { tabId });
      return;
    }

    // Add button clicked
    if (target.closest('#tab-add-btn')) {
      this.emit('tab-add');
      return;
    }

    // Tab clicked
    const tabEl = target.closest('.xact-tab') as HTMLElement | null;
    if (tabEl) {
      const tabId = tabEl.dataset.tabId;
      if (tabId) this.emit('tab-select', { tabId });
    }
  };

  setTabs(tabs: TabData[]): void {
    this.tabData = tabs;
    this.refreshTabStrip();
  }

  setPageTitle(title: string): void {
    // Legacy - update active tab title if tabs exist
    const activeTab = this.tabData.find(t => t.active);
    if (activeTab) {
      activeTab.title = title;
      const el = this.querySelector(`.xact-tab.active .xact-tab-title`);
      if (el) el.textContent = title;
    }
  }

  setEditMode(editing: boolean): void {
    this.setDashboardMode(editing ? 'edit' : 'view');
  }

  setDashboardMode(mode: DashboardMode): void {
    this.dashboardMode = mode;
    const btn = this.querySelector('#menu-btn') as HTMLElement | null;
    if (btn) {
      btn.style.color = mode !== 'view' ? 'var(--accent-color)' : '';
      const dot = btn.querySelector('span');
      if (mode !== 'view' && !dot) {
        const d = document.createElement('span');
        d.className = 'absolute top-0.5 right-0.5 w-2 h-2 rounded-full';
        d.style.backgroundColor = 'var(--accent-color)';
        btn.appendChild(d);
      } else if (mode === 'view' && dot) {
        dot.remove();
      }
    }
    this.refreshDashboardMenu();
  }

  setDashboardCapabilities(capabilities: { canEdit?: boolean; canInspect?: boolean }): void {
    this.canEditDashboard = !!capabilities.canEdit;
    this.canInspectDashboard = !!capabilities.canInspect;
    this.refreshDashboardMenu();
  }

  setUser(username: string): void {
    this.username = username;
    const avatarEl = this.querySelector('#user-btn .w-7') as HTMLElement | null;
    const nameEl = this.querySelector('#user-btn span') as HTMLElement | null;
    if (avatarEl) avatarEl.textContent = username.charAt(0).toUpperCase();
    if (nameEl) nameEl.textContent = username;
  }

  clearUser(): void {
    this.username = '';
    const avatarEl = this.querySelector('#user-btn .w-7') as HTMLElement | null;
    const nameEl = this.querySelector('#user-btn span') as HTMLElement | null;
    if (avatarEl) avatarEl.textContent = 'N';
    if (nameEl) nameEl.textContent = 'Not signed in';
  }

  setIsOnDashboard(onDashboard: boolean): void {
    if (this.isOnDashboard === onDashboard) return;
    this.isOnDashboard = onDashboard;
    if (!onDashboard) {
      this.dashboardMode = 'view';
      this.canEditDashboard = false;
      this.canInspectDashboard = false;
    }
    this.refreshDashboardMenu();
  }

  private refreshDashboardMenu(): void {
    const dropdown = this.querySelector('#menu-dropdown');
    if (!dropdown) return;
    dropdown.innerHTML = this.renderDashboardMenuHTML();
    dropdown.querySelectorAll('.menu-action-item').forEach(el =>
      el.addEventListener('click', this.handleMenuAction)
    );
  }

  private renderDashboardMenuHTML(): string {
    if (!this.isOnDashboard) {
      return `<div class="px-4 py-2 text-xs opacity-40">No dashboard active</div>`;
    }

    const items: string[] = [];
    if (this.canInspectDashboard) {
      items.push(`
        <button class="menu-action-item w-full text-left px-4 py-2 text-sm hover:opacity-80 transition-colors flex items-center gap-2"
                data-action="toggle-inspect">
          ${this.dashboardMode === 'inspect'
            ? `<svg class="w-4 h-4 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg> Stop Inspecting`
            : `<svg class="w-4 h-4 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"/></svg> Inspect Dashboard`}
        </button>
      `);
    }
    if (this.canEditDashboard) {
      items.push(`
        <button class="menu-action-item w-full text-left px-4 py-2 text-sm hover:opacity-80 transition-colors flex items-center gap-2"
                data-action="toggle-edit">
          ${this.dashboardMode === 'edit'
            ? `<svg class="w-4 h-4 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/></svg> Stop Editing`
            : `<svg class="w-4 h-4 opacity-60" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg> Edit Dashboard`}
        </button>
      `);
    }

    return items.length
      ? items.join('')
      : `<div class="px-4 py-2 text-xs opacity-40">View only</div>`;
  }
}

customElements.define('app-header', AppHeader);
