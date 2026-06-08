import { BaseComponent } from './base-component';
import { showConfirm } from './app-dialog';
import '../dashboards/dashboard-config-editor';
import '../dashboards/dashboard-container';
import type { DashboardContainer, DashboardMode } from '../dashboards/dashboard-container';

const SYSTEM_DASHBOARDS = new Set(['dashboard-config-editor']);

function isBlankDashboard(id: string): boolean {
  return id.startsWith('__blank__');
}

function createBlankElement(): HTMLElement {
  const el = document.createElement('div');
  el.style.cssText = 'display: flex; flex: 1; align-items: center; justify-content: center; opacity: 0.35; font-size: 0.875rem; color: var(--content-text);';
  el.textContent = 'Select a dashboard from the sidebar';
  return el;
}

export class AppContent extends BaseComponent {
  private dashboards: Map<string, HTMLElement> = new Map();
  private activeDashboard: string = 'dashboard-config-editor';

  protected render(): void {
    this.style.cssText = 'grid-area: content; display: flex; flex-direction: column; overflow: hidden;';
    this.style.backgroundColor = 'var(--content-bg)';
    this.style.color = 'var(--content-text)';
    this.showDashboard(this.activeDashboard);
  }

  private showDashboard(dashboardId: string): void {
    let dashboardElement = this.dashboards.get(dashboardId);

    if (!dashboardElement) {
      if (isBlankDashboard(dashboardId)) {
        dashboardElement = createBlankElement();
      } else if (SYSTEM_DASHBOARDS.has(dashboardId)) {
        // System dashboard: create by tag name
        try {
          dashboardElement = document.createElement(dashboardId);
          dashboardElement.style.cssText = 'display: block; flex: 1; overflow: auto; padding: 1.5rem;';
        } catch {
          return;
        }
      } else {
        // User dashboard: create a dashboard-container
        dashboardElement = document.createElement('dashboard-container');
        // Height is managed by dashboard-container's own render() via flex styles
      }
      this.dashboards.set(dashboardId, dashboardElement);
    }

    const title = this.getDashboardTitle(dashboardId);
    this.emit('dashboard-shown', { dashboardId, title });

    this.innerHTML = '';
    this.appendChild(dashboardElement);
    this.activeDashboard = dashboardId;

    if (!isBlankDashboard(dashboardId) && !SYSTEM_DASHBOARDS.has(dashboardId) && dashboardElement.tagName === 'DASHBOARD-CONTAINER') {
      (dashboardElement as DashboardContainer).loadDashboard(dashboardId);
    }
  }

  private getDashboardTitle(dashboardId: string): string {
    if (isBlankDashboard(dashboardId)) return 'New Tab';
    switch (dashboardId) {
      case 'dashboard-config-editor':
        return 'Dashboards';
      default:
        return dashboardId;
    }
  }

  protected attachEventListeners(): void {}
  protected detachEventListeners(): void {}

  getActiveDashboardContainer(): DashboardContainer | null {
    const el = this.dashboards.get(this.activeDashboard);
    return el?.tagName === 'DASHBOARD-CONTAINER' ? el as DashboardContainer : null;
  }

  toggleEditMode(): void {
    this.getActiveDashboardContainer()?.toggleEditMode();
  }

  toggleInspectMode(): void {
    this.getActiveDashboardContainer()?.toggleInspectMode();
  }

  setDashboardMode(mode: DashboardMode): void {
    this.getActiveDashboardContainer()?.setDashboardMode(mode);
  }

  async switchToDashboard(dashboardId: string): Promise<boolean> {
    if (dashboardId === this.activeDashboard && !isBlankDashboard(dashboardId)) return true;

    const current = this.dashboards.get(this.activeDashboard);
    if (current && current.tagName === 'DASHBOARD-CONTAINER') {
      const container = current as DashboardContainer;
      if (container.hasUnsavedChanges()) {
        const shouldDiscard = await showConfirm('You have unsaved changes. Discard and switch dashboards?', {
          title: 'Unsaved changes',
          confirmLabel: 'Discard changes',
          cancelLabel: 'Stay here',
          tone: 'danger',
        });
        if (!shouldDiscard) return false;
      }
    }

    this.showDashboard(dashboardId);
    return true;
  }
}

customElements.define('app-content', AppContent);
