import { BaseComponent } from '../../components/base-component';
import { getMirrorStore } from '../../store/store';
import { getUiStore } from '../../store/ui-store';
import { registerWidgetType } from './widget-registry';
import './sparkline-widget';
import type { SparklineWidget } from './sparkline-widget';
import type { PropertyField } from './widget-properties-dialog';
import { resolveMetricTagPath } from './tag-path-resolver';

// ── Config ────────────────────────────────────────────────────────────────────

interface Config {
  headerText: string;
  tagPrefix: string;
  tagPath: string;
  minValue: number;
  maxValue: number;
  maxTagPath: string;
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
  headerText: 'Gauge',
  tagPrefix: '',
  tagPath: '',
  minValue: 0,
  maxValue: 100,
  maxTagPath: '',
  showSparkline: true,
  refreshInterval: 0,
  colorBandsEnabled: false,
  colorBandsThreshold1: 50,
  colorBandsThreshold2: 80,
  colorBandsColor1: '#22c55e',
  colorBandsColor2: '#f59e0b',
  colorBandsColor3: '#ef4444',
};

// ── Global styles (injected once) ─────────────────────────────────────────────

function ensureGlobalStyles(): void {
  if (document.getElementById('gauge-widget-styles')) return;
  const style = document.createElement('style');
  style.id = 'gauge-widget-styles';
  style.textContent = `
    @keyframes gw-fadein {
      from { opacity: 0; transform: scale(0.97); }
      to   { opacity: 1; transform: scale(1); }
    }
    .gw-fadein { animation: gw-fadein 0.28s ease-out; }
  `;
  document.head.appendChild(style);
}

// ── Widget ────────────────────────────────────────────────────────────────────

export class GaugeWidget extends BaseComponent {
  private config: Config = { ...DEFAULT_CONFIG };
  private currentValue: any = null;
  private currentMaxValue: number | null = null;
  private subscriptionActive = false;
  private _storeUnsubs: Array<() => void> = [];
  private _deviceNameUnsub: (() => void) | null = null;
  private _updateFrame: number | null = null;
  private _appendSparklineOnUpdate = false;

  // ── Public API ──────────────────────────────────────────────────────────────

  setConfig(c: Partial<Config> & Record<string, any>): void {
    const prevPath = this.config.tagPath;
    const prevPrefix = this.config.tagPrefix;
    const prevMaxTag = this.config.maxTagPath;
    const showSparkline = Boolean(c.showSparkline ?? this.config.showSparkline);

    this.config = {
      ...this.config,
      ...c,
      showSparkline,
      refreshInterval: showSparkline ? Number(c.refreshInterval ?? this.config.refreshInterval) || 0 : 0,
    };

    if (this.config.tagPath !== prevPath || this.config.tagPrefix !== prevPrefix) {
      this.currentValue = null;
    }
    if (this.config.maxTagPath !== prevMaxTag) {
      this.currentMaxValue = null;
    }

    this.rerender();
  }

