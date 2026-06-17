import { BaseComponent } from '../../components/base-component';
import { showConfirm } from '../../components/app-dialog';
import { getCurrentUser } from '../../auth';
import { can } from '../../permissions/permissions';
import { registerPermissions } from '../../permissions/registry';
import { registerWidgetType } from './widget-registry';
import {
  createAgentToken,
  deleteAgentToken,
  getAgentToken,
  listAgentTokens,
  listAgentTokenUsers,
} from '../../api';
import type { AgentToken, AgentTokenUser } from '../../api';

registerPermissions('agentkeys', 'Agent Keys', [
  { name: 'manage', description: 'Create and delete agent tokens for users in the current organisation' },
  { name: 'personal', description: 'Create and delete your own agent tokens' },
  { name: 'access', description: 'Retrieve your own agent token values' },
], 'Controls access to the Agent Keys widget and bearer tokens for agents and MCP clients.');

registerWidgetType({
  type: 'agentkeys-widget',
  name: 'Agent Keys',
  icon: '🔑',
  category: 'System',
  defaultW: 16,
  defaultH: 18,
  minW: 10,
  minH: 12,
});

export class AgentKeysWidget extends BaseComponent {
  private tokens: AgentToken[] = [];
  private users: AgentTokenUser[] = [];
  private revealed = new Map<number, string>();
  private loading = true;
  private error = '';
  private feedback = '';
  private canManage = false;
  private canPersonal = false;
  private canAccess = false;
  private creating = false;
  private currentUserID = Number(getCurrentUser()?.id ?? 0);

  connectedCallback(): void {
    super.connectedCallback();
    this.initWithPermissions();
  }

