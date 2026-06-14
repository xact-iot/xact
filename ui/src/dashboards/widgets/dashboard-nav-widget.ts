import { BaseComponent } from '../../components/base-component';
import { listDashboards } from '../../api';
import type { DashboardMeta } from '../../api';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { registerWidgetType } from './widget-registry';
import type { PropertyField, WidgetPropertiesDialog } from './widget-properties-dialog';

interface Config {
  headerText: string;
  targetDashboardId: string;
  deviceParentPath: string;
}

const DEFAULT_CONFIG: Config = {
  headerText: '',
  targetDashboardId: '',
  deviceParentPath: '',
};

export class DashboardNavWidget extends BaseComponent {
  private static dashboardCache: DashboardMeta[] = [];
  private static dashboardLoadPromise: Promise<DashboardMeta[]> | null = null;

  private config: Config = { ...DEFAULT_CONFIG };
  private dashboards: DashboardMeta[] = [];
  private devices: string[] = [];
  private treeUnsub: (() => void) | null = null;
  private dashboardsLoaded = false;
  private configDialog: WidgetPropertiesDialog | null = null;
  private configSaved = false;
  private deviceFilter = '';
  private deviceDropdownOpen = false;
  private highlightedDeviceIndex = -1;

  setConfig(c: Partial<Config> & Record<string, any>): void {
    const previousParent = this.config.deviceParentPath;
    this.config = {
      ...this.config,
      ...c,
      targetDashboardId: String(c.targetDashboardId ?? this.config.targetDashboardId ?? ''),
      deviceParentPath: String(c.deviceParentPath ?? this.config.deviceParentPath ?? '').trim(),
    };

    if (this.config.deviceParentPath !== previousParent) {
      this.refreshDevices();
    }

    this.rerender();
  }

  getConfig(): Config {
    return { ...this.config };
  }

  private dashboardSelectOptions(): Array<{ value: string; label: string }> {
    const uniqueDashboards = [...this.dashboards
      .filter(dashboard => !dashboard.isCategory)
      .reduce((byName, dashboard) => {
        const key = dashboard.name.trim().toLowerCase() || String(dashboard.id);
        const existing = byName.get(key);
        if (!existing || String(dashboard.id) === this.config.targetDashboardId) byName.set(key, dashboard);
        return byName;
      }, new Map<string, DashboardMeta>())
      .values()];

    return [
      { value: '', label: 'Current dashboard' },
      ...uniqueDashboards.map(dashboard => ({ value: String(dashboard.id), label: dashboard.name })),
    ];
  }

  async openConfig(): Promise<void> {
    await this.loadDashboards();
    this.cleanupConfigDialog();
    this.configSaved = false;

    const dialog = document.createElement('widget-properties-dialog') as WidgetPropertiesDialog;
    document.body.appendChild(dialog);
    this.configDialog = dialog;

    dialog.addEventListener('properties-updated', ((e: CustomEvent) => {
      e.stopPropagation();
      this.configSaved = true;
      this.setConfig({ ...(this.config || {}), ...(e.detail?.config || {}) });
      this.emit('widget-config-save', { config: this.getConfig(), forceDirty: true });
      this.cleanupConfigDialog();
    }) as EventListener, { once: true });

    dialog.addEventListener('properties-closed', ((e: CustomEvent) => {
      e.stopPropagation();
      if (!this.configSaved) this.emit('widget-config-close');
      this.cleanupConfigDialog();
    }) as EventListener, { once: true });

    dialog.open('dashboard-nav-widget', this.getPropertySchema(), this.getConfig(), false, 'Dashboard Nav');
  }

  getPropertySchema(): PropertyField[] {
    return [
      {
        name: 'headerText',
        type: 'string',
        label: 'Header text',
        default: '',
      },
      {
        name: 'targetDashboardId',
        type: 'select',
        label: 'Target dashboard',
        description: 'Stored as the dashboard ID so renames do not break navigation.',
        default: '',
        context: {
          options: this.dashboardSelectOptions(),
        },
      },
      {
        name: 'deviceParentPath',
        type: 'path',
        label: 'Device selection list parent node',
        description: 'Child node names are the devices in the list.',
        default: '',
      },
    ];
  }

