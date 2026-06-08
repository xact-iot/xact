import { BaseComponent } from '../../components/base-component';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { registerWidgetType } from './widget-registry';
import './sparkline-widget';
import type { SparklineWidget } from './sparkline-widget';
import type { PropertyField } from './widget-properties-dialog';
import { resolveMetricTagPath } from './tag-path-resolver';
import { getIconSVG, isIconSetLoaded, loadIconSet } from '../../utils/icons';

// ── Config ────────────────────────────────────────────────────────────────────

interface Config {
  headerText: string;
  tagPrefix: string;
  tagPath: string;
  fontSize: number;
  decimals: number;
  showIcon: boolean;
  icon: string;
  iconSize: number;
  iconColor: string;
  showSparkline: boolean;
  refreshInterval: number;   // seconds between sparkline auto-refresh (0 = off)
  colorBandsEnabled: boolean;
  colorBandsThreshold1: number;
  colorBandsThreshold2: number;
  colorBandsColor1: string;
  colorBandsColor2: string;
  colorBandsColor3: string;
}

const DEFAULT_CONFIG: Config = {
  headerText: 'Metric',
  tagPrefix: '',
  tagPath: '',
  fontSize: 56,
  decimals: 2,
  showIcon: false,
  icon: 'mdi:car',
  iconSize: 48,
  iconColor: '',
  showSparkline: true,
  refreshInterval: 0,
  colorBandsEnabled: false,
  colorBandsThreshold1: 50,
  colorBandsThreshold2: 80,
  colorBandsColor1: '#22c55e',
  colorBandsColor2: '#f59e0b',
  colorBandsColor3: '#ef4444',
};

// ── Widget ────────────────────────────────────────────────────────────────────

