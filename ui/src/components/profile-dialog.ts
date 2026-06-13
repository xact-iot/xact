import { BaseComponent } from './base-component';
import { getMyProfile, updateMyProfile, changeMyPassword } from '../api';
import type { UserRecord } from '../api';
import { can } from '../permissions/permissions';

export class ProfileDialog extends BaseComponent {
  private profileStatus: { message: string; error: boolean } | null = null;
  private passwordStatus: { message: string; error: boolean } | null = null;
  private profileSaving = false;
  private passwordSaving = false;
  private user: UserRecord | null = null;
  private canChangeProfile = true;

  protected render(): void {
    this.className = 'fixed inset-0 flex items-center justify-center hidden';
    this.style.zIndex = '20000';
    const disabledAttr = this.canChangeProfile ? '' : 'disabled';
    const disabledClass = this.canChangeProfile ? '' : 'opacity-60 cursor-not-allowed';

    this.innerHTML = `
      <div id="profile-backdrop" class="absolute inset-0 bg-black/60"></div>
      <div class="relative w-full max-w-md mx-4 border shadow-2xl" style="background-color: var(--modal-bg); color: var(--modal-text); border-color: var(--border-color);">

        <!-- Header bar -->
        <div class="flex items-center justify-between px-5 py-4 border-b" style="border-color: var(--border-color);">
          <div class="flex items-center gap-3">
            <div class="w-8 h-8 flex items-center justify-center text-xs font-mono font-semibold border" style="border-color: var(--accent-color); color: var(--accent-color);">
              ${this.user ? (this.user.firstName?.[0] || this.user.loginName?.[0] || '?').toUpperCase() : '?'}
            </div>
            <div>
              <div class="text-sm font-semibold tracking-wide">Profile Settings</div>
              ${this.user ? `<div class="text-xs opacity-50">${this.user.loginName}</div>` : ''}
            </div>
          </div>
          <button id="profile-close" class="p-1.5 rounded hover:opacity-70 transition-opacity" title="Close">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
            </svg>
          </button>
        </div>

        <div class="p-5 space-y-6">

          <!-- Personal Information -->
          <div>
            <div class="text-xs font-mono font-medium tracking-widest uppercase mb-3 flex items-center gap-2" style="color: var(--accent-color);">
              Personal Information
              <span class="flex-1 h-px" style="background: var(--accent-color); opacity: 0.25;"></span>
            </div>

            ${this.user === null ? `
              <div class="flex items-center gap-2 py-4 text-sm opacity-50">
                <svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                Loading profile…
              </div>
            ` : `
              <div class="space-y-3">
                <div class="grid grid-cols-2 gap-3">
                  <div>
                    <label class="block text-xs uppercase tracking-wider opacity-60 mb-1.5">First Name</label>
                    <input id="profile-firstname" type="text" value="${this._esc(this.user?.firstName || '')}"
                      class="w-full px-3 py-2 text-sm border bg-transparent outline-none focus:ring-1 transition-all"
                      style="border-color: var(--border-color); focus-ring-color: var(--accent-color);"
                      placeholder="First name" ${disabledAttr} />
                  </div>
                  <div>
                    <label class="block text-xs uppercase tracking-wider opacity-60 mb-1.5">Last Name</label>
                    <input id="profile-lastname" type="text" value="${this._esc(this.user?.lastName || '')}"
                      class="w-full px-3 py-2 text-sm border bg-transparent outline-none focus:ring-1 transition-all"
                      style="border-color: var(--border-color);"
                      placeholder="Last name" ${disabledAttr} />
                  </div>
                </div>
                <div>
                  <label class="block text-xs uppercase tracking-wider opacity-60 mb-1.5">Email</label>
                  <input id="profile-email" type="email" value="${this._esc(this.user?.email || '')}"
                    class="w-full px-3 py-2 text-sm border bg-transparent outline-none focus:ring-1 transition-all"
                    style="border-color: var(--border-color);"
                    placeholder="email@example.com" ${disabledAttr} />
                </div>

                ${this.profileStatus ? `
                  <div class="px-3 py-2 text-xs border ${this.profileStatus.error
                    ? 'border-red-500/40 bg-red-500/10 text-red-400'
                    : 'border-green-500/40 bg-green-500/10 text-green-400'}">
                    ${this._esc(this.profileStatus.message)}
                  </div>
                ` : ''}

                <!-- Notification Preferences -->
                <div class="pt-2">
                  <label class="block text-xs uppercase tracking-wider opacity-60 mb-2">Notifications</label>
                  <div class="flex items-center gap-4 mb-2">
                    <label class="flex items-center gap-1.5 text-xs cursor-pointer"
                           style="opacity: ${this.user?.notificationOptions?.emailEnabled ? '1' : '0.5'};">
                      <input type="checkbox" id="profile-notif-email"
                             ${this.user?.notificationOptions?.emailEnabled ? 'checked' : ''}
                             ${disabledAttr}
                             style="accent-color: var(--accent-color);">
                      Email
                    </label>
                    <label class="flex items-center gap-1.5 text-xs cursor-pointer"
                           style="opacity: ${this.user?.notificationOptions?.telegramEnabled ? '1' : '0.5'};">
                      <input type="checkbox" id="profile-notif-telegram"
                             ${this.user?.notificationOptions?.telegramEnabled ? 'checked' : ''}
                             ${disabledAttr}
                             style="accent-color: var(--accent-color);">
                      Telegram
                    </label>
                  </div>
                  <div>
                    <label class="block text-xs uppercase tracking-wider opacity-60 mb-1.5">Telegram ID</label>
                    <input id="profile-telegram-id" type="text"
                           value="${this._esc(this.user?.notificationOptions?.telegramId || '')}"
                           class="w-full px-3 py-2 text-sm border bg-transparent outline-none focus:ring-1 transition-all font-mono"
                           style="border-color: var(--border-color);"
                           placeholder="Numeric chat ID" ${disabledAttr} />
                  </div>
                </div>

                <button id="profile-save" class="w-full py-2 text-xs font-mono font-medium uppercase tracking-widest border transition-all
                  ${this.profileSaving || !this.canChangeProfile ? 'opacity-50 cursor-not-allowed' : 'hover:opacity-80'}"
                  style="border-color: var(--accent-color); color: var(--accent-color);"
                  ${this.profileSaving || !this.canChangeProfile ? 'disabled' : ''}>
                  ${this.profileSaving ? 'Saving…' : this.canChangeProfile ? 'Save Profile' : 'Profile Locked'}
                </button>
              </div>
            `}
          </div>

          <!-- Change Password -->
          <div>
            <div class="text-xs font-mono font-medium tracking-widest uppercase mb-3 flex items-center gap-2" style="color: var(--accent-color);">
              Change Password
              <span class="flex-1 h-px" style="background: var(--accent-color); opacity: 0.25;"></span>
            </div>

            <div class="space-y-3 ${disabledClass}">
              <div>
                <label class="block text-xs uppercase tracking-wider opacity-60 mb-1.5">Current Password</label>
                <input id="pwd-current" type="password"
                  class="w-full px-3 py-2 text-sm border bg-transparent outline-none focus:ring-1 transition-all"
                  style="border-color: var(--border-color);"
                  autocomplete="current-password" ${disabledAttr} />
              </div>
              <div>
                <label class="block text-xs uppercase tracking-wider opacity-60 mb-1.5">New Password</label>
                <input id="pwd-new" type="password"
                  class="w-full px-3 py-2 text-sm border bg-transparent outline-none focus:ring-1 transition-all"
                  style="border-color: var(--border-color);"
                  autocomplete="new-password" ${disabledAttr} />
              </div>
              <div>
                <label class="block text-xs uppercase tracking-wider opacity-60 mb-1.5">Confirm New Password</label>
                <input id="pwd-confirm" type="password"
                  class="w-full px-3 py-2 text-sm border bg-transparent outline-none focus:ring-1 transition-all"
                  style="border-color: var(--border-color);"
                  autocomplete="new-password" ${disabledAttr} />
              </div>

              ${this.passwordStatus ? `
                <div class="px-3 py-2 text-xs border ${this.passwordStatus.error
                  ? 'border-red-500/40 bg-red-500/10 text-red-400'
                  : 'border-green-500/40 bg-green-500/10 text-green-400'}">
                  ${this._esc(this.passwordStatus.message)}
                </div>
              ` : ''}

              <button id="pwd-save" class="w-full py-2 text-xs font-mono font-medium uppercase tracking-widest border transition-all
                ${this.passwordSaving || !this.canChangeProfile ? 'opacity-50 cursor-not-allowed' : 'hover:opacity-80'}"
                style="border-color: var(--accent-color); color: var(--accent-color);"
                ${this.passwordSaving || !this.canChangeProfile ? 'disabled' : ''}>
                ${this.passwordSaving ? 'Changing…' : this.canChangeProfile ? 'Change Password' : 'Password Locked'}
              </button>
            </div>
          </div>

        </div>
      </div>
    `;
  }

  private _esc(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  protected attachEventListeners(): void {
    this.querySelector('#profile-close')?.addEventListener('click', this.close);
    this.querySelector('#profile-backdrop')?.addEventListener('click', this.close);
    this.querySelector('#profile-save')?.addEventListener('click', this.handleSaveProfile);
    this.querySelector('#pwd-save')?.addEventListener('click', this.handleChangePassword);
    document.addEventListener('keydown', this.handleKeydown);
  }

  protected detachEventListeners(): void {
    this.querySelector('#profile-close')?.removeEventListener('click', this.close);
    this.querySelector('#profile-backdrop')?.removeEventListener('click', this.close);
    this.querySelector('#profile-save')?.removeEventListener('click', this.handleSaveProfile);
    this.querySelector('#pwd-save')?.removeEventListener('click', this.handleChangePassword);
    document.removeEventListener('keydown', this.handleKeydown);
  }

  private handleKeydown = (e: KeyboardEvent): void => {
    if (e.key === 'Escape') this.close();
  };

  private handleSaveProfile = async (): Promise<void> => {
    if (this.profileSaving || !this.canChangeProfile) return;
    const firstName = (this.querySelector('#profile-firstname') as HTMLInputElement)?.value.trim();
    const lastName = (this.querySelector('#profile-lastname') as HTMLInputElement)?.value.trim();
    const email = (this.querySelector('#profile-email') as HTMLInputElement)?.value.trim();
    const notificationOptions = {
      emailEnabled: (this.querySelector('#profile-notif-email') as HTMLInputElement)?.checked ?? false,
      telegramEnabled: (this.querySelector('#profile-notif-telegram') as HTMLInputElement)?.checked ?? false,
      telegramId: (this.querySelector('#profile-telegram-id') as HTMLInputElement)?.value.trim() ?? '',
    };

    this.profileSaving = true;
    this.profileStatus = null;
    this.rerender();

    try {
      const updated = await updateMyProfile({ firstName, lastName, email, notificationOptions });
      this.user = updated;
      this.profileStatus = { message: 'Profile saved successfully.', error: false };
      this.dispatchEvent(new CustomEvent('profile-updated', { bubbles: true, composed: true, detail: { user: updated } }));
    } catch (err: any) {
      this.profileStatus = { message: err?.message || 'Failed to save profile.', error: true };
    } finally {
      this.profileSaving = false;
      this.rerender();
    }
  };

  private handleChangePassword = async (): Promise<void> => {
    if (this.passwordSaving || !this.canChangeProfile) return;
    const current = (this.querySelector('#pwd-current') as HTMLInputElement)?.value;
    const next = (this.querySelector('#pwd-new') as HTMLInputElement)?.value;
    const confirm = (this.querySelector('#pwd-confirm') as HTMLInputElement)?.value;

    if (!current || !next) {
      this.passwordStatus = { message: 'Current and new password are required.', error: true };
      this.rerender();
      return;
    }
    if (next !== confirm) {
      this.passwordStatus = { message: 'New passwords do not match.', error: true };
      this.rerender();
      return;
    }

    this.passwordSaving = true;
    this.passwordStatus = null;
    this.rerender();

    try {
      await changeMyPassword(current, next);
      this.passwordStatus = { message: 'Password changed successfully.', error: false };
      // Clear password fields after success
      (this.querySelector('#pwd-current') as HTMLInputElement | null)?.value !== undefined &&
        ((this.querySelector('#pwd-current') as HTMLInputElement).value = '');
      (this.querySelector('#pwd-new') as HTMLInputElement | null)?.value !== undefined &&
        ((this.querySelector('#pwd-new') as HTMLInputElement).value = '');
      (this.querySelector('#pwd-confirm') as HTMLInputElement | null)?.value !== undefined &&
        ((this.querySelector('#pwd-confirm') as HTMLInputElement).value = '');
    } catch (err: any) {
      this.passwordStatus = { message: err?.message || 'Failed to change password.', error: true };
    } finally {
      this.passwordSaving = false;
      this.rerender();
    }
  };

  private rerender(): void {
    const visible = !this.classList.contains('hidden');
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    if (visible) this.classList.remove('hidden');
  }

  open = (): void => {
    this.user = null;
    this.profileStatus = null;
    this.passwordStatus = null;
    this.profileSaving = false;
    this.passwordSaving = false;
    this.canChangeProfile = true;
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    this.classList.remove('hidden');

    // Load profile data async
    Promise.all([getMyProfile(), can('profile.change')]).then(([user, canChangeProfile]) => {
      this.user = user;
      this.canChangeProfile = canChangeProfile;
      this.rerender();
    }).catch(() => {
      this.profileStatus = { message: 'Failed to load profile.', error: true };
      this.rerender();
    });
  };

  close = (): void => {
    this.classList.add('hidden');
  };
}

customElements.define('profile-dialog', ProfileDialog);