  connectedCallback(): void {
    super.connectedCallback();
    this.loadDashboards();
    this.refreshDevices();
  }

  disconnectedCallback(): void {
    this.deviceDropdownOpen = false;
    this.updateDropdownOverflow();
    super.disconnectedCallback();
    this.treeUnsub?.();
    this.treeUnsub = null;
    this.cleanupConfigDialog();
  }

  protected render(): void {
    this.updateCardTitle();
    const hasDeviceList = this.config.deviceParentPath.length > 0;
    const targetLabel = this.targetDashboardLabel();
    const selectedDevice = getUiStore().get('deviceName') || '';

    this.innerHTML = `
      <style>
        .dnw-root {
          height: 100%;
          display: flex;
          align-items: flex-start;
          justify-content: center;
          padding: 0.75rem;
          color: var(--content-text);
          font-family: inherit;
          box-sizing: border-box;
        }
        .dnw-control {
          width: min(100%, 20rem);
          display: flex;
          align-items: stretch;
          gap: 0.5rem;
        }
        .dnw-combo-input,
        .dnw-button {
          width: 100%;
          min-width: 0;
          border: 1px solid var(--widget-border);
          border-radius: 6px;
          background: color-mix(in srgb, var(--widget-bg) 88%, var(--accent-color));
          color: var(--content-text);
          font: inherit;
          font-size: 0.875rem;
          line-height: 1.2;
          min-height: 2.5rem;
          padding: 0.55rem 0.75rem;
          outline: none;
          box-sizing: border-box;
        }
        .dnw-combo {
          position: relative;
          width: min(100%, 20rem);
        }
        .dnw-combo-input {
          padding-right: 2.25rem;
        }
        .dnw-combo-input::placeholder {
          color: var(--footer-text);
          opacity: 0.95;
        }
        .dnw-combo-input:focus,
        .dnw-button:focus {
          border-color: var(--accent-color);
          box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent-color) 25%, transparent);
        }
        .dnw-combo-toggle {
          position: absolute;
          top: 1px;
          right: 1px;
          width: 2rem;
          min-height: calc(2.5rem - 2px);
          border: 0;
          border-left: 1px solid color-mix(in srgb, var(--widget-border) 75%, transparent);
          border-radius: 0 5px 5px 0;
          background: transparent;
          color: var(--content-text);
          cursor: pointer;
          opacity: 0.7;
        }
        .dnw-combo-toggle:hover,
        .dnw-combo-toggle:focus {
          opacity: 1;
          color: var(--accent-color);
          outline: none;
        }
        .dnw-options {
          display: none;
          position: absolute;
          top: calc(100% + 4px);
          left: 0;
          z-index: 30;
          min-width: 100%;
          width: max-content;
          max-width: min(48rem, calc(100vw - 2rem));
          max-height: min(18rem, 60vh);
          overflow-y: auto;
          overflow-x: hidden;
          border: 1px solid var(--widget-border);
          border-radius: 6px;
          background: var(--widget-bg);
          box-shadow: 0 12px 28px rgba(0, 0, 0, 0.35);
          padding: 0.25rem;
        }
        .dnw-options.open {
          display: block;
        }
        .grid-stack-item.dnw-dropdown-open {
          z-index: 5000 !important;
        }
        .grid-stack-item.dnw-dropdown-open > .grid-stack-item-content,
        .grid-stack-item.dnw-dropdown-open .widget-card,
        .grid-stack-item.dnw-dropdown-open .widget-body {
          overflow: visible !important;
        }
        .dnw-option,
        .dnw-no-results {
          padding: 0.45rem 0.55rem;
          border-radius: 4px;
          font-size: 0.8125rem;
          line-height: 1.25;
        }
        .dnw-option {
          cursor: pointer;
          white-space: nowrap;
          display: flex;
          align-items: baseline;
          gap: 0.5rem;
        }
        .dnw-option:hover,
        .dnw-option.highlighted {
          color: var(--accent-color);
          background: color-mix(in srgb, var(--accent-color) 14%, transparent);
        }
        .dnw-option.selected {
          font-weight: 700;
          background: color-mix(in srgb, var(--accent-color) 20%, transparent);
        }
        .dnw-option-name {
          flex: 0 0 auto;
          white-space: nowrap;
        }
        .dnw-option-description {
          flex: 1 1 auto;
          white-space: nowrap;
          color: var(--footer-text);
          font-size: 0.75rem;
          font-weight: 400;
          opacity: 0.95;
        }
        .dnw-no-results {
          color: var(--footer-text);
          text-align: center;
        }
        .dnw-button {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          gap: 0.5rem;
          cursor: pointer;
          font-weight: 600;
          background: color-mix(in srgb, var(--accent-color) 16%, var(--widget-bg));
          transition: border-color 0.12s, color 0.12s, background-color 0.12s;
        }
        .dnw-button:hover {
          color: var(--accent-color);
          border-color: var(--accent-color);
          background: color-mix(in srgb, var(--accent-color) 22%, var(--widget-bg));
        }
        .dnw-button:disabled,
        .dnw-combo-input:disabled {
          cursor: default;
          opacity: 0.55;
        }
        .dnw-empty {
          width: 100%;
          text-align: center;
          color: var(--footer-text);
          font-size: 0.75rem;
          letter-spacing: 0;
          text-transform: uppercase;
        }
      </style>
      <div class="dnw-root">
        ${hasDeviceList ? this.renderDeviceSelect(selectedDevice) : `
          <div class="dnw-control">
            <button class="dnw-button" type="button" ${this.dashboardsLoaded || !this.config.targetDashboardId ? '' : 'disabled'}>
              <span>${this.esc(targetLabel)}</span>
            </button>
          </div>
        `}
      </div>
    `;
  }

