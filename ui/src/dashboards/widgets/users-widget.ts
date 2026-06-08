import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { registerPermissions } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import {
  listUsers, createUser, updateUser, resetUserPassword, listRoles,
} from '../../api';
import type { UserRecord, Role, NotificationOptions } from '../../api';

registerPermissions('users', 'User Accounts', [
  { name: 'view', description: 'View user accounts' },
  { name: 'manage', description: 'Manage user accounts' },
], 'Controls access to the User Manager widget - roles with view can inspect user accounts; roles with manage can create, edit, and reset passwords for user accounts.');

registerWidgetType({
  type: 'users-widget',
  name: 'User Manager',
  icon: '👤',
  category: 'System',
  defaultW: 12,
  defaultH: 24,
  minW: 8,
  minH: 12,
});

interface DialogState {
  open: boolean;
  mode: 'create' | 'edit';
  user: Partial<UserRecord> & { password?: string };
  roles: string[];
  error: string;
  saving: boolean;
  resetResult: string;   // plaintext password after a reset, shown inline
  resetting: boolean;
}

export class UsersWidget extends BaseComponent {
  private users: UserRecord[] = [];
  private availableRoles: Role[] = [];
  private loading = true;
  private error = '';
  private canManage = false;
  private dialog: DialogState = {
    open: false, mode: 'create',
    user: { active: true }, roles: [], error: '', saving: false,
    resetResult: '', resetting: false,
  };
  private resetFeedback: { id: number; password: string } | null = null;

  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  private async initWithPermissions(): Promise<void> {
    const [canView, canManage] = await Promise.all([can('users.view'), can('users.manage')]);
    this.canManage = canManage;
    if (!canView && !canManage) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Insufficient permissions</div>`;
      return;
    }
    await this.loadData();
  }

  private async loadData(): Promise<void> {
    try {
      const [users, roles] = await Promise.all([listUsers(), listRoles()]);
      this.users = users;
      this.availableRoles = roles;
      this.loading = false;
    } catch (err) {
      this.error = 'Failed to load users';
      this.loading = false;
    }
    this.rerender();
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  protected detachEventListeners(): void {
    // Event listeners are added fresh in attachEventListeners each rerender;
    // innerHTML replacement removes old listeners automatically.
  }

  protected render(): void {
    if (this.loading) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Loading users…</div>`;
      return;
    }

    if (this.error) {
      this.innerHTML = `<div class="p-8 text-center text-red-400 text-sm">${this.error}</div>`;
      return;
    }