export class BigNumberWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG };
  private currentValue: any = null;
  private subscriptionActive = false;
  private _storeUnsub: (() => void) | null = null;
  private _deviceNameUnsub: (() => void) | null = null;
  private _loadingIconPrefix: string | null = null;

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    const prevPath = this.config.tagPath;
    const prevPrefix = this.config.tagPrefix;
    const showSparkline = Boolean(c.showSparkline ?? this.config.showSparkline);
    this.config = {
      ...this.config,
      ...c,
      showSparkline,
      refreshInterval: showSparkline ? Number(c.refreshInterval ?? this.config.refreshInterval) || 0 : 0,
      fontSize: Number(c.fontSize ?? this.config.fontSize) || DEFAULT_CONFIG.fontSize,
      decimals: this.normalizeDecimals(c.decimals ?? this.config.decimals),
      iconSize: Number(c.iconSize ?? this.config.iconSize) || DEFAULT_CONFIG.iconSize,
    };

    if (this.config.tagPath !== prevPath || this.config.tagPrefix !== prevPrefix) {
      this.currentValue = null;
    }

    this.ensureIconLoaded();
    this.rerender();
  }

  private resolveTagPath(): string {
    return resolveMetricTagPath(this.config.tagPrefix, this.config.tagPath);
  }

  getPropertySchema(): PropertyField[] {
    const inArray = !!(this.config as any).arrayElementPath;
    return [
      {
        name: 'headerText',
        type: 'string',
        label: 'Header text',
        default: 'Metric',
      },
      ...(!inArray ? [{
        name: 'tagPrefix',
        type: 'path' as const,
        label: 'Tag prefix (use * for dashboard device name)',
        description: 'e.g. NASA.* - * is replaced with the current device name',
        default: '',
        context: { includeLeaves: false },
      }] : []),
      {
        name: 'tagPath',
        type: 'path',
        label: 'Tag path',
        description: 'Relative path when using tag prefix, or full absolute path',
        default: '',
        context: { includeLeaves: true, rootFromField: 'tagPrefix', stripBrowseRoot: true },
      },
      {
        name: 'fontSize',
        type: 'number',
        label: 'Font size (px)',
        default: 56,
      },
      {
        name: 'decimals',
        type: 'number',
        label: 'Decimal places',
        default: 2,
        context: { min: 0, max: 10, step: 1 },
      },
      {
        name: 'showIcon',
        type: 'boolean',
        label: 'Show icon',
        default: false,
      },
      {
        name: 'icon',
        type: 'icon',
        label: 'Icon',
        default: 'mdi:car',
      },
      {
        name: 'iconSize',
        type: 'number',
        label: 'Icon size (px)',
        default: 48,
      },
      {
        name: 'iconColor',
        type: 'string',
        label: 'Icon color',
        description: 'Optional CSS color. Leave blank to match the number color.',
        default: '',
      },
      {
        name: 'showSparkline',
        type: 'boolean',
        label: 'Show sparkline',
        default: true,
      },
      {
        name: 'refreshInterval', type: 'select', label: 'Refresh interval', default: '0',
        context: { disabledUnlessField: 'showSparkline', options: [
          { value: '0',   label: 'Off' },
          { value: '30',  label: '30 s' },
          { value: '60',  label: '1 min' },
          { value: '300', label: '5 min' },
          { value: '600', label: '10 min' },
        ]},
      },
      {
        name: 'colorBandsEnabled',
        type: 'boolean',
        label: 'Enable color bands',
        default: false,
      },
      {
        name: 'colorBandsThreshold1',
        type: 'number',
        label: 'Color band threshold 1',
        description: 'Values below this use Band 1 color',
        default: 50,
      },
      {
        name: 'colorBandsThreshold2',
        type: 'number',
        label: 'Color band threshold 2',
        description: 'Values below this (and ≥ threshold 1) use Band 2 color',
        default: 80,
      },
      {
        name: 'colorBandsColor1',
        type: 'color',
        label: 'Band 1 color (below threshold 1)',
        default: '#22c55e',
      },
      {
        name: 'colorBandsColor2',
        type: 'color',
        label: 'Band 2 color (between thresholds)',
        default: '#f59e0b',
      },
      {
        name: 'colorBandsColor3',
        type: 'color',
        label: 'Band 3 color (above threshold 2)',
        default: '#ef4444',
      },
    ];
  }

  // ── Lifecycle ───────────────────────────────────────────────────────────────

  protected render(): void {
    const { fontSize, showSparkline } = this.config;
    const fullPath = this.resolveTagPath();
    const color = this.resolveColor();
    const iconHtml = this.renderIconHtml(color);
    const displayValue = this.formatValue(this.currentValue);
    const units = fullPath ? (getMirrorStore().getNodeShared(getMirrorStore().baseTagPath(fullPath))?.units ?? '') : '';
    const dm = this.getDeviceAndMetric();
    const uid = this.getAttribute('id') || 'bn0';

    this.innerHTML = `
      <div style="
        display:flex; flex-direction:column; height:100%;
        padding:0.75rem 0 0.625rem; box-sizing:border-box;
        overflow:hidden;
      ">
        <!-- Value -->
        <div style="flex:1; display:flex; align-items:center; justify-content:center; min-height:0; overflow:hidden;">
          <div style="display:flex; align-items:center; gap:0.45em; overflow:hidden; max-width:100%;">
            ${iconHtml ? `<div id="bn-icon" style="display:flex;align-items:center;justify-content:center;flex-shrink:0;color:${this.iconColor(color)};">${iconHtml}</div>` : ''}
            <div id="bn-val" style="
              font-family:ui-monospace,'Cascadia Code','SF Mono','Menlo','Consolas',monospace;
              font-size:${fontSize}px; font-weight:300; line-height:1;
              color:${color}; transition:color 0.35s ease;
              letter-spacing:-0.02em; white-space:nowrap;
              overflow:hidden; text-overflow:ellipsis;
            ">${displayValue !== null ? this.esc(displayValue) : '<span style="opacity:0.18">-</span>'}</div>
            ${units ? `<div id="bn-units" style="
              font-family:ui-monospace,'Cascadia Code','SF Mono','Menlo','Consolas',monospace;
              font-size:${Math.round(fontSize * 0.5)}px; font-weight:400; line-height:1;
              color:${color}; opacity:0.75; white-space:nowrap; flex-shrink:0;
            ">${this.esc(units)}</div>` : ''}
          </div>
        </div>

        <!-- Sparkline -->
        ${showSparkline && dm ? `
          <div style="flex-shrink:0; height:38px; margin-top:0.25rem;">
            <sparkline-widget
              id="${uid}-sl"
              device="${this.esc(dm.device)}"
              metric="${this.esc(dm.metric)}"
              time-period="48"
              color="${this.esc(color)}"
              units="${this.esc(units)}"
              refresh-interval="${this.config.refreshInterval}"
              style="display:block; height:38px;"
            ></sparkline-widget>
          </div>` : ''}
      </div>
    `;

    this.updateCardTitle();
  }

  protected attachEventListeners(): void {
    this.subscriptionActive = true;

    const fullPath = this.resolveTagPath();
    if (fullPath) {
      this._storeUnsub = getMirrorStore().subscribeTagReference(fullPath, (value: any) => {
        if (!this.subscriptionActive) return;
        this.currentValue = value;
        this.updateDisplay(true);
      });
    }

    // Re-subscribe when device changes only if the prefix still contains a wildcard.
    if (this.config.tagPrefix.includes('*')) {
      let skipInitialDeviceName = getUiStore().get('deviceName') !== '';
      this._deviceNameUnsub = getUiStore().subscribe('deviceName', () => {
        if (skipInitialDeviceName) {
          skipInitialDeviceName = false;
          return;
        }
        this.currentValue = null;
        this.rerender();
      });
    }
  }

  protected detachEventListeners(): void {
    this.subscriptionActive = false;
    this._storeUnsub?.();
    this._storeUnsub = null;
    this._deviceNameUnsub?.();
    this._deviceNameUnsub = null;
  }

  // ── Rendering helpers ───────────────────────────────────────────────────────

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (card && typeof card.setTitle === 'function') {
      card.setTitle(this.config.headerText ?? 'Big Number');
    }
  }

  /** Targeted DOM update - avoids full rerender on every live value tick. */
  private updateDisplay(appendSparkline = false): void {
    const valEl = this.querySelector<HTMLElement>('#bn-val');
    const displayValue = this.formatValue(this.currentValue);
    const color = this.resolveColor();

    if (!valEl) {
      // Placeholder was rendered (no data yet) - do a full rerender to show value
      this.rerender();
      return;
    }

    valEl.innerHTML = displayValue !== null
      ? this.esc(displayValue)
      : '<span style="opacity:0.18">-</span>';
    valEl.style.color = color;

    const unitsEl = this.querySelector<HTMLElement>('#bn-units');
    if (unitsEl) unitsEl.style.color = color;

    const iconEl = this.querySelector<HTMLElement>('#bn-icon');
    if (iconEl) {
      iconEl.style.color = this.iconColor(color);
      iconEl.innerHTML = this.renderIconHtml(color);
    }

    // Update sparkline color and fetch new data point
    const sparkline = this.querySelector<SparklineWidget>('sparkline-widget');
    if (sparkline) {
      sparkline.setAttribute('color', color);
      if (appendSparkline && typeof sparkline.appendLiveValue === 'function') {
        sparkline.appendLiveValue(this.currentValue);
      }
    }
  }

  private resolveColor(): string {
    const {
      colorBandsEnabled,
      colorBandsThreshold1, colorBandsThreshold2,
      colorBandsColor1, colorBandsColor2, colorBandsColor3,
    } = this.config;

    if (colorBandsEnabled && this.currentValue !== null) {
      const n = parseFloat(String(this.currentValue));
      if (!isNaN(n)) {
        if (n < colorBandsThreshold1) return colorBandsColor1;
        if (n < colorBandsThreshold2) return colorBandsColor2;
        return colorBandsColor3;
      }
    }

    return 'var(--accent-color)';
  }

  private formatValue(v: any): string | null {
    if (v === null || v === undefined) return null;
    if (typeof v === 'number') {
      return v.toFixed(this.config.decimals);
    }
    return String(v);
  }

  private normalizeDecimals(value: any): number {
    const n = Number(value);
    if (!Number.isFinite(n)) return DEFAULT_CONFIG.decimals;
    return Math.max(0, Math.min(10, Math.trunc(n)));
  }

  private iconColor(valueColor: string): string {
    return this.config.iconColor?.trim() || valueColor;
  }

  private renderIconHtml(valueColor: string): string {
    if (!this.config.showIcon || !this.config.icon) return '';
    const color = this.iconColor(valueColor);
    const svg = getIconSVG(this.config.icon, color, this.config.iconSize);
    return svg || '';
  }

  private ensureIconLoaded(): void {
    if (!this.config.showIcon || !this.config.icon?.includes(':')) return;
    const prefix = this.config.icon.split(':', 1)[0];
    if (!prefix || isIconSetLoaded(prefix) || this._loadingIconPrefix === prefix) return;
    this._loadingIconPrefix = prefix;
    loadIconSet(prefix).then(() => {
      if (this._loadingIconPrefix === prefix) this._loadingIconPrefix = null;
      if (this.isConnected) this.rerender();
    });
  }

  /**
   * Derive the metrics API device path and metric name from the resolved tag path.
   *
   * Assumes standard MQTT ingestion path structure:
   *   {org}.{deviceType}.{deviceName}[.tagGroups...].{tagName}
   * where the device stored in metric_devices is everything between the org and
   * the last two path segments (tagGroup + tagName).
   *
   * Returns null if the path is too short to extract a device.
   */
  private getDeviceAndMetric(): { device: string; metric: string } | null {
    const fullPath = this.resolveTagPath();
    if (!fullPath) return null;

    const parts = getMirrorStore().baseTagPath(fullPath).split('.');
    if (parts.length < 4) return null; // minimum: org.device.tagGroup.tagName

    const metric = parts.slice(-2).join('.');
    // Strip org (first) and last two segments (tagGroup + tagName) to get device path
    const device = parts.slice(1, -2).join('.');
    if (!device) return null;

    return { device, metric };
  }

  private rerender(): void {
    this.detachEventListeners();
    this.render();
    this.attachEventListeners();
  }

  private esc(s: string): string {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
}

// ── Registration ──────────────────────────────────────────────────────────────

registerWidgetType({
  type: 'big-number-widget',
  name: 'Big Number',
  icon: '🔢',
  category: 'Metrics',
  defaultW: 6,
  defaultH: 5,
  minW: 1,
  minH: 1,
});

customElements.define('big-number-widget', BigNumberWidget);