  protected attachEventListeners(): void {
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.addEventListener('focus', this.handleDeviceInputFocus);
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.addEventListener('input', this.handleDeviceFilterInput);
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.addEventListener('keydown', this.handleDeviceInputKeyDown);
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.addEventListener('blur', this.handleDeviceInputBlur);
    this.querySelector<HTMLButtonElement>('.dnw-combo-toggle')?.addEventListener('click', this.handleDeviceToggleClick);
    this.querySelector<HTMLElement>('.dnw-options')?.addEventListener('mousedown', this.handleDeviceOptionsMouseDown);
    this.querySelector<HTMLElement>('.dnw-options')?.addEventListener('click', this.handleDeviceOptionClick);
    this.querySelector<HTMLButtonElement>('.dnw-button')?.addEventListener('click', this.handleButtonClick);
    document.addEventListener('mousedown', this.handleDocumentMouseDown);
  }

  protected detachEventListeners(): void {
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.removeEventListener('focus', this.handleDeviceInputFocus);
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.removeEventListener('input', this.handleDeviceFilterInput);
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.removeEventListener('keydown', this.handleDeviceInputKeyDown);
    this.querySelector<HTMLInputElement>('.dnw-combo-input')?.removeEventListener('blur', this.handleDeviceInputBlur);
    this.querySelector<HTMLButtonElement>('.dnw-combo-toggle')?.removeEventListener('click', this.handleDeviceToggleClick);
    this.querySelector<HTMLElement>('.dnw-options')?.removeEventListener('mousedown', this.handleDeviceOptionsMouseDown);
    this.querySelector<HTMLElement>('.dnw-options')?.removeEventListener('click', this.handleDeviceOptionClick);
    this.querySelector<HTMLButtonElement>('.dnw-button')?.removeEventListener('click', this.handleButtonClick);
    document.removeEventListener('mousedown', this.handleDocumentMouseDown);
  }