  protected render(): void {
    if (this.loading) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Loading agent keys...</div>`;
      return;
    }
    if (this.error) {
      this.innerHTML = `<div class="p-8 text-center text-red-400 text-sm">${this.esc(this.error)}</div>`;
      return;
    }

    this.innerHTML = `
      <div class="flex flex-col h-full text-sm">
        <div class="flex items-center justify-between gap-3 px-4 py-3 border-b shrink-0"
             style="border-color:var(--border-color);">
          <div class="flex items-center gap-2 min-w-0">
            <span class="font-medium">Agent Keys</span>
            <span class="text-xs px-2 py-0.5 rounded-full font-mono"
                  style="background:color-mix(in srgb,var(--accent-color) 15%,transparent);color:var(--accent-color);">
              ${this.tokens.length}
            </span>
          </div>
          ${(this.canManage || this.canPersonal) ? this.renderCreateControls() : `
            <span class="text-xs opacity-50">Read only</span>
          `}
        </div>

        ${this.feedback ? `
          <div class="px-4 py-2 text-xs border-b"
               style="border-color:color-mix(in srgb,var(--accent-color) 24%,transparent);
                      color:var(--accent-color);
                      background:color-mix(in srgb,var(--accent-color) 8%,transparent);">
            ${this.esc(this.feedback)}
          </div>
        ` : ''}

        <div class="flex-1 overflow-auto">
          <table class="w-full border-collapse">
            <thead>
              <tr class="text-left text-xs uppercase opacity-50 border-b" style="border-color:var(--border-color);">
                <th class="px-4 py-2.5 font-medium">Name</th>
                <th class="px-4 py-2.5 font-medium">User</th>
                <th class="px-4 py-2.5 font-medium">Roles</th>
                <th class="px-4 py-2.5 font-medium">Token</th>
                <th class="px-4 py-2.5 font-medium">Expiry</th>
                <th class="px-4 py-2.5 w-36"></th>
              </tr>
            </thead>
            <tbody>
              ${this.tokens.length
                ? this.tokens.map(token => this.renderTokenRow(token)).join('')
                : `<tr><td colspan="6" class="px-4 py-8 text-center opacity-35 text-xs">No agent keys</td></tr>`}
            </tbody>
          </table>
        </div>
      </div>
    `;
  }

  protected attachEventListeners(): void {
    this.querySelector('#agent-key-create-btn')?.addEventListener('click', () => this.handleCreate());
    this.querySelector('#agent-key-name')?.addEventListener('keydown', (event: Event) => {
      if ((event as KeyboardEvent).key === 'Enter') this.handleCreate();
    });
    this.querySelectorAll<HTMLElement>('.agent-key-reveal-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = Number(btn.dataset.id ?? 0);
        if (id > 0) this.handleReveal(id);
      });
    });
    this.querySelectorAll<HTMLElement>('.agent-key-copy-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = Number(btn.dataset.id ?? 0);
        const value = this.revealed.get(id);
        if (value) this.copyToClipboard(value, btn);
      });
    });
    this.querySelectorAll<HTMLElement>('.agent-key-delete-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const id = Number(btn.dataset.id ?? 0);
        if (id > 0) this.handleDelete(id);
      });
    });
  }

  protected detachEventListeners(): void {
    // innerHTML replacement removes event listeners.
  }

  private async initWithPermissions(): Promise<void> {
    const [canManage, canPersonal, canAccess] = await Promise.all([
      can('agentkeys.manage'),
      can('agentkeys.personal'),
      can('agentkeys.access'),
    ]);
    this.canManage = canManage;
    this.canPersonal = canPersonal;
    this.canAccess = canAccess;
    if (!canManage && !canPersonal && !canAccess) {
      this.innerHTML = `<div class="p-8 text-center opacity-40 text-sm">Insufficient permissions</div>`;
      return;
    }
    await this.loadData();
  }

  private async loadData(): Promise<void> {
    this.loading = true;
    this.error = '';
    try {
      const [tokens, users] = await Promise.all([
        listAgentTokens(),
        this.canManage ? listAgentTokenUsers() : Promise.resolve([]),
      ]);
      this.tokens = tokens;
      this.users = users;
    } catch (err: any) {
      this.error = err?.message ?? 'Failed to load agent keys';
    }
    this.loading = false;
    this.rerender();
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  private renderCreateControls(): string {
    const userOptions = this.canManage
      ? `<select id="agent-key-user" class="px-2 py-1.5 text-xs rounded border outline-none"
                 style="background:var(--input-bg);border-color:var(--border-color);color:inherit;max-width:180px;">
          ${this.users.map(user => `
            <option value="${user.id}" style="background:#ffffff;color:#111827;">${this.esc(user.displayName)} (${this.esc(user.loginName)})</option>
          `).join('')}
        </select>`
      : '';
    return `
      <div class="flex items-center gap-2 min-w-0">
        <input id="agent-key-name" type="text" placeholder="Name"
               class="px-2.5 py-1.5 text-xs rounded border outline-none"
               style="background:var(--input-bg);border-color:var(--border-color);color:inherit;width:150px;"
               maxlength="64">
        ${userOptions}
        <select id="agent-key-expiry" class="px-2 py-1.5 text-xs rounded border outline-none"
                style="background:var(--input-bg);border-color:var(--border-color);color:inherit;">
          <option value="30" style="background:#ffffff;color:#111827;">30 days</option>
          <option value="90" style="background:#ffffff;color:#111827;">90 days</option>
          <option value="365" style="background:#ffffff;color:#111827;">1 year</option>
          <option value="never" style="background:#ffffff;color:#111827;">No expiry</option>
        </select>
        <button id="agent-key-create-btn"
                class="px-3 py-1.5 text-xs font-medium rounded border transition-colors"
                style="background:color-mix(in srgb,var(--accent-color) 15%,transparent);
                       color:var(--accent-color);
                       border-color:color-mix(in srgb,var(--accent-color) 30%,transparent);"
                ${this.creating || (this.canManage && this.users.length === 0) ? 'disabled' : ''}>
          ${this.creating ? 'Creating...' : 'Create'}
        </button>
      </div>
    `;
  }

  private renderTokenRow(token: AgentToken): string {
    const raw = this.revealed.get(token.id);
    const canReveal = this.canManage || (this.canAccess && token.userId === this.currentUserID);
    const canDelete = this.canManage || (this.canPersonal && token.userId === this.currentUserID);
    const displayUser = token.userDisplayName || token.userLoginName || String(token.userId || '');
    const tokenText = raw || token.token || this.maskToken(token);
    return `
      <tr class="border-b" style="border-color:var(--border-color);">
        <td class="px-4 py-2.5 font-medium">${this.esc(token.name)}</td>
        <td class="px-4 py-2.5">
          <div class="text-xs">${this.esc(displayUser)}</div>
          ${token.userLoginName && token.userLoginName !== displayUser ? `
            <div class="text-xs opacity-45 font-mono">${this.esc(token.userLoginName)}</div>
          ` : ''}
        </td>
        <td class="px-4 py-2.5">
          <div class="flex flex-wrap gap-1">
            ${(token.roles ?? []).map(role => `
              <span class="text-xs px-1.5 py-0.5 rounded border"
                    style="border-color:color-mix(in srgb,var(--accent-color) 20%,transparent);
                           color:var(--accent-color);
                           background:color-mix(in srgb,var(--accent-color) 8%,transparent);">
                ${this.esc(role)}
              </span>
            `).join('')}
          </div>
        </td>
        <td class="px-4 py-2.5">
          <code class="block font-mono text-xs"
                style="max-width:320px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;color:${raw ? 'var(--content-text)' : 'var(--org-manager-muted-text, currentColor)'};"
                title="${this.esc(tokenText)}">${this.esc(tokenText)}</code>
        </td>
        <td class="px-4 py-2.5 text-xs opacity-60">${token.expiresAt ? new Date(token.expiresAt).toLocaleDateString() : 'No expiry'}</td>
        <td class="px-4 py-2.5">
          <div class="flex items-center justify-end gap-1.5">
            ${canReveal && !raw ? `<button class="agent-key-reveal-btn px-2 py-1 text-xs rounded border" data-id="${token.id}" style="border-color:var(--border-color);">Reveal</button>` : ''}
            ${raw ? `<button class="agent-key-copy-btn px-2 py-1 text-xs rounded border" data-id="${token.id}" style="border-color:var(--border-color);">Copy</button>` : ''}
            ${canDelete ? `<button class="agent-key-delete-btn px-2 py-1 text-xs rounded border" data-id="${token.id}" style="border-color:rgba(239,68,68,0.35);color:#f87171;">Delete</button>` : ''}
          </div>
        </td>
      </tr>
    `;
  }

  private async handleCreate(): Promise<void> {
    if (this.creating || (!this.canManage && !this.canPersonal)) return;
    const nameInput = this.querySelector<HTMLInputElement>('#agent-key-name');
    const name = nameInput?.value.trim() ?? '';
    if (!name) {
      if (nameInput) {
        nameInput.style.borderColor = '#f87171';
        nameInput.focus();
      }
      return;
    }

    const expires = this.querySelector<HTMLSelectElement>('#agent-key-expiry')?.value ?? '30';
    let expiresAt: string | undefined;
    if (expires !== 'never') {
      expiresAt = new Date(Date.now() + Number(expires) * 24 * 60 * 60 * 1000).toISOString();
    }

    const userID = this.canManage
      ? Number(this.querySelector<HTMLSelectElement>('#agent-key-user')?.value ?? 0)
      : undefined;
    this.creating = true;
    this.feedback = '';
    this.rerender();
    try {
      const created = await createAgentToken({ name, userId: userID || undefined, expiresAt });
      if (created.token) this.revealed.set(created.id, created.token);
      await this.loadData();
      this.feedback = 'Agent key created';
    } catch (err: any) {
      this.error = err?.message ?? 'Failed to create agent key';
    }
    this.creating = false;
    this.rerender();
  }

  private async handleReveal(id: number): Promise<void> {
    try {
      const token = await getAgentToken(id);
      if (token.token) {
        this.revealed.set(id, token.token);
        this.feedback = '';
      }
    } catch (err: any) {
      this.feedback = err?.message ?? 'Failed to retrieve agent key';
    }
    this.rerender();
  }

  private async handleDelete(id: number): Promise<void> {
    const confirmed = await showConfirm(
      'Delete this agent key?',
      { title: 'Delete Agent Key', confirmLabel: 'Delete', tone: 'danger' },
    );
    if (!confirmed) return;
    try {
      await deleteAgentToken(id);
      this.revealed.delete(id);
      await this.loadData();
      this.feedback = 'Agent key deleted';
    } catch (err: any) {
      this.feedback = err?.message ?? 'Failed to delete agent key';
      this.rerender();
    }
  }

  private maskToken(token: AgentToken): string {
    if (token.tokenPrefix && token.tokenLast4) return `${token.tokenPrefix}...${token.tokenLast4}`;
    return token.token ?? 'Hidden';
  }

  private async copyToClipboard(text: string, btn: HTMLElement): Promise<void> {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const input = document.createElement('textarea');
        input.value = text;
        input.setAttribute('readonly', '');
        input.style.position = 'fixed';
        input.style.left = '-9999px';
        document.body.appendChild(input);
        input.select();
        document.execCommand('copy');
        input.remove();
      }
      btn.textContent = 'Copied';
    } catch {
      btn.textContent = 'Failed';
    }
    setTimeout(() => {
      if (btn.isConnected) btn.textContent = 'Copy';
    }, 1500);
  }

  private esc(value: string): string {
    return String(value ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
}

customElements.define('agentkeys-widget', AgentKeysWidget);
