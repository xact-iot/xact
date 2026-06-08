import { BaseComponent } from './base-component';
import { showAlert } from './app-dialog';
import { fetchHealth, type HealthInfo } from '../api';
import packageInfo from '../../package.json';
import typescriptInfo from 'typescript/package.json';

type ConnectionState = 'checking' | 'online' | 'offline';

export class AppFooter extends BaseComponent {
  private static readonly HEALTH_CHECK_INTERVAL_MS = 10000;

  private statusText: string = 'Checking...';
  private connectionState: ConnectionState = 'checking';
  private healthCheckTimer: number | null = null;
  private appVersion: string = '';

  protected render(): void {
    this.className = 'flex items-center justify-center px-2 sm:px-6 text-xs border-t gap-1 sm:gap-2 whitespace-nowrap overflow-hidden transition-all duration-300';
    this.applyConnectionAppearance();

    this.innerHTML = `
      <span id="app-version" class="hidden sm:inline">${this.getAppVersionLabel()}</span>
      <span class="hidden sm:inline opacity-40">|</span>
      <span id="connection-status" class="${this.getConnectionStatusClass()}" role="status" aria-live="polite">
        <span id="status-dot" class="${this.getStatusDotClass()}"></span>
        <span id="status-text">${this.statusText}</span>
      </span>
      <span class="opacity-40">|</span>
      <a id="about-link" href="#" class="hover:underline" style="color: var(--footer-link-color);">About</a>
    `;
  }

  protected attachEventListeners(): void {
    this.querySelector('#about-link')?.addEventListener('click', this.handleAboutClick);
    void this.checkHealth();
    this.healthCheckTimer = window.setInterval(
      () => void this.checkHealth(),
      AppFooter.HEALTH_CHECK_INTERVAL_MS
    );
  }

  protected detachEventListeners(): void {
    this.querySelector('#about-link')?.removeEventListener('click', this.handleAboutClick);
    if (this.healthCheckTimer !== null) {
      window.clearInterval(this.healthCheckTimer);
      this.healthCheckTimer = null;
    }
  }

  private handleAboutClick = (e: Event): void => {
    e.preventDefault();
    void this.showAbout();
  };

  setConnectionStatus(status: string, isOnline: boolean): void {
    this.statusText = isOnline ? status : 'Server disconnected';
    this.connectionState = isOnline ? 'online' : 'offline';
    this.applyConnectionAppearance();
    const dot = this.querySelector('#status-dot');
    const text = this.querySelector('#status-text');
    const statusGroup = this.querySelector('#connection-status');
    if (dot) dot.className = this.getStatusDotClass();
    if (text) text.textContent = this.statusText;
    if (statusGroup) statusGroup.className = this.getConnectionStatusClass();
  }

  private setAppVersion(version: string | undefined): void {
    const nextVersion = (version ?? '').trim();
    if (!nextVersion) return;
    this.appVersion = nextVersion;
    const versionText = this.querySelector('#app-version');
    if (versionText) versionText.textContent = this.getAppVersionLabel();
  }

  private getAppVersionLabel(): string {
    return `2026 \u00a9 XACT${this.appVersion ? ` v${this.appVersion}` : ''}`;
  }

  private applyConnectionAppearance(): void {
    this.dataset.connectionState = this.connectionState;
    this.style.gridArea = 'footer';
    this.style.transition = 'background-color 180ms ease, color 180ms ease, border-color 180ms ease, box-shadow 180ms ease';

    if (this.connectionState === 'offline') {
      this.style.backgroundColor = '#f97316';
      this.style.backgroundImage = 'linear-gradient(90deg, #fbbf24 0%, #f97316 55%, #ea580c 100%)';
      this.style.color = '#111827';
      this.style.borderColor = '#fed7aa';
      this.style.boxShadow = '0 -4px 22px rgba(249, 115, 22, 0.45)';
      this.style.setProperty('--footer-link-color', '#111827');
      return;
    }

    this.style.backgroundColor = 'var(--footer-bg)';
    this.style.backgroundImage = '';
    this.style.color = 'var(--footer-text)';
    this.style.borderColor = 'var(--border-color)';
    this.style.boxShadow = '';
    this.style.setProperty('--footer-link-color', 'var(--accent-color)');
  }

  private getConnectionStatusClass(): string {
    const base = 'flex items-center gap-1.5';
    if (this.connectionState === 'offline') {
      return `${base} font-semibold uppercase`;
    }
    return base;
  }

  private getStatusDotClass(): string {
    if (this.connectionState === 'online') {
      return 'w-1.5 h-1.5 rounded-full bg-green-500';
    }
    if (this.connectionState === 'offline') {
      return 'w-2.5 h-2.5 rounded-full bg-red-700 ring-2 ring-white/80 animate-pulse';
    }
    return 'w-1.5 h-1.5 rounded-full bg-yellow-400 animate-pulse';
  }

  private async checkHealth(): Promise<void> {
    try {
      const health = await fetchHealth();
      const status = health.status?.toLowerCase();
      const isOnline = status === 'healthy' || status === 'ok';
      this.setAppVersion(health.appVersion);
      this.setConnectionStatus(isOnline ? 'Connected' : 'Disconnected', isOnline);
    } catch {
      this.setConnectionStatus('Disconnected', false);
    }
  }

  private async showAbout(): Promise<void> {
    let health: HealthInfo | null = null;
    try {
      health = await fetchHealth();
    } catch {
      health = null;
    }

    await showAlert(this.buildAboutMessage(health), {
      title: 'About XACT',
      confirmLabel: 'Close',
    });
  }

  private buildAboutMessage(health: HealthInfo | null): string {
    const appVersion = health?.appVersion || packageInfo.version || 'Unknown';
    const goVersion = health?.goVersion || 'Unavailable';
    const tsVersion = typescriptInfo.version || 'Unknown';
    const serverStatus = health?.status || 'Disconnected';
    const service = health?.service || 'Unavailable';
    const timezone = health?.timezone || 'Unavailable';
    const checkedAt = health?.timestamp
      ? new Date(health.timestamp * 1000).toLocaleString()
      : 'Unavailable';

    return [
      `Application: XACT ${appVersion}`,
      `Service: ${service}`,
      `Server status: ${serverStatus}`,
      `Go: ${goVersion}`,
      `TypeScript: ${tsVersion}`,
      `Server timezone: ${timezone}`,
      `Health checked: ${checkedAt}`,
    ].join('\n');
  }

}

customElements.define('app-footer', AppFooter);