  private renderDeviceSelect(selectedDevice: string): string {
    if (this.devices.length === 0) {
      return `<div class="dnw-empty">No devices</div>`;
    }

    const inputValue = this.deviceDropdownOpen ? this.deviceFilter : selectedDevice;
    const placeholder = selectedDevice || 'Select device';

    return `
      <div class="dnw-combo" data-device-combo>
        <input class="dnw-combo-input" type="text" role="combobox"
               aria-label="Select device" aria-autocomplete="list"
               aria-expanded="${this.deviceDropdownOpen ? 'true' : 'false'}"
               aria-controls="dnw-options"
               autocomplete="off" spellcheck="false"
               placeholder="${this.esc(placeholder)}" value="${this.esc(inputValue)}">
        <button class="dnw-combo-toggle" type="button" title="Show devices" aria-label="Show devices">&#9662;</button>
        <div id="dnw-options" class="dnw-options${this.deviceDropdownOpen ? ' open' : ''}" role="listbox">
          ${this.renderDeviceOptions(selectedDevice)}
        </div>
      </div>
    `;
  }

  private renderDeviceOptions(selectedDevice: string): string {
    const filtered = this.filteredDevices();
    if (filtered.length === 0) {
      return `<div class="dnw-no-results">No matching devices</div>`;
    }

    return filtered.map((name, idx) => {
      const description = this.deviceDescription(name);
      const label = description ? `${name} - ${description}` : name;
      const classes = [
        'dnw-option',
        name === selectedDevice ? 'selected' : '',
        idx === this.highlightedDeviceIndex ? 'highlighted' : '',
      ].filter(Boolean).join(' ');
      return `<div class="${classes}" role="option" data-device="${this.esc(name)}"
                   aria-selected="${name === selectedDevice ? 'true' : 'false'}"
                   title="${this.esc(label)}">
                <span class="dnw-option-name">${this.esc(name)}</span>
                ${description ? `<span class="dnw-option-description">${this.esc(description)}</span>` : ''}
              </div>`;
    }).join('');
  }

  private filteredDevices(): string[] {
    const terms = this.deviceFilter.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (terms.length === 0) return this.devices;
    return this.devices.filter(name => {
      const description = this.deviceDescription(name);
      const lower = `${name} ${description}`.toLowerCase();
      return terms.every(term => lower.includes(term));
    });
  }

  private deviceDescription(name: string): string {
    const parentPath = this.config.deviceParentPath.trim().replace(/\.$/, '');
    if (!parentPath || !name) return '';

    const store = getMirrorStore();
    const relativePath = `${parentPath}.${name}`;
    const paths = [...new Set([relativePath, store.toAbsolute(relativePath)])];

    for (const path of paths) {
      const value = (store as any).resolveTagReference?.(`${path}:description`);
      if (typeof value === 'string' && value.trim()) return value.trim();
    }

    return '';
  }

  private async loadDashboards(): Promise<void> {
    try {
      this.dashboards = await DashboardNavWidget.fetchDashboards();
    } catch (err) {
      console.error('DashboardNavWidget: failed to load dashboards:', err);
      this.dashboards = [];
    } finally {
      this.dashboardsLoaded = true;
      if (this.isConnected) this.rerender();
    }
  }

  private static fetchDashboards(): Promise<DashboardMeta[]> {
    if (DashboardNavWidget.dashboardCache.length > 0) {
      return Promise.resolve(DashboardNavWidget.dashboardCache);
    }
    if (!DashboardNavWidget.dashboardLoadPromise) {
      DashboardNavWidget.dashboardLoadPromise = listDashboards()
        .then(dashboards => {
          DashboardNavWidget.dashboardCache = dashboards;
          return dashboards;
        })
        .finally(() => {
          DashboardNavWidget.dashboardLoadPromise = null;
        });
    }
    return DashboardNavWidget.dashboardLoadPromise;
  }