  getPropertySchema(): PropertyField[] {
    const inArray = !!(this.config as any).arrayElementPath;
    return [
      {
        name: 'headerText',
        type: 'string',
        label: 'Header text',
        default: 'Gauge',
      },
      ...(!inArray ? [{
        name: 'tagPrefix',
        type: 'path' as const,
        label: 'Tag prefix (use * for dashboard device name)',
        description: 'e.g. Pumps.* - * is replaced with the current device name',
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
        name: 'minValue',
        type: 'number',
        label: 'Min value',
        default: 0,
      },
      {
        name: 'maxValue',
        type: 'number',
        label: 'Max value (constant)',
        default: 100,
      },
      {
        name: 'maxTagPath',
        type: 'path',
        label: 'Max value tag (overrides constant if set)',
        default: '',
        context: { includeLeaves: true },
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
    ensureGlobalStyles();

    const { showSparkline } = this.config;
    const fullPath = this.resolveTagPath();
    const units = fullPath ? (getMirrorStore().getNodeShared(getMirrorStore().baseTagPath(fullPath))?.units ?? '') : '';
    const dm = this.getDeviceAndMetric();
    const uid = this.getAttribute('id') || 'g0';
    const accentColor = this.resolveColor();

    this.innerHTML = `
      <div style="
        display:flex; flex-direction:column; height:100%;
        padding:0.125rem 0 0.5rem; box-sizing:border-box; overflow:hidden;
      ">
        <div class="gw-svg-container gw-fadein" style="
          flex:1; min-height:0; width:100%;
          display:flex; align-items:center; justify-content:center; overflow:hidden;
        ">
          ${this.buildGaugeSvg(units)}
        </div>

        ${showSparkline && dm ? `
          <div style="flex-shrink:0; height:38px; margin-top:0.375rem;">
            <sparkline-widget
              id="${uid}-sl"
              device="${this.esc(dm.device)}"
              metric="${this.esc(dm.metric)}"
              time-period="48"
              color="${this.esc(accentColor)}"
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
      this._storeUnsubs.push(getMirrorStore().subscribeTagReference(fullPath, (value: any) => {
        if (!this.subscriptionActive) return;
        this.currentValue = value;
        this.scheduleGaugeUpdate(true);
      }));
    }

    if (this.config.maxTagPath) {
      this._storeUnsubs.push(getMirrorStore().subscribeTagReference(getMirrorStore().toAbsolute(this.config.maxTagPath), (value: any) => {
        if (!this.subscriptionActive) return;
        const n = parseFloat(String(value));
        this.currentMaxValue = isNaN(n) ? null : n;
        this.scheduleGaugeUpdate();
      }));
    }

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
    this.cancelScheduledGaugeUpdate();
    this._storeUnsubs.forEach(unsub => unsub());
    this._storeUnsubs = [];
    this._deviceNameUnsub?.();
    this._deviceNameUnsub = null;
  }

  // ── Rendering helpers ───────────────────────────────────────────────────────

  private updateCardTitle(): void {
    const card = this.closest('widget-card') as any;
    if (card && typeof card.setTitle === 'function') {
      card.setTitle(this.config.headerText ?? 'Gauge');
    }
  }

  /** Targeted update - replaces the SVG only, preserving sparkline. */
  private updateGauge(appendSparkline = false): void {
    const container = this.querySelector<HTMLElement>('.gw-svg-container');
    if (!container) { this.rerender(); return; }

    const fullPath = this.resolveTagPath();
    const units = fullPath ? (getMirrorStore().getNodeShared(getMirrorStore().baseTagPath(fullPath))?.units ?? '') : '';
    container.innerHTML = this.buildGaugeSvg(units);

    const sparkline = this.querySelector<SparklineWidget>('sparkline-widget');
    if (sparkline) {
      sparkline.setAttribute('color', this.resolveColor());
      if (appendSparkline && typeof sparkline.appendLiveValue === 'function') {
        sparkline.appendLiveValue(this.currentValue);
      }
    }
  }

  private scheduleGaugeUpdate(appendSparkline = false): void {
    this._appendSparklineOnUpdate ||= appendSparkline;
    if (this._updateFrame !== null) return;
    this._updateFrame = window.requestAnimationFrame(() => {
      this._updateFrame = null;
      if (!this.subscriptionActive || !this.isConnected) {
        this._appendSparklineOnUpdate = false;
        return;
      }
      const shouldAppend = this._appendSparklineOnUpdate;
      this._appendSparklineOnUpdate = false;
      this.updateGauge(shouldAppend);
    });
  }

  private cancelScheduledGaugeUpdate(): void {
    if (this._updateFrame === null) return;
    window.cancelAnimationFrame(this._updateFrame);
    this._updateFrame = null;
    this._appendSparklineOnUpdate = false;
  }

  // ── SVG gauge ───────────────────────────────────────────────────────────────

  /**
   * Builds the SVG gauge.
   *
   * Geometry:
   *   - Arc starts at 8 o'clock (SVG angle 150°) and sweeps 240° clockwise to 4 o'clock (SVG 30°).
   *   - SVG angle convention: 0° = 3 o'clock, increasing clockwise.
   */
  private buildGaugeSvg(units: string): string {
    const {
      minValue,
      colorBandsEnabled, colorBandsThreshold1, colorBandsThreshold2,
      colorBandsColor1, colorBandsColor2, colorBandsColor3,
    } = this.config;

    const maxValue = this.effectiveMax();
    const rawValue = this.currentValue !== null ? parseFloat(String(this.currentValue)) : null;
    const value = rawValue !== null && !isNaN(rawValue) ? rawValue : null;

    // ── Geometry constants ────────────────────────────────────────────────────
    const CX = 100, CY = 98;
    const R = 70;
    const TRACK_W = 14;
    const START_DEG = 150;   // 8 o'clock in SVG convention
    const SWEEP = 240;       // degrees clockwise to 4 o'clock

    const toRad = (d: number) => d * Math.PI / 180;
    const px = (a: number, r: number) => CX + r * Math.cos(toRad(a));
    const py = (a: number, r: number) => CY + r * Math.sin(toRad(a));

    /** SVG arc path: clockwise arc from startDeg by sweepDeg at radius r. */
    const arcD = (r: number, startDeg: number, sweepDeg: number): string => {
      if (sweepDeg < 0.05) return '';
      const x1 = px(startDeg, r),          y1 = py(startDeg, r);
      const x2 = px(startDeg + sweepDeg, r), y2 = py(startDeg + sweepDeg, r);
      const largeArc = sweepDeg > 180 ? 1 : 0;
      return `M${x1.toFixed(2)},${y1.toFixed(2)} A${r},${r} 0 ${largeArc} 1 ${x2.toFixed(2)},${y2.toFixed(2)}`;
    };

    // ── Value mapping ─────────────────────────────────────────────────────────
    let valueRatio = 0;
    if (value !== null && maxValue > minValue) {
      valueRatio = Math.max(0, Math.min(1, (value - minValue) / (maxValue - minValue)));
    }
    const needleDeg  = START_DEG + valueRatio * SWEEP;
    const fillColor  = this.resolveColor();
    const fillSweep  = valueRatio * SWEEP;

    // ── Track (background) ────────────────────────────────────────────────────
    const trackPath = arcD(R, START_DEG, SWEEP);

    // ── Active fill arc ───────────────────────────────────────────────────────
    const fillPath = fillSweep > 0.5 ? arcD(R, START_DEG, fillSweep) : '';

    // ── Color bands ───────────────────────────────────────────────────────────
    let bandsHtml = '';
    if (colorBandsEnabled && maxValue > minValue) {
      const BAND_R = R - TRACK_W / 2 - 2;
      const BAND_W = 3;

      const drawBand = (from: number, to: number, color: string): string => {
        const f = Math.max(0, Math.min(1, (from - minValue) / (maxValue - minValue)));
        const t = Math.max(0, Math.min(1, (to - minValue) / (maxValue - minValue)));
        if (t <= f) return '';
        const d = arcD(BAND_R, START_DEG + f * SWEEP, (t - f) * SWEEP);
        return d
          ? `<path d="${d}" fill="none" stroke="${color}" stroke-width="${BAND_W}" stroke-linecap="butt" opacity="0.9"/>`
          : '';
      };

      bandsHtml = drawBand(minValue, colorBandsThreshold1, colorBandsColor1)
               + drawBand(colorBandsThreshold1, colorBandsThreshold2, colorBandsColor2)
               + drawBand(colorBandsThreshold2, maxValue, colorBandsColor3);
    }

    // ── Tick marks ────────────────────────────────────────────────────────────
    let ticksHtml = '';
    const NUM_MAJOR = 5;
    for (let i = 0; i <= NUM_MAJOR; i++) {
      const angle = START_DEG + (i / NUM_MAJOR) * SWEEP;
      const outerR = R + 2;
      const innerR = R - TRACK_W - 3;
      ticksHtml += `<line
        x1="${px(angle, outerR).toFixed(2)}" y1="${py(angle, outerR).toFixed(2)}"
        x2="${px(angle, innerR).toFixed(2)}" y2="${py(angle, innerR).toFixed(2)}"
        stroke="var(--content-text)" stroke-opacity="0.3" stroke-width="1.2" stroke-linecap="round"/>`;

      if (i < NUM_MAJOR) {
        for (let j = 1; j <= 4; j++) {
          const ma = START_DEG + ((i + j / 5) / NUM_MAJOR) * SWEEP;
          ticksHtml += `<line
            x1="${px(ma, R).toFixed(2)}" y1="${py(ma, R).toFixed(2)}"
            x2="${px(ma, R - 5).toFixed(2)}" y2="${py(ma, R - 5).toFixed(2)}"
            stroke="var(--content-text)" stroke-opacity="0.15" stroke-width="0.6"/>`;
        }
      }
    }

    // ── Needle ────────────────────────────────────────────────────────────────
    const NEEDLE_LEN = R - TRACK_W / 2 - 1;
    const needleX = px(needleDeg, NEEDLE_LEN);
    const needleY = py(needleDeg, NEEDLE_LEN);

    const needleHtml = value !== null ? `
      <line x1="${CX}" y1="${CY}"
            x2="${needleX.toFixed(2)}" y2="${needleY.toFixed(2)}"
            stroke="${fillColor}" stroke-width="2" stroke-linecap="round" opacity="0.95"/>
      <circle cx="${CX}" cy="${CY}" r="5" fill="${fillColor}" opacity="0.9"/>
      <circle cx="${CX}" cy="${CY}" r="2.5" fill="var(--panel-bg, #111)"/>
    ` : `
      <circle cx="${CX}" cy="${CY}" r="5" fill="var(--content-text)" opacity="0.12"/>
    `;

    // ── Value label ───────────────────────────────────────────────────────────
    const displayValue = value !== null
      ? (Number.isInteger(value) ? String(value) : value.toFixed(2))
      : null;
    const textColor   = value !== null ? fillColor : 'var(--content-text)';
    const textOpacity = value !== null ? 1 : 0.15;
    const LABEL_Y = CY + 28;

    // Number is centered on CX so the decimal point falls at the midpoint for
    // symmetric values (e.g. "78.20"). Units are placed immediately to the right
    // of the number's right edge, estimated from character count × monospace
    // char width (~11.5 SVG units at font-size 20).
    const numStr = displayValue !== null ? displayValue : '-';
    const CHAR_W = 11.5;
    const unitsX = CX + (numStr.length * CHAR_W / 2) + 3;

    const valueText = `
      <text x="${CX}" y="${LABEL_Y}"
            text-anchor="middle"
            font-family="ui-monospace,'Cascadia Code','SF Mono','Menlo','Consolas',monospace"
            font-size="20" font-weight="300" letter-spacing="-0.01em"
            fill="${textColor}" opacity="${textOpacity}">
        ${this.esc(numStr)}
      </text>
      ${units && displayValue !== null ? `
      <text x="${unitsX.toFixed(1)}" y="${LABEL_Y}"
            text-anchor="start" dominant-baseline="auto"
            font-family="ui-monospace,'Cascadia Code','SF Mono','Menlo','Consolas',monospace"
            font-size="11" font-weight="400"
            fill="${textColor}" opacity="${String(textOpacity * 0.65)}">
        ${this.esc(units)}
      </text>` : ''}`;

    // ── Min / max labels ──────────────────────────────────────────────────────
    const fmtLabel = (n: number) => Number.isInteger(n) ? String(n) : n.toFixed(1);
    const LABEL_R = R + 18;
    const minLx = px(START_DEG, LABEL_R),        minLy = py(START_DEG, LABEL_R);
    const maxLx = px(START_DEG + SWEEP, LABEL_R), maxLy = py(START_DEG + SWEEP, LABEL_R);

    const minMaxHtml = `
      <text x="${minLx.toFixed(2)}" y="${minLy.toFixed(2)}"
            text-anchor="middle"
            font-family="ui-monospace,'Cascadia Code','SF Mono',monospace"
            font-size="11" fill="var(--content-text)" opacity="0.35">
        ${this.esc(fmtLabel(minValue))}
      </text>
      <text x="${maxLx.toFixed(2)}" y="${maxLy.toFixed(2)}"
            text-anchor="middle"
            font-family="ui-monospace,'Cascadia Code','SF Mono',monospace"
            font-size="11" fill="var(--content-text)" opacity="0.35">
        ${this.esc(fmtLabel(maxValue))}
      </text>`;

    // ── Assemble ──────────────────────────────────────────────────────────────
    return `
      <svg viewBox="0 0 200 168" xmlns="http://www.w3.org/2000/svg"
           style="width:100%;height:100%;max-height:100%;display:block;">
        <!-- Background track -->
        <path d="${trackPath}" fill="none"
              stroke="var(--content-text)" stroke-opacity="0.08"
              stroke-width="${TRACK_W}" stroke-linecap="round"/>

        <!-- Color bands -->
        ${bandsHtml}

        <!-- Active fill -->
        ${fillPath ? `<path d="${fillPath}" fill="none" stroke="${fillColor}"
                           stroke-width="${TRACK_W}" stroke-linecap="round" opacity="0.85"/>` : ''}

        <!-- Tick marks -->
        ${ticksHtml}

        <!-- Needle -->
        ${needleHtml}

        <!-- Value -->
        ${valueText}

        <!-- Min / max labels -->
        ${minMaxHtml}
      </svg>`;
  }

  // ── Utility ─────────────────────────────────────────────────────────────────

  private resolveTagPath(): string {
    return resolveMetricTagPath(this.config.tagPrefix, this.config.tagPath);
  }

  private effectiveMax(): number {
    if (this.config.maxTagPath && this.currentMaxValue !== null) {
      return this.currentMaxValue;
    }
    return this.config.maxValue;
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

  private getDeviceAndMetric(): { device: string; metric: string } | null {
    const fullPath = this.resolveTagPath();
    if (!fullPath) return null;
    const parts = getMirrorStore().baseTagPath(fullPath).split('.');
    if (parts.length < 4) return null;
    const metric = parts.slice(-2).join('.');
    const device = parts.slice(1, -2).join('.');
    return device ? { device, metric } : null;
  }

  private rerender(): void {
    this.cancelScheduledGaugeUpdate();
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
  type: 'gauge-widget',
  name: 'Gauge',
  icon: '🎯',
  category: 'Metrics',
  defaultW: 5,
  defaultH: 6,
  minW: 3,
  minH: 4,
});

customElements.define('gauge-widget', GaugeWidget);
