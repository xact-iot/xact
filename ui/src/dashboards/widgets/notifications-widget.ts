import { BaseComponent } from '../../components/base-component';
import { registerWidgetType } from './widget-registry';
import { registerPermissions } from '../../permissions/registry';
import { can } from '../../permissions/permissions';
import { showConfirm } from '../../components/app-dialog';
import {
  listNotificationProfiles, createNotificationProfile, updateNotificationProfile,
  deleteNotificationProfile, getChannelConfig, saveChannelConfig,
  listRoles, listUsers,
} from '../../api';
import type { NotificationProfile, ChannelConfig, Role, UserRecord } from '../../api';

registerPermissions('notifications', 'Notifications', [
  { name: 'view', description: 'View notification profiles and channels' },
  { name: 'manage', description: 'Manage notification profiles and channels' },
], 'Controls access to the Notifications widget - roles with view can inspect settings; roles with manage can configure email/telegram channels and notification profiles.');

registerWidgetType({
  type: 'notifications-widget',
  name: 'Notifications',
  icon: '\u{1F514}',
  category: 'System',
  defaultW: 12,
  defaultH: 24,
  minW: 8,
  minH: 12,
});

type Tab = 'channels' | 'profiles';

interface ProfileDialog {
  open: boolean;
  mode: 'create' | 'edit';
  profile: Partial<NotificationProfile>;
  error: string;
  saving: boolean;
}

export class NotificationsWidget extends BaseComponent {
  private tab: Tab = 'profiles';
  private profiles: NotificationProfile[] = [];
  private channelConfig: ChannelConfig = {
    email: { host: '', port: 587, username: '', password: '', from: '', useTls: true },
    telegram: { botToken: '' },
  };
  private roles: Role[] = [];
  private users: UserRecord[] = [];
  private loading = true;
  private error = '';
  private channelSaving = false;
  private channelSaved = false;
  private canManage = false;
  private dialog: ProfileDialog = {
    open: false, mode: 'create', profile: {}, error: '', saving: false,
  };

  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  private async initWithPermissions(): Promise<void> {
    const [canView, canManage] = await Promise.all([can('notifications.view'), can('notifications.manage')]);
    this.canManage = canManage;
    if (!canView && !canManage) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Insufficient permissions</div>`;
      return;
    }
    await this.loadData();
  }

  private async loadData(): Promise<void> {
    try {
      const [profiles, cfg, roles, users] = await Promise.all([
        listNotificationProfiles(),
        getChannelConfig(),
        this.canManage ? listRoles() : Promise.resolve([]),
        this.canManage ? listUsers() : Promise.resolve([]),
      ]);
      this.profiles = profiles;
      this.channelConfig = cfg;
      this.roles = roles;
      this.users = users;
      this.loading = false;
    } catch (err) {
      this.error = 'Failed to load notification settings';
      this.loading = false;
    }
    this.rerender();
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  protected detachEventListeners(): void {}

  protected render(): void {
    if (this.loading) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Loading notifications settings...</div>`;
      return;
    }
    if (this.error) {
      this.innerHTML = `<div class="p-8 text-center text-red-400 text-sm">${this.error}</div>`;
      return;
    }