  private cleanupConfigDialog(): void {
    this.configDialog?.remove();
    this.configDialog = null;
  }

  private refreshDevices(): void {
    this.treeUnsub?.();
    this.treeUnsub = null;
    const parentPath = this.config.deviceParentPath;
    if (!parentPath) {
      this.devices = [];
      return;
    }

    const store = getMirrorStore();
    const absParent = store.toAbsolute(parentPath);
    this.devices = store.listChildrenNames(absParent).sort((a, b) => a.localeCompare(b));
    this.treeUnsub = store.subscribeToTreeChanges(absParent, (changedPath) => {
      const expectedDepth = absParent.split('.').length + 1;
      if (changedPath.split('.').length !== expectedDepth) return;
      this.devices = store.listChildrenNames(absParent).sort((a, b) => a.localeCompare(b));
      this.rerender();
    });
  }

  private targetDashboardLabel(): string {
    if (!this.config.targetDashboardId) return 'Current dashboard';
    return this.dashboards.find(dashboard => String(dashboard.id) === this.config.targetDashboardId)?.name || 'Dashboard';
  }

  private gotoTarget(): void {
    const selectedDevice = getUiStore().get('deviceName') || '';
    const parentPath = this.config.deviceParentPath.trim().replace(/\.$/, '');
    const devicePath = selectedDevice ? (parentPath ? `${parentPath}.${selectedDevice}` : selectedDevice) : undefined;
    this.emit('dashboard-open', {
      dashboard: this.config.targetDashboardId,
      id: this.config.targetDashboardId,
      title: this.targetDashboardLabel(),
      devicePath,
    });
  }

  private selectDevice(deviceName: string): void {
    const ui = getUiStore();
    ui.set('deviceName', deviceName);
    const deviceType = this.config.deviceParentPath.split('.').filter(Boolean).pop() || '';
    if (deviceType) ui.set('deviceType', deviceType);
    this.deviceFilter = '';
    this.deviceDropdownOpen = false;
    this.highlightedDeviceIndex = -1;
    this.patchDeviceDropdown();
    this.gotoTarget();
  }

  private patchDeviceDropdown(): void {
    const selectedDevice = getUiStore().get('deviceName') || '';
    const input = this.querySelector<HTMLInputElement>('.dnw-combo-input');
    const options = this.querySelector<HTMLElement>('.dnw-options');
    if (!input || !options) {
      this.updateDropdownOverflow();
      return;
    }

    input.setAttribute('aria-expanded', this.deviceDropdownOpen ? 'true' : 'false');
    if (this.deviceDropdownOpen) {
      if (input.value !== this.deviceFilter) input.value = this.deviceFilter;
    } else if (input.value !== selectedDevice) {
      input.value = selectedDevice;
    }
    input.placeholder = selectedDevice || 'Select device';
    options.classList.toggle('open', this.deviceDropdownOpen);
    options.innerHTML = this.renderDeviceOptions(selectedDevice);
    this.updateDropdownOverflow();
  }

  private openDeviceDropdown(): void {
    this.deviceDropdownOpen = true;
    const filtered = this.filteredDevices();
    const selectedDevice = getUiStore().get('deviceName') || '';
    const selectedIndex = filtered.indexOf(selectedDevice);
    this.highlightedDeviceIndex = selectedIndex >= 0 ? selectedIndex : (filtered.length > 0 ? 0 : -1);
    this.patchDeviceDropdown();
  }

  private closeDeviceDropdown(): void {
    this.deviceDropdownOpen = false;
    this.deviceFilter = '';
    this.highlightedDeviceIndex = -1;
    this.patchDeviceDropdown();
  }

  private handleDeviceInputFocus = (): void => {
    const input = this.querySelector<HTMLInputElement>('.dnw-combo-input');
    this.deviceFilter = input?.value || getUiStore().get('deviceName') || '';
    this.openDeviceDropdown();
  };

