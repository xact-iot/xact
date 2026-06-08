import { getBootstrapAdminStatus, login, setBootstrapAdminPassword } from '../auth';

const STYLE = `
  :host {
    display: flex;
    align-items: center;
    justify-content: center;
    position: fixed;
    inset: 0;
    z-index: 9999;
    padding: 20px;
    background:
      radial-gradient(circle at top, color-mix(in srgb, var(--accent-color) 12%, transparent), transparent 38%),
      rgba(0, 0, 0, 0.62);
    color: var(--modal-text);
    font-family: var(--widget-font-family, system-ui, sans-serif);
    overflow: hidden;
  }

  :host::before {
    content: '';
    position: fixed;
    inset: 0;
    background:
      linear-gradient(180deg, color-mix(in srgb, var(--accent-color) 6%, transparent), transparent 28%),
      linear-gradient(90deg, color-mix(in srgb, var(--accent-color) 4%, transparent) 1px, transparent 1px),
      linear-gradient(color-mix(in srgb, var(--accent-color) 4%, transparent) 1px, transparent 1px);
    background-size: auto, 40px 40px, 40px 40px;
    pointer-events: none;
    z-index: 1;
    opacity: 0.7;
  }

  .card {
    position: relative;
    z-index: 2;
    width: min(460px, 100%);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    background: var(--modal-bg);
    color: var(--modal-text);
    box-shadow:
      0 24px 60px rgba(0, 0, 0, 0.45),
      inset 0 1px 0 rgba(255, 255, 255, 0.03);
    overflow: hidden;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border-color);
    background: color-mix(in srgb, var(--accent-color) 5%, transparent);
  }

  .logo-mark {
    width: 34px;
    height: 34px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .logo-mark img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  .logo-text {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .logo-name {
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--modal-text);
    letter-spacing: 0.08em;
    line-height: 1;
  }

  .logo-sub {
    font-size: 0.62rem;
    font-weight: 400;
    color: color-mix(in srgb, var(--modal-text) 55%, transparent);
    letter-spacing: 0.12em;
    text-transform: uppercase;
  }

  .card-body {
    padding: 20px;
  }

  .section-label {
    font-size: 0.65rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--accent-color);
    margin-bottom: 14px;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .section-label::after {
    content: '';
    flex: 1;
    height: 1px;
    background: color-mix(in srgb, var(--accent-color) 22%, var(--border-color));
  }

  .intro {
    margin-bottom: 18px;
    color: color-mix(in srgb, var(--modal-text) 72%, transparent);
    font-size: 0.72rem;
    line-height: 1.6;
  }

  .checking {
    padding: 16px 0 4px;
    color: color-mix(in srgb, var(--modal-text) 65%, transparent);
    font-size: 0.72rem;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .field {
    margin-bottom: 16px;
  }

  .field label {
    display: block;
    font-size: 0.68rem;
    font-weight: 500;
    color: color-mix(in srgb, var(--modal-text) 72%, transparent);
    margin-bottom: 8px;
  }

  .field input {
    width: 100%;
    box-sizing: border-box;
    background: var(--input-bg, rgba(255, 255, 255, 0.04));
    border: 1px solid var(--border-color);
    border-radius: 5px;
    color: var(--modal-text);
    font-family: inherit;
    font-size: 0.75rem;
    padding: 10px 12px;
    outline: none;
    transition: border-color 0.15s, box-shadow 0.15s;
  }

  .field input::placeholder {
    color: color-mix(in srgb, var(--modal-text) 35%, transparent);
  }

  .field input:focus {
    border-color: var(--accent-color);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent-color) 12%, transparent);
  }

  .submit-btn {
    width: 100%;
    margin-top: 10px;
    padding: 12px;
    border-radius: 5px;
    background: color-mix(in srgb, var(--accent-color) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent-color) 40%, var(--border-color));
    color: var(--accent-color);
    font-family: inherit;
    font-size: 0.72rem;
    font-weight: 500;
    letter-spacing: 0.08em;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s, opacity 0.15s;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
  }

  .submit-btn:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-color) 22%, transparent);
    border-color: var(--accent-color);
  }

  .submit-btn:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .spinner {
    width: 12px;
    height: 12px;
    border: 1px solid color-mix(in srgb, var(--accent-color) 30%, transparent);
    border-top-color: var(--accent-color);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .error-msg {
    margin-top: 14px;
    padding: 10px 12px;
    border: 1px solid color-mix(in srgb, var(--error-color) 35%, transparent);
    border-radius: 5px;
    background: var(--error-bg);
    color: var(--error-color);
    font-size: 0.68rem;
    line-height: 1.5;
    display: none;
  }

  .error-msg.visible {
    display: block;
  }

  .status-row {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 18px;
    padding-top: 14px;
    border-top: 1px solid var(--border-color);
  }

  .status-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--status-good-color, #22c55e);
    animation: pulse 2s ease-in-out infinite;
  }

  .status-text {
    font-size: 0.64rem;
    color: color-mix(in srgb, var(--modal-text) 52%, transparent);
    letter-spacing: 0.05em;
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.3; }
  }

  @media (max-width: 640px) {
    :host {
      padding: 12px;
    }

    .card-header,
    .card-body {
      padding-left: 16px;
      padding-right: 16px;
    }
  }
`;

export class LoginPage extends HTMLElement {
  private shadow: ShadowRoot;
  private mode: 'checking' | 'login' | 'setup' = 'checking';

  constructor() {
    super();
    this.shadow = this.attachShadow({ mode: 'open' });
  }