    this.innerHTML = `
      <div class="flex flex-col h-full">
        <!-- Header bar -->
        <div class="flex items-center justify-between px-4 py-3 border-b shrink-0"
             style="border-color: var(--border-color);">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium">Users</span>
            <span class="text-xs px-2 py-0.5 rounded-full font-mono"
                  style="background: color-mix(in srgb, var(--accent-color) 15%, transparent);
                         color: var(--accent-color);">${this.users.length}</span>
          </div>
          ${this.canManage ? `<button id="add-user-btn"
                  class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded transition-colors"
                  style="background: color-mix(in srgb, var(--accent-color) 15%, transparent);
                         color: var(--accent-color); border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);">
            + New User
          </button>` : `<span class="text-xs opacity-50">Read only</span>`}
        </div>

        <!-- Reset password feedback -->
        ${this.resetFeedback ? `
        <div class="flex items-center gap-3 px-4 py-2.5 text-xs border-b"
             style="background: color-mix(in srgb, #22c55e 8%, transparent);
                    border-color: color-mix(in srgb, #22c55e 25%, transparent);
                    color: #4ade80;">
          <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
          </svg>
          Password reset. New password copied to clipboard:
          <code class="font-mono px-2 py-0.5 rounded text-xs"
                style="background: rgba(0,0,0,0.3);">${this.resetFeedback.password}</code>
          <button id="dismiss-reset" class="ml-auto opacity-60 hover:opacity-100">✕</button>
        </div>
        ` : ''}

        <!-- User table -->
        <div class="flex-1 overflow-auto">
          <table class="w-full text-sm border-collapse">
            <thead>
              <tr class="text-left text-xs font-medium opacity-40 uppercase tracking-wide"
                  style="border-bottom: 1px solid var(--border-color);">
                <th class="px-4 py-2.5">Name</th>
                <th class="px-4 py-2.5">Login</th>
                <th class="px-4 py-2.5">Email</th>
                <th class="px-4 py-2.5">Roles</th>
                <th class="px-4 py-2.5">Status</th>
                <th class="px-4 py-2.5 w-28"></th>
              </tr>
            </thead>
            <tbody>
              ${this.users.map(u => this.renderUserRow(u)).join('')}
              ${this.users.length === 0 ? `
              <tr><td colspan="6" class="px-4 py-8 text-center opacity-30 text-xs">No users found</td></tr>
              ` : ''}
            </tbody>
          </table>
        </div>
      </div>

      ${this.dialog.open ? this.renderDialog() : ''}
    `;
  }

  private renderUserRow(u: UserRecord): string {
    const displayRoles = u.orgs?.flatMap(o => o.roles) ?? [];
    const uniqueRoles = [...new Set(displayRoles)];
    return `
      <tr class="border-b transition-colors hover:opacity-90 user-row"
          style="border-color: var(--border-color);" data-user-id="${u.id}">
        <td class="px-4 py-2.5">
          <div class="flex items-center gap-2.5">
            <div class="w-7 h-7 rounded-full flex items-center justify-center text-xs font-medium shrink-0"
                 style="background: color-mix(in srgb, var(--accent-color) 20%, transparent);
                        color: var(--accent-color);">
              ${(u.firstName?.[0] ?? u.loginName[0]).toUpperCase()}
            </div>
            <span class="font-medium">${this.esc(u.firstName)} ${this.esc(u.lastName)}</span>
          </div>
        </td>
        <td class="px-4 py-2.5 font-mono text-xs opacity-70">${this.esc(u.loginName)}</td>
        <td class="px-4 py-2.5 text-xs opacity-60">${this.esc(u.email)}</td>
        <td class="px-4 py-2.5">
          <div class="flex flex-wrap gap-1">
            ${uniqueRoles.map(r => `
              <span class="text-xs px-1.5 py-0.5 rounded font-mono"
                    style="background: color-mix(in srgb, var(--accent-color) 10%, transparent);
                           color: var(--accent-color); border: 1px solid color-mix(in srgb, var(--accent-color) 20%, transparent);">
                ${this.esc(r)}
              </span>`).join('')}
          </div>
        </td>
        <td class="px-4 py-2.5">
          <span class="text-xs px-2 py-0.5 rounded-full ${u.active ? 'text-green-400' : 'text-red-400'}"
                style="background: ${u.active ? 'rgba(34,197,94,0.1)' : 'rgba(239,68,68,0.1)'};">
            ${u.active ? 'Active' : 'Disabled'}
          </span>
        </td>
        <td class="px-4 py-2.5">
          <div class="flex items-center gap-1 justify-end">
            <button class="edit-user-btn p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="${this.canManage ? 'Edit user' : 'View user'}" data-user-id="${u.id}">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>
              </svg>
            </button>
            ${this.canManage ? `<button class="reset-pw-btn p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="Reset password" data-user-id="${u.id}">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"/>
              </svg>
            </button>
            <button class="toggle-active-btn p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="${u.active ? 'Disable' : 'Enable'} user" data-user-id="${u.id}"
                    data-active="${u.active}">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="${u.active
                        ? 'M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636'
                        : 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z'}"/>
              </svg>
            </button>` : ''}
          </div>
        </td>
      </tr>
    `;
  }

  private renderDialog(): string {
    const isCreate = this.dialog.mode === 'create';
    const u = this.dialog.user;
    const title = isCreate ? 'New User' : (this.canManage ? 'Edit User' : 'View User');
    const disabled = this.canManage ? '' : 'disabled';

    return `
      <div class="fixed inset-0 z-50 flex items-center justify-center" style="background: rgba(0,0,0,0.6);">
        <div class="rounded-lg border shadow-xl w-full max-w-md mx-4"
             style="background: var(--header-bg); border-color: var(--border-color);"
             id="user-dialog">
          <!-- Dialog header -->
          <div class="flex items-center justify-between px-5 py-4 border-b"
               style="border-color: var(--border-color);">
            <span class="font-medium text-sm">${title}</span>
            <button id="dialog-close" class="opacity-40 hover:opacity-100 p-1 rounded">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
              </svg>
            </button>
          </div>

          <!-- Dialog body -->
          <div class="px-5 py-4 space-y-3">
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-xs opacity-50 mb-1">First Name</label>
              <input id="dlg-first" type="text" value="${this.esc(u.firstName ?? '')}"
                       class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                       style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
              </div>
              <div>
                <label class="block text-xs opacity-50 mb-1">Last Name</label>
              <input id="dlg-last" type="text" value="${this.esc(u.lastName ?? '')}"
                       class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                       style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
              </div>
            </div>

            <div>
              <label class="block text-xs opacity-50 mb-1">Login Name ${isCreate ? '*' : ''}</label>
              <input id="dlg-login" type="text" value="${this.esc(u.loginName ?? '')}"
                     ${isCreate && this.canManage ? '' : 'disabled'}
                     class="w-full px-2.5 py-1.5 text-sm rounded border outline-none font-mono"
                     style="background: var(--input-bg); border-color: var(--border-color); color: inherit;
                            ${isCreate ? '' : 'opacity: 0.5;'}">
            </div>

            <div>
              <label class="block text-xs opacity-50 mb-1">Email *</label>
              <input id="dlg-email" type="email" value="${this.esc(u.email ?? '')}"
                     class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                     style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
            </div>

            ${isCreate ? `
            <div>
              <label class="block text-xs opacity-50 mb-1">Password *</label>
              <input id="dlg-password" type="password"
                     class="w-full px-2.5 py-1.5 text-sm rounded border outline-none font-mono"
                     style="background: var(--input-bg); border-color: var(--border-color); color: inherit;">
            </div>
            ` : ''}

            <div>
              <label class="block text-xs opacity-50 mb-1.5">Roles</label>
              <div class="flex flex-wrap gap-2">
                ${this.availableRoles.map(role => {
                  const checked = this.dialog.roles.includes(role.name);
                  return `
                    <label class="flex items-center gap-1.5 text-xs cursor-pointer role-toggle"
                           style="color: ${checked ? 'var(--accent-color)' : 'inherit'}; opacity: ${checked ? '1' : '0.5'};">
                      <input type="checkbox" class="role-cb" value="${this.esc(role.name)}"
                             ${checked ? 'checked' : ''} ${disabled} style="accent-color: var(--accent-color);">
                      ${this.esc(role.name)}
                    </label>`;
                }).join('')}
              </div>
            </div>

            <div>
              <label class="block text-xs opacity-50 mb-1.5">Notifications</label>
              <div class="space-y-2">
                <div class="flex items-center gap-4">
                  <label class="flex items-center gap-1.5 text-xs cursor-pointer"
                         style="opacity: ${(u as any)._notifEmailOn ? '1' : '0.5'};">
                    <input type="checkbox" id="dlg-notif-email" ${(u as any)._notifEmailOn ? 'checked' : ''}
                           ${disabled} style="accent-color: var(--accent-color);">
                    Email notifications
                  </label>
                  <label class="flex items-center gap-1.5 text-xs cursor-pointer"
                         style="opacity: ${(u as any)._notifTelegramOn ? '1' : '0.5'};">
                    <input type="checkbox" id="dlg-notif-telegram" ${(u as any)._notifTelegramOn ? 'checked' : ''}
                           ${disabled} style="accent-color: var(--accent-color);">
                    Telegram notifications
                  </label>
                </div>
                <div>
                  <label class="block text-xs opacity-40 mb-1">Telegram ID</label>
                  <input id="dlg-telegram-id" type="text" value="${this.esc((u as any)._telegramId ?? '')}"
                         placeholder="Numeric chat ID"
                         class="w-full px-2.5 py-1.5 text-sm rounded border outline-none font-mono"
                         style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
                </div>
              </div>
            </div>

            ${!isCreate ? `
            <div class="flex items-center justify-between pt-1">
              <label class="block text-xs opacity-50">Account Status</label>
              ${this.canManage ? `<button id="dlg-toggle-active"
                      class="flex items-center gap-2 px-3 py-1.5 text-xs rounded border transition-colors"
                      style="${u.active
                        ? 'border-color: rgba(34,197,94,0.4); color: #4ade80; background: rgba(34,197,94,0.08);'
                        : 'border-color: rgba(239,68,68,0.4); color: #f87171; background: rgba(239,68,68,0.08);'}">
                <span class="inline-block w-1.5 h-1.5 rounded-full"
                      style="background: ${u.active ? '#4ade80' : '#f87171'};"></span>
                ${u.active ? 'Active - click to disable' : 'Disabled - click to enable'}
              </button>` : `<span class="text-xs opacity-60">${u.active ? 'Active' : 'Disabled'}</span>`}
            </div>
            ` : ''}

            ${this.dialog.error ? `
            <div class="text-xs text-red-400 px-2.5 py-1.5 rounded"
                 style="background: rgba(239,68,68,0.08);">${this.dialog.error}</div>
            ` : ''}

            ${this.dialog.resetResult ? `
            <div class="flex items-center gap-2 px-2.5 py-2 rounded text-xs"
                 style="background: rgba(34,197,94,0.08); border: 1px solid rgba(34,197,94,0.25); color: #4ade80;">
              <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7"/>
              </svg>
              New password copied to clipboard:
              <code class="font-mono ml-1 px-1.5 py-0.5 rounded"
                    style="background: rgba(0,0,0,0.3); color: #86efac;">${this.dialog.resetResult}</code>
            </div>
            ` : ''}
          </div>

          <!-- Dialog footer -->
          <div class="flex items-center gap-2 px-5 py-4 border-t"
               style="border-color: var(--border-color);">
            ${!isCreate && this.canManage ? `
            <button id="dialog-reset-pw"
                    class="px-3 py-1.5 text-xs rounded border transition-colors flex items-center gap-1.5"
                    style="border-color: var(--border-color); opacity: 0.7;"
                    ${this.dialog.resetting ? 'disabled' : ''}>
              <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z"/>
              </svg>
              ${this.dialog.resetting ? 'Resetting…' : 'Reset Password'}
            </button>
            ` : ''}
            <div class="flex-1"></div>
            <button id="dialog-cancel"
                    class="px-4 py-1.5 text-xs rounded border transition-colors"
                    style="border-color: var(--border-color);">
              ${this.canManage ? 'Cancel' : 'Close'}
            </button>
            ${this.canManage ? `<button id="dialog-save"
                    class="px-4 py-1.5 text-xs font-medium rounded transition-colors"
                    style="background: var(--accent-color); color: #000;"
                    ${this.dialog.saving ? 'disabled' : ''}>
              ${this.dialog.saving ? 'Saving…' : 'Save'}
            </button>` : ''}
          </div>
        </div>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    if (this.canManage) this.querySelector('#add-user-btn')?.addEventListener('click', () => this.openCreateDialog());
    this.querySelector('#dismiss-reset')?.addEventListener('click', () => {
      this.resetFeedback = null;
      this.rerender();
    });

    this.querySelectorAll('.edit-user-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const id = parseInt((e.currentTarget as HTMLElement).dataset.userId!);
        this.openEditDialog(id);
      });
    });

    if (this.canManage) this.querySelectorAll('.reset-pw-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const id = parseInt((e.currentTarget as HTMLElement).dataset.userId!);
        this.handleResetPassword(id);
      });
    });

    if (this.canManage) this.querySelectorAll('.toggle-active-btn').forEach(btn => {
      btn.addEventListener('click', (e) => {
        const el = e.currentTarget as HTMLElement;
        const id = parseInt(el.dataset.userId!);
        const currentActive = el.dataset.active === 'true';
        this.handleToggleActive(id, !currentActive);
      });
    });

    if (this.dialog.open) {
      this.querySelector('#dialog-close')?.addEventListener('click', () => this.closeDialog());
      this.querySelector('#dialog-cancel')?.addEventListener('click', () => this.closeDialog());
      if (this.canManage) this.querySelector('#dialog-save')?.addEventListener('click', () => this.handleSave());

      if (this.canManage) this.querySelector('#dialog-reset-pw')?.addEventListener('click', () => this.handleDialogResetPassword());

      if (this.canManage) this.querySelector('#dlg-toggle-active')?.addEventListener('click', () => {
        this.dialog.user.active = !this.dialog.user.active;
        this.rerender();
      });

      if (this.canManage) this.querySelectorAll('.role-cb').forEach(cb => {
        cb.addEventListener('change', (e) => {
          const input = e.target as HTMLInputElement;
          if (input.checked) {
            this.dialog.roles = [...this.dialog.roles, input.value];
          } else {
            this.dialog.roles = this.dialog.roles.filter(r => r !== input.value);
          }
          // Update label colours without full rerender
          this.querySelectorAll<HTMLElement>('.role-toggle').forEach(label => {
            const cb = label.querySelector<HTMLInputElement>('input')!;
            label.style.color = cb.checked ? 'var(--accent-color)' : '';
            label.style.opacity = cb.checked ? '1' : '0.5';
          });
        });
      });
    }
  }

  private openCreateDialog(): void {
    if (!this.canManage) return;
    this.dialog = {
      open: true, mode: 'create',
      user: { active: true, _notifEmailOn: true, _notifTelegramOn: false, _telegramId: '' } as any,
      roles: [],
      error: '', saving: false, resetResult: '', resetting: false,
    };
    this.rerender();
  }

  private openEditDialog(id: number): void {
    const user = this.users.find(u => u.id === id);
    if (!user) return;
    const roles = user.orgs?.flatMap(o => o.roles) ?? [];
    const opts = user.notificationOptions;
    this.dialog = {
      open: true, mode: 'edit',
      user: {
        ...user,
        _notifEmailOn: opts?.emailEnabled ?? false,
        _notifTelegramOn: opts?.telegramEnabled ?? false,
        _telegramId: opts?.telegramId ?? '',
      } as any,
      roles: [...new Set(roles)],
      error: '', saving: false, resetResult: '', resetting: false,
    };
    this.rerender();
  }

  private closeDialog(): void {
    this.dialog.open = false;
    this.rerender();
  }

  private async handleSave(): Promise<void> {
    if (!this.canManage) return;
    const firstName = (this.querySelector('#dlg-first') as HTMLInputElement)?.value.trim() ?? '';
    const lastName = (this.querySelector('#dlg-last') as HTMLInputElement)?.value.trim() ?? '';
    const email = (this.querySelector('#dlg-email') as HTMLInputElement)?.value.trim() ?? '';
    const loginName = (this.querySelector('#dlg-login') as HTMLInputElement)?.value.trim() ?? '';
    const password = (this.querySelector('#dlg-password') as HTMLInputElement)?.value ?? '';

    const notificationOptions: NotificationOptions = {
      emailEnabled: (this.querySelector('#dlg-notif-email') as HTMLInputElement)?.checked ?? false,
      telegramEnabled: (this.querySelector('#dlg-notif-telegram') as HTMLInputElement)?.checked ?? false,
      telegramId: (this.querySelector('#dlg-telegram-id') as HTMLInputElement)?.value.trim() ?? '',
    };

    if (!email) {
      this.dialog.error = 'Email is required.';
      this.rerender();
      return;
    }

    this.dialog.saving = true;
    this.dialog.error = '';
    this.rerender();

    try {
      if (this.dialog.mode === 'create') {
        if (!loginName || !password) {
          this.dialog.error = 'Login name and password are required.';
          this.dialog.saving = false;
          this.rerender();
          return;
        }
        await createUser({ firstName, lastName, loginName, email, password, roles: this.dialog.roles });
        // Set notification options on the newly created user
        const users = await listUsers();
        const newUser = users.find(u => u.loginName === loginName);
        if (newUser) {
          await updateUser(newUser.id, { notificationOptions });
        }
      } else {
        await updateUser(this.dialog.user.id!, {
          firstName, lastName, email, active: this.dialog.user.active,
          roles: this.dialog.roles,
          notificationOptions,
        });
      }
      await this.loadData();
      this.closeDialog();
    } catch (err: any) {
      this.dialog.error = err?.message ?? 'Failed to save user.';
      this.dialog.saving = false;
      this.rerender();
    }
  }

  private async handleResetPassword(id: number): Promise<void> {
    if (!this.canManage) return;
    try {
      const { password } = await resetUserPassword(id);
      this.resetFeedback = { id, password };
      navigator.clipboard?.writeText(password).catch(() => {});
      this.rerender();
    } catch (err: any) {
      console.error('Failed to reset password:', err);
    }
  }

  private async handleDialogResetPassword(): Promise<void> {
    if (!this.canManage) return;
    const id = this.dialog.user.id;
    if (!id) return;
    this.dialog.resetting = true;
    this.dialog.resetResult = '';
    this.rerender();
    try {
      const { password } = await resetUserPassword(id);
      this.dialog.resetResult = password;
      navigator.clipboard?.writeText(password).catch(() => {});
    } catch (err: any) {
      this.dialog.error = err?.message ?? 'Failed to reset password.';
    }
    this.dialog.resetting = false;
    this.rerender();
  }

  private async handleToggleActive(id: number, active: boolean): Promise<void> {
    if (!this.canManage) return;
    try {
      await updateUser(id, { active });
      await this.loadData();
    } catch (err) {
      console.error('Failed to toggle user active state:', err);
    }
  }

  private esc(s: string): string {
    return (s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }
}

customElements.define('users-widget', UsersWidget);