  private handleDeviceFilterInput = (e: Event): void => {
    this.deviceFilter = (e.currentTarget as HTMLInputElement).value;
    this.deviceDropdownOpen = true;
    this.highlightedDeviceIndex = this.filteredDevices().length > 0 ? 0 : -1;
    this.patchDeviceDropdown();
  };

  private handleDeviceInputKeyDown = (e: KeyboardEvent): void => {
    const filtered = this.filteredDevices();
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        if (!this.deviceDropdownOpen) {
          this.openDeviceDropdown();
          return;
        }
        if (filtered.length > 0) {
          this.highlightedDeviceIndex = (this.highlightedDeviceIndex + 1 + filtered.length) % filtered.length;
          this.patchDeviceDropdown();
          this.scrollHighlightedOptionIntoView();
        }
        break;
      case 'ArrowUp':
        e.preventDefault();
        if (!this.deviceDropdownOpen) {
          this.openDeviceDropdown();
          return;
        }
        if (filtered.length > 0) {
          this.highlightedDeviceIndex = (this.highlightedDeviceIndex - 1 + filtered.length) % filtered.length;
          this.patchDeviceDropdown();
          this.scrollHighlightedOptionIntoView();
        }
        break;
      case 'Enter':
        if (!this.deviceDropdownOpen) return;
        e.preventDefault();
        if (filtered.length === 0) return;
        this.selectDevice(filtered[Math.max(0, this.highlightedDeviceIndex)]);
        break;
      case 'Escape':
        if (this.deviceDropdownOpen) {
          e.preventDefault();
          this.closeDeviceDropdown();
        }
        break;
    }
  };

  private handleDeviceInputBlur = (): void => {
    setTimeout(() => {
      if (!this.contains(document.activeElement)) this.closeDeviceDropdown();
    }, 0);
  };

  private handleDeviceToggleClick = (): void => {
    const input = this.querySelector<HTMLInputElement>('.dnw-combo-input');
    if (this.deviceDropdownOpen) {
      this.closeDeviceDropdown();
    } else {
      this.deviceFilter = '';
      this.openDeviceDropdown();
      input?.focus();
    }
  };

  private handleDeviceOptionsMouseDown = (e: Event): void => {
    if ((e.target as HTMLElement).closest('.dnw-option')) {
      e.preventDefault();
    }
  };

  private handleDeviceOptionClick = (e: Event): void => {
    const option = (e.target as HTMLElement).closest<HTMLElement>('.dnw-option');
    const deviceName = option?.dataset.device;
    if (deviceName) this.selectDevice(deviceName);
  };

  private handleDocumentMouseDown = (e: MouseEvent): void => {
    if (!this.deviceDropdownOpen) return;
    if (this.contains(e.target as Node)) return;
    this.closeDeviceDropdown();
  };

  private scrollHighlightedOptionIntoView(): void {
    this.querySelector<HTMLElement>('.dnw-option.highlighted')?.scrollIntoView({ block: 'nearest' });
  }

  private updateDropdownOverflow(): void {
    this.closest('.grid-stack-item')?.classList.toggle('dnw-dropdown-open', this.deviceDropdownOpen);
  }

  private handleButtonClick = (): void => {
    this.gotoTarget();
  };

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (card && typeof card.setTitle === 'function') {
      card.setTitle(this.config.headerText ?? '');
    }
  }

  private rerender(): void {
    this.updateDropdownOverflow();
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
    this.updateDropdownOverflow();
  }

  private esc(s: string): string {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
}

registerWidgetType({
  type: 'dashboard-nav-widget',
  name: 'Dashboard Nav',
  icon: '📊',
  category: 'Layout',
  defaultW: 6,
  defaultH: 4,
  minW: 3,
  minH: 3,
});

customElements.define('dashboard-nav-widget', DashboardNavWidget);