  connectedCallback() {
    this.render();
    this.checkBootstrapStatus();
  }

  private render() {
    const body = this.mode === 'checking' ? this.renderChecking() : this.renderForm();
    this.shadow.innerHTML = `
      <style>${STYLE}</style>
      <div class="card">
        <div class="card-header">
          <div style="display:flex;align-items:center;gap:12px;">
            <div class="logo-mark">
              <img src="/xact/logo.svg" alt="XACT logo" />
            </div>
            <div class="logo-text">
              <div class="logo-name">XACT</div>
              <div class="logo-sub">Authentication</div>
            </div>
          </div>
          <div class="logo-sub">Secure Access</div>
        </div>

        <div class="card-body">
          ${body}
          <div class="status-row">
            <div class="status-dot"></div>
            <span class="status-text">XACT Server ready</span>
          </div>
        </div>
      </div>
    `;
    this.shadow.querySelector('form')?.addEventListener('submit', this.handleSubmit.bind(this));
  }

  private renderChecking() {
    return `
      <div class="section-label">Authentication</div>
      <div class="checking"><div class="spinner"></div>Checking server state...</div>
      <div class="error-msg" id="error-msg"></div>
    `;
  }

  private renderForm() {
    if (this.mode === 'setup') {
      return `
        <div class="section-label">Set Admin Password</div>
        <div class="intro">Create the initial password for the admin account.</div>

        <form autocomplete="off" novalidate>
          <div class="field">
            <label for="password">New Password</label>
            <input id="password" name="password" type="password" autocomplete="new-password" />
          </div>
          <div class="field">
            <label for="confirm-password">Confirm Password</label>
            <input id="confirm-password" name="confirm-password" type="password" autocomplete="new-password" />
          </div>

          <button type="submit" class="submit-btn" id="submit-btn">
            <span id="btn-label">Set Password</span>
          </button>
        </form>

        <div class="error-msg" id="error-msg"></div>
      `;
    }

    return `
      <div class="section-label">Authentication Required</div>
      <div class="intro">Sign in with your XACT account to continue to the workspace.</div>

      <form autocomplete="off" novalidate>
        <div class="field">
          <label for="username">Login Name / Email</label>
          <input id="username" name="username" type="text"
                 autocomplete="username" spellcheck="false" />
        </div>
        <div class="field">
          <label for="password">Password</label>
          <input id="password" name="password" type="password" />
        </div>

        <button type="submit" class="submit-btn" id="submit-btn">
          <span id="btn-label">Sign In</span>
        </button>
      </form>

      <div class="error-msg" id="error-msg"></div>
    `;
  }

  private async checkBootstrapStatus() {
    try {
      const status = await getBootstrapAdminStatus();
      this.mode = status.setupRequired ? 'setup' : 'login';
      this.render();
      const firstInput = this.shadow.querySelector('input') as HTMLInputElement | null;
      firstInput?.focus();
    } catch {
      this.mode = 'login';
      this.render();
    }
  }

  private async handleSubmit(e: Event) {
    e.preventDefault();

    const passwordEl = this.shadow.getElementById('password') as HTMLInputElement;
    const submitBtn = this.shadow.getElementById('submit-btn') as HTMLButtonElement;
    const btnLabel = this.shadow.getElementById('btn-label')!;
    const errorMsg = this.shadow.getElementById('error-msg')!;

    const password = passwordEl.value;

    if (this.mode === 'setup') {
      const confirmPasswordEl = this.shadow.getElementById('confirm-password') as HTMLInputElement;
      const confirmPassword = confirmPasswordEl.value;
      if (password.length < 8) {
        this.showError('Password must be at least 8 characters.');
        passwordEl.focus();
        return;
      }
      if (password !== confirmPassword) {
        this.showError('Passwords do not match.');
        confirmPasswordEl.focus();
        return;
      }

      submitBtn.disabled = true;
      btnLabel.textContent = '';
      btnLabel.insertAdjacentHTML('beforeend', '<div class="spinner"></div>Setting password...');
      errorMsg.classList.remove('visible');

      try {
        await setBootstrapAdminPassword(password);
        this.dispatchEvent(new CustomEvent('auth-success', { bubbles: true, composed: true }));
      } catch (err: any) {
        this.showError(err?.message || 'Failed to set admin password.');
        submitBtn.disabled = false;
        btnLabel.textContent = 'Set Password';
        passwordEl.value = '';
        confirmPasswordEl.value = '';
        passwordEl.focus();
      }
      return;
    }

    const usernameEl = this.shadow.getElementById('username') as HTMLInputElement;
    const username = usernameEl.value.trim();

    if (!username) {
      this.showError('Login name or email is required.');
      usernameEl.focus();
      return;
    }

    // Loading state
    submitBtn.disabled = true;
    btnLabel.textContent = '';
    btnLabel.insertAdjacentHTML('beforeend', '<div class="spinner"></div>Authenticating...');
    errorMsg.classList.remove('visible');

    try {
      await login(username, password);
      // Dispatch success event for main.ts to handle
      this.dispatchEvent(new CustomEvent('auth-success', { bubbles: true, composed: true }));
    } catch (err: any) {
      this.showError(err?.message || 'Authentication failed. Check your credentials.');
      submitBtn.disabled = false;
      btnLabel.textContent = 'Sign In';
      passwordEl.value = '';
      passwordEl.focus();
    }
  }

  private showError(msg: string) {
    const errorMsg = this.shadow.getElementById('error-msg')!;
    errorMsg.textContent = `> ${msg}`;
    errorMsg.classList.add('visible');
  }
}

customElements.define('login-page', LoginPage);