    this.innerHTML = `
      <div class="flex flex-col h-full">
        <!-- Tab bar -->
        <div class="flex items-center gap-0 px-4 border-b shrink-0"
             style="border-color: var(--border-color);">
          ${this.renderTab('profiles', 'Profiles')}
          ${this.renderTab('channels', 'Channels')}
        </div>

        <!-- Tab content -->
        <div class="flex-1 overflow-auto">
          ${this.tab === 'profiles' ? this.renderProfilesTab() : this.renderChannelsTab()}
        </div>
      </div>
      ${this.dialog.open ? this.renderProfileDialog() : ''}
    `;
  }

  private renderTab(id: Tab, label: string): string {
    const active = this.tab === id;
    return `
      <button class="tab-btn px-4 py-3 text-xs font-medium transition-colors relative"
              data-tab="${id}"
              style="color: ${active ? 'var(--accent-color)' : 'inherit'}; opacity: ${active ? '1' : '0.5'};">
        ${label}
        ${active ? `<span class="absolute bottom-0 left-2 right-2 h-0.5 rounded-full"
                          style="background: var(--accent-color);"></span>` : ''}
      </button>
    `;
  }

  private renderProfilesTab(): string {
    return `
      <div class="p-4">
        <div class="flex items-center justify-between mb-3">
          <span class="text-sm font-medium">Notification Profiles</span>
          ${this.canManage ? `<button id="add-profile-btn"
                  class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded transition-colors"
                  style="background: color-mix(in srgb, var(--accent-color) 15%, transparent);
                         color: var(--accent-color); border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);">
            + New Profile
          </button>` : `<span class="text-xs opacity-50">Read only</span>`}
        </div>

        <div class="space-y-2">
          ${this.profiles.length === 0
            ? `<div class="text-center text-xs opacity-30 py-8">No profiles configured</div>`
            : this.profiles.map(p => this.renderProfileCard(p)).join('')}
        </div>
      </div>
    `;
  }

  private renderProfileCard(p: NotificationProfile): string {
    return `
      <div class="rounded border p-3 transition-colors"
           style="border-color: var(--border-color); background: var(--surface-tint);">
        <div class="flex items-center justify-between mb-1.5">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium">${this.esc(p.name)}</span>
          </div>
          <div class="flex items-center gap-1">
            <button class="edit-profile-btn p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="${this.canManage ? 'Edit' : 'View'}" data-profile-id="${p.id}">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>
              </svg>
            </button>
            ${this.canManage ? `<button class="delete-profile-btn p-1.5 rounded opacity-50 hover:opacity-100 transition-opacity"
                    title="Delete" data-profile-id="${p.id}"
                    style="color: var(--danger-color);">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/>
              </svg>
            </button>` : ''}
          </div>
        </div>
        <div class="text-xs opacity-50 mb-2">${this.esc(p.description)}</div>
        <div class="flex flex-wrap gap-1.5">
          ${p.roles.map(r => `
            <span class="text-xs px-1.5 py-0.5 rounded font-mono"
                  style="background: color-mix(in srgb, var(--accent-color) 10%, transparent);
                         color: var(--accent-color); border: 1px solid color-mix(in srgb, var(--accent-color) 20%, transparent);">
              ${this.esc(r)}
            </span>`).join('')}
          ${p.users.length > 0 ? `
            <span class="text-xs px-1.5 py-0.5 rounded opacity-50"
                  style="background: var(--input-bg);">
              +${p.users.length} user${p.users.length > 1 ? 's' : ''}
            </span>` : ''}
        </div>
      </div>
    `;
  }

  private renderChannelsTab(): string {
    const e = this.channelConfig.email;
    const t = this.channelConfig.telegram;
    const disabled = this.canManage ? '' : 'disabled';

    return `
      <div class="p-4 space-y-6">
        <!-- Email SMTP Settings -->
        <div>
          <div class="flex items-center gap-2 mb-3 px-3 py-2 rounded border"
               style="background: color-mix(in srgb, var(--accent-color) 10%, transparent);
                      border-color: color-mix(in srgb, var(--accent-color) 30%, transparent);
                      color: var(--accent-color);">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/>
            </svg>
            <span class="text-sm font-semibold tracking-wide">Email (SMTP)</span>
          </div>
          <div class="space-y-2">
            <div class="grid grid-cols-3 gap-2">
              <div class="col-span-2">
                <label class="block text-xs opacity-40 mb-1">SMTP Host</label>
                <input id="ch-smtp-host" type="text" value="${this.esc(e.host)}"
                       placeholder="smtp.example.com"
                       class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                       style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
              </div>
              <div>
                <label class="block text-xs opacity-40 mb-1">Port</label>
                <input id="ch-smtp-port" type="number" value="${e.port}"
                       class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                       style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
              </div>
            </div>
            <div class="grid grid-cols-2 gap-2">
              <div>
                <label class="block text-xs opacity-40 mb-1">Username</label>
                <input id="ch-smtp-user" type="text" value="${this.esc(e.username)}"
                       class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                       style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
              </div>
              <div>
                <label class="block text-xs opacity-40 mb-1">Password</label>
                <input id="ch-smtp-pass" type="password" value="${this.esc(e.password)}"
                       class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                       style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
              </div>
            </div>
            <div>
              <label class="block text-xs opacity-40 mb-1">From Address</label>
              <input id="ch-smtp-from" type="email" value="${this.esc(e.from)}"
                     placeholder="noreply@example.com"
                     class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                     style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
            </div>
            <label class="flex items-center gap-1.5 text-xs cursor-pointer"
                   style="opacity: ${e.useTls ? '1' : '0.5'};">
              <input type="checkbox" id="ch-smtp-tls" ${e.useTls ? 'checked' : ''} ${disabled}
                     style="accent-color: var(--accent-color);">
              Use TLS
            </label>
          </div>
        </div>

        <!-- Telegram Settings -->
        <div>
          <div class="flex items-center gap-2 mb-3 px-3 py-2 rounded border"
               style="background: color-mix(in srgb, var(--accent-color) 10%, transparent);
                      border-color: color-mix(in srgb, var(--accent-color) 30%, transparent);
                      color: var(--accent-color);">
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
                    d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"/>
            </svg>
            <span class="text-sm font-semibold tracking-wide">Telegram</span>
            <button id="tg-info-btn" class="relative flex items-center justify-center w-4 h-4 rounded-full text-xs font-bold cursor-pointer"
                    style="background: color-mix(in srgb, var(--accent-color) 20%, var(--widget-bg)); color: var(--accent-color); line-height: 1;"
                    title="Setup instructions">i</button>
          </div>

          <!-- Telegram setup tooltip -->
          <div id="tg-info-popup" class="hidden mb-3 rounded border p-3 text-xs leading-relaxed"
               style="background: color-mix(in srgb, var(--accent-color) 6%, var(--widget-bg));
                      border-color: color-mix(in srgb, var(--accent-color) 25%, transparent);
                      color: var(--content-text);">
            <div class="font-medium mb-1.5" style="color: var(--accent-color);">Telegram Bot Setup</div>
            <ol class="list-decimal pl-4 space-y-1 opacity-80">
              <li>Open Telegram and search for <strong>@BotFather</strong>.</li>
              <li>Send <code class="px-1 py-0.5 rounded" style="background: var(--code-bg);">/newbot</code> and follow the prompts to create a bot.</li>
              <li>Copy the <strong>API token</strong> BotFather gives you and paste it in the Bot Token field below.</li>
              <li>Each user who wants Telegram notifications must message your bot first (send any message to start a conversation).</li>
              <li>To find a user's <strong>Chat ID</strong>, have them message the bot, then open<br>
                <code class="px-1 py-0.5 rounded" style="background: var(--code-bg);">https://api.telegram.org/bot&lt;TOKEN&gt;/getUpdates</code><br>
                and look for <code class="px-1 py-0.5 rounded" style="background: var(--code-bg);">"chat":{"id": ...}</code> in the response.</li>
              <li>Enter each user's Chat ID in their profile under <strong>Telegram ID</strong>.</li>
            </ol>
          </div>

          <div>
            <label class="block text-xs opacity-40 mb-1">Bot Token</label>
            <input id="ch-tg-token" type="text" value="${this.esc(t.botToken)}"
                   placeholder="123456:ABC-DEF..."
                   class="w-full px-2.5 py-1.5 text-sm rounded border outline-none font-mono"
                   style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${disabled}>
          </div>
        </div>

        <!-- Save button -->
        ${this.canManage ? `<div class="flex items-center gap-3">
          <button id="save-channels-btn"
                  class="px-4 py-1.5 text-xs font-medium rounded transition-colors"
                  style="background: var(--accent-color); color: #000;"
                  ${this.channelSaving ? 'disabled' : ''}>
            ${this.channelSaving ? 'Saving...' : 'Save Channels'}
          </button>
          ${this.channelSaved ? `
            <span class="text-xs" style="color: var(--status-good-color);">Saved</span>
          ` : ''}
        </div>` : ''}
      </div>
    `;
  }

  private renderProfileDialog(): string {
    const isCreate = this.dialog.mode === 'create';
    const p = this.dialog.profile;
    const selectedRoles = p.roles ?? [];
    const selectedUsers = p.users ?? [];

    return `
      <div class="fixed inset-0 z-50 flex items-center justify-center" style="background: rgba(0,0,0,0.6);">
        <div class="rounded-lg border shadow-xl w-full max-w-md mx-4"
             style="background: var(--header-bg); border-color: var(--border-color);">
          <div class="flex items-center justify-between px-5 py-4 border-b"
               style="border-color: var(--border-color);">
            <span class="font-medium text-sm">${isCreate ? 'New Profile' : (this.canManage ? 'Edit Profile' : 'View Profile')}</span>
            <button id="profile-dialog-close" class="opacity-40 hover:opacity-100 p-1 rounded">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
              </svg>
            </button>
          </div>

          <div class="px-5 py-4 space-y-3">
            <div>
              <label class="block text-xs opacity-50 mb-1">Name *</label>
              <input id="pdlg-name" type="text" value="${this.esc(p.name ?? '')}"
                     class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                     style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${this.canManage ? '' : 'disabled'}>
            </div>

            <div>
              <label class="block text-xs opacity-50 mb-1">Description</label>
              <input id="pdlg-desc" type="text" value="${this.esc(p.description ?? '')}"
                     class="w-full px-2.5 py-1.5 text-sm rounded border outline-none"
                     style="background: var(--input-bg); border-color: var(--border-color); color: inherit;" ${this.canManage ? '' : 'disabled'}>
            </div>

            <div>
              <label class="block text-xs opacity-50 mb-1.5">Roles</label>
              <div class="flex flex-wrap gap-2">
                ${this.roles.map(role => {
                  const checked = selectedRoles.includes(role.name);
                  return `
                    <label class="flex items-center gap-1.5 text-xs cursor-pointer profile-role-toggle"
                           style="color: ${checked ? 'var(--accent-color)' : 'inherit'}; opacity: ${checked ? '1' : '0.5'};">
                      <input type="checkbox" class="profile-role-cb" value="${this.esc(role.name)}"
                             ${checked ? 'checked' : ''} ${this.canManage ? '' : 'disabled'} style="accent-color: var(--accent-color);">
                      ${this.esc(role.name)}
                    </label>`;
                }).join('')}
              </div>
            </div>

            <div>
              <label class="block text-xs opacity-50 mb-1.5">Users</label>
              <div class="max-h-32 overflow-auto space-y-1 rounded border p-2"
                   style="border-color: var(--border-color); background: var(--surface-tint);">
                ${this.users.map(u => {
                  const checked = selectedUsers.includes(u.id);
                  const name = `${u.firstName} ${u.lastName}`.trim() || u.loginName;
                  return `
                    <label class="flex items-center gap-1.5 text-xs cursor-pointer profile-user-toggle"
                           style="opacity: ${checked ? '1' : '0.5'};">
                      <input type="checkbox" class="profile-user-cb" value="${u.id}"
                             ${checked ? 'checked' : ''} ${this.canManage ? '' : 'disabled'} style="accent-color: var(--accent-color);">
                      ${this.esc(name)}
                      <span class="font-mono opacity-40 ml-auto">${this.esc(u.loginName)}</span>
                    </label>`;
                }).join('')}
              </div>
            </div>

            ${this.dialog.error ? `
            <div class="text-xs text-red-400 px-2.5 py-1.5 rounded"
                 style="background: rgba(239,68,68,0.08);">${this.dialog.error}</div>
            ` : ''}
          </div>

          <div class="flex items-center gap-2 px-5 py-4 border-t justify-end"
               style="border-color: var(--border-color);">
            <button id="profile-dialog-cancel"
                    class="px-4 py-1.5 text-xs rounded border transition-colors"
                    style="border-color: var(--border-color);">
              ${this.canManage ? 'Cancel' : 'Close'}
            </button>
            ${this.canManage ? `<button id="profile-dialog-save"
                    class="px-4 py-1.5 text-xs font-medium rounded transition-colors"
                    style="background: var(--accent-color); color: #000;"
                    ${this.dialog.saving ? 'disabled' : ''}>
              ${this.dialog.saving ? 'Saving...' : 'Save'}
            </button>` : ''}
          </div>
        </div>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    // Tab switching
    this.querySelectorAll('.tab-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        this.tab = (btn as HTMLElement).dataset.tab as Tab;
        this.rerender();
      });
    });

    // Profiles tab
    if (this.canManage) this.querySelector('#add-profile-btn')?.addEventListener('click', () => this.openCreateProfile());

    this.querySelectorAll('.edit-profile-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = parseInt((btn as HTMLElement).dataset.profileId!);
        this.openEditProfile(id);
      });
    });

    if (this.canManage) this.querySelectorAll('.delete-profile-btn').forEach(btn => {
      btn.addEventListener('click', async () => {
        const id = parseInt((btn as HTMLElement).dataset.profileId!);
        const confirmed = await showConfirm('Delete this notification profile?', {
          title: 'Delete notification profile',
          confirmLabel: 'Delete',
          cancelLabel: 'Keep',
          tone: 'danger',
        });
        if (!confirmed) return;
        try {
          await deleteNotificationProfile(id);
          await this.loadData();
        } catch (err: any) {
          console.error('Failed to delete profile:', err);
        }
      });
    });

    // Channels tab
    this.querySelector('#tg-info-btn')?.addEventListener('click', () => {
      this.querySelector('#tg-info-popup')?.classList.toggle('hidden');
    });
    if (this.canManage) this.querySelector('#save-channels-btn')?.addEventListener('click', () => this.handleSaveChannels());

    // Profile dialog
    if (this.dialog.open) {
      this.querySelector('#profile-dialog-close')?.addEventListener('click', () => this.closeProfileDialog());
      this.querySelector('#profile-dialog-cancel')?.addEventListener('click', () => this.closeProfileDialog());
      if (this.canManage) this.querySelector('#profile-dialog-save')?.addEventListener('click', () => this.handleSaveProfile());
    }
  }

  private openCreateProfile(): void {
    if (!this.canManage) return;
    this.dialog = {
      open: true, mode: 'create',
      profile: { name: '', description: '', roles: [], users: [], ackRequired: false },
      error: '', saving: false,
    };
    this.rerender();
  }

  private openEditProfile(id: number): void {
    const profile = this.profiles.find(p => p.id === id);
    if (!profile) return;
    this.dialog = {
      open: true, mode: 'edit',
      profile: { ...profile, roles: [...profile.roles], users: [...profile.users] },
      error: '', saving: false,
    };
    this.rerender();
  }

  private closeProfileDialog(): void {
    this.dialog.open = false;
    this.rerender();
  }

  private async handleSaveProfile(): Promise<void> {
    if (!this.canManage) return;
    const name = (this.querySelector('#pdlg-name') as HTMLInputElement)?.value.trim();
    const description = (this.querySelector('#pdlg-desc') as HTMLInputElement)?.value.trim() ?? '';
    const ackRequired = (this.querySelector('#pdlg-ack') as HTMLInputElement)?.checked ?? false;

    const roles: string[] = [];
    this.querySelectorAll<HTMLInputElement>('.profile-role-cb').forEach(cb => {
      if (cb.checked) roles.push(cb.value);
    });

    const users: number[] = [];
    this.querySelectorAll<HTMLInputElement>('.profile-user-cb').forEach(cb => {
      if (cb.checked) users.push(parseInt(cb.value));
    });

    if (!name) {
      this.dialog.error = 'Name is required.';
      this.rerender();
      return;
    }

    this.dialog.saving = true;
    this.dialog.error = '';
    this.rerender();

    try {
      const data = { name, description, roles, users, ackRequired };
      if (this.dialog.mode === 'create') {
        await createNotificationProfile(data);
      } else {
        await updateNotificationProfile(this.dialog.profile.id!, data);
      }
      await this.loadData();
      this.closeProfileDialog();
    } catch (err: any) {
      this.dialog.error = err?.message ?? 'Failed to save profile.';
      this.dialog.saving = false;
      this.rerender();
    }
  }

  private async handleSaveChannels(): Promise<void> {
    if (!this.canManage) return;
    // Read form values BEFORE rerender destroys the DOM inputs
    const cfg: ChannelConfig = {
      email: {
        host: (this.querySelector('#ch-smtp-host') as HTMLInputElement)?.value.trim() ?? '',
        port: parseInt((this.querySelector('#ch-smtp-port') as HTMLInputElement)?.value ?? '587') || 587,
        username: (this.querySelector('#ch-smtp-user') as HTMLInputElement)?.value.trim() ?? '',
        password: (this.querySelector('#ch-smtp-pass') as HTMLInputElement)?.value ?? '',
        from: (this.querySelector('#ch-smtp-from') as HTMLInputElement)?.value.trim() ?? '',
        useTls: (this.querySelector('#ch-smtp-tls') as HTMLInputElement)?.checked ?? true,
      },
      telegram: {
        botToken: (this.querySelector('#ch-tg-token') as HTMLInputElement)?.value.trim() ?? '',
      },
    };

    this.channelSaving = true;
    this.channelSaved = false;
    this.channelConfig = cfg;
    this.rerender();

    try {
      this.channelConfig = await saveChannelConfig(cfg);
      this.channelSaved = true;
    } catch (err: any) {
      console.error('Failed to save channel config:', err);
    }
    this.channelSaving = false;
    this.rerender();

    // Clear "Saved" indicator after a few seconds
    if (this.channelSaved) {
      setTimeout(() => {
        this.channelSaved = false;
        this.rerender();
      }, 3000);
    }
  }

  private esc(s: string): string {
    return (s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }
}

customElements.define('notifications-widget', NotificationsWidget);
